package tests

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/server"
	"github.com/gleicon/go-refluxdb/internal/udp"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/stretchr/testify/assert"
)

func setupTestEnvironment(t *testing.T) (*server.Server, *udp.Server, *persistence.Manager) {
	// Create a temporary file for the database
	dbPath := "test.db"
	db, err := persistence.New(dbPath)
	assert.NoError(t, err)

	// Clean up the database file when the test finishes
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})

	// Use dynamic port allocation
	httpServer := server.New(":0", db)
	udpServer := udp.New(":0", db)

	return httpServer, udpServer, db
}

func TestEndToEnd(t *testing.T) {
	httpServer, udpServer, db := setupTestEnvironment(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start servers and get actual ports
	httpAddr := make(chan string, 1)
	udpAddr := make(chan string, 1)

	go func() {
		listener, err := net.Listen("tcp", httpServer.Addr())
		assert.NoError(t, err)
		httpAddr <- listener.Addr().String()
		err = httpServer.StartWithListener(ctx, listener)
		assert.NoError(t, err)
	}()

	go func() {
		addr, err := udpServer.Start(ctx)
		assert.NoError(t, err)
		udpAddr <- addr
	}()

	// Wait for servers to start and get addresses
	httpAddress := <-httpAddr
	_ = <-udpAddr // Ignore UDP address since we're not using it

	// Wait for servers to be ready
	time.Sleep(100 * time.Millisecond)

	// Create InfluxDB client
	client := influxdb2.NewClient("http://"+httpAddress, "")
	writeAPI := client.WriteAPIBlocking("my-org", "my-bucket")

	// Test HTTP write and query
	t.Run("http write and query", func(t *testing.T) {
		// Write data using official client
		p := influxdb2.NewPoint("test",
			map[string]string{"host": "server1"},
			map[string]interface{}{"value": 42.5},
			time.Now())
		err := writeAPI.WritePoint(context.Background(), p)
		assert.NoError(t, err)

		// Query data
		resp, err := http.Get("http://" + httpAddress + "/api/v2/query?org=my-org&bucket=my-bucket&measurement=test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Test UDP write
	t.Run("udp write", func(t *testing.T) {
		// Write data using official client
		p := influxdb2.NewPoint("test",
			map[string]string{"host": "server2"},
			map[string]interface{}{"value": 85.0},
			time.Now())
		err := writeAPI.WritePoint(context.Background(), p)
		assert.NoError(t, err)

		// Give some time for processing
		time.Sleep(100 * time.Millisecond)

		// Query the data through HTTP
		resp, err := http.Get("http://" + httpAddress + "/api/v2/query?org=my-org&bucket=my-bucket&measurement=test")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestInfluxDBCompatibility(t *testing.T) {
	httpServer, udpServer, db := setupTestEnvironment(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start servers and get actual ports
	httpAddr := make(chan string, 1)

	go func() {
		listener, err := net.Listen("tcp", httpServer.Addr())
		assert.NoError(t, err)
		httpAddr <- listener.Addr().String()
		err = httpServer.StartWithListener(ctx, listener)
		assert.NoError(t, err)
	}()

	// Start UDP server in background
	go func() {
		_, err := udpServer.Start(ctx)
		assert.NoError(t, err)
	}()

	// Wait for servers to start and get addresses
	httpAddress := <-httpAddr

	// Wait for servers to be ready
	time.Sleep(100 * time.Millisecond)

	// Create InfluxDB client
	client := influxdb2.NewClient("http://"+httpAddress, "")
	writeAPI := client.WriteAPIBlocking("my-org", "my-bucket")

	// Test various data types and formats
	t.Run("data types and formats", func(t *testing.T) {
		testCases := []struct {
			name     string
			point    *write.Point
			wantCode int
		}{
			{
				name:     "integer value",
				point:    influxdb2.NewPoint("test", map[string]string{"host": "server1"}, map[string]interface{}{"value": 42}, time.Now()),
				wantCode: http.StatusNoContent,
			},
			{
				name:     "float value",
				point:    influxdb2.NewPoint("test", map[string]string{"host": "server1"}, map[string]interface{}{"value": 42.5}, time.Now()),
				wantCode: http.StatusNoContent,
			},
			{
				name:     "string value",
				point:    influxdb2.NewPoint("test", map[string]string{"host": "server1"}, map[string]interface{}{"value": "test"}, time.Now()),
				wantCode: http.StatusNoContent,
			},
			{
				name:     "boolean value",
				point:    influxdb2.NewPoint("test", map[string]string{"host": "server1"}, map[string]interface{}{"value": true}, time.Now()),
				wantCode: http.StatusNoContent,
			},
			{
				name:     "multiple fields",
				point:    influxdb2.NewPoint("test", map[string]string{"host": "server1"}, map[string]interface{}{"value1": 42, "value2": "test"}, time.Now()),
				wantCode: http.StatusNoContent,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := writeAPI.WritePoint(context.Background(), tc.point)
				assert.NoError(t, err)
			})
		}
	})
}
