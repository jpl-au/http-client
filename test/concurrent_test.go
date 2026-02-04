package client_test

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/jpl-au/http-client"
	"github.com/jpl-au/http-client/options"
	"github.com/stretchr/testify/assert"
)

func TestSharedConcurrentRequests(t *testing.T) {
	server := setupTestServer(t)

	// Force cleanup after each test case
	t.Cleanup(func() {
		server.Close()
		// Instead of sleeping, we can wait for connections to drain
		done := make(chan struct{})
		go func() {
			// Give connections a short time to close gracefully
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			// If it takes too long, just continue
		}
	})

	resultSet := TestResultSet{
		Timestamp:   time.Now(),
		TestName:    "Shared Client Concurrent Tests",
		Environment: runtime.Version(),
		Results:     []TestResult{},
	}

	tests := []struct {
		name          string
		numGoroutines int
		requestsPerGo int
		scenario      string // "mixed", "upload", "download"
	}{
		{"Light Concurrent Mixed Load (Shared)", 10, 5, "mixed"},
		{"Heavy Concurrent Mixed Load (Shared)", 50, 10, "mixed"},
		{"Concurrent File Uploads (Shared)", 20, 5, "upload"},
		{"Concurrent File Downloads (Shared)", 20, 5, "download"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			errors := make(chan error, tt.numGoroutines*tt.requestsPerGo)
			start := time.Now()

			for i := 0; i < tt.numGoroutines; i++ {
				routineNum := i
				wg.Go(func() {
					for j := 0; j < tt.requestsPerGo; j++ {
						var err error
						switch tt.scenario {
						case "mixed":
							err = performMixedRequests(server.URL, routineNum, j, nil)
						case "upload":
							err = performUploadRequest(server.URL, routineNum, j, nil)
						case "download":
							err = performDownloadRequest(server.URL, routineNum, j, nil)
						}
						if err != nil {
							errors <- fmt.Errorf("routine %d request %d: %w", routineNum, j, err)
						}
					}
				})
			}

			wg.Wait()
			close(errors)

			// Collect errors
			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}

			duration := time.Since(start)
			totalRequests := tt.numGoroutines * tt.requestsPerGo
			successRate := float64(totalRequests-len(errs)) / float64(totalRequests) * 100
			requestsPerSec := float64(totalRequests) / duration.Seconds()

			// Create TestResult
			result := TestResult{
				ScenarioName:   tt.name,
				NumGoroutines:  tt.numGoroutines,
				RequestsPerGo:  tt.requestsPerGo,
				TotalRequests:  totalRequests,
				Duration:       duration,
				RequestsPerSec: requestsPerSec,
				SuccessRate:    successRate,
				ErrorCount:     len(errs),
			}
			resultSet.Results = append(resultSet.Results, result)

			// Log the results
			t.Logf("\nResults for %s:", tt.name)
			t.Logf("Total requests: %d", result.TotalRequests)
			t.Logf("Duration: %v", result.Duration)
			t.Logf("Requests/second: %.2f", result.RequestsPerSec)
			t.Logf("Success rate: %.2f%%", result.SuccessRate)
			t.Logf("Error count: %d", result.ErrorCount)

			// Assert high success rate
			assert.GreaterOrEqual(t, result.SuccessRate, 95.0, "Success rate should be at least 95%")
		})
	}

	// Add results to global slice
	globalTestResults = append(globalTestResults, resultSet)

	// Write results to file
	if err := writeResultsToFile(resultSet); err != nil {
		t.Errorf("Failed to write results: %v", err)
	}
}

