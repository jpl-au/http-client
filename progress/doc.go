// Package progress provides utilities for displaying progress bars in the terminal.
//
// [Terminal] returns a progress callback that displays upload or download progress
// in the terminal.
//
// # Basic Usage
//
//	opt := options.New().OnDownloadProgress(progress.Terminal())
//	resp, err := client.Get(url, opt)
//
// # Terminal Width
//
// The progress bar automatically adapts to the terminal width. If the terminal
// size cannot be determined, it defaults to 80 characters.
//
// # Custom Progress Tracking
//
// For custom progress handling, use the callbacks directly without this package:
//
//	opt := options.New()
//	opt.Progress.OnUpload = func(bytesRead, totalBytes int64) {
//	    pct := float64(bytesRead) / float64(totalBytes) * 100
//	    fmt.Printf("\rProgress: %.1f%%", pct)
//	}
package progress
