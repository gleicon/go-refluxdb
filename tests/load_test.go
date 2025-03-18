package tests

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentWrites(t *testing.T) {
	httpServer, _, db := setupTestEnvironment(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server and get actual address
	httpAddr := make(chan string, 1)
	go func() {
		listener, err := net.Listen("tcp", httpServer.Addr())
		assert.NoError(t, err)
		httpAddr <- listener.Addr().String()
		err = httpServer.StartWithListener(ctx, listener)
		assert.NoError(t, err)
	}()

	// Wait for server address
	serverAddr := <-httpAddr
	t.Logf("Server started on %s", serverAddr)

	// Initialize database by creating a test point
	initData := "cpu,host=init value=1"
	resp, err := http.Post(fmt.Sprintf("http://%s/api/v2/write?org=my-org&bucket=my-bucket", serverAddr),
		"text/plain", strings.NewReader(initData))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	t.Run("concurrent http writes", func(t *testing.T) {
		numWorkers := 10
		pointsPerWorker := 1000

		var wg sync.WaitGroup
		errors := make(chan error, numWorkers*pointsPerWorker)

		startTime := time.Now()

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < pointsPerWorker; j++ {
					data := fmt.Sprintf("cpu,host=server%d value=%d %d",
						workerID, j, time.Now().UnixNano())

					resp, err := http.Post(fmt.Sprintf("http://%s/api/v2/write?org=my-org&bucket=my-bucket", serverAddr),
						"text/plain", strings.NewReader(data))

					if err != nil {
						errors <- fmt.Errorf("worker %d write error: %v", workerID, err)
						continue
					}

					if resp.StatusCode != http.StatusNoContent {
						errors <- fmt.Errorf("worker %d received status code %d",
							workerID, resp.StatusCode)
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		duration := time.Since(startTime)
		totalPoints := numWorkers * pointsPerWorker
		pointsPerSecond := float64(totalPoints) / duration.Seconds()

		t.Logf("Write performance: %.2f points/second", pointsPerSecond)
		t.Logf("Total duration: %v", duration)

		var errCount int
		for err := range errors {
			t.Logf("Error: %v", err)
			errCount++
		}

		assert.Equal(t, 0, errCount, "Expected no errors during concurrent writes")
	})
}

func TestQueryPerformance(t *testing.T) {
	httpServer, _, db := setupTestEnvironment(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server and get actual address
	httpAddr := make(chan string, 1)
	go func() {
		listener, err := net.Listen("tcp", httpServer.Addr())
		assert.NoError(t, err)
		httpAddr <- listener.Addr().String()
		err = httpServer.StartWithListener(ctx, listener)
		assert.NoError(t, err)
	}()

	// Wait for server address
	serverAddr := <-httpAddr
	t.Logf("Server started on %s", serverAddr)

	// Initialize database by creating a test point
	initData := "cpu,host=init value=1"
	resp, err := http.Post(fmt.Sprintf("http://%s/api/v2/write?org=my-org&bucket=my-bucket", serverAddr),
		"text/plain", strings.NewReader(initData))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Write test data
	numPoints := 1000
	for i := 0; i < numPoints; i++ {
		data := fmt.Sprintf("cpu,host=server1 value=%d %d",
			i, time.Now().UnixNano())

		resp, err := http.Post(fmt.Sprintf("http://%s/api/v2/write?org=my-org&bucket=my-bucket", serverAddr),
			"text/plain", strings.NewReader(data))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	}

	// Wait a bit for writes to be processed
	time.Sleep(100 * time.Millisecond)

	t.Run("query performance", func(t *testing.T) {
		numQueries := 100
		var wg sync.WaitGroup
		errors := make(chan error, numQueries)
		queryTimes := make(chan time.Duration, numQueries)

		startTime := time.Now()

		for i := 0; i < numQueries; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				queryStart := time.Now()
				resp, err := http.Get(fmt.Sprintf("http://%s/api/v2/query?org=my-org&bucket=my-bucket&measurement=cpu", serverAddr))

				if err != nil {
					errors <- err
					return
				}

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("received status code %d", resp.StatusCode)
					return
				}

				queryTimes <- time.Since(queryStart)
			}()
		}

		wg.Wait()
		close(errors)
		close(queryTimes)

		duration := time.Since(startTime)
		queriesPerSecond := float64(numQueries) / duration.Seconds()

		var totalQueryTime time.Duration
		var queryCount int
		for qt := range queryTimes {
			totalQueryTime += qt
			queryCount++
		}

		var avgQueryTime time.Duration
		if queryCount > 0 {
			avgQueryTime = totalQueryTime / time.Duration(queryCount)
		}

		t.Logf("Query performance: %.2f queries/second", queriesPerSecond)
		t.Logf("Average query time: %v", avgQueryTime)
		t.Logf("Total duration: %v", duration)

		var errCount int
		for err := range errors {
			t.Logf("Error: %v", err)
			errCount++
		}

		assert.Equal(t, 0, errCount, "Expected no errors during concurrent queries")
		assert.Greater(t, queryCount, 0, "Expected at least one successful query")
	})
}
