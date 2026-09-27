package client_test

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

const raceIterations = 1000

// TestClientGlobalOptionsRace checks that a Client's global options can be
// merged into and replaced while other goroutines clone them.
func TestClientGlobalOptionsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := client.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = c.CloneOptions()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				newOpt := options.New()
				newOpt.AddHeader("Iter", "Val")
				c.AddGlobalOptions(newOpt)
			}
		})

		wg.Go(func() {
			for range raceIterations {
				c.UpdateGlobalOptions(options.New())
			}
		})

		wg.Wait()
	})
}

// TestOptionMethodsRace calls each group of Option setters from its own
// goroutine, and every getter from another. The race detector reports only
// unsynchronised accesses from different goroutines, so a getter must not
// share a goroutine with the setters it reads after.
func TestOptionMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		// Getters
		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
				_ = opt.Client()
				if _, err := opt.NewCompressor(nil); err != nil {
					t.Errorf("NewCompressor() error = %v", err)
				}
				opt.Log("msg", "key", "value")
				_ = opt.HasRange()
				_ = opt.MaxRedirects()
				_ = opt.Writer()
				_ = opt.IdentifierType()
				_ = opt.GenerateIdentifier()
				_ = opt.ProgressTracking()
				_ = opt.HasFile()
				_ = opt.Size()
				_ = opt.Filename()
			}
		})

		// Client setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetClient(&http.Client{})
				opt.UseSharedClient()
				opt.UsePerRequestClient()
			}
		})

		// Header and cookie setters
		wg.Go(func() {
			for range raceIterations {
				opt.AddHeader("K", "V")
				opt.AddCookie(&http.Cookie{Name: "C", Value: "V"})
				opt.ClearHeaders()
				opt.ClearCookies()
			}
		})

		// Transport setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetTransport(&http.Transport{})
				opt.SetMaxResponseHeaderBytes(1024)
				opt.SetMaxBodySize(1024)
				opt.SetProtocol(options.HTTP1)
				opt.SetProtocolScheme("https://")
			}
		})

		// Compression setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetCompression(options.CompressionGzip)
				opt.SetCompression(options.CompressionNone)
			}
		})

		// Logging setters
		wg.Go(func() {
			for range raceIterations {
				opt.EnableLogging()
				opt.DisableLogging()
				opt.SetLogger(slog.New(slog.DiscardHandler))
				opt.UseTextLogger()
			}
		})

		// Range setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetRange(0, 100)
				opt.SetRangeFrom(50)
				opt.SetRangeLast(100)
				opt.ClearRange()
			}
		})

		// Redirect setters
		wg.Go(func() {
			for range raceIterations {
				opt.EnableRedirects()
				opt.DisableRedirects()
				opt.SetMaxRedirects(5)
				opt.Redirects(true, 5)
			}
		})

		// Response writer setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetFileOutput("/tmp/test.txt")
				opt.SetBufferOutput()
				if err := opt.SetOutput(options.WriteToBuffer); err != nil {
					t.Errorf("SetOutput(WriteToBuffer) error = %v", err)
				}
				if err := opt.SetOutput(options.WriteToFile, "/tmp/test.txt"); err != nil {
					t.Errorf("SetOutput(WriteToFile) error = %v", err)
				}
			}
		})

		// Tracing setters
		wg.Go(func() {
			for range raceIterations {
				opt.SetIdentifierType(options.IdentifierUUID)
				opt.SetIdentifierType(options.IdentifierULID)
			}
		})

		// Progress setters
		wg.Go(func() {
			for range raceIterations {
				opt.TrackBeforeCompression()
				opt.TrackAfterCompression()
				opt.SetDownloadBufferSize(4096)
				opt.SetUploadBufferSize(4096)
				opt.OnUploadProgress(func(bytesRead, totalBytes int64) {})
				opt.OnDownloadProgress(func(bytesRead, totalBytes int64) {})
			}
		})

		// Context setter
		wg.Go(func() {
			for range raceIterations {
				opt.SetContext(context.Background())
			}
		})

		// File setter
		wg.Go(func() {
			for range raceIterations {
				if err := opt.PrepareFile(smallf); err != nil {
					t.Errorf("PrepareFile() error = %v", err)
				}
			}
		})

		// Merge
		wg.Go(func() {
			for range raceIterations {
				src := options.New()
				src.AddHeader("Merge", "Test")
				opt.Merge(src)
			}
		})

		wg.Wait()
	})
}

// TestMergeSourceRace checks that Merge reads its source safely while
// another goroutine changes that source.
func TestMergeSourceRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := options.New()
		dst := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				src.SetContext(context.Background())
				src.AddHeader("K", "V")
			}
		})

		wg.Go(func() {
			for range raceIterations {
				dst.Merge(src)
			}
		})

		wg.Wait()
	})
}

// TestMergeDoesNotDeadlock checks that self-merge and two Options merging
// into each other at the same time both complete.
func TestMergeDoesNotDeadlock(t *testing.T) {
	a := options.New()
	b := options.New()

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Merge(a)

		var wg sync.WaitGroup
		wg.Go(func() {
			for range raceIterations {
				a.Merge(b)
			}
		})
		wg.Go(func() {
			for range raceIterations {
				b.Merge(a)
			}
		})
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Merge deadlocked")
	}
}
