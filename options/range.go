package options

import (
	"fmt"
	"os"
)

// RangeConfig holds configuration for HTTP Range requests (RFC 7233).
// This enables partial content downloads and resumable downloads.
type RangeConfig struct {
	// Start is the starting byte offset (inclusive).
	// Used with End for explicit ranges, or alone for "from offset to end".
	Start int64

	// End is the ending byte offset (inclusive).
	// When zero and Start is set, the range extends to the end of the resource.
	End int64

	// Last specifies the number of bytes from the end of the resource.
	// When set, Start and End are ignored, and the range is "last N bytes".
	Last int64

	// IsSet indicates whether a range has been configured.
	IsSet bool

	// IsResume indicates this is a resume operation from an existing partial file.
	// When true, the response writer should open in append mode.
	IsResume bool
}

// RangeHeader returns the formatted Range header value for the request.
// Returns an empty string if no range is configured.
func (rc *RangeConfig) RangeHeader() string {
	if !rc.IsSet {
		return ""
	}

	// Last N bytes: "bytes=-N"
	if rc.Last > 0 {
		return fmt.Sprintf("bytes=-%d", rc.Last)
	}

	// Open-ended range: "bytes=N-" (from offset to end)
	if rc.End == 0 {
		return fmt.Sprintf("bytes=%d-", rc.Start)
	}

	// Explicit range: "bytes=N-M"
	return fmt.Sprintf("bytes=%d-%d", rc.Start, rc.End)
}

// SetRange configures an explicit byte range for partial content requests.
// Both start and end are inclusive byte offsets (0-indexed).
// For example, SetRange(0, 499) requests the first 500 bytes.
func (opt *Option) SetRange(start, end int64) *Option {
	opt.mu.Lock()
	opt.Range.Start = start
	opt.Range.End = end
	opt.Range.Last = 0
	opt.Range.IsSet = true
	opt.Range.IsResume = false
	opt.mu.Unlock()
	return opt
}

// SetRangeFrom configures a range from the specified byte offset to the end of the resource.
// For example, SetRangeFrom(1000) requests all bytes from offset 1000 onwards.
func (opt *Option) SetRangeFrom(offset int64) *Option {
	opt.mu.Lock()
	opt.Range.Start = offset
	opt.Range.End = 0
	opt.Range.Last = 0
	opt.Range.IsSet = true
	opt.Range.IsResume = false
	opt.mu.Unlock()
	return opt
}

// SetRangeLast configures a range to request the last n bytes of the resource.
// For example, SetRangeLast(1024) requests the last 1024 bytes.
func (opt *Option) SetRangeLast(n int64) *Option {
	opt.mu.Lock()
	opt.Range.Start = 0
	opt.Range.End = 0
	opt.Range.Last = n
	opt.Range.IsSet = true
	opt.Range.IsResume = false
	opt.mu.Unlock()
	return opt
}

// Resume configures the request to resume a download to an existing partial file.
// It determines the current file size and sets the appropriate Range header to continue
// downloading from where the previous download stopped. The file output is automatically
// set to the same path, opened in append mode.
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

	info, err := os.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - no range needed, start fresh
			opt.mu.Lock()
			opt.Range.IsSet = false
			opt.Range.IsResume = false
			opt.mu.Unlock()
			return opt
		}
		opt.Log("failed to stat file for resume", "error", err, "filepath", filepath)
		return opt
	}

	size := info.Size()
	if size == 0 {
		// Empty file - no range needed, start fresh
		opt.mu.Lock()
		opt.Range.IsSet = false
		opt.Range.IsResume = false
		opt.mu.Unlock()
		return opt
	}

	opt.mu.Lock()
	opt.Range.Start = size
	opt.Range.End = 0
	opt.Range.Last = 0
	opt.Range.IsSet = true
	opt.Range.IsResume = true
	opt.mu.Unlock()
	return opt
}

// ClearRange removes any configured range settings.
func (opt *Option) ClearRange() *Option {
	opt.mu.Lock()
	opt.Range = RangeConfig{}
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
