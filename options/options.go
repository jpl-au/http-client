package options

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ua defines the default User-Agent string for requests
const ua = "jpl-au/http-client/v0.1.0"

// Common errors returned by Option methods
var (
	ErrInvalidWriterType  = errors.New("invalid writer type")
	ErrMissingFilePath    = errors.New("file path must be specified when using WriteToFile")
	ErrUnexpectedFilePath = errors.New("filepath should not be provided when using WriteToBuffer")
	ErrInvalidCompression = errors.New("unsupported compression type")
	ErrFileNotFound       = errors.New("file does not exist")
	ErrFileNotPrepared    = errors.New("no file prepared: call PrepareFile first")
)

// MaxReplayableBodySize is the maximum size of a non-seekable request body
// that will be buffered for redirect/retry replay. Bodies larger than this limit
// that cannot be seeked (like streaming io.Reader) will fail on redirect/retry
// with ErrPayloadNotReplayable.
const MaxReplayableBodySize = 10 * 1024 * 1024 // 10MB

// Option provides configuration for HTTP requests. It allows customization of various aspects
// of the request including headers, compression, logging, response handling, and progress tracking.
// If no options are provided when making a request, a default configuration is automatically generated.
//
// Options are safe for concurrent use when accessed through their getter/setter methods,
// which use internal mutex protection. Direct field access should be avoided in concurrent contexts.
type Option struct {
	initialised     bool              // Internal - determine if the struct was initialised with a call to New()
	useSharedClient bool              // Internal - use of the shared *http.Client or the client bound to the Option struct
	client          *http.Client      // Default or custom *http.Client
	mu              sync.RWMutex      // mutex for concurrent access
	Context         context.Context   // Optional context for request cancellation and timeouts. If nil, context.Background() is used.
	Logging         LoggingConfig     // Logging configuration
	Header          http.Header       // Headers to be included in the request
	Cookies         []*http.Cookie    // Cookies to be included in the request
	Compression     CompressionConfig // Compression configuration
	UserAgent       string            // User Agent to send with requests
	Redirect        RedirectConfig    // Redirect configuration
	Tracing         TracingConfig     // Request tracing configuration
	Transport       TransportConfig   // Transport and protocol settings
	File            FileConfig        // File upload metadata
	ResponseWriter  ResponseWriter    // Define the type of response writer
	Progress        ProgressConfig    // Progress tracking configuration
	Range           RangeConfig       // Range request configuration for partial downloads
}

// New creates a default Option with pre-configured settings. If additional options are provided
// via the variadic parameter, they will be merged with the default settings, with the provided
// options taking precedence.
func New(opts ...*Option) *Option {
	if len(opts) > 0 && opts[0] != nil {
		// if the variadic parameter Option
		if opts[0].initialised {
			return opts[0]
		}
		// If opts[0] is not initialized, initialize and merge it
		opt := defaultOption()
		opt.Merge(opts[0])
		return opt
	}
	// No options provided or nil option; return a new default Option
	return defaultOption()
}

// defaultOption initializes and returns a default Option with pre-configured settings.
func defaultOption() *Option {
	return &Option{
		initialised:     true,
		useSharedClient: true, // Default to using shared client (http.DefaultClient)
		client:          nil,  // Don't create a client yet
		Logging:         defaultLoggingConfig(),
		Header:          http.Header{},
		Compression:     defaultCompressionConfig(),
		UserAgent:       ua,
		Redirect:        defaultRedirectConfig(),
		Tracing:         defaultTracingConfig(),
		Transport:       defaultTransportConfig(),
		ResponseWriter: ResponseWriter{
			Type: WriteToBuffer,
		},
		Progress: defaultProgressConfig(),
	}
}

// Client returns the HTTP client to be used for requests.
// If a custom client has been set via SetClient, that client is returned.
// Otherwise, returns a new default http.Client instance.
func (opt *Option) Client() *http.Client {
	opt.mu.RLock()
	useShared := opt.useSharedClient
	client := opt.client
	opt.mu.RUnlock()

	if useShared {
		// Return a new Client struct that shares the DefaultClient's Transport
		// for connection pooling, but allows per-request CheckRedirect configuration
		// without mutating the global http.DefaultClient.
		return &http.Client{
			Transport: http.DefaultClient.Transport,
			Jar:       http.DefaultClient.Jar,
			Timeout:   http.DefaultClient.Timeout,
		}
	}

	// For per-request client or custom client cases
	if client == nil {
		// Create new client only if we're not using shared client
		opt.mu.Lock()
		// Double-check after acquiring write lock
		if opt.client == nil {
			opt.client = &http.Client{
				Transport: cloneTransport(),
				Timeout:   30 * time.Second,
			}
		}
		client = opt.client
		opt.mu.Unlock()
	}
	return client
}

