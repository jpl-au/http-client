package options

// ResponseWriterType defines how the HTTP response body should be handled.
// It determines whether responses are written to an in-memory buffer or directly to a file.
type ResponseWriterType string

// Supported response writer types
const (
	// WriteToBuffer indicates that responses should be written to an in-memory buffer.
	// This is useful for smaller responses that need to be processed in memory.
	WriteToBuffer ResponseWriterType = "buffer"

	// WriteToFile indicates that responses should be written directly to a file.
	// This is recommended for large responses to minimise memory usage.
	WriteToFile ResponseWriterType = "file"
)

// ResponseWriter configures where response bodies are stored.
type ResponseWriter struct {
	// Type selects in-memory buffering or file output.
	Type ResponseWriterType

	// FilePath is the destination when Type is WriteToFile.
	FilePath string
}

// SetOutput configures how the response should be written, either to a file or buffer.
// For file output, a filepath must be provided. Returns an error if the configuration is invalid.
func (opt *Option) SetOutput(writerType ResponseWriterType, filepath ...string) error {
	switch writerType {
	case WriteToFile:
		if len(filepath) == 0 {
			return ErrMissingFilePath
		}
		opt.mu.Lock()
		opt.ResponseWriter.Type = writerType
		opt.ResponseWriter.FilePath = filepath[0]
		opt.explicit |= settingOutput
		opt.mu.Unlock()
	case WriteToBuffer:
		if len(filepath) > 0 {
			return ErrUnexpectedFilePath
		}
		opt.mu.Lock()
		opt.ResponseWriter.Type = writerType
		opt.ResponseWriter.FilePath = ""
		opt.explicit |= settingOutput
		opt.mu.Unlock()
	default:
		return ErrInvalidWriterType
	}

	return nil
}

// SetFileOutput configures the response writer to write responses to a file at the specified path.
func (opt *Option) SetFileOutput(filepath string) *Option {
	opt.mu.Lock()
	opt.ResponseWriter = ResponseWriter{
		Type:     WriteToFile,
		FilePath: filepath,
	}
	opt.explicit |= settingOutput
	opt.mu.Unlock()
	return opt
}

// SetMaxBodySize limits a response body held in memory to size bytes. A longer
// body fails the request with an error that wraps client.ErrBodyTooLarge. The
// limit applies to the body after decompression, and to every buffered body,
// including the body of an error response to a file download. A download
// written to a file has no limit. A size of zero or less, the default, means
// no limit.
func (opt *Option) SetMaxBodySize(size int64) *Option {
	opt.mu.Lock()
	opt.MaxBodySize = size
	opt.explicit |= settingMaxBodySize
	opt.mu.Unlock()
	return opt
}

// SetBufferOutput configures the response writer to write responses to an in-memory buffer.
func (opt *Option) SetBufferOutput() *Option {
	opt.mu.Lock()
	opt.ResponseWriter = ResponseWriter{
		Type: WriteToBuffer,
	}
	opt.explicit |= settingOutput
	opt.mu.Unlock()
	return opt
}
