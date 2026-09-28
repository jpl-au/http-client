package download

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// Transfer processes one prepared request's response and releases its resources.
type Transfer interface {
	// Complete must be called at most once. It closes the supplied response body
	// and retains response status and headers when body processing fails.
	Complete(res *http.Response, resp response.Response, start time.Time) (response.Response, error)
	// Close must be called on every path. It releases claims and file resources,
	// is safe to repeat, and never sends a request or publishes a file.
	Close() error
}

// standard holds the body processing state shared by ordinary, resumable and
// segmented transfers. A live writer belongs to the transfer until disposed.
type standard struct {
	opt      *options.Option
	sum      *checksum
	writer   io.WriteCloser
	closed   bool
	closeErr error
}

// Complete processes a buffered or ordinary file response.
func (t *standard) Complete(res *http.Response, resp response.Response, start time.Time) (response.Response, error) {
	return processResponse(res, resp, t, nil, start)
}

// Close releases an unfinished writer and repeats its cleanup result.
func (t *standard) Close() error {
	if !t.closed {
		t.closed = true
		if t.writer != nil {
			t.closeErr = t.writer.Close()
			t.writer = nil
		}
	}
	return t.closeErr
}

// New prepares a transfer for a prepared request, configured HTTP client and
// request-local options without sending the request. It clones req's headers
// before adding transfer headers. On setup failure it releases acquired claims
// and returns no transfer; the caller remains responsible for req's body.
func New(req *http.Request, client *http.Client, opt *options.Option) (Transfer, error) {
	sum, err := newChecksum(opt.Checksum)
	if err != nil {
		return nil, err
	}
	req.Header = req.Header.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	base := &standard{opt: opt, sum: sum}
	if opt.Range.IsResume {
		r := &Resumable{standard: base}
		release, err := r.claim()
		if err != nil {
			return nil, err
		}
		r.release = release
		if err := r.prepare(req); err != nil {
			return nil, errors.Join(err, r.Close())
		}
		return r, nil
	}
	if req.Method == http.MethodGet &&
		(req.Body == nil || req.Body == http.NoBody) &&
		opt.Segments > 1 &&
		opt.ResponseWriter.Type == options.WriteToFile &&
		!opt.HasRange() {
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Range", "bytes=0-"+strconv.Itoa(minSegmentSize-1))
		return &Segmented{
			standard: base,
			client:   client,
			template: req.Clone(req.Context()),
		}, nil
	}
	return base, nil
}
