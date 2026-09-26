package client_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

func TestMultipartUpload(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{"PostMultipartUpload", http.MethodPost, http.StatusOK},
		{"PutMultipartUpload", http.MethodPut, http.StatusOK},
		{"PatchMultipartUpload", http.MethodPatch, http.StatusOK},
	}

	url := server.URL + "/upload/multipart"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lastProgress float64
			opt := options.New()
			opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
				if totalBytes > 0 {
					lastProgress = float64(bytesRead) / float64(totalBytes) * 100
				}
			}

			s, err := os.Open(smallf)
			if err != nil {
				t.Fatalf("unable to open %s: %s", smallf, err)
			}
			l, err := os.Open(largef)
			if err != nil {
				t.Fatalf("unable to open %s: %s", largef, err)
			}

			payload := map[string]any{
				smallf: s,
				largef: l,
			}

			var resp response.Response

			switch tt.method {
			case http.MethodPost:
				resp, err = client.PostMultipartUpload(url, payload, opt)
			case http.MethodPut:
				resp, err = client.PutMultipartUpload(url, payload, opt)
			case http.MethodPatch:
				resp, err = client.PatchMultipartUpload(url, payload, opt)
			}

			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.expectedStatus)
			}
			if lastProgress != 100 {
				t.Errorf("last progress = %v, want %v", lastProgress, 100.0)
			}

			// Parse the JSON response
			var fileInfo map[string]int64
			err = json.Unmarshal(resp.Body.Bytes(), &fileInfo)
			if err != nil {
				t.Errorf("json.Unmarshal() error = %v", err)
			}

			// Check file sizes
			if got, want := fileInfo[filepath.Base(smallf)], int64(smallfile.Len()); got != want {
				t.Errorf("size of %q = %d, want %d", smallf, got, want)
			}
			if got, want := fileInfo[filepath.Base(largef)], int64(largefile.Len()); got != want {
				t.Errorf("size of %q = %d, want %d", largef, got, want)
			}
		})
	}
}

// TestMultipartUploadRecordsFormErrors checks that a form that cannot be built
// fails like any other request: the response records the error.
func TestMultipartUploadRecordsFormErrors(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	file, err := os.Open(smallf)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", smallf, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	resp, err := client.PostMultipartUpload(server.URL+"/upload/multipart", map[string]any{"file": file})
	if err == nil {
		t.Fatal("PostMultipartUpload() error = nil, want error: a closed file cannot be read into the form")
	}
	if resp.Error != err {
		t.Errorf("Response.Error = %v, want %v", resp.Error, err)
	}
	if resp.UniqueIdentifier == "" {
		t.Errorf("UniqueIdentifier = %q, want non-empty", resp.UniqueIdentifier)
	}
}
