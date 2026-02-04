package options

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

// rgsLength defines the length of the random string generated for IdentifierRGS
const rgsLength = 15

// UniqueIdentifierType defines the type of unique identifier to use for request tracing.
// It supports both UUID and ULID formats.
type UniqueIdentifierType string

// Supported identifier types for request tracing
const (
	IdentifierNone UniqueIdentifierType = ""     // No identifier
	IdentifierUUID UniqueIdentifierType = "uuid" // UUID v4
	IdentifierULID UniqueIdentifierType = "ulid" // ULID timestamp-based identifier
	IdentifierRGS  UniqueIdentifierType = "rgs"  // Randomly generated string
)

// TracingConfig holds request tracing configuration.
type TracingConfig struct {
	// Type specifies which identifier type to use for request tracing.
	Type UniqueIdentifierType

	// entropy is used for ULID generation (internal use)
	entropy *ulid.MonotonicEntropy

	// mu protects entropy during concurrent access
	mu sync.Mutex
}

// defaultTracingConfig returns the default tracing configuration.
func defaultTracingConfig() TracingConfig {
	return TracingConfig{
		Type:    IdentifierULID,
		entropy: ulid.Monotonic(rand.Reader, 0),
	}
}

// SetIdentifierType configures which identifier type to use for request tracing.
func (opt *Option) SetIdentifierType(t UniqueIdentifierType) *Option {
	opt.mu.Lock()
	opt.Tracing.Type = t
	opt.mu.Unlock()
	return opt
}

// IdentifierType returns the current identifier type setting.
func (opt *Option) IdentifierType() UniqueIdentifierType {
	opt.mu.RLock()
	t := opt.Tracing.Type
	opt.mu.RUnlock()
	return t
}

// GenerateIdentifier creates a unique identifier based on the configured Type.
// Returns a UUID, ULID, or random string, or empty string if no identifier type is configured.
func (opt *Option) GenerateIdentifier() string {
	opt.mu.RLock()
	tracingType := opt.Tracing.Type
	opt.mu.RUnlock()

	switch tracingType {
	case IdentifierUUID:
		return uuid.New().String()
	case IdentifierULID:
		opt.Tracing.mu.Lock()
		id := ulid.MustNew(ulid.Timestamp(time.Now()), opt.Tracing.entropy).String()
		opt.Tracing.mu.Unlock()
		return id
	case IdentifierRGS:
		return rand.Text()[:rgsLength]
	}
	return ""
}
