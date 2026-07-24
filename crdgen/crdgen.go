package crdgen

// Options configures CRD generation. SpecSchema/StatusSchema are JSON Schema documents (a chart's
// values.schema.json and an optional status schema). Managed adds the crossplane ConditionedStatus
// to the status subresource.
type Options struct {
	Group        string
	Version      string
	Kind         string
	Categories   []string
	SpecSchema   []byte
	StatusSchema []byte
	Managed      bool
}

// Generate transpiles the Options' JSON Schemas into a Kubernetes CustomResourceDefinition (YAML).
//
// It uses the direct JSON-Schema -> structural-OpenAPI-v3 transpiler (transpile.go): $refs are
// inlined by JSON pointer, cycles broken with x-kubernetes-preserve-unknown-fields, map value
// schemas preserved, and the result is gated on the API server's own CRD validation so the output
// is always structurally valid. See crdgen/docs/ref-resolution-redesign.md.
func Generate(opts Options) ([]byte, error) {
	return generateDirect(opts)
}
