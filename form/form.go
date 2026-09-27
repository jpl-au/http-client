package form

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"

	"github.com/jpl-au/http-client/options"
)

// ErrFileChanged is returned when a file's size changes between the check
// before sending and the upload of its content.
var ErrFileChanged = errors.New("file changed size during upload")

// Form is a multipart/form-data body of fields and files. The parts are sent
// in the order they were added, and a name can be used more than once.
// Add every part before sending the form.
type Form struct {
	boundary string
	parts    []part
}

// part is a field, or a file read from its path when the form is sent.
type part struct {
	name  string
	value string // The field's value, or the file's path.
	file  bool
}

// New returns an empty Form.
func New() *Form {
	return &Form{boundary: multipart.NewWriter(io.Discard).Boundary()}
}

// Field adds a text field.
func (f *Form) Field(name, value string) *Form {
	f.parts = append(f.parts, part{name: name, value: value})
	return f
}

// File adds the file at path. The file is read when the form is sent, and read
// again if a redirect sends the form again. Its file name in the form is the
// last element of path, and its content type is detected from its name and
// content.
func (f *Form) File(name, path string) *Form {
	f.parts = append(f.parts, part{name: name, value: path, file: true})
	return f
}

// ContentType returns the Content-Type of the form, with its boundary.
func (f *Form) ContentType() string {
	return f.writer(io.Discard).FormDataContentType()
}

// Len checks every file and returns the length of the encoded form in bytes.
// A file that cannot be read returns an error.
func (f *Form) Len() (int64, error) {
	parts, err := f.plan()
	if err != nil {
		return 0, err
	}
	var n counter
	err = f.write(&n, parts, func(_ io.Writer, p planned) error {
		n += counter(p.size)
		return nil
	})
	return int64(n), err
}

// Reader returns the encoded form. The form is encoded as it is read, and a
// file is read in small pieces, so the whole form is never held in memory.
// Reader checks every file before it sends any data. A file whose size then
// changes returns an error that wraps ErrFileChanged. Close stops the encoding.
func (f *Form) Reader() io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		parts, err := f.plan()
		if err == nil {
			err = f.write(pw, parts, copyFile)
		}
		// CloseWithError always returns nil.
		pw.CloseWithError(err)
	}()
	return pr
}

// planned is a part as it is sent: a file part also has its header and size.
type planned struct {
	part
	header textproto.MIMEHeader
	size   int64
}

// plan checks each file and returns the parts with their headers and sizes.
func (f *Form) plan() ([]planned, error) {
	parts := make([]planned, len(f.parts))
	for i, p := range f.parts {
		parts[i].part = p
		if !p.file {
			continue
		}
		header, size, err := fileHeader(p)
		if err != nil {
			return nil, fmt.Errorf("form file %q: %w", p.name, err)
		}
		parts[i].header = header
		parts[i].size = size
	}
	return parts, nil
}

// fileHeader returns the part header and the size of the file part p.
func fileHeader(p part) (_ textproto.MIMEHeader, _ int64, err error) {
	file, err := os.Open(p.value)
	if err != nil {
		return nil, 0, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	contentType, err := options.DetectContentType(file, p.value)
	if err != nil {
		return nil, 0, err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition(p.name, filepath.Base(p.value)))
	header.Set("Content-Type", contentType)
	return header, info.Size(), nil
}

// write encodes the parts to w. body writes the content of each file part.
func (f *Form) write(w io.Writer, parts []planned, body func(io.Writer, planned) error) error {
	mw := f.writer(w)
	for _, p := range parts {
		if !p.file {
			if err := mw.WriteField(p.name, p.value); err != nil {
				return err
			}
			continue
		}
		pw, err := mw.CreatePart(p.header)
		if err != nil {
			return err
		}
		if err := body(pw, p); err != nil {
			return err
		}
	}
	return mw.Close()
}

// writer returns a multipart writer to w with the form's boundary.
func (f *Form) writer(w io.Writer) *multipart.Writer {
	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(f.boundary); err != nil {
		// The boundary came from multipart.NewWriter, so it is always valid.
		panic(err)
	}
	return mw
}

// copyFile writes the content of the file part p to w, and returns an error
// wrapping ErrFileChanged when the file no longer has the planned size.
func copyFile(w io.Writer, p planned) (err error) {
	file, err := os.Open(p.value)
	if err != nil {
		return fmt.Errorf("form file %q: %w", p.name, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	if _, err := io.CopyN(w, file, p.size); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: %s is shorter than %d bytes", ErrFileChanged, p.value, p.size)
		}
		return err
	}
	extra, err := io.CopyN(io.Discard, file, 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if extra > 0 {
		return fmt.Errorf("%w: %s is longer than %d bytes", ErrFileChanged, p.value, p.size)
	}
	return nil
}

// counter is an io.Writer that counts the bytes written to it.
type counter int64

// Write counts the bytes of b.
func (c *counter) Write(b []byte) (int, error) {
	*c += counter(len(b))
	return len(b), nil
}
