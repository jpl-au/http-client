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
// but an update that reaches the known total is always shown.
func Terminal() func(int64, int64) {
	var lastUpdate time.Time // Tracks the last time the progress was updated
	var lastBytes int64      // Tracks the number of bytes processed during the last update

	return func(bytesRead, totalBytes int64) {
		now := time.Now()
		// Limit updates to at least 100 milliseconds apart. The final update
		// is always shown, because no later update will replace it.
		final := totalBytes > 0 && bytesRead >= totalBytes
		if !final && now.Sub(lastUpdate) < 100*time.Millisecond {
			return
		}

		// The first update has no earlier sample, so its speed is unknown and
		// it shows no ETA.
		var speed float64
		if !lastUpdate.IsZero() {
			bytesSinceLast := bytesRead - lastBytes // Bytes processed since the last update
			timeSinceLast := now.Sub(lastUpdate)    // Time elapsed since the last update
			if timeSinceLast > 0 {
				speed = float64(bytesSinceLast) / timeSinceLast.Seconds()
			}
		}

		width := terminalWidth()    // Dynamically get terminal width
		const progressBarWidth = 50 // Fixed width for the progress bar

		if totalBytes > 0 {
			// Calculate percentage completed
			percentage := float64(bytesRead) / float64(totalBytes) * 100
			if bytesRead >= totalBytes {
				percentage = 100 // Ensure exactly 100% at completion
			}

			// Estimate time remaining
			var eta time.Duration
			if speed > 0 && bytesRead < totalBytes {
				remainingBytes := totalBytes - bytesRead
				eta = time.Duration(float64(remainingBytes)/speed) * time.Second
			}

			// Format speed string
			var speedStr string
			switch {
			case speed >= 1024*1024*1024:
				speedStr = fmt.Sprintf("%.2f GB/s", speed/(1024*1024*1024))
			case speed >= 1024*1024:
				speedStr = fmt.Sprintf("%.2f MB/s", speed/(1024*1024))
			case speed >= 1024:
				speedStr = fmt.Sprintf("%.2f KB/s", speed/1024)
			default:
				speedStr = fmt.Sprintf("%.2f B/s", speed)
			}

			// Format ETA string
			var etaStr string
			if eta > 0 {
				if eta >= time.Hour {
					etaStr = fmt.Sprintf("%.1fh", eta.Hours())
				} else if eta >= time.Minute {
					etaStr = fmt.Sprintf("%.1fm", eta.Minutes())
				} else {
					etaStr = fmt.Sprintf("%.0fs", eta.Seconds())
				}
			}

			// Generate progress bar
			progressLength := int(float64(progressBarWidth) * (percentage / 100))
			bar := strings.Repeat("=", progressLength) + strings.Repeat(" ", progressBarWidth-progressLength)

			// Format the progress message
			var message string
			if percentage < 100 {
				message = fmt.Sprintf("\r[%s] %.2f%% | Speed: %s | ETA: %s", bar, percentage, speedStr, etaStr)
			} else {
				message = fmt.Sprintf("\r[%s] 100.00%% | Complete", strings.Repeat("=", progressBarWidth))
			}

			// Pad the message to fill the terminal width
			paddedMessage := message
			padLength := width - len(message)
			if padLength > 0 {
				paddedMessage += strings.Repeat(" ", padLength)
			}

			// Print the padded message
			fmt.Print(paddedMessage)

		} else {
			// Handle cases where the total size is unknown
			message := fmt.Sprintf("\rTransferred %d bytes | Speed: %.2f MB/s", bytesRead, speed/(1024*1024))
			padLength := width - len(message)
			if padLength > 0 {
				message += strings.Repeat(" ", padLength)
			}
			fmt.Print(message)
		}

		lastUpdate = now      // Update the last update time
		lastBytes = bytesRead // Update the last bytes processed
	}
}
