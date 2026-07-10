package helm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// mapperServing builds a RESTMapper that resolves ONLY the given GVKs — everything else is
// "unserved" (RESTMapping returns an error), simulating a CRD version pruned by an out-of-band
// composition bump.
func mapperServing(gvks ...schema.GroupVersionKind) apimeta.RESTMapper {
	m := apimeta.NewDefaultRESTMapper(nil)
	for _, gvk := range gvks {
		m.Add(gvk, apimeta.RESTScopeNamespace)
	}
	return m
}

func TestFilterUnmappableManifestDocs(t *testing.T) {
	const manifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep-me
  namespace: default
data:
  x: "1"
---
apiVersion: composition.krateo.io/v1-5-1
kind: Portal
metadata:
  name: portal
  namespace: krateo-system
---
apiVersion: v1
kind: Secret
metadata:
  name: keep-secret
  namespace: default
`
	// The cluster serves core v1 ConfigMap + Secret, but Portal only at v1-5-9 (v1-5-1 pruned).
	mapper := mapperServing(
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
		schema.GroupVersionKind{Group: "composition.krateo.io", Version: "v1-5-9", Kind: "Portal"},
	)

	kept, dropped := filterUnmappableManifestDocs(manifest, mapper)

	// The unserved Portal/v1-5-1 is dropped; the two core resources survive.
	assert.Equal(t, []string{"composition.krateo.io/v1-5-1, Kind=Portal"}, dropped)
	assert.NotContains(t, kept, "kind: Portal", "the unserved Portal must be dropped")
	assert.Contains(t, kept, "keep-me", "a served ConfigMap must be kept")
	assert.Contains(t, kept, "keep-secret", "a served Secret must be kept")
}

func TestFilterUnmappableManifestDocs_AllServed_NoOp(t *testing.T) {
	const manifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: only
  namespace: default
`
	mapper := mapperServing(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

	kept, dropped := filterUnmappableManifestDocs(manifest, mapper)

	assert.Nil(t, dropped, "nothing to drop when every GVK is served")
	// No-op returns the original manifest verbatim.
	assert.Equal(t, manifest, kept)
}

func TestFilterUnmappableManifestDocs_UnparseableKept(t *testing.T) {
	// A doc with no apiVersion/kind header must be KEPT, never dropped blindly.
	const manifest = `# just a comment, no gvk
foo: bar
---
apiVersion: example.test/v1
kind: Widget
metadata:
  name: ghost
`
	mapper := mapperServing() // serves nothing

	kept, dropped := filterUnmappableManifestDocs(manifest, mapper)

	assert.Equal(t, []string{"example.test/v1, Kind=Widget"}, dropped)
	assert.True(t, strings.Contains(kept, "foo: bar"), "the header-less doc must be kept")
	assert.NotContains(t, kept, "Widget")
}
