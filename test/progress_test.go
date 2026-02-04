package client_test

import (
	"net/http"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressTracking(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	largeLen := int64(largefile.Len())

	t.Run("Upload with Progress", func(t *testing.T) {
		var lastProgress float64

		opt := options.New()
		opt.Progress.OnUpload = func(current, total int64) {
			lastProgress = float64(current) / float64(total) * 100
		}

		resp, err := client.Post(server.URL+"/upload", smallfile.Bytes(), opt)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, float64(100.00), lastProgress)
		require.InDelta(t, 100.0, lastProgress, 0.1)
	})

	t.Run("Upload with Redirect", func(t *testing.T) {
		var lastProgress float64
		progressCalls := 0

		opt := options.New().Redirects(true, true, 5)
		opt.AddHeader("X-DATA", "upload/redirect")

		opt.Progress.OnUpload = func(current, total int64) {
			t.Logf("Uploaded %d bytes", current)
			lastProgress = float64(current) / float64(total) * 100
			t.Logf("Uploaded: %f", lastProgress)
			progressCalls++
			t.Logf("Progress calls: %d", progressCalls)
		}

		resp, err := client.Post(server.URL+"/upload/redirect", smallfile.Bytes(), opt)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, float64(100.00), lastProgress)
		require.Greater(t, progressCalls, 0)
	})

	t.Run("Upload with Compression - Track Before Compression", func(t *testing.T) {
		var lastProgress float64

		opt := options.New()
		opt.SetCompression(options.CompressionGzip)

		opt.Progress.OnUpload = func(current, total int64) {
			lastProgress = float64(current) / float64(total) * 100
			t.Logf("Internal buffer read upload progress: %f", lastProgress)
		}

		resp, err := client.Post(server.URL+"/upload", smallfile.Bytes(), opt)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, float64(100.00), lastProgress)
	})

	t.Run("Upload with Compression | Track After Compression", func(t *testing.T) {
		var lastProgress int64

		opt := options.New()
		opt.SetCompression(options.CompressionGzip).TrackAfterCompression()
		opt.Progress.OnUpload = func(current, total int64) {
			lastProgress = current
		}

		resp, err := client.Post(server.URL+"/upload", smallfile.Bytes(), opt)
		t.Logf("Total bytes sent (compressed bytes): %d", lastProgress)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, int64(smallfile.Len()), resp.Len())
	})

	t.Run("Download with Progress", func(t *testing.T) {
		var lastProgress float64
		progressCalls := 0

		opt := options.New()
		opt.Progress.OnDownload = func(current, total int64) {
			lastProgress = float64(current) / float64(total) * 100
			progressCalls++
		}

		resp, err := client.Get(server.URL+"/download", opt)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, largeLen, resp.Len())
		assert.Equal(t, float64(100.00), lastProgress)
		require.Greater(t, progressCalls, 0)
	})
}
