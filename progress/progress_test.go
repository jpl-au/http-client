package progress_test

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jpl-au/http-client/progress"
)

// output calls a new Terminal callback with each pair of byte counts in turn
// and returns what it printed.
func output(t *testing.T, updates ...[2]int64) string {
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
	for _, u := range updates {
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

func TestTerminal(t *testing.T) {
	// The final update is shown even when it comes straight after another,
	// so the display does not stop short of 100%.
	t.Run("final update shown", func(t *testing.T) {
		got := output(t, [2]int64{1, 2}, [2]int64{2, 2})
		if !strings.Contains(got, "100.00%") {
			t.Errorf("output = %q, want it to show 100.00%%", got)
		}
	})

	t.Run("intermediate update throttled", func(t *testing.T) {
		got := output(t, [2]int64{1, 4}, [2]int64{2, 4})
		if strings.Contains(got, "50.00%") {
			t.Errorf("output = %q, want the second update throttled", got)
		}
	})

	// The first update has no earlier sample to measure the speed against.
	t.Run("first update has no ETA", func(t *testing.T) {
		got := output(t, [2]int64{1, 2})
		if regexp.MustCompile(`ETA: \S`).MatchString(got) {
			t.Errorf("output = %q, want no ETA", got)
		}
	})

	// The same callback reports uploads and downloads.
	t.Run("wording fits both directions", func(t *testing.T) {
		for _, total := range []int64{2, 0} {
			got := output(t, [2]int64{2, total})
			if strings.Contains(strings.ToLower(got), "upload") {
				t.Errorf("total %d: output = %q, want no mention of upload", total, got)
			}
		}
	})
}
