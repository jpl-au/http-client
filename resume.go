package client

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// partialFiles holds the partial files that resumed downloads in this process
// use. A partial file has one resumed download at a time: another would read
// the file's size, and then the first would change it.
var partialFiles = struct {
	sync.Mutex
	paths map[string]bool
}{paths: make(map[string]bool)}

// resumed is a download that continues its partial file (see Option.Resume).
type resumed struct {
	opt *options.Option
	sum *checksum // The checksum set with SetChecksum, or nil.
}

// claim claims the partial file, and returns a function that releases it. It
// returns an error wrapping ErrDownloadInProgress when another resumed
// download holds the file. The claim uses the absolute path, so it does not
// detect one file reached through a symbolic or hard link.
func (r *resumed) claim() (func(), error) {
	path, err := filepath.Abs(options.PartialPath(r.opt.ResponseWriter.FilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve partial file path: %w", err)
	}
	partialFiles.Lock()
	defer partialFiles.Unlock()
	if partialFiles.paths[path] {
		return nil, fmt.Errorf("%w: %s", ErrDownloadInProgress, path)
	}
	partialFiles.paths[path] = true
	return func() {
		partialFiles.Lock()
		delete(partialFiles.paths, path)
		partialFiles.Unlock()
	}, nil
}

// prepare sets up the request when it starts.
//
// The partial file continues from its size at that moment, and If-Range carries
// the validator of the representation the file holds: if the resource has
// changed, the server sends it whole and the file is replaced. Without a strong
// validator, nothing proves the bytes on disk belong to the current
// representation, so the download starts again. A missing or empty file needs
// no range.
//
// Resumed downloads ask for the identity encoding, because range offsets count
// encoded bytes and the file holds decoded ones.
func (r *resumed) prepare() error {
	opt := r.opt
	opt.Header.Set("Accept-Encoding", "identity")

	info, err := os.Stat(options.PartialPath(opt.ResponseWriter.FilePath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat partial file for resume: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}

	// If-Range needs a strong validator (RFC 9110, section 13.1.5). Without a
	// range set, the partial file starts again.
	validator := opt.Range.Validator
	if validator == "" || strings.HasPrefix(validator, "W/") {
		return nil
	}

	opt.Range.Start = info.Size()
	opt.Range.End = -1
	opt.Range.IsSet = true
	opt.Header.Set("If-Range", validator)
	return nil
}

// parseRange returns the range of a partial response. It returns an error
// wrapping ErrRangeMismatch unless the response has a valid bytes
// Content-Range that starts at the end of the partial file, and belongs to the
// representation the file holds: no content encoding, and no ETag or
// Last-Modified value that differs from the response that started the file.
func (r *resumed) parseRange(res *http.Response) (*response.ContentRange, error) {
	rc := r.opt.Range
	if encoding := res.Header.Get(ContentEncoding); encoding != "" && encoding != "identity" {
		return nil, fmt.Errorf("%w: body has content encoding %q", ErrRangeMismatch, encoding)
	}
	if err := checkVersion(res.Header, rc.ETag, rc.LastModified); err != nil {
		return nil, err
	}

	cr, err := response.ParseContentRange(res.Header.Get("Content-Range"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRangeMismatch, err)
	}
	if cr.Unit != "bytes" {
		return nil, fmt.Errorf("%w: unit is %q, not bytes", ErrRangeMismatch, cr.Unit)
	}
	if cr.Start != rc.Start {
		return nil, fmt.Errorf("%w: range starts at %d, file ends at %d", ErrRangeMismatch, cr.Start, rc.Start)
	}
	return cr, nil
}

// complete completes a download whose partial file holds at least the whole
// file of total bytes. The request asked for a range past the end of the
// partial file with a strong If-Range validator, so the server compared the
// range with the version the partial file holds (RFC 9110, section 13.1.5),
// and answered 416 with the file's size.
//
// A response that names another version fails and keeps the partial file. A
// partial file longer than the file is not a copy of it, so it is removed and
// the next resume starts again. Otherwise the partial file is published.
func (r *resumed) complete(res *http.Response, total int64) error {
	rc := r.opt.Range
	if err := checkVersion(res.Header, rc.ETag, rc.LastModified); err != nil {
		return err
	}
	if total < rc.Start {
		mismatch := fmt.Errorf("%w: the partial file has %d bytes, the file has %d; resume again to start again",
			ErrRangeMismatch, rc.Start, total)
		if err := os.Remove(options.PartialPath(r.opt.ResponseWriter.FilePath)); err != nil {
			return errors.Join(mismatch, fmt.Errorf("failed to remove partial file: %w", err))
		}
		return mismatch
	}
	return r.publish(total)
}

// publish publishes a partial file that holds the whole file of total bytes,
// after it matches the checksum set with SetChecksum, when there is one. A
// partial file that does not match is removed, because any of its bytes can be
// wrong.
func (r *resumed) publish(total int64) error {
	writer, err := r.opt.InitialiseWriter()
	if err != nil {
		return fmt.Errorf("failed to initialise writer: %w", err)
	}
	partial, ok := writer.(*options.PartialWriter)
	if !ok {
		return errors.Join(fmt.Errorf("resumed download has writer %T, want a partial file", writer), writer.Close())
	}
	if r.sum != nil {
		if err := r.sum.hashPartialFile(r.opt.ResponseWriter.FilePath, total); err != nil {
			return errors.Join(err, partial.Close())
		}
		if err := r.sum.check(); err != nil {
			return errors.Join(err, partial.Remove())
		}
	}
	return partial.Publish()
}
