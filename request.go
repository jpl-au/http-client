package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jpl-au/http-client/form"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

const (
	SchemeHTTP      string = "http://"
	SchemeHTTPS     string = "https://"
	SchemeWS        string = "ws://"
	SchemeWSS       string = "wss://"
	ContentType     string = "Content-Type"
	ContentEncoding string = "Content-Encoding"
	URLencoded      string = "application/x-www-form-urlencoded"
)

// doRequest performs the HTTP request to the server/resource.
// Every outcome, success or failure, records the returned error in the
// response's Error field and the time the request took.
func doRequest(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	// Work on a private copy: the request writes headers and state into its
	// options, and the caller may reuse theirs for later requests.
	opt := options.New(opts...).Clone()
	start := time.Now()
	resp, err := send(method, url, payload, opt, start)
	resp.Error = err
	resp.AccessTime = time.Since(start)
	resp.ProcessedTime = time.Now().Unix()
	return resp, err
}

// send performs the HTTP request with the given options.
// This function orchestrates the entire request-response cycle, delegating
// to helper functions for transport configuration, payload preparation,
// and response processing. The http.Client follows redirects.
func send(method string, url string, payload any, opt *options.Option, start time.Time) (_ response.Response, err error) {

	opt.AddHeader("User-Agent", opt.UserAgent)

	// One identifier names the request in the trace header and in the response.
	// With tracing off, the response still needs its own identifier: a Client
	// keys its response history by it.
	id := opt.GenerateIdentifier()
	if opt.Tracing.Type != options.IdentifierNone {
		opt.AddHeader("X-Trace-ID", id)
	} else {
		id = rand.Text()
	}

	// Configure the HTTP client and transport
	client, release := configureClient(opt)
	defer release()

	if opt.StallTimeout > 0 {
		parent := opt.Context
		if parent == nil {
			parent = context.Background()
		}
		ctx, watch := watchStall(parent, opt.StallTimeout)
		opt.Context = ctx
		next := client.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		client.Transport = &stallTransport{next: next, watch: watch}
		defer func() {
			if err != nil && watch.stalled() {
				err = fmt.Errorf("%w: no data for %v: %w", ErrStalled, opt.StallTimeout, err)
			}
			watch.stop()
		}()
	}

	// Create the response before anything that can fail, so every failure
	// is recorded with the request's identifier.
	resp := response.New(id, url, method, payload, opt)

	// Normalise the URL
	url, err = normaliseURL(url, opt.Transport.Scheme)
	if err != nil {
		return resp, fmt.Errorf("supplied url did not pass url.Parse(): %w", err)
	}
	resp.URL = url

	sum, err := newChecksum(opt.Checksum)
	if err != nil {
		return resp, err
	}

	if opt.Range.IsResume {
		releasePartial, err := claimPartialFile(opt.ResponseWriter.FilePath)
		if err != nil {
			return resp, err
		}
		defer releasePartial()
	}

	if err := prepareResume(opt); err != nil {
		return resp, err
	}

	// A segmented download's segments are counted in bytes of the file, so
	// they must not be encoded.
	split := segmented(method, opt)
	if split {
		opt.Header.Set("Accept-Encoding", "identity")
	}

	source, err := preparePayload(payload, opt)
	if err != nil {
		return resp, err
	}

	req, err := prepareRequest(method, url, source, opt)
	if err != nil {
		return resp, err
	}
	if split {
		req.Header.Set("Range", "bytes=0-"+strconv.Itoa(segmentMin-1))
	}
	if req.Body != nil {
		// The transport closes the body, but may do so after Do returns.
		// Closing it here as well ensures an upload file is closed on return.
		defer func() {
			if err := req.Body.Close(); err != nil {
				opt.Log("failed to close request body", "error", err)
			}
		}()
	}

	// Log only parts that cannot carry credentials: the URL without user
	// information, query or fragment, and header names without values.
	logURL := *req.URL
	logURL.User = nil
	logURL.RawQuery = ""
	logURL.Fragment = ""
	logURL.RawFragment = ""
	opt.Log("sending request", "url", logURL.String(), "method", method, "headers", slices.Sorted(maps.Keys(req.Header)))
	resp.RequestTime = time.Now().Unix()

	httpResp, err := client.Do(req)
	if err != nil {
		// When a redirect policy rejects a redirect, Do also returns the
		// redirect response, with its body already closed.
		if httpResp != nil {
			resp.PopulateResponse(httpResp, start)
		}
		return resp, err
	}

	resp.ResponseTime = time.Now().Unix()

	// net/http returns a 307 or 308 unfollowed when it cannot send the body again.
	last := httpResp.Request
	if opt.Redirect.Follow &&
		(httpResp.StatusCode == http.StatusTemporaryRedirect || httpResp.StatusCode == http.StatusPermanentRedirect) &&
		httpResp.Header.Get("Location") != "" &&
		last.Body != nil && last.Body != http.NoBody && last.GetBody == nil {
		resp.PopulateResponse(httpResp, start)
		httpResp.Body.Close()
		return resp, ErrPayloadNotReplayable
	}

	if split {
		return downloadSegments(client, httpResp, resp, opt, start, sum)
	}

	// Process final response
	return processResponse(httpResp, resp, opt, start, sum)
}

