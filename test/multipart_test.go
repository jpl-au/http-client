package client_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			assert.Equal(t, float64(100), lastProgress)

			// Parse the JSON response
			var fileInfo map[string]int64
			err = json.Unmarshal(resp.Body.Bytes(), &fileInfo)
			assert.NoError(t, err)

			// Check file sizes
			assert.Equal(t, int64(smallfile.Len()), fileInfo[smallf])
			assert.Equal(t, int64(largefile.Len()), fileInfo[largef])
		})
	}
}

// TestMultipartUploadRecordsFormErrors checks that a form that cannot be built
// fails like any other request: the response records the error.
func TestMultipartUploadRecordsFormErrors(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	file, err := os.Open(smallf)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	resp, err := client.PostMultipartUpload(server.URL+"/upload/multipart", map[string]any{"file": file})
	require.Error(t, err, "a closed file cannot be read into the form")
	assert.Equal(t, err, resp.Error)
	assert.NotEmpty(t, resp.UniqueIdentifier)
}
