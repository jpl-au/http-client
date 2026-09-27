// Package client provides a convenient HTTP client with support for common
// operations like GET, POST, PUT, PATCH, DELETE, file uploads, compression,
// and progress tracking.
//
// # Quick Start
//
// The simplest way to make requests is using the package-level functions:
//
//	resp, err := client.Get("https://httpbin.org/get")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(resp.String())
//
// # Configuring Requests
//
// Use [options.Option] to customize requests with headers, compression,
// redirects, and progress tracking:
//
//	opt := options.New().
//	    AddHeader("Authorization", "Bearer token").
//	    SetCompression(options.CompressionGzip)
//
//	resp, err := client.Post(url, payload, opt)
//
// # Payloads
//
// Post, Put, Patch and Custom take the request body as a payload. The payload's
// type decides how it is sent:
//   - nil sends no body.
//   - []byte, string and *bytes.Buffer are sent as they are.
//   - An io.Reader is read to the end. A reader that is also an io.Seeker can
//     be sent again after a 307 or 308 redirect.
//   - An *os.File is opened again by name, so it can be sent again after a
//     redirect.
//   - url.Values is sent URL-encoded, with the Content-Type
//     application/x-www-form-urlencoded.
//
// For example:
//
//	resp, err := client.Post(url, url.Values{"name": {"Ada"}, "tag": {"a", "b"}})
//
// # Reusable Client
//
// For connection pooling and shared configuration, use [Client]:
//
//	c := client.New(options.New().
//	    AddHeader("X-API-Key", "secret"))
//
//	resp1, _ := c.Get(url1)
//	resp2, _ := c.Get(url2)  // reuses connections
//
// # File Uploads
//
// Upload files using [PostFile], [PutFile], or [PatchFile]:
//
//	resp, err := client.PostFile(url, "/path/to/file.txt")
//
// Or pass an *os.File directly as payload:
//
//	file, _ := os.Open("data.json")
//	defer file.Close()
//	resp, err := client.Post(url, file, opt)
//
// # Compression
//
// Compress request payloads with gzip, deflate, or brotli:
//
//	opt := options.New().SetCompression(options.CompressionGzip)
//	resp, err := client.Post(url, largePayload, opt)
//
// Response decompression is automatic based on Content-Encoding headers.
//
// # Progress Tracking
//
// Monitor upload and download progress:
//
//	opt := options.New().
//	    OnUploadProgress(func(bytesRead, totalBytes int64) {
//	        fmt.Printf("Upload: %.1f%%\n", float64(bytesRead)/float64(totalBytes)*100)
//	    }).
//	    OnDownloadProgress(func(bytesRead, totalBytes int64) {
//	        fmt.Printf("Download: %.1f%%\n", float64(bytesRead)/float64(totalBytes)*100)
//	    })
//
// # Redirects
//
// Redirects are not followed by default. To follow them:
//
//	opt := options.New().
//	    EnableRedirects().   // follow redirects (off by default)
//	    SetMaxRedirects(5)   // maximum redirects (10 by default)
package client