// partialFiles holds the partial files that resumed downloads in this process
// use. A partial file has one resumed download at a time: another would read
// the file's size, and then the first would change it.
var partialFiles = struct {
	sync.Mutex
	paths map[string]bool
}{paths: make(map[string]bool)}

// claimPartialFile claims the partial file of a resumed download to dest, and
// returns a function that releases it. It returns an error wrapping
// ErrDownloadInProgress when another resumed download holds the file. The claim
// uses the absolute path, so it does not detect one file reached through a
// symbolic or hard link.
func claimPartialFile(dest string) (func(), error) {
	path, err := filepath.Abs(options.PartialPath(dest))
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

// prepareResume sets up a resumed download when the request starts.
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
func prepareResume(opt *options.Option) error {
	if !opt.Range.IsResume {
		return nil
	}
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

// configureClient returns an HTTP client for one request and a release function
// to call when the request is complete.
// The base client can be shared by concurrent requests, so its settings are
// copied into a new client and the base client is never written to.
// A transport set on the Option takes precedence over the base client's transport.
// Header-limit and protocol settings need a cloned transport; the clone serves
// only this request, and release closes its idle connections.
func configureClient(opt *options.Option) (*http.Client, func()) {
	base := opt.Client()
	client := &http.Client{
		Transport: base.Transport,
		Jar:       base.Jar,
		Timeout:   base.Timeout,
	}
	if opt.Transport.HTTP != nil {
		client.Transport = opt.Transport.HTTP
	}

	release := func() {}
	if opt.Transport.MaxResponseHeaderBytes != 0 || opt.Transport.Protocol != options.Both {
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if t, ok := transport.(*http.Transport); ok {
			t = t.Clone()
			applyTransportConfig(t, opt.Transport)
			client.Transport = t
			release = t.CloseIdleConnections
		}
	}

	// Redirects follow net/http rules: sensitive headers are dropped for another
	// host, 307 and 308 repeat the method and body, and 301, 302 and 303 change
	// a POST to a GET without a body.
	check := base.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !opt.Redirect.Follow {
			return http.ErrUseLastResponse
		}
		if len(via) > opt.Redirect.Max {
			return fmt.Errorf("%w: %d", ErrMaxRedirectsExceeded, opt.Redirect.Max)
		}
		// net/http copies every header to the next request. When the body is
		// dropped, the headers that describe it no longer apply.
		if req.Body == nil {
			req.Header.Del(ContentType)
			req.Header.Del(ContentEncoding)
			req.Header.Del("Content-Disposition")
		}
		if check != nil {
			return check(req, via)
		}
		return nil
	}

	return client, release
}

// applyTransportConfig applies header-limit and protocol settings to t.
func applyTransportConfig(t *http.Transport, cfg options.TransportConfig) {
	if cfg.MaxResponseHeaderBytes != 0 {
		t.MaxResponseHeaderBytes = cfg.MaxResponseHeaderBytes
	}

	switch cfg.Protocol {
	case options.HTTP1:
		t.Protocols = new(http.Protocols)
		t.Protocols.SetHTTP1(true)
	case options.HTTP2:
		t.Protocols = new(http.Protocols)
		t.Protocols.SetHTTP2(true)
	case options.UnencryptedHTTP2:
		t.Protocols = new(http.Protocols)
		t.Protocols.SetUnencryptedHTTP2(true)
	}
}

// uploadFile is a payload that names a file to upload. The file is prepared
// inside the request, so a file that cannot be read fails like any other
// request: the response records the error.
type uploadFile string

// payloadSource opens the request payload for one attempt.
type payloadSource struct {
	open       func() (io.Reader, error) // Returns the payload from its start.
	length     int64                     // Payload length in bytes, or -1 if unknown.
	replayable bool                      // open can be called again to send the payload on a redirect.
}

// preparePayload returns the source of the request payload, or nil when the
// request has no payload. A payload is sent with any method.
func preparePayload(payload any, opt *options.Option) (*payloadSource, error) {
	switch v := payload.(type) {
	case uploadFile:
		if err := opt.PrepareFile(string(v)); err != nil {
			return nil, err
		}
		payload = nil
	case url.Values:
		opt.AddHeader(ContentType, URLencoded)
		payload = v.Encode()
	case *form.Form:
		// The form checks its files and works out its length now, so a file
		// that cannot be read fails before the request is sent.
		length, err := v.Len()
		if err != nil {
			return nil, fmt.Errorf("failed to prepare form: %w", err)
		}
		opt.AddHeader(ContentType, v.ContentType())
		return &payloadSource{
			open: func() (io.Reader, error) {
				return v.Reader(), nil
			},
			length:     length,
			replayable: true,
		}, nil
	}

	// If payload is an *os.File and no file path is configured, extract the path
	// so we can reopen the file fresh for redirects/retries instead of reusing
	// the caller's handle (which may have an inconsistent position).
	if f, ok := payload.(*os.File); ok && !opt.HasFile() {
		info, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to stat file: %w", err)
		}
		opt.File.SetPath(f.Name())
		opt.File.SetSize(info.Size())
	}

	// Handle file uploads - open fresh each time
	if opt.HasFile() {
		return &payloadSource{
			open: func() (io.Reader, error) {
				file, err := opt.OpenFile()
				if err != nil {
					return nil, fmt.Errorf("failed to open file: %w", err)
				}
				return file, nil
			},
			length:     opt.Size(),
			replayable: true,
		}, nil
	}

	if payload == nil {
		return nil, nil
	}

	_, length, err := opt.CreatePayloadReader(payload)
	if err != nil {
		return nil, fmt.Errorf("unable to create payload reader: %w", err)
	}

	// CreatePayloadReader returns a new reader for byte payloads, and seeks a
	// seekable reader back to its start, so each call gives the whole payload.
	// net/http closes the body after sending it, so a closable reader cannot
	// be sent twice.
	var replayable bool
	switch payload.(type) {
	case []byte, string, *bytes.Buffer:
		replayable = true
	case io.Closer:
		replayable = false
	case io.Seeker:
		replayable = true
	}

	return &payloadSource{
		open: func() (io.Reader, error) {
			reader, _, err := opt.CreatePayloadReader(payload)
			return reader, err
		},
		length:     length,
		replayable: replayable,
	}, nil
}

