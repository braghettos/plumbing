package schemas_test

import (
	"encoding/json"
	"testing"

	"github.com/krateo-platformops/plumbing/crdgen/schemas"
)

func TestAllOfMerge_SecretKeySelector(t *testing.T) {
	// Schema minimale con Reference + SecretKeySelector
	raw := []byte(`{
		"$defs": {
			"Reference": {
				"type": "object",
				"properties": {
					"name": { "type": "string" },
					"namespace": { "type": "string" }
				},
				"required": ["name", "namespace"]
			},
			"SecretKeySelector": {
				"allOf": [
					{ "$ref": "#/$defs/Reference" },
					{
						"type": "object",
						"properties": {
							"key": { "type": "string" }
						},
						"required": ["key"]
					}
				]
			}
		}
	}`)

	var s schemas.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	// Prendiamo la definizione di SecretKeySelector
	sel := s.Definitions["SecretKeySelector"]
	if sel == nil {
		t.Fatalf("SecretKeySelector not found")
	}

	// Risolviamo allOf manualmente
	merged, err := schemas.AllOf(sel.AllOf, s.Definitions)
	if err != nil {
		t.Fatalf("AllOf merge failed: %v", err)
	}

	// Controllo che le proprietà mergiate siano giuste
	props := merged.Properties
	if _, ok := props["name"]; !ok {
		t.Errorf("expected property 'name', not found")
	}
	if _, ok := props["namespace"]; !ok {
		t.Errorf("expected property 'namespace', not found")
	}
	if _, ok := props["key"]; !ok {
		t.Errorf("expected property 'key', not found")
	}

	// Controllo required
	expectedReq := []string{"name", "namespace", "key"}
	for _, req := range expectedReq {
		found := false
		for _, r := range merged.Required {
			if r == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected required %q, not found in %v", req, merged.Required)
		}
	}
}
