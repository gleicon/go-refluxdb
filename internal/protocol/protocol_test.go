package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEmpty(t *testing.T) {
	_, err := Parse("")
	assert.Error(t, err)
}

func TestParseMissingField(t *testing.T) {
	_, err := Parse("cpu")
	assert.Error(t, err)
}

func TestParseBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		expected *LineProtocol
	}{
		{
			name:    "empty line",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing fields",
			input:   "cpu",
			wantErr: true,
		},
		{
			name:  "basic measurement with field",
			input: "cpu value=42",
			expected: &LineProtocol{
				Measurement: "cpu",
				Fields:      map[string]string{"value": "42"},
			},
		},
		{
			name:  "measurement with integer field",
			input: "cpu value=42i",
			expected: &LineProtocol{
				Measurement: "cpu",
				Fields:      map[string]string{"value": "42i"},
			},
		},
		{
			name:  "measurement with string field",
			input: "cpu value=\"42\"",
			expected: &LineProtocol{
				Measurement: "cpu",
				Fields:      map[string]string{"value": "\"42\""},
			},
		},
		{
			name:  "measurement with tag",
			input: "cpu,host=server1 value=42",
			expected: &LineProtocol{
				Measurement: "cpu",
				Tags:        map[string]string{"host": "server1"},
				Fields:      map[string]string{"value": "42"},
			},
		},
		{
			name:  "measurement with quoted tag value",
			input: "cpu,host=\"server 1\" value=42",
			expected: &LineProtocol{
				Measurement: "cpu",
				Tags:        map[string]string{"host": "server 1"},
				Fields:      map[string]string{"value": "42"},
			},
		},
		{
			name:  "measurement with timestamp",
			input: "cpu value=42 1465839830100400200",
			expected: &LineProtocol{
				Measurement: "cpu",
				Fields:      map[string]string{"value": "42"},
				Timestamp:   1465839830100400200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if got != nil {
				assert.Equal(t, tt.expected.Measurement, got.Measurement)
				assert.Equal(t, tt.expected.Tags, got.Tags)
				assert.Equal(t, tt.expected.Fields, got.Fields)
				assert.Equal(t, tt.expected.Timestamp, got.Timestamp)
			} else {
				assert.Nil(t, tt.expected)
			}
		})
	}
}

func TestSerialize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic measurement",
			input:    "cpu value=42",
			expected: "cpu value=42",
		},
		{
			name:     "measurement with tag",
			input:    "cpu,host=server1 value=42",
			expected: "cpu,host=server1 value=42",
		},
		{
			name:     "measurement with quoted tag value",
			input:    "cpu,host=\"server 1\" value=42",
			expected: "cpu,host=\"server 1\" value=42",
		},
		{
			name:     "measurement with multiple fields",
			input:    "cpu value=42i,temp=23.4",
			expected: "cpu value=42i,temp=23.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, err := Parse(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, proto.String())
		})
	}
}

func TestNewLineProtocol(t *testing.T) {
	proto := New("cpu")
	assert.NotNil(t, proto)
	assert.Equal(t, "cpu", proto.Measurement)
	assert.Nil(t, proto.Tags)
	assert.Nil(t, proto.Fields)
	assert.Equal(t, int64(0), proto.Timestamp)
}
