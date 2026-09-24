package options

import (
	"fmt"
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

// Resume configures the request to resume a download to an existing partial file.
// Each request reads the size of the file when it starts and asks for the bytes
// from that offset, so the Option can be reused as the file grows. The response
// is appended to the file at the same path.
//
// If the file doesn't exist or is empty, the download starts from the beginning.
//
// Example usage:
//
//	opt := options.New().Resume("/path/to/partial.bin")
//	resp, err := client.Get("https://example.com/file.bin", opt)
func (opt *Option) Resume(filepath string) *Option {
	// Always set file output - either appending or creating fresh
	opt.SetFileOutput(filepath)

	opt.mu.Lock()
	opt.Range = RangeConfig{IsResume: true}
	opt.explicit |= settingRange
	opt.mu.Unlock()
	return opt
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