// SetClient configures a custom HTTP client to be used for requests.
// This client will be used instead of the default client for all subsequent
// requests made with this Option instance. The provided client should be
// configured with any desired settings (timeouts, transport, etc) before
// being set.
//
// Note: If the provided client has a nil Transport, the library will use
// http.DefaultTransport as a fallback. Be aware that any modifications to
// this transport will affect the global default. If you require isolation,
// ensure your custom client has its own Transport configured.
func (opt *Option) SetClient(client *http.Client) *Option {
	if client == nil {
		opt.Log("client cannot be nil")
		return opt
	}
	opt.mu.Lock()
	opt.useSharedClient = false // Disable shared client when setting custom
	opt.client = client
	opt.mu.Unlock()
	return opt
}

// UseSharedClient enables the use of the shared HTTP client for this Option instance.
// Using a shared client provides better performance through connection pooling and reuse,
// especially when making multiple requests to the same host. This is the default behavior.
// The shared client is thread-safe and can be used concurrently across multiple goroutines.
func (opt *Option) UseSharedClient() *Option {
	opt.mu.Lock()
	opt.useSharedClient = true
	opt.client = nil // Clear any custom client
	opt.mu.Unlock()
	return opt
}

// UsePerRequestClient disables the use of the shared HTTP client for this Option instance.
// This creates a new client for each request, providing better isolation at the cost of
// performance. Use this when you need complete isolation between requests or when you want
// to customize client behavior for specific requests without affecting other requests.
// UsePerRequestClient disables shared client and ensures a new client is created
func (opt *Option) UsePerRequestClient() *Option {
	opt.mu.Lock()
	opt.useSharedClient = false
	opt.client = nil // Force creation of new client in GetClient
	opt.mu.Unlock()
	return opt
}

// AddHeader adds a new header with the specified key and value to the request headers.
// If the headers map hasn't been initialized, it will be created.
// Kept for backwards compatability
func (opt *Option) AddHeader(key string, value string) *Option {
	opt.mu.Lock()
	if opt.Header == nil {
		opt.Header = http.Header{}
	}
	// Use set over add to replace the key with the value
	opt.Header.Set(key, value)
	opt.mu.Unlock()
	return opt
}

// ClearHeaders removes all previously set headers from the Option.
func (opt *Option) ClearHeaders() *Option {
	opt.mu.Lock()
	opt.Header = http.Header{}
	opt.mu.Unlock()
	return opt
}

// AddCookie adds a new cookie to the Option's cookie collection.
// If the cookie slice hasn't been initialized, it will be created.
func (opt *Option) AddCookie(cookie *http.Cookie) *Option {
	opt.mu.Lock()
	if opt.Cookies == nil {
		opt.Cookies = []*http.Cookie{}
	}
	opt.Cookies = append(opt.Cookies, cookie)
	opt.mu.Unlock()
	return opt
}

// ClearCookies removes all previously set cookies from the Option.
func (opt *Option) ClearCookies() *Option {
	opt.mu.Lock()
	opt.Cookies = []*http.Cookie{}
	opt.mu.Unlock()
	return opt
}

