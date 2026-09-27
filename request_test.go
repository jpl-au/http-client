package client_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

func TestBasicRequests(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"GET Request", http.MethodGet, "/", http.StatusOK},
		{"POST Request", http.MethodPost, "/", http.StatusOK},
		{"PUT Request", http.MethodPut, "/", http.StatusOK},
		{"DELETE Request", http.MethodDelete, "/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Custom(tt.method, server.URL+tt.path, nil)
			if err != nil {
				t.Errorf("Custom() error = %v", err)
			}
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.expectedStatus)
			}
			if err != nil {
				t.Logf("err: %s", err)
			}
		})
	}
}

func TestPostUpload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name    string
		payload func(t *testing.T) any
	}{
		{"File", func(t *testing.T) any {
			file, err := os.Open(smallf)
			if err != nil {
				t.Fatalf("Open(%q) error = %v", smallf, err)
			}
			t.Cleanup(func() { file.Close() })
			return file
		}},
		{"String", func(t *testing.T) any { return smallfile.String() }},
		{"Bytes", func(t *testing.T) any { return smallfile.Bytes() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lastProgress float64
			opt := options.New()
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress = float64(bytesRead) / float64(totalBytes) * 100
				}
			}

			resp, err := client.Post(server.URL+"/upload", tt.payload(t), opt)
			if err != nil {
				t.Fatalf("Post() error = %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if lastProgress != 100 {
				t.Errorf("upload progress = %v, want 100", lastProgress)
			}
			if !bytes.Equal(resp.Body.Bytes(), smallfile.Bytes()) {
				t.Errorf("body does not match: got %d bytes, want %d bytes", resp.Body.Len(), smallfile.Len())
			}
		})
	}
}

func TestFileFuncUpload(t *testing.T) {
	var err error
	var resp response.Response

	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{"PostFile Request", http.MethodPost, http.StatusOK},
		{"PutFile Request", http.MethodPut, http.StatusOK},
		{"PatchFile Request", http.MethodPatch, http.StatusOK},
	}

	url := server.URL + "/upload"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Track upload progress
			var lastProgress float64
			opt := options.New()
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress = float64(bytesRead) / float64(totalBytes) * 100
				}
			}

			switch tt.method {
			case http.MethodPost:
				resp, err = client.PostFile(url, largef, opt)
			case http.MethodPut:
				resp, err = client.PutFile(url, largef, opt)
			case http.MethodPatch:
				resp, err = client.PatchFile(url, largef, opt)
			}

			if err != nil {
				t.Errorf("upload error = %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if lastProgress != 100 {
				t.Errorf("upload progress = %v, want 100", lastProgress)
			}
			if !bytes.Equal(resp.Body.Bytes(), largefile.Bytes()) {
				t.Errorf("body does not match: got %d bytes, want %d bytes", resp.Body.Len(), largefile.Len())
			}
		})
	}
}

func TestCustomHeaders(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	opt := options.New()
	opt.AddHeader("X-Custom-Header", "test-value")

	resp, err := client.Get(server.URL+"/echo-headers", opt)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got, want := resp.Header.Get("Echo-X-Custom-Header"), "test-value"; got != want {
		t.Errorf("Header.Get(%q) = %q, want %q", "Echo-X-Custom-Header", got, want)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// blockingCompressor passes writes to the pipe and reports when the first write starts
// and when the compressor is closed.
type blockingCompressor struct {
	w       io.Writer
	writing chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *blockingCompressor) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.writing) })
	return c.w.Write(p)
}

func (c *blockingCompressor) Close() error {
	close(c.closed)
	return nil
}

