package client_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// sha256Hex returns the SHA-256 checksum of s as hex.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newBodyServer returns a server that replies with status and body.
func newBodyServer(t *testing.T, status int, body string) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// newPartialServer returns a server that replies to a resume with the bytes
// from offset 3 of "abcdef" up to end, and the Range header of each request.
func newPartialServer(t *testing.T, end int) (*httptest.Server, func() []string) {
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("Range") == "" {
			_, _ = w.Write([]byte("abcdef"))
			return
		}
		w.Header().Set("Content-Range", "bytes 3-"+strconv.Itoa(end)+"/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abcdef"[3 : end+1]))
	}))
	t.Cleanup(server.Close)
	return server, func() []string { return ranges }
}

func TestOption_SetChecksum(t *testing.T) {
	t.Run("buffered body", func(t *testing.T) {
		server := newBodyServer(t, http.StatusOK, "hello")

		tests := []struct {
			name     string
			expected string
			wantErr  error
		}{
			{"matching", sha256Hex("hello"), nil},
			{"matching in upper case", strings.ToUpper(sha256Hex("hello")), nil},
			{"different", sha256Hex("other"), client.ErrChecksumMismatch},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp, err := client.Get(server.URL, options.New().SetChecksum(sha256.New, tt.expected))
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
				}
				if tt.wantErr == nil && resp.String() != "hello" {
					t.Errorf("String() = %q, want %q", resp.String(), "hello")
				}
				if tt.wantErr != nil && !resp.Body.IsEmpty() {
					t.Errorf("body has %d bytes, want none", resp.Body.Len())
				}
			})
		}
	})

	t.Run("file download", func(t *testing.T) {
		server := newBodyServer(t, http.StatusOK, "hello")

		tests := []struct {
			name     string
			existing string
			expected string
			wantErr  error
			want     string
		}{
			{"matching", "old", sha256Hex("hello"), nil, "hello"},
			{"different keeps the destination", "old", sha256Hex("other"), client.ErrChecksumMismatch, "old"},
			{"different creates no file", absent, sha256Hex("other"), client.ErrChecksumMismatch, absent},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "download.txt")
				if tt.existing != absent {
					if err := os.WriteFile(path, []byte(tt.existing), 0o644); err != nil {
						t.Fatal(err)
					}
				}

				_, err := client.Get(server.URL, options.New().SetFileOutput(path).SetChecksum(sha256.New, tt.expected))
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
				}
				if got := contentOrAbsent(t, path); got != tt.want {
					t.Errorf("destination = %q, want %q", got, tt.want)
				}
			})
		}
	})

	// A resumed download is checked against the checksum of the whole file,
	// not only the bytes it received.
	t.Run("resume covers whole file", func(t *testing.T) {
		server, _ := newPartialServer(t, 5)
		path := filepath.Join(t.TempDir(), "download.bin")
		writePartial(t, path, "abc")

		_, err := client.Get(server.URL, options.New().Resume(path, etag(`"v1"`)).SetChecksum(sha256.New, sha256Hex("abcdef")))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got := contentOrAbsent(t, path); got != "abcdef" {
			t.Errorf("destination = %q, want %q", got, "abcdef")
		}
	})

	// A complete resumed file that does not match is removed, so the next
	// resume starts again.
	t.Run("resume mismatch removes partial file", func(t *testing.T) {
		server, ranges := newPartialServer(t, 5)
		path := filepath.Join(t.TempDir(), "download.bin")
		writePartial(t, path, "abc")

		_, err := client.Get(server.URL, options.New().Resume(path, etag(`"v1"`)).SetChecksum(sha256.New, sha256Hex("other")))
		if !errors.Is(err, client.ErrChecksumMismatch) {
			t.Fatalf("Get() error = %v, want %v", err, client.ErrChecksumMismatch)
		}
		if got := contentOrAbsent(t, path); got != absent {
			t.Errorf("destination = %q, want %q", got, absent)
		}
		if got := contentOrAbsent(t, options.PartialPath(path)); got != absent {
			t.Errorf("partial file = %q, want %q", got, absent)
		}

		if _, err := client.Get(server.URL, options.New().Resume(path, etag(`"v1"`))); err != nil {
			t.Fatalf("second Get() error = %v", err)
		}
		if got := ranges(); len(got) != 2 || got[1] != "" {
			t.Errorf("Range headers = %q, want the second request to have none", got)
		}
	})

	// A resume that leaves the file incomplete is not checked, and keeps its
	// partial file.
	t.Run("incomplete resume", func(t *testing.T) {
		server, _ := newPartialServer(t, 4)
		path := filepath.Join(t.TempDir(), "download.bin")
		writePartial(t, path, "abc")

		_, err := client.Get(server.URL, options.New().Resume(path, etag(`"v1"`)).SetChecksum(sha256.New, sha256Hex("abcdef")))
		if !errors.Is(err, client.ErrDownloadIncomplete) {
			t.Fatalf("Get() error = %v, want %v", err, client.ErrDownloadIncomplete)
		}
		if got := contentOrAbsent(t, options.PartialPath(path)); got != "abcde" {
			t.Errorf("partial file = %q, want %q", got, "abcde")
		}
	})

	// An error response is not checked, so the caller sees its status.
	t.Run("error response", func(t *testing.T) {
		server := newBodyServer(t, http.StatusNotFound, "not found")

		resp, err := client.Get(server.URL, options.New().SetChecksum(sha256.New, sha256Hex("hello")))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNotFound)
		}
	})

	// A compressed response is checked against the checksum of the
	// decompressed body.
	t.Run("decompressed body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte("hello"))
			if err := gz.Close(); err != nil {
				t.Error(err)
			}
		}))
		defer server.Close()

		resp, err := client.Get(server.URL, options.New().SetChecksum(sha256.New, sha256Hex("hello")))
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.String() != "hello" {
			t.Errorf("String() = %q, want %q", resp.String(), "hello")
		}
	})

	// A checksum that is not valid hex of the hash's size fails before the
	// request is sent.
	t.Run("invalid value", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
		}))
		defer server.Close()

		for _, expected := range []string{"not hex", "abcd"} {
			t.Run(expected, func(t *testing.T) {
				resp, err := client.Get(server.URL, options.New().SetChecksum(sha256.New, expected))
				if err == nil {
					t.Fatal("Get() error = nil, want error")
				}
				if resp.Error != err {
					t.Errorf("Response.Error = %v, want %v", resp.Error, err)
				}
			})
		}
		if got := requests.Load(); got != 0 {
			t.Errorf("server received %d requests, want 0", got)
		}
	})

	// A per-request checksum applies through a Client.
	t.Run("through a client", func(t *testing.T) {
		server := newBodyServer(t, http.StatusOK, "hello")

		c := client.New()
		if _, err := c.Get(server.URL, options.New().SetChecksum(sha256.New, sha256Hex("other"))); !errors.Is(err, client.ErrChecksumMismatch) {
			t.Errorf("Get() error = %v, want %v", err, client.ErrChecksumMismatch)
		}
	})
}