// CreatePayloadReader converts the given payload into an io.Reader along with its size.
// Supported payload types include:
//   - nil: Returns a nil reader and a size of -1.
//   - []byte: Returns a bytes.Reader for the byte slice and its length as size.
//   - *bytes.Buffer: Returns a bytes.Reader for the buffer data and its length as size.
//   - io.Reader: Returns the reader and attempts to determine its size if it implements io.Seeker.
//   - string: Returns a strings.Reader for the string and its length as size.
//
// For unsupported payload types, an error is returned.
func (opt *Option) CreatePayloadReader(payload any) (io.Reader, int64, error) {
	switch v := payload.(type) {
	case nil:
		// No payload, return nil reader and size -1
		return nil, -1, nil
	case []byte:
		// Byte slice payload, return bytes.Reader and its length
		opt.Log("Setting payload reader", "reader", "bytes.Reader")
		return bytes.NewReader(v), int64(len(v)), nil
	case *bytes.Buffer:
		opt.Log("Setting payload reader", "reader", "bytes.Buffer")
		return bytes.NewReader(v.Bytes()), int64(v.Len()), nil
	case io.Reader:
		// io.Reader payload - check if it's seekable
		if seeker, ok := v.(io.Seeker); ok {
			// Seekable reader - determine size and ensure we're at the start
			size := int64(-1)
			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				opt.Log("failed to seek to start", "error", err)
			} else if endPos, err := seeker.Seek(0, io.SeekEnd); err != nil {
				opt.Log("failed to seek to end for size detection", "error", err)
			} else {
				size = endPos
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					opt.Log("failed to reset seek position", "error", err)
				}
			}
			opt.Log("Setting payload reader", "reader", "io.Seeker", "size", size)
			return v, size, nil
		}

		// Non-seekable reader - pass through as-is
		// Note: If a redirect/retry occurs with this reader, it cannot be replayed
		// and will result in an error at that point
		opt.Log("Setting payload reader", "reader", "io.Reader (non-seekable)", "size", -1)
		return v, -1, nil
	case string:
		// String payload, return strings.Reader and its length
		opt.Log("Setting payload reader", "reader", "strings.Reader")
		return strings.NewReader(v), int64(len(v)), nil
	default:
		// Unsupported payload type, return an error
		return nil, -1, fmt.Errorf("unsupported payload type: %T", payload)
	}
}

// SetContext configures an optional context for the request.
//
// The context can be used for:
//   - Request cancellation: Cancel in-flight requests when the context is cancelled
//   - Timeouts: Set deadlines for request completion using context.WithTimeout()
//   - Request-scoped values: Pass values through the request chain
//
// If no context is set (nil), context.Background() is used by default.
//
// Example usage:
//
//	// With timeout
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	opt := options.New().SetContext(ctx)
//	resp, err := client.Get(url, opt)
//
//	// With cancellation
//	ctx, cancel := context.WithCancel(context.Background())
//	go func() {
//	    <-someSignal
//	    cancel()
//	}()
//	opt := options.New().SetContext(ctx)
//
// This method returns the Option pointer for method chaining.
func (opt *Option) SetContext(ctx context.Context) *Option {
	opt.mu.Lock()
	opt.Context = ctx
	opt.mu.Unlock()
	return opt
}

// Merge combines the settings from another Option instance into this one.
// Settings from the source Option take precedence over existing settings.
// This includes headers, cookies, compression settings, and all other configuration options.
func (opt *Option) Merge(src *Option) *Option {
	if src == nil {
		return opt
	}
	opt.mu.Lock()
	// Merge Headers
	if opt.Header == nil {
		opt.Header = make(http.Header)
	}
	// Replaces any existing values
	for key, values := range src.Header {
		opt.Header[key] = values
	}

	// Merge Cookies
	for _, sc := range src.Cookies {
		found := false
		for i, tc := range opt.Cookies {
			if tc.Name == sc.Name {
				opt.Cookies[i] = sc
				found = true
				break
			}
		}
		if !found {
			opt.Cookies = append(opt.Cookies, sc)
		}
	}

	// Merge boolean and primitive fields only if source was properly initialized
	if src.initialised {
		opt.Logging.Enabled = src.Logging.Enabled
		opt.Redirect.Follow = src.Redirect.Follow
		opt.Redirect.PreserveMethod = src.Redirect.PreserveMethod
	}
	if src.Redirect.Max != 0 {
		opt.Redirect.Max = src.Redirect.Max
	}

	if src.Logging.Logger != (slog.Logger{}) {
		opt.Logging.Logger = src.Logging.Logger
	}

	// Merge transport config
	if src.Transport.HTTP != nil {
		opt.Transport.HTTP = src.Transport.HTTP
	}
	if src.Transport.MaxResponseHeaderBytes != 0 {
		opt.Transport.MaxResponseHeaderBytes = src.Transport.MaxResponseHeaderBytes
	}
	if src.Transport.Protocol != Both {
		opt.Transport.Protocol = src.Transport.Protocol
	}
	if src.Transport.Scheme != "" {
		opt.Transport.Scheme = src.Transport.Scheme
	}

	if src.Context != nil {
		opt.Context = src.Context
	}

	if src.ResponseWriter.Type != "" {
		opt.ResponseWriter = src.ResponseWriter
	}

	// Merge compression config
	if src.Compression.Type != "" {
		opt.Compression.Type = src.Compression.Type
	}
	if src.Compression.CustomType != "" {
		opt.Compression.CustomType = src.Compression.CustomType
	}
	if src.Compression.Compressor != nil {
		opt.Compression.Compressor = src.Compression.Compressor
	}
	if src.Compression.Decompressor != nil {
		opt.Compression.Decompressor = src.Compression.Decompressor
	}

	if src.UserAgent != "" {
		opt.UserAgent = src.UserAgent
	}

	// Merge tracing config
	if src.Tracing.Type != "" {
		opt.Tracing.Type = src.Tracing.Type
	}

	// Merge file config
	if src.File.path != "" {
		opt.File.path = src.File.path
		opt.File.size = src.File.size
	}

	// Merge progress config
	if src.Progress.DownloadBufferSize != nil {
		opt.Progress.DownloadBufferSize = src.Progress.DownloadBufferSize
	}
	if src.Progress.UploadBufferSize != nil {
		opt.Progress.UploadBufferSize = src.Progress.UploadBufferSize
	}
	if src.Progress.OnUpload != nil {
		opt.Progress.OnUpload = src.Progress.OnUpload
	}
	if src.Progress.OnDownload != nil {
		opt.Progress.OnDownload = src.Progress.OnDownload
	}
	if src.initialised {
		opt.Progress.Tracking = src.Progress.Tracking
	}

	// Merge range config
	if src.Range.IsSet {
		opt.Range = src.Range
	}
	opt.mu.Unlock()
	return opt
}

