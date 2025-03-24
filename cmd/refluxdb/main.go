package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gleicon/go-refluxdb/internal/mqtt"
	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/server"
	"github.com/gleicon/go-refluxdb/internal/udp"
	"github.com/sirupsen/logrus"
)

func main() {

	lvl, ok := os.LookupEnv("LOG_LEVEL")
	// LOG_LEVEL not set, let's default to debug
	if !ok {
		lvl = "debug"
	}
	// parse string, this is built-in feature of logrus
	ll, err := logrus.ParseLevel(lvl)
	if err != nil {
		ll = logrus.DebugLevel
	}
	// set global log level
	logrus.SetLevel(ll)
	logger := logrus.New()

	logger.Println("Starting go-refluxdb...")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize persistence layer
	db, err := persistence.New("timeseries.db")
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize servers
	httpServer := server.New(":8086", db, logger)
	udpServer := udp.New(":8089", db, logger)
	mqttServer := mqtt.New("localhost:1883", db, logger)

	// WaitGroup for graceful shutdown
	var wg sync.WaitGroup

	// Start HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.Start(ctx); err != nil {
			logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Start UDP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if addr, err := udpServer.Start(ctx); err != nil {
			logger.Printf("UDP server error: %v", err)
		} else {
			logger.Printf("UDP server started on %s", addr)
		}
	}()

	// Start MQTT server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := mqttServer.Start(ctx); err != nil {
			logger.Printf("MQTT server error: %v", err)
		} else {
			logger.Printf("MQTT server started on %s", mqttServer.Addr())
		}
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logrus.Printf("Received signal %v, initiating graceful shutdown...", sig)

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
		logger.Println("Shutdown timed out")
	case <-done:
		logger.Println("Graceful shutdown completed")
	}
}
