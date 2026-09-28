package response_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// httpResponse returns a response to a request for address, with the header
// Content-Range set to contentRange when it is not empty.
func httpResponse(t *testing.T, address, contentRange string) *http.Response {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	if contentRange != "" {
		header.Set("Content-Range", contentRange)
	}
	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Header:     header,
		Request:    &http.Request{URL: u},
	}
}

// Populate describes the latest response. A field the new response does not
// set does not keep its value from an earlier response.
func TestPopulate(t *testing.T) {
	const address = "https://example.com/file"

	t.Run("range", func(t *testing.T) {
		tests := []struct {
			name         string
			contentRange string
		}{
			{"absent", ""},
			{"invalid", "bytes 9-3/8"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp := response.New("id", address, http.MethodGet, nil, options.New())
				resp.Populate(httpResponse(t, address, "bytes 0-3/8"), time.Now())
				if resp.ContentRange == nil {
					t.Fatal("ContentRange = nil after a valid range")
				}
				resp.Populate(httpResponse(t, address, tt.contentRange), time.Now())
				if resp.ContentRange != nil {
					t.Errorf("ContentRange = %+v, want nil", *resp.ContentRange)
				}
			})
		}
	})

	t.Run("redirect", func(t *testing.T) {
		const first = "https://example.com/first"
		const second = "https://example.com/second"
		resp := response.New("id", address, http.MethodGet, nil, options.New())

		resp.Populate(httpResponse(t, first, ""), time.Now())
		if !resp.Redirected || resp.Location != first {
			t.Errorf("Redirected = %v, Location = %q, want true and %q", resp.Redirected, resp.Location, first)
		}
		resp.Populate(httpResponse(t, second, ""), time.Now())
		if !resp.Redirected || resp.Location != second {
			t.Errorf("Redirected = %v, Location = %q, want true and %q", resp.Redirected, resp.Location, second)
		}
		resp.Populate(httpResponse(t, address, ""), time.Now())
		if resp.Redirected || resp.Location != "" {
			t.Errorf("Redirected = %v, Location = %q, want false and empty", resp.Redirected, resp.Location)
		}
	})
}
