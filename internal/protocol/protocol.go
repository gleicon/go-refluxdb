// Package protocol implements the InfluxDB Line Protocol parser and serializer.
//
// The InfluxDB Line Protocol is a text-based format for writing points to InfluxDB.
// Each line represents a single point with the following format:
//
//	<measurement>[,<tag_key>=<tag_value>...] <field_key>=<field_value>[,<field_key>=<field_value>...] [timestamp]
//
// Where:
// - measurement: The name of the measurement (can be quoted if contains spaces or special chars)
// - tags: Optional comma-separated key-value pairs. Tag values can be quoted if they contain spaces
// - fields: One or more key-value pairs. Field values can be:
//   - Integers (e.g., value=42i)
//   - Floats (e.g., value=42.0)
//   - Strings (e.g., value="42")
//   - Booleans (e.g., value=true)
//
// - timestamp: Optional Unix timestamp in nanoseconds
//
// Examples:
//
//	cpu,host=server1,region=us-west value=42i,temp=23.4 1465839830100400200
//	"my measurement with spaces",foo=bar value="string field"
//	weather,location=us-midwest temperature=82 1465839830100400200
//
// Reference: https://docs.influxdata.com/influxdb/v1.8/write_protocols/line_protocol_reference/
package protocol

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// LineProtocol represents a line in the InfluxDB line protocol
type LineProtocol struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]string
	Timestamp   int64
	fieldOrder  []string // to preserve field order
	tagOrder    []string // to preserve tag order
}

// Parse parses a line protocol string into a LineProtocol struct
func Parse(line string) (*LineProtocol, error) {
	lp := New("")

	// Trim any whitespace and newlines
	line = strings.TrimSpace(line)

	// Split into measurement+tags and fields+timestamp
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid line protocol format")
	}

	// Parse measurement and tags
	measurementAndTags := parts[0]
	var measurement string
	var tags string

	// Handle quoted measurement
	if strings.HasPrefix(measurementAndTags, "\"") {
		// Find the closing quote
		var i int
		var inEscape bool
		for i = 1; i < len(measurementAndTags); i++ {
			if inEscape {
				inEscape = false
				continue
			}
			if measurementAndTags[i] == '\\' {
				inEscape = true
				continue
			}
			if measurementAndTags[i] == '"' {
				break
			}
		}
		if i >= len(measurementAndTags) {
			return nil, fmt.Errorf("unterminated quoted measurement")
		}
		measurement = measurementAndTags[1:i]
		if i+1 < len(measurementAndTags) {
			if measurementAndTags[i+1] != ',' {
				return nil, fmt.Errorf("invalid character after quoted measurement")
			}
			tags = measurementAndTags[i+2:]
		}
	} else {
		// Unquoted measurement
		measurementParts := strings.SplitN(measurementAndTags, ",", 2)
		measurement = measurementParts[0]
		if len(measurementParts) > 1 {
			tags = measurementParts[1]
		}
	}

	if measurement == "" {
		return nil, fmt.Errorf("empty measurement")
	}

	lp.Measurement = measurement

	// Parse tags
	if tags != "" {
		lp.Tags = make(map[string]string)
		tagPairs := strings.Split(tags, ",")
		for _, pair := range tagPairs {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid tag format: %s", pair)
			}
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])

			// Handle quoted tag values
			if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				value = value[1 : len(value)-1]
			}

			if key == "" {
				return nil, fmt.Errorf("empty tag key")
			}
			if value == "" {
				return nil, fmt.Errorf("empty tag value")
			}

			lp.Tags[key] = value
			lp.tagOrder = append(lp.tagOrder, key)
		}
	} else {
		lp.Tags = nil
		lp.tagOrder = nil
	}

	// Split fields and timestamp
	fieldsAndTime := strings.SplitN(parts[1], " ", 2)
	if len(fieldsAndTime) == 0 {
		return nil, fmt.Errorf("missing fields")
	}

	// Parse fields
	lp.Fields = make(map[string]string)
	fields := strings.Split(fieldsAndTime[0], ",")
	for _, field := range fields {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid field format: %s", field)
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		if key == "" {
			return nil, fmt.Errorf("empty field key")
		}

		// Handle field value types
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			// String value - validate it's properly quoted
			if len(value) < 2 {
				return nil, fmt.Errorf("invalid string field value: %s", value)
			}
			lp.Fields[key] = value
		} else if strings.HasSuffix(value, "i") {
			// Integer value
			numStr := value[:len(value)-1]
			if _, err := strconv.ParseInt(numStr, 10, 64); err != nil {
				return nil, fmt.Errorf("invalid integer field value: %s", value)
			}
			lp.Fields[key] = value
		} else if strings.ToLower(value) == "true" || strings.ToLower(value) == "false" {
			// Boolean value
			lp.Fields[key] = strings.ToLower(value)
		} else {
			// Try to parse as float (default numeric type)
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return nil, fmt.Errorf("invalid numeric field value: %s", value)
			}
			lp.Fields[key] = value
		}
		lp.fieldOrder = append(lp.fieldOrder, key)
	}

	// Parse timestamp if present
	if len(fieldsAndTime) > 1 {
		timestamp, err := strconv.ParseInt(fieldsAndTime[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %s", fieldsAndTime[1])
		}
		lp.Timestamp = timestamp
	}

	return lp, nil
}

