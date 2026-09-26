package options

import (
	"fmt"
	"net/http"
	"time"
)

// RangeConfig holds configuration for HTTP Range requests (RFC 7233).
// This enables partial content downloads and resumable downloads.
type RangeConfig struct {
	// Start is the starting byte offset (inclusive).
	// Used with End for explicit ranges, or alone for "from offset to end".
	Start int64

	// End is the ending byte offset (inclusive).
	// When -1, the range extends to the end of the resource.
	End int64

	// Last specifies the number of bytes from the end of the resource.
	// When set, Start and End are ignored, and the range is "last N bytes".
	Last int64

	// IsSet indicates whether a range has been configured.
	IsSet bool

	// IsResume indicates this is a resume operation from an existing partial file.
	// When true, the response writer should open in append mode.
	IsResume bool

	// Validator is the strong validator of the representation that the partial
	// file holds, sent as If-Range, or empty when there is none.
	Validator string

	// ETag and LastModified are the values from the response that started the
	// partial file. A resumed response that states different values belongs to
	// another representation.
	ETag         string
	LastModified string

	// err records an invalid range passed to a setter. The request fails with it.
	err error
}

// RangeHeader returns the formatted Range header value for the request.
// Returns an empty string if no range is configured, and an error wrapping
// ErrInvalidRange if the range is invalid.
func (rc *RangeConfig) RangeHeader() (string, error) {
	if !rc.IsSet {
		return "", nil
	}
	if rc.err != nil {
		return "", rc.err
	}

	// Last N bytes: "bytes=-N"
	if rc.Last > 0 {
		return fmt.Sprintf("bytes=-%d", rc.Last), nil
	}

	if rc.Start < 0 {
		return "", fmt.Errorf("%w: start %d is negative", ErrInvalidRange, rc.Start)
	}

	// Open-ended range: "bytes=N-" (from offset to end)
	if rc.End < 0 {
		return fmt.Sprintf("bytes=%d-", rc.Start), nil
	}

	if rc.End < rc.Start {
		return "", fmt.Errorf("%w: end %d is before start %d", ErrInvalidRange, rc.End, rc.Start)
	}

	// Explicit range: "bytes=N-M"
	return fmt.Sprintf("bytes=%d-%d", rc.Start, rc.End), nil
}

// SetRange configures an explicit byte range for partial content requests.
// Both start and end are inclusive byte offsets (0-indexed).
// For example, SetRange(0, 499) requests the first 500 bytes.
// The request fails with ErrInvalidRange if start is negative or end is before start.
func (opt *Option) SetRange(start, end int64) *Option {
	opt.mu.Lock()
	opt.Range = RangeConfig{Start: start, End: end, IsSet: true}
	if end < 0 {
		opt.Range.err = fmt.Errorf("%w: end %d is negative", ErrInvalidRange, end)
	}
	opt.mu.Unlock()
	return opt
}

// SetRangeFrom configures a range from the specified byte offset to the end of the resource.
// For example, SetRangeFrom(1000) requests all bytes from offset 1000 onwards.
// The request fails with ErrInvalidRange if offset is negative.
func (opt *Option) SetRangeFrom(offset int64) *Option {
	opt.mu.Lock()
	opt.Range = RangeConfig{Start: offset, End: -1, IsSet: true}
	opt.mu.Unlock()
	return opt
}

// SetRangeLast configures a range to request the last n bytes of the resource.
// For example, SetRangeLast(1024) requests the last 1024 bytes.
// The request fails with ErrInvalidRange if n is not positive.
func (opt *Option) SetRangeLast(n int64) *Option {
	opt.mu.Lock()
	opt.Range = RangeConfig{Last: n, IsSet: true}
	if n <= 0 {
		opt.Range.err = fmt.Errorf("%w: suffix length %d is not positive", ErrInvalidRange, n)
	}
	opt.mu.Unlock()
	return opt
}

// Resume configures a download to filepath that can continue after an interruption.
// The data goes to a partial file (see PartialPath) and is renamed to filepath
// only when the download is complete, so filepath never holds a partial download.
// Each request reads the size of the partial file when it starts and asks for the
// bytes from that offset, so the Option can be reused as the file grows. An
// interrupted request keeps the bytes it received in the partial file. A valid
// range that ends before the complete representation returns ErrDownloadIncomplete;
// resume again to continue.
//
// from is the header of the response that started the partial file, usually
// resp.Header from the earlier attempt. Resume sends its strong validator as
// If-Range, so if the resource has changed, the server sends it whole and the
// partial file starts again. When from has no strong validator, nothing proves
// the partial file belongs to the current resource, and the download starts
// from the beginning. See resumeValidator for the rules.
//
// If the partial file doesn't exist or is empty, the download starts from the beginning.
// Resumed downloads ask for the identity encoding and reject encoded responses
// before writing, including on the first attempt and when restarting. Decoded
// bytes cannot safely be resumed using the encoded representation's offsets.
//
// Example usage:
//
//	opt := options.New().Resume("/path/to/file.bin", previous.Header)
//	resp, err := client.Get("https://example.com/file.bin", opt)
func (opt *Option) Resume(filepath string, from http.Header) *Option {
	// Always set file output - either appending or creating fresh
	opt.SetFileOutput(filepath)

	opt.mu.Lock()
	opt.Range = RangeConfig{
		IsResume:     true,
		Validator:    resumeValidator(from),
		ETag:         from.Get("ETag"),
		LastModified: from.Get("Last-Modified"),
	}
	opt.explicit |= settingRange
	opt.mu.Unlock()
	return opt
}

// resumeValidator returns the value to send as If-Range for a partial file
// that came from a response with header from, or "" when it has no strong
// validator (RFC 9110, section 13.1.5).
//
// A strong ETag is used when present. A date is used only when the response
// has no ETag at all, and only when it is strong: the response's Date must be
// at least one second after its Last-Modified (RFC 9110, section 8.8.2.2).
// A header that does not parse gives no validator.
func resumeValidator(from http.Header) string {
	if etag := from.Get("ETag"); etag != "" {
		if isStrongETag(etag) {
			return etag
		}
		return ""
	}

	modified, err := http.ParseTime(from.Get("Last-Modified"))
	if err != nil {
		return ""
	}
	date, err := http.ParseTime(from.Get("Date"))
	if err != nil {
		return ""
	}
	if date.Sub(modified) < time.Second {
		return ""
	}
	return from.Get("Last-Modified")
}

// isStrongETag reports whether v is a strong entity tag: a quoted string of
// etagc characters (RFC 9110, section 8.8.3).
func isStrongETag(v string) bool {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return false
	}
	for i := 1; i < len(v)-1; i++ {
		c := v[i]
		if c != 0x21 && (c < 0x23 || c > 0x7e) && c < 0x80 {
			return false
		}
	}
	return true
}

// PartialPath returns the path of the partial file that Resume keeps for the
// destination path until the download is complete.
func PartialPath(path string) string {
	return path + ".part"
}

// ClearRange removes any configured range settings.
func (opt *Option) ClearRange() *Option {
	opt.mu.Lock()
	opt.Range = RangeConfig{}
	opt.explicit |= settingRange
	opt.mu.Unlock()
	return opt
}

// HasRange returns true if a range has been configured.
func (opt *Option) HasRange() bool {
	opt.mu.RLock()
	isSet := opt.Range.IsSet
	opt.mu.RUnlock()
	return isSet
}
