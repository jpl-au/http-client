package client_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"testing/synctest"

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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = c.CloneOptions()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.AddHeader("K", "V")
				opt.AddCookie(&http.Cookie{Name: "C", Value: "V"})
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetTransport(&http.Transport{})
				opt.SetMaxResponseHeaderBytes(1024 * 1024)
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_, _ = opt.NewCompressor(nil)
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				opt.Log("test message", "key", "value")
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.EnableLogging()
				opt.DisableLogging()
			}
		})

		wg.Go(func() {
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.HasRange()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetRange(0, 100)
				opt.SetRangeFrom(50)
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Writer()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetFileOutput("/tmp/test.txt")
				opt.SetBufferOutput()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.MaxRedirects()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.EnableRedirects()
				opt.DisableRedirects()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.Redirects(true, true, 5)
				opt.SetMaxRedirects(10)
				opt.EnablePreserveMethod()
				opt.DisablePreserveMethod()
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.IdentifierType()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				_ = opt.GenerateIdentifier()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.HasFile()
				_ = opt.Size()
				_ = opt.Filename()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.ProgressTracking()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.TrackBeforeCompression()
				opt.TrackAfterCompression()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Client()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetClient(&http.Client{})
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Clone()
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.AddHeader("Key", "Value")
				opt.AddCookie(&http.Cookie{Name: "test", Value: "value"})
			}
		})

		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
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
			for i := 0; i < raceIterations; i++ {
				_ = opt.Clone()
			}
		})

		// Client methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				_ = opt.Client()
				opt.SetClient(&http.Client{})
				opt.UseSharedClient()
				opt.UsePerRequestClient()
			}
		})

		// Header and Cookie methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.AddHeader("K", "V")
				opt.AddCookie(&http.Cookie{Name: "C", Value: "V"})
				opt.ClearHeaders()
				opt.ClearCookies()
			}
		})

		// Transport methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetTransport(&http.Transport{})
				opt.SetMaxResponseHeaderBytes(1024)
				opt.SetProtocol(options.HTTP1)
				opt.SetProtocolScheme("https://")
			}
		})

		// Compression methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetCompression(options.CompressionGzip)
				_, _ = opt.NewCompressor(nil)
			}
		})

		// Logging methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.EnableLogging()
				opt.DisableLogging()
				opt.Log("msg")
			}
		})

		// Range methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetRange(0, 100)
				opt.SetRangeFrom(50)
				opt.SetRangeLast(100)
				_ = opt.HasRange()
				opt.ClearRange()
			}
		})

		// Redirect methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.EnableRedirects()
				opt.DisableRedirects()
				opt.SetMaxRedirects(5)
				_ = opt.MaxRedirects()
				opt.EnablePreserveMethod()
				opt.DisablePreserveMethod()
			}
		})

		// ResponseWriter methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetFileOutput("/tmp/test.txt")
				opt.SetBufferOutput()
				_ = opt.Writer()
			}
		})

		// Tracing methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetIdentifierType(options.IdentifierUUID)
				_ = opt.IdentifierType()
				_ = opt.GenerateIdentifier()
			}
		})

		// Progress methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.TrackBeforeCompression()
				opt.TrackAfterCompression()
				_ = opt.ProgressTracking()
				opt.SetDownloadBufferSize(4096)
				opt.SetUploadBufferSize(4096)
			}
		})

		// Context methods
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				opt.SetContext(context.Background())
			}
		})

		// File methods (readers only - PrepareFile needs actual file)
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				_ = opt.HasFile()
				_ = opt.Size()
				_ = opt.Filename()
			}
		})

		// Merge operations
		wg.Go(func() {
			for i := 0; i < raceIterations; i++ {
				src := options.New()
				src.AddHeader("Merge", "Test")
				opt.Merge(src)
			}
		})

		wg.Wait()
	})
}
