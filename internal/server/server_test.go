package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/stretchr/testify/assert"
)

func setupTestServer(t *testing.T) (*Server, *persistence.Manager) {
	db, err := persistence.New(":memory:")
	assert.NoError(t, err)

	srv := New(":8087", db)
	return srv, db
}

func TestHTTPServer(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	// Test write endpoint
	t.Run("write endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/api/v2/write?org=test-org&bucket=test-bucket", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	// Test query endpoint
	t.Run("query endpoint", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/api/v2/write?org=test-org&bucket=test-bucket", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Now query it back
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/api/v2/query?org=test-org&bucket=test-bucket&measurement=cpu", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// Test ping endpoint
	t.Run("ping endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// Test SHOW MEASUREMENTS command
	t.Run("show measurements", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Now test SHOW MEASUREMENTS
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SHOW MEASUREMENTS", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Greater(t, len(values), 0)

		// Verify that "cpu" is in the measurements list
		found := false
		for _, value := range values {
			if value[0].(string) == "cpu" {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find 'cpu' in measurements list")
	})

	// Test query with quoted identifiers
	t.Run("query with quoted identifiers", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with quoted identifiers
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= 1556813561098000000ms and time <= 1556813561098000000ms GROUP BY time(20s) fill(null) ORDER BY time ASC", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Greater(t, len(values), 0)
	})

	// Test query with time range
	t.Run("query with time range", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value=42.5 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with time range in milliseconds
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT value FROM cpu WHERE time >= 1556813561098ms and time <= 1556813561098ms", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Len(t, values, 1)

		// Verify the timestamp was properly converted
		firstValue := values[0]
		assert.Len(t, firstValue, 2) // time, value
		timestamp := firstValue[0].(int64)
		assert.Equal(t, int64(1556813561098000000), timestamp) // Should be in nanoseconds
	})

	// Test query with time range in nanoseconds
	t.Run("query with time range in nanoseconds", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value=42.5 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with time range in nanoseconds
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT value FROM cpu WHERE time >= 1556813561098000000 and time <= 1556813561098000000", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Len(t, values, 1)

		// Verify the timestamp was properly handled
		firstValue := values[0]
		assert.Len(t, firstValue, 2) // time, value
		timestamp := firstValue[0].(int64)
		assert.Equal(t, int64(1556813561098000000), timestamp) // Should be in nanoseconds
	})

	// Test query with time range and escaped quotes
	t.Run("query with time range and escaped quotes", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with escaped quotes and time range
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= 1556813561098ms and time <= 1556813561098ms GROUP BY time(20s) fill(null) ORDER BY time ASC", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Greater(t, len(values), 0)

		// Verify the timestamp was properly converted
		firstValue := values[0]
		assert.Len(t, firstValue, 3) // time, host, value
		timestamp := firstValue[0].(int64)
		assert.Equal(t, int64(1556813561098000000), timestamp) // Should be in nanoseconds
	})

	// Test timestamp handling with different formats
	t.Run("timestamp handling with different formats", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with millisecond timestamps
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= 1556813561098ms and time <= 1556813561098ms", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Test query with nanosecond timestamps
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= 1556813561098000000 and time <= 1556813561098000000", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format for both queries
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Greater(t, len(values), 0)

		// Verify the timestamp was properly handled
		firstValue := values[0]
		assert.Len(t, firstValue, 3) // time, host, value
		timestamp := firstValue[0].(int64)
		assert.Equal(t, int64(1556813561098000000), timestamp) // Should be in nanoseconds
	})

	// Test timestamp parsing in WHERE clause
	t.Run("timestamp parsing in WHERE clause", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write?db=mydb", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Test query with both start and end times
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?db=mydb&q=SELECT mean(\"value\") FROM \"cpu\" WHERE time >= 1556813561098ms and time <= 1556813561098ms", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify response format
		var response map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)

		results, ok := response["results"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, results, 1)

		series, ok := results[0].(map[string]interface{})["series"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, series, 1)

		values, ok := series[0].(map[string]interface{})["values"].([][]interface{})
		assert.True(t, ok)
		assert.Greater(t, len(values), 0)

		// Verify the timestamp was properly handled
		firstValue := values[0]
		assert.Len(t, firstValue, 3) // time, host, value
		timestamp := firstValue[0].(int64)
		assert.Equal(t, int64(1556813561098000000), timestamp) // Should be in nanoseconds
	})
}

func TestServerStartStop(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)

	// Start server
	go func() {
		errChan <- srv.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test server is running
	resp, err := http.Get("http://localhost:8087/health")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Shutdown server
	cancel()
	err = <-errChan
	assert.NoError(t, err)
}

// TestInsertTestData inserts test data points with March 2025 timestamps
func TestInsertTestData(t *testing.T) {
	db, err := persistence.New(":memory:")
	assert.NoError(t, err)
	defer db.Close()

	// Base timestamp for March 19, 2025 12:00:00 UTC
	baseTime := time.Date(2025, 3, 19, 12, 0, 0, 0, time.UTC).UnixNano()

	// Test data points
	testData := []struct {
		measurement string
		field       string
		value       float64
		tags        map[string]string
		timestamp   int64
	}{
		{
			measurement: "cpu",
			field:       "value",
			value:       42.5,
			tags:        map[string]string{"host": "server1"},
			timestamp:   baseTime,
		},
		{
			measurement: "cpu",
			field:       "value",
			value:       85.0,
			tags:        map[string]string{"host": "server2"},
			timestamp:   baseTime,
		},
		{
			measurement: "memory",
			field:       "used",
			value:       1024.0,
			tags:        map[string]string{"host": "server1"},
			timestamp:   baseTime,
		},
		{
			measurement: "memory",
			field:       "free",
			value:       2048.0,
			tags:        map[string]string{"host": "server1"},
			timestamp:   baseTime,
		},
	}

	// Insert test data
	for _, data := range testData {
		err := db.SaveMeasurement(data.measurement, data.field, data.value, data.tags, data.timestamp)
		assert.NoError(t, err)
		fmt.Printf("Inserted point: measurement=%s, field=%s, value=%f, tags=%v, timestamp=%d (UTC: %s)\n",
			data.measurement,
			data.field,
			data.value,
			data.tags,
			data.timestamp,
			time.Unix(0, data.timestamp).UTC().Format(time.RFC3339Nano))
	}

	// Verify the data was inserted
	points, err := db.GetMeasurementRange("cpu", baseTime-3600000000000, baseTime+3600000000000)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(points), "Expected 2 CPU points")

	points, err = db.GetMeasurementRange("memory", baseTime-3600000000000, baseTime+3600000000000)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(points), "Expected 2 memory points")
}
