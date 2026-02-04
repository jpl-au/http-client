package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	netURL "net/url"
	"os"
	"time"

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

// requestState holds mutable state for a single request chain.
// Created fresh for each top-level request, passed through redirects.
type requestState struct {
	redirectCount int
}

// redirectLimitReached increments and checks if redirect limit is reached.
func (s *requestState) redirectLimitReached(max int) bool {
	s.redirectCount++
	return s.redirectCount > max
}

// doRequest performs the HTTP request to the server/resource.
// This function orchestrates the entire request-response cycle, delegating
// to helper functions for transport configuration, payload preparation,
// redirect handling, and response processing.
func doRequest(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	// Initialise options, combining defaults with user-provided options
	opt := options.New(opts...)

	// Create fresh request state for this request chain
	state := &requestState{}

	return doRequestWithState(method, url, payload, opt, state)
}

// doRequestWithState performs the HTTP request with the given state.
// This is the internal worker function that handles the actual request execution.
func doRequestWithState(method string, url string, payload any, opt *options.Option, state *requestState) (response.Response, error) {
	st := time.Now()

	opt.AddHeader("User-Agent", opt.UserAgent)

	if opt.Tracing.Type != options.IdentifierNone {
		opt.AddHeader("X-Trace-ID", opt.GenerateIdentifier())
	}

	// Configure the HTTP client and transport
	client := configureClient(opt, state)

	// Normalise the URL
	url, err := normaliseURL(url, opt.Transport.Scheme)
	if err != nil {
		return response.Response{}, fmt.Errorf("supplied url did not pass url.Parse(): %w", err)
	}

	// Set up base response object
	resp := response.New(url, method, payload, opt)

	// Prepare the payload (handles file opening and reader creation)
	payloadReader, contentLength, cleanupFn, err := preparePayload(method, payload, opt)
	if err != nil {
		return resp, err
	}

	// Prepare and execute the request
	req, err := prepareRequest(method, url, payloadReader, contentLength, opt)
	if err != nil {
		if cleanupFn != nil {
			cleanupFn()
		}
		return resp, err
	}

	opt.Log("sending request", "url", req.URL, "method", method, "headers", req.Header)
	resp.RequestTime = time.Now().Unix()

	httpResp, err := client.Do(req)
	if err != nil {
		if cleanupFn != nil {
			cleanupFn()
		}
		// If the request fails (e.g., DNS error, connection refused), we must close the request body
		// if it's a pipe to unblock the writer goroutine (see compressData).
		if req.Body != nil {
			req.Body.Close()
		}
		resp.Error = err
		return resp, err
	}

	resp.ResponseTime = time.Now().Unix()

	// Handle redirects - pass cleanup function to handleRedirect so it can
	// close the file after properly draining the response
	if isRedirect(httpResp.StatusCode) {
		return handleRedirect(httpResp, resp, method, payload, opt, state, cleanupFn)
	}

	// Process final response
	result, err := processResponse(httpResp, resp, opt, st)
	if cleanupFn != nil {
		cleanupFn()
	}
	return result, err
}

// configureClient sets up the HTTP client with appropriate transport settings.
func configureClient(opt *options.Option, state *requestState) *http.Client {
	client := opt.Client()

	// Clone transport if we need to modify per-request settings
	needsClone := opt.Transport.MaxResponseHeaderBytes != 0 || opt.Transport.Protocol != options.Both
	if client.Transport == nil {
		if needsClone && opt.Transport.HTTP != nil {
			client.Transport = opt.Transport.HTTP.Clone()
		} else {
			client.Transport = opt.Transport.HTTP
		}
	} else if needsClone {
		if t, ok := client.Transport.(*http.Transport); ok {
			client.Transport = t.Clone()
		}
	}

	// Apply MaxResponseHeaderBytes if configured
	if opt.Transport.MaxResponseHeaderBytes != 0 {
		if t, ok := client.Transport.(*http.Transport); ok {
			t.MaxResponseHeaderBytes = opt.Transport.MaxResponseHeaderBytes
		}
	}

	// Apply protocol configuration (HTTP/1, HTTP/2, or both)
	if opt.Transport.Protocol != options.Both {
		if t, ok := client.Transport.(*http.Transport); ok {
			if t.Protocols == nil {
				t.Protocols = new(http.Protocols)
			}
			switch opt.Transport.Protocol {
			case options.HTTP1:
				t.Protocols.SetHTTP1(true)
				t.Protocols.SetHTTP2(false)
			case options.HTTP2:
				t.Protocols.SetHTTP1(false)
				t.Protocols.SetHTTP2(true)
			case options.UnencryptedHTTP2:
				t.Protocols.SetHTTP1(false)
				t.Protocols.SetUnencryptedHTTP2(true)
			}
		}
	}

	// Disable automatic redirects - we handle them manually
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if state.redirectLimitReached(opt.Redirect.Max) {
			return fmt.Errorf("%w: %d", ErrMaxRedirectsExceeded, opt.Redirect.Max)
		}
		return http.ErrUseLastResponse
	}

	return client
}

