package options

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

// FileConfig holds file upload metadata.
type FileConfig struct {
	// path is the path to the file being uploaded
	path string

	// size is the size of the file in bytes
	size int64
}

// PrepareFile validates a file for upload and stores its metadata.
// It checks that the file exists and is accessible, stores the filename
// and size, infers the content type, and sets Content-Disposition headers.
// The file is NOT opened - use OpenFile() to get a fresh file handle.
func (opt *Option) PrepareFile(filename string) error {
	fileinfo, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrFileNotFound, filename)
		}
		return fmt.Errorf("failed to access file: %w", err)
	}

	opt.mu.Lock()
	opt.File.path = filename
	opt.File.size = fileinfo.Size()
	opt.mu.Unlock()

	// Open temporarily to infer content type, then close
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	if err := opt.inferContentType(file, fileinfo); err != nil {
		return fmt.Errorf("failed to infer content type: %w", err)
	}

	// Add Content-Disposition header
	contentDisposition := fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(filename))
	opt.AddHeader("Content-Disposition", contentDisposition)

	return nil
}

// OpenFile opens and returns a fresh file handle for the prepared file.
// This should be called each time a file read is needed (initial request,
// redirects, retries). The caller is responsible for closing the returned file.
// Returns an error if no file has been prepared or if opening fails.
func (opt *Option) OpenFile() (*os.File, error) {
	opt.mu.RLock()
	path := opt.File.path
	size := opt.File.size
	opt.mu.RUnlock()

	if path == "" {
		return nil, ErrFileNotPrepared
	}
	opt.Log("Opening file", "filename", path, "filesize", size)
	return os.Open(path)
}

// HasFile returns true if a file has been prepared for upload.
func (opt *Option) HasFile() bool {
	opt.mu.RLock()
	hasFile := opt.File.path != ""
	opt.mu.RUnlock()
	return hasFile
}

// Size returns the size in bytes of the prepared file.
// Returns 0 if no file has been prepared.
func (opt *Option) Size() int64 {
	opt.mu.RLock()
	size := opt.File.size
	opt.mu.RUnlock()
	return size
}

// Filename returns the path of the prepared file.
// Returns empty string if no file has been prepared.
func (opt *Option) Filename() string {
	opt.mu.RLock()
	path := opt.File.path
	opt.mu.RUnlock()
	return path
}

// SetPath sets the file path directly on the FileConfig.
// This is used internally when an *os.File is passed as payload
// to enable path-based reopening for redirects.
func (f *FileConfig) SetPath(path string) {
	f.path = path
}

// SetSize sets the file size directly on the FileConfig.
func (f *FileConfig) SetSize(size int64) {
	f.size = size
}

// inferContentType determines the MIME type of a file based on its content and extension.
// If it is unable to determine a MIME type, it defaults to application/octet-stream.
func (opt *Option) inferContentType(file *os.File, fileInfo os.FileInfo) error {
	// check if a content type has already been defined
	opt.mu.RLock()
	hasContentType := opt.Header.Get("Content-Type") != ""
	opt.mu.RUnlock()
	if hasContentType {
		return nil
	}

	// default content type: application/octet-stream
	contentType := "application/octet-stream"

	// Use a buffer to read a portion of the file for detecting its MIME type.
	buffer := make([]byte, 512)
	_, err := file.Read(buffer)
	if err != nil {
		return err
	}

	// Reset the file pointer after reading.
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}

	// Try to detect MIME type from file content.
	detectedContentType := http.DetectContentType(buffer)
	if detectedContentType != "" {
		contentType = detectedContentType
	}

	// Check for MIME type based on file extension and use it if available.
	extMimeType := mime.TypeByExtension(filepath.Ext(fileInfo.Name()))
	if extMimeType != "" {
		contentType = extMimeType
	}

	opt.AddHeader("Content-Type", contentType)
	return nil
}
