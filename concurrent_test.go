package client_test

import (
	"net/http"
	"sync"
	"testing"

	client "github.com/jpl-au/http-client"
)

// TestClient_Concurrent checks that one Client serves requests from several goroutines.
func TestClient_Concurrent(t *testing.T) {
	// One Client serves concurrent requests. Run with -race: the Client must not mutate its shared http.Client per request.
	t.Run("shared across goroutines", func(t *testing.T) {
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
	})
}