// preparePayload creates the payload reader for the request.
// Returns a cleanup function that should be deferred if non-nil.
func preparePayload(method string, payload any, opt *options.Option) (io.Reader, int64, func(), error) {
	// Only POST, PUT, PATCH can have payloads
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch {
		return nil, 0, nil, nil
	}

	// If payload is an *os.File and no file path is configured, extract the path
	// so we can reopen the file fresh for redirects/retries instead of reusing
	// the caller's handle (which may have an inconsistent position).
	if f, ok := payload.(*os.File); ok && !opt.HasFile() {
		info, err := f.Stat()
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to stat file: %w", err)
		}
		opt.File.SetPath(f.Name())
		opt.File.SetSize(info.Size())
	}

	// Handle file uploads - open fresh each time
	if opt.HasFile() {
		file, err := opt.OpenFile()
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to open file: %w", err)
		}
		reader, length, err := opt.CreatePayloadReader(file)
		if err != nil {
			file.Close()
			return nil, 0, nil, fmt.Errorf("unable to create payload reader: %w", err)
		}
		return reader, length, func() { file.Close() }, nil
	}

	// Handle other payloads
	if payload != nil {
		reader, length, err := opt.CreatePayloadReader(payload)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("unable to create payload reader: %w", err)
		}
		return reader, length, nil, nil
	}

	return nil, 0, nil, nil
}

// handleRedirect processes a redirect response and follows it if configured.
// cleanupFn closes any resources (e.g., file handles) from the original request.
func handleRedirect(httpResp *http.Response, resp response.Response, method string, payload any, opt *options.Option, state *requestState, cleanupFn func()) (response.Response, error) {
	st := time.Now()

	// Helper to drain and close response, then cleanup file handle
	closeAll := func() {
		// Drain response body to ensure transport is done with the request.
		// Error intentionally ignored - we're just draining before close.
		_, _ = io.Copy(io.Discard, httpResp.Body)
		httpResp.Body.Close()
		// Now safe to close the file handle
		if cleanupFn != nil {
			cleanupFn()
		}
	}

	// If redirects are not allowed, return the redirect response immediately
	if !opt.Redirect.Follow {
		resp.PopulateResponse(httpResp, st)
		httpResp.Body.Close()
		if cleanupFn != nil {
			cleanupFn()
		}
		return resp, nil
	}

	redirectURL := httpResp.Header.Get("Location")
	if redirectURL == "" {
		closeAll()
		return resp, ErrRedirectMissingLocation
	}

	// Parse and resolve the redirect URL
	parsedRedirect, err := netURL.Parse(redirectURL)
	if err != nil {
		closeAll()
		return resp, fmt.Errorf("invalid redirect URL: %w", err)
	}

	nextURL := httpResp.Request.URL.ResolveReference(parsedRedirect).String()
	closeAll()

	// Handle the redirect
	if opt.Redirect.PreserveMethod {
		var newPayload any

		// For file uploads, OpenFile will be called again in the recursive doRequestWithState
		// For non-file payloads, recreate them
		if !opt.HasFile() && payload != nil {
			switch v := payload.(type) {
			case []byte:
				newPayload = v
			case *bytes.Buffer:
				newPayload = bytes.NewBuffer(v.Bytes())
			case string:
				newPayload = v
			case io.Seeker:
				// For seekable readers (including *os.File), seek back to start
				if _, err := v.Seek(0, io.SeekStart); err != nil {
					return resp, fmt.Errorf("failed to seek payload for redirect: %w", err)
				}
				newPayload = payload
			case io.Reader:
				// Non-seekable io.Reader - attempt to buffer for replay
				buffered, err := bufferReaderForReplay(v, options.MaxReplayableBodySize)
				if err != nil {
					return resp, err
				}
				newPayload = buffered
			}
		}

		return doRequestWithState(method, nextURL, newPayload, opt, state)
	}

	// Switch to GET method as per HTTP spec for other redirects
	return doRequestWithState(http.MethodGet, nextURL, nil, opt, state)
}

