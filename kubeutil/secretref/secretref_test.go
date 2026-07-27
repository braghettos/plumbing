package secretref

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func newFakeClientWithSecret(t *testing.T, ns, name string, data map[string]string) *fake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		secretGVR: "SecretList",
	}
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)

	encoded := map[string]interface{}{}
	for k, v := range data {
		encoded[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}

	sec := &unstructured.Unstructured{}
	sec.SetAPIVersion("v1")
	sec.SetKind("Secret")
	sec.SetNamespace(ns)
	sec.SetName(name)
	if err := unstructured.SetNestedMap(sec.Object, encoded, "data"); err != nil {
		t.Fatalf("SetNestedMap: %v", err)
	}

	if _, err := dyn.Resource(secretGVR).Namespace(ns).Create(context.Background(), sec, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding secret: %v", err)
	}
	return dyn
}

func TestGetSecretValueDecodesBase64(t *testing.T) {
	dyn := newFakeClientWithSecret(t, "ns1", "creds", map[string]string{"password": "hunter2"})

	got, err := GetSecretValue(context.Background(), dyn, "ns1", "creds", "password")
	if err != nil {
		t.Fatalf("GetSecretValue: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("expected decoded value %q, got %q", "hunter2", got)
	}
}

func TestGetSecretValueMissingKey(t *testing.T) {
	dyn := newFakeClientWithSecret(t, "ns1", "creds", map[string]string{"password": "hunter2"})

	_, err := GetSecretValue(context.Background(), dyn, "ns1", "creds", "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for a missing key")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error message must not leak the secret value: %v", err)
	}
}

func TestGetSecretValueMissingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{secretGVR: "SecretList"})

	_, err := GetSecretValue(context.Background(), dyn, "ns1", "nope", "password")
	if err == nil {
		t.Fatal("expected an error for a missing secret")
	}
}
