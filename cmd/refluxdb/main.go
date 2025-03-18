package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/server"
	"github.com/gleicon/go-refluxdb/internal/udp"
)

func main() {
	log.Println("Starting go-refluxdb...")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize persistence layer
	db, err := persistence.New("timeseries.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize servers
	httpServer := server.New(":8086", db)
	udpServer := udp.New(":8089", db)

	// WaitGroup for graceful shutdown
	var wg sync.WaitGroup

	// Start HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.Start(ctx); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Start UDP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if addr, err := udpServer.Start(ctx); err != nil {
			log.Printf("UDP server error: %v", err)
		} else {
			log.Printf("UDP server started on %s", addr)
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, initiating graceful shutdown...", sig)

	// Cancel context to initiate shutdown
	cancel()

	// Wait for servers to shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-shutdownCtx.Done():
		log.Println("Shutdown timed out")
	case <-done:
		log.Println("Graceful shutdown completed")
	}
}
