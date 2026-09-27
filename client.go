package client

import (
	"net/http"
	"sync"

	"github.com/jpl-au/http-client/history"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// Client represents a reusable HTTP client with persistent connection pooling.
// All requests made through a Client instance share the same underlying http.Client,
// enabling connection reuse and improved performance for multiple requests to the same hosts.
type Client struct {
	mu      sync.RWMutex     // Protects global and history.
	client  *http.Client     // Persistent HTTP client shared across all requests for connection pooling.
	history *history.History // Records every response when set. Nil turns history off.
	global  *options.Option  // Global request options applied to all requests.
}

// New returns a reusable Client with a persistent http.Client for connection pooling.
// Global options can be provided which will be applied to all subsequent requests.
func New(opts ...*options.Option) *Client {
	c := &Client{
		client: &http.Client{},
	}
	// if no options are passed through, use the defaults
	c.global = options.New(opts...)
	return c
}

// SetHistory records every response the Client returns in h. A nil History,
// the default, turns history off.
func (c *Client) SetHistory(h *history.History) {
	c.mu.Lock()
	c.history = h
	c.mu.Unlock()
}

// NewCustom returns a reusable Client with a custom http.Client for connection pooling.
// Use this when you need specific http.Client configurations such as custom timeouts,
// transport settings, or TLS configuration. The provided client will be shared across
// all requests made through this Client instance.
func NewCustom(client *http.Client, opts ...*options.Option) *Client {
	c := New(opts...)
	c.client = client
	return c
}

// GlobalOptions returns the global RequestOptions of the client.
func (c *Client) GlobalOptions() *options.Option {
	c.mu.RLock()
	global := c.global
	c.mu.RUnlock()
	return global
}

// AddGlobalOptions merges the provided options into the client's existing global options.
// This preserves existing settings while adding or overwriting specific values from opts.
// Use UpdateGlobalOptions instead if you want to completely replace the global options.
func (c *Client) AddGlobalOptions(opts *options.Option) {
	c.mu.RLock()
	global := c.global
	c.mu.RUnlock()
	global.Merge(opts)
}

// UpdateGlobalOptions replaces the client's global options entirely with the provided options.
// This discards all existing global settings. Use AddGlobalOptions instead if you want to
// merge new settings while preserving existing ones.
func (c *Client) UpdateGlobalOptions(opts *options.Option) {
	c.mu.Lock()
	c.global = opts
	c.mu.Unlock()
}

// CloneOptions returns a deep copy of the client's global options.
//
// The returned Option can be modified without affecting the client's
// global configuration. This is used internally for per-request option
// merging and can be used externally to create request-specific variations.
func (c *Client) CloneOptions() *options.Option {
	return c.GlobalOptions().Clone()
}

// doRequest executes an HTTP request using the client's connection pool and global options.
// It clones the global options to avoid mutation, merges any per-request options, and stores
// the response in the attached History, if any.
func (c *Client) doRequest(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	// Start with cloned global options, then apply per-request options on top
	opt := c.CloneOptions()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	opt.SetClient(c.client)
	// Perform the request with the merged options
	resp, err := doRequest(method, url, payload, opt)

	c.mu.RLock()
	h := c.history
	c.mu.RUnlock()
	if h != nil {
		h.Add(resp)
	}

	return resp, err
}

// Get performs an HTTP GET to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Get(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodGet, url, nil, opts...)
}

// Post performs an HTTP POST to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Post(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPost, url, payload, opts...)
}

// PostFile uploads a file to the specified URL using an HTTP POST request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PostFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPost, url, uploadFile(filename), opts...)
}

// Put performs an HTTP PUT to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Put(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPut, url, payload, opts...)
}

// PutFile uploads a file to the specified URL using an HTTP PUT request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PutFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPut, url, uploadFile(filename), opts...)
}

// Patch performs an HTTP PATCH to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Patch(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPatch, url, payload, opts...)
}

// PatchFile uploads a file to the specified URL using an HTTP PATCH request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PatchFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPatch, url, uploadFile(filename), opts...)
}

// Delete performs an HTTP DELETE to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Delete(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodDelete, url, nil, opts...)
}

// Connect performs an HTTP CONNECT to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Connect(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodConnect, url, nil, opts...)
}

// Head performs an HTTP HEAD to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Head(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodHead, url, nil, opts...)
}

// Options performs an HTTP OPTIONS to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Options(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodOptions, url, nil, opts...)
}

// Trace performs an HTTP TRACE to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Trace(url string, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodTrace, url, nil, opts...)
}

// Custom performs a custom HTTP method to the specified URL with the given payload.
// It accepts the HTTP method as its first argument, the URL string as the second argument,
// the payload as the third argument, and optionally additional Options to customize the request.
// The payload is sent as the request body whatever the method.
// Returns the HTTP response and an error if any.
func (c *Client) Custom(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(method, url, payload, opts...)
}
