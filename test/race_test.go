package client_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
)

const raceIterations = 1000

// TestOptionRace verifies that concurrent calls to Clone() and Merge() on the same Option
// instance do not cause data races.
func TestOptionRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()
		opt.AddHeader("Initial", "Value")

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				src := options.New()
				src.AddHeader("New", "Value")
				opt.Merge(src)
			}
		})

		wg.Wait()
	})
}

// TestClientGlobalOptionsRace checks for race conditions when a Client's global options
// are being modified while other goroutines are cloning those options.
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

		wg.Wait()
	})
}

// TestOptionSettersRace ensures that various setter methods on the Option struct
// can be called concurrently with Clone().
func TestOptionSettersRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.AddHeader("K", "V")
				opt.AddCookie(&http.Cookie{Name: "C", Value: "V"})
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetContext(context.Background())
			}
		})

		wg.Wait()
	})
}

// TestTransportMethodsRace tests concurrent access to transport-related methods.
func TestTransportMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetTransport(&http.Transport{})
				opt.SetMaxResponseHeaderBytes(1024 * 1024)
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetProtocol(options.HTTP1)
				opt.SetProtocolScheme("https://")
			}
		})

		wg.Wait()
	})
}

// TestCompressionMethodsRace tests concurrent access to compression-related methods.
func TestCompressionMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_, _ = opt.NewCompressor(nil)
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetCompression(options.CompressionGzip)
				opt.SetCompression(options.CompressionNone)
			}
		})

		wg.Wait()
	})
}

// TestLoggingMethodsRace tests concurrent access to logging-related methods.
func TestLoggingMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				opt.Log("test message", "key", "value")
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.EnableLogging()
				opt.DisableLogging()
			}
		})

		wg.Go(func() {
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			for range raceIterations {
				opt.SetLogger(logger)
				opt.UseTextLogger()
			}
		})

		wg.Wait()
	})
}

// TestRangeMethodsRace tests concurrent access to range-related methods.
func TestRangeMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.HasRange()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetRange(0, 100)
				opt.SetRangeFrom(50)
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetRangeLast(1024)
				opt.ClearRange()
			}
		})

		wg.Wait()
	})
}

// TestResponseWriterMethodsRace tests concurrent access to response writer methods.
func TestResponseWriterMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Writer()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetFileOutput("/tmp/test.txt")
				opt.SetBufferOutput()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				_ = opt.SetOutput(options.WriteToBuffer)
				_ = opt.SetOutput(options.WriteToFile, "/tmp/test.txt")
			}
		})

		wg.Wait()
	})
}

// TestRedirectMethodsRace tests concurrent access to redirect-related methods.
func TestRedirectMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.MaxRedirects()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.EnableRedirects()
				opt.DisableRedirects()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.Redirects(true, 5)
				opt.SetMaxRedirects(10)
			}
		})

		wg.Wait()
	})
}

// TestTracingMethodsRace tests concurrent access to tracing-related methods.
func TestTracingMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.IdentifierType()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				_ = opt.GenerateIdentifier()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetIdentifierType(options.IdentifierUUID)
				opt.SetIdentifierType(options.IdentifierULID)
			}
		})

		wg.Wait()
	})
}

// TestFileMethodsRace tests concurrent access to file-related methods.
func TestFileMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.HasFile()
				_ = opt.Size()
				_ = opt.Filename()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				// PrepareFile will fail if file doesn't exist, but we're testing for races
				_ = opt.PrepareFile("test-small.txt")
			}
		})

		wg.Wait()
	})
}

// TestProgressMethodsRace tests concurrent access to progress-related methods.
func TestProgressMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.ProgressTracking()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.TrackBeforeCompression()
				opt.TrackAfterCompression()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetDownloadBufferSize(4096)
				opt.SetUploadBufferSize(4096)
				opt.OnUploadProgress(func(bytesRead, totalBytes int64) {})
				opt.OnDownloadProgress(func(bytesRead, totalBytes int64) {})
			}
		})

		wg.Wait()
	})
}

// TestClientMethodsRace tests concurrent access to client-related methods on Option.
func TestClientMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Client()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.SetClient(&http.Client{})
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.UseSharedClient()
				opt.UsePerRequestClient()
			}
		})

		wg.Wait()
	})
}

// TestClearMethodsRace tests concurrent access to clear methods.
func TestClearMethodsRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.AddHeader("Key", "Value")
				opt.AddCookie(&http.Cookie{Name: "test", Value: "value"})
			}
		})

		wg.Go(func() {
			for range raceIterations {
				opt.ClearHeaders()
				opt.ClearCookies()
			}
		})

		wg.Wait()
	})
}

// TestAllMethodsConcurrent is a comprehensive test that exercises all Option methods
// concurrently to detect any race conditions.
func TestAllMethodsConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opt := options.New()

		var wg sync.WaitGroup

		// Clone operations (readers)
		wg.Go(func() {
			for range raceIterations {
				_ = opt.Clone()
			}
		})

		// Client methods
		wg.Go(func() {
			for range raceIterations {
				_ = opt.Client()
				opt.SetClient(&http.Client{})
				opt.UseSharedClient()
				opt.UsePerRequestClient()
			}
		})

		// Header and Cookie methods
		wg.Go(func() {
			for range raceIterations {
				opt.AddHeader("K", "V")
				opt.AddCookie(&http.Cookie{Name: "C", Value: "V"})
				opt.ClearHeaders()
				opt.ClearCookies()
			}
		})

		// Transport methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetTransport(&http.Transport{})
				opt.SetMaxResponseHeaderBytes(1024)
				opt.SetProtocol(options.HTTP1)
				opt.SetProtocolScheme("https://")
			}
		})

		// Compression methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetCompression(options.CompressionGzip)
				_, _ = opt.NewCompressor(nil)
			}
		})

		// Logging methods
		wg.Go(func() {
			for range raceIterations {
				opt.EnableLogging()
				opt.DisableLogging()
				opt.Log("msg")
			}
		})

		// Range methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetRange(0, 100)
				opt.SetRangeFrom(50)
				opt.SetRangeLast(100)
				_ = opt.HasRange()
				opt.ClearRange()
			}
		})

		// Redirect methods
		wg.Go(func() {
			for range raceIterations {
				opt.EnableRedirects()
				opt.DisableRedirects()
				opt.SetMaxRedirects(5)
				_ = opt.MaxRedirects()
			}
		})

		// ResponseWriter methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetFileOutput("/tmp/test.txt")
				opt.SetBufferOutput()
				_ = opt.Writer()
			}
		})

		// Tracing methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetIdentifierType(options.IdentifierUUID)
				_ = opt.IdentifierType()
				_ = opt.GenerateIdentifier()
			}
		})

		// Progress methods
		wg.Go(func() {
			for range raceIterations {
				opt.TrackBeforeCompression()
				opt.TrackAfterCompression()
				_ = opt.ProgressTracking()
				opt.SetDownloadBufferSize(4096)
				opt.SetUploadBufferSize(4096)
			}
		})

		// Context methods
		wg.Go(func() {
			for range raceIterations {
				opt.SetContext(context.Background())
			}
		})

		// File methods (readers only - PrepareFile needs actual file)
		wg.Go(func() {
			for range raceIterations {
				_ = opt.HasFile()
				_ = opt.Size()
				_ = opt.Filename()
			}
		})

		// Merge operations
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

// TestClientUpdateGlobalOptionsRace checks that replacing a Client's global options
// is safe while other goroutines clone them.
func TestClientUpdateGlobalOptionsRace(t *testing.T) {
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
				c.UpdateGlobalOptions(options.New())
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
