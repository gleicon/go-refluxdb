package udp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/protocol"
	"github.com/sirupsen/logrus"
)

// Server represents a UDP server
type Server struct {
	addr       string
	db         *persistence.Manager
	conn       *net.UDPConn
	wg         sync.WaitGroup
	mu         sync.Mutex
	isRunning  bool
	bufferSize int
}

// New creates a new UDP server
func New(addr string, db *persistence.Manager) *Server {
	return &Server{
		addr:       addr,
		db:         db,
		bufferSize: 1024,
	}
}

// Start starts the UDP server
func (s *Server) Start(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return "", fmt.Errorf("server is already running")
	}
	s.isRunning = true
	s.mu.Unlock()

	udpAddr, err := net.ResolveUDPAddr("udp", s.addr)
	if err != nil {
		return "", fmt.Errorf("failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return "", fmt.Errorf("failed to start UDP server: %v", err)
	}
	s.conn = conn

	actualAddr := conn.LocalAddr().String()
	logrus.Infof("Starting UDP server on %s", actualAddr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		buffer := make([]byte, s.bufferSize)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, _, err := conn.ReadFromUDP(buffer)
				if err != nil {
					if !strings.Contains(err.Error(), "use of closed network connection") {
						logrus.Errorf("Error reading UDP packet: %v", err)
					}
					continue
				}

				data := string(buffer[:n])
				lines := strings.Split(strings.TrimSpace(data), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}

					proto, err := protocol.Parse(line)
					if err != nil {
						logrus.Errorf("Error parsing line protocol: %v", err)
						continue
					}

					// Save each field as a separate measurement
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
								logrus.Errorf("Invalid integer value: %s", value)
								continue
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
								logrus.Errorf("Invalid numeric value: %s", value)
								continue
							}
						}

						err = s.db.SaveMeasurement(proto.Measurement, field, floatValue, proto.Tags, proto.Timestamp)
						if err != nil {
							logrus.Errorf("Error saving measurement: %v", err)
						}
					}
				}
			}
		}
	}()

	return actualAddr, nil
}

// Stop stops the UDP server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			return fmt.Errorf("error closing UDP connection: %v", err)
		}
		s.conn = nil
	}

	s.wg.Wait()
	s.isRunning = false
	return nil
}
