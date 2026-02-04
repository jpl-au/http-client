package client_test

import (
	"errors"
	"net/http"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientFormDataMethods tests the Client struct's FormData methods
func TestClientFormDataMethods(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	tests := []struct {
		name   string
		method string
	}{
		{"PostFormData", "POST"},
		{"PutFormData", "PUT"},
		{"PatchFormData", "PATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]string{
				"field1": "value1",
				"field2": "value2",
			}

			var resp response.Response
			var err error

			switch tt.method {
			case "POST":
				resp, err = c.PostFormData(server.URL+"/echo-headers", payload)
			case "PUT":
				resp, err = c.PutFormData(server.URL+"/echo-headers", payload)
			case "PATCH":
				resp, err = c.PatchFormData(server.URL+"/echo-headers", payload)
			}

			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify the Content-Type header was set correctly (echoed back by server)
			assert.Equal(t, "application/x-www-form-urlencoded", resp.Header.Get("Echo-Content-Type"))
		})
	}
}

// TestClientFormDataWithOptions tests that Client FormData methods properly handle options
func TestClientFormDataWithOptions(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	// Test with custom options
	opt := options.New()
	opt.AddHeader("X-Custom-Header", "test-value")

	payload := map[string]string{"key": "value"}

	resp, err := c.PostFormData(server.URL+"/echo", payload, opt)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestClientFileMethods tests the Client struct's file upload methods
func TestClientFileMethods(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	tests := []struct {
		name   string
		method string
	}{
		{"PostFile", "POST"},
		{"PutFile", "PUT"},
		{"PatchFile", "PATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp response.Response
			var err error

			switch tt.method {
			case "POST":
				resp, err = c.PostFile(server.URL+"/upload", smallf)
			case "PUT":
				resp, err = c.PutFile(server.URL+"/upload", smallf)
			case "PATCH":
				resp, err = c.PatchFile(server.URL+"/upload", smallf)
			}

			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

// TestClientFileMethodsContentType verifies that Client file methods set Content-Type via PrepareFile
func TestClientFileMethodsContentType(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	// The PrepareFile method should infer content-type and set Content-Disposition
	resp, err := c.PostFile(server.URL+"/echo-headers", smallf)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Check that Content-Disposition header was set (PrepareFile sets this)
	contentDisposition := resp.Header.Get("Echo-Content-Disposition")
	assert.Contains(t, contentDisposition, "form-data")
	assert.Contains(t, contentDisposition, smallf)

	// Check that Content-Type was inferred
	contentType := resp.Header.Get("Echo-Content-Type")
	assert.NotEmpty(t, contentType, "Content-Type should be set by PrepareFile")
}

// TestClientFileMethodsNonExistent tests error handling for non-existent files
func TestClientFileMethodsNonExistent(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	_, err := c.PostFile(server.URL+"/upload", "nonexistent-file.txt")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, options.ErrFileNotFound), "expected ErrFileNotFound, got: %v", err)
}
