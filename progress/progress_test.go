package progress_test

import (
	"io"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jpl-au/http-client/progress"
)

// output calls a new Terminal callback with each pair of byte counts in turn,
// waiting for step before each one after the first, and returns what it
// printed.
func output(t *testing.T, step time.Duration, updates ...[2]int64) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	report := progress.Terminal()
	for i, u := range updates {
		if i > 0 {
			time.Sleep(step)
		}
		report(u[0], u[1])
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// lastLine returns the last line the callback drew, without the spaces that
// pad it.
func lastLine(out string) string {
	drawn := strings.Split(out, "\r")
	return strings.TrimRight(drawn[len(drawn)-1], " ")
}

// The tests that measure speed run on a fake clock, so the speed and time
// remaining are exact. Output goes to a pipe, so the terminal width is the
// default of 80 characters.
func TestTerminal(t *testing.T) {
	// The final update is shown even when it comes straight after another,
	// so the display does not stop short of 100%.
	t.Run("final update shown", func(t *testing.T) {
		got := output(t, 0, [2]int64{1, 2}, [2]int64{2, 2})
		if !strings.Contains(got, "100.00%") {
			t.Errorf("output = %q, want it to show 100.00%%", got)
		}
	})

	t.Run("intermediate update throttled", func(t *testing.T) {
		got := output(t, 0, [2]int64{1, 4}, [2]int64{2, 4})
		if strings.Contains(got, "50.00%") {
			t.Errorf("output = %q, want the second update throttled", got)
		}
	})

	// The first update has no earlier update to measure the speed against, so
	// it shows no speed and no ETA.
	t.Run("first update has no speed", func(t *testing.T) {
		for _, total := range []int64{2, -1} {
			got := output(t, 0, [2]int64{1, total})
			if strings.Contains(got, "Speed") || strings.Contains(got, "ETA") {
				t.Errorf("total %d: output = %q, want no speed or ETA", total, got)
			}
		}
	})

	t.Run("speed and ETA", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := lastLine(output(t, time.Second, [2]int64{0, 1000}, [2]int64{500, 1000}))
			if want := "50.00% | Speed: 500.00 B/s | ETA: 1s"; !strings.HasSuffix(got, want) {
				t.Errorf("line = %q, want it to end with %q", got, want)
			}
		})
	})

	t.Run("ETA under a second", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := lastLine(output(t, time.Second, [2]int64{0, 1000}, [2]int64{900, 1000}))
			if want := "ETA: <1s"; !strings.HasSuffix(got, want) {
				t.Errorf("line = %q, want it to end with %q", got, want)
			}
		})
	})

	// A count that goes down, as when an upload starts again after a
	// redirect, starts the speed measurement again.
	t.Run("count goes down", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := lastLine(output(t, time.Second, [2]int64{500, 1000}, [2]int64{100, 1000}))
			if !strings.HasSuffix(got, "] 10.00%") {
				t.Errorf("line = %q, want 10.00%% and no speed", got)
			}
		})
	})

	t.Run("unknown total", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := lastLine(output(t, time.Second, [2]int64{0, -1}, [2]int64{2048, -1}))
			if want := "Transferred 2048 bytes | Speed: 2.00 KB/s"; got != want {
				t.Errorf("line = %q, want %q", got, want)
			}
		})
	})

	// A wrapped line cannot be redrawn, so every line is shorter than the
	// terminal width, even with the longest speed and ETA.
	t.Run("line fits terminal", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			for _, total := range []int64{1 << 40, -1} {
				got := output(t, time.Second, [2]int64{0, total}, [2]int64{1 << 30, total})
				for _, line := range strings.Split(got, "\r")[1:] {
					if len(line) >= 80 {
						t.Errorf("total %d: line %q has %d characters, want fewer than 80", total, line, len(line))
					}
				}
			}
		})
	})

	// An empty body has a known total of 0, so it is complete at once.
	t.Run("empty body", func(t *testing.T) {
		got := lastLine(output(t, 0, [2]int64{0, 0}))
		if !strings.HasSuffix(got, "100.00% | Complete") {
			t.Errorf("line = %q, want 100.00%% | Complete", got)
		}
	})

	// The same callback reports uploads and downloads.
	t.Run("wording fits both directions", func(t *testing.T) {
		for _, total := range []int64{2, -1} {
			got := output(t, 0, [2]int64{2, total})
			if strings.Contains(strings.ToLower(got), "upload") {
				t.Errorf("total %d: output = %q, want no mention of upload", total, got)
			}
		}
	})
}