// prepareRequest creates the HTTP request with its headers, cookies, range and body.
func prepareRequest(method, url string, source *payloadSource, opt *options.Option) (*http.Request, error) {
	ctx := opt.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	// Set headers and cookies
	req.Header = opt.Header
	for _, cookie := range opt.Cookies {
		req.AddCookie(cookie)
	}

	// Set Range header for partial content requests
	if opt.HasRange() {
		value, err := opt.Range.RangeHeader()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Range", value)
	}

	// Build the body last: it opens the payload and may start the compression
	// goroutine, so nothing after it may fail.
	if source != nil {
		if opt.Compression.Type != options.CompressionNone {
			opt.Header.Set("Transfer-Encoding", "chunked")
			opt.Header.Del("Content-Length")
			if opt.Compression.Type != options.CompressionCustom {
				opt.Header.Set(ContentEncoding, string(opt.Compression.Type))
			} else if opt.Compression.CustomType != "" {
				opt.Header.Set(ContentEncoding, string(opt.Compression.CustomType))
			} else {
				opt.Header.Set(ContentEncoding, "application/octet-stream")
			}
		}

		body, err := newBody(source, opt)
		if err != nil {
			return nil, err
		}
		req.Body = body
		req.ContentLength = -1
		if opt.Compression.Type == options.CompressionNone {
			req.ContentLength = source.length
		}
		if source.replayable {
			req.GetBody = func() (io.ReadCloser, error) {
				return newBody(source, opt)
			}
		}
	}

	return req, nil
}

