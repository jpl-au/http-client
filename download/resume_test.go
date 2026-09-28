package download

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

func TestCompletePartialWithWrongWriterReturnsError(t *testing.T) {
	opt := options.New().SetBufferOutput()
	r := &Resumable{standard: &standard{opt: opt}, continuation: true, offset: 3}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=3-")
	httpResp := &http.Response{
		StatusCode: http.StatusRequestedRangeNotSatisfiable,
		Header:     http.Header{"Content-Range": {"bytes */3"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
	_, err = r.Complete(httpResp, response.New("id", req.URL.String(), req.Method, nil, opt), time.Now())
	if err == nil || !strings.Contains(err.Error(), "want a partial file") {
		t.Fatalf("Complete error = %v, want writer type error", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close after rejected writer: %v", err)
	}
}
