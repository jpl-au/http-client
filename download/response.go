package download

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// processResponse handles the final response processing including decompression
// and body reading.
func processResponse(r *http.Response, resp response.Response, t *standard, resume *Resumable, startTime time.Time) (response.Response, error) {
	opt, sum := t.opt, t.sum
	defer closeBody(opt, r.Body)

	// Record what was received before reading the body, so a response that
	// fails later still has its status and headers.
	resp.Populate(r, startTime)

	// A response without a body leaves the output alone, so a file destination
	// keeps its content, and has nothing to decompress.
	if !hasBody(r) {
		return resp, nil
	}

	// A 206 answers a request for a range. A request that asked for none
	// receives only part of the file, which must not replace the destination.
	if r.StatusCode == http.StatusPartialContent && opt.ResponseWriter.Type == options.WriteToFile && r.Request.Header.Get("Range") == "" {
		return resp, fmt.Errorf("%w: asked for the whole file, received %s", ErrRangeMismatch, r.Header.Get("Content-Range"))
	}

	// A partial file that already holds the whole file asks for a range past
	// its end, and the server answers 416 with the file's size.
	if resume != nil && resume.continuation && r.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		cr, err := response.ParseContentRange(r.Header.Get("Content-Range"))
		if err == nil && cr.Unit == "bytes" && cr.Total >= 0 && cr.Total <= resume.offset {
			if err := resume.completePartial(r, cr.Total); err != nil {
				return resp, err
			}
			// No body is read, so report the whole file once.
			if opt.Progress.OnDownload != nil {
				opt.Progress.OnDownload(cr.Total, cr.Total)
			}
			setWholeFile(&resp, r.Header, cr.Total)
			return resp, nil
		}
	}

	encoding := r.Header.Get("Content-Encoding")

	// A resumed download appends to the partial file, so a partial response
	// must continue the file exactly where it ends. Any other successful
	// response carries the whole representation and starts the partial file again.
	var continued *response.ContentRange
	expected := int64(-1)
	if resume != nil && r.StatusCode < http.StatusMultipleChoices {
		// Resume offsets describe the saved representation. Decoding an
		// encoded response would leave a prefix that cannot safely use its
		// validator or byte offsets, even on the first request or a restart.
		if r.Uncompressed || (encoding != "" && encoding != "identity") {
			return resp, fmt.Errorf("%w: resumable download requires an unencoded response", ErrRangeMismatch)
		}
		if r.StatusCode == http.StatusPartialContent {
			cr, err := resume.parseRange(r)
			if err != nil {
				return resp, err
			}
			continued = cr
			expected = cr.End - cr.Start + 1
		} else {
			resume.continuation = false
		}
	}

	// A checksum describes the requested resource, so an error response is
	// not checked. A server's checksum covers the bytes it sent, which are
	// not available when net/http has decompressed the response.
	if r.StatusCode >= http.StatusMultipleChoices {
		sum = nil
	}
	var digest *checksum
	if !opt.SkipDigestCheck && !r.Uncompressed {
		digest = serverDigest(r, continued != nil)
	}

	// A resumed download is checked as a whole file, so the bytes already in
	// the partial file are hashed first.
	for _, c := range []*checksum{sum, digest} {
		if c != nil && continued != nil {
			if err := c.hashPartialFile(opt.ResponseWriter.FilePath, continued.Start); err != nil {
				return resp, err
			}
		}
	}

	// The server's checksum covers the bytes before decompression.
	sent := r.Body
	if digest != nil {
		sent = struct {
			io.Reader
			io.Closer
		}{io.TeeReader(r.Body, digest.hash), r.Body}
	}

	decompressedBody, err := opt.NewDecompressor(sent, encoding)
	if err != nil {
		return resp, fmt.Errorf("failed to create decompressed reader: %w", err)
	}
	defer closeBody(opt, decompressedBody)

	// A file destination receives only a successful response. Any other
	// response goes to a buffer, so the caller can read the error body and
	// the file keeps its content.
	var writer io.WriteCloser
	if opt.ResponseWriter.Type == options.WriteToFile && r.StatusCode >= http.StatusMultipleChoices {
		writer = &options.WriteCloserBuffer{Buffer: &bytes.Buffer{}}
	} else {
		writer, err = newWriter(opt.ResponseWriter, resume != nil, resume != nil && resume.continuation)
		if err != nil {
			return resp, fmt.Errorf("failed to initialise writer: %w", err)
		}
		t.writer = writer
	}

	totalSize := r.ContentLength
	onDownload := opt.Progress.OnDownload

	// Progress on a resumed download covers the whole file: the bytes already
	// on disk count, and the total is the length of the representation.
	if onDownload != nil && continued != nil {
		totalSize = continued.Total
		report, offset := onDownload, continued.Start
		onDownload = func(current, total int64) {
			report(offset+current, total)
		}
	}

	var reader io.Reader = decompressedBody
	if onDownload != nil {
		if encoding != "" && encoding != "identity" {
			totalSize = -1
		}
		reader = options.NewProgressReader(decompressedBody, totalSize, onDownload)
	}

	// Write no more than the range, so surplus bytes never reach the file.
	body := reader
	if expected >= 0 {
		reader = io.LimitReader(body, expected)
	}

	// A buffered body is held in memory, so it must not exceed the limit.
	// Reading one byte past the limit shows whether the body is longer. No body
	// can be longer than the largest limit, and one more byte would overflow.
	_, buffered := writer.(*options.WriteCloserBuffer)
	limited := buffered && opt.MaxBodySize > 0 && opt.MaxBodySize < math.MaxInt64
	if limited {
		reader = io.LimitReader(reader, opt.MaxBodySize+1)
	}
	if sum != nil {
		reader = io.TeeReader(reader, sum.hash)
	}

	var written int64
	var copyErr error
	if opt.Progress.DownloadBufferSize != nil {
		buf := make([]byte, *opt.Progress.DownloadBufferSize)
		written, copyErr = io.CopyBuffer(writer, reader, buf)
	} else {
		written, copyErr = io.Copy(writer, reader)
	}

	if copyErr == nil && expected >= 0 {
		copyErr = checkRangeLength(body, written, expected)
	}
	if copyErr == nil && limited && written > opt.MaxBodySize {
		copyErr = fmt.Errorf("%w: body is longer than %d bytes", ErrBodyTooLarge, opt.MaxBodySize)
	}

	// A range with an unknown total is taken to run to the end, as requested.
	incomplete := continued != nil && continued.Total >= 0 && continued.End+1 < continued.Total
	for _, c := range []*checksum{sum, digest} {
		if copyErr == nil && c != nil && !incomplete {
			copyErr = c.check()
		}
	}

	var closeErr error
	switch w := writer.(type) {
	case *fileWriter:
		// A file download that failed part way is discarded, so the
		// destination keeps its content.
		if copyErr != nil {
			closeErr = w.Discard()
		} else {
			closeErr = w.Publish()
		}
	case *partialWriter:
		// A resumed download is published only when it is complete. A response
		// that failed validation is removed from the partial file. A complete
		// file that fails its checksum is removed whole, because any of its
		// bytes can be wrong. Otherwise the partial file keeps what arrived
		// for the next resume.
		switch {
		case errors.Is(copyErr, ErrRangeMismatch):
			closeErr = w.Discard()
		case errors.Is(copyErr, ErrChecksumMismatch):
			closeErr = w.Remove()
		case copyErr != nil:
			closeErr = w.Close()
		case incomplete:
			closeErr = w.Close()
			copyErr = fmt.Errorf("%w: the partial file has %d of %d bytes; resume again to continue",
				ErrIncomplete, continued.End+1, continued.Total)
		default:
			closeErr = w.Publish()
		}
	default:
		closeErr = writer.Close()
	}
	if copyErr != nil || closeErr != nil {
		return resp, errors.Join(copyErr, closeErr)
	}

	if buf, ok := writer.(*options.WriteCloserBuffer); ok {
		resp.Body = *buf
	}

	return resp, nil
}

