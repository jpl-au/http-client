package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// minSegmentSize is the smallest segment in bytes. The first request asks for a
// segment of this size.
const minSegmentSize = 1 << 20

// newSegmented returns the segmented download of a request, or nil when the
// request does not download its file in segments. A request with a body is not
// split, because its body can be sent only once. source is the prepared body,
// or nil when the request has none.
func newSegmented(method string, source *payloadSource, client *http.Client, opt *options.Option, sum *checksum, start time.Time) *segmented {
	if method != http.MethodGet ||
		source != nil ||
		opt.Segments <= 1 ||
		opt.ResponseWriter.Type != options.WriteToFile ||
		opt.Range.IsResume ||
		opt.HasRange() {
		return nil
	}
	return &segmented{client: client, opt: opt, sum: sum, start: start}
}

// segmented is a file download split into segments that download at the same
// time. The fields up to start are set when the request starts, template just
// before the first request is sent, and first when its response arrives. The fields after first are set
// once that response is known to start a segmented download.
type segmented struct {
	client   *http.Client
	template *http.Request // The first request as it was before net/http sent it.
	opt      *options.Option
	sum      *checksum // The checksum set with SetChecksum, or nil.
	start    time.Time
	first    *http.Response // The response to the first request.

	firstRange *response.ContentRange // The first segment, and the size of the file.
	validator  string                 // The strong validator every segment asks for.
	file       *os.File
	progress   *segmentProgress
}

// complete completes the download from the response to its first request,
// which asked for the first segment. A response that is not a range continues
// as an ordinary download. A range with no strong validator, or no known size,
// is fetched again in one request.
//
// Every later request repeats the template. Each one goes to the original
// address, so net/http applies its redirect rules and adds the cookie jar's
// cookies once.
func (s *segmented) complete(first *http.Response, resp response.Response) (response.Response, error) {
	s.first = first
	switch first.StatusCode {
	case http.StatusPartialContent:
	case http.StatusRequestedRangeNotSatisfiable:
		// An empty file has no first byte to ask for.
		return s.fetchAll(resp)
	default:
		return processResponse(first, resp, s.opt, s.start, s.sum)
	}

	// Bytes the transport decoded are not byte ranges of the file.
	if first.Uncompressed {
		return s.fetchAll(resp)
	}

	resp.Populate(first, s.start)
	cr, err := s.parseRange(first, 0, -1, -1)
	if err != nil {
		closeBody(s.opt, first.Body)
		return resp, err
	}
	validator := options.StrongValidator(first.Header)
	if validator == "" || cr.Total < 0 {
		return s.fetchAll(resp)
	}

	writer, err := s.opt.InitialiseWriter()
	if err != nil {
		closeBody(s.opt, first.Body)
		return resp, fmt.Errorf("failed to initialise writer: %w", err)
	}
	file, ok := writer.(*options.FileWriter)
	if !ok {
		closeBody(s.opt, first.Body)
		return resp, errors.Join(fmt.Errorf("segmented download has writer %T, want a file", writer), writer.Close())
	}
	s.firstRange = cr
	s.validator = validator
	s.file = file.File
	s.progress = &segmentProgress{report: s.opt.Progress.OnDownload, total: cr.Total}

	err = s.fetchRemaining()
	var digest *checksum
	if !s.opt.SkipDigestCheck {
		digest = serverDigest(first, true)
	}
	for _, c := range []*checksum{s.sum, digest} {
		if err == nil && c != nil {
			err = c.hashFile(file.Name())
			if err == nil {
				err = c.check()
			}
		}
	}
	if err != nil {
		return resp, errors.Join(err, file.Discard())
	}
	if err := file.Close(); err != nil {
		return resp, err
	}

	setWholeFile(&resp, first.Header, cr.Total)
	return resp, nil
}

// fetchAll closes the first response and fetches the whole file in one
// request, which repeats the template without its range. The response records
// the first response until the new one replaces it, so a request that fails
// still returns what the server sent.
func (s *segmented) fetchAll(resp response.Response) (response.Response, error) {
	resp.Populate(s.first, s.start)
	closeBody(s.opt, s.first.Body)
	req := s.template.Clone(s.template.Context())
	req.Header.Del("Range")
	r, err := s.client.Do(req)
	if err != nil {
		// When a redirect policy rejects a redirect, Do also returns the
		// redirect response, with its body already closed.
		if r != nil {
			resp.Populate(r, s.start)
		}
		return resp, err
	}
	return processResponse(r, resp, s.opt, s.start, s.sum)
}

// fetchRemaining writes the first response, and fetches the remaining bytes of the file
// in segments that download at the same time. The first error cancels the
// other segments.
func (s *segmented) fetchRemaining() error {
	first, total := s.first, s.firstRange.Total
	if err := s.file.Truncate(total); err != nil {
		closeBody(s.opt, first.Body)
		return fmt.Errorf("failed to size segmented download: %w", err)
	}

	ctx, cancel := context.WithCancelCause(first.Request.Context())
	defer cancel(nil)
	// The first body belongs to the parent context, so close it to stop it.
	stop := context.AfterFunc(ctx, func() { closeBody(s.opt, first.Body) })
	defer stop()

	var wg sync.WaitGroup
	fail := func(err error) {
		if err != nil {
			cancel(err)
		}
	}
	wg.Go(func() {
		fail(s.write(first, s.firstRange.Start, s.firstRange.End))
	})
	for _, part := range splitSegments(s.firstRange.End+1, total, s.opt.Segments-1) {
		wg.Go(func() {
			fail(s.fetch(ctx, part[0], part[1]))
		})
	}
	wg.Wait()
	return context.Cause(ctx)
}

