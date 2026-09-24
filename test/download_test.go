package client_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileDownload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tmpDir, err := os.MkdirTemp("", "download-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	downloadPath := filepath.Join(tmpDir, downloadf)

	var lastProgress float64
	opt := options.New()
	opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress = float64(bytesRead) / float64(totalBytes) * 100
		}
	}
	opt.SetFileOutput(downloadPath)

	resp, err := client.Get(server.URL+"/download", opt)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, float64(100), lastProgress)

	info, err := os.Stat(downloadPath)
	assert.NoError(t, err)
	assert.Equal(t, int64(largefile.Len()), info.Size())
}

func TestFileDownloadDirectToFile(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	var lastProgress float64
	opt := options.New()
	opt.SetFileOutput(downloadf)

	opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
		if totalBytes > 0 {
			lastProgress = float64(bytesRead) / float64(totalBytes) * 100
		}
	}

	resp, err := client.Get(server.URL+"/download", opt)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, float64(100), lastProgress)

	info, err := os.Stat(downloadf)
	assert.NoError(t, err)
	assert.Equal(t, int64(largefile.Len()), info.Size())
}

func TestBufferSizes(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name         string
		bufferSize   int
		expectedSize int64
	}{
		{"Small Buffer", 1024, int64(largefile.Len())},
		{"Large Buffer", 32768, int64(largefile.Len())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := options.New()
			opt.SetDownloadBufferSize(tt.bufferSize)

			start := time.Now()
			resp, err := client.Get(server.URL+"/download", opt)
			duration := time.Since(start)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedSize, resp.Len())

			t.Logf("Download with %d buffer took %v", tt.bufferSize, duration)
		})
	}
}

// writeFile creates path with content and fails the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the content of path and fails the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBodylessResponsesLeaveFileOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("status"))
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(code)
	}))
	defer server.Close()

	tests := []struct {
		name   string
		method string
		status int
	}{
		{"HEAD 200", http.MethodHead, http.StatusOK},
		{"GET 204", http.MethodGet, http.StatusNoContent},
		{"GET 304", http.MethodGet, http.StatusNotModified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.bin")
			writeFile(t, path, "existing")

			url := fmt.Sprintf("%s?status=%d", server.URL, tt.status)
			resp, err := client.Custom(tt.method, url, nil, options.New().SetFileOutput(path))
			require.NoError(t, err)

			assert.Equal(t, tt.status, resp.StatusCode, "metadata should be kept")
			assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), "metadata should be kept")
			assert.Equal(t, "existing", readFile(t, path), "a response without a body must not change the file")
		})
	}
}

// dirEntries returns the names of the files in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestFileOutputKeepsDestinationOnErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	writeFile(t, path, "valid")

	resp, err := client.Get(server.URL, options.New().SetFileOutput(path))
	require.NoError(t, err, "an error status is a response, not a transport failure")

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "server error\n", resp.String(), "the error body should be in the response")
	assert.Equal(t, "valid", readFile(t, path), "an error status must not change the file")
	assert.Equal(t, []string{"download.bin"}, dirEntries(t, dir))
}

func TestFileOutputKeepsDestinationOnTruncatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 20\r\n\r\nbad"))
		conn.Close()
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	writeFile(t, path, "valid")

	_, err := client.Get(server.URL, options.New().SetFileOutput(path))
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)

	assert.Equal(t, "valid", readFile(t, path), "a truncated body must not replace the file")
	assert.Equal(t, []string{"download.bin"}, dirEntries(t, dir), "the temporary file should be removed")
}

func TestFileOutputReplacesDestinationOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new content"))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	writeFile(t, path, "old")

	_, err := client.Get(server.URL, options.New().SetFileOutput(path))
	require.NoError(t, err)

	assert.Equal(t, "new content", readFile(t, path))
	assert.Equal(t, []string{"download.bin"}, dirEntries(t, dir), "the temporary file should be renamed into place")
}

func TestResumeKeepsPartialFileOnErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "download.bin")
	writePartial(t, path, "abc")

	resp, err := client.Get(server.URL, options.New().Resume(path, `"v1"`))
	require.NoError(t, err)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "abc", readFile(t, options.PartialPath(path)), "an error body must not be appended")
	assert.Equal(t, absent, contentOrAbsent(t, path), "an error body must not be published")
}
