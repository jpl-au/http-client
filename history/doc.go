// Package history provides a bounded store of responses that a Client records.
//
// Response history is off by default. Stored responses hold bodies, request
// payloads and credentials, so they are recorded only on request. Attach a
// [History] to a Client to record every response it returns:
//
//	h := history.New()
//	c := client.New()
//	c.SetHistory(h)
//
//	c.Get(url1)
//	c.Get(url2)
//
//	for resp := range h.All() {
//	    if resp.Error != nil {
//	        log.Printf("request to %s failed: %v", resp.URL, resp.Error)
//	    }
//	}
//
// # Limits
//
// A History keeps at most [DefaultLimit] responses for at most [DefaultMaxAge].
// [History.SetLimit] and [History.SetMaxAge] change these. When a History is
// full, it removes expired responses first, then the oldest.
package history
