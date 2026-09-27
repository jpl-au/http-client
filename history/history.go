package history

import (
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/jpl-au/http-client/response"
)

// Defaults for a History returned by New.
const (
	DefaultLimit  = 100             // Maximum number of responses to keep.
	DefaultMaxAge = 5 * time.Minute // How long to keep a response.
)

// History is a bounded store of responses, oldest first. It is safe for
// concurrent use, so one History can record the responses of several Clients.
type History struct {
	mu      sync.Mutex
	limit   int
	maxAge  time.Duration
	entries []entry // Oldest first.
}

// entry is a stored response and the time it was added, which expiry uses.
type entry struct {
	response response.Response
	added    time.Time
}

// New returns an empty History that keeps DefaultLimit responses for DefaultMaxAge.
func New() *History {
	return &History{limit: DefaultLimit, maxAge: DefaultMaxAge}
}

// SetLimit sets the maximum number of responses to keep. A limit below 1
// becomes 1. A lower limit removes the excess at once, oldest first.
func (h *History) SetLimit(limit int) *History {
	h.mu.Lock()
	h.limit = max(limit, 1)
	h.evict(h.limit)
	h.mu.Unlock()
	return h
}

// SetMaxAge sets how long to keep a response. A maximum age of zero or less
// keeps responses until the limit removes them.
func (h *History) SetMaxAge(maxAge time.Duration) *History {
	h.mu.Lock()
	h.maxAge = maxAge
	h.evict(h.limit)
	h.mu.Unlock()
	return h
}

// Add records a response. When the History is full, it removes expired
// responses first, then the oldest.
func (h *History) Add(resp response.Response) {
	h.mu.Lock()
	h.evict(h.limit - 1)
	h.entries = append(h.entries, entry{response: resp, added: time.Now()})
	h.mu.Unlock()
}

// All returns an iterator over the responses that have not expired, oldest
// first. It iterates over a snapshot, so the loop body can call Add.
func (h *History) All() iter.Seq[response.Response] {
	h.mu.Lock()
	h.evict(h.limit)
	snapshot := slices.Clone(h.entries)
	h.mu.Unlock()

	return func(yield func(response.Response) bool) {
		for _, e := range snapshot {
			if !yield(e.response) {
				return
			}
		}
	}
}

// Lookup returns a copy of the response with the identifier id, and reports
// whether it was found. The copy shares its Header, Body and Options with the
// stored response.
func (h *History) Lookup(id string) (response.Response, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.evict(h.limit)
	for _, e := range slices.Backward(h.entries) {
		if e.response.UniqueIdentifier == id {
			return e.response, true
		}
	}
	return response.Response{}, false
}

// Len returns the number of responses that have not expired.
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.evict(h.limit)
	return len(h.entries)
}

// Clear removes all responses.
func (h *History) Clear() {
	h.mu.Lock()
	h.entries = nil
	h.mu.Unlock()
}

// evict removes expired entries, then the oldest entries until no more than
// limit remain. h.mu must be held.
func (h *History) evict(limit int) {
	if h.maxAge > 0 {
		cutoff := time.Now().Add(-h.maxAge)
		h.entries = slices.DeleteFunc(h.entries, func(e entry) bool {
			return e.added.Before(cutoff)
		})
	}
	if excess := len(h.entries) - limit; excess > 0 {
		h.entries = slices.Delete(h.entries, 0, excess)
	}
}
