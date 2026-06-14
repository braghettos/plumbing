package strings

import (
	"encoding/json"
	"fmt"
	"reflect"
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

// DefaultValForKubebuilder renders a JSON-Schema default into a +kubebuilder:default:= marker
// value, OR returns "" when controller-gen's marker DSL cannot express the value (objects and
// arrays-containing-objects). controller-gen parses `{...}` as a LIST of scalars; it has no syntax
// for object/struct defaults, so emitting one produces an invalid CRD. Callers must skip the marker
// when this returns "". The source values.schema.json keeps the full default regardless — this only
// governs what lands in the generated CRD.
func DefaultValForKubebuilder(def any) string { return markerVal(def) }

// ExampleValForKubebuilder mirrors DefaultValForKubebuilder for +kubebuilder:example:= markers.
func ExampleValForKubebuilder(ex any) string { return markerVal(ex) }

// markerVal returns a controller-gen-DSL value, or "" if unexpressible.
func markerVal(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return fmt.Sprintf("%q", x)
	case bool:
		return fmt.Sprintf("%v", x)
	case json.Number:
		return x.String()
	case float64, float32, int, int32, int64:
		return fmt.Sprintf("%v", x)
	case []any:
		if len(x) == 0 {
			return "{}" // empty list default
		}
		parts := make([]string, 0, len(x))
		for _, e := range x {
			m := markerVal(e)
			if m == "" {
				return "" // contains an unexpressible element (e.g. an object) -> omit whole default
			}
			parts = append(parts, m)
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []string:
		if len(x) == 0 {
			return "{}"
		}
		return `{"` + strings.Join(x, `","`) + `"}`
	case map[string]any:
		return "" // controller-gen has no object/struct default syntax
	default:
		return ""
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
