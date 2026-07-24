package helm

import (
	"context"
	"testing"
	"time"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapix "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeInvalidator struct {
	ch chan struct{}
}

func newFakeInvalidator() *fakeInvalidator {
	return &fakeInvalidator{ch: make(chan struct{}, 10)}
}

func (f *fakeInvalidator) Reset() {
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

func (f *fakeInvalidator) WaitCall(timeout time.Duration) bool {
	select {
	case <-f.ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestStartCRDInformer_Add_Update_Delete_InvokeInvalidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	crdInformer := NewCRDInformer(1*time.Minute, fakeClient, invalidator, func(format string, v ...interface{}) {
	})

	if err := crdInformer.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	// Let the informer's initial List complete and its Watch establish before mutating; otherwise the
	// Create/Update/Delete race watch setup and events are missed (the fake client does not replay
	// missed events). A real apiserver + long-lived informer does not hit this in production.
	time.Sleep(500 * time.Millisecond)

	// create a CRD
	crd := &apixv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tests.example.com",
		},
		Spec: apixv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apixv1.CustomResourceDefinitionNames{
				Plural:   "tests",
				Singular: "test",
				Kind:     "Test",
			},
			Scope: apixv1.NamespaceScoped,
			Versions: []apixv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
				},
			},
		},
	}

	created, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create crd: %v", err)
	}

	// wait for invalidate triggered by Add
	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD add")
	}

	// Update from the CREATED object (carrying its server-assigned resourceVersion) and bump it, so
	// the informer observes a distinct Modified event — the fake client does not auto-bump RV, and
	// without a changed RV the reflector never delivers the update (a real apiserver always bumps it).
	updated := created.DeepCopy()
	updated.Spec.PreserveUnknownFields = true
	updated.ResourceVersion = "2"
	_, err = fakeClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update crd: %v", err)
	}

	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD update")
	}

	// Delete
	if err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete crd: %v", err)
	}

	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD delete")
	}
}
