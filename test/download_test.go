package client_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
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
