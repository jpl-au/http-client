package client

import (
	"errors"

	"github.com/jpl-au/http-client/download"
)

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

	// ErrRangeMismatch is returned when a range response does not match the
	// request, in three cases:
	//   - A resumed download receives an encoded representation or a partial
	//     response that does not continue the partial file: a missing or
	//     invalid Content-Range, another range unit, another start offset, or
	//     a body whose length differs from its range.
	//   - A segmented download receives a segment that is not the requested
	//     range of the first response's version, or that the transport
	//     decoded. This includes a file that changes on the server during the
	//     download.
	//   - A download to a file that asked for no range receives a 206 partial
	//     response, which is not the whole file.
	ErrRangeMismatch = download.ErrRangeMismatch

	// ErrDownloadIncomplete is returned when a resumed download receives a valid
	// range that ends before the complete representation. The partial file keeps
	// the bytes received, and resuming again continues from its end.
	ErrDownloadIncomplete = download.ErrIncomplete

	// ErrDownloadInProgress is returned when a resumed download starts while
	// another resumed download in this process uses the same partial file. The
	// request is not sent.
	ErrDownloadInProgress = download.ErrInProgress

	// ErrPayloadNotReplayable is returned when a 307 or 308 redirect needs the request
	// body again but the payload is a reader that can only be read once. Use []byte,
	// string, *bytes.Buffer, a file, or a seekable reader to follow such redirects.
	ErrPayloadNotReplayable = errors.New("payload cannot be replayed for redirect")

	// ErrBodyTooLarge is returned when a response body held in memory is longer
	// than the limit set with Option.SetMaxBodySize.
	ErrBodyTooLarge = download.ErrBodyTooLarge

	// ErrStalled is returned when no data is sent or received for longer than
	// the timeout set with Option.SetStallTimeout.
	ErrStalled = errors.New("request stalled")

	// ErrChecksumMismatch is returned when a downloaded body does not match
	// the checksum set with Option.SetChecksum, or the checksum a server sent
	// in a Repr-Digest or Content-Digest header.
	ErrChecksumMismatch = download.ErrChecksumMismatch
)
