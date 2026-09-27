package client_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

// segmentServer serves content with http.ServeContent, which answers range
// requests, and records the requests it receives.
type segmentServer struct {
	*httptest.Server
	mu          sync.Mutex
	ranges      []string // Range header of each request, in order.
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

// segmentConfig describes how a segmentServer answers request n, counting
// from 1.
type segmentConfig struct {
	content func(n int) []byte // The content to serve. Required.
	etag    func(n int) string // The ETag to send, or "" for none.
	fail    func(r *http.Request) bool
	header  http.Header // Extra headers to send.
}

func newSegmentServer(t *testing.T, cfg segmentConfig) *segmentServer {
	s := &segmentServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		n := len(s.ranges)
		s.mu.Unlock()

		current := s.inFlight.Add(1)
		defer s.inFlight.Add(-1)
		for {
			highest := s.maxInFlight.Load()
			if current <= highest || s.maxInFlight.CompareAndSwap(highest, current) {
				break
			}
		}
		// A short wait lets the requests of one download overlap.
		time.Sleep(20 * time.Millisecond)

		if cfg.fail != nil && cfg.fail(r) {
			http.Error(w, "segment failed", http.StatusInternalServerError)
			return
		}
		maps.Copy(w.Header(), cfg.header)
		if cfg.etag != nil && cfg.etag(n) != "" {
			w.Header().Set("ETag", cfg.etag(n))
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(cfg.content(n)))
	}))
	t.Cleanup(s.Close)
	return s
}

// requests returns the Range header of each request the server received.
func (s *segmentServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ranges)
}

// same returns a function that gives v for every request.
func same[T any](v T) func(int) T { return func(int) T { return v } }

// onlyFile checks that dir holds only the file name, so no temporary file is
// left behind.
func onlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != name {
		t.Errorf("directory holds %q, want only %q", names, name)
	}
}

func TestSegmentedDownload(t *testing.T) {
	server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
	path := filepath.Join(t.TempDir(), "download.bin")

	var calls, overlaps atomic.Int32
	var last, total int64
	backwards := false
	opt := options.New().SetFileOutput(path).SetSegments(4).OnDownloadProgress(func(current, size int64) {
		if calls.Add(1) != 1 {
			overlaps.Add(1)
		}
		if current < last {
			backwards = true
		}
		last, total = current, size
		calls.Add(-1)
	})

	resp, err := client.Get(server.URL, opt)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, largefile.Bytes()) {
		t.Errorf("file has %d bytes that do not match the %d served", len(data), largefile.Len())
	}
	if got := len(server.requests()); got != 4 {
		t.Errorf("server received %d requests, want 4 segments", got)
	}
	if got := server.maxInFlight.Load(); got < 2 {
		t.Errorf("at most %d requests ran at once, want at least 2", got)
	}

	if resp.StatusCode != http.StatusOK || resp.IsPartialContent || resp.ContentRange != nil {
		t.Errorf("StatusCode = %d, IsPartialContent = %v, ContentRange = %v, want a 200 response for the whole file",
			resp.StatusCode, resp.IsPartialContent, resp.ContentRange)
	}
	if want := int64(largefile.Len()); resp.ContentLength != want {
		t.Errorf("ContentLength = %d, want %d", resp.ContentLength, want)
	}

	if last != int64(largefile.Len()) || total != int64(largefile.Len()) {
		t.Errorf("last progress = %d of %d, want %d of %d", last, total, largefile.Len(), largefile.Len())
	}
	if backwards {
		t.Error("progress went backwards")
	}
	if overlaps.Load() != 0 {
		t.Error("the progress callback ran more than once at the same time")
	}
}

func TestSegmentedDownloadWithoutRangeSupport(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(largefile.Bytes())
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "download.bin")

	if _, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4)); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("server received %d requests, want 1", got)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
		t.Errorf("file does not match the content served (error %v)", err)
	}
}