// TestFailedUploadReleasesCompressor checks that a failed request closes the
// compression pipe even when upload progress wraps it.
func TestFailedUploadReleasesCompressor(t *testing.T) {
	compressor := &blockingCompressor{writing: make(chan struct{}), closed: make(chan struct{})}

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-compressor.writing
		r.Body.Close()
		return nil, errors.New("synthetic transport failure")
	})

	opt := options.New().
		SetClient(&http.Client{Transport: transport}).
		SetCompression(options.CompressionCustom).
		TrackAfterCompression().
		OnUploadProgress(func(int64, int64) {})
	opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
		compressor.w = w
		return compressor, nil
	}

	_, err := client.Post("http://example.invalid/upload", "payload", opt)
	if err == nil {
		t.Fatal("Post() error = nil, want error")
	}

	select {
	case <-compressor.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the compressor is still blocked on the pipe after the request failed")
	}
}

func TestResponseMetadataMatchesRequest(t *testing.T) {
	var traceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID = r.Header.Get("X-Trace-ID")
		w.Header().Set("Content-Length", "3")
		_, _ = w.Write([]byte("abc"))
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if traceID == "" {
		t.Error("X-Trace-ID header = \"\", want an identifier")
	}
	if resp.UniqueIdentifier != traceID {
		t.Errorf("UniqueIdentifier = %q, want %q (the X-Trace-ID header)", resp.UniqueIdentifier, traceID)
	}
	if resp.ContentLength != 3 {
		t.Errorf("ContentLength = %d, want 3", resp.ContentLength)
	}
}

func TestFailedResponseKeepsStatusAndError(t *testing.T) {
	invalidGzip := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write([]byte("not gzip"))
	}))
	defer invalidGzip.Close()

	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\nbad"))
		conn.Close()
	}))
	defer truncated.Close()

	tests := []struct {
		name string
		url  string
		opt  *options.Option
	}{
		{"invalid gzip body", invalidGzip.URL, options.New().AddHeader("Accept-Encoding", "gzip")},
		{"truncated body", truncated.URL, options.New()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(tt.url, tt.opt)
			if err == nil {
				t.Fatal("Get() error = nil, want error")
			}
			if resp.Error != err {
				t.Errorf("Response.Error = %v, want the returned error %v", resp.Error, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d (the received status)", resp.StatusCode, http.StatusOK)
			}
		})
	}

	t.Run("invalid URL", func(t *testing.T) {
		resp, err := client.Get("http://[::1")
		if err == nil {
			t.Fatal("Get() error = nil, want error")
		}
		if resp.Error != err {
			t.Errorf("Response.Error = %v, want the returned error %v", resp.Error, err)
		}
	})
}

func TestLoggingRedactsCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	var logs bytes.Buffer
	opt := options.New().
		SetLogger(slog.New(slog.NewTextHandler(&logs, nil))).
		AddHeader("Authorization", "Bearer secret-token").
		AddCookie(&http.Cookie{Name: "session", Value: "secret-cookie"})

	url := strings.Replace(server.URL, "http://", "http://user:secret-password@", 1) + "/path?token=secret-query"
	_, err := client.Get(url, opt)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "/path") {
		t.Errorf("log = %q, want it to name the request path %q", out, "/path")
	}
	if !strings.Contains(out, "Authorization") {
		t.Errorf("log = %q, want it to list the header name %q", out, "Authorization")
	}
	for _, secret := range []string{"secret-token", "secret-cookie", "secret-password", "secret-query"} {
		if strings.Contains(out, secret) {
			t.Errorf("log = %q, want %q redacted", out, secret)
		}
	}
}

func TestUploadEmptyFile(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resp, err := client.PostFile(server.URL, path)
	if err != nil {
		t.Fatalf("PostFile() error = %v, want nil for an empty file", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(received) != 0 {
		t.Errorf("received body length = %d, want 0", len(received))
	}
}

func TestCustomMethodSendsPayload(t *testing.T) {
	var method, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		method, body = r.Method, string(data)
	}))
	defer server.Close()

	for _, m := range []string{"PROPFIND", http.MethodDelete} {
		_, err := client.Custom(m, server.URL, "payload")
		if err != nil {
			t.Fatalf("Custom(%q) error = %v", m, err)
		}
		if method != m {
			t.Errorf("method = %q, want %q", method, m)
		}
		if body != "payload" {
			t.Errorf("%s request body = %q, want %q", m, body, "payload")
		}
	}
}

