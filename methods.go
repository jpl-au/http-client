package client

import (
	"net/http"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// Get performs an HTTP GET to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Get(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodGet, url, nil, opts...)
}

// Post performs an HTTP POST to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Post(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPost, url, payload, opts...)
}

// PostFile uploads a file to the specified URL using an HTTP POST request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func PostFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPost, url, uploadFile(filename), opts...)
}

// PostMultipartUpload performs a POST multipart form-data upload request to the specified URL.
// This is the most common method for file uploads and creating new resources with file attachments.
func PostMultipartUpload(url string, payload map[string]any, opts ...*options.Option) (response.Response, error) {
	return MultipartUpload(http.MethodPost, url, payload, opts...)
}

// Put performs an HTTP PUT to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Put(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPut, url, payload, opts...)
}

// PutFile uploads a file to the specified URL using an HTTP PUT request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func PutFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPut, url, uploadFile(filename), opts...)
}

// PutMultipartUpload performs a PUT multipart form-data upload request to the specified URL.
// This method is less common but can be used when updating an entire resource with new data,
// including file attachments.
func PutMultipartUpload(url string, payload map[string]any, opts ...*options.Option) (response.Response, error) {
	return MultipartUpload(http.MethodPut, url, payload, opts...)
}

// Patch performs an HTTP PATCH to the specified URL with the given payload.
// It accepts the URL string as its first argument and the payload as the second argument.
// The package documentation lists the payload types.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Patch(url string, payload any, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPatch, url, payload, opts...)
}

// PatchFile uploads a file to the specified URL using an HTTP PATCH request.
// It accepts the URL string as its first argument and the filename as the second argument.
// The file is read from the specified filename and uploaded as the request payload.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func PatchFile(url string, filename string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodPatch, url, uploadFile(filename), opts...)
}

// PatchMultipartUpload performs a PATCH multipart form-data upload request to the specified URL.
// This method can be used for partial updates to a resource, which might include updating or
// adding new file attachments.
func PatchMultipartUpload(url string, payload map[string]any, opts ...*options.Option) (response.Response, error) {
	return MultipartUpload(http.MethodPatch, url, payload, opts...)
}

// Delete performs an HTTP DELETE to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Delete(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodDelete, url, nil, opts...)
}

// Connect performs an HTTP CONNECT to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Connect(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodConnect, url, nil, opts...)
}

// Head performs an HTTP HEAD to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Head(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodHead, url, nil, opts...)
}

// Options performs an HTTP OPTIONS to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Options(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodOptions, url, nil, opts...)
}

// Trace performs an HTTP TRACE to the specified URL.
// It accepts the URL string as its first argument.
// Optionally, you can provide additional Options to customize the request.
// Returns the HTTP response and an error if any.
func Trace(url string, opts ...*options.Option) (response.Response, error) {
	return doRequest(http.MethodTrace, url, nil, opts...)
}

// Custom performs a custom HTTP method to the specified URL with the given payload.
// It accepts the HTTP method as its first argument, the URL string as the second argument,
// the payload as the third argument, and optionally additional Options to customize the request.
// The payload is sent as the request body whatever the method. The package
// documentation lists the payload types.
// Returns the HTTP response and an error if any.
func Custom(method string, url string, payload any, opts ...*options.Option) (response.Response, error) {
	return doRequest(method, url, payload, opts...)
}
