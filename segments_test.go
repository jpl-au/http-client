package client_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
	cookies     []string // Cookie header of each request, in order.
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
		s.cookies = append(s.cookies, r.Header.Get("Cookie"))
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

func TestOption_SetSegments(t *testing.T) {
	// A file with a strong validator is fetched in four segments that run at once,
	// and the response and progress describe the whole file.
	t.Run("downloads in segments", func(t *testing.T) {
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
	})

	// A server that ignores Range sends the whole file in the first response.
	t.Run("no range support", func(t *testing.T) {
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
	})

	// A file with no strong validator is downloaded whole, because its
	// segments could come from different versions.
	t.Run("no validator", func(t *testing.T) {
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
	})

	// A download fails when the file changes on the server between segments.
	t.Run("file changes between segments", func(t *testing.T) {
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
	})

	// A failed segment fails the download and leaves the destination and its
	// directory as they were.
	t.Run("segment fails", func(t *testing.T) {
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
	})

	// A file too small to split is not split into segments.
	t.Run("small file", func(t *testing.T) {
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
	})

	// SetChecksum and Repr-Digest are checked against the whole file, and a
	// mismatch leaves the destination as it was.
	t.Run("checksums", func(t *testing.T) {
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
	})

	// Segments go to the address the first request was redirected to, with the
	// headers net/http sent there, so credentials removed for another host are
	// not sent to it.
	t.Run("after redirect", func(t *testing.T) {
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
		if got := originRequests.Load(); got != 4 {
			t.Errorf("origin received %d requests, want 4, because each segment follows the redirect", got)
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
	})

	// A resumed download is not split into segments.
	t.Run("resume is not split", func(t *testing.T) {
		server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
		path := filepath.Join(t.TempDir(), "download.bin")

		if _, err := client.Get(server.URL, options.New().Resume(path, nil).SetSegments(4)); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got := server.requests(); len(got) != 1 || got[0] != "" {
			t.Errorf("Range headers = %q, want one request with no range", got)
		}
	})

	// A request with a body is not split, because its body can be sent only
	// once.
	t.Run("request with body is not split", func(t *testing.T) {
		server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: same(`"v1"`)})
		path := filepath.Join(t.TempDir(), "download.bin")

		if _, err := client.Custom(http.MethodGet, server.URL, "payload", options.New().SetFileOutput(path).SetSegments(4)); err != nil {
			t.Fatalf("Custom() error = %v", err)
		}
		if got := server.requests(); len(got) != 1 || got[0] != "" {
			t.Errorf("Range headers = %q, want one request with no range", got)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
			t.Errorf("file does not match the content served (error %v)", err)
		}
	})

	// A body set with PrepareFile is a body too, even though the method gets
	// no payload.
	t.Run("request with prepared file is not split", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "payload.txt")
		if err := os.WriteFile(input, []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var bodies []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("reading request body: %v", err)
			}
			mu.Lock()
			bodies = append(bodies, string(body))
			mu.Unlock()
			w.Header().Set("ETag", `"v1"`)
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(largefile.Bytes()))
		}))
		t.Cleanup(server.Close)
		path := filepath.Join(dir, "download.bin")

		opt := options.New().SetFileOutput(path).SetSegments(2)
		if err := opt.PrepareFile(input); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Get(server.URL, opt); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(bodies) != 1 || bodies[0] != "payload" {
			t.Errorf("request bodies = %q, want one request with body %q", bodies, "payload")
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, largefile.Bytes()) {
			t.Errorf("file does not match the content served (error %v)", err)
		}
	})

	// A download whose first response has a date validator fails when a later
	// segment comes from a version with another Last-Modified, even from a
	// server that ignores If-Range.
	t.Run("date validator changes", func(t *testing.T) {
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
	})

	// Decoded bytes are never accepted as byte ranges of the file: a decoded
	// first response is downloaded again in one request, and a decoded later
	// segment fails the download.
	t.Run("transport decoded", func(t *testing.T) {
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
	})

	// The request for the whole file, which follows a first response that
	// cannot be split, fails when the server answers it with a range instead
	// of the whole file.
	t.Run("whole file request gets range", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No validator, so the first response cannot be split, and every
			// response is the same range of four bytes.
			requests.Add(1)
			w.Header().Set("Content-Range", "bytes 0-3/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("abcd"))
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
		if got := requests.Load(); got != 2 {
			t.Errorf("server received %d requests, want 2", got)
		}
		if got := contentOrAbsent(t, path); got != "old" {
			t.Errorf("destination = %q, want %q", got, "old")
		}
		onlyFile(t, dir, "download.bin")
	})

	// Each request of a segmented download sends the cookie jar's cookies
	// once, including the request for the whole file when the first response
	// cannot be split.
	t.Run("jar cookies sent once", func(t *testing.T) {
		tests := []struct {
			name     string
			etag     func(int) string
			requests int
		}{
			{"segments", same(`"v1"`), 4},
			{"whole-file request", same(""), 2},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := newSegmentServer(t, segmentConfig{content: same(largefile.Bytes()), etag: tt.etag})
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				u, err := url.Parse(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "abc"}})
				c := client.NewCustom(&http.Client{Jar: jar})
				path := filepath.Join(t.TempDir(), "download.bin")

				if _, err := c.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4)); err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				server.mu.Lock()
				cookies := slices.Clone(server.cookies)
				server.mu.Unlock()
				if len(cookies) != tt.requests {
					t.Errorf("server received %d requests, want %d", len(cookies), tt.requests)
				}
				for i, cookie := range cookies {
					if cookie != "session=abc" {
						t.Errorf("request %d sent Cookie %q, want %q", i+1, cookie, "session=abc")
					}
				}
			})
		}
	})

	// When the first response cannot be split and the request for the whole
	// file then fails, the response still records the first response.
	t.Run("whole file request fails", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) == 1 {
				w.Header().Set("X-First", "yes")
				w.Header().Set("Content-Range", "bytes */0")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			// Close the connection without a response, so the request fails.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
		}))
		defer server.Close()
		path := filepath.Join(t.TempDir(), "download.bin")

		resp, err := client.Get(server.URL, options.New().SetFileOutput(path).SetSegments(4))
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusRequestedRangeNotSatisfiable)
		}
		if got := resp.Header.Get("X-First"); got != "yes" {
			t.Errorf("Header.Get(%q) = %q, want %q", "X-First", got, "yes")
		}
	})

	// When a redirect policy stops the request for the whole file, the response
	// records the redirect the client received, as it does for any other
	// request.
	t.Run("whole file redirect rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Header.Get("Range") != "":
				w.Header().Set("Content-Range", "bytes */0")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			case r.URL.Path == "/":
				http.Redirect(w, r, "/a", http.StatusFound)
			case r.URL.Path == "/a":
				w.Header().Set("X-Hop", "a")
				http.Redirect(w, r, "/b", http.StatusFound)
			default:
				_, _ = w.Write([]byte("content"))
			}
		}))
		defer server.Close()
		path := filepath.Join(t.TempDir(), "download.bin")

		opt := options.New().SetFileOutput(path).SetSegments(4).EnableRedirects().SetMaxRedirects(1)
		resp, err := client.Get(server.URL, opt)
		if !errors.Is(err, client.ErrMaxRedirectsExceeded) {
			t.Fatalf("Get() error = %v, want %v", err, client.ErrMaxRedirectsExceeded)
		}
		if resp.StatusCode != http.StatusFound {
			t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusFound)
		}
		if got := resp.Header.Get("X-Hop"); got != "a" {
			t.Errorf("Header.Get(%q) = %q, want %q", "X-Hop", got, "a")
		}
	})
}
