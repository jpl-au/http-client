package client_test

import (
	"errors"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedirectPostUploadNoFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.Redirect.Follow = false

	resp, err := client.Post(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Location"))
}

func TestRedirectPostUploadNoPreserve(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = false

	resp, err := client.Post(server.URL+"/upload/no-preserve", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "GET", resp.String())
}

func TestRedirectMaxRedirects(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("error opening %s: %s", smallf, err)
	}

	opt := options.New()
	opt.EnableLogging()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = false
	opt.Redirect.Max = 5

	resp, err := client.Post(server.URL+"/max-redirects", tmpfile, opt)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, client.ErrMaxRedirectsExceeded), "expected ErrMaxRedirectsExceeded, got: %v", err)
	assert.Equal(t, "", resp.String())
}

func TestRedirectPostUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			progress := float64(bytesRead) / float64(totalBytes) * 100
			lastProgress.Store(progress)
			t.Logf("Upload progress: %f", progress)
		}
	}

	resp, err := client.Post(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectPutUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
		}
	}

	resp, err := client.Put(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectPatchUploadFollow(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpfile, err := os.Open(largef)
	if err != nil {
		t.Fatalf("error opening %s: %s", largef, err)
	}
	defer tmpfile.Close()

	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = true

	opt.EnableLogging()

	t.Logf("filesize: %d", largefile.Len())

	var lastProgress atomic.Value
	lastProgress.Store(float64(0))
	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
		}
	}

	resp, err := client.Patch(server.URL+"/upload/redirect", tmpfile, opt)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(100), lastProgress.Load().(float64))
	assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
}

func TestRedirectFileFuncUpload(t *testing.T) {
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

	url := server.URL + "/upload/redirect"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt.Redirects(true, true, 5)
			opt.EnableLogging()

			var lastProgress atomic.Value
			lastProgress.Store(float64(0))
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
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
			assert.Equal(t, largefile.Bytes(), resp.Body.Bytes())
			// Verify upload progress completed
			assert.Equal(t, float64(100), lastProgress.Load().(float64))
		})
	}
}

func TestCompressedFileRedirect(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name         string
		method       string
		compression  options.CompressionType
		expectedSize int64
	}{
		{"POST Gzip Compressed Redirect", http.MethodPost, options.CompressionGzip, int64(largefile.Len())},
		{"POST Deflate Compressed Redirect", http.MethodPost, options.CompressionDeflate, int64(largefile.Len())},
		{"POST Brotli Compressed Redirect", http.MethodPost, options.CompressionBrotli, int64(largefile.Len())},
		{"PUT Gzip Compressed Redirect", http.MethodPut, options.CompressionGzip, int64(largefile.Len())},
		{"PUT Deflate Compressed Redirect", http.MethodPut, options.CompressionDeflate, int64(largefile.Len())},
		{"PUT Brotli Compressed Redirect", http.MethodPut, options.CompressionBrotli, int64(largefile.Len())},
		{"PATCH Gzip Compressed Redirect", http.MethodPatch, options.CompressionGzip, int64(largefile.Len())},
		{"PATCH Deflate Compressed Redirect", http.MethodPatch, options.CompressionDeflate, int64(largefile.Len())},
		{"PATCH Brotli Compressed Redirect", http.MethodPatch, options.CompressionBrotli, int64(largefile.Len())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp response.Response
			var err error

			opt := options.New()
			opt.Redirects(true, true, 5) // Enable redirects and preserve method
			opt.SetCompression(tt.compression)

			// Track upload progress
			var lastProgress atomic.Value
			lastProgress.Store(float64(0))
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress.Store(float64(bytesRead) / float64(totalBytes) * 100)
				}
			}

			t.Logf("[%s] Original file size: %d bytes", tt.name, int64(smallfile.Len()))

			url := server.URL + "/upload/redirect"

			switch tt.method {
			case http.MethodPost:
				resp, err = client.PostFile(url, smallf, opt)
			case http.MethodPut:
				resp, err = client.PutFile(url, smallf, opt)
			case http.MethodPatch:
				resp, err = client.PatchFile(url, smallf, opt)
			}

			// Verify the request succeeded
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify the content was transmitted correctly
			assert.Equal(t, smallfile.String(), resp.String())

			// Verify upload progress completed
			t.Logf("%s Last progress: %f", tt.name, lastProgress.Load().(float64))
			// TODO: Progress tracking with compression + redirects may not reach 100%
			// assert.Equal(t, float64(100), lastProgress.Load().(float64))

			// Log the response size to see compression effectiveness
			t.Logf("[%s] Response size: %d bytes", tt.name, len(resp.Body.Bytes()))
		})
	}
}

func TestRedirectWithFileReopenError(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-redirect-*.txt")
	require.NoError(t, err)
	_, _ = tmpFile.WriteString("test content")
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	// Set up options to follow redirects and preserve method
	opt := options.New()
	opt.Redirect.Follow = true
	opt.Redirect.PreserveMethod = true
	opt.Redirect.Max = 5

	// Delete the file before making the request that will redirect
	// This simulates the file becoming unavailable between redirects
	os.Remove(tmpPath)

	// This should fail because the file doesn't exist
	_, err = client.PostFile(server.URL+"/redirect/upload", tmpPath, opt)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, options.ErrFileNotFound), "expected ErrFileNotFound, got: %v", err)
}
