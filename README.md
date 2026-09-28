# http-client

A Go HTTP client library with support for compression, progress tracking, and connection pooling.

## Features

- Simple API for GET, POST, PUT, PATCH, DELETE requests
- Compression (gzip, deflate, brotli, custom)
- Upload and download progress tracking, with a ready-made terminal display
- File uploads with automatic content-type detection
- URL-encoded forms, and multipart forms that stream files instead of holding them in memory
- Redirect handling that follows net/http rules
- Range requests and resumable downloads
- Downloads split into segments that download at the same time
- Checksum checks, against a checksum you supply or one the server sends
- A size limit for response bodies held in memory
- A timeout that cancels a request when no data is sent or received
- Request tracing with ULID, UUID or random identifiers
- Protocol selection (HTTP/1, HTTP/2)
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
| `io.Reader` | The bytes read from it |
| `*os.File` | The contents of the file |
| `url.Values` | A URL-encoded form |
| `*form.Form` | A multipart form |

```go
resp, err := client.Post(url, url.Values{"name": {"Ada"}, "tag": {"a", "b"}})
```

## Forms

Build a multipart form with the `form` package. The parts are sent in the order you add them, and a name can be used more than once:

```go
import "github.com/jpl-au/http-client/form"

f := form.New().
    Field("title", "Quarterly report").
    File("attachment", "/path/report.pdf").
    File("attachment", "/path/summary.pdf")

resp, err := client.Post(url, f)
```

Files are read while the form is sent, so a large file is not held in memory. A file that cannot be read fails the request before anything is sent.

## File Uploads

### Using PrepareFile (recommended)

```go
opt := options.New()
if err := opt.PrepareFile("/path/to/file.txt"); err != nil {
    log.Fatal(err)
}
resp, err := client.Post(url, nil, opt)
```

### Using the PostFile Helper

```go
resp, err := client.PostFile(url, "/path/to/file.txt")
```

### Passing a File Handle

```go
file, err := os.Open("data.json")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

resp, err := client.Post(url, file, opt)
```

## Compression

```go
opt := options.New().SetCompression(options.CompressionGzip)
resp, err := client.Post(url, largePayload, opt)
```

The supported types are `CompressionGzip`, `CompressionDeflate` and `CompressionBrotli`.

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

The total is -1 when the size is not known.

The `progress` package has a ready-made callback that shows the percentage, speed and time remaining in the terminal:

```go
opt := options.New().OnDownloadProgress(progress.Terminal())
```

## Redirect Handling

Redirects are not followed by default:

```go
opt := options.New().
    EnableRedirects().   // follow redirects (default: off)
    SetMaxRedirects(5)   // maximum redirects (default: 10)
```

Redirects follow the same rules as Go's net/http. For example, credentials and cookies are not sent to another host. A request body that can be read only once, such as a plain `io.Reader`, cannot be sent again after a 307 or 308 redirect, so the request fails with `client.ErrPayloadNotReplayable`.

## Range Requests

Download part of a file:

```go
// Download bytes 0-499
opt := options.New().SetRange(0, 499)
resp, err := client.Get(url, opt)

// Download from byte 1000 to the end
opt := options.New().SetRangeFrom(1000)
resp, err := client.Get(url, opt)

// Download the last 1024 bytes
opt := options.New().SetRangeLast(1024)
resp, err := client.Get(url, opt)
```

### Resumable Downloads

`Resume` downloads to a file and can continue after an interruption. The data goes to `file.zip.part`, which is renamed to `file.zip` only when the download is complete.

To continue a download, pass the header of the response that started it. The server then sends only the rest of the file, or the whole file again if it has changed since. Pass nil on the first attempt, or to start again from the beginning.

```go
var previous http.Header // the header of the earlier attempt, or nil
resp, err := client.Get(url, options.New().Resume("/path/to/file.zip", previous))
switch {
case err != nil:
    fmt.Printf("The download did not complete: %v\n", err)
case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent:
    fmt.Println("The download is complete.")
default:
    fmt.Printf("The server answered %d.\n", resp.StatusCode)
}
```

An HTTP error status does not return a Go error, so check the status as well. `client.ErrDownloadIncomplete` means the server sent only part of what was left: resume again to continue. Within one program, only one download at a time can resume a file, and another fails with `client.ErrDownloadInProgress`. Separate programs are not coordinated.

## Request Tracing

Add a unique identifier to each request, sent in the `X-Trace-ID` header:

```go
opt := options.New().SetIdentifierType(options.IdentifierULID)   // default
opt := options.New().SetIdentifierType(options.IdentifierUUID)
opt := options.New().SetIdentifierType(options.IdentifierRandom) // a random string
opt := options.New().SetIdentifierType(options.IdentifierNone)   // no header

// Read the identifier from the response
fmt.Println(resp.UniqueIdentifier)
```

## Protocol Selection

Choose the HTTP protocol version:

```go
opt := options.New().SetProtocol(options.HTTP1)   // Force HTTP/1.1
opt := options.New().SetProtocol(options.HTTP2)   // Force HTTP/2 (HTTPS only)
opt := options.New().SetProtocol(options.HTTPAny) // Choose automatically (default)
```

