package client

import (
	"net/http"
	"sync"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// Default configuration for response history.
const (
	DefaultMaxResponses = 1000            // Maximum number of responses to keep.
	DefaultResponseTTL  = 5 * time.Minute // How long to keep responses before expiry.
)

// responseEntry wraps a response with metadata for TTL management.
// This enables automatic expiry of old responses to prevent unbounded memory growth.
type responseEntry struct {
	// response is the stored HTTP response data.
	response response.Response
	// createdAt tracks when this entry was added, used for TTL expiry calculations.
	createdAt time.Time
}

// Client represents a reusable HTTP client with persistent connection pooling.
// All requests made through a Client instance share the same underlying http.Client,
// enabling connection reuse and improved performance for multiple requests to the same hosts.
type Client struct {
	mu           sync.RWMutex              // Protects responses map.
	client       *http.Client              // Persistent HTTP client shared across all requests for connection pooling.
	responses    map[string]*responseEntry // Response history keyed by UniqueIdentifier.
	maxResponses int                       // Maximum number of responses to keep.
	responseTTL  time.Duration             // How long to keep responses before expiry.
	global       *options.Option           // Global request options applied to all requests.
}

// New returns a reusable Client with a persistent http.Client for connection pooling.
// Global options can be provided which will be applied to all subsequent requests.
func New(opts ...*options.Option) *Client {
	c := &Client{
		client:       &http.Client{},
		maxResponses: DefaultMaxResponses,
		responseTTL:  DefaultResponseTTL,
		responses:    make(map[string]*responseEntry),
	}
	// if no options are passed through, use the defaults
	c.global = options.New(opts...)
	return c
}

// SetMaxResponses sets the maximum number of responses to keep.
// When the limit is exceeded, expired entries are cleaned up first,
// then oldest entries are removed if still over the limit.
func (c *Client) SetMaxResponses(max int) {
	if max < 1 {
		max = 1
	}
	c.mu.Lock()
	c.maxResponses = max
	c.mu.Unlock()
}

// SetResponseTTL sets how long responses are kept before being eligible for cleanup.
// Responses older than the TTL are removed during cleanup operations.
func (c *Client) SetResponseTTL(ttl time.Duration) {
	if ttl < 0 {
		ttl = 0
	}
	c.mu.Lock()
	c.responseTTL = ttl
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
	return c.global.Clone()
}

// Clear clears any Responses that have already been made and kept.
func (c *Client) Clear() {
	c.mu.Lock()
	c.responses = make(map[string]*responseEntry)
	c.mu.Unlock()
}

// Responses returns a slice of all non-expired responses made by this Client.
// Responses are returned in no guaranteed order.
func (c *Client) Responses() []response.Response {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	responses := make([]response.Response, 0, len(c.responses))
	for _, entry := range c.responses {
		if c.responseTTL == 0 || now.Sub(entry.createdAt) <= c.responseTTL {
			responses = append(responses, entry.response)
		}
	}
	return responses
}

// Response retrieves a specific response by its UniqueIdentifier.
// Returns nil if the response is not found or has expired.
func (c *Client) Response(id string) *response.Response {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.responses[id]
	if !ok {
		return nil
	}

	// Check if expired
	if c.responseTTL > 0 && time.Since(entry.createdAt) > c.responseTTL {
		return nil
	}

	return &entry.response
}

// ResponseCount returns the number of stored responses (including expired ones
// that haven't been cleaned up yet).
func (c *Client) ResponseCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.responses)
}

// cleanupResponses removes expired entries and enforces max limit.
// Must be called with write lock held.
//
// Uses a single-pass algorithm: while iterating for TTL expiry, we also
// track the oldest non-expired entry. If we're still at capacity after
// TTL cleanup, we can immediately evict the oldest without a second scan.
func (c *Client) cleanupResponses() {
	now := time.Now()
	var oldestID string
	var oldestTime time.Time

	// Single pass: remove expired entries AND track oldest non-expired
	for id, entry := range c.responses {
		if c.responseTTL > 0 && now.Sub(entry.createdAt) > c.responseTTL {
			delete(c.responses, id)
			continue
		}
		// Track oldest non-expired entry
		if oldestID == "" || entry.createdAt.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.createdAt
		}
	}

	// If still at/over capacity, evict the oldest we already found
	if len(c.responses) >= c.maxResponses && oldestID != "" {
		delete(c.responses, oldestID)
	}
}

