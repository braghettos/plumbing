package strings

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func StrSlice(v any) []string {
	switch v := v.(type) {
	case []string:
		return v
	case []any:
		b := make([]string, 0, len(v))
		for _, s := range v {
			if s != nil {
				b = append(b, StrVal(s))
			}
		}
		return b
	default:
		val := reflect.ValueOf(v)
		switch val.Kind() {
		case reflect.Array, reflect.Slice:
			l := val.Len()
			b := make([]string, 0, l)
			for i := 0; i < l; i++ {
				value := val.Index(i).Interface()
				if value != nil {
					b = append(b, StrVal(value))
				}
			}
			return b
		default:
			if v == nil {
				return []string{}
			}

			return []string{StrVal(v)}
		}
	}
}

func StrVal(v any) string {
	switch v := v.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	case []byte:
		return string(v)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = StrVal(e)
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ","))
	default:
		return fmt.Sprintf("%v", v)
	}
}

func DefaultValForKubebuilder(def any) string {
	switch v := def.(type) {
	case []any:
		// Empty array default must render as `{}` (empty list), NOT `{""}` (a one-element string
		// list) which yields an invalid CRD for object-item arrays (default.[0] must be object).
		// Elements formatted by type via formatMapValue (objects -> object literal, strings -> quoted).
		if len(v) == 0 {
			return "{}"
		}
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = formatMapValue(item)
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ","))
	case []string:
		if len(v) == 0 {
			return "{}"
		}
		return fmt.Sprintf("{%s}", `"`+strings.Join(v, `","`)+`"`)
	case map[string]any:
		// Sort by keys for a stable output
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s: %v", k, formatMapValue(v[k]))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func ExampleValForKubebuilder(ex any) string {
	switch v := ex.(type) {
	case []any:
		// Empty array default must render as `{}` (empty list), NOT `{""}` (a one-element string
		// list) which yields an invalid CRD for object-item arrays (default.[0] must be object).
		// Elements formatted by type via formatMapValue (objects -> object literal, strings -> quoted).
		if len(v) == 0 {
			return "{}"
		}
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = formatMapValue(item)
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ","))
	case []string:
		if len(v) == 0 {
			return "{}"
		}
		return fmt.Sprintf("{%s}", `"`+strings.Join(v, `","`)+`"`)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s: %v", k, formatMapValue(v[k]))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatMapValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case []any:
		strs := make([]string, len(val))
		for i, item := range val {
			strs[i] = fmt.Sprintf("%v", item)
		}
		return fmt.Sprintf("{%s}", strings.Join(strs, ","))
	case map[string]any:
		return DefaultValForKubebuilder(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func Join(v any, sep string) string {
	return strings.Join(StrSlice(v), sep)
}