// newBody opens the payload and builds the request body for one attempt:
// upload progress, then compression through a pipe.
func newBody(source *payloadSource, opt *options.Option) (io.ReadCloser, error) {
	payload, err := source.open()
	if err != nil {
		return nil, err
	}

	// Progress wrappers hide Close. The body keeps the closers so that closing
	// it closes a payload file or a caller's closable reader, as net/http would
	// for an unwrapped body, and unblocks the compression goroutine.
	body := &requestBody{}
	if c, ok := payload.(io.Closer); ok {
		body.closers = append(body.closers, c)
	}

	reader := payload
	if opt.Progress.OnUpload != nil && opt.ProgressTracking() == options.TrackBeforeCompression {
		reader = options.NewProgressReader(reader, source.length, opt.Progress.OnUpload)
	}

	if opt.Compression.Type != options.CompressionNone {
		pr, pw := io.Pipe()
		go compressData(pw, reader, opt)
		reader = pr
		body.closers = append(body.closers, pr)
	}

	if opt.Progress.OnUpload != nil && opt.ProgressTracking() == options.TrackAfterCompression {
		reader = options.NewProgressReader(reader, 0, opt.Progress.OnUpload)
	}

	body.Reader = reader
	return body, nil
}

// requestBody is a request body built from a chain of readers. Close closes
// every closer at the source of the chain, once.
type requestBody struct {
	io.Reader
	closers []io.Closer
	once    sync.Once
	err     error
}

// Close closes each closer and returns their joined errors.
// The transport and doRequest both close the body, so later calls return
// the result of the first.
func (b *requestBody) Close() error {
	b.once.Do(func() {
		var errs []error
		for _, c := range b.closers {
			errs = append(errs, c.Close())
		}
		b.err = errors.Join(errs...)
	})
	return b.err
}

// compressData handles the compression of request data in a goroutine.
func compressData(pw *io.PipeWriter, reader io.Reader, opt *options.Option) {
	compressor, err := opt.NewCompressor(pw)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("unsupported compression type: %s", opt.Compression.Type))
		return
	}

	var copyErr error
	if opt.Progress.UploadBufferSize != nil {
		buf := make([]byte, *opt.Progress.UploadBufferSize)
		_, copyErr = io.CopyBuffer(compressor, reader, buf)
	} else {
		_, copyErr = io.Copy(compressor, reader)
	}

	closeErr := compressor.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		pw.CloseWithError(err)
		return
	}
	pw.Close()
}

