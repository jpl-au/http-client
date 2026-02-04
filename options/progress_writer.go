package options

import (
	"io"
)

// NewProgressWriter returns an io.Writer that tracks progress during write operations.
// The onProgress callback is invoked with current bytes written and total size.
// Use for scenarios like download progress tracking.
func NewProgressWriter(w io.Writer, totalSize int64, onProgress func(current, total int64)) io.Writer {
	p := &progress{
		totalSize:  totalSize,
		onProgress: onProgress,
	}
	return io.MultiWriter(w, p)
}
