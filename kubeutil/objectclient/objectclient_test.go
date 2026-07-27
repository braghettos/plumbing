package objectclient

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

var roleGVR = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}

func newFakeClient(t *testing.T) *fake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		roleGVR: "RoleList",
	}
	return fake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
}

func newRole(ns, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("rbac.authorization.k8s.io/v1")
	obj.SetKind("Role")
	obj.SetNamespace(ns)
	obj.SetName(name)
	return obj
}

func TestApplyCreatesWhenAbsent(t *testing.T) {
	dyn := newFakeClient(t)
	ctx := context.Background()

	obj := newRole("ns1", "my-role")
	if err := Apply(ctx, dyn, roleGVR, obj, ApplyOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := Get(ctx, dyn, roleGVR, "ns1", "my-role")
	if err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if got.GetName() != "my-role" {
		t.Fatalf("unexpected name %q", got.GetName())
	}
}

func TestApplyUpdatesWhenPresent(t *testing.T) {
	dyn := newFakeClient(t)
	ctx := context.Background()

	obj := newRole("ns1", "my-role")
	if err := Apply(ctx, dyn, roleGVR, obj, ApplyOptions{}); err != nil {
		t.Fatalf("initial Apply: %v", err)
	}

	updated := newRole("ns1", "my-role")
	if err := unstructured.SetNestedField(updated.Object, "changed", "metadata", "labels", "marker"); err != nil {
		t.Fatalf("SetNestedField: %v", err)
	}
	if err := Apply(ctx, dyn, roleGVR, updated, ApplyOptions{}); err != nil {
		t.Fatalf("second Apply (update path): %v", err)
	}

	got, err := Get(ctx, dyn, roleGVR, "ns1", "my-role")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.GetLabels()["marker"] != "changed" {
		t.Fatalf("expected update to be applied, got labels %v", got.GetLabels())
	}
}

func TestGetNotFound(t *testing.T) {
	dyn := newFakeClient(t)
	_, err := Get(context.Background(), dyn, roleGVR, "ns1", "does-not-exist")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUninstallDeletesWhenPresent(t *testing.T) {
	dyn := newFakeClient(t)
	ctx := context.Background()

	obj := newRole("ns1", "my-role")
	if _, err := dyn.Resource(roleGVR).Namespace("ns1").Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seeding create: %v", err)
	}

	if err := Uninstall(ctx, dyn, roleGVR, "ns1", "my-role", UninstallOptions{}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	_, err := Get(ctx, dyn, roleGVR, "ns1", "my-role")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected object to be gone, got err %v", err)
	}
}

func TestUninstallIsIdempotentWhenAbsent(t *testing.T) {
	dyn := newFakeClient(t)
	if err := Uninstall(context.Background(), dyn, roleGVR, "ns1", "never-existed", UninstallOptions{}); err != nil {
		t.Fatalf("expected Uninstall of a missing object to succeed, got %v", err)
	}
}

func TestClusterScopedWhenNamespaceEmpty(t *testing.T) {
	dyn := newFakeClient(t)
	ctx := context.Background()

	obj := newRole("", "cluster-role")
	if err := Apply(ctx, dyn, roleGVR, obj, ApplyOptions{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := Get(ctx, dyn, roleGVR, "", "cluster-role")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.GetName() != "cluster-role" {
		t.Fatalf("unexpected name %q", got.GetName())
	}
}
