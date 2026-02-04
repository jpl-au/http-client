# http-client

A Go HTTP client library with support for compression, progress tracking, and connection pooling.

## Features

- Simple API for GET, POST, PUT, PATCH, DELETE requests
- Compression (gzip, deflate, brotli, custom)
- Upload and download progress tracking
- File uploads with automatic content-type detection
- Redirect handling with method preservation
- Range requests for partial downloads and resumable transfers
- Request tracing with UUID/ULID identifiers
- Protocol selection (HTTP/1, HTTP/2)
- Multipart form uploads
- Reusable client with connection pooling and response history

## Installation

```bash
go get github.com/jpl-au/http-client
```

## Quick Start

For simple, one-off requests, use the package-level functions:

```go
package main

import (
    "fmt"
    "log"

    client "github.com/jpl-au/http-client"
)

func main() {
    resp, err := client.Get("https://httpbin.org/get")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.String())
}
```

## Configuration

Use `options.Option` to customise requests:

```go
import (
    client "github.com/jpl-au/http-client"
    "github.com/jpl-au/http-client/options"
)

opt := options.New().
    AddHeader("Authorization", "Bearer token").
    AddHeader("Content-Type", "application/json")

resp, err := client.Post(url, payload, opt)
```

## File Uploads

### Using PrepareFile (recommended)

```go
opt := options.New()
if err := opt.PrepareFile("/path/to/file.txt"); err != nil {
    log.Fatal(err)
}
resp, err := client.Post(url, nil, opt)
```

### Using the PostFile helper

```go
resp, err := client.PostFile(url, "/path/to/file.txt")
```

### Passing a file handle

```go
file, _ := os.Open("data.json")
defer file.Close()

resp, err := client.Post(url, file, opt)
```

## Compression

```go
opt := options.New().SetCompression(options.CompressionGzip)
resp, err := client.Post(url, largePayload, opt)
```

Supported types: `CompressionGzip`, `CompressionDeflate`, `CompressionBrotli`

### Custom Compression

```go
opt := options.New()
opt.SetCompression(options.CompressionCustom)
opt.Compression.CustomType = "snappy"
opt.Compression.Compressor = func(w *io.PipeWriter) (io.WriteCloser, error) {
    return snappy.NewBufferedWriter(w), nil
}
```

## Progress Tracking

```go
opt := options.New()
opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
    pct := float64(bytesRead) / float64(totalBytes) * 100
    fmt.Printf("\rUploading: %.1f%%", pct)
}
opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
    pct := float64(bytesRead) / float64(totalBytes) * 100
    fmt.Printf("\rDownloading: %.1f%%", pct)
}

resp, err := client.PostFile(url, "large-file.zip", opt)
```

## Redirect Handling

```go
opt := options.New()
opt.Redirect.Follow = true          // follow redirects (default: false)
opt.Redirect.PreserveMethod = true  // preserve POST/PUT method on redirect
opt.Redirect.Max = 10               // maximum redirects (default: 10)
```

## Range Requests

Download partial content or resume interrupted downloads:

```go
// Download bytes 0-499
opt := options.New().SetRange(0, 499)
resp, err := client.Get(url, opt)

// Download from offset to end
opt := options.New().SetRangeFrom(1000)
resp, err := client.Get(url, opt)

// Download the last 1024 bytes
opt := options.New().SetRangeLast(1024)
resp, err := client.Get(url, opt)

// Resume a partial download
opt := options.New().Resume("/path/to/partial-file.zip")
resp, err := client.Get(url, opt)
```

## Request Tracing

Add unique identifiers to requests for distributed tracing:

```go
opt := options.New().SetIdentifierType(options.IdentifierULID)  // default
// or
opt := options.New().SetIdentifierType(options.IdentifierUUID)

// Access the identifier from the response
fmt.Println(resp.UniqueIdentifier)
```

## Protocol Selection

Control the HTTP protocol version:

```go
opt := options.New().SetProtocol(options.HTTP1)  // Force HTTP/1.1
opt := options.New().SetProtocol(options.HTTP2)  // Force HTTP/2 (HTTPS only)
opt := options.New().SetProtocol(options.Both)   // Auto-negotiate (default)
```

## Writing Responses to a File

Download content directly to a file without buffering in memory:

```go
opt := options.New().SetFileOutput("/path/to/output.txt")

resp, err := client.Get(url, opt)
// Response body is written directly to the file
```

---

## Reusable Client

For applications making multiple HTTP requests, the `Client` type provides connection pooling, shared configuration, and response history tracking.

### Why Use a Reusable Client?

- **Connection pooling**: Requests to the same host reuse TCP connections, reducing latency and resource usage
- **Shared configuration**: Global options (headers, authentication) are applied to all requests automatically
- **Response history**: All responses are stored for later inspection, useful for debugging, logging, or batch operations

### Basic Usage

```go
c := client.New(options.New().
    AddHeader("X-API-Key", "secret"))

// All requests share connections and include the API key header
resp1, _ := c.Get(url1)
resp2, _ := c.Post(url2, data)
```

### Response History

The client stores all responses in an internal map, enabling batch operations and deferred error handling:

```go
// Make multiple requests
c.Get(url1)
c.Get(url2)
c.Post(url3, data)

// Inspect all responses afterwards
for _, resp := range c.Responses() {
    if resp.Error != nil {
        log.Printf("Request to %s failed: %v", resp.URL, resp.Error)
        continue
    }
    fmt.Printf("%s: %d\n", resp.URL, resp.StatusCode)
}

// Retrieve a specific response by its unique identifier
resp := c.Response(someID)
```

The `Error` field on each response allows you to collect results from many requests and check for failures later, rather than handling errors inline.

### Memory Management

Response history grows with each request. To prevent unbounded memory usage, the client provides automatic limits and manual controls:

```go
// Configure limits (call before making requests)
c.SetMaxResponses(500)              // Maximum responses to retain (default: 1000)
c.SetResponseTTL(10 * time.Minute)  // Expire responses after this duration (default: 5 minutes)

// Manual cleanup
c.Clear()  // Remove all stored responses
```

Responses older than the TTL are automatically removed during cleanup. When the maximum is reached, the oldest entries are evicted first.

### Managing Global Options

```go
// Get current global options
opts := c.GlobalOptions()

// Merge additional options (preserves existing settings)
c.AddGlobalOptions(options.New().AddHeader("X-New-Header", "value"))

// Replace global options entirely
c.UpdateGlobalOptions(options.New().AddHeader("Authorization", "Bearer new-token"))

// Clone options for per-request modifications
opt := c.CloneOptions()
opt.AddHeader("X-Request-Specific", "value")
resp, _ := c.Get(url, opt)
```

---

## Response

The `Response` type contains the HTTP response data and metadata:

```go
resp, err := client.Get(url)

resp.StatusCode       // HTTP status code (e.g., 200)
resp.Status           // Status text (e.g., "200 OK")
resp.String()         // Body as string
resp.Bytes()          // Body as []byte
resp.Buffer()         // Body as *bytes.Buffer
resp.Header           // Response headers
resp.Cookies          // Response cookies
resp.AccessTime       // Request duration
resp.Redirected       // Whether a redirect occurred
resp.Location         // Final URL after redirects
resp.UniqueIdentifier // Request trace ID
resp.Error            // Any error encountered (for batch inspection)
```

## Testing

See the [test directory](test/) for comprehensive examples and test cases.

## Licence

MIT