// TestSegmentedDownloadWithoutValidator checks that a file with no strong
// validator is downloaded whole, because its segments could come from
// different versions.
func TestSegmentedDownloadWithoutValidator(t *testing.T) {
	server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes())})
	path := filepath.Join(t.TempDir(), "download.bin")

	if _, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4)); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := server.requests(); len(got) != 2 || got[1] != "" {
		t.Errorf("Range headers = %q, want a first request and one request with no range", got)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
		t.Errorf("file does not match the content served (error %v)", err)
	}
}

// TestSegmentedDownloadFileChanged checks that a download fails when the file
// changes on the server between segments.
func TestSegmentedDownloadFileChanged(t *testing.T) {
	changed := bytes.Repeat([]byte("changed "), largefile.Len()/8)
	server := newSegmentServer(t, segmentConfig{
		content: func(n int) []byte {
			if n == 1 {
				return largefile.Bytes()
			}
			return changed
		},
		etag: func(n int) string {
			if n == 1 {
				return `"v1"`
			}
			return `"v2"`
		},
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4))
	if !errors.Is(err, client.ErrRangeMismatch) {
		t.Fatalf("Get() error = %v, want %v", err, client.ErrRangeMismatch)
	}
	if got := contentOrAbsent(t, path); got != "old" {
		t.Errorf("destination = %q, want %q", got, "old")
	}
	onlyFile(t, dir, "download.bin")
}

// TestSegmentedDownloadSegmentFails checks that a failed segment fails the
// download and leaves the destination and its directory as they were.
func TestSegmentedDownloadSegmentFails(t *testing.T) {
	server := newSegmentServer(t, segmentConfig{
		content: same(largefile.Bytes()),
		etag:    same(`"v1"`),
		fail: func(r *http.Request) bool {
			return r.Header.Get("Range") != "" && !strings.HasPrefix(r.Header.Get("Range"), "bytes=0-")
		},
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4)); err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	if got := contentOrAbsent(t, path); got != "old" {
		t.Errorf("destination = %q, want %q", got, "old")
	}
	onlyFile(t, dir, "download.bin")
}

// TestSegmentedDownloadSmallFile checks that a file too small to split is not
// split into segments.
func TestSegmentedDownloadSmallFile(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		requests int
	}{
		{"fits in the first request", 512 << 10, 1},
		{"too small for two segments", 3 << 19, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := largefile.Bytes()[:tt.size]
			server := newSegmentServer(t, segmentConfig{content: same(content), etag: same(`"v1"`)})
			path := filepath.Join(t.TempDir(), "download.bin")

			if _, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4)); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got := len(server.requests()); got != tt.requests {
				t.Errorf("server received %d requests, want %d", got, tt.requests)
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, content) {
				t.Errorf("file does not match the content served (error %v)", err)
			}
		})
	}
}

