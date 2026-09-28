package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jpl-au/http-client/download"
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
	URLEncoded      string = "application/x-www-form-urlencoded"
)

// doRequest performs the HTTP request to the server/resource.
// Every outcome, success or failure, records the returned error in the
// response's Error field and the time the request took.
func doRequest(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	// Work on a private copy: request setup adds headers and may prepare a
	// file upload, and the caller may reuse their options for later requests.
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
	// With tracing off, the response still needs its own identifier, because
	// a response history looks responses up by it.
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
		ctx, watch := newStallWatch(parent, opt.StallTimeout)
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

	// Prepare the payload first: PrepareFile can set a body even when the
	// method gets no payload, and a request with a body is never split.
	source, err := preparePayload(payload, opt)
	if err != nil {
		return resp, err
	}

	req, err := prepareRequest(method, url, source, opt)
	if err != nil {
		return resp, err
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
	transfer, err := download.New(req, client, opt)
	if err != nil {
		return resp, err
	}
	defer func() {
		if closeErr := transfer.Close(); closeErr != nil && !errors.Is(err, closeErr) {
			err = errors.Join(err, closeErr)
		}
	}()

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
			resp.Populate(httpResp, start)
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
		resp.Populate(httpResp, start)
		closeBody(opt, httpResp.Body)
		return resp, ErrPayloadNotReplayable
	}

	return transfer.Complete(httpResp, resp, start)
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
	if opt.Transport.MaxResponseHeaderBytes != 0 || opt.Transport.Protocol != options.HTTPAny {
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

// fileUpload is a payload that names a file to upload. The file is prepared
// inside the request, so a file that cannot be read fails like any other
// request: the response records the error.
type fileUpload string

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
	case fileUpload:
		if err := opt.PrepareFile(string(v)); err != nil {
			return nil, err
		}
		payload = nil
	case url.Values:
		opt.AddHeader(ContentType, URLEncoded)
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
			length:     opt.FileSize(),
			replayable: true,
		}, nil
	}

	if payload == nil {
		return nil, nil
	}

	_, length, err := opt.PayloadReader(payload)
	if err != nil {
		return nil, fmt.Errorf("unable to create payload reader: %w", err)
	}

	// PayloadReader returns a new reader for byte payloads, and seeks a
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
			reader, _, err := opt.PayloadReader(payload)
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
	req.Header = opt.Header.Clone()
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
			req.Header.Set("Transfer-Encoding", "chunked")
			req.Header.Del("Content-Length")
			if opt.Compression.Type != options.CompressionCustom {
				req.Header.Set(ContentEncoding, string(opt.Compression.Type))
			} else if opt.Compression.CustomType != "" {
				req.Header.Set(ContentEncoding, string(opt.Compression.CustomType))
			} else {
				req.Header.Set(ContentEncoding, "application/octet-stream")
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
	// CloseWithError and Close always return nil.
	if err := errors.Join(copyErr, closeErr); err != nil {
		pw.CloseWithError(err)
		return
	}
	pw.Close()
}

// closeBody logs a response-body close failure without changing the HTTP result.
// Download paths perform the same cleanup inside their package.
func closeBody(opt *options.Option, body io.Closer) {
	if err := body.Close(); err != nil {
		opt.Log("failed to close body", "error", err)
	}
}
