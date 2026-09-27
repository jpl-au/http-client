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

// downloadSegments finishes a segmented download from the response to its
// first request, which asked for the first segment. A response that is not a
// range continues as an ordinary download. A range with no strong validator,
// or no known size, is downloaded again in one request.
func downloadSegments(client *http.Client, first *http.Response, resp response.Response, opt *options.Option, start time.Time, sum *checksum) (response.Response, error) {
	switch first.StatusCode {
	case http.StatusPartialContent:
	case http.StatusRequestedRangeNotSatisfiable:
		// An empty file has no first byte to ask for.
		return downloadWhole(client, first, resp, opt, start, sum)
	default:
		return processResponse(first, resp, opt, start, sum)
	}

	// Bytes the transport decoded are not byte ranges of the file.
	if first.Uncompressed {
		return downloadWhole(client, first, resp, opt, start, sum)
	}

	resp.PopulateResponse(first, start)
	cr, err := segmentRange(first, 0, -1, -1)
	if err != nil {
		first.Body.Close()
		return resp, err
	}
	tag := options.StrongValidator(first.Header)
	if tag == "" || cr.Total < 0 {
		return downloadWhole(client, first, resp, opt, start, sum)
	}

	writer, err := opt.InitialiseWriter()
	if err != nil {
		first.Body.Close()
		return resp, fmt.Errorf("failed to initialise writer: %w", err)
	}
	file, ok := writer.(*options.FileWriter)
	if !ok {
		first.Body.Close()
		return resp, errors.Join(fmt.Errorf("segmented download has writer %T, want a file", writer), writer.Close())
	}

	err = fetchSegments(client, first, tag, cr, file.File, opt)
	for _, c := range []*checksum{sum, wholeDigest(first, opt)} {
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

// downloadWhole closes the first response and downloads the whole file in one
// request to the address the first request reached.
func downloadWhole(client *http.Client, first *http.Response, resp response.Response, opt *options.Option, start time.Time, sum *checksum) (response.Response, error) {
	if err := first.Body.Close(); err != nil {
		opt.Log("failed to close first segment", "error", err)
	}
	req := first.Request.Clone(first.Request.Context())
	req.Header.Del("Range")
	r, err := client.Do(req)
	if err != nil {
		return resp, err
	}
	return processResponse(r, resp, opt, start, sum)
}

// fetchSegments writes the first response and the rest of the file, in
// segments that download at the same time, to file. The first error cancels
// the other segments.
func fetchSegments(client *http.Client, first *http.Response, tag string, cr *response.ContentRange, file *os.File, opt *options.Option) error {
	if err := file.Truncate(cr.Total); err != nil {
		first.Body.Close()
		return fmt.Errorf("failed to size segmented download: %w", err)
	}

	ctx, cancel := context.WithCancelCause(first.Request.Context())
	defer cancel(nil)
	// The first body belongs to the parent context, so close it to stop it.
	stop := context.AfterFunc(ctx, func() { first.Body.Close() })
	defer stop()

	progress := &segmentProgress{report: opt.Progress.OnDownload, total: cr.Total}
	var wg sync.WaitGroup
	fail := func(err error) {
		if err != nil {
			cancel(err)
		}
	}
	wg.Go(func() {
		fail(writeSegment(first, cr.Start, cr.End, file, progress, opt))
	})
	for _, s := range splitSegments(cr.End+1, cr.Total, opt.Segments-1) {
		wg.Go(func() {
			fail(fetchSegment(ctx, client, first, tag, s[0], s[1], cr.Total, file, progress, opt))
		})
	}
	wg.Wait()
	return context.Cause(ctx)
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

// fetchSegment downloads the bytes from first to last of the version tag into
// file. The request repeats the request of the response origin. A response
// that is not that range of the version of origin fails with an error wrapping
// ErrRangeMismatch.
func fetchSegment(ctx context.Context, client *http.Client, origin *http.Response, tag string, first, last, total int64, file *os.File, progress *segmentProgress, opt *options.Option) error {
	req := origin.Request.Clone(ctx)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(first, 10)+"-"+strconv.FormatInt(last, 10))
	req.Header.Set("If-Range", tag)
	r, err := client.Do(req)
	if err != nil {
		return err
	}
	switch {
	case r.StatusCode == http.StatusOK:
		r.Body.Close()
		return fmt.Errorf("%w: the file changed on the server during the download", ErrRangeMismatch)
	case r.StatusCode != http.StatusPartialContent:
		r.Body.Close()
		return fmt.Errorf("segment %d-%d failed: %s", first, last, r.Status)
	}
	// A server that ignores If-Range sends a range of whichever version is
	// current, so compare every validator the responses carry.
	if err := sameVersion(r.Header, origin.Header); err != nil {
		r.Body.Close()
		return err
	}
	if _, err := segmentRange(r, first, last, total); err != nil {
		r.Body.Close()
		return err
	}
	return writeSegment(r, first, last, file, progress, opt)
}

// sameVersion returns an error wrapping ErrRangeMismatch when a segment's
// header names another version than the first response's header: a different
// ETag or a different Last-Modified. A validator only one of them has is not
// compared.
func sameVersion(segment, first http.Header) error {
	if a, b := segment.Get("ETag"), first.Get("ETag"); a != "" && b != "" && a != b {
		return fmt.Errorf("%w: ETag %s differs from %s", ErrRangeMismatch, a, b)
	}
	if a, b := segment.Get("Last-Modified"), first.Get("Last-Modified"); a != "" && b != "" && !sameTime(a, b) {
		return fmt.Errorf("%w: Last-Modified %s differs from %s", ErrRangeMismatch, a, b)
	}
	return nil
}

// segmentRange returns the range of a segment response, and an error wrapping
// ErrRangeMismatch unless the response is unencoded and holds bytes first to
// last of total. A last or total of -1 accepts any value.
func segmentRange(r *http.Response, first, last, total int64) (*response.ContentRange, error) {
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
	if cr.Unit != "bytes" || cr.Start != first || (last >= 0 && cr.End != last) || (total >= 0 && cr.Total != total) {
		return nil, fmt.Errorf("%w: segment is %s %d-%d/%d, want bytes %d-%d/%d",
			ErrRangeMismatch, cr.Unit, cr.Start, cr.End, cr.Total, first, last, total)
	}
	return cr, nil
}

// writeSegment writes the body of r, which holds bytes first to last, to file
// at offset first, and checks its Content-Digest.
func writeSegment(r *http.Response, first, last int64, file *os.File, progress *segmentProgress, opt *options.Option) error {
	defer r.Body.Close()

	var body io.Reader = r.Body
	var digest *checksum
	if !opt.SkipDigestCheck {
		digest = serverDigest(r, false)
	}
	if digest != nil {
		body = io.TeeReader(body, digest.hash)
	}

	expected := last - first + 1
	written, err := io.Copy(io.NewOffsetWriter(file, first), progress.reader(io.LimitReader(body, expected)))
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

// wholeDigest returns the server's checksum of the whole file from the first
// response of a segmented download, or nil when there is none to check.
func wholeDigest(first *http.Response, opt *options.Option) *checksum {
	if opt.SkipDigestCheck {
		return nil
	}
	return serverDigest(first, true)
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