func TestNonSharedConcurrentRequests(t *testing.T) {
	server := setupTestServer(t)

	// Force cleanup after each test case
	t.Cleanup(func() {
		server.Close()
		// Instead of sleeping, we can wait for connections to drain
		done := make(chan struct{})
		go func() {
			// Give connections a short time to close gracefully
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			// If it takes too long, just continue
		}
	})

	resultSet := TestResultSet{
		Timestamp:   time.Now(),
		TestName:    "Non-Shared Client Concurrent Tests",
		Environment: runtime.Version(),
		Results:     []TestResult{},
	}

	tests := []struct {
		name          string
		numGoroutines int
		requestsPerGo int
		scenario      string
	}{
		{"Light Concurrent Mixed Load (Non Shared)", 10, 5, "mixed"},
		{"Heavy Concurrent Mixed Load (Non Shared)", 50, 10, "mixed"},
		{"Concurrent File Uploads (Non Shared)", 20, 5, "upload"},
		{"Concurrent File Downloads (Non Shared)", 20, 5, "download"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup
			errors := make(chan error, tt.numGoroutines*tt.requestsPerGo)
			start := time.Now()

			for i := 0; i < tt.numGoroutines; i++ {
				routineNum := i
				wg.Go(func() {
					for j := 0; j < tt.requestsPerGo; j++ {

						// Create options with per-request client for each request
						opt := options.New()
						opt.UsePerRequestClient()

						var err error
						switch tt.scenario {
						case "mixed":
							err = performMixedRequests(server.URL, routineNum, j, opt)
						case "upload":
							err = performUploadRequest(server.URL, routineNum, j, opt)
						case "download":
							err = performDownloadRequest(server.URL, routineNum, j, opt)
						}
						if err != nil {
							errors <- fmt.Errorf("routine %d request %d: %w", routineNum, j, err)
						}
					}
				})
			}

			wg.Wait()
			close(errors)

			// Collect errors
			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}

			duration := time.Since(start)
			totalRequests := tt.numGoroutines * tt.requestsPerGo
			successRate := float64(totalRequests-len(errs)) / float64(totalRequests) * 100
			requestsPerSec := float64(totalRequests) / duration.Seconds()

			// Create TestResult
			result := TestResult{
				ScenarioName:   tt.name,
				NumGoroutines:  tt.numGoroutines,
				RequestsPerGo:  tt.requestsPerGo,
				TotalRequests:  totalRequests,
				Duration:       duration,
				RequestsPerSec: requestsPerSec,
				SuccessRate:    successRate,
				ErrorCount:     len(errs),
			}
			resultSet.Results = append(resultSet.Results, result)

			// Log the results
			t.Logf("\nResults for %s:", tt.name)
			t.Logf("Total requests: %d", result.TotalRequests)
			t.Logf("Duration: %v", result.Duration)
			t.Logf("Requests/second: %.2f", result.RequestsPerSec)
			t.Logf("Success rate: %.2f%%", result.SuccessRate)
			t.Logf("Error count: %d", result.ErrorCount)

			assert.GreaterOrEqual(t, result.SuccessRate, 95.0, "Success rate should be at least 95%")
		})
	}

	// Add results to global slice
	globalTestResults = append(globalTestResults, resultSet)

	// Write results to file
	if err := writeResultsToFile(resultSet); err != nil {
		t.Errorf("Failed to write results: %v", err)
	}
}

func performMixedRequests(baseURL string, routineNum, reqNum int, opts *options.Option) error {
	// Create new options for this request
	opt := options.New(opts)

	switch reqNum % 3 {
	case 0:
		resp, err := client.Get(baseURL+"/echo-headers", opt)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	case 1:
		opt.AddHeader(fmt.Sprintf("X-Test-%d-%d", routineNum, reqNum), "test")
		resp, err := client.Post(baseURL+"/upload", []byte("test data"), opt)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	case 2:
		opt.SetBufferOutput()
		resp, err := client.Get(baseURL+"/download", opt)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}
	return nil
}

