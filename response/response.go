package response

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jpl-au/http-client/options"
)

// ContentRange holds parsed Content-Range header information from partial content responses.
type ContentRange struct {
	// Unit is the range unit, typically "bytes"
	Unit string
	// Start is the starting byte offset of the range, or -1 for an unsatisfied range
	Start int64
	// End is the ending byte offset of the range (inclusive), or -1 for an unsatisfied range
	End int64
	// Total is the total size of the resource, or -1 if unknown (indicated by "*")
	Total int64
}

// Response represents the HTTP response along with additional metadata
type Response struct {
	UniqueIdentifier string                    // Unique ID for the request, generated internally
	URL              string                    // URL the request was made to
	Method           string                    // HTTP method used (e.g., GET, POST)
	RequestPayload   any                       // Payload sent with the request
	Options          *options.Option           // Configuration options for the request
	RequestTime      int64                     // Timestamp of when the request was initiated
	ResponseTime     int64                     // Timestamp of when the response was received
	ProcessedTime    int64                     // Duration taken to process the request
	Status           string                    // HTTP status message (e.g., "200 OK")
	StatusCode       int                       // HTTP status code (e.g., 200, 404)
	Proto            string                    // Protocol used (e.g., HTTP/1.1)
	Header           http.Header               // Headers included in the response
	ContentLength    int64                     // Length of the response content
	TransferEncoding []string                  // Transfer encoding details from the response
	CompressionType  options.CompressionType   // Type of compression applied to the response
	Uncompressed     bool                      // Indicates if the response was uncompressed
	Cookies          []*http.Cookie            // Cookies received with the response
	AccessTime       time.Duration             // Time taken to complete the request
	Body             options.WriteCloserBuffer // The response body as a buffer
	TLS              *tls.ConnectionState      // Details about the TLS connection

	// Error stores any error encountered during the request. This field exists
	// in addition to the error returned by request functions (Get, Post, etc.)
	// to support batch operations and response history. When responses are stored
	// in a collection for later inspection, the Error field allows iteration
	// through responses to determine which requests failed and why, without
	// requiring separate error tracking.
	//
	// For immediate error handling, check the returned error. For deferred
	// inspection of stored responses, check this field.
	Error      error
	Redirected bool   // Indicates if the request was redirected
	Location   string // New location if the request was redirected

	// Range response fields (RFC 7233)
	ContentRange     *ContentRange // Parsed Content-Range header, or nil if absent or invalid
	AcceptRanges     string        // Accept-Ranges header value (e.g., "bytes" or "none")
	IsPartialContent bool          // True if response is 206 Partial Content
}

// New initializes a new Response instance with basic details
func New(url string, method string, payload any, opt *options.Option) Response {
	return Response{
		UniqueIdentifier: opt.GenerateIdentifier(), // Generate unique request identifier
		URL:              url,                      // Request URL
		Method:           method,                   // HTTP method
		RequestPayload:   payload,                  // Request payload
		Options:          opt,                      // Request options
		CompressionType:  opt.Compression.Type,     // Compression type from options
	}
}

// Bytes returns the response body as a byte slice
func (r *Response) Bytes() []byte {
	if r.Body.IsEmpty() {
		return nil
	}
	return r.Body.Bytes()
}

// String returns the response body as a string
func (r *Response) String() string {
	if r.Body.IsEmpty() {
		return ""
	}
	return r.Body.String()
}

// Len returns the length of the response body
// If there is no body, it returns -1 to indicate there is
// an issue
func (r *Response) Len() int64 {
	if r.Body.IsEmpty() {
		return -1
	}
	return int64(r.Body.Len())
}

// Buffer returns the response body as a *bytes.Buffer for efficient streaming operations.
// Returns nil if the body is empty or was not stored (e.g., when writing directly to file).
func (r *Response) Buffer() *bytes.Buffer {
	if r.Body.IsEmpty() {
		return nil
	}
	return r.Body.Buffer
}

// PopulateResponse populates the Response struct with data from an http.Response
func (r *Response) PopulateResponse(resp *http.Response, start time.Time) {
	r.Status = resp.Status                     // Set HTTP status message
	r.StatusCode = resp.StatusCode             // Set HTTP status code
	r.Proto = resp.Proto                       // Set protocol used
	r.Header = resp.Header                     // Copy response headers
	r.TransferEncoding = resp.TransferEncoding // Copy transfer encoding
	r.Cookies = resp.Cookies()                 // Copy response cookies
	r.AccessTime = time.Since(start)           // Calculate and set access time
	r.Uncompressed = resp.Uncompressed         // Set uncompressed flag
	r.TLS = resp.TLS                           // Copy TLS connection state

	// Check and record if the request was redirected
	if resp.Request.URL.String() != r.URL {
		r.Redirected = true
		r.Location = resp.Request.URL.String()
	}

	// Handle range response fields (RFC 7233)
	r.AcceptRanges = resp.Header.Get("Accept-Ranges")
	r.IsPartialContent = resp.StatusCode == http.StatusPartialContent

	// An invalid header leaves ContentRange nil, as documented on the field.
	// ParseContentRange reports the reason to callers that need it.
	if cr, err := ParseContentRange(resp.Header.Get("Content-Range")); err == nil {
		r.ContentRange = cr
	}
}

// ErrInvalidContentRange is returned by ParseContentRange for a malformed header.
var ErrInvalidContentRange = errors.New("invalid Content-Range header")

// ParseContentRange parses a Content-Range header value (RFC 9110, section 14.4).
// Formats: "bytes 0-499/1234", "bytes 500-999/*", and "bytes */1234" for an
// unsatisfied range, which has Start and End of -1.
// It returns an error wrapping ErrInvalidContentRange for a malformed header,
// or for bounds that are negative, reversed, or beyond the total.
func ParseContentRange(header string) (*ContentRange, error) {
	invalid := fmt.Errorf("%w: %q", ErrInvalidContentRange, header)

	unit, spec, ok := strings.Cut(header, " ")
	if !ok || unit == "" {
		return nil, invalid
	}
	bounds, total, ok := strings.Cut(spec, "/")
	if !ok {
		return nil, invalid
	}

	cr := &ContentRange{Unit: unit, Start: -1, End: -1, Total: -1}
	if total != "*" {
		n, err := strconv.ParseInt(total, 10, 64)
		if err != nil || n < 0 {
			return nil, invalid
		}
		cr.Total = n
	}

	// An unsatisfied range must state the complete length.
	if bounds == "*" {
		if cr.Total < 0 {
			return nil, invalid
		}
		return cr, nil
	}

	first, last, ok := strings.Cut(bounds, "-")
	if !ok {
		return nil, invalid
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return nil, invalid
	}
	end, err := strconv.ParseInt(last, 10, 64)
	if err != nil {
		return nil, invalid
	}
	if start < 0 || end < start || (cr.Total >= 0 && end >= cr.Total) {
		return nil, invalid
	}
	cr.Start = start
	cr.End = end
	return cr, nil
}
