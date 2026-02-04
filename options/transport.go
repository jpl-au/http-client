package options

import "net/http"

// Protocol defines which HTTP protocol versions to use for requests.
// This type leverages Go 1.24's Transport.Protocols field to provide explicit
// control over HTTP version negotiation.
//
// By default, Go's HTTP client uses HTTP/1.1 for unencrypted connections (http://)
// and negotiates HTTP/2 via ALPN for TLS connections (https://). The Protocol type
// allows overriding this behaviour for specific use cases.
//
// Note: Changing the protocol affects connection behaviour. HTTP/2 uses multiplexed
// streams over a single connection, whilst HTTP/1.1 may open multiple connections.
// Consider connection pooling implications when selecting a protocol.
type Protocol int

// Protocol constants for HTTP version selection.
//
// Usage considerations:
//   - Both: Recommended for most applications. Provides automatic protocol selection
//     based on the URL scheme and server capabilities.
//   - HTTP1: Use when targeting servers that have HTTP/2 compatibility issues, or when
//     debugging protocol-specific behaviour.
//   - HTTP2: Use when HTTP/2 features (multiplexing, header compression) are required
//     and the target server is known to support HTTP/2. Note: Only works with https://
//     URLs as HTTP/2 requires TLS for protocol negotiation via ALPN.
//   - UnencryptedHTTP2: Use for HTTP/2 over plaintext (h2c), typically in controlled
//     environments such as internal services or behind a TLS-terminating proxy. The
//     server must explicitly support h2c; most public servers do not.
const (
	// Both uses HTTP/1.1 for http:// URLs and HTTP/2 for https:// URLs.
	// This is the default behaviour and matches Go's standard HTTP client.
	Both Protocol = iota

	// HTTP1 forces HTTP/1.1 for all requests, regardless of URL scheme.
	// Useful for compatibility with servers that have HTTP/2 issues.
	HTTP1

	// HTTP2 forces HTTP/2 for all requests. Requires https:// URLs as HTTP/2
	// depends on TLS for protocol negotiation (ALPN). Requests to http:// URLs
	// will fail when this protocol is selected.
	HTTP2

	// UnencryptedHTTP2 enables HTTP/2 over plaintext TCP (h2c). This is an
	// advanced option for use in controlled environments where TLS is not
	// required. The target server must support HTTP/2 cleartext upgrades.
	UnencryptedHTTP2
)

// TransportConfig holds transport and protocol settings.
type TransportConfig struct {
	// HTTP is the HTTP transport to use for requests.
	HTTP *http.Transport

	// MaxResponseHeaderBytes limits the size of response headers including 1xx responses.
	// 0 uses Go's default (1MB).
	MaxResponseHeaderBytes int64

	// Protocol specifies HTTP protocol version selection (Both, HTTP1, HTTP2, UnencryptedHTTP2).
	Protocol Protocol

	// Scheme defines the protocol scheme (e.g., "https://", "http://") for requests.
	Scheme string
}

// defaultTransportConfig returns the default transport configuration.
// Uses cloneTransport() to ensure each Option gets an isolated copy,
// preventing mutations from affecting the global http.DefaultTransport.
func defaultTransportConfig() TransportConfig {
	return TransportConfig{
		HTTP:     cloneTransport(),
		Protocol: Both,
	}
}

// cloneTransport creates a clone of http.DefaultTransport if it's a *http.Transport,
// otherwise returns a new default Transport.
func cloneTransport() *http.Transport {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		return t.Clone()
	}
	return &http.Transport{}
}

// SetTransport configures a custom HTTP transport for the requests.
// This allows fine-grained control over connection pooling, timeouts, and other transport-level settings.
func (opt *Option) SetTransport(transport *http.Transport) *Option {
	opt.mu.Lock()
	opt.Transport.HTTP = transport
	opt.mu.Unlock()
	return opt
}

// SetMaxResponseHeaderBytes configures the maximum number of bytes allowed for response headers,
// including all 1xx informational responses. This is particularly relevant for APIs that send
// multiple 1xx responses (e.g., 100 Continue, 103 Early Hints). A value of 0 uses Go's default
// of 1MB.
//
// The value is stored in Option and applied to a cloned transport at request time,
// ensuring thread-safety and avoiding mutation of shared transport instances.
func (opt *Option) SetMaxResponseHeaderBytes(size int64) *Option {
	opt.mu.Lock()
	opt.Transport.MaxResponseHeaderBytes = size
	opt.mu.Unlock()
	return opt
}

// SetProtocol configures which HTTP protocol version(s) to use for requests.
//
// The default is Both, which uses HTTP/1.1 for http:// URLs and negotiates
// HTTP/2 for https:// URLs via ALPN. This matches Go's standard behaviour.
//
// Protocol selection is applied per-request by cloning the transport, ensuring
// that protocol settings do not affect other requests or the global default transport.
//
// Example usage:
//
//	opt := options.New().SetProtocol(options.HTTP1)  // Force HTTP/1.1
//	opt := options.New().SetProtocol(options.HTTP2)  // Force HTTP/2 (https:// only)
//
// Important considerations:
//   - HTTP2 requires https:// URLs; using it with http:// will result in connection errors.
//   - UnencryptedHTTP2 (h2c) requires server support and is typically only used in
//     controlled environments.
//   - For advanced HTTP/2 tuning (MaxConcurrentStreams, flow control, ping timeouts),
//     configure a custom Transport via SetTransport() or use client.NewCustom().
//
// This method returns the Option pointer for method chaining.
func (opt *Option) SetProtocol(p Protocol) *Option {
	opt.mu.Lock()
	opt.Transport.Protocol = p
	opt.mu.Unlock()
	return opt
}

// SetProtocolScheme sets the protocol scheme (e.g., "http://", "https://") for requests.
// If the provided scheme doesn't end with "://", it will be automatically appended.
func (opt *Option) SetProtocolScheme(scheme string) *Option {
	if len(scheme) > 0 && scheme[len(scheme)-3:] != "://" {
		scheme += "://"
	}
	opt.mu.Lock()
	opt.Transport.Scheme = scheme
	opt.mu.Unlock()
	return opt
}
