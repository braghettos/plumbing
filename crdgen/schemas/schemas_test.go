package schemas_test

import (
	"encoding/json"
	"testing"

	"github.com/krateo-platformops/plumbing/crdgen/schemas"
	"github.com/stretchr/testify/require"
)

func TestSchema_UnmarshalDefinitions(t *testing.T) {
	jsonData := []byte(`
{
  "$id": "https://example.com/test.schema.json",
  "$defs": {
    "MyType": {
      "type": "object",
      "properties": {
        "name": { "type": "string" }
      },
      "required": ["name"]
    }
  },
  "type": "object",
  "properties": {
    "myField": { "$ref": "#/$defs/MyType" }
  }
}
`)

	var s schemas.Schema
	if err := json.Unmarshal(jsonData, &s); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	if s.Definitions == nil {
		t.Fatal("Definitions should not be nil")
	}

	if _, ok := s.Definitions["MyType"]; !ok {
		t.Fatal("MyType should be present in Definitions")
	}

	// Controlla che la proprietà myField punti correttamente al $ref
	ref := s.Properties["myField"].Ref
	if ref != "#/$defs/MyType" {
		t.Fatalf("expected myField $ref to be '#/$defs/MyType', got '%s'", ref)
	}
}

func TestUnmarshalJSONSchema(t *testing.T) {
	jsonData := `{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"type": "object",
		"properties": {
			"fromRepo": {
				"type": "object",
				"properties": {
					"krateoIgnorePath": {"type": "string","default":"/"},
					"url": {"type": "string"},
					"path": {"type": "string","default":"/","nullable":true},
					"branch": {"type": "string"},
					"secretRef": {"$ref": "#/$defs/SecretKeySelector"},
					"usernameRef": {"$ref": "#/$defs/SecretKeySelector","nullable":true},
					"authMethod": {"type": "string","enum":["generic","bearer","cookiefile"],"default":"generic","nullable":true},
					"cloneFromBranch": {"type": "string","nullable":true}
				},
				"required":["url","branch"]
			},
			"toRepo": {
				"type": "object",
				"properties": {
					"url": {"type": "string"},
					"path": {"type": "string","default":"/","nullable":true},
					"branch": {"type": "string"},
					"secretRef": {"$ref": "#/$defs/SecretKeySelector"},
					"usernameRef": {"$ref": "#/$defs/SecretKeySelector","nullable":true},
					"authMethod": {"type": "string","enum":["generic","bearer","cookiefile"],"default":"generic","nullable":true},
					"cloneFromBranch": {"type": "string","nullable":true}
				},
				"required":["url","branch"]
			}
		},
		"$defs": {
			"Reference": {"type":"object","properties":{"name":{"type":"string"},"namespace":{"type":"string"}},"required":["name","namespace"]},
			"EnvSelector": {"type":"object","properties":{"name":{"type":"string"}},"required":["name"]},
			"SecretKeySelector": {"allOf":[{"$ref":"#/$defs/Reference"},{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}]},
			"ConfigMapKeySelector": {"allOf":[{"$ref":"#/$defs/Reference"},{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}]}
		}
	}`

	var schema schemas.Schema
	err := json.Unmarshal([]byte(jsonData), &schema)
	require.NoError(t, err, "failed to unmarshal schema")

	// Controlla che Definitions sia popolato
	require.NotNil(t, schema.Definitions, "Definitions should not be nil")
	require.Contains(t, schema.Definitions, "Reference", "Definitions should contain 'Reference'")
	require.Contains(t, schema.Definitions, "SecretKeySelector", "Definitions should contain 'SecretKeySelector'")
	require.Contains(t, schema.Definitions, "ConfigMapKeySelector", "Definitions should contain 'ConfigMapKeySelector'")
}
