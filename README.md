# http-client

A Go HTTP client library with support for compression, progress tracking, and connection pooling.

## Features

- Simple API for GET, POST, PUT, PATCH, DELETE requests
- Compression (gzip, deflate, brotli, custom)
- Upload and download progress tracking
- File uploads with automatic content-type detection
- Redirect handling that follows net/http rules
- Range requests for partial downloads and resumable transfers
- Request tracing with UUID/ULID identifiers
- Protocol selection (HTTP/1, HTTP/2)
- Multipart form uploads that stream files instead of holding them in memory
- Reusable client with connection pooling and optional response history

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

## Payloads

`Post`, `Put`, `Patch` and `Custom` take the request body as a payload. The payload's type decides how it is sent:

| Payload | Sent as |
|---|---|
| `nil` | No body |
| `[]byte`, `string`, `*bytes.Buffer` | The bytes as they are |
| `io.Reader` | Read to the end. A reader that is also an `io.Seeker` can be sent again after a 307 or 308 redirect |
| `*os.File` | The file, opened again by name so it can be sent again after a redirect |
| `url.Values` | URL-encoded, with the `Content-Type` `application/x-www-form-urlencoded` |
| `*form.Form` | `multipart/form-data`, with its parts in the order they were added |

```go
resp, err := client.Post(url, url.Values{"name": {"Ada"}, "tag": {"a", "b"}})
```

## Forms

Build a multipart form with the `form` package. Add fields and files in the order the server expects them. A name can be used more than once:

```go
import "github.com/jpl-au/http-client/form"

f := form.New().
    Field("title", "Quarterly report").
    File("attachment", "/path/report.pdf").
    File("attachment", "/path/summary.pdf")

resp, err := client.Post(url, f)
```

Each file is read in small pieces while the form is sent, so a large file is not held in memory. Before sending, the form checks every file and works out the exact size of the request, so the request has a `Content-Length` and upload progress can show a percentage. A file that cannot be read fails the request before anything is sent. A file whose size changes during the upload fails the request with `form.ErrFileChanged`. After a 307 or 308 redirect, the form is sent again from its files. Each file's content type is detected from its name and content.

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
opt := options.New().
    OnUploadProgress(func(bytesRead, totalBytes int64) {
        pct := float64(bytesRead) / float64(totalBytes) * 100
        fmt.Printf("\rUploading: %.1f%%", pct)
    }).
    OnDownloadProgress(func(bytesRead, totalBytes int64) {
        pct := float64(bytesRead) / float64(totalBytes) * 100
        fmt.Printf("\rDownloading: %.1f%%", pct)
    })

resp, err := client.PostFile(url, "large-file.zip", opt)
```

The total is -1 when the size is unknown, for example for a compressed or chunked response.

## Redirect Handling

Redirects are not followed by default:

```go
opt := options.New().
    EnableRedirects().   // follow redirects (default: off)
    SetMaxRedirects(5)   // maximum redirects (default: 10)
```

Redirects follow net/http rules. A 307 or 308 repeats the method and body, and a 301, 302 or 303 changes a POST to a GET without a body. Credentials and cookies are not sent to another host. A 307 or 308 with a payload that can only be read once, such as a plain `io.Reader`, returns `client.ErrPayloadNotReplayable`; use `[]byte`, a string, a file or a seekable reader to follow it.

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

```

### Resumable downloads

`Resume` downloads to a file and can continue after an interruption. The data goes to a partial file, `file.zip.part`, and is renamed to `file.zip` only when the download is complete, so `file.zip` never holds a partial download.

Pass the header of the response that started the partial file, or nil on the first attempt. The library sends its strong validator as `If-Range`, so if the resource has changed, the download starts again instead of mixing two versions. Without a strong validator, the download starts again from the beginning.

