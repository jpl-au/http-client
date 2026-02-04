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
//	opt := options.New()
//	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
//	    fmt.Printf("Upload: %.1f%%\n", float64(bytesRead)/float64(totalBytes)*100)
//	}
//	opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
//	    fmt.Printf("Download: %.1f%%\n", float64(bytesRead)/float64(totalBytes)*100)
//	}
//
// # Redirects
//
// Configure redirect behavior:
//
//	opt := options.New()
//	opt.Redirect.Follow = true           // follow redirects (default)
//	opt.Redirect.PreserveMethod = true   // keep POST on redirect
//	opt.Redirect.Max = 10                // maximum redirects
package client