// String converts the LineProtocol struct to a line protocol string
func (lp *LineProtocol) String() string {
	if lp == nil {
		return ""
	}

	var sb strings.Builder

	// Write measurement
	if strings.Contains(lp.Measurement, " ") || strings.Contains(lp.Measurement, ",") {
		sb.WriteString("\"")
		sb.WriteString(strings.ReplaceAll(lp.Measurement, "\"", "\\\""))
		sb.WriteString("\"")
	} else {
		sb.WriteString(lp.Measurement)
	}

	// Write tags in order
	if lp.Tags != nil && len(lp.tagOrder) > 0 {
		for _, k := range lp.tagOrder {
			v := lp.Tags[k]
			sb.WriteString(",")
			sb.WriteString(k)
			sb.WriteString("=")
			if strings.Contains(v, " ") {
				sb.WriteString("\"")
				sb.WriteString(strings.ReplaceAll(v, "\"", "\\\""))
				sb.WriteString("\"")
			} else {
				sb.WriteString(v)
			}
		}
	} else if lp.Tags != nil {
		// Fallback to sorted order if no order is preserved
		keys := make([]string, 0, len(lp.Tags))
		for k := range lp.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := lp.Tags[k]
			sb.WriteString(",")
			sb.WriteString(k)
			sb.WriteString("=")
			if strings.Contains(v, " ") {
				sb.WriteString("\"")
				sb.WriteString(strings.ReplaceAll(v, "\"", "\\\""))
				sb.WriteString("\"")
			} else {
				sb.WriteString(v)
			}
		}
	}

	// Write fields in order
	sb.WriteString(" ")
	if lp.Fields != nil && len(lp.fieldOrder) > 0 {
		first := true
		for _, k := range lp.fieldOrder {
			if !first {
				sb.WriteString(",")
			}
			first = false
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(lp.Fields[k])
		}
	} else if lp.Fields != nil {
		// Fallback to unordered if no order is preserved
		first := true
		for k, v := range lp.Fields {
			if !first {
				sb.WriteString(",")
			}
			first = false
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(v)
		}
	}

	// Write timestamp
	if lp.Timestamp > 0 {
		sb.WriteString(" ")
		sb.WriteString(strconv.FormatInt(lp.Timestamp, 10))
	}

	return sb.String()
}

// isNumeric checks if a string represents a numeric value
func isNumeric(s string) bool {
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if b, err := strconv.ParseBool(s); err == nil {
		if b {
			return true // true is represented as 1
		}
		return true // false is represented as 0
	}
	return false
}

// New creates a new LineProtocol instance
func New(measurement string) *LineProtocol {
	return &LineProtocol{
		Measurement: measurement,
		Tags:        nil,
		Fields:      nil,
		fieldOrder:  make([]string, 0),
		tagOrder:    make([]string, 0),
	}
}
