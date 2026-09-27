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

// segmentMin is the smallest segment in bytes. The first request asks for a
// segment of this size.
const segmentMin = 1 << 20

// segmented reports whether a request downloads its file in segments.
func segmented(method string, opt *options.Option) bool {
	return method == http.MethodGet &&
		opt.Segments > 1 &&
		opt.ResponseWriter.Type == options.WriteToFile &&
		!opt.Range.IsResume &&
		!opt.HasRange()
}

// segmentedDownload is a file download split into segments that download at
// the same time. The fields up to start are set before the first request is
// sent. The others are set from the first response, before any segment starts.
type segmentedDownload struct {
	client   *http.Client
	template *http.Request // The first request as it was before net/http sent it.
	opt      *options.Option
	sum      *checksum // The checksum set with SetChecksum, or nil.
	start    time.Time

	header   http.Header // The first response's header, which names the version.
	tag      string      // The strong validator every segment asks for.
	total    int64       // The size of the file in bytes.
	file     *os.File
	progress *segmentProgress
}

// finish completes the download from the response to its first request, which
// asked for the first segment. A response that is not a range continues as an
// ordinary download. A range with no strong validator, or no known size, is
// fetched again in one request.
//
// Every later request repeats the template. Each one goes to the original
// address, so net/http applies its redirect rules and adds the cookie jar's
// cookies once.
func (d *segmentedDownload) finish(first *http.Response, resp response.Response) (response.Response, error) {
	switch first.StatusCode {
	case http.StatusPartialContent:
	case http.StatusRequestedRangeNotSatisfiable:
		// An empty file has no first byte to ask for.
		return d.fetchFile(first, resp)
	default:
		return processResponse(first, resp, d.opt, d.start, d.sum)
	}

	// Bytes the transport decoded are not byte ranges of the file.
	if first.Uncompressed {
		return d.fetchFile(first, resp)
	}

	resp.PopulateResponse(first, d.start)
	cr, err := parseSegmentRange(first, 0, -1, -1)
	if err != nil {
		first.Body.Close()
		return resp, err
	}
	d.tag = options.StrongValidator(first.Header)
	if d.tag == "" || cr.Total < 0 {
		return d.fetchFile(first, resp)
	}
	d.header = first.Header
	d.total = cr.Total
	d.progress = &segmentProgress{report: d.opt.Progress.OnDownload, total: cr.Total}

	writer, err := d.opt.InitialiseWriter()
	if err != nil {
		first.Body.Close()
		return resp, fmt.Errorf("failed to initialise writer: %w", err)
	}
	file, ok := writer.(*options.FileWriter)
	if !ok {
		first.Body.Close()
		return resp, errors.Join(fmt.Errorf("segmented download has writer %T, want a file", writer), writer.Close())
	}
	d.file = file.File

	err = d.fetchSegments(first, cr)
	var digest *checksum
	if !d.opt.SkipDigestCheck {
		digest = serverDigest(first, true)
	}
	for _, c := range []*checksum{d.sum, digest} {
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

	// The caller receives the whole file, as from an ordinary download.
	resp.Status = strconv.Itoa(http.StatusOK) + " " + http.StatusText(http.StatusOK)
	resp.StatusCode = http.StatusOK
	resp.ContentLength = cr.Total
	resp.IsPartialContent = false
	resp.ContentRange = nil
	resp.Header = first.Header.Clone()
	resp.Header.Del("Content-Range")
	resp.Header.Del("Content-Digest")
	resp.Header.Set("Content-Length", strconv.FormatInt(cr.Total, 10))
	return resp, nil
}

// fetchFile closes the first response and fetches the whole file in one
// request, which repeats the template without its range.
func (d *segmentedDownload) fetchFile(first *http.Response, resp response.Response) (response.Response, error) {
	if err := first.Body.Close(); err != nil {
		d.opt.Log("failed to close first segment", "error", err)
	}
	req := d.template.Clone(d.template.Context())
	req.Header.Del("Range")
	r, err := d.client.Do(req)
	if err != nil {
		return resp, err
	}
	return processResponse(r, resp, d.opt, d.start, d.sum)
}

// fetchSegments writes the first response, and fetches the rest of the file
// in segments that download at the same time. The first error cancels the
// other segments.
func (d *segmentedDownload) fetchSegments(first *http.Response, cr *response.ContentRange) error {
	if err := d.file.Truncate(d.total); err != nil {
		first.Body.Close()
		return fmt.Errorf("failed to size segmented download: %w", err)
	}

	ctx, cancel := context.WithCancelCause(first.Request.Context())
	defer cancel(nil)
	// The first body belongs to the parent context, so close it to stop it.
	stop := context.AfterFunc(ctx, func() { first.Body.Close() })
	defer stop()

	var wg sync.WaitGroup
	fail := func(err error) {
		if err != nil {
			cancel(err)
		}
	}
	wg.Go(func() {
		fail(d.writeSegment(first, cr.Start, cr.End))
	})
	for _, s := range splitSegments(cr.End+1, d.total, d.opt.Segments-1) {
		wg.Go(func() {
			fail(d.fetchSegment(ctx, s[0], s[1]))
		})
	}
	wg.Wait()
	return context.Cause(ctx)
}

// fetchSegment fetches the bytes from offset from to offset to, of the version
// the first response names, and writes them to the file. A response that is
// not that range of that version fails with an error wrapping
// ErrRangeMismatch.
func (d *segmentedDownload) fetchSegment(ctx context.Context, from, to int64) error {
	req := d.template.Clone(ctx)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-"+strconv.FormatInt(to, 10))
	req.Header.Set("If-Range", d.tag)
	r, err := d.client.Do(req)
	if err != nil {
		return err
	}
	switch {
	case r.StatusCode == http.StatusOK:
		r.Body.Close()
		return fmt.Errorf("%w: the file changed on the server during the download", ErrRangeMismatch)
	case r.StatusCode != http.StatusPartialContent:
		r.Body.Close()
		return fmt.Errorf("segment %d-%d failed: %s", from, to, r.Status)
	}
	// A server that ignores If-Range sends a range of whichever version is
	// current, so compare every validator the responses carry.
	if err := checkVersion(r.Header, d.header); err != nil {
		r.Body.Close()
		return err
	}
	if _, err := parseSegmentRange(r, from, to, d.total); err != nil {
		r.Body.Close()
		return err
	}
	return d.writeSegment(r, from, to)
}

// writeSegment writes the body of r, which holds the bytes from offset from to
// offset to, to the file at offset from, and checks its Content-Digest.
func (d *segmentedDownload) writeSegment(r *http.Response, from, to int64) error {
	defer r.Body.Close()

	var body io.Reader = r.Body
	var digest *checksum
	if !d.opt.SkipDigestCheck {
		digest = serverDigest(r, false)
	}
	if digest != nil {
		body = io.TeeReader(body, digest.hash)
	}

	expected := to - from + 1
	written, err := io.Copy(io.NewOffsetWriter(d.file, from), d.progress.reader(io.LimitReader(body, expected)))
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
// segments of at least segmentMin bytes. Each segment is a first and last
// offset.
func splitSegments(from, total int64, n int) [][2]int64 {
	remaining := total - from
	if remaining <= 0 {
		return nil
	}
	count := max(min(int64(n), remaining/segmentMin), 1)
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

// checkVersion returns an error wrapping ErrRangeMismatch when a segment's
// header names another version than the first response's header: a different
// ETag or a different Last-Modified. A validator only one of them has is not
// compared.
func checkVersion(segment, first http.Header) error {
	if a, b := segment.Get("ETag"), first.Get("ETag"); a != "" && b != "" && a != b {
		return fmt.Errorf("%w: ETag %s differs from %s", ErrRangeMismatch, a, b)
	}
	if a, b := segment.Get("Last-Modified"), first.Get("Last-Modified"); a != "" && b != "" && !sameTime(a, b) {
		return fmt.Errorf("%w: Last-Modified %s differs from %s", ErrRangeMismatch, a, b)
	}
	return nil
}

// parseSegmentRange returns the range of a segment response. It returns an
// error wrapping ErrRangeMismatch unless the response is unencoded and holds
// the bytes from offset from to offset to, of total. A to or total of -1
// accepts any value.
func parseSegmentRange(r *http.Response, from, to, total int64) (*response.ContentRange, error) {
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
