package server

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/protocol"
	"github.com/sirupsen/logrus"
)

type Server struct {
	addr   string
	db     *persistence.Manager
	router *gin.Engine
	log    *logrus.Logger
}

func New(addr string, db *persistence.Manager) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	s := &Server{
		addr:   addr,
		db:     db,
		router: router,
		log:    logrus.New(),
	}

	s.setupRoutes()
	return s
}

// Addr returns the server's address
func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) setupRoutes() {
	// InfluxDB v2 API endpoints
	v2 := s.router.Group("/api/v2")
	{
		v2.POST("/write", s.handleWrite)
		v2.POST("/query", s.handleQuery)
		v2.GET("/query", s.handleQuery)
	}

	// Health check endpoint
	s.router.GET("/health", s.handlePing)
}

func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log.Errorf("Server shutdown error: %v", err)
		}
	}()

	s.log.Infof("Starting HTTP server on %s", s.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// StartWithListener starts the server with a pre-configured listener
func (s *Server) StartWithListener(ctx context.Context, listener net.Listener) error {
	srv := &http.Server{
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log.Errorf("Server shutdown error: %v", err)
		}
	}()

	s.log.Infof("Starting HTTP server on %s", listener.Addr().String())
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func (s *Server) handleWrite(c *gin.Context) {
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get org and bucket from query parameters
	org := c.Query("org")
	bucket := c.Query("bucket")
	if org == "" || bucket == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "org and bucket are required"})
		return
	}

	// Split into lines and process each line
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse line protocol
		proto, err := protocol.Parse(line)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to parse line: %v", err)})
			return
		}

		// Convert field values to float64
		for field, value := range proto.Fields {
			var floatValue float64

			// Handle different field value types
			if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				// String value - store as 1.0 (presence)
				value = strings.Trim(value, "\"")
				floatValue = 1.0
			} else if strings.HasSuffix(value, "i") {
				// Integer value
				numStr := value[:len(value)-1]
				if intVal, err := strconv.ParseInt(numStr, 10, 64); err == nil {
					floatValue = float64(intVal)
				} else {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid integer value: %s", value)})
					return
				}
			} else if strings.ToLower(value) == "true" {
				floatValue = 1.0
			} else if strings.ToLower(value) == "false" {
				floatValue = 0.0
			} else {
				// Try to parse as float
				if val, err := strconv.ParseFloat(value, 64); err == nil {
					floatValue = val
				} else {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid numeric value: %s", value)})
					return
				}
			}

			// Save each field as a separate measurement
			err = s.db.SaveMeasurement(proto.Measurement, field, floatValue, proto.Tags, proto.Timestamp)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save measurement: %v", err)})
				return
			}
		}
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) handleQuery(c *gin.Context) {
	// Get org and bucket from query parameters
	org := c.Query("org")
	bucket := c.Query("bucket")
	if org == "" || bucket == "" {
		s.log.Error("Missing org or bucket parameters")
		c.JSON(http.StatusBadRequest, gin.H{"error": "org and bucket are required"})
		return
	}

	// Get measurement from query parameters
	measurement := c.Query("measurement")
	if measurement == "" {
		s.log.Error("Missing measurement parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "measurement is required"})
		return
	}

	// Get time range (optional)
	start := c.Query("start")
	end := c.Query("end")

	var startTime, endTime int64
	var err error

	if start != "" {
		startTime, err = strconv.ParseInt(start, 10, 64)
		if err != nil {
			s.log.Errorf("Invalid start time: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid start time: %v", err)})
			return
		}
	} else {
		startTime = 0
	}

	if end != "" {
		endTime, err = strconv.ParseInt(end, 10, 64)
		if err != nil {
			s.log.Errorf("Invalid end time: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid end time: %v", err)})
			return
		}
	} else {
		endTime = time.Now().UnixNano()
	}

	s.log.Infof("Querying measurement %s from %d to %d", measurement, startTime, endTime)

	// Query the database
	points, err := s.db.GetMeasurementRange(measurement, startTime, endTime)
	if err != nil {
		s.log.Errorf("Failed to query measurements: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to query measurements: %v", err)})
		return
	}

	s.log.Infof("Found %d points", len(points))

	// Convert points to InfluxDB v2 response format
	response := map[string]interface{}{
		"results": []map[string]interface{}{
			{
				"statement_id": 0,
				"series": []map[string]interface{}{
					{
						"name":    measurement,
						"columns": []string{"time", "field", "value"},
						"values":  make([][]interface{}, 0, len(points)),
					},
				},
			},
		},
	}

	for _, point := range points {
		// For each field in the point, add a value
		for field, value := range point.Fields {
			response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"] = append(
				response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"].([][]interface{}),
				[]interface{}{point.Timestamp.UnixNano(), field, value},
			)
		}
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "1.0.0",
		"status":  "ok",
	})
}
