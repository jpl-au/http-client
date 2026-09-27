package client

import "errors"

// Sentinel errors for request handling
var (
	// ErrMaxRedirectsExceeded is returned when the number of redirects exceeds the configured maximum.
	ErrMaxRedirectsExceeded = errors.New("max redirects exceeded")

	// ErrEmptyURL is returned when an empty URL is provided.
	ErrEmptyURL = errors.New("empty URL")

	// ErrInvalidURL is returned when the URL cannot be parsed.
	ErrInvalidURL = errors.New("invalid URL")

	// ErrMissingHost is returned when the URL has no host component.
	ErrMissingHost = errors.New("missing host")

	// ErrRangeMismatch is returned when a resumed download receives an encoded
	// representation or a partial response that does not continue the partial
	// file: a missing or invalid Content-Range, another range unit, another
	// start offset, or a body whose length differs from its range.
	ErrRangeMismatch = errors.New("response does not match the requested range")

	// ErrDownloadIncomplete is returned when a resumed download receives a valid
	// range that ends before the complete representation. The partial file keeps
	// the bytes received, and resuming again continues from its end.
	ErrDownloadIncomplete = errors.New("download incomplete")

	// ErrPayloadNotReplayable is returned when a 307 or 308 redirect needs the request
	// body again but the payload is a reader that can only be read once. Use []byte,
	// string, *bytes.Buffer, a file, or a seekable reader to follow such redirects.
	ErrPayloadNotReplayable = errors.New("payload cannot be replayed for redirect")

	// ErrBodyTooLarge is returned when a response body held in memory is longer
	// than the limit set with Option.SetMaxBodySize.
	ErrBodyTooLarge = errors.New("response body too large")
)
