package options

import (
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// CompressionType defines the compression algorithm used for HTTP requests.
// It supports standard compression types (gzip, deflate, brotli) as well as
// custom compression implementations.
type CompressionType string

// Compression types supported by the client
const (
	CompressionNone    CompressionType = ""        // No compression
	CompressionGzip    CompressionType = "gzip"    // Gzip compression (RFC 1952)
	CompressionDeflate CompressionType = "deflate" // Deflate compression (RFC 1951)
	CompressionBrotli  CompressionType = "br"      // Brotli compression
	CompressionCustom  CompressionType = "custom"  // Custom compression implementation
)

// CompressionConfig holds compression-related settings.
type CompressionConfig struct {
	// Type specifies the compression algorithm to use.
	Type CompressionType

	// CustomType is the Content-Encoding header value when using custom compression.
	CustomType CompressionType

	// Compressor is a function that returns a custom compressor.
	// Only used when Type is CompressionCustom.
	Compressor func(w *io.PipeWriter) (io.WriteCloser, error)

	// Decompressor is a function that returns a custom decompressor.
	// Used for decompressing responses with custom encoding.
	Decompressor func(r io.Reader) (io.Reader, error)
}

// defaultCompressionConfig returns the default compression configuration.
func defaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Type: CompressionNone,
	}
}

// SetCompression configures the compression type to be used for the request.
// Valid compression types include: none, gzip, deflate, brotli, and custom.
func (opt *Option) SetCompression(compressionType CompressionType) *Option {
	opt.mu.Lock()
	opt.Compression.Type = compressionType
	opt.mu.Unlock()
	return opt
}

// NewCompressor returns an appropriate io.WriteCloser based on the configured compression type.
func (opt *Option) NewCompressor(w *io.PipeWriter) (io.WriteCloser, error) {
	opt.mu.RLock()
	compressionType := opt.Compression.Type
	compressor := opt.Compression.Compressor
	opt.mu.RUnlock()

	switch compressionType {
	case CompressionGzip:
		return gzip.NewWriter(w), nil
	case CompressionDeflate:
		return zlib.NewWriter(w), nil
	case CompressionBrotli:
		return brotli.NewWriter(w), nil
	case CompressionCustom:
		if compressor == nil {
			return nil, fmt.Errorf("custom compression specified but no compressor provided")
		}
		return compressor(w)
	case CompressionNone:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compressionType)
	}
}

// NewDecompressor returns an appropriate io.Reader for the given encoding.
func (opt *Option) NewDecompressor(r io.ReadCloser, encoding string) (io.ReadCloser, error) {
	opt.mu.RLock()
	decompressor := opt.Compression.Decompressor
	opt.mu.RUnlock()

	switch encoding {
	case "":
		return r, nil
	case string(CompressionGzip):
		return gzip.NewReader(r)
	case string(CompressionDeflate):
		return zlib.NewReader(r)
	case string(CompressionBrotli):
		return io.NopCloser(brotli.NewReader(r)), nil
	default:
		// Try custom decompressor if available
		if decompressor != nil {
			reader, err := decompressor(r)
			if err != nil {
				return nil, err
			}
			if rc, ok := reader.(io.ReadCloser); ok {
				return rc, nil
			}
			return io.NopCloser(reader), nil
		}
		return nil, fmt.Errorf("unsupported compression type: %s", encoding)
	}
}
