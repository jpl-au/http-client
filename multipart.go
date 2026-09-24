package client

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// MultipartUpload performs a multipart form-data upload request to the specified URL.
// It supports file uploads and other form fields.
func MultipartUpload(method, url string, payload map[string]any, opts ...*options.Option) (response.Response, error) {
	return doRequest(method, url, multipartForm(payload), opts...)
}

// multipartForm is a payload of form fields and files. The form is encoded
// inside the request, so a file that cannot be read fails like any other
// request: the response records the error.
type multipartForm map[string]any

// encode returns the form as a multipart/form-data body and its content type.
// An *os.File value becomes a file part; any other value becomes a field.
func (f multipartForm) encode() ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for key, value := range f {
		switch v := value.(type) {
		case *os.File:
			part, err := writer.CreateFormFile(key, filepath.Base(v.Name()))
			if err != nil {
				return nil, "", err
			}
			if _, err := io.Copy(part, v); err != nil {
				return nil, "", err
			}
		default:
			if err := writer.WriteField(key, fmt.Sprintf("%v", v)); err != nil {
				return nil, "", err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}
