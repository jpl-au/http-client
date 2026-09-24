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

	// ErrPayloadNotReplayable is returned when a 307 or 308 redirect needs the request
	// body again but the payload is a reader that can only be read once. Use []byte,
	// string, *bytes.Buffer, a file, or a seekable reader to follow such redirects.
	ErrPayloadNotReplayable = errors.New("payload cannot be replayed for redirect")
)