```go
// Use saved headers only if they belong to the bytes in file.zip.part.
// Leave previous nil on the first attempt, or to restart from the beginning.
var previous http.Header
resp, err := client.Get(url, options.New().Resume("/path/to/file.zip", previous))
switch {
case err != nil:
    fmt.Printf("The download did not complete: %v\n", err)
case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent:
    fmt.Println("The completed download is at /path/to/file.zip.")
default:
    fmt.Printf("HTTP %d did not produce a completed download.\n", resp.StatusCode)
}
```

HTTP error statuses do not produce a Go error, so check the status as well. A response without a body, such as 204, leaves the destination unchanged.

For a later attempt, keep headers associated with the retained partial bytes. Do not replace them with headers from a rejected response, including `client.ErrRangeMismatch`, or an HTTP error response. If that association is uncertain, pass nil to restart safely. `client.ErrDownloadIncomplete` means a valid range was retained but the file is not complete. Resume requests require an unencoded response; an encoded response is rejected before any file data changes.

If the partial file already holds the whole file, for example because the program stopped before it could rename the file into place, the server answers the resume with 416 and the file's size. The partial file is then published, and the response has the status 200 OK. A partial file longer than the file on the server is removed with `client.ErrRangeMismatch`, so the next resume starts again.

A partial file has one resumed download at a time in a process. Another resumed download to the same destination fails with `client.ErrDownloadInProgress` before it sends a request. Separate processes are not coordinated, so do not resume the same file from two processes at once.

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
// A successful body is written to the file
```

The body is written to a temporary file beside the destination and renamed into place only when it has arrived in full, so a failed download leaves the destination as it was. A replaced file keeps its permissions, and a new file follows the process umask. Only a 2xx response is written to the file. Any other response is returned in the response buffer with no error, so check `resp.StatusCode`. A 206 partial response to a download that asked for no range is not the whole file, so it fails with `client.ErrRangeMismatch` and leaves the destination as it was.

### Downloading in segments

`SetSegments` splits a file download into segments that download at the same time. This can be faster from a server that limits the speed of each connection:

```go
opt := options.New().
    SetFileOutput("/path/to/file.iso").
    SetSegments(4) // Up to 4 requests at the same time