// Clone creates an independent deep copy of the Option.
//
// This method creates a copy of all configuration including:
//   - Headers and cookies (deep copied)
//   - Logging, compression, redirect, and transport settings
//   - Progress callbacks and buffer sizes
//   - Range configuration
//
// The returned Option can be modified without affecting the original.
//
// Note: Function references (callbacks) are copied by reference,
// not cloned. Modifying the underlying function behavior will affect all copies.
func (opt *Option) Clone() *Option {
	clone := New()

	opt.mu.RLock()
	// Deep clone the http.Header
	clone.Header = make(http.Header)
	for key, values := range opt.Header {
		clone.Header[key] = make([]string, len(values))
		copy(clone.Header[key], values)
	}

	// Deep clone http.Cookies
	clone.Cookies = make([]*http.Cookie, len(opt.Cookies))
	for i, cookie := range opt.Cookies {
		clone.Cookies[i] = &http.Cookie{
			Name:       cookie.Name,
			Value:      cookie.Value,
			Path:       cookie.Path,
			Domain:     cookie.Domain,
			Expires:    cookie.Expires,
			RawExpires: cookie.RawExpires,
			MaxAge:     cookie.MaxAge,
			Secure:     cookie.Secure,
			HttpOnly:   cookie.HttpOnly,
			SameSite:   cookie.SameSite,
			Raw:        cookie.Raw,
			Unparsed:   append([]string{}, cookie.Unparsed...),
		}
	}

	// Copy configuration fields
	clone.Logging = opt.Logging
	clone.Compression = opt.Compression
	clone.UserAgent = opt.UserAgent
	clone.Redirect = opt.Redirect
	clone.Tracing.Type = opt.Tracing.Type

	// Transport is shallow-copied intentionally. The *http.Transport pointer is treated
	// as read-only; transport settings (MaxResponseHeaderBytes, Protocol) are stored as
	// values in TransportConfig and applied to a cloned transport at request time in
	// configureClient(). This avoids shared state bugs while allowing transport reuse.
	clone.Transport = opt.Transport
	clone.Context = opt.Context
	clone.ResponseWriter = opt.ResponseWriter
	clone.File = opt.File
	clone.Range = opt.Range

	// Copy progress config with deep copy of pointer fields
	clone.Progress.Tracking = opt.Progress.Tracking
	clone.Progress.OnUpload = opt.Progress.OnUpload
	clone.Progress.OnDownload = opt.Progress.OnDownload
	if opt.Progress.UploadBufferSize != nil {
		size := *opt.Progress.UploadBufferSize
		clone.Progress.UploadBufferSize = &size
	}
	if opt.Progress.DownloadBufferSize != nil {
		size := *opt.Progress.DownloadBufferSize
		clone.Progress.DownloadBufferSize = &size
	}
	opt.mu.RUnlock()

	return clone
}
