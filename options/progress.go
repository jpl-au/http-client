package options

// ProgressTracking defines when progress should be tracked relative to compression.
type ProgressTracking int

const (
	// TrackBeforeCompression tracks progress on original data before compression.
	// This shows actual data size but may not reflect transfer size.
	TrackBeforeCompression ProgressTracking = iota

	// TrackAfterCompression tracks progress on compressed data during transfer.
	// This shows actual transfer size but not original data size.
	TrackAfterCompression
)

// ProgressConfig holds progress tracking settings.
type ProgressConfig struct {
	// Tracking determines when progress is measured relative to compression.
	Tracking ProgressTracking

	// OnUpload is called during upload with bytes read and total bytes.
	// Total bytes may be -1 if unknown (e.g., streaming or compressed).
	OnUpload func(bytesRead, totalBytes int64)

	// OnDownload is called during download with bytes read and total bytes.
	// Total bytes may be -1 if unknown (e.g., chunked or compressed).
	OnDownload func(bytesRead, totalBytes int64)

	// UploadBufferSize controls the buffer size when uploading.
	// If nil, default buffer size is used.
	UploadBufferSize *int

	// DownloadBufferSize controls the buffer size when downloading.
	// If nil, default buffer size is used.
	DownloadBufferSize *int
}

// defaultProgressConfig returns the default progress configuration.
func defaultProgressConfig() ProgressConfig {
	return ProgressConfig{
		Tracking: TrackBeforeCompression,
	}
}

// TrackBeforeCompression sets progress tracking to occur before data compression.
// This tracks the original data size but may not reflect final transfer size.
func (opt *Option) TrackBeforeCompression() *Option {
	opt.mu.Lock()
	opt.Progress.Tracking = TrackBeforeCompression
	opt.mu.Unlock()
	return opt
}

// TrackAfterCompression sets progress tracking to occur after data compression.
// This tracks actual transfer size but not original data size.
func (opt *Option) TrackAfterCompression() *Option {
	opt.mu.Lock()
	opt.Progress.Tracking = TrackAfterCompression
	opt.mu.Unlock()
	return opt
}

// ProgressTracking returns the current progress tracking setting.
func (opt *Option) ProgressTracking() ProgressTracking {
	opt.mu.RLock()
	tracking := opt.Progress.Tracking
	opt.mu.RUnlock()
	return tracking
}

// SetDownloadBufferSize configures the buffer size used when downloading files.
// The size must be positive; otherwise, the setting will be ignored.
func (opt *Option) SetDownloadBufferSize(size int) *Option {
	if size > 0 {
		opt.mu.Lock()
		opt.Progress.DownloadBufferSize = &size
		opt.mu.Unlock()
	}
	return opt
}

// SetUploadBufferSize configures the buffer size used when uploading files.
// The size must be positive; otherwise, the setting will be ignored.
func (opt *Option) SetUploadBufferSize(size int) *Option {
	if size > 0 {
		opt.mu.Lock()
		opt.Progress.UploadBufferSize = &size
		opt.mu.Unlock()
	}
	return opt
}

// OnUploadProgress sets a callback function to track upload progress.
func (opt *Option) OnUploadProgress(fn func(bytesRead, totalBytes int64)) *Option {
	opt.mu.Lock()
	opt.Progress.OnUpload = fn
	opt.mu.Unlock()
	return opt
}

// OnDownloadProgress sets a callback function to track download progress.
func (opt *Option) OnDownloadProgress(fn func(bytesRead, totalBytes int64)) *Option {
	opt.mu.Lock()
	opt.Progress.OnDownload = fn
	opt.mu.Unlock()
	return opt
}
