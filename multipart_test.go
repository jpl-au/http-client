package client_test

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/form"
	"github.com/jpl-au/http-client/options"
	"github.com/jpl-au/http-client/response"
)

// receivedPart is one part of a form as a server received it.
type receivedPart struct {
	Name     string
	FileName string
	Size     int64
}

// receipt is what a form server received.
type receipt struct {
	ContentLength    int64
	BodyLength       int64
	TransferEncoding []string
	Parts            []receivedPart
}

// newFormServer returns a server that reads a multipart form and replies with
// a receipt as JSON.
func newFormServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := &countingReader{r: r.Body}
		r.Body = io.NopCloser(body)
		rec := receipt{ContentLength: r.ContentLength, TransferEncoding: r.TransferEncoding}
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			size, err := io.Copy(io.Discard, p)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rec.Parts = append(rec.Parts, receivedPart{p.FormName(), p.FileName(), size})
		}
		if _, err := io.Copy(io.Discard, body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.BodyLength = body.n
		if err := json.NewEncoder(w).Encode(rec); err != nil {
			t.Errorf("Encode() error = %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func TestFormUpload(t *testing.T) {
	t.Run("methods", func(t *testing.T) {
		server := newFormServer(t)
		c := client.New()
		tests := []struct {
			name string
			send func(url string, payload any, opts ...*options.Option) (response.Response, error)
		}{
			{"post", client.Post},
			{"put", client.Put},
			{"patch", client.Patch},
			{"client post", c.Post},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var lastProgress float64
				opt := options.New().OnUploadProgress(func(bytesRead, totalBytes int64) {
					if totalBytes > 0 {
						lastProgress = float64(bytesRead) / float64(totalBytes) * 100
					}
				})
				f := form.New().
					Field("title", "Report").
					File("attachment", smallf).
					File("attachment", largef).
					Field("tag", "x")

				resp, err := tt.send(server.URL, f, opt)
				if err != nil {
					t.Fatalf("send error = %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("StatusCode = %d, want %d: %s", resp.StatusCode, http.StatusOK, resp.String())
				}
				var rec receipt
				if err := json.Unmarshal(resp.Bytes(), &rec); err != nil {
					t.Fatalf("Unmarshal() error = %v", err)
				}

				want := []receivedPart{
					{"title", "", int64(len("Report"))},
					{"attachment", filepath.Base(smallf), int64(smallfile.Len())},
					{"attachment", filepath.Base(largef), int64(largefile.Len())},
					{"tag", "", 1},
				}
				if !slices.Equal(rec.Parts, want) {
					t.Errorf("parts = %+v, want %+v", rec.Parts, want)
				}
				if rec.ContentLength < 0 || rec.ContentLength != rec.BodyLength {
					t.Errorf("Content-Length = %d, body length = %d, want the exact length", rec.ContentLength, rec.BodyLength)
				}
				if len(rec.TransferEncoding) != 0 {
					t.Errorf("Transfer-Encoding = %q, want none", rec.TransferEncoding)
				}
				if lastProgress != 100 {
					t.Errorf("upload progress = %v, want 100", lastProgress)
				}
			})
		}
	})

	// A 307 or 308 redirect sends the whole form again.
	t.Run("follows redirect", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data.txt")
		if err := os.WriteFile(path, []byte("file content"), 0o644); err != nil {
			t.Fatal(err)
		}

		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			t.Run(strconv.Itoa(status), func(t *testing.T) {
				server, received := newRedirectServer(t, "")
				f := form.New().Field("title", "Report").File("attachment", path)

				_, err := client.Post(server.URL+"/redirect/"+strconv.Itoa(status), f, options.New().EnableRedirects())
				if err != nil {
					t.Fatalf("Post() error = %v", err)
				}
				hops := received()
				if len(hops) != 1 {
					t.Fatalf("destination received %d requests, want 1", len(hops))
				}

				_, params, err := mime.ParseMediaType(hops[0].Header.Get("Content-Type"))
				if err != nil {
					t.Fatalf("ParseMediaType() error = %v", err)
				}
				mr := multipart.NewReader(strings.NewReader(hops[0].Body), params["boundary"])
				var got []string
				for {
					p, err := mr.NextPart()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatalf("NextPart() error = %v", err)
					}
					content, err := io.ReadAll(p)
					if err != nil {
						t.Fatalf("ReadAll() error = %v", err)
					}
					got = append(got, p.FormName()+"="+string(content))
				}
				if want := []string{"title=Report", "attachment=file content"}; !slices.Equal(got, want) {
					t.Errorf("destination parts = %q, want %q", got, want)
				}
			})
		}
	})

	// A file that cannot be read fails the request before it is sent, and the
	// response records the error.
	t.Run("missing file", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
		}))
		defer server.Close()

		missing := filepath.Join(t.TempDir(), "missing.txt")
		resp, err := client.Post(server.URL, form.New().File("attachment", missing))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Post() error = %v, want %v", err, fs.ErrNotExist)
		}
		if resp.Error != err {
			t.Errorf("Response.Error = %v, want %v", resp.Error, err)
		}
		if got := requests.Load(); got != 0 {
			t.Errorf("server received %d requests, want 0", got)
		}
	})

	// Uploading a file does not hold the file in memory: the upload allocates
	// much less than the file's size.
	t.Run("streams files", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Errorf("Copy() error = %v", err)
			}
		}))
		defer server.Close()

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if _, err := client.Post(server.URL, form.New().File("attachment", largef)); err != nil {
			t.Fatalf("Post() error = %v", err)
		}
		runtime.ReadMemStats(&after)

		if allocated, limit := after.TotalAlloc-before.TotalAlloc, uint64(largefile.Len()/4); allocated > limit {
			t.Errorf("upload of %d bytes allocated %d bytes, want at most %d", largefile.Len(), allocated, limit)
		}
	})
}
