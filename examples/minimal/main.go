// Command minimal is the runnable example for the plumbing library: it feeds a
// chart-style values.schema.json (JSON Schema) to crdgen.Generate and prints the
// resulting CustomResourceDefinition YAML — the exact transformation core-provider
// performs for every CompositionDefinition. It needs no cluster and no network.
//
// Run from the repo root:
//
//	go run ./examples/minimal
package main

import (
	"fmt"
	"log"

	"github.com/krateo-platformops/plumbing/crdgen"
)

// specSchema is a minimal chart values.schema.json: two typed fields, one with a
// default, one required.
const specSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["host"],
  "properties": {
    "host": {
      "type": "string",
      "description": "Hostname the app is served on."
    },
    "replicas": {
      "type": "integer",
      "default": 1,
      "description": "Number of replicas."
    }
  }
}`

func main() {
	crd, err := crdgen.Generate(crdgen.Options{
		Group:      "examples.krateo.io",
		Version:    crdgen.NormalizeVersionName("1.0.0"), // "v1-0-0"
		Kind:       "DummyApp",
		Categories: []string{"compositions"},
		SpecSchema: []byte(specSchema),
		Managed:    true, // adds the conditioned status subresource
	})
	if err != nil {
		log.Fatalf("crdgen: %v", err)
	}
	fmt.Print(string(crd))
}
