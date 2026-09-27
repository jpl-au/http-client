package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/history"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

func TestClient_FileUploads(t *testing.T) {
	// The Client's file upload methods send the file.
	t.Run("methods", func(t *testing.T) {
		server := setupTestServer(t)
		defer server.Close()

		c := client.New()

		tests := []struct {
			name   string
			method string
		}{
			{"post file", "POST"},
			{"put file", "PUT"},
			{"patch file", "PATCH"},
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
					t.Fatalf("upload error = %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
				}
			})
		}
	})

	// The Client's file methods set Content-Type through PrepareFile.
	t.Run("content type", func(t *testing.T) {
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
	})

	// A file that cannot be prepared fails like any other request: the
	// response records the error and the Client keeps it in its history.
	t.Run("missing file", func(t *testing.T) {
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
	})
}

func TestClient_GlobalOptions(t *testing.T) {
	// A per-request Option that does not mention redirects leaves the Client's
	// redirect setting in place.
	t.Run("request option keeps redirects", func(t *testing.T) {
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

		resp, err = c.Post(server.URL+"/upload/no-preserve", url.Values{"k": {"v"}})
		if err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Post() with the global EnableRedirects: StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		resp, err = c.Get(server.URL+"/upload/no-preserve", options.New().DisableRedirects())
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if resp.StatusCode != http.StatusFound {
			t.Errorf("Get() with a per-request DisableRedirects: StatusCode = %d, want %d", resp.StatusCode, http.StatusFound)
		}
	})
}

func TestClient_SetHistory(t *testing.T) {
	// A Client records its responses in the attached History, and stops when
	// the History is detached.
	t.Run("attach and detach", func(t *testing.T) {
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
	})

	// Turning off the trace header still gives each response its own
	// identifier in the history.
	t.Run("without tracing", func(t *testing.T) {
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
	})

	// Requests that fail before sending still get their own identifier and
	// history entry.
	t.Run("invalid url failures", func(t *testing.T) {
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
	})
}