## Writing Responses to a File

Download straight to a file instead of into memory:

```go
opt := options.New().SetFileOutput("/path/to/output.txt")

resp, err := client.Get(url, opt)
```

The file is replaced only when the whole body has arrived, so a failed download leaves it as it was. Only a successful (2xx) response is written to the file. Any other response is returned in the response body with no error, so check `resp.StatusCode`.

### Downloading in Segments

`SetSegments` splits a file download into segments that download at the same time. This can be faster from a server that limits the speed of each connection:

```go
opt := options.New().
    SetFileOutput("/path/to/file.iso").
    SetSegments(4) // Up to 4 requests at the same time

resp, err := client.Get(url, opt)
```

A small file, or a file from a server that does not support segments, is downloaded in one request. If the file changes on the server during the download, the download fails with `client.ErrRangeMismatch`. If any segment fails, the destination is left as it was.

## Limiting Buffered Responses

A body held in memory has no size limit by default. Set one to protect against unexpectedly large responses:

```go
opt := options.New().SetMaxBodySize(10 << 20) // 10 MiB

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrBodyTooLarge) {
    // The body was longer than 10 MiB
}
```

A body written to a file has no limit.

## Stalled Requests

`SetStallTimeout` cancels a request when no data is sent or received for a set time, for example when a server stops sending data but keeps the connection open. There is no limit by default.

```go
opt := options.New().SetStallTimeout(30 * time.Second)

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrStalled) {
    // No data was sent or received for 30 seconds
}
```

The timer restarts each time data arrives, so a slow download that keeps moving is not cancelled. The timer also runs while the server prepares its reply, so allow more time than the server needs to start replying.

## Checksums

`SetChecksum` checks a download against a checksum you already have, such as one published next to the file. Pass the function that creates the hash, and the checksum as hex, as tools like `sha256sum` print it:

```go
opt := options.New().
    SetFileOutput("/path/file.zip").
    SetChecksum(sha256.New, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

resp, err := client.Get(url, opt)
if errors.Is(err, client.ErrChecksumMismatch) {
    // The download did not match, and /path/file.zip was not changed
}
```

### Checksums from the Server

Some servers send a checksum with the response. The client checks it automatically, and a response that does not match fails with `client.ErrChecksumMismatch`. Turn the check off for a server that sends wrong checksums:

```go
opt := options.New().DisableDigestCheck()
```

---

## Reusable Client

For applications that make many requests, the `Client` type provides connection pooling, shared configuration, and optional response history.

### Why Use a Reusable Client?

- **Connection pooling**: requests to the same host reuse connections, which is faster
- **Shared configuration**: global options, such as headers, apply to every request
- **Response history**: when turned on, responses are kept for later inspection

### Basic Usage

```go
c := client.New(options.New().
    AddHeader("X-API-Key", "secret"))

// Both requests share connections and send the API key header
resp, err := c.Get(url1)
resp, err = c.Post(url2, data)
```

### Response History

Response history is off by default, because stored responses hold bodies and credentials. Attach a `history.History` to keep every response the client returns:

```go
h := history.New()
c.SetHistory(h)

c.Get(url1)
c.Get(url2)

// Inspect the responses afterwards, oldest first
for resp := range h.All() {
    if resp.Error != nil {
        log.Printf("Request to %s failed: %v", resp.URL, resp.Error)
        continue
    }
    fmt.Printf("%s: %d\n", resp.URL, resp.StatusCode)
}

// Find one response by its identifier
resp, ok := h.Lookup(someID)
```

A history keeps at most 100 responses for 5 minutes by default:

```go
h := history.New().
    SetLimit(500).              // Keep up to 500 responses
    SetMaxAge(10 * time.Minute) // Keep each response for 10 minutes

c.SetHistory(nil) // Stop recording
```

### Managing Global Options

```go
// Read the current global options
opts := c.GlobalOptions()

// Add to the global options, keeping the existing settings
c.MergeGlobalOptions(options.New().AddHeader("X-New-Header", "value"))

// Replace the global options
c.SetGlobalOptions(options.New().AddHeader("Authorization", "Bearer new-token"))

// Copy the global options to change them for one request
opt := c.CloneOptions()
opt.AddHeader("X-Request-Specific", "value")
resp, err := c.Get(url, opt)
```

---

## Response

The `Response` type holds the response and details of the request:

```go
resp, err := client.Get(url)

resp.StatusCode       // HTTP status code, for example 200
resp.Status           // Status text, for example "200 OK"
resp.String()         // Body as a string
resp.Bytes()          // Body as []byte
resp.Buffer()         // Body as *bytes.Buffer
resp.Header           // Response headers
resp.Cookies          // Response cookies
resp.AccessTime       // How long the request took
resp.Redirected       // Whether the request was redirected
resp.Location         // Final URL after redirects
resp.UniqueIdentifier // The request's trace identifier
resp.Error            // The error, if the request failed
```

## Testing

Run the test suite from the repository root:

```bash
go test ./...
```

The `*_test.go` files give further examples of each feature.

## Licence

MIT
