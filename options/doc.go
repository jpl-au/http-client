// Package options provides configuration types for the HTTP client.
//
// # Option Configuration
//
// [Option] is the main configuration type, created with [New]:
//
//	opt := options.New().
//	    AddHeader("Content-Type", "application/json").
//	    SetCompression(options.CompressionGzip)
//
// Options use embedded config structs for organization:
//   - [LoggingConfig] - logging settings
//   - [CompressionConfig] - compression type and custom compressors
//   - [RedirectConfig] - redirect behavior
//   - [ProgressConfig] - upload/download progress callbacks
//   - [TransportConfig] - HTTP transport settings
//   - [TracingConfig] - request tracing/correlation IDs
//   - [FileConfig] - file upload metadata
//
// # Compression
//
// Built-in compression types:
//   - [CompressionNone] - no compression (default)
//   - [CompressionGzip] - gzip compression
//   - [CompressionDeflate] - deflate compression
//   - [CompressionBrotli] - brotli compression
//   - [CompressionCustom] - custom compression with user-provided compressor
//
// # Progress Tracking
//
// Track upload and download progress:
//
//	opt := options.New()
//	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
//	    fmt.Printf("Uploaded %d of %d bytes\n", bytesRead, totalBytes)
//	}
package options
