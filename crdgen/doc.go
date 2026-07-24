// Package crdgen transpiles a chart's JSON Schema (values.schema.json) into a Kubernetes
// CustomResourceDefinition via a direct JSON-Schema -> structural-OpenAPI-v3 walk. See
// crdgen/docs/ref-resolution-redesign.md.
package crdgen
