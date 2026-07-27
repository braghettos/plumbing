// Package secretref reads a single key out of a Kubernetes Secret via a dynamic.Interface. It exists so
// callers whose reconciliation model is schema-unknown-at-compile-time (and therefore already carry a
// dynamic.Interface rather than a typed client.Client) do not need a second client type just to read
// Secret data.
package secretref

import (
	"context"
	"encoding/base64"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var secretGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}

// GetSecretValue reads and base64-decodes the value at key in the Secret named name in namespace ns.
// Error messages never include the secret's value, only its coordinates (namespace/name/key).
func GetSecretValue(ctx context.Context, dyn dynamic.Interface, ns, name, key string) (string, error) {
	sec, err := dyn.Resource(secretGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	data, _, err := unstructured.NestedMap(sec.Object, "data")
	if err != nil {
		return "", fmt.Errorf("reading data of secret %s/%s: %w", ns, name, err)
	}

	raw, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", key, ns, name)
	}

	encoded, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("value for key %q in secret %s/%s is not a string", key, ns, name)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decoding key %q in secret %s/%s: %w", key, ns, name, err)
	}
	return string(decoded), nil
}
