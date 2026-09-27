package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/history"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
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

			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			// Verify the Content-Type header was set correctly (echoed back by server)
			if got, want := resp.Header.Get("Echo-Content-Type"), "application/x-www-form-urlencoded"; got != want {
				t.Errorf("Header.Get(%q) = %q, want %q", "Echo-Content-Type", got, want)
			}
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
	if err != nil {
		t.Fatalf("PostFormData() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
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

			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}
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
	if err != nil {
		t.Fatalf("PostFile() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check that Content-Disposition header was set (PrepareFile sets this)
	contentDisposition := resp.Header.Get("Echo-Content-Disposition")
	if !strings.Contains(contentDisposition, "form-data") {
		t.Errorf("Content-Disposition = %q, want it to contain %q", contentDisposition, "form-data")
	}
	if name := filepath.Base(smallf); !strings.Contains(contentDisposition, name) {
		t.Errorf("Content-Disposition = %q, want it to contain %q", contentDisposition, name)
	}

	// Check that Content-Type was inferred
	contentType := resp.Header.Get("Echo-Content-Type")
	if contentType == "" {
		t.Error(`Content-Type = "", want a type set by PrepareFile`)
	}
}

// TestClientFileMethodsNonExistent tests error handling for non-existent files
func TestClientFileMethodsNonExistent(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()

	_, err := c.PostFile(server.URL+"/upload", "nonexistent-file.txt")
	if err == nil {
		t.Error("PostFile() error = nil, want error")
	}
	if !errors.Is(err, options.ErrFileNotFound) {
		t.Errorf("PostFile() error = %v, want %v", err, options.ErrFileNotFound)
	}
}

// TestClientPerRequestOptionKeepsGlobalSettings checks that a per-request Option
// that does not mention redirects leaves the Client's redirect setting in place.
func TestClientPerRequestOptionKeepsGlobalSettings(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New(options.New().EnableRedirects())

	resp, err := c.Get(server.URL+"/upload/no-preserve", options.New().AddHeader("X-Request", "1"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get() with the global EnableRedirects: StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = c.PostFormData(server.URL+"/upload/no-preserve", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("PostFormData() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("PostFormData() with the global EnableRedirects: StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp, err = c.Get(server.URL+"/upload/no-preserve", options.New().DisableRedirects())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("Get() with a per-request DisableRedirects: StatusCode = %d, want %d", resp.StatusCode, http.StatusFound)
	}
}

// TestClientHistoryWithoutTracing checks that turning off the trace header still
// gives each response its own identifier in the history.
func TestClientHistoryWithoutTracing(t *testing.T) {
	var traceHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceHeaders = append(traceHeaders, r.Header.Get("X-Trace-ID"))
	}))
	defer server.Close()

	h := history.New()
	c := client.New(options.New().SetIdentifierType(options.IdentifierNone))
	c.SetHistory(h)
	for range 3 {
		resp, err := c.Get(server.URL)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.UniqueIdentifier == "" {
			t.Error(`UniqueIdentifier = "", want an identifier`)
		}
	}

	if got, want := h.Len(), 3; got != want {
		t.Errorf("Len() = %d, want %d (one history entry per response)", got, want)
	}
	if want := []string{"", "", ""}; !slices.Equal(traceHeaders, want) {
		t.Errorf("X-Trace-ID headers = %q, want %q (no trace header)", traceHeaders, want)
	}
}

// TestClientSetHistory checks that a Client records its responses in the
// attached History, and stops when the History is detached.
func TestClientSetHistory(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	h := history.New()
	c := client.New()
	c.SetHistory(h)
	resp, err := c.Get(server.URL + "/echo")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := h.Lookup(resp.UniqueIdentifier); !ok {
		t.Errorf("Lookup(%q) found nothing, want the response", resp.UniqueIdentifier)
	}

	c.SetHistory(nil)
	if _, err := c.Get(server.URL + "/echo"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got, want := h.Len(), 1; got != want {
		t.Errorf("Len() after SetHistory(nil) = %d, want %d", got, want)
	}
}

// TestClientFileMethodsRecordPreparationErrors checks that a file that cannot be
// prepared fails like any other request: the response records the error and
// the Client keeps it in its history.
func TestClientFileMethodsRecordPreparationErrors(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	h := history.New()
	c := client.New()
	c.SetHistory(h)
	calls := []func() (response.Response, error){
		func() (response.Response, error) { return c.PostFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return c.PutFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return c.PatchFile(server.URL+"/upload", "missing.txt") },
		func() (response.Response, error) { return client.PostFile(server.URL+"/upload", "missing.txt") },
	}
	for _, call := range calls {
		resp, err := call()
		if !errors.Is(err, options.ErrFileNotFound) {
			t.Fatalf("upload error = %v, want %v", err, options.ErrFileNotFound)
		}
		if resp.Error != err {
			t.Errorf("Response.Error = %v, want %v", resp.Error, err)
		}
		if resp.UniqueIdentifier == "" {
			t.Error(`UniqueIdentifier = "", want an identifier`)
		}
	}

	if got, want := h.Len(), 3; got != want {
		t.Errorf("Len() = %d, want %d (one history entry per failed Client upload)", got, want)
	}
}

// TestClientHistoryKeepsInvalidURLFailures checks that requests that fail
// before sending still get their own identifier and history entry.
func TestClientHistoryKeepsInvalidURLFailures(t *testing.T) {
	h := history.New()
	c := client.New()
	c.SetHistory(h)

	first, err := c.Get("http://[::1")
	if err == nil {
		t.Fatal("Get() error = nil, want error")
	}
	second, err := c.Get("http://[::2")
	if err == nil {
		t.Fatal("Get() error = nil, want error")
	}

	if first.UniqueIdentifier == "" {
		t.Error(`first UniqueIdentifier = "", want an identifier`)
	}
	if first.UniqueIdentifier == second.UniqueIdentifier {
		t.Errorf("second UniqueIdentifier = %q, want a value different from the first", second.UniqueIdentifier)
	}
	if got, want := h.Len(), 2; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
}
