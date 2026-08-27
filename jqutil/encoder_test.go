package jqutil

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncoder_encodeBool(t *testing.T) {
	tests := []struct {
		value    bool
		expected string
	}{
		{true, "true"},
		{false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

// #27: a json.Number (as produced by gojq's fromjson) must encode to its verbatim numeric text
// instead of panicking through the `default` branch.
func TestEncoder_encodeJSONNumber(t *testing.T) {
	tests := []struct {
		name     string
		value    json.Number
		expected string
	}{
		{"int", json.Number("32767"), "32767"},
		{"negative", json.Number("-456"), "-456"},
		{"float", json.Number("3.14"), "3.14"},
		{"exponent", json.Number("1e5"), "1e5"},
		// > 2^53: verbatim text preserves precision a float64 round-trip would destroy.
		{"large int precision", json.Number("9007199254740993"), "9007199254740993"},
		// Degenerate (never produced by gojq): degrade to null, never emit invalid JSON.
		{"empty degrades to null", json.Number(""), "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

// #27: json.Number leaves nested inside objects/arrays must also encode (encode recurses), since a
// fromjson-parsed schema carries them at every numeric position.
func TestEncoder_encodeNestedJSONNumber(t *testing.T) {
	v := map[string]any{
		"maximum": json.Number("32767"),
		"nested":  map[string]any{"replicas": json.Number("3")},
		"list":    []any{json.Number("1"), json.Number("2")},
	}
	encoder := newEncoder(false, 0)
	err := encoder.encode(v)
	assert.NoError(t, err)
	// encodeObject sorts keys.
	assert.Equal(t, `{"list":[1,2],"maximum":32767,"nested":{"replicas":3}}`, encoder.w.String())
}

func TestEncoder_encodeInt(t *testing.T) {
	tests := []struct {
		value    int
		expected string
	}{
		{123, "123"},
		{-456, "-456"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_encodeFloat64(t *testing.T) {
	tests := []struct {
		value    float64
		expected string
	}{
		{3.14159, "3.14159"},
		{math.NaN(), "null"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			encoder.encode(tt.value)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_encodeBigInt(t *testing.T) {
	tests := []struct {
		value    *big.Int
		expected string
	}{
		{big.NewInt(123456789), "123456789"},
		{big.NewInt(-987654321), "-987654321"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_encodeString(t *testing.T) {
	tests := []struct {
		value    string
		expected string
	}{
		{"hello", "\"hello\""},
		{"\"escaped\"", "\"\\\"escaped\\\"\""},
		{"backslash\\test", "\"backslash\\\\test\""},
		{"new\nline", "\"new\\nline\""},
		{"tab\tcharacter", "\"tab\\tcharacter\""},
		{"2025-06-03T11:37:32Z", "\"2025-06-03T11:37:32Z\""},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_encodeArray(t *testing.T) {
	tests := []struct {
		value    []any
		expected string
	}{
		{[]any{1, 2, 3}, "[1,2,3]"},
		{[]any{"a", "b", "c"}, "[\"a\",\"b\",\"c\"]"},
		{[]any{true, false}, "[true,false]"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_encodeObject(t *testing.T) {
	tests := []struct {
		value    map[string]any
		expected string
	}{
		{
			map[string]any{"name": "John", "age": 30},
			"{\"age\":30,\"name\":\"John\"}",
		},
		{
			map[string]any{"active": true, "score": 100},
			"{\"active\":true,\"score\":100}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			encoder := newEncoder(false, 0)
			err := encoder.encode(tt.value)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, encoder.w.String())
		})
	}
}

func TestEncoder_flush(t *testing.T) {
	// Simula un writer di output
	var buf bytes.Buffer
	encoder := newEncoder(false, 0)
	encoder.out = &buf

	// Scrivi qualcosa
	err := encoder.encode("Hello")
	assert.NoError(t, err)

	// Fai il flush
	err = encoder.flush()
	assert.NoError(t, err)

	// Verifica che il buffer contenga i dati corretti
	assert.Equal(t, "\"Hello\"", buf.String())
}
