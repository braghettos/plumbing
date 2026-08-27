package jqutil

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// InferType attempts to infer and convert a string value to its most appropriate Go type.
// It supports primitive types (bool, int32, int64, float64, string), as well as
// structured types commonly found in Kubernetes configurations (map[string]any and []any).
// The function first tries to parse the input as JSON. If that fails, it falls back to
// custom parsing logic for booleans, nil/null, integers, and floats.
// If no conversion is possible, the original string is returned.
func InferType(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var jsonVal any
	if err := decoder.Decode(&jsonVal); err == nil {
		return normalizeNumbers(jsonVal)
	}

	if strings.EqualFold(value, "true") {
		return true
	}
	if strings.EqualFold(value, "false") {
		return false
	}

	if strings.EqualFold(value, "null") || strings.EqualFold(value, "nil") {
		return nil
	}

	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		if intVal >= math.MinInt32 && intVal <= math.MaxInt32 {
			return int32(intVal)
		}
		return intVal
	}

	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		if floatVal == math.Trunc(floatVal) {
			if floatVal >= math.MinInt64 && floatVal <= math.MaxInt64 {
				return int64(floatVal)
			}
		}
		return floatVal
	}

	return value
}

// normalizeNumbers converts every json.Number in a value decoded by a UseNumber() decoder to a
// concrete Go numeric type, recursing into objects and arrays. InferType previously normalized only
// a TOP-LEVEL scalar number and returned nested numbers as json.Number; those leaked to callers and
// — when re-encoded by jqutil's encoder — panicked (see jqutil/encoder.go). Doing the conversion in
// one recursive place makes top-level and nested numbers behave identically.
func normalizeNumbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		return numberToGo(v)
	case map[string]any:
		for k, val := range v {
			v[k] = normalizeNumbers(val)
		}
		return v
	case []any:
		for i, val := range v {
			v[i] = normalizeNumbers(val)
		}
		return v
	default:
		return v
	}
}

// numberToGo converts a json.Number to int32 (when it fits), int64, or float64, matching the type
// selection InferType uses for a bare scalar number. A Number that parses as neither (never produced
// by a valid JSON decode) degrades to its literal string.
func numberToGo(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		if i >= math.MinInt32 && i <= math.MaxInt32 {
			return int32(i)
		}
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}
