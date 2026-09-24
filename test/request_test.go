package client_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			if err != nil {
				t.Logf("err: %s", err)
			}
		})
	}
}

func TestPostFileUpload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}
	defer tmpfile.Close()

	var lastProgress int64
	opt := options.New()
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress = (bytesRead * 100) / totalBytes
		}
	}

	resp, err := client.Post(server.URL+"/upload", tmpfile, opt)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int64(100), lastProgress)
	assert.Equal(t, smallfile.Bytes(), resp.Body.Bytes())
}

func TestPostStringUpload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	var lastProgress float64
	opt := options.New()
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress = float64(bytesRead) / float64(totalBytes) * 100
		}
	}

	resp, err := client.Post(server.URL+"/upload", smallfile.String(), opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress)

	assert.Equal(t, smallfile.Bytes(), resp.Body.Bytes())
}

func TestPostByteUpload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	var lastProgress float64
	opt := options.New()
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress = float64(bytesRead) / float64(totalBytes) * 100
		}
	}

	resp, err := client.Post(server.URL+"/upload", smallfile.Bytes(), opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress)
	assert.Equal(t, smallfile.Bytes(), resp.Body.Bytes())
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

			assert.NoError(t, err)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, float64(100), lastProgress)
			assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
		})
	}
}

func TestCustomHeaders(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	opt := options.New()
	opt.AddHeader("X-Custom-Header", "test-value")

	resp, err := client.Get(server.URL+"/echo-headers", opt)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "test-value", resp.Header.Get("Echo-X-Custom-Header"))
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
	require.Error(t, err)

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
	require.NoError(t, err)

	assert.NotEmpty(t, traceID)
	assert.Equal(t, traceID, resp.UniqueIdentifier, "the trace header and the response should share one identifier")
	assert.Equal(t, int64(3), resp.ContentLength)
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
			require.Error(t, err)
			assert.Equal(t, err, resp.Error, "the response should record the returned error")
			assert.Equal(t, http.StatusOK, resp.StatusCode, "the response should record the received status")
		})
	}

	t.Run("invalid URL", func(t *testing.T) {
		resp, err := client.Get("http://[::1")
		require.Error(t, err)
		assert.Equal(t, err, resp.Error, "the response should record the returned error")
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
	require.NoError(t, err)

	out := logs.String()
	assert.Contains(t, out, "/path", "the log should still name the request")
	assert.Contains(t, out, "Authorization", "the log should still list header names")
	for _, secret := range []string{"secret-token", "secret-cookie", "secret-password", "secret-query"} {
		assert.NotContains(t, out, secret)
	}
}

func TestUploadEmptyFile(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "empty.txt")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	resp, err := client.PostFile(server.URL, path)
	require.NoError(t, err, "an empty file is a valid upload")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, received)
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
		require.NoError(t, err)
		assert.Equal(t, m, method)
		assert.Equal(t, "payload", body, "the payload of a %s request should be sent", m)
	}
}
