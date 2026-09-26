package client_test

import (
	"math"
	"net/http"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
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
		if err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if lastProgress != 100 {
			t.Errorf("last progress = %v, want %v", lastProgress, 100.0)
		}
		if math.Abs(100.0-lastProgress) > 0.1 {
			t.Fatalf("last progress = %v, want %v within %v", lastProgress, 100.0, 0.1)
		}
	})

	t.Run("Upload with Redirect", func(t *testing.T) {
		var lastProgress float64
		progressCalls := 0

		opt := options.New().Redirects(true, 5)
		opt.AddHeader("X-DATA", "upload/redirect")

		opt.Progress.OnUpload = func(current, total int64) {
			t.Logf("Uploaded %d bytes", current)
			lastProgress = float64(current) / float64(total) * 100
			t.Logf("Uploaded: %f", lastProgress)
			progressCalls++
			t.Logf("Progress calls: %d", progressCalls)
		}

		resp, err := client.Post(server.URL+"/upload/redirect", smallfile.Bytes(), opt)
		if err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if lastProgress != 100 {
			t.Errorf("last progress = %v, want %v", lastProgress, 100.0)
		}
		if progressCalls <= 0 {
			t.Fatalf("progress calls = %d, want > 0", progressCalls)
		}
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
		if err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if lastProgress != 100 {
			t.Errorf("last progress = %v, want %v", lastProgress, 100.0)
		}
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
		if err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if want := int64(smallfile.Len()); resp.Len() != want {
			t.Errorf("Len() = %d, want %d", resp.Len(), want)
		}
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
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if resp.Len() != largeLen {
			t.Fatalf("Len() = %d, want %d", resp.Len(), largeLen)
		}
		if lastProgress != 100 {
			t.Errorf("last progress = %v, want %v", lastProgress, 100.0)
		}
		if progressCalls <= 0 {
			t.Fatalf("progress calls = %d, want > 0", progressCalls)
		}
	})
}
