package client_test

import (
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

	t.Run("Initialised source should override booleans", func(t *testing.T) {
		// Create a properly initialised option with specific boolean values
		dest := options.New()
		dest.Logging.Enabled = true
		dest.Redirect.Follow = true
		dest.Redirect.PreserveMethod = true

		// Create an initialised option with false values
		src := options.New()
		src.Logging.Enabled = false
		src.Redirect.Follow = false
		src.Redirect.PreserveMethod = false

		// Merge - initialised source SHOULD override dest booleans
		dest.Merge(src)

		assert.False(t, dest.Logging.Enabled, "Logging.Enabled should be false after merge with initialised source")
		assert.False(t, dest.Redirect.Follow, "FollowRedirects should be false after merge with initialised source")
		assert.False(t, dest.Redirect.PreserveMethod, "PreserveMethodOnRedirect should be false after merge with initialised source")
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
