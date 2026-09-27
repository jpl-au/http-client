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
// Options group related settings in config structs:
//   - [LoggingConfig] - logging settings
//   - [CompressionConfig] - compression type and custom compressors
//   - [RedirectConfig] - redirect behaviour
//   - [ProgressConfig] - upload/download progress callbacks
//   - [TransportConfig] - HTTP transport settings
//   - [TracingConfig] - request tracing/correlation IDs
//   - [FileConfig] - file upload metadata
//   - [ResponseWriter] - whether the response body goes to memory or a file
//   - [RangeConfig] - range requests and resumed downloads
//   - [ChecksumConfig] - the checksum a download must match
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
