package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
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

	// InfluxDB v1 API endpoints
	v1 := s.router.Group("/")
	{
		v1.POST("/write", s.handleV1Write)
		v1.GET("/query", s.handleV1Query)
		v1.POST("/query", s.handleV1Query)
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

func (s *Server) handleV1Write(c *gin.Context) {
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get database from query parameters
	db := c.Query("db")
	if db == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database is required"})
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

func (s *Server) handleV1Query(c *gin.Context) {
	// Log the incoming request details
	s.log.Infof("Received %s request to %s", c.Request.Method, c.Request.URL.Path)
	s.log.Debugf("Query parameters: %v", c.Request.URL.Query())

	// Get query from query parameters or body
	var query string
	if c.Request.Method == "GET" {
		query = c.Query("q")
		s.log.Debugf("GET query from parameters: %q", query)
		if query == "" {
			// Try to get query from body even for GET requests
			body, err := ioutil.ReadAll(c.Request.Body)
			if err != nil {
				s.log.Errorf("Error reading body: %v", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			query = string(body)
			s.log.Debugf("GET query from body: %q", query)
		}
	} else {
		// For POST requests, try query parameter first
		query = c.Query("q")
		s.log.Debugf("POST query from parameters: %q", query)
		if query == "" {
			// If not in query parameters, try body
			body, err := ioutil.ReadAll(c.Request.Body)
			if err != nil {
				s.log.Errorf("Error reading body: %v", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			query = string(body)
			s.log.Debugf("POST query from body: %q", query)
		}
	}

	if query == "" {
		s.log.Error("Missing query parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is required"})
		return
	}

	// Convert query to lowercase for case-insensitive matching
	queryLower := strings.ToLower(query)
	s.log.Debugf("Processing query: %q", queryLower)

	// Handle SHOW DATABASES command
	if queryLower == "show databases" {
		s.log.Info("Handling SHOW DATABASES command")
		// TODO: Get actual databases from persistence layer
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"statement_id": 0,
					"series": []map[string]interface{}{
						{
							"name":    "databases",
							"columns": []string{"name"},
							"values":  [][]interface{}{{"mydb"}},
						},
					},
				},
			},
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// Handle SHOW MEASUREMENTS command
	if queryLower == "show measurements" {
		s.log.Info("Handling SHOW MEASUREMENTS command")
		measurements, err := s.db.ListTimeseries()
		if err != nil {
			s.log.Errorf("Failed to list measurements: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list measurements: %v", err)})
			return
		}

		// Convert measurements to response format
		values := make([][]interface{}, len(measurements))
		for i, m := range measurements {
			values[i] = []interface{}{m}
		}

		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"statement_id": 0,
					"series": []map[string]interface{}{
						{
							"name":    "measurements",
							"columns": []string{"name"},
							"values":  values,
						},
					},
				},
			},
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// Handle CREATE DATABASE command
	if strings.HasPrefix(queryLower, "create database") {
		s.log.Info("Handling CREATE DATABASE command")
		// Extract database name
		parts := strings.Fields(query)
		if len(parts) < 3 {
			s.log.Error("Invalid CREATE DATABASE syntax")
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid CREATE DATABASE syntax"})
			return
		}

		dbName := parts[2]
		s.log.Infof("Creating database: %s", dbName)
		// TODO: Actually create the database in persistence layer

		// Return success response
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"statement_id": 0,
				},
			},
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// Handle USE command
	if strings.HasPrefix(queryLower, "use") {
		s.log.Info("Handling USE command")
		// Extract database name
		parts := strings.Fields(query)
		if len(parts) < 2 {
			s.log.Error("Invalid USE syntax")
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid USE syntax"})
			return
		}

		dbName := parts[1]
		s.log.Infof("Using database: %s", dbName)
		// TODO: Check if database exists in persistence layer
		// For now, we'll accept any database name

		// Return success response
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"statement_id": 0,
				},
			},
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// For other queries, we need a database
	db := c.Query("db")
	if db == "" {
		s.log.Error("Missing database parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "database is required"})
		return
	}

	// Parse the query to get measurement name and aggregation
	measurement := ""
	aggregation := ""
	field := "*"
	startTime := int64(0)
	endTime := time.Now().UnixNano()

	// Handle SELECT queries
	if strings.HasPrefix(queryLower, "select") {
		// Extract aggregation function if present
		selectPart := strings.Split(queryLower, "from")[0]
		selectPart = strings.TrimPrefix(selectPart, "select")
		selectPart = strings.TrimSpace(selectPart)

		// Check for aggregation functions
		aggFuncs := []string{"mean", "sum", "count", "min", "max"}
		for _, agg := range aggFuncs {
			if strings.HasPrefix(selectPart, agg+"(") {
				aggregation = agg
				// Extract field name from inside parentheses
				field = strings.Trim(strings.Split(selectPart, "(")[1], ")")
				break
			}
		}

		// If no aggregation, just get the field name
		if aggregation == "" {
			field = selectPart
		}

		// Extract measurement name and WHERE clause from FROM clause
		parts := strings.Split(queryLower, "from")
		if len(parts) > 1 {
			fromPart := strings.TrimSpace(parts[1])

			// Extract WHERE clause if present
			if whereIdx := strings.Index(fromPart, "where"); whereIdx != -1 {
				whereClause := strings.TrimSpace(fromPart[whereIdx+5:])

				// Parse time range from WHERE clause
				if timeIdx := strings.Index(whereClause, "time"); timeIdx != -1 {
					timePart := strings.TrimSpace(whereClause[timeIdx+4:])
					s.log.Debugf("Parsing time part: %q", timePart)

					// Parse >= condition
					if startIdx := strings.Index(timePart, ">="); startIdx != -1 {
						startStr := strings.TrimSpace(timePart[startIdx+2:])
						if endIdx := strings.Index(startStr, "and"); endIdx != -1 {
							startStr = strings.TrimSpace(startStr[:endIdx])
							s.log.Debugf("Found start time string: %q", startStr)
							var parseErr error
							// Convert to nanoseconds if in milliseconds
							if strings.HasSuffix(startStr, "ms") {
								startStr = strings.TrimSuffix(startStr, "ms")
								startTime, parseErr = strconv.ParseInt(startStr, 10, 64)
								if parseErr != nil {
									s.log.Errorf("Invalid start time format: %v", parseErr)
									c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid start time format: %v", parseErr)})
									return
								}
								startTime *= 1000000 // Convert ms to ns
								s.log.Debugf("Converted start time from ms to ns: %d", startTime)
							} else {
								// If no ms suffix, assume nanoseconds
								startTime, parseErr = strconv.ParseInt(startStr, 10, 64)
								if parseErr != nil {
									s.log.Errorf("Invalid start time format: %v", parseErr)
									c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid start time format: %v", parseErr)})
									return
								}
								s.log.Debugf("Parsed start time as ns: %d", startTime)
							}
						}
					}

					// Parse <= condition
					if endIdx := strings.Index(timePart, "<="); endIdx != -1 {
						endStr := strings.TrimSpace(timePart[endIdx+2:])
						s.log.Debugf("Found end time string: %q", endStr)
						// Find the end of the timestamp by looking for the next space or end of string
						spaceIdx := strings.Index(endStr, " ")
						if spaceIdx != -1 {
							endStr = endStr[:spaceIdx]
						}
						s.log.Debugf("Trimmed end time string: %q", endStr)
						var parseErr error
						// Convert to nanoseconds if in milliseconds
						if strings.HasSuffix(endStr, "ms") {
							endStr = strings.TrimSuffix(endStr, "ms")
							endTime, parseErr = strconv.ParseInt(endStr, 10, 64)
							if parseErr != nil {
								s.log.Errorf("Invalid end time format: %v", parseErr)
								c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid end time format: %v", parseErr)})
								return
							}
							endTime *= 1000000 // Convert ms to ns
							s.log.Debugf("Converted end time from ms to ns: %d", endTime)
						} else {
							// If no ms suffix, assume nanoseconds
							endTime, parseErr = strconv.ParseInt(endStr, 10, 64)
							if parseErr != nil {
								s.log.Errorf("Invalid end time format: %v", parseErr)
								c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid end time format: %v", parseErr)})
								return
							}
							s.log.Debugf("Parsed end time as ns: %d", endTime)
						}
					}
				}
				fromPart = strings.TrimSpace(fromPart[:whereIdx])
			}

			// Split by GROUP BY if present
			groupParts := strings.Split(fromPart, "group by")
			measurement = strings.TrimSpace(groupParts[0])
			// Strip quotes from measurement name, handling both regular and escaped quotes
			measurement = strings.Trim(strings.Trim(measurement, "\""), "\\\"")
		}
	}

	// Strip quotes from field name, handling both regular and escaped quotes
	field = strings.Trim(strings.Trim(field, "\""), "\\\"")

	if measurement == "" {
		s.log.Error("Could not determine measurement from query")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid query format"})
		return
	}

	s.log.Infof("Parsed query - measurement: %s, field: %s, start: %d, end: %d", measurement, field, startTime, endTime)

	// Log the query in a format ready for InfluxDB CLI
	influxQuery := fmt.Sprintf("SELECT mean(\"%s\") FROM \"%s\" WHERE time >= %dms and time <= %dms GROUP BY time(1m) fill(null) ORDER BY time ASC",
		field, measurement, startTime/1000000, endTime/1000000)
	s.log.Debugf("InfluxDB CLI ready query: %s", influxQuery)

	// Query the database with the parsed time range
	s.log.Infof("Querying measurement %s with time range: start=%d (UTC: %s), end=%d (UTC: %s)",
		measurement,
		startTime,
		time.Unix(0, startTime).UTC().Format(time.RFC3339Nano),
		endTime,
		time.Unix(0, endTime).UTC().Format(time.RFC3339Nano))

	points, err := s.db.GetMeasurementRange(measurement, startTime, endTime)
	if err != nil {
		s.log.Errorf("Failed to query measurements: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to query measurements: %v", err)})
		return
	}

	s.log.Infof("Found %d points in time range", len(points))
	if len(points) > 0 {
		s.log.Debugf("First point timestamp: %d (UTC: %s)",
			points[0].Timestamp.UnixNano(),
			points[0].Timestamp.UTC().Format(time.RFC3339Nano))
		s.log.Debugf("Last point timestamp: %d (UTC: %s)",
			points[len(points)-1].Timestamp.UnixNano(),
			points[len(points)-1].Timestamp.UTC().Format(time.RFC3339Nano))
	}

	// Process points based on aggregation
	if aggregation == "mean" {
		// Extract group by interval from the query
		groupByInterval := int64(5 * 60 * 1e9) // default 5 minutes in nanoseconds
		if strings.Contains(queryLower, "group by time") {
			groupByPart := strings.Split(queryLower, "group by time(")[1]
			if strings.Contains(groupByPart, "m)") {
				minutes := strings.Split(groupByPart, "m)")[0]
				if mins, err := strconv.ParseInt(minutes, 10, 64); err == nil {
					groupByInterval = mins * 60 * 1e9 // convert minutes to nanoseconds
					s.log.Debugf("Using group by interval: %d minutes", mins)
				}
			}
		}

		// Group points by time bucket
		groupedPoints := make(map[int64][]float64)

		for _, point := range points {
			if val, ok := point.Fields[field]; ok {
				// Calculate bucket timestamp
				ts := point.Timestamp.UnixNano()
				bucketTime := ts - (ts % groupByInterval)
				s.log.Debugf("Point timestamp: %d, Bucket timestamp: %d", ts, bucketTime)
				groupedPoints[bucketTime] = append(groupedPoints[bucketTime], val)
			}
		}

		// Calculate mean for each bucket
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"statement_id": 0,
					"series": []map[string]interface{}{
						{
							"name":    measurement,
							"columns": []string{"time", "mean"},
							"values":  make([][]interface{}, 0),
						},
					},
				},
			},
		}

		// Sort timestamps for consistent ordering
		timestamps := make([]int64, 0, len(groupedPoints))
		for ts := range groupedPoints {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

		// Calculate mean for each bucket and add to response
		for _, ts := range timestamps {
			values := groupedPoints[ts]
			sum := 0.0
			for _, v := range values {
				sum += v
			}
			mean := sum / float64(len(values))

			s.log.Debugf("Adding bucket - Time: %d (UTC: %s), Mean: %f",
				ts,
				time.Unix(0, ts).UTC().Format(time.RFC3339Nano),
				mean)

			// Convert timestamp from nanoseconds to milliseconds for Grafana
			tsMillis := ts / 1000000

			response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"] = append(
				response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"].([][]interface{}),
				[]interface{}{tsMillis, mean},
			)
		}

		// Log the response payload in a more readable format
		jsonResponse, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			s.log.Errorf("Error marshaling response: %v", err)
		} else {
			s.log.Debugf("Response payload:\n%s", string(jsonResponse))
		}

		c.JSON(http.StatusOK, response)
		return
	}

	// For non-aggregated queries, return all points with their timestamps
	response := map[string]interface{}{
		"results": []map[string]interface{}{
			{
				"statement_id": 0,
				"series": []map[string]interface{}{
					{
						"name":    measurement,
						"columns": []string{"time", field},
						"values":  make([][]interface{}, 0),
					},
				},
			},
		},
	}

	// For regular queries, return all points
	for _, point := range points {
		if field == "*" {
			// Include all fields
			for _, fieldValue := range point.Fields {
				// Convert timestamp from nanoseconds to milliseconds for Grafana
				tsMillis := point.Timestamp.UnixNano() / 1000000
				response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"] = append(
					response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"].([][]interface{}),
					[]interface{}{tsMillis, fieldValue},
				)
			}
		} else if val, ok := point.Fields[field]; ok {
			// Convert timestamp from nanoseconds to milliseconds for Grafana
			tsMillis := point.Timestamp.UnixNano() / 1000000
			response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"] = append(
				response["results"].([]map[string]interface{})[0]["series"].([]map[string]interface{})[0]["values"].([][]interface{}),
				[]interface{}{tsMillis, val},
			)
		}
	}

	// Log the response payload in a more readable format
	jsonResponse, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		s.log.Errorf("Error marshaling response: %v", err)
	} else {
		s.log.Debugf("Response payload:\n%s", string(jsonResponse))
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "1.0.0",
		"status":  "ok",
	})
}
