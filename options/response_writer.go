package options

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// ResponseWriterType defines how the HTTP response body should be handled.
// It determines whether responses are written to an in-memory buffer or directly to a file.
type ResponseWriterType string

// Supported response writer types
const (
	// WriteToBuffer indicates that responses should be written to an in-memory buffer.
	// This is useful for smaller responses that need to be processed in memory.
	WriteToBuffer ResponseWriterType = "buffer"

	// WriteToFile indicates that responses should be written directly to a file.
	// This is recommended for large responses to minimize memory usage.
	WriteToFile ResponseWriterType = "file"
)

// ResponseWriter contains configuration for handling HTTP response bodies.
// It supports writing responses either to an in-memory buffer or directly to a file,
// allowing for flexible response handling based on the needs of the caller.
type ResponseWriter struct {
	// Type determines the destination for response data.
	// Must be either WriteToBuffer or WriteToFile.
	Type ResponseWriterType

	// FilePath specifies the destination file path when Type is WriteToFile.
	// This field is ignored when Type is WriteToBuffer.
	// The path must be writable and will be created if it doesn't exist.
	FilePath string

	// writer is the underlying io.WriteCloser that handles the actual writing.
	// It is initialized during Option.InitialiseWriter() based on the Type.
	// For WriteToBuffer, this will be a bytes.Buffer.
	// For WriteToFile, this will be an *os.File.
	writer io.WriteCloser
}

// InitialiseWriter sets up the appropriate writer based on the ResponseWriter configuration.
// Returns an error if the writer type is invalid or if required parameters are missing.
// When resuming a download (Range.IsResume is true), files are opened in append mode.
func (opt *Option) InitialiseWriter() (io.WriteCloser, error) {
	opt.mu.Lock()
	writerType := opt.ResponseWriter.Type
	filePath := opt.ResponseWriter.FilePath
	isResume := opt.Range.IsResume
	opt.mu.Unlock()

	switch writerType {
	case WriteToFile:
		if filePath == "" {
			return nil, ErrMissingFilePath
		}
		var file *os.File
		var err error
		if isResume {
			// Open in append mode for resume operations
			file, err = os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		} else {
			// Create/truncate for new downloads
			file, err = os.Create(filePath)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		opt.mu.Lock()
		opt.ResponseWriter.writer = file
		opt.mu.Unlock()
		return file, nil
	case WriteToBuffer:
		if filePath != "" {
			return nil, ErrUnexpectedFilePath
		}
		writer := &WriteCloserBuffer{Buffer: &bytes.Buffer{}}
		opt.mu.Lock()
		opt.ResponseWriter.writer = writer
		opt.mu.Unlock()
		return writer, nil
	default:
		return nil, ErrInvalidWriterType
	}
}

// Writer returns the currently configured io.WriteCloser instance.
func (opt *Option) Writer() io.WriteCloser {
	opt.mu.RLock()
	writer := opt.ResponseWriter.writer
	opt.mu.RUnlock()
	return writer
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
		opt.mu.Unlock()
	case WriteToBuffer:
		if len(filepath) > 0 {
			return ErrUnexpectedFilePath
		}
		opt.mu.Lock()
		opt.ResponseWriter.Type = writerType
		opt.ResponseWriter.FilePath = ""
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
	opt.mu.Unlock()
	return opt
}

// SetBufferOutput configures the response writer to write responses to an in-memory buffer.
func (opt *Option) SetBufferOutput() *Option {
	opt.mu.Lock()
	opt.ResponseWriter = ResponseWriter{
		Type: WriteToBuffer,
	}
	opt.mu.Unlock()
	return opt
}
