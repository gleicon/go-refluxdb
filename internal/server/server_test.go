package server

import (
	"context"
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
		req, _ := http.NewRequest("POST", "/write", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	// Test query endpoint
	t.Run("query endpoint", func(t *testing.T) {
		// First write some test data
		w := httptest.NewRecorder()
		data := `cpu,host=server1 value="42.5" 1556813561098000000`
		req, _ := http.NewRequest("POST", "/write", strings.NewReader(data))
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Now query it back
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/query?measurement=test", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	// Test ping endpoint
	t.Run("ping endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ping", nil)
		srv.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
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
	resp, err := http.Get("http://localhost:8087/ping")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Shutdown server
	cancel()
	err = <-errChan
	assert.NoError(t, err)
}
