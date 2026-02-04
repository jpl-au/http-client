package client_test

import (
	"net/http"
	"os"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
	"github.com/stretchr/testify/assert"
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
