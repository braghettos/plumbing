package utils

import (
	"bytes"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestLabelPostRenderFromSpec(t *testing.T) {
	tests := []struct {
		name            string
		mg              *unstructured.Unstructured
		pluralizer      pluralizer
		krateoNamespace string
		wantErr         bool
		expectedUID     types.UID
		expectedName    string
		expectedNs      string
	}{
		{
			name: "successful creation",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "example.com/v1",
					"kind":       "MyResource",
					"metadata": map[string]interface{}{
						"name":      "test-resource",
						"namespace": "test-namespace",
						"uid":       "test-uid-123",
					},
				},
			},
			pluralizer: &mockPluralizer{
				gvr: schema.GroupVersionResource{
					Group:    "example.com",
					Version:  "v1",
					Resource: "myresources",
				},
			},
			krateoNamespace: "krateo-system",
			wantErr:         false,
			expectedUID:     "test-uid-123",
			expectedName:    "test-resource",
			expectedNs:      "test-namespace",
		},
		{
			name: "pluralizer error",
			mg: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "example.com/v1",
					"kind":       "MyResource",
					"metadata": map[string]interface{}{
						"name":      "test-resource",
						"namespace": "test-namespace",
					},
				},
			},
			pluralizer: &mockPluralizer{
				err: errors.New("pluralizer error"),
			},
			krateoNamespace: "krateo-system",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LabelPostRenderFromSpec(tt.mg, tt.pluralizer, tt.krateoNamespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("LabelPostRenderFromSpec() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if result.UID != tt.expectedUID {
					t.Errorf("expected UID %s, got %s", tt.expectedUID, result.UID)
				}
				if result.CompositionName != tt.expectedName {
					t.Errorf("expected name %s, got %s", tt.expectedName, result.CompositionName)
				}
				if result.CompositionNamespace != tt.expectedNs {
					t.Errorf("expected namespace %s, got %s", tt.expectedNs, result.CompositionNamespace)
				}
				if result.KrateoNamespace != tt.krateoNamespace {
					t.Errorf("expected krateo namespace %s, got %s", tt.krateoNamespace, result.KrateoNamespace)
				}
			}
		})
	}
}

func TestLabelsPostRender_Run(t *testing.T) {
	tests := []struct {
		name           string
		postRender     *LabelsPostRender
		inputManifests string
		wantErr        bool
		checkLabels    map[string]string
	}{
		{
			name: "add labels to manifest without labels",
			postRender: &LabelsPostRender{
				UID: "test-uid",
				CompositionGVR: schema.GroupVersionResource{
					Group:    "example.com",
					Version:  "v1",
					Resource: "myresources",
				},
				CompositionName:      "my-composition",
				CompositionNamespace: "my-namespace",
				CompositionGVK: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1",
					Kind:    "MyResource",
				},
				KrateoNamespace: "krateo-system",
			},
			inputManifests: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
data:
  key: value
`,
			wantErr: false,
			checkLabels: map[string]string{
				"krateo.io/composition-id":                "test-uid",
				"krateo.io/composition-group":             "example.com",
				"krateo.io/composition-installed-version": "v1",
				"krateo.io/composition-resource":          "myresources",
				"krateo.io/composition-name":              "my-composition",
				"krateo.io/composition-namespace":         "my-namespace",
				"krateo.io/composition-kind":              "MyResource",
				"krateo.io/krateo-namespace":              "krateo-system",
			},
		},
		{
			name: "add labels to manifest with existing labels",
			postRender: &LabelsPostRender{
				UID: "test-uid",
				CompositionGVR: schema.GroupVersionResource{
					Group:    "example.com",
					Version:  "v1",
					Resource: "myresources",
				},
				CompositionName:      "my-composition",
				CompositionNamespace: "my-namespace",
				CompositionGVK: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1",
					Kind:    "MyResource",
				},
				KrateoNamespace: "krateo-system",
			},
			inputManifests: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
  labels:
    existing: label
data:
  key: value
`,
			wantErr: false,
			checkLabels: map[string]string{
				"existing":                                "label",
				"krateo.io/composition-id":                "test-uid",
				"krateo.io/composition-group":             "example.com",
				"krateo.io/composition-installed-version": "v1",
				"krateo.io/composition-resource":          "myresources",
				"krateo.io/composition-name":              "my-composition",
				"krateo.io/composition-namespace":         "my-namespace",
				"krateo.io/composition-kind":              "MyResource",
				"krateo.io/krateo-namespace":              "krateo-system",
			},
		},
		{
			name: "multiple manifests",
			postRender: &LabelsPostRender{
				UID: "test-uid",
				CompositionGVR: schema.GroupVersionResource{
					Group:    "example.com",
					Version:  "v1",
					Resource: "myresources",
				},
				CompositionName:      "my-composition",
				CompositionNamespace: "my-namespace",
				CompositionGVK: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1",
					Kind:    "MyResource",
				},
				KrateoNamespace: "krateo-system",
			},
			inputManifests: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap2
`,
			wantErr: false,
			checkLabels: map[string]string{
				"krateo.io/composition-id": "test-uid",
			},
		},
		{
			name: "invalid yaml",
			postRender: &LabelsPostRender{
				UID: "test-uid",
			},
			inputManifests: `invalid: yaml: content: [`,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := bytes.NewBufferString(tt.inputManifests)
			result, err := tt.postRender.Run(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				resultStr := result.String()
				for key, value := range tt.checkLabels {
					if !bytes.Contains([]byte(resultStr), []byte(key)) {
						t.Errorf("expected label key %s not found in result", key)
					}
					if !bytes.Contains([]byte(resultStr), []byte(value)) {
						t.Errorf("expected label value %s not found in result", value)
					}
				}
			}
		})
	}
}

func TestLabelsPostRender_Run_EmptyManifest(t *testing.T) {
	postRender := &LabelsPostRender{
		UID: "test-uid",
		CompositionGVR: schema.GroupVersionResource{
			Group:    "example.com",
			Version:  "v1",
			Resource: "myresources",
		},
		CompositionName:      "my-composition",
		CompositionNamespace: "my-namespace",
		CompositionGVK: schema.GroupVersionKind{
			Group:   "example.com",
			Version: "v1",
			Kind:    "MyResource",
		},
		KrateoNamespace: "krateo-system",
	}

	input := bytes.NewBufferString("")
	_, err := postRender.Run(input)
	if err != nil {
		t.Errorf("Run() with empty manifest should not error, got: %v", err)
	}
}