// processResponse handles the final response processing including decompression
// and body reading.
func processResponse(r *http.Response, resp response.Response, opt *options.Option, startTime time.Time, sum *checksum) (response.Response, error) {
	defer r.Body.Close()

	// Record what was received before reading the body, so a response that
	// fails later still has its status and headers.
	resp.PopulateResponse(r, startTime)

	// A response without a body leaves the output alone, so a file destination
	// keeps its content, and has nothing to decompress.
	if !hasBody(r) {
		return resp, nil
	}

	encoding := r.Header.Get("Content-Encoding")

	// A resumed download appends to the partial file, so a partial response
	// must continue the file exactly where it ends. Any other successful
	// response carries the whole representation and starts the partial file again.
	var resumed *response.ContentRange
	expected := int64(-1)
	if opt.Range.IsResume && r.StatusCode < http.StatusMultipleChoices {
		// Resume offsets describe the saved representation. Decoding an
		// encoded response would leave a prefix that cannot safely use its
		// validator or byte offsets, even on the first request or a restart.
		if r.Uncompressed || (encoding != "" && encoding != "identity") {
			return resp, fmt.Errorf("%w: resumable download requires an unencoded response", ErrRangeMismatch)
		}
		if r.StatusCode == http.StatusPartialContent {
			cr, err := resumeRange(r, opt.Range)
			if err != nil {
				return resp, err
			}
			resumed = cr
			expected = cr.End - cr.Start + 1
		} else {
			opt.Range.IsSet = false
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
		digest = serverDigest(r, resumed != nil)
	}

	// A resumed download is checked as a whole file, so the bytes already in
	// the partial file are hashed first.
	for _, c := range []*checksum{sum, digest} {
		if c != nil && resumed != nil {
			if err := c.hashPartialFile(opt.ResponseWriter.FilePath, resumed.Start); err != nil {
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
	defer decompressedBody.Close()

	// A file destination receives only a successful response. Any other
	// response goes to a buffer, so the caller can read the error body and
	// the file keeps its content.
	var writer io.WriteCloser
	if opt.ResponseWriter.Type == options.WriteToFile && r.StatusCode >= http.StatusMultipleChoices {
		writer = &options.WriteCloserBuffer{Buffer: &bytes.Buffer{}}
	} else {
		writer, err = opt.InitialiseWriter()
		if err != nil {
			return resp, fmt.Errorf("failed to initialise writer: %w", err)
		}
	}

	totalSize := r.ContentLength
	onDownload := opt.Progress.OnDownload

	// Progress on a resumed download covers the whole file: the bytes already
	// on disk count, and the total is the length of the representation.
	if onDownload != nil && resumed != nil {
		totalSize = resumed.Total
		report, offset := onDownload, resumed.Start
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
	incomplete := resumed != nil && resumed.Total >= 0 && resumed.End+1 < resumed.Total
	for _, c := range []*checksum{sum, digest} {
		if copyErr == nil && c != nil && !incomplete {
			copyErr = c.check()
		}
	}

	var closeErr error
	switch w := writer.(type) {
	case *options.FileWriter:
		// A file download that failed part way is discarded, so the
		// destination keeps its content.
		if copyErr != nil {
			closeErr = w.Discard()
		} else {
			closeErr = w.Close()
		}
	case *options.PartialWriter:
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
				ErrDownloadIncomplete, resumed.End+1, resumed.Total)
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

// resumeRange returns the range of a partial response to a resumed download.
// It returns an error wrapping ErrRangeMismatch unless the response has a valid
// bytes Content-Range that starts at the end of the partial file, and belongs
// to the representation the file holds: no content encoding, and no ETag or
// Last-Modified value that differs from the response that started the file.
func resumeRange(r *http.Response, rc options.RangeConfig) (*response.ContentRange, error) {
	if encoding := r.Header.Get(ContentEncoding); encoding != "" && encoding != "identity" {
		return nil, fmt.Errorf("%w: body has content encoding %q", ErrRangeMismatch, encoding)
	}
	if etag := r.Header.Get("ETag"); etag != "" && rc.ETag != "" && etag != rc.ETag {
		return nil, fmt.Errorf("%w: ETag %s differs from %s", ErrRangeMismatch, etag, rc.ETag)
	}
	if modified := r.Header.Get("Last-Modified"); modified != "" && rc.LastModified != "" && !sameTime(modified, rc.LastModified) {
		return nil, fmt.Errorf("%w: Last-Modified %s differs from %s", ErrRangeMismatch, modified, rc.LastModified)
	}

	cr, err := response.ParseContentRange(r.Header.Get("Content-Range"))
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

// sameTime reports whether two HTTP dates name the same time. Values that do
// not parse are compared as text.
func sameTime(a, b string) bool {
	ta, errA := http.ParseTime(a)
	tb, errB := http.ParseTime(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ta.Equal(tb)
}
