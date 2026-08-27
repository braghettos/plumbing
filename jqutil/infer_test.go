package jqutil

import (
	"reflect"
	"testing"
)

func TestInferType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected any
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Boolean true",
			input:    "true",
			expected: true,
		},
		{
			name:     "Boolean false",
			input:    "false",
			expected: false,
		},
		{
			name:     "Null value",
			input:    "null",
			expected: nil,
		},
		{
			name:     "Nil value",
			input:    "nil",
			expected: nil,
		},
		{
			name:     "Integer within int32 range",
			input:    "123",
			expected: int32(123),
		},
		{
			name:     "Integer outside int32 range",
			input:    "2147483648",
			expected: int64(2147483648),
		},
		{
			name:     "Floating point number",
			input:    "3.14",
			expected: 3.14,
		},
		{
			name:     "Map with string keys and values",
			input:    "{\"foo\": \"bar\"}",
			expected: map[string]any{"foo": "bar"},
		},
		{
			// #27: nested numbers were returned as json.Number (only top-level scalars were
			// normalized); now they recurse to concrete Go types like the top-level case.
			name:  "Nested numbers in object are normalized (not json.Number)",
			input: `{"maximum": 32767, "meta": {"replicas": 3, "ratio": 1.5}}`,
			expected: map[string]any{
				"maximum": int32(32767),
				"meta":    map[string]any{"replicas": int32(3), "ratio": 1.5},
			},
		},
		{
			name:     "Nested numbers in array are normalized",
			input:    `[1, 2147483648, 3.14]`,
			expected: []any{int32(1), int64(2147483648), 3.14},
		},
		{
			name:     "Unquoted string",
			input:    "hello world",
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InferType(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("InferType(%q) = %v (%T), want %v (%T)", tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}
