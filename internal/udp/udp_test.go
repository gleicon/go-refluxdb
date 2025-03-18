package udp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/stretchr/testify/assert"
)

func setupTestServer(t *testing.T) (*Server, *persistence.Manager) {
	db, err := persistence.New(":memory:")
	assert.NoError(t, err)

	srv := New(":8089", db)
	return srv, db
}

func TestUDPServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New(":0", nil)
	assert.NotNil(t, srv)

	errChan := make(chan error, 1)
	addrChan := make(chan string, 1)

	go func() {
		addr, err := srv.Start(ctx)
		if err != nil {
			errChan <- err
			return
		}
		addrChan <- addr
	}()

	select {
	case err := <-errChan:
		t.Fatalf("Failed to start UDP server: %v", err)
	case addr := <-addrChan:
		t.Logf("UDP server started on %s", addr)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for UDP server to start")
	}
}

func TestUDPServerWithInvalidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New("invalid-address", nil)
	assert.NotNil(t, srv)

	errChan := make(chan error, 1)
	addrChan := make(chan string, 1)

	go func() {
		addr, err := srv.Start(ctx)
		if err != nil {
			errChan <- err
			return
		}
		addrChan <- addr
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err)
	case <-addrChan:
		t.Fatal("Expected error but server started successfully")
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for UDP server to start")
	}
}

func TestUDPServerInvalidData(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	errChan := make(chan error, 1)
	addrChan := make(chan string, 1)
	go func() {
		addr, err := srv.Start(ctx)
		if err != nil {
			errChan <- err
			return
		}
		addrChan <- addr
	}()

	// Wait for server to start
	select {
	case err := <-errChan:
		t.Fatalf("Failed to start UDP server: %v", err)
	case addr := <-addrChan:
		t.Logf("UDP server started on %s", addr)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for UDP server to start")
	}

	// Test sending invalid data
	t.Run("send invalid data", func(t *testing.T) {
		conn, err := net.Dial("udp", ":8089")
		assert.NoError(t, err)
		defer conn.Close()

		data := "invalid data format"
		_, err = conn.Write([]byte(data))
		assert.NoError(t, err)

		// Give some time for the server to process the data
		time.Sleep(100 * time.Millisecond)
		// The server should log the error but continue running
	})

	// Test server shutdown
	cancel()
	err := <-errChan
	assert.NoError(t, err)
}