func performUploadRequest(baseURL string, routineNum, reqNum int, opts *options.Option) error {
	file, err := os.Open(smallf) // Using the small file for quicker tests
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	opt := options.New(opts)
	opt.AddHeader(fmt.Sprintf("X-Upload-Test-%d-%d", routineNum, reqNum), "test")

	resp, err := client.Post(baseURL+"/upload", file, opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

func performDownloadRequest(baseURL string, routineNum, reqNum int, opts *options.Option) error {
	opt := options.New(opts)
	tempDir := os.TempDir()
	downloadPath := filepath.Join(tempDir, fmt.Sprintf("download-%d-%d.txt", routineNum, reqNum))
	opt.SetFileOutput(downloadPath)
	defer os.Remove(downloadPath) // Clean up after test

	resp, err := client.Get(baseURL+"/download", opt)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// TestResultsAnalysis compares performance between shared and non-shared HTTP clients.
//
// This test is sensitive to system conditions (CPU scheduling, system load,
// GC pauses, network stack behaviour). The 50% threshold is intentionally lenient
// to catch severe regressions while tolerating normal system variance.
//
// For precise benchmarking, use Go's benchmark framework in a controlled environment.
func TestResultsAnalysis(t *testing.T) {
	t.Log("\nAnalyzing all test results:")

	if len(globalTestResults) < 2 {
		t.Log("Not enough test sets to perform analysis")
		return
	}

	// Group results by base scenario name
	scenarioComparisons := make(map[string]struct {
		shared    TestResult
		nonShared TestResult
	})

	// Process shared client results
	for _, result := range globalTestResults[0].Results {
		baseScenario := strings.TrimSuffix(result.ScenarioName, " (Shared)")
		scenarioComparisons[baseScenario] = struct {
			shared    TestResult
			nonShared TestResult
		}{shared: result}
	}

	// Process non-shared client results
	for _, result := range globalTestResults[1].Results {
		baseScenario := strings.TrimSuffix(result.ScenarioName, " (Non Shared)")
		if comp, exists := scenarioComparisons[baseScenario]; exists {
			comp.nonShared = result
			scenarioComparisons[baseScenario] = comp
		}
	}

	var analysisResults []TestResult

	// Analyze and collect results
	for scenario, comp := range scenarioComparisons {
		// Calculate throughput difference (negative if non-shared is worse)
		throughputDiff := ((comp.nonShared.RequestsPerSec - comp.shared.RequestsPerSec) / comp.shared.RequestsPerSec) * 100
		throughputDiff = math.Round(throughputDiff*100) / 100 // Round to 2 decimal places

		// Calculate duration improvement
		durationImprov := ((comp.shared.Duration.Seconds() - comp.nonShared.Duration.Seconds()) / comp.shared.Duration.Seconds()) * 100
		durationImprov = math.Round(durationImprov*100) / 100 // Round to 2 decimal places

		// Create analysis result
		analysisResult := TestResult{
			ScenarioName:   fmt.Sprintf("%s (Analysis)", scenario),
			NumGoroutines:  comp.shared.NumGoroutines,
			RequestsPerGo:  comp.shared.RequestsPerGo,
			TotalRequests:  comp.shared.TotalRequests,
			Duration:       comp.shared.Duration,
			RequestsPerSec: throughputDiff, // Store raw throughput diff
			SuccessRate:    durationImprov, // Store raw duration improvement
			ErrorCount:     0,
		}
		analysisResults = append(analysisResults, analysisResult)

		// Log detailed comparison
		t.Logf("\nScenario: %s", scenario)
		t.Logf("Performance Comparison:")
		t.Logf("Requests/sec: Shared=%.2f, Non-Shared=%.2f (%.2f%% %s)",
			comp.shared.RequestsPerSec,
			comp.nonShared.RequestsPerSec,
			math.Abs(throughputDiff),
			func() string {
				if throughputDiff >= 0 {
					return "improvement"
				}
				return "decrease"
			}())
		t.Logf("Duration: Shared=%v, Non-Shared=%v (%.2f%% %s)",
			comp.shared.Duration,
			comp.nonShared.Duration,
			math.Abs(durationImprov),
			func() string {
				if durationImprov >= 0 {
					return "faster"
				}
				return "slower"
			}())
		t.Logf("Success rate: Both achieved %.0f%%",
			comp.shared.SuccessRate)

		// Threshold is 50% to account for system variance while still catching
		// severe regressions. A non-shared client creating new transports per-request
		// will naturally be slower due to connection setup overhead, but should not
		// be dramatically worse. Values beyond 50% indicate a real problem
		// (e.g., resource leaks, excessive allocations, or broken connection reuse).
		assert.GreaterOrEqual(t, throughputDiff, -50.0,
			"Non-shared client performance degraded by more than 50%%")

		// Success rates must be nearly identical regardless of performance.
		// Both configurations should complete requests successfully - any significant
		// difference in success rate indicates a correctness bug, not just slowness.
		assert.InDelta(t, comp.shared.SuccessRate, comp.nonShared.SuccessRate, 5.0,
			"Success rates should be within 5%% of each other")
	}

	// Write analysis results
	analysisResult := TestResultSet{
		Timestamp:   time.Now(),
		TestName:    "Performance Analysis Summary",
		Environment: runtime.Version(),
		Results:     analysisResults,
	}

	if err := writeResultsToFile(analysisResult); err != nil {
		t.Errorf("Failed to write analysis results: %v", err)
	}
}
