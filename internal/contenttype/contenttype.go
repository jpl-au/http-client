// Package contenttype detects the content type of a file to upload.
package contenttype

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
)

// Detect returns the content type of the file r named name. The file name's
// extension decides when it has a known type. Otherwise the first 512 bytes
// decide, as http.DetectContentType reads them. Detect leaves r at its start.
func Detect(r io.ReadSeeker, name string) (string, error) {
	if byExtension := mime.TypeByExtension(filepath.Ext(name)); byExtension != "" {
		return byExtension, nil
	}

	// An empty file reads io.EOF at once and still has a type.
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}
