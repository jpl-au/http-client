package progress

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// terminalWidth retrieves the width of the terminal window.
// If the terminal size cannot be determined, it defaults to a width of 80 characters.
func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80 // Default fallback width
	}
	return width
}

// Terminal returns a progress callback that displays upload or download progress
// in the terminal: the percentage completed, the speed, and the estimated time
// remaining (ETA). Pass it to Option.OnUploadProgress or
// Option.OnDownloadProgress. Updates are shown at most every 100 milliseconds,
// but an update that reaches the known total is always shown. The progress bar
// shrinks to fit the terminal width.
func Terminal() func(int64, int64) {
	var lastUpdate time.Time // Time of the last update shown, or zero before the first.
	var lastBytes int64      // Bytes transferred at the last update shown.

	return func(bytesRead, totalBytes int64) {
		now := time.Now()
		// A total of -1 means the size is not known. Limit updates to at least
		// 100 milliseconds apart. The final update is always shown, because no
		// later update will replace it.
		known := totalBytes >= 0
		final := known && bytesRead >= totalBytes
		if !final && now.Sub(lastUpdate) < 100*time.Millisecond {
			return
		}

		// The speed is measured from the last update shown, so the first update
		// has none. When the count goes down, as when an upload starts again
		// after a redirect, the last update no longer applies.
		measured := !lastUpdate.IsZero() && bytesRead >= lastBytes && now.After(lastUpdate)
		var speed float64
		if measured {
			speed = float64(bytesRead-lastBytes) / now.Sub(lastUpdate).Seconds()
		}

		var line string
		if known {
			percentage := 100.0
			if bytesRead < totalBytes {
				percentage = float64(bytesRead) / float64(totalBytes) * 100
			}

			status := "100.00% | Complete"
			if !final {
				status = fmt.Sprintf("%.2f%%", percentage)
				if measured {
					status += " | Speed: " + formatSpeed(speed)
					if speed > 0 {
						status += " | ETA: " + formatETA(float64(totalBytes-bytesRead)/speed)
					}
				}
			}

			// The bar takes the width the text leaves, so the line does not
			// wrap: "\r" can only redraw the last line of a wrapped line.
			barWidth := min(max(terminalWidth()-1-len("[] ")-len(status), minBarWidth), maxBarWidth)
			filled := int(float64(barWidth) * percentage / 100)
			line = "[" + strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled) + "] " + status
		} else {
			line = fmt.Sprintf("Transferred %d bytes", bytesRead)
			if measured {
				line += " | Speed: " + formatSpeed(speed)
			}
		}

		// Spaces clear what is left of a longer earlier line. The line stays one
		// character short of the width, so the cursor does not move to the next
		// line.
		fmt.Print("\r" + line + strings.Repeat(" ", max(terminalWidth()-1-len(line), 0)))

		lastUpdate = now
		lastBytes = bytesRead
	}
}

// The progress bar is at least minBarWidth and at most maxBarWidth characters
// wide.
const (
	minBarWidth = 10
	maxBarWidth = 50
)

// formatSpeed returns bytesPerSecond in B/s, KB/s, MB/s or GB/s.
func formatSpeed(bytesPerSecond float64) string {
	switch {
	case bytesPerSecond >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB/s", bytesPerSecond/(1024*1024*1024))
	case bytesPerSecond >= 1024*1024:
		return fmt.Sprintf("%.2f MB/s", bytesPerSecond/(1024*1024))
	case bytesPerSecond >= 1024:
		return fmt.Sprintf("%.2f KB/s", bytesPerSecond/1024)
	default:
		return fmt.Sprintf("%.2f B/s", bytesPerSecond)
	}
}

// formatETA returns a time remaining of seconds in seconds, minutes or hours.
// It takes seconds as a float64, because a slow transfer of a large file can
// need more time than a time.Duration holds.
func formatETA(seconds float64) string {
	switch {
	case seconds < 1:
		return "<1s"
	case seconds < 59.5:
		return fmt.Sprintf("%.0fs", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%.1fm", seconds/60)
	default:
		return fmt.Sprintf("%.1fh", seconds/3600)
	}
}
