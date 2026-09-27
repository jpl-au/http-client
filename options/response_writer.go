package options

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// FileWriter writes a download to a temporary file beside its destination.
// Close renames the temporary file to the destination, so the destination
// changes only when the whole body has arrived. Discard removes the temporary
// file and leaves the destination as it was.
type FileWriter struct {
	*os.File
	path string
}

// Close closes the temporary file and renames it to the destination.
func (w *FileWriter) Close() error {
	if err := w.File.Close(); err != nil {
		return errors.Join(err, os.Remove(w.Name()))
	}
	if err := os.Rename(w.Name(), w.path); err != nil {
		return errors.Join(err, os.Remove(w.Name()))
	}
	return nil
}

// Discard closes and removes the temporary file.
func (w *FileWriter) Discard() error {
	return errors.Join(w.File.Close(), os.Remove(w.Name()))
}

// PartialWriter writes a resumed download to its partial file (see PartialPath).
// Close keeps the partial file so a later Resume can continue it. Discard
// removes the bytes this response added. Publish renames the complete partial
// file to the destination.
type PartialWriter struct {
	*os.File
	path   string
	offset int64 // Size of the partial file before this response.
}

// Discard truncates the partial file to its size before this response, so it
// holds only data that passed validation, and closes it.
func (w *PartialWriter) Discard() error {
	if err := w.Truncate(w.offset); err != nil {
		return errors.Join(err, w.File.Close())
	}
	return w.File.Close()
}

// Publish closes the partial file and renames it to the destination.
func (w *PartialWriter) Publish() error {
	if err := matchDestinationPermissions(w.File, w.path); err != nil {
		return errors.Join(err, w.File.Close())
	}
	if err := w.File.Close(); err != nil {
		return err
	}
	return os.Rename(w.Name(), w.path)
}

// matchDestinationPermissions applies an existing destination's access mode
// before appending or publishing. With no destination, keep the partial file's
// current permissions. In particular, do not widen an imported private file.
func matchDestinationPermissions(file *os.File, dest string) error {
	info, err := os.Stat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Chmod(info.Mode().Perm())
}

// createFor creates the new file name, which will later replace dest.
// When dest exists, the file takes its permissions, so publishing a download
// never changes who can read it. Otherwise the file gets 0666 less the process
// umask, as os.Create would give.
func createFor(name, dest string) (*os.File, error) {
	perm := fs.FileMode(0o666)
	info, err := os.Stat(dest)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	exists := err == nil
	if exists {
		perm = info.Mode().Perm()
	}

	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	// The umask can narrow perm; the replacement must match dest exactly.
	if exists {
		if err := file.Chmod(perm); err != nil {
			return nil, errors.Join(err, file.Close(), os.Remove(name))
		}
	}
	return file, nil
}

// InitialiseWriter sets up the appropriate writer based on the ResponseWriter configuration.
// Returns an error if the writer type is invalid or if required parameters are missing.
// A resumed download (Range.IsResume is true) goes to a PartialWriter: it appends
// to the partial file when a range is set, and starts the partial file again
// otherwise. Any other file download goes to a FileWriter.
func (opt *Option) InitialiseWriter() (io.WriteCloser, error) {
	opt.mu.Lock()
	writerType := opt.ResponseWriter.Type
	filePath := opt.ResponseWriter.FilePath
	isResume := opt.Range.IsResume
	isContinuation := opt.Range.IsSet
	opt.mu.Unlock()

	switch writerType {
	case WriteToFile:
		if filePath == "" {
			return nil, ErrMissingFilePath
		}
		var writer io.WriteCloser
		if isResume {
			partial := PartialPath(filePath)
			var file *os.File
			var offset int64
			var err error
			if isContinuation {
				file, err = os.OpenFile(partial, os.O_WRONLY|os.O_APPEND, 0)
				if err == nil {
					err = matchDestinationPermissions(file, filePath)
					if err == nil {
						offset, err = file.Seek(0, io.SeekEnd)
					}
					if err != nil {
						err = errors.Join(err, file.Close())
					}
				}
			} else {
				if err := os.Remove(partial); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return nil, fmt.Errorf("failed to remove partial file: %w", err)
				}
				file, err = createFor(partial, filePath)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to open partial file: %w", err)
			}
			writer = &PartialWriter{File: file, path: filePath, offset: offset}
		} else {
			// The temporary file must be in the destination's directory:
			// a rename across file systems fails.
			file, err := createFor(filePath+"."+rand.Text()+".part", filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to create file: %w", err)
			}
			writer = &FileWriter{File: file, path: filePath}
		}
		opt.mu.Lock()
		opt.ResponseWriter.writer = writer
		opt.mu.Unlock()
		return writer, nil
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
