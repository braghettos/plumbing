package coders

import (
	"testing"

	"github.com/krateoplatformops/plumbing/crdgen/schemas"
)

// These tests cover the webhookless nested-defaulting synthesis (krateoplatformops/roadmap#235):
// an object property with no default of its own gets a synthesized `+kubebuilder:default:={}` so the
// apiserver materializes it and the nested defaults below it are applied — but ONLY when the empty
// object stays schema-valid.

func obj(props map[string]*schemas.Type, required ...string) *schemas.Type {
	return &schemas.Type{Type: schemas.TypeList{"object"}, Properties: props, Required: required}
}

func str(def any) *schemas.Type {
	t := &schemas.Type{Type: schemas.TypeList{"string"}}
	if def != nil {
		t.Default = def
	}
	return t
}

func newCoder() *typesCoder {
	return &typesCoder{resolvedDefs: map[string]*schemas.Type{}}
}

func TestShouldSynthesizeEmptyDefault(t *testing.T) {
	co := newCoder()
	cases := []struct {
		name string
		in   *schemas.Type
		want bool
	}{
		{
			name: "optional parent with a defaulted leaf child -> synthesize",
			in:   obj(map[string]*schemas.Type{"tag": str("latest")}),
			want: true,
		},
		{
			name: "object with no defaulted descendant -> do not synthesize",
			in:   obj(map[string]*schemas.Type{"tag": str(nil)}),
			want: false,
		},
		{
			name: "defaulted child but a REQUIRED non-defaulted sibling -> unsafe, do not synthesize",
			in:   obj(map[string]*schemas.Type{"mandatory": str(nil), "opt": str("x")}, "mandatory"),
			want: false,
		},
		{
			name: "required child that itself carries a default -> still safe",
			in:   obj(map[string]*schemas.Type{"withdef": str("d")}, "withdef"),
			want: true,
		},
		{
			// Review blocker: required child OBJECT with no own default, plus a defaulted sibling (so
			// subtreeHasDefault of the parent is true). Because a required field is never
			// auto-materialized, `{}` would leave reqChild absent -> apiserver rejection. Must NOT
			// synthesize.
			name: "required child object with no own default -> must NOT synthesize (blocker)",
			in: obj(map[string]*schemas.Type{
				"reqChild": obj(map[string]*schemas.Type{"opt": str(nil)}),
				"sib":      str("d"),
			}, "reqChild"),
			want: false,
		},
		{
			// Even a required child object WITH a nested (but not own) default must not make the parent
			// synthesizable: we never auto-default the required child, so parent `{}` would still lack
			// it. "required means required" — the parent stays absent on omission.
			name: "required child object with only a nested default -> must NOT synthesize",
			in:   obj(map[string]*schemas.Type{"reqChild": obj(map[string]*schemas.Type{"leaf": str("z")})}, "reqChild"),
			want: false,
		},
		{
			name: "deeply nested default under two optional objects -> synthesize at the top",
			in:   obj(map[string]*schemas.Type{"mid": obj(map[string]*schemas.Type{"leaf": str("z")})}),
			want: true,
		},
		{
			name: "required nested object whose own required child lacks a default -> unsafe",
			in: obj(map[string]*schemas.Type{
				"child": obj(map[string]*schemas.Type{"need": str(nil), "opt": str("x")}, "need"),
			}, "child"),
			want: false,
		},
		{
			name: "not an object -> never synthesize",
			in:   str("latest"),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := co.shouldSynthesizeEmptyDefault(tc.in); got != tc.want {
				t.Errorf("shouldSynthesizeEmptyDefault = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSubtreeHasDefault(t *testing.T) {
	co := newCoder()
	// self default must NOT count; only descendants.
	selfOnly := str("x")
	if co.subtreeHasDefault(selfOnly) {
		t.Errorf("subtreeHasDefault must ignore the node's own default")
	}
	// default inside an array item counts.
	arr := &schemas.Type{Type: schemas.TypeList{"array"}, Items: str("d")}
	if !co.subtreeHasDefault(arr) {
		t.Errorf("subtreeHasDefault must see a default in array items")
	}
	// nested default counts.
	if !co.subtreeHasDefault(obj(map[string]*schemas.Type{"a": obj(map[string]*schemas.Type{"b": str("d")})})) {
		t.Errorf("subtreeHasDefault must see a transitively nested default")
	}
}

func TestEmptyObjectIsValid(t *testing.T) {
	co := newCoder()
	// no required fields -> {} valid.
	if !co.emptyObjectIsValid(obj(map[string]*schemas.Type{"a": str(nil)}), map[*schemas.Type]bool{}) {
		t.Errorf("object with only optional fields: {} should be valid")
	}
	// required scalar without default -> {} invalid.
	if co.emptyObjectIsValid(obj(map[string]*schemas.Type{"r": str(nil)}, "r"), map[*schemas.Type]bool{}) {
		t.Errorf("required non-defaulted scalar: {} should be invalid")
	}
	// required scalar WITH default -> {} valid.
	if !co.emptyObjectIsValid(obj(map[string]*schemas.Type{"r": str("d")}, "r"), map[*schemas.Type]bool{}) {
		t.Errorf("required defaulted scalar: {} should be valid")
	}
}

func TestSynthesizeWithRefResolution(t *testing.T) {
	// A $ref to a def whose subtree carries a default, and whose {} is valid, should synthesize.
	target := obj(map[string]*schemas.Type{"tag": str("latest")})
	co := &typesCoder{resolvedDefs: map[string]*schemas.Type{"Image": target}}
	ref := &schemas.Type{Ref: "#/$defs/Image"}
	if !co.isObjectSchema(ref) {
		t.Fatalf("isObjectSchema must resolve a $ref to an object")
	}
	if !co.shouldSynthesizeEmptyDefault(ref) {
		t.Errorf("shouldSynthesizeEmptyDefault must follow a $ref to find nested defaults")
	}
}
