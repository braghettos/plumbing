package schemas

import (
	"strings"
	"testing"
)

// TestCollectAllDefinitions exercises the top-level $defs harvesting the crdgen transpiler relies on
// to build its JSON-pointer ref table. Chart values.schema.json documents (loft/vcluster included)
// declare a flat top-level $defs table, which is what this covers. Kept self-contained (no fixture).
func TestCollectAllDefinitions(t *testing.T) {
	const src = `{
	  "type": "object",
	  "properties": {
	    "a": { "$ref": "#/$defs/Reference" },
	    "b": { "$ref": "#/$defs/EnvSelector" }
	  },
	  "$defs": {
	    "Reference":            { "type": "object" },
	    "EnvSelector":          { "type": "object" },
	    "SecretKeySelector":    { "type": "object" },
	    "ConfigMapKeySelector": { "type": "object" }
	  }
	}`

	schema, err := FromJSONReader(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	defs := CollectAllDefinitions(schema)

	for _, name := range []string{"Reference", "EnvSelector", "SecretKeySelector", "ConfigMapKeySelector"} {
		if _, ok := defs[name]; !ok {
			t.Errorf("definition %q not collected (nested $defs missed?); got %v", name, keys(defs))
		}
	}
	if len(defs) != 4 {
		t.Errorf("expected 4 definitions, got %d: %v", len(defs), keys(defs))
	}
}

func keys(m map[string]*Type) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
