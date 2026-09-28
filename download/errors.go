package download

import "errors"

// Errors reported while checking or storing downloaded response data.
var (
	// ErrRangeMismatch means a response does not match the requested range or version.
	ErrRangeMismatch = errors.New("response does not match the requested range")
	// ErrIncomplete means a valid partial response ended before the whole file.
	ErrIncomplete = errors.New("download incomplete")
	// ErrInProgress means another transfer in this process owns the partial file.
	ErrInProgress = errors.New("download already in progress")
	// ErrChecksumMismatch means downloaded data did not match a configured or server digest.
	ErrChecksumMismatch = errors.New("checksum does not match")
	// ErrBodyTooLarge means a buffered response exceeded the configured size limit.
	ErrBodyTooLarge = errors.New("response body too large")
)
