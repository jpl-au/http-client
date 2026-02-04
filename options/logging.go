package options

import (
	"log/slog"
	"os"
)

// LoggingConfig holds logging-related settings.
type LoggingConfig struct {
	// Enabled controls whether verbose logging is active.
	Enabled bool

	// Logger is the slog.Logger instance used for logging.
	Logger slog.Logger
}

// defaultLoggingConfig returns the default logging configuration.
func defaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Enabled: false,
		Logger:  *slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
}

// Log logs a message if verbose logging is enabled.
// The message will be logged at INFO level with any additional arguments provided.
func (opt *Option) Log(msg string, args ...any) {
	opt.mu.RLock()
	enabled := opt.Logging.Enabled
	logger := opt.Logging.Logger
	opt.mu.RUnlock()

	if enabled {
		logger.Info(msg, args...)
	}
}

// EnableLogging turns on verbose logging for the Option instance.
func (opt *Option) EnableLogging() *Option {
	opt.mu.Lock()
	opt.Logging.Enabled = true
	opt.mu.Unlock()
	return opt
}

// DisableLogging turns off verbose logging for the Option instance.
func (opt *Option) DisableLogging() *Option {
	opt.mu.Lock()
	opt.Logging.Enabled = false
	opt.mu.Unlock()
	return opt
}

// UseTextLogger configures the Option to use a text-based logger and enables verbose logging.
// The logger will output to stdout using the default slog TextHandler format.
func (opt *Option) UseTextLogger() *Option {
	opt.mu.Lock()
	opt.Logging.Enabled = true
	opt.Logging.Logger = *slog.New(slog.NewTextHandler(os.Stdout, nil))
	opt.mu.Unlock()
	return opt
}

// UseJsonLogger configures the Option to use a JSON-based logger and enables verbose logging.
// The logger will output to stdout using the default slog JSONHandler format.
func (opt *Option) UseJsonLogger() *Option {
	opt.mu.Lock()
	opt.Logging.Enabled = true
	opt.Logging.Logger = *slog.New(slog.NewJSONHandler(os.Stdout, nil))
	opt.mu.Unlock()
	return opt
}

// SetLogger configures a custom logger and enables verbose logging.
// The provided logger will replace any existing logger configuration.
func (opt *Option) SetLogger(logger *slog.Logger) *Option {
	opt.mu.Lock()
	opt.Logging.Enabled = true
	opt.Logging.Logger = *logger
	opt.mu.Unlock()
	return opt
}