// fetch fetches the bytes from offset from to offset to, of the version
// the first response names, and writes them to the file. A response that is
// not that range of that version fails with an error wrapping
// ErrRangeMismatch.
func (s *segmented) fetch(ctx context.Context, from, to int64) error {
	req := s.template.Clone(ctx)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-"+strconv.FormatInt(to, 10))
	req.Header.Set("If-Range", s.validator)
	r, err := s.client.Do(req)
	if err != nil {
		return err
	}
	switch {
	case r.StatusCode == http.StatusOK:
		closeBody(s.opt, r.Body)
		return fmt.Errorf("%w: the file changed on the server during the download", ErrRangeMismatch)
	case r.StatusCode != http.StatusPartialContent:
		closeBody(s.opt, r.Body)
		return fmt.Errorf("segment %d-%d failed: %s", from, to, r.Status)
	}
	// A server that ignores If-Range sends a range of whichever version is
	// current, so compare every validator the responses carry.
	if err := checkVersion(r.Header, s.first.Header.Get("ETag"), s.first.Header.Get("Last-Modified")); err != nil {
		closeBody(s.opt, r.Body)
		return err
	}
	if _, err := s.parseRange(r, from, to, s.firstRange.Total); err != nil {
		closeBody(s.opt, r.Body)
		return err
	}
	return s.write(r, from, to)
}

// write writes the body of r, which holds the bytes from offset from to
// offset to, to the file at offset from, and checks its Content-Digest.
func (s *segmented) write(r *http.Response, from, to int64) error {
	defer closeBody(s.opt, r.Body)

	var body io.Reader = r.Body
	var digest *checksum
	if !s.opt.SkipDigestCheck {
		digest = serverDigest(r, false)
	}
	if digest != nil {
		body = io.TeeReader(body, digest.hash)
	}

	expected := to - from + 1
	written, err := io.Copy(io.NewOffsetWriter(s.file, from), s.progress.reader(io.LimitReader(body, expected)))
	if err != nil {
		return err
	}
	if err := checkRangeLength(body, written, expected); err != nil {
		return err
	}
	if digest != nil {
		return digest.check()
	}
	return nil
}

// splitSegments divides the bytes from offset from to total into at most n
// segments of at least minSegmentSize bytes. Each segment is a first and last
// offset.
func splitSegments(from, total int64, n int) [][2]int64 {
	remaining := total - from
	if remaining <= 0 {
		return nil
	}
	count := max(min(int64(n), remaining/minSegmentSize), 1)
	size := remaining / count
	segments := make([][2]int64, count)
	for i := range count {
		first := from + i*size
		last := first + size - 1
		if i == count-1 {
			last = total - 1
		}
		segments[i] = [2]int64{first, last}
	}
	return segments
}

// parseRange returns the range of a segment response. It returns an
// error wrapping ErrRangeMismatch unless the response is unencoded and holds
// the bytes from offset from to offset to, of total. A to or total of -1
// accepts any value.
func (s *segmented) parseRange(r *http.Response, from, to, total int64) (*response.ContentRange, error) {
	if r.Uncompressed {
		return nil, fmt.Errorf("%w: the transport decoded the segment", ErrRangeMismatch)
	}
	if encoding := r.Header.Get(ContentEncoding); encoding != "" && encoding != "identity" {
		return nil, fmt.Errorf("%w: segment has content encoding %q", ErrRangeMismatch, encoding)
	}
	cr, err := response.ParseContentRange(r.Header.Get("Content-Range"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRangeMismatch, err)
	}
	if cr.Unit != "bytes" || cr.Start != from || (to >= 0 && cr.End != to) || (total >= 0 && cr.Total != total) {
		return nil, fmt.Errorf("%w: segment is %s %d-%d/%d, want bytes %d-%d/%d",
			ErrRangeMismatch, cr.Unit, cr.Start, cr.End, cr.Total, from, to, total)
	}
	return cr, nil
}

// segmentProgress adds up the bytes of all segments and reports the total, one
// call at a time, so the caller's callback does not run concurrently.
type segmentProgress struct {
	mu     sync.Mutex
	done   int64
	total  int64
	report func(current, total int64)
}

// reader returns r, counting the bytes read from it.
func (p *segmentProgress) reader(r io.Reader) io.Reader {
	if p.report == nil {
		return r
	}
	return &segmentProgressReader{r: r, progress: p}
}

// segmentProgressReader counts the bytes read from a segment.
type segmentProgressReader struct {
	r        io.Reader
	progress *segmentProgress
}

// Read reads from the segment and reports the bytes.
func (s *segmentProgressReader) Read(b []byte) (int, error) {
	n, err := s.r.Read(b)
	if n > 0 {
		s.progress.mu.Lock()
		s.progress.done += int64(n)
		s.progress.report(s.progress.done, s.progress.total)
		s.progress.mu.Unlock()
	}
	return n, err
}