resp, err := client.Get(url, opt)
```

The first request asks for the first 1 MiB, and the server's reply gives the file's size. The rest of the file is split into segments of at least 1 MiB, so a small file is not split. Each segment asks for the same version of the file as the first reply. If the file changes on the server during the download, the download fails with `client.ErrRangeMismatch`.

- A server that does not support range requests sends the whole file in its first reply, and the download finishes as one request.
- A reply with no strong validator, such as an `ETag`, cannot prove that all segments come from one version of the file, so the file is downloaded again in one request.
- If any segment fails, the other segments stop and the destination is left as it was.
- Every segment is requested from the original address. After a redirect, each segment follows the redirect again, so Go's rules for sending headers and cookies to another host apply to every segment.
- Progress adds up all segments, and the progress callback never runs twice at the same time.
- `SetChecksum` and a server's `Repr-Digest` are checked against the whole file.
- The response describes the whole file, with the status `200 OK`.
- A resumed download is not split. Segments apply only to a GET request with file output. `SetSegments(0)` or `SetSegments(1)` downloads in one request, which is the default.

## Limiting Buffered Responses

A body held in memory has no size limit by default. Set one to protect against unexpectedly large responses:

```go
opt := options.New().SetMaxBodySize(10 << 20) // 10 MiB

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrBodyTooLarge) {
    // The body was longer than 10 MiB and resp.Body is empty
}
```

The limit applies to the body after decompression, and to the error response body of a file download. A body written to a file has no limit. `SetMaxBodySize(0)` removes a limit, for example one set in a client's global options.

## Stalled Requests

`SetStallTimeout` cancels a request when no data is sent or received for a set time, for example when a server stops sending data but keeps the connection open. There is no limit by default.

```go
opt := options.New().SetStallTimeout(30 * time.Second)

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrStalled) {
    // No data was sent or received for 30 seconds
}
```

The timer restarts each time data is sent or received, so a large download is not cancelled while data is still arriving. A context deadline is different: it limits the time for the whole request. The timer also runs while the server prepares its reply, so set a time longer than the server needs to start replying. `SetStallTimeout(0)` removes a limit, for example one set in a client's global options.

## Checksums

`SetChecksum` checks a download against a checksum you already have, such as one published next to the file. Pass the function that creates the hash and the checksum as hex, as tools like `sha256sum` print it:

```go
opt := options.New().
    SetFileOutput("/path/file.zip").
    SetChecksum(sha256.New, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrChecksumMismatch) {
    // The download did not match, and /path/file.zip was not changed
}
```

The checksum covers the body as it is saved, after decompression. A file download that does not match leaves the destination as it was, and a download to memory that does not match leaves the response body empty. For a resumed download, the checksum covers the whole file and is checked when the file is complete. If the complete file does not match, its partial file is removed, so the next resume starts again from the beginning. Error responses, such as a 404, are not checked. A checksum that is not valid hex fails the request before it is sent. `SetChecksum(nil, "")` removes a checksum.

### Checksums from the server

Some servers send a checksum with the response, in a `Repr-Digest` or `Content-Digest` header (RFC 9530). The client checks it automatically, and a response that does not match fails with `client.ErrChecksumMismatch`, the same as `SetChecksum`. If you also set a checksum, both are checked.

Only the `sha-256` and `sha-512` algorithms are checked. A header with no known algorithm is ignored. The server's checksum covers the bytes it sent, so a gzip response that Go's HTTP library decompresses itself is not checked. For a resumed download, `Repr-Digest` covers the whole file and is checked when the file is complete. For any other range request, `Content-Digest` covers only the bytes received.

Turn the check off for a server that sends wrong checksums:

```go
opt := options.New().DisableDigestCheck()
```

---

## Reusable Client

For applications making multiple HTTP requests, the `Client` type provides connection pooling, shared configuration, and optional response history.

### Why Use a Reusable Client?

- **Connection pooling**: Requests to the same host reuse TCP connections, reducing latency and resource usage
- **Shared configuration**: Global options (headers, authentication) are applied to all requests automatically
- **Response history**: When turned on, responses are stored for later inspection, useful for debugging, logging, or batch operations

### Basic Usage

```go
c := client.New(options.New().
    AddHeader("X-API-Key", "secret"))

// All requests share connections and include the API key header
resp1, _ := c.Get(url1)
resp2, _ := c.Post(url2, data)
```

### Response History

Response history is off by default. Stored responses hold bodies, request payloads and credentials, so they are recorded only on request. Attach a `history.History` to record every response the client returns:

```go
h := history.New()
c.SetHistory(h)

// Make multiple requests
c.Get(url1)
c.Get(url2)
c.Post(url3, data)

// Inspect all responses afterwards, oldest first
for resp := range h.All() {
    if resp.Error != nil {
        log.Printf("Request to %s failed: %v", resp.URL, resp.Error)
        continue
    }
    fmt.Printf("%s: %d\n", resp.URL, resp.StatusCode)
}

// Retrieve a specific response by its unique identifier
resp, ok := h.Lookup(someID)
```

The `Error` field on each response allows you to collect results from many requests and check for failures later, rather than handling errors inline.

A history is bounded. It keeps at most 100 responses for at most 5 minutes by default:

```go
h := history.New().
    SetLimit(500).                 // Keep up to 500 responses
    SetMaxAge(10 * time.Minute)    // Expire responses after 10 minutes (0 keeps them until the limit removes them)

h.Len()             // Number of stored responses
h.Clear()           // Remove all stored responses
c.SetHistory(nil)   // Stop recording
```

When the history is full, expired responses are removed first, then the oldest. One history can record the responses of several clients.

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

Run the test suite from the repository root:

```bash
go test ./...
```

The `*_test.go` files give further examples of each feature.

## Licence

MIT
