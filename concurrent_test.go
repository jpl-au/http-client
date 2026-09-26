package client_test

import (
	"net/http"
	"sync"
	"testing"

	client "github.com/jpl-au/http-client"
)

// TestClientSharedAcrossGoroutines verifies that one Client can serve concurrent requests.
// Run with -race: the Client must not mutate its shared http.Client per request.
func TestClientSharedAcrossGoroutines(t *testing.T) {
	server := setupTestServer(t)
	defer server.Close()

	c := client.New()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 4 {
				resp, err := c.Get(server.URL + "/echo")
				if err != nil {
					t.Error(err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
				}
			}
		})
	}
	wg.Wait()
}
