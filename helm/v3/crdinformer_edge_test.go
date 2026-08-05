package helm

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapix "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newEdgeTestCRD builds a minimal, valid CRD used across the edge-case tests below.
func newEdgeTestCRD(name, group, plural, kind string) *apixv1.CustomResourceDefinition {
	return &apixv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apixv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apixv1.CustomResourceDefinitionNames{
				Plural:   plural,
				Singular: strings.TrimSuffix(plural, "s"),
				Kind:     kind,
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
}

// TestCRDInformer_UpdateUnchangedSpec_NoReset asserts that an Update event whose Spec is
// reflect.DeepEqual to the prior object does NOT invalidate the discovery cache. The informer's
// UpdateFunc short-circuits on an unchanged Spec, so invalidator.Reset must not be called even
// though a Modified event is delivered (RV bumped so the reflector actually delivers it).
func TestCRDInformer_UpdateUnchangedSpec_NoReset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	inf := NewCRDInformer(1*time.Minute, fakeClient, invalidator, func(format string, v ...interface{}) {})
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	// Let the informer's initial List complete and its Watch establish before mutating; otherwise the
	// Create/Update race watch setup and events are missed (mirrors the 500ms note in the existing test).
	time.Sleep(500 * time.Millisecond)

	crd := newEdgeTestCRD("nochange.example.com", "example.com", "nochanges", "NoChange")
	created, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create crd: %v", err)
	}
	// Add fires => drain the one expected Reset.
	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD add")
	}

	// Update the object but keep the Spec byte-identical; only bump RV (and a metadata-only change) so
	// the reflector delivers a Modified event whose Spec is DeepEqual to the old one.
	updated := created.DeepCopy()
	updated.ResourceVersion = "2"
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	updated.Labels["touched"] = "true"
	if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update crd: %v", err)
	}

	// The UpdateFunc must NOT call Reset for an unchanged Spec.
	if invalidator.WaitCall(1 * time.Second) {
		t.Fatalf("invalidate WAS called on an unchanged-Spec update; expected no Reset")
	}
}

// TestCRDInformer_UpdateChangedSpec_Reset asserts the complementary case: an Update whose Spec
// actually differs DOES invalidate the cache.
func TestCRDInformer_UpdateChangedSpec_Reset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	inf := NewCRDInformer(1*time.Minute, fakeClient, invalidator, func(format string, v ...interface{}) {})
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	crd := newEdgeTestCRD("changed.example.com", "example.com", "changeds", "Changed")
	created, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create crd: %v", err)
	}
	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD add")
	}

	// Genuinely change the Spec.
	updated := created.DeepCopy()
	updated.ResourceVersion = "2"
	updated.Spec.PreserveUnknownFields = true
	if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update crd: %v", err)
	}

	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on a changed-Spec update; expected Reset")
	}
}

// TestCRDInformer_NilInvalidator_NoPanic ensures a nil invalidator is tolerated: Start succeeds and
// CRD events flow through the handlers without panicking (every handler guards `c.invalidator != nil`).
func TestCRDInformer_NilInvalidator_NoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()

	inf := NewCRDInformer(1*time.Minute, fakeClient, nil, func(format string, v ...interface{}) {})
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer with nil invalidator: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	crd := newEdgeTestCRD("nilinv.example.com", "example.com", "nilinvs", "NilInv")
	if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create crd: %v", err)
	}
	if err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete crd: %v", err)
	}
	// Give the handlers time to run; a panic in an informer goroutine would crash the test binary.
	time.Sleep(500 * time.Millisecond)
}

// TestCRDInformer_NilApiExtCli_NoOp asserts Start returns nil and does nothing when apiExtCli is nil.
func TestCRDInformer_NilApiExtCli_NoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	invalidator := newFakeInvalidator()
	inf := NewCRDInformer(1*time.Minute, nil, invalidator, func(format string, v ...interface{}) {})

	if err := inf.Start(ctx); err != nil {
		t.Fatalf("expected nil error for nil apiExtCli, got: %v", err)
	}
	// No client => no informer => no events => Reset must never fire.
	if invalidator.WaitCall(500 * time.Millisecond) {
		t.Fatalf("invalidate called despite nil apiExtCli; Start should be a no-op")
	}
}

// TestCRDInformer_ManyRapidAdds_ResetPerEvent creates several CRDs in quick succession and asserts
// Reset fires for each Add (bounded per-event wait). This guards the crdExists-after-register path
// that the cdc must honor for every newly registered CRD, not just the first.
func TestCRDInformer_ManyRapidAdds_ResetPerEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	inf := NewCRDInformer(1*time.Minute, fakeClient, invalidator, func(format string, v ...interface{}) {})
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	const n = 5
	for i := 0; i < n; i++ {
		suffix := string(rune('a' + i))
		crd := newEdgeTestCRD(
			"rapid"+suffix+".example.com",
			"example.com",
			"rapid"+suffix+"s",
			"Rapid"+strings.ToUpper(suffix),
		)
		if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create crd %d: %v", i, err)
		}
	}

	for i := 0; i < n; i++ {
		if !invalidator.WaitCall(2 * time.Second) {
			t.Fatalf("invalidate not called for rapid add #%d (expected one Reset per Add)", i)
		}
	}
}

// TestCRDInformer_ContextCancel_StopsCleanly cancels the context and asserts the informer stops
// without panicking. After cancellation no further Reset should be delivered for new events.
func TestCRDInformer_ContextCancel_StopsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	inf := NewCRDInformer(1*time.Minute, fakeClient, invalidator, func(format string, v ...interface{}) {})
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Stop the informer.
	cancel()
	// Allow the factory's Start(ctx.Done()) goroutines to unwind.
	time.Sleep(500 * time.Millisecond)

	// Creating a CRD after cancellation must not trigger Reset (informer is stopped) and must not panic.
	crd := newEdgeTestCRD("afterstop.example.com", "example.com", "afterstops", "AfterStop")
	if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(context.Background(), crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create crd after cancel: %v", err)
	}
	if invalidator.WaitCall(1 * time.Second) {
		t.Fatalf("invalidate called after context cancel; informer should be stopped")
	}
}

// TestCRDInformer_LoggerReceivesMessage_OnAddAndDelete captures the logger callback and asserts it
// receives non-empty messages on Add and Delete events.
func TestCRDInformer_LoggerReceivesMessage_OnAddAndDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fakeapix.NewSimpleClientset()
	invalidator := newFakeInvalidator()

	var mu sync.Mutex
	var messages []string
	logger := func(format string, v ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, format)
	}

	inf := NewCRDInformer(1*time.Minute, fakeClient, invalidator, logger)
	if err := inf.Start(ctx); err != nil {
		t.Fatalf("start informer: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	crd := newEdgeTestCRD("logged.example.com", "example.com", "loggeds", "Logged")
	if _, err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create crd: %v", err)
	}
	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD add")
	}
	if err := fakeClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete crd: %v", err)
	}
	if !invalidator.WaitCall(2 * time.Second) {
		t.Fatalf("invalidate not called on CRD delete")
	}
	// Let the logger callbacks (invoked from the handler after Reset) run.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	sawAdd := false
	sawDelete := false
	for _, m := range messages {
		if m == "" {
			t.Fatalf("logger received an empty message")
		}
		if strings.Contains(m, "added") {
			sawAdd = true
		}
		if strings.Contains(m, "deleted") {
			sawDelete = true
		}
	}
	if !sawAdd {
		t.Fatalf("logger did not receive an add message; got %v", messages)
	}
	if !sawDelete {
		t.Fatalf("logger did not receive a delete message; got %v", messages)
	}
}
