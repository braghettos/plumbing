package crdgen

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// End-to-end (real Generate + controller-gen) coverage for the webhookless nested-default synthesis
// (krateoplatformops/roadmap#235) and the guards added after the adversarial review. Complements the
// coders-package unit tests, which exercise the predicate in isolation but not the emission gating
// (optional-only) or the actual generated openAPIV3Schema.

func genSpecProps(t *testing.T, spec string) map[string]apiextensionsv1.JSONSchemaProps {
	t.Helper()
	out, err := Generate(Options{
		Group: "composition.krateo.io", Version: "v1-0-0", Kind: "NdT", Managed: true,
		SpecSchema: []byte(spec),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(out, &crd); err != nil {
		t.Fatalf("unmarshal CRD: %v\n%s", err, out)
	}
	if len(crd.Spec.Versions) == 0 || crd.Spec.Versions[0].Schema == nil {
		t.Fatalf("no schema in generated CRD:\n%s", out)
	}
	specSchema, ok := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	if !ok {
		t.Fatalf("no spec in openAPIV3Schema:\n%s", out)
	}
	return specSchema.Properties
}

func hasDefault(p apiextensionsv1.JSONSchemaProps) bool { return p.Default != nil }

// The canonical #235 case: optional parent, nested default -> parent gets default:{} and the leaf
// default is preserved.
func TestGen_NestedDefault_CanonicalSynthesizes(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","properties":{
		"image":{"type":"object","properties":{"tag":{"type":"string","default":"latest"}}}
	}}`)
	img, ok := props["image"]
	if !ok {
		t.Fatal("spec.image missing")
	}
	if !hasDefault(img) {
		t.Errorf("spec.image should carry synthesized default:{}, got none")
	}
	if tag := img.Properties["tag"]; tag.Default == nil {
		t.Errorf("spec.image.tag default not preserved")
	}
}

// Review Finding B: a REQUIRED object parent must NOT be synthesized (that would silently relax the
// author's `required`). The parent's nested default still applies when present, so nothing is lost.
func TestGen_NestedDefault_RequiredParentNotSynthesized(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","required":["image"],"properties":{
		"image":{"type":"object","properties":{"tag":{"type":"string","default":"latest"}}}
	}}`)
	if img := props["image"]; hasDefault(img) {
		t.Errorf("required spec.image must NOT get a synthesized default:{} (relaxes required), got %v", img.Default)
	}
}

// Review blocker: an optional parent with a defaulted sibling AND a required child-object that has no
// defaulted subtree must NOT be synthesized — else omitting the parent yields {} that lacks the
// required child and is rejected at admission.
func TestGen_NestedDefault_RequiredChildObjectNoSubtreeDefaultNotSynthesized(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","properties":{
		"cfg":{"type":"object","required":["reqChild"],"properties":{
			"reqChild":{"type":"object","properties":{"opt":{"type":"string"}}},
			"sib":{"type":"string","default":"d"}
		}}
	}}`)
	if cfg := props["cfg"]; hasDefault(cfg) {
		t.Errorf("cfg has a required child object with no defaults; must NOT get default:{} (blocker), got %v", cfg.Default)
	}
}

// "required means required": even when the required child object carries a nested default, the parent
// is NOT synthesized — a required field is never auto-materialized, so parent `{}` would still lack
// it. The parent simply stays absent on omission; if the user provides it, they must provide the
// required child, and that child's nested default then applies.
func TestGen_NestedDefault_RequiredChildObjectNotSynthesizedEvenWithNestedDefault(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","properties":{
		"cfg":{"type":"object","required":["reqChild"],"properties":{
			"reqChild":{"type":"object","properties":{"leaf":{"type":"string","default":"z"}}}
		}}
	}}`)
	if cfg := props["cfg"]; hasDefault(cfg) {
		t.Errorf("cfg has a required child object; must NOT be synthesized (required means required), got %v", cfg.Default)
	}
}

// Multi-level chain of OPTIONAL objects: default:{} must be synthesized at EVERY intermediate level
// so the apiserver materializes the whole path down to the defaulted leaf.
func TestGen_NestedDefault_OptionalChainCascades(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","properties":{
		"a":{"type":"object","properties":{
			"b":{"type":"object","properties":{
				"c":{"type":"string","default":"deep"}
			}}
		}}
	}}`)
	a, ok := props["a"]
	if !ok || !hasDefault(a) {
		t.Fatalf("spec.a should be synthesized; default=%v", a.Default)
	}
	b := a.Properties["b"]
	if b.Default == nil {
		t.Errorf("spec.a.b should also be synthesized (cascade)")
	}
	if c := b.Properties["c"]; c.Default == nil {
		t.Errorf("spec.a.b.c leaf default not preserved")
	}
}

// A parent whose only defaulted descendant lives under a required scalar-less path stays untouched;
// and a parent with no nested defaults at all must never be synthesized (no spurious churn).
func TestGen_NestedDefault_NoNestedDefaultNoSynthesis(t *testing.T) {
	props := genSpecProps(t, `{"type":"object","properties":{
		"plain":{"type":"object","properties":{"name":{"type":"string"}}}
	}}`)
	if p := props["plain"]; hasDefault(p) {
		t.Errorf("plain object with no nested defaults must NOT get default:{}, got %v", p.Default)
	}
}