// doRequest executes an HTTP request using the client's connection pool and global options.
// It clones the global options to avoid mutation, merges any per-request options, and stores
// the response in the client's history map for later retrieval via Response() or Responses().
func (c *Client) doRequest(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	// Start with cloned global options, then apply per-request options on top
	opt := c.CloneOptions()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	opt.SetClient(c.client)
	// Perform the request with the merged options
	resp, err := doRequest(method, url, payload, opt)

	// Store the response in the map
	c.mu.Lock()
	c.cleanupResponses() // Lazy cleanup before adding new entry
	c.responses[resp.UniqueIdentifier] = &responseEntry{
		response:  resp,
		createdAt: time.Now(),
	}
	c.mu.Unlock()

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
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Post(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPost, url, payload, opts...)
}

// PostFormData performs an HTTP POST as an x-www-form-urlencoded payload to the specified URL.
// It accepts the URL string as its first argument and a map[string]string the payload.
// The map is converted to a url.QueryEscaped k/v pair that is sent to the server.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PostFormData(url string, payload map[string]string, opts ...*options.Option) (response.Response, error) {
	opt := options.New()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	opt.AddHeader(ContentType, URLencoded)

	return c.Post(url, encodeFormData(payload), opt)
}

// PostFile uploads a file to the specified URL using an HTTP POST request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PostFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	opt := c.CloneOptions()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	if err := opt.PrepareFile(filename); err != nil {
		return response.Response{}, err
	}
	return c.doRequest(http.MethodPost, url, nil, opt)
}

// Put performs an HTTP PUT to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Put(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPut, url, payload, opts...)
}

// PutFormData performs an HTTP PUT as an x-www-form-urlencoded payload to the specified URL.
// It accepts the URL string as its first argument and a map[string]string the payload.
// The map is converted to a url.QueryEscaped k/v pair that is sent to the server.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PutFormData(url string, payload map[string]string, opts ...*options.Option) (response.Response, error) {
	opt := options.New()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	opt.AddHeader(ContentType, URLencoded)

	return c.Put(url, encodeFormData(payload), opt)
}

// PutFile uploads a file to the specified URL using an HTTP PUT request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PutFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	opt := c.CloneOptions()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	if err := opt.PrepareFile(filename); err != nil {
		return response.Response{}, err
	}
	return c.doRequest(http.MethodPut, url, nil, opt)
}

// Patch performs an HTTP PATCH to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) Patch(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(http.MethodPatch, url, payload, opts...)
}

// PatchFormData performs an HTTP PATCH as an x-www-form-urlencoded payload to the specified URL.
// It accepts the URL string as its first argument and a map[string]string the payload.
// The map is converted to a url.QueryEscaped k/v pair that is sent to the server.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PatchFormData(url string, payload map[string]string, opts ...*options.Option) (response.Response, error) {
	opt := options.New()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	opt.AddHeader(ContentType, URLencoded)

	return c.Patch(url, encodeFormData(payload), opt)
}

// PatchFile uploads a file to the specified URL using an HTTP PATCH request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func (c *Client) PatchFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	opt := c.CloneOptions()
	if len(opts) > 0 && opts[0] != nil {
		opt.Merge(opts[0])
	}
	if err := opt.PrepareFile(filename); err != nil {
		return response.Response{}, err
	}
	return c.doRequest(http.MethodPatch, url, nil, opt)
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
// Returns the HTTP response and an error if any.
func (c *Client) Custom(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	return c.doRequest(method, url, payload, opts...)
}
