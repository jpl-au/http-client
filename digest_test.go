package client_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// sha256Digest returns an RFC 9530 digest header value for data.
func sha256Digest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

// sha512Digest returns an RFC 9530 digest header value for data.
func sha512Digest(data string) string {
	sum := sha512.Sum512([]byte(data))
	return "sha-512=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

// newDigestServer returns a server that replies with status, body and the
// given headers.
func newDigestServer(t *testing.T, status int, body string, header http.Header) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maps.Copy(w.Header(), header)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDigestHeaders(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		header  http.Header
		wantErr error
	}{
		{"matching Repr-Digest", http.StatusOK, http.Header{"Repr-Digest": {sha256Digest("hello")}}, nil},
		{"different Repr-Digest", http.StatusOK, http.Header{"Repr-Digest": {sha256Digest("other")}}, client.ErrChecksumMismatch},
		{"matching Content-Digest", http.StatusOK, http.Header{"Content-Digest": {sha256Digest("hello")}}, nil},
		{"different Content-Digest", http.StatusOK, http.Header{"Content-Digest": {sha256Digest("other")}}, client.ErrChecksumMismatch},
		{"matching sha-512", http.StatusOK, http.Header{"Repr-Digest": {sha512Digest("hello")}}, nil},
		{"sha-512 is preferred", http.StatusOK, http.Header{"Repr-Digest": {sha256Digest("hello") + ", " + sha512Digest("other")}}, client.ErrChecksumMismatch},
		{"unknown algorithm is ignored", http.StatusOK, http.Header{"Repr-Digest": {"md5=:AAAAAAAAAAAAAAAAAAAAAA==:"}}, nil},
		{"malformed value is ignored", http.StatusOK, http.Header{"Repr-Digest": {"sha-256=not a byte sequence"}}, nil},
		{"error response is not checked", http.StatusNotFound, http.Header{"Repr-Digest": {sha256Digest("other")}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDigestServer(t, tt.status, "hello", tt.header)

			resp, err := client.Get(server.URL)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && !resp.Body.IsEmpty() {
				t.Errorf("body has %d bytes, want none", resp.Body.Len())
			}
		})
	}
}

func TestDigestFileDownloadKeepsDestination(t *testing.T) {
	server := newDigestServer(t, http.StatusOK, "hello", http.Header{"Repr-Digest": {sha256Digest("other")}})
	path := filepath.Join(t.TempDir(), "download.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Get(server.URL, options.New().SetFileOutput(path)); !errors.Is(err, client.ErrChecksumMismatch) {
		t.Fatalf("Get() error = %v, want %v", err, client.ErrChecksumMismatch)
	}
	if got := contentOrAbsent(t, path); got != "old" {
		t.Errorf("destination = %q, want %q", got, "old")
	}
}

func TestDisableDigestCheck(t *testing.T) {
	server := newDigestServer(t, http.StatusOK, "hello", http.Header{"Repr-Digest": {sha256Digest("other")}})

	resp, err := client.Get(server.URL, options.New().DisableDigestCheck())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.String() != "hello" {
		t.Errorf("String() = %q, want %q", resp.String(), "hello")
	}

	c := client.New(options.New().DisableDigestCheck())
	if _, err := c.Get(server.URL, options.New().EnableDigestCheck()); !errors.Is(err, client.ErrChecksumMismatch) {
		t.Errorf("Get() with a per-request EnableDigestCheck error = %v, want %v", err, client.ErrChecksumMismatch)
	}
}

// TestDigestCoversSentBytes checks that a digest of a response the client
// decompresses itself covers the compressed bytes the server sent.
func TestDigestCoversSentBytes(t *testing.T) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		digest  string
		wantErr error
	}{
		{"digest of the sent bytes", sha256Digest(compressed.String()), nil},
		{"digest of the decompressed bytes", sha256Digest("hello"), client.ErrChecksumMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDigestServer(t, http.StatusOK, compressed.String(), http.Header{
				"Content-Encoding": {"deflate"},
				"Content-Digest":   {tt.digest},
			})

			resp, err := client.Get(server.URL)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && resp.String() != "hello" {
				t.Errorf("String() = %q, want %q", resp.String(), "hello")
			}
		})
	}
}

// TestDigestSkipsTransportDecompressedResponse checks that a gzip response
// that net/http decompresses is not checked, because the bytes the digest
// covers are not available.
func TestDigestSkipsTransportDecompressedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Digest", sha256Digest("not the sent bytes"))
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("hello"))
		if err := gz.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.String() != "hello" {
		t.Errorf("String() = %q, want %q", resp.String(), "hello")
	}
}

// newDigestResumeServer returns a server that replies to a resume of "abcdef"
// with bytes 3 to 5 and the given headers.
func newDigestResumeServer(t *testing.T, header http.Header) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maps.Copy(w.Header(), header)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("def"))
	}))
	t.Cleanup(server.Close)
	return server
}

// TestDigestResumeCoversWholeFile checks that Repr-Digest on a resumed download
// is checked against the whole file.
func TestDigestResumeCoversWholeFile(t *testing.T) {
	tests := []struct {
		name        string
		digest      string
		wantErr     error
		wantFile    string
		wantPartial string
	}{
		{"matching", sha256Digest("abcdef"), nil, "abcdef", absent},
		{"different", sha256Digest("other"), client.ErrChecksumMismatch, absent, absent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDigestResumeServer(t, http.Header{"Repr-Digest": {tt.digest}})
			path := filepath.Join(t.TempDir(), "download.bin")
			writePartial(t, path, "abc")

			_, err := client.Get(server.URL, options.New().Resume(path, etag(`"v1"`)))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if got := contentOrAbsent(t, path); got != tt.wantFile {
				t.Errorf("destination = %q, want %q", got, tt.wantFile)
			}
			if got := contentOrAbsent(t, options.PartialPath(path)); got != tt.wantPartial {
				t.Errorf("partial file = %q, want %q", got, tt.wantPartial)
			}
		})
	}
}

// TestDigestRange checks that a range response is checked with Content-Digest,
// which covers the bytes it holds, and not with Repr-Digest, which covers the
// whole representation.
func TestDigestRange(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		wantErr error
	}{
		{"matching Content-Digest", http.Header{"Content-Digest": {sha256Digest("def")}}, nil},
		{"different Content-Digest", http.Header{"Content-Digest": {sha256Digest("other")}}, client.ErrChecksumMismatch},
		{"Repr-Digest is not checked", http.Header{"Repr-Digest": {sha256Digest("abcdef")}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDigestResumeServer(t, tt.header)

			_, err := client.Get(server.URL, options.New().SetRange(3, 5))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
