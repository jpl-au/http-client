// Package progress provides utilities for displaying progress bars in the terminal.
//
// This package includes a sample [ProgressBar] implementation that can be used
// with the http-client's progress callbacks to display upload/download progress.
//
// # Basic Usage
//
//	bar := progress.NewProgressBar("Downloading", totalBytes)
//	opt := options.New()
//	opt.Progress.OnDownload = func(bytesRead, totalBytes int64) {
//	    bar.Update(bytesRead)
//	}
//	resp, err := client.Get(url, opt)
//	bar.Finish()
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
