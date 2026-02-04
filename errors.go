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

	// ErrRedirectMissingLocation is returned when a redirect response has no Location header.
	ErrRedirectMissingLocation = errors.New("redirect location header missing")

	// ErrPayloadNotReplayable is returned when a redirect or retry is needed but the
	// payload is a non-seekable io.Reader that exceeds the buffer limit and cannot be replayed.
	ErrPayloadNotReplayable = errors.New("payload cannot be replayed: non-seekable reader exceeds buffer limit")
)
