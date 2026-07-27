package dynamicwatch

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// fakeController is a minimal controller.Controller: it never actually starts a source (Watch just
// records the call), which is all EnsureWatch's dedup logic needs to be exercised.
type fakeController struct {
	watchErr error

	mu         sync.Mutex
	watchCalls int
}

func (f *fakeController) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (f *fakeController) Watch(src source.Source) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchCalls++
	return f.watchErr
}

func (f *fakeController) Start(ctx context.Context) error { return nil }

func (f *fakeController) GetLogger() logr.Logger { return logr.Discard() }

func (f *fakeController) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchCalls
}

var testGVK = schema.GroupVersionKind{Group: "krateo.io", Version: "v1alpha1", Kind: "Configuration"}

func noopMapFunc(ctx context.Context, obj client.Object) []reconcile.Request { return nil }

func TestEnsureWatchRegistersOnce(t *testing.T) {
	r := NewRegistry(nil)
	fc := &fakeController{}

	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("first EnsureWatch: %v", err)
	}
	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("second EnsureWatch: %v", err)
	}
	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("third EnsureWatch: %v", err)
	}

	if got := fc.calls(); got != 1 {
		t.Fatalf("expected exactly one underlying Watch call, got %d", got)
	}
	if !r.Watching(testGVK) {
		t.Fatal("expected Watching to report true after successful registration")
	}
}

func TestEnsureWatchDistinctGVKsEachRegister(t *testing.T) {
	r := NewRegistry(nil)
	fc := &fakeController{}

	other := schema.GroupVersionKind{Group: "krateo.io", Version: "v1alpha1", Kind: "OtherKind"}

	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("EnsureWatch(testGVK): %v", err)
	}
	if err := r.EnsureWatch(fc, other, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("EnsureWatch(other): %v", err)
	}

	if got := fc.calls(); got != 2 {
		t.Fatalf("expected one Watch call per distinct GVK, got %d", got)
	}
}

func TestEnsureWatchFailureIsNotSticky(t *testing.T) {
	r := NewRegistry(nil)
	fc := &fakeController{watchErr: errors.New("kind not yet discoverable")}

	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err == nil {
		t.Fatal("expected the underlying Watch error to propagate")
	}
	if r.Watching(testGVK) {
		t.Fatal("a failed registration must not be marked as watched")
	}

	fc.watchErr = nil
	if err := r.EnsureWatch(fc, testGVK, handler.MapFunc(noopMapFunc)); err != nil {
		t.Fatalf("expected retry to succeed once the error clears: %v", err)
	}
	if !r.Watching(testGVK) {
		t.Fatal("expected Watching to report true after the retry succeeds")
	}
	if got := fc.calls(); got != 2 {
		t.Fatalf("expected 2 Watch calls (failed + retried), got %d", got)
	}
}
