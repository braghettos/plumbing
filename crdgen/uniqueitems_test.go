package crdgen

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// genSpecProps transpiles a spec-schema and returns the generated CRD's spec.properties. A non-nil
// Generate error means the apiserver validation gate rejected the CRD — so these tests double as
// proof that the result is a valid CRD (in particular, free of the forbidden `uniqueItems: true`).
func genSpecProps(t *testing.T, specSchema string) map[string]apiextensionsv1.JSONSchemaProps {
	t.Helper()
	out, err := Generate(Options{
		Group:        "composition.krateo.io",
		Version:      "v0-1-0",
		Kind:         "Thing",
		SpecSchema:   []byte(specSchema),
		StatusSchema: []byte(defaultStatusSchema),
	})
	if err != nil {
		t.Fatalf("Generate failed (apiserver gate rejected the CRD): %v", err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(out, &crd); err != nil {
		t.Fatalf("unmarshal generated CRD: %v", err)
	}
	return crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties
}

// Scalar-item array: uniqueItems:true -> x-kubernetes-list-type: set (native, apiserver-enforced).
func TestUniqueItems_ScalarBecomesListTypeSet(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"tags":{"type":"array","uniqueItems":true,"items":{"type":"string"}}}}`)
	tags := p["tags"]
	if tags.XListType == nil || *tags.XListType != "set" {
		t.Fatalf("expected x-kubernetes-list-type: set, got %v", tags.XListType)
	}
	if tags.UniqueItems {
		t.Error("uniqueItems must not be carried into the CRD (forbidden)")
	}
	if len(tags.XValidations) != 0 {
		t.Error("scalar case should use list-type, not a CEL rule")
	}
}

// Object-item array that is BOUNDED (maxItems) -> a CEL uniqueness rule.
func TestUniqueItems_BoundedObjectBecomesCEL(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"rules":{"type":"array","uniqueItems":true,"maxItems":10,
		         "items":{"type":"object","properties":{"k":{"type":"string"}}}}}}`)
	rules := p["rules"]
	if rules.XListType != nil {
		t.Errorf("object items are not a set; got list-type %v", *rules.XListType)
	}
	if rules.UniqueItems {
		t.Error("uniqueItems must not be carried into the CRD")
	}
	if len(rules.XValidations) != 1 || !strings.Contains(rules.XValidations[0].Rule, "exists_one") {
		t.Fatalf("expected one CEL uniqueness rule, got %+v", rules.XValidations)
	}
}

// Object-item array that is UNBOUNDED -> dropped (a CEL rule would blow the cost budget).
func TestUniqueItems_UnboundedObjectDropped(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"rules":{"type":"array","uniqueItems":true,
		         "items":{"type":"object","properties":{"k":{"type":"string"}}}}}}`)
	rules := p["rules"]
	if rules.XListType != nil || len(rules.XValidations) != 0 || rules.UniqueItems {
		t.Fatalf("unbounded object array should drop uniqueItems entirely; got listType=%v validations=%d uniqueItems=%v",
			rules.XListType, len(rules.XValidations), rules.UniqueItems)
	}
}

// uniqueItems absent/false -> nothing added.
func TestUniqueItems_AbsentIsNoop(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"tags":{"type":"array","items":{"type":"string"}}}}`)
	tags := p["tags"]
	if tags.XListType != nil || tags.UniqueItems || len(tags.XValidations) != 0 {
		t.Fatalf("no uniqueItems -> no list-type/validation; got %+v", tags)
	}
}

// An explicitly-authored x-kubernetes-list-type is respected (not overridden to set).
func TestUniqueItems_ExplicitListTypeRespected(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"tags":{"type":"array","uniqueItems":true,"x-kubernetes-list-type":"atomic","items":{"type":"string"}}}}`)
	tags := p["tags"]
	if tags.XListType == nil || *tags.XListType != "atomic" {
		t.Fatalf("explicit list-type atomic must be preserved, got %v", tags.XListType)
	}
	if tags.UniqueItems {
		t.Error("uniqueItems must not be carried into the CRD")
	}
}

// Regression for the agentgateway-policies failure: a nested scope array of enum strings with
// uniqueItems now yields a valid CRD (list-type: set) instead of "uniqueItems: Forbidden".
func TestUniqueItems_AgentgatewayScopeRegression(t *testing.T) {
	p := genSpecProps(t, `{"type":"object","properties":{
		"componentValues":{"type":"object","properties":{
			"guardrails":{"type":"object","properties":{
				"scope":{"type":"array","uniqueItems":true,"items":{"type":"string","enum":["PII","REGEX"]}}}}}}}}`)
	scope := p["componentValues"].Properties["guardrails"].Properties["scope"]
	if scope.XListType == nil || *scope.XListType != "set" {
		t.Fatalf("scope array should become list-type: set, got %v", scope.XListType)
	}
	if scope.UniqueItems {
		t.Error("uniqueItems must not be carried into the CRD")
	}
}