func TestFailedResponseRecordsTiming(t *testing.T) {
	const stall = 100 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\nbad"))
		time.Sleep(stall)
		conn.Close()
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want error")
	}

	if resp.AccessTime < stall {
		t.Errorf("AccessTime = %v, want at least %v (the time spent reading the body)", resp.AccessTime, stall)
	}
	if resp.ProcessedTime == 0 {
		t.Error("ProcessedTime = 0, want a timestamp for a failed response")
	}
}

// TestMaxBodySize checks that a buffered body longer than the limit fails with
// ErrBodyTooLarge and leaves the response body empty.
func TestMaxBodySize(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	exact := "Hello from path: /exact"
	tests := []struct {
		name    string
		path    string
		limit   int64
		wantErr error
	}{
		{"over the limit", "/download", 1 << 20, client.ErrBodyTooLarge},
		{"at the limit", "/exact", int64(len(exact)), nil},
		{"decompressed body over the limit", "/download/compressed?compression=gzip", 1 << 20, client.ErrBodyTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(server.URL+tt.path, options.New().SetMaxBodySize(tt.limit))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if !errors.Is(resp.Error, tt.wantErr) {
				t.Errorf("Response.Error = %v, want %v", resp.Error, tt.wantErr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if tt.wantErr != nil && !resp.Body.IsEmpty() {
				t.Errorf("body has %d bytes, want none", resp.Body.Len())
			}
			if tt.wantErr == nil && resp.String() != exact {
				t.Errorf("String() = %q, want %q", resp.String(), exact)
			}
		})
	}
}

// TestMaxBodySizeLeavesFileOutput checks that the limit applies only to a body
// held in memory, not to a download written to a file.
func TestMaxBodySizeLeavesFileOutput(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "download.txt")
	_, err := client.Get(server.URL+"/download", options.New().SetMaxBodySize(1024).SetFileOutput(path))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got, want := info.Size(), int64(largefile.Len()); got != want {
		t.Errorf("file size = %d, want %d", got, want)
	}
}

// TestClientMaxBodySize checks that a Client applies a global limit, and that a
// per-request SetMaxBodySize(0) removes it.
func TestClientMaxBodySize(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New(options.New().SetMaxBodySize(1024))
	if _, err := c.Get(server.URL + "/download"); !errors.Is(err, client.ErrBodyTooLarge) {
		t.Errorf("Get() error = %v, want %v", err, client.ErrBodyTooLarge)
	}

	resp, err := c.Get(server.URL+"/download", options.New().SetMaxBodySize(0))
	if err != nil {
		t.Fatalf("Get() with SetMaxBodySize(0) error = %v", err)
	}
	if got, want := resp.Body.Len(), largefile.Len(); got != want {
		t.Errorf("body has %d bytes, want %d", got, want)
	}
}

// TestMaxBodySizeLimitsErrorBodyOfFileDownload checks that the limit applies to
// the body of an error response to a file download, which is held in memory.
func TestMaxBodySizeLimitsErrorBodyOfFileDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "download.txt")
	_, err := client.Get(server.URL, options.New().SetMaxBodySize(1024).SetFileOutput(path))
	if !errors.Is(err, client.ErrBodyTooLarge) {
		t.Errorf("Get() error = %v, want %v", err, client.ErrBodyTooLarge)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat() error = %v, want %v (no file for an error response)", err, fs.ErrNotExist)
	}
}

// TestMaxBodySizeLargestLimit checks that the largest limit does not overflow
// and cut the body short.
func TestMaxBodySizeLargestLimit(t *testing.T) {
	server := newBodyServer(t, http.StatusOK, "hello")

	resp, err := client.Get(server.URL, options.New().SetMaxBodySize(math.MaxInt64))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := resp.String(); got != "hello" {
		t.Errorf("String() = %q, want %q", got, "hello")
	}
}
