package utils

import (
	"fmt"

	"github.com/krateoplatformops/plumbing/maps"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	AnnotationKeyReconciliationGracefullyPaused = "krateo.io/gracefully-paused"
)

type Values map[string]any

type pluralizer interface {
	GVKtoGVR(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error)
}

func ValuesFromSpec(un *unstructured.Unstructured) (Values, error) {
	if un == nil {
		return nil, nil
	}

	spec, ok, err := maps.NestedMap(un.UnstructuredContent(), "spec")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("spec field not found in unstructured object")
	}

	return spec, nil
}

func (v Values) InjectGlobalValues(mg *unstructured.Unstructured, pluralizer pluralizer, krateoNamespace string) error {
	gvk := mg.GroupVersionKind()
	gvr, err := pluralizer.GVKtoGVR(gvk)
	if err != nil {
		return err
	}
	gracefullyPaused := "false"
	if val, found := mg.GetAnnotations()[AnnotationKeyReconciliationGracefullyPaused]; found && val == "true" {
		gracefullyPaused = "true"
	}

	gv := map[string]any{
		"compositionName":             mg.GetName(),
		"compositionNamespace":        mg.GetNamespace(),
		"compositionId":               string(mg.GetUID()),
		"compositionApiVersion":       mg.GetAPIVersion(),
		"compositionGroup":            gvr.Group,
		"compositionKind":             gvk.Kind,
		"compositionResource":         gvr.Resource,
		"compositionInstalledVersion": gvr.Version,
		"gracefullyPaused":            gracefullyPaused,
		"krateoNamespace":             krateoNamespace,
	}

	err = maps.SetNestedField(v, gv, "global")
	if err != nil {
		return fmt.Errorf("failed to set global values: %w", err)
	}
	return nil
}
