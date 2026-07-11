// Package labels is the single shared home for the Krateo Composition label keys that couple
// core-provider and composition-dynamic-controller across repositories.
//
// These labels are the contract between the two controllers: core-provider stamps a composition
// instance with its owning CompositionDefinition (name/namespace/group/version/resource) and the
// served version it was written through (composition-version); the per-version dynamic controller
// selects instances by those same keys, and owner-scoped version migration re-stamps them. If the
// two sides ever disagreed on a byte, migration would silently select nothing and orphan instances.
// Declaring the keys ONCE here — imported (or re-exported) by both repos — makes that agreement a
// compile-time guarantee instead of a hand-maintained cross-repo contract.
package labels

const (
	// CompositionDefinitionNameLabel / CompositionDefinitionNamespaceLabel identify the
	// CompositionDefinition that owns a composition instance.
	CompositionDefinitionNameLabel      = "krateo.io/composition-definition-name"
	CompositionDefinitionNamespaceLabel = "krateo.io/composition-definition-namespace"
	// CompositionDefinitionGroupLabel / CompositionDefinitionVersionLabel / CompositionDefinitionResourceLabel
	// record the owning definition's generated GVR.
	CompositionDefinitionGroupLabel    = "krateo.io/composition-definition-group"
	CompositionDefinitionVersionLabel  = "krateo.io/composition-definition-version"
	CompositionDefinitionResourceLabel = "krateo.io/composition-definition-resource"
	// CompositionVersionLabel is the served version an instance was written through (stamped by the
	// version MutatingAdmissionPolicy / owner-scoped migration); it drives per-version listing and
	// migration.
	CompositionVersionLabel = "krateo.io/composition-version"
)
