package persistence

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Manager handles database operations for time series data
type Manager struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

// Point represents a single time series data point
type Point struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]float64
	Timestamp   time.Time
}

// New creates a new persistence manager
func New(dbPath string) (*Manager, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables if they don't exist
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &Manager{
		db:   db,
		path: dbPath,
	}, nil
}

func createSchema(db *sql.DB) error {
	schema := `
    CREATE TABLE IF NOT EXISTS points (
        id INTEGER PRIMARY KEY,
        measurement TEXT NOT NULL,
        timestamp INTEGER NOT NULL,
        tags TEXT NOT NULL,
        fields TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_measurement ON points(measurement);
    CREATE INDEX IF NOT EXISTS idx_timestamp ON points(timestamp);
    `

	_, err := db.Exec(schema)
	return err
}

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// SaveMeasurement saves a single measurement to the database
func (m *Manager) SaveMeasurement(measurement, field string, value float64, tags map[string]string, timestamp int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	fields := map[string]float64{field: value}
	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("failed to marshal fields: %w", err)
	}

	query := `
        INSERT INTO points (measurement, timestamp, tags, fields)
        VALUES (?, ?, ?, ?)
    `

	_, err = m.db.Exec(query, measurement, timestamp, string(tagsJSON), string(fieldsJSON))
	if err != nil {
		return fmt.Errorf("failed to insert measurement: %w", err)
	}

	return nil
}

// GetMeasurementRange retrieves measurements within a time range
func (m *Manager) GetMeasurementRange(measurement string, start, end int64) ([]Point, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `
        SELECT timestamp, tags, fields
        FROM points
        WHERE measurement = ? AND timestamp >= ? AND timestamp <= ?
        ORDER BY timestamp
    `

	rows, err := m.db.Query(query, measurement, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query measurements: %w", err)
	}
	defer rows.Close()

	var points []Point
	for rows.Next() {
		var timestamp int64
		var tagsJSON, fieldsJSON string

		err := rows.Scan(&timestamp, &tagsJSON, &fieldsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var tags map[string]string
		var fields map[string]float64

		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
		}

		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return nil, fmt.Errorf("failed to unmarshal fields: %w", err)
		}

		points = append(points, Point{
			Measurement: measurement,
			Tags:        tags,
			Fields:      fields,
			Timestamp:   time.Unix(0, timestamp),
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return points, nil
}

// ListTimeseries returns a list of all measurement names
func (m *Manager) ListTimeseries() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query := `SELECT DISTINCT measurement FROM points`

	rows, err := m.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query measurements: %w", err)
	}
	defer rows.Close()

	var measurements []string
	for rows.Next() {
		var measurement string
		if err := rows.Scan(&measurement); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		measurements = append(measurements, measurement)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return measurements, nil
}
