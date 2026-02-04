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
	opt := options.New(opts...)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for key, value := range payload {
		switch v := value.(type) {
		case *os.File:
			part, err := writer.CreateFormFile(key, filepath.Base(v.Name()))
			if err != nil {
				return response.Response{}, err
			}
			_, err = io.Copy(part, v)
			if err != nil {
				return response.Response{}, err
			}
		default:
			if err := writer.WriteField(key, fmt.Sprintf("%v", v)); err != nil {
				return response.Response{}, err
			}
		}
	}

	writer.Close()

	// Wrap the buffer with a ProgressReader if upload progress is enabled
	var finalReader io.Reader = body
	if opt.Progress.OnUpload != nil {
		finalReader = options.NewProgressReader(body, int64(body.Len()), opt.Progress.OnUpload)
	}

	opt.AddHeader(ContentType, writer.FormDataContentType())
	return doRequest(method, url, finalReader, opt)
}
