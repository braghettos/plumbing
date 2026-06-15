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

// DefaultValForKubebuilder / ExampleValForKubebuilder render a schema default/example
// as a controller-gen marker argument (+kubebuilder:default:= / example:=).
func DefaultValForKubebuilder(def any) string { return markerVal(def) }
func ExampleValForKubebuilder(ex any) string  { return markerVal(ex) }

// markerVal renders v as a controller-gen marker argument. controller-gen's marker DSL
// can express scalars and scalar LISTS ({a,b}) only — it has NO syntax for object or
// object-array defaults ({k:v} is parsed as a scalar list start and fails; an empty
// array default must be {} not {""}). So markerVal returns "" for anything controller-gen
// cannot express (objects, arrays-containing-objects), and the caller OMITS the marker.
// This is the fix for the array/object-default CRD-gen failure (see the core-provider
// crdgen-array-default fix shipped as braghettos/plumbing v1.7.6, here on the v1.6.x line).
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
			return "{}" // empty array default -> {} (NOT {""})
		}
		parts := make([]string, 0, len(x))
		for _, e := range x {
			m := markerVal(e)
			if m == "" { // an element controller-gen can't express -> omit the whole default
				return ""
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

func Join(v any, sep string) string {
	return strings.Join(StrSlice(v), sep)
}