// isRedirect checks if the status code indicates a redirect.
func isRedirect(statusCode int) bool {
	return statusCode == http.StatusMovedPermanently ||
		statusCode == http.StatusFound ||
		statusCode == http.StatusSeeOther ||
		statusCode == http.StatusTemporaryRedirect ||
		statusCode == http.StatusPermanentRedirect
}

// bufferReaderForReplay reads a non-seekable io.Reader into a byte slice for replay.
// Returns ErrPayloadNotReplayable if the reader exceeds the max buffer size.
func bufferReaderForReplay(r io.Reader, maxSize int64) ([]byte, error) {
	// Use LimitReader to cap memory usage, read one extra byte to detect overflow
	limited := io.LimitReader(r, maxSize+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to buffer payload for replay: %w", err)
	}
	if int64(len(buf)) > maxSize {
		return nil, fmt.Errorf("%w: size exceeds %d bytes (use []byte or a seekable reader for large payloads)", ErrPayloadNotReplayable, maxSize)
	}
	return buf, nil
}

// prepareRequest creates and configures the HTTP request with compression
// and progress tracking.
func prepareRequest(method, url string, payloadReader io.Reader, contentLength int64, opt *options.Option) (*http.Request, error) {
	var reader io.Reader = payloadReader

	// Add progress tracking before compression if specified
	if reader != nil && opt.Progress.OnUpload != nil &&
		(method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) {

		if opt.ProgressTracking() == options.TrackBeforeCompression {
			var totalSize int64 = contentLength
			if sizer, ok := payloadReader.(io.Seeker); ok {
				if size, err := sizer.Seek(0, io.SeekEnd); err == nil {
					if _, err := sizer.Seek(0, io.SeekStart); err == nil {
						totalSize = size
					}
					// If seek back fails, use contentLength as fallback
				}
			}
			reader = options.NewProgressReader(payloadReader, totalSize, opt.Progress.OnUpload)
		}
	}

	// Handle compression via pipe
	var pipeReader *io.PipeReader
	if reader != nil && opt.Compression.Type != options.CompressionNone {
		pr, pw := io.Pipe()
		pipeReader = pr
		go compressData(pw, reader, opt)
		reader = pr
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

	// Add progress tracking after compression if specified
	if reader != nil && opt.Progress.OnUpload != nil &&
		(method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) &&
		opt.ProgressTracking() == options.TrackAfterCompression {
		reader = options.NewProgressReader(reader, 0, opt.Progress.OnUpload)
	}

	// Create the request with context
	ctx := opt.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		if pipeReader != nil {
			pipeReader.Close()
		}
		return nil, err
	}

	// Set content length
	if reader == nil {
		req.ContentLength = 0
	} else if opt.Compression.Type == options.CompressionNone {
		req.ContentLength = contentLength
	}

	// Set headers and cookies
	req.Header = opt.Header
	for _, cookie := range opt.Cookies {
		req.AddCookie(cookie)
	}

	// Set Range header for partial content requests
	if opt.HasRange() {
		req.Header.Set("Range", opt.Range.RangeHeader())
	}

	return req, nil
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
func processResponse(r *http.Response, resp response.Response, opt *options.Option, startTime time.Time) (response.Response, error) {
	defer r.Body.Close()

	encoding := r.Header.Get("Content-Encoding")

	decompressedBody, err := opt.NewDecompressor(r.Body, encoding)
	if err != nil {
		return resp, fmt.Errorf("failed to create decompressed reader: %w", err)
	}
	defer decompressedBody.Close()

	writer, err := opt.InitialiseWriter()
	if err != nil {
		return resp, fmt.Errorf("failed to initialise writer: %w", err)
	}

	totalSize := r.ContentLength

	var reader io.Reader = decompressedBody
	if opt.Progress.OnDownload != nil {
		if encoding != "" {
			totalSize = -1
		}
		reader = options.NewProgressReader(decompressedBody, totalSize, opt.Progress.OnDownload)
	}

	var copyErr error
	if opt.Progress.DownloadBufferSize != nil {
		buf := make([]byte, *opt.Progress.DownloadBufferSize)
		_, copyErr = io.CopyBuffer(writer, reader, buf)
	} else {
		_, copyErr = io.Copy(writer, reader)
	}

	closeErr := writer.Close()
	if copyErr != nil || closeErr != nil {
		err = errors.Join(copyErr, closeErr)
		resp.Error = err
		return resp, err
	}

	if buf, ok := writer.(*options.WriteCloserBuffer); ok {
		resp.Body = *buf
	}

	resp.ProcessedTime = time.Now().Unix()
	resp.PopulateResponse(r, startTime)

	return resp, nil
}