func TestSegmentedDownloadChecksums(t *testing.T) {
	whole := string(largefile.Bytes())
	tests := []struct {
		name    string
		opt     func(*options.Option)
		header  http.Header
		wantErr error
	}{
		{"matching SetChecksum", func(o *options.Option) { o.SetChecksum(sha256.New, sha256Hex(whole)) }, nil, nil},
		{"different SetChecksum", func(o *options.Option) { o.SetChecksum(sha256.New, sha256Hex("other")) }, nil, client.ErrChecksumMismatch},
		{"matching Repr-Digest", func(*options.Option) {}, http.Header{"Repr-Digest": {sha256Digest(whole)}}, nil},
		{"different Repr-Digest", func(*options.Option) {}, http.Header{"Repr-Digest": {sha256Digest("other")}}, client.ErrChecksumMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`), header: tt.header})
			dir := t.TempDir()
			path := filepath.Join(dir, "download.bin")
			if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			opt := options.New().SetFileOutput(path).SetSegments(4)
			tt.opt(opt)

			_, err := client.Get(server.URL, opt)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got := contentOrAbsent(t, path); got != "old" {
					t.Errorf("destination = %q, want %q", got, "old")
				}
				onlyFile(t, dir, "download.bin")
			}
		})
	}
}

// TestSegmentedDownloadAfterRedirect checks that segments go to the address
// the first request was redirected to, with the headers net/http sent there,
// so credentials removed for another host are not sent to it.
func TestSegmentedDownloadAfterRedirect(t *testing.T) {
	var mu sync.Mutex
	var authorization []string
	content := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorization = append(authorization, r.Header.Get("Authorization"))
		mu.Unlock()
		content.Config.Handler.ServeHTTP(w, r)
	}))
	defer destination.Close()
	// The same server addressed by another host name counts as a different host.
	other := strings.Replace(destination.URL, "127.0.0.1", "localhost", 1)
	var originRequests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		http.Redirect(w, r, other, http.StatusFound)
	}))
	defer origin.Close()
	path := filepath.Join(t.TempDir(), "download.bin")

	opt := options.New().SetFileOutput(path).SetSegments(4).EnableRedirects().AddHeader("Authorization", "Bearer secret")
	if _, err := client.Get(origin.URL, opt); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := originRequests.Load(); got != 1 {
		t.Errorf("origin received %d requests, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(authorization) != 4 {
		t.Errorf("destination received %d requests, want 4", len(authorization))
	}
	for _, a := range authorization {
		if a != "" {
			t.Errorf("destination received Authorization %q, want none", a)
		}
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
		t.Errorf("file does not match the content served (error %v)", err)
	}
}

// TestSegmentedDownloadIgnoresResume checks that a resumed download is not
// split into segments.
func TestSegmentedDownloadIgnoresResume(t *testing.T) {
	server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
	path := filepath.Join(t.TempDir(), "download.bin")

	if _, err := client.Get(server.URL, options.New().Resume(path, nil).SetSegments(4)); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := server.requests(); len(got) != 1 || got[0] != "" {
		t.Errorf("Range headers = %q, want one request with no range", got)
	}
}

// TestSegmentedDownloadDateValidatorChanged checks that a download whose first
// response has a date validator fails when a later segment comes from a
// version with another Last-Modified, even from a server that ignores If-Range.
func TestSegmentedDownloadDateValidatorChanged(t *testing.T) {
	older := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	changed := bytes.Repeat([]byte("changed "), largefile.Len()/8)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignoring If-Range sends a range of whichever version is current.
		r.Header.Del("If-Range")
		content, modified := largefile.Bytes(), older
		if requests.Add(1) > 1 {
			content, modified = changed, older.Add(time.Hour)
		}
		http.ServeContent(w, r, "", modified, bytes.NewReader(content))
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4))
	if !errors.Is(err, client.ErrRangeMismatch) {
		t.Fatalf("Get() error = %v, want %v", err, client.ErrRangeMismatch)
	}
	if got := contentOrAbsent(t, path); got != "old" {
		t.Errorf("destination = %q, want %q", got, "old")
	}
	onlyFile(t, dir, "download.bin")
}

// decodedTransport marks responses as decoded by the transport, as a transport
// that decompresses bodies does, when decoded reports true for the request.
type decodedTransport struct {
	decoded func(r *http.Request) bool
}

func (d decodedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil && d.decoded(r) {
		resp.Uncompressed = true
	}
	return resp, err
}

// TestSegmentedDownloadTransportDecoded checks that decoded bytes are never
// accepted as byte ranges of the file: a decoded first response is downloaded
// again in one request, and a decoded later segment fails the download.
func TestSegmentedDownloadTransportDecoded(t *testing.T) {
	tests := []struct {
		name    string
		decoded func(r *http.Request) bool
		wantErr error
	}{
		{"first response", func(r *http.Request) bool { return strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") }, nil},
		{"later segment", func(r *http.Request) bool {
			return r.Header.Get("Range") != "" && !strings.HasPrefix(r.Header.Get("Range"), "bytes=0-")
		}, client.ErrRangeMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
			c := client.NewCustom(&http.Client{Transport: decodedTransport{decoded: tt.decoded}})
			dir := t.TempDir()
			path := filepath.Join(dir, "download.bin")
			if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := c.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got := contentOrAbsent(t, path); got != "old" {
					t.Errorf("destination = %q, want %q", got, "old")
				}
				onlyFile(t, dir, "download.bin")
				return
			}
			if got := server.requests(); len(got) != 2 || got[1] != "" {
				t.Errorf("Range headers = %q, want a first request and one request with no range", got)
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
				t.Errorf("file does not match the content served (error %v)", err)
			}
		})
	}
}
