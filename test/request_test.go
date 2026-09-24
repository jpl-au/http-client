package client_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
