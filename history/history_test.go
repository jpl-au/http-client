package history_test

import (
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jpl-au/http-client/history"
	"github.com/jpl-au/http-client/response"
)

// add records n responses with the identifiers "1" to "n".
func add(h *history.History, n int) {
	for i := 1; i <= n; i++ {
		h.Add(response.Response{UniqueIdentifier: strconv.Itoa(i)})
	}
}

// identifiers returns the identifiers of the responses in h, in the order All yields them.
func identifiers(h *history.History) []string {
	var ids []string
	for resp := range h.All() {
		ids = append(ids, resp.UniqueIdentifier)
	}
	return ids
}

func TestNewUsesDefaultLimit(t *testing.T) {
	h := history.New()
	add(h, history.DefaultLimit+1)

	if got, want := h.Len(), history.DefaultLimit; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
	if _, ok := h.Lookup("1"); ok {
		t.Error(`Lookup("1") found the oldest response, want it removed`)
	}
}

func TestAllYieldsOldestFirst(t *testing.T) {
	h := history.New()
	add(h, 3)

	if got, want := identifiers(h), []string{"1", "2", "3"}; !slices.Equal(got, want) {
		t.Errorf("All() identifiers = %q, want %q", got, want)
	}
}

func TestSetLimitRemovesOldest(t *testing.T) {
	h := history.New()
	add(h, 3)

	h.SetLimit(2)
	if got, want := identifiers(h), []string{"2", "3"}; !slices.Equal(got, want) {
		t.Errorf("All() after SetLimit(2) = %q, want %q", got, want)
	}

	h.Add(response.Response{UniqueIdentifier: "4"})
	if got, want := identifiers(h), []string{"3", "4"}; !slices.Equal(got, want) {
		t.Errorf("All() after Add = %q, want %q", got, want)
	}
}

func TestSetLimitBelowOneKeepsOne(t *testing.T) {
	for _, limit := range []int{0, -1} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			h := history.New().SetLimit(limit)
			add(h, 2)

			if got, want := identifiers(h), []string{"2"}; !slices.Equal(got, want) {
				t.Errorf("All() = %q, want %q", got, want)
			}
		})
	}
}

func TestMaxAgeExpiresResponses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := history.New().SetMaxAge(time.Minute)
		h.Add(response.Response{UniqueIdentifier: "old"})
		time.Sleep(30 * time.Second)
		h.Add(response.Response{UniqueIdentifier: "new"})
		time.Sleep(31 * time.Second)

		if got, want := h.Len(), 1; got != want {
			t.Errorf("Len() = %d, want %d", got, want)
		}
		if _, ok := h.Lookup("old"); ok {
			t.Error(`Lookup("old") found an expired response`)
		}
		if _, ok := h.Lookup("new"); !ok {
			t.Error(`Lookup("new") found nothing, want the response`)
		}
		if got, want := identifiers(h), []string{"new"}; !slices.Equal(got, want) {
			t.Errorf("All() = %q, want %q", got, want)
		}
	})
}

func TestNewUsesDefaultMaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := history.New()
		h.Add(response.Response{UniqueIdentifier: "1"})
		time.Sleep(history.DefaultMaxAge + time.Second)

		if got := h.Len(); got != 0 {
			t.Errorf("Len() = %d, want 0", got)
		}
	})
}

func TestMaxAgeZeroNeverExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := history.New().SetMaxAge(0)
		h.Add(response.Response{UniqueIdentifier: "1"})
		time.Sleep(24 * time.Hour)

		if got := h.Len(); got != 1 {
			t.Errorf("Len() = %d, want 1", got)
		}
	})
}

func TestLookupReturnsCopy(t *testing.T) {
	h := history.New()
	h.Add(response.Response{UniqueIdentifier: "1", StatusCode: 200})

	resp, ok := h.Lookup("1")
	if !ok {
		t.Fatal(`Lookup("1") found nothing, want the response`)
	}
	resp.StatusCode = 500

	stored, _ := h.Lookup("1")
	if stored.StatusCode != 200 {
		t.Errorf("stored StatusCode = %d, want 200 (a change to the copy must not reach the history)", stored.StatusCode)
	}
}

func TestClearRemovesAll(t *testing.T) {
	h := history.New()
	add(h, 3)

	h.Clear()
	if got := h.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}

func TestAllAllowsAddDuringIteration(t *testing.T) {
	h := history.New()
	add(h, 2)

	for resp := range h.All() {
		h.Add(response.Response{UniqueIdentifier: resp.UniqueIdentifier + "-again"})
	}
	if got, want := h.Len(), 4; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
}
