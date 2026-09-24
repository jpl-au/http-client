package client_test

import (
	"log/slog"
	"testing"

	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
)

// TestOptionsMergeInitialised tests that Merge respects the initialised flag for boolean fields
func TestOptionsMergeInitialised(t *testing.T) {
	t.Run("Uninitialised source should not override booleans", func(t *testing.T) {
		// Create a properly initialised option with specific boolean values
		dest := options.New()
		dest.Logging.Enabled = true
		dest.Redirect.Follow = true
		dest.Redirect.PreserveMethod = true

		// Create an uninitialised option (zero values for booleans)
		src := &options.Option{}

		// Merge - uninitialised source should NOT override dest booleans
		dest.Merge(src)

		assert.True(t, dest.Logging.Enabled, "Logging.Enabled should remain true after merge with uninitialised source")
		assert.True(t, dest.Redirect.Follow, "FollowRedirects should remain true after merge with uninitialised source")
		assert.True(t, dest.Redirect.PreserveMethod, "PreserveMethodOnRedirect should remain true after merge with uninitialised source")
	})

	t.Run("Default source should not override booleans", func(t *testing.T) {
		dest := options.New()
		dest.Logging.Enabled = true
		dest.Redirect.Follow = true
		dest.Redirect.PreserveMethod = true
		dest.TrackAfterCompression()
		dest.SetMaxRedirects(3)
		dest.SetIdentifierType(options.IdentifierUUID)
		dest.UserAgent = "custom-agent"
		dest.SetLogger(slog.New(slog.DiscardHandler))
		dest.SetFileOutput("download.bin")

		// A default source carries no choices, so it must not reset dest
		dest.Merge(options.New())

		assert.True(t, dest.Logging.Enabled, "Logging.Enabled should remain true after merge with a default source")
		assert.True(t, dest.Redirect.Follow, "FollowRedirects should remain true after merge with a default source")
		assert.True(t, dest.Redirect.PreserveMethod, "PreserveMethodOnRedirect should remain true after merge with a default source")
		assert.Equal(t, options.TrackAfterCompression, dest.ProgressTracking(), "Tracking should remain after merge with a default source")
		assert.Equal(t, 3, dest.MaxRedirects(), "MaxRedirects should remain after merge with a default source")
		assert.Equal(t, options.IdentifierUUID, dest.IdentifierType(), "IdentifierType should remain after merge with a default source")
		assert.Equal(t, "custom-agent", dest.UserAgent, "UserAgent should remain after merge with a default source")
		assert.Equal(t, slog.DiscardHandler, dest.Logging.Logger.Handler(), "Logger should remain after merge with a default source")
		assert.Equal(t, options.WriteToFile, dest.ResponseWriter.Type, "file output should remain after merge with a default source")
	})

	t.Run("Setters that choose a zero value should override", func(t *testing.T) {
		dest := options.New().
			EnableLogging().
			EnableRedirects().
			EnablePreserveMethod().
			TrackAfterCompression().
			SetCompression(options.CompressionGzip).
			SetIdentifierType(options.IdentifierUUID).
			SetProtocol(options.HTTP1).
			SetMaxResponseHeaderBytes(1024).
			SetRange(0, 99).
			SetFileOutput("download.bin")

		src := options.New().
			DisableLogging().
			DisableRedirects().
			DisablePreserveMethod().
			TrackBeforeCompression().
			SetCompression(options.CompressionNone).
			SetIdentifierType(options.IdentifierNone).
			SetProtocol(options.Both).
			SetMaxResponseHeaderBytes(0).
			ClearRange().
			SetBufferOutput()

		dest.Merge(src)

		assert.False(t, dest.Logging.Enabled)
		assert.False(t, dest.Redirect.Follow)
		assert.False(t, dest.Redirect.PreserveMethod)
		assert.Equal(t, options.TrackBeforeCompression, dest.ProgressTracking())
		assert.Equal(t, options.CompressionNone, dest.Compression.Type)
		assert.Equal(t, options.IdentifierNone, dest.IdentifierType())
		assert.Equal(t, options.Both, dest.Transport.Protocol)
		assert.Zero(t, dest.Transport.MaxResponseHeaderBytes)
		assert.False(t, dest.HasRange())
		assert.Equal(t, options.WriteToBuffer, dest.ResponseWriter.Type)
	})

	t.Run("MaxRedirects zero should not override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := &options.Option{} // uninitialised, MaxRedirects = 0

		dest.Merge(src)

		assert.Equal(t, 15, dest.Redirect.Max, "MaxRedirects should remain 15 after merge with zero value")
	})

	t.Run("MaxRedirects non-zero should override", func(t *testing.T) {
		dest := options.New()
		dest.Redirect.Max = 15

		src := options.New()
		src.Redirect.Max = 5

		dest.Merge(src)

		assert.Equal(t, 5, dest.Redirect.Max, "MaxRedirects should be 5 after merge")
	})
}