// closeBody closes body and logs the error. A body is closed once the result
// of the request is known, and an error closing it does not change that
// result.
func closeBody(opt *options.Option, body io.Closer) {
	if err := body.Close(); err != nil {
		opt.Log("failed to close body", "error", err)
	}
}

// hasBody reports whether r can carry a body. A response to HEAD, and a 1xx,
// 204, 205 or 304 response, has none (RFC 9110).
func hasBody(r *http.Response) bool {
	switch {
	case r.Request.Method == http.MethodHead:
		return false
	case r.StatusCode < http.StatusOK:
		return false
	case r.StatusCode == http.StatusNoContent,
		r.StatusCode == http.StatusResetContent,
		r.StatusCode == http.StatusNotModified:
		return false
	}
	return true
}

// checkVersion returns an error wrapping ErrRangeMismatch when the header h
// names another version than the ETag etag and the Last-Modified modified: a
// different ETag or a different Last-Modified. A validator that only one side
// has is not compared.
func checkVersion(h http.Header, etag, modified string) error {
	if got := h.Get("ETag"); got != "" && etag != "" && got != etag {
		return fmt.Errorf("%w: ETag %s differs from %s", ErrRangeMismatch, got, etag)
	}
	if got := h.Get("Last-Modified"); got != "" && modified != "" && !equalDates(got, modified) {
		return fmt.Errorf("%w: Last-Modified %s differs from %s", ErrRangeMismatch, got, modified)
	}
	return nil
}

// setWholeFile sets resp to describe a response that delivered the whole file of
// total bytes, with the status 200 OK and header h. It removes the headers
// that describe only one range of the file.
func setWholeFile(resp *response.Response, h http.Header, total int64) {
	resp.Status = strconv.Itoa(http.StatusOK) + " " + http.StatusText(http.StatusOK)
	resp.StatusCode = http.StatusOK
	resp.ContentLength = total
	resp.IsPartialContent = false
	resp.ContentRange = nil
	resp.Header = h.Clone()
	resp.Header.Del("Content-Range")
	resp.Header.Del("Content-Digest")
	resp.Header.Set("Content-Length", strconv.FormatInt(total, 10))
}

// checkRangeLength returns an error wrapping ErrRangeMismatch when the body
// held fewer or more bytes than its range. A complete HTTP message can still
// carry fewer bytes than its Content-Range states.
func checkRangeLength(body io.Reader, written, expected int64) error {
	if written < expected {
		return fmt.Errorf("%w: body has %d bytes, range has %d", ErrRangeMismatch, written, expected)
	}
	extra, err := io.Copy(io.Discard, io.LimitReader(body, 1))
	if err != nil {
		return fmt.Errorf("failed to read past the range: %w", err)
	}
	if extra > 0 {
		return fmt.Errorf("%w: body is longer than its range of %d bytes", ErrRangeMismatch, expected)
	}
	return nil
}

// equalDates reports whether two HTTP dates name the same time. Values that do
// not parse are compared as text.
func equalDates(a, b string) bool {
	ta, errA := http.ParseTime(a)
	tb, errB := http.ParseTime(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ta.Equal(tb)
}
