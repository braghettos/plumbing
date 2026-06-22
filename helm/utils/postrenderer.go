package utils

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kustomize/kyaml/kio"
)

type LabelsPostRender struct {
	UID                  types.UID
	CompositionGVR       schema.GroupVersionResource
	CompositionName      string
	CompositionNamespace string
	CompositionGVK       schema.GroupVersionKind
	KrateoNamespace      string
	// Traceparent/Tracestate, when set, are stamped as krateo.io/traceparent (+ tracestate)
	// annotations on every rendered child manifest, so the controller that reconciles the
	// child continues the distributed trace across the compose-of-compositions tree.
	Traceparent string
	Tracestate  string
}

func LabelPostRenderFromSpec(mg *unstructured.Unstructured, pluralizer pluralizer, krateoNamespace string) (*LabelsPostRender, error) {
	gvk := mg.GroupVersionKind()
	gvr, err := pluralizer.GVKtoGVR(gvk)
	if err != nil {
		return nil, fmt.Errorf("failed to get GVR from GVK: %w", err)
	}

	return &LabelsPostRender{
		UID:                  mg.GetUID(),
		CompositionGVR:       gvr,
		CompositionName:      mg.GetName(),
		CompositionNamespace: mg.GetNamespace(),
		CompositionGVK:       gvk,
		KrateoNamespace:      krateoNamespace,
	}, nil
}

// WithTraceparent sets the W3C traceparent (+ optional tracestate) to stamp as the
// krateo.io/traceparent annotation on every rendered child manifest. Returns r for chaining.
// Additive: callers that don't set it keep the existing label-only behavior.
func (r *LabelsPostRender) WithTraceparent(traceparent, tracestate string) *LabelsPostRender {
	r.Traceparent = traceparent
	r.Tracestate = tracestate
	return r
}

func (r *LabelsPostRender) Run(renderedManifests *bytes.Buffer) (modifiedManifests *bytes.Buffer, err error) {
	nodes, err := kio.FromBytes(renderedManifests.Bytes())
	if err != nil {
		return renderedManifests, fmt.Errorf("failed to parse rendered manifests: %w", err)
	}
	for _, v := range nodes {
		labels := v.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		// your labels
		labels["krateo.io/composition-id"] = string(r.UID)
		labels["krateo.io/composition-group"] = r.CompositionGVR.Group
		labels["krateo.io/composition-installed-version"] = r.CompositionGVR.Version
		labels["krateo.io/composition-resource"] = r.CompositionGVR.Resource
		labels["krateo.io/composition-name"] = r.CompositionName
		labels["krateo.io/composition-namespace"] = r.CompositionNamespace
		labels["krateo.io/composition-kind"] = r.CompositionGVK.Kind
		labels["krateo.io/krateo-namespace"] = r.KrateoNamespace
		v.SetLabels(labels)

		if r.Traceparent != "" {
			annotations := v.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["krateo.io/traceparent"] = r.Traceparent
			if r.Tracestate != "" {
				annotations["krateo.io/tracestate"] = r.Tracestate
			}
			v.SetAnnotations(annotations)
		}
	}

	str, err := kio.StringAll(nodes)
	if err != nil {
		return renderedManifests, fmt.Errorf("failed to convert nodes to string: %w", err)
	}

	return bytes.NewBufferString(str), nil
}
