package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
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

// TestClientPerRequestOptionKeepsGlobalSettings checks that a per-request Option
// that does not mention redirects leaves the Client's redirect setting in place.
func TestClientPerRequestOptionKeepsGlobalSettings(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New(options.New().EnableRedirects())

	resp, err := c.Get(server.URL+"/upload/no-preserve", options.New().AddHeader("X-Request", "1"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the global EnableRedirects should still apply")

	resp, err = c.PostFormData(server.URL+"/upload/no-preserve", map[string]string{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the global EnableRedirects should apply to form posts")

	resp, err = c.Get(server.URL+"/upload/no-preserve", options.New().DisableRedirects())
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode, "a per-request DisableRedirects should override the global setting")
}

// TestClientHistoryWithoutTracing checks that turning off the trace header still
// gives each response its own identifier in the history.
func TestClientHistoryWithoutTracing(t *testing.T) {
	var traceHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceHeaders = append(traceHeaders, r.Header.Get("X-Trace-ID"))
	}))
	defer server.Close()

	c := client.New(options.New().SetIdentifierType(options.IdentifierNone))
	for range 3 {
		resp, err := c.Get(server.URL)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.UniqueIdentifier)
	}

	assert.Equal(t, 3, c.ResponseCount(), "each response should have its own history entry")
	assert.Equal(t, []string{"", "", ""}, traceHeaders, "no trace header should be sent")
}

// TestClientSetMaxResponsesEnforcesLimit checks that lowering the limit takes
// effect on the next request.
func TestClientSetMaxResponsesEnforcesLimit(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	for range 3 {
		_, err := c.Get(server.URL + "/echo")
		require.NoError(t, err)
	}
	require.Equal(t, 3, c.ResponseCount())

	c.SetMaxResponses(1)
	resp, err := c.Get(server.URL + "/echo")
	require.NoError(t, err)

	assert.Equal(t, 1, c.ResponseCount())
	assert.NotNil(t, c.Response(resp.UniqueIdentifier), "the newest response should be kept")
}

// TestClientFileMethodsRecordPreparationErrors checks that a file that cannot be
// prepared fails like any other request: the response records the error and
// the Client keeps it in its history.
func TestClientFileMethodsRecordPreparationErrors(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	calls := []func() (response.Response, error){
		func() (response.Response, error) { return c.PostFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return c.PutFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return c.PatchFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return client.PostFile(server.URL+"/upload", "missing.txt") },
	}
	for _, call := range calls {
		resp, err := call()
		require.ErrorIs(t, err, options.ErrFileNotFound)
		assert.Equal(t, err, resp.Error)
		assert.NotEmpty(t, resp.UniqueIdentifier)
	}

	assert.Equal(t, 3, c.ResponseCount(), "each failed Client upload should be in the history")
}

// TestClientHistoryKeepsInvalidURLFailures checks that requests that fail
// before sending still get their own identifier and history entry.
func TestClientHistoryKeepsInvalidURLFailures(t *testing.T) {
	c := client.New()

	first, err := c.Get("http://[::1")
	require.Error(t, err)
	second, err := c.Get("http://[::2")
	require.Error(t, err)

	assert.NotEmpty(t, first.UniqueIdentifier)
	assert.NotEqual(t, first.UniqueIdentifier, second.UniqueIdentifier)
	assert.Equal(t, 2, c.ResponseCount())
}
