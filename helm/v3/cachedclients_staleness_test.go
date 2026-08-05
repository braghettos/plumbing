package helm

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	clienttesting "k8s.io/client-go/testing"
)

// widgetGroupVersion / widgetGroupKind describe the new CRD (group example.com/v1, kind Widget)
// that a fresh CRD registration would introduce. Discovery starts WITHOUT it.
var (
	widgetGroupVersion = "example.com/v1"
	widgetGroupKind    = schema.GroupKind{Group: "example.com", Kind: "Widget"}
)

// coreOnlyResources is the initial discovery state: only the core v1 group, no example.com/v1.
func coreOnlyResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{
					Name:       "configmaps",
					Namespaced: true,
					Kind:       "ConfigMap",
				},
			},
		},
	}
}

// widgetResourceList is the discovery entry that appears once the Widget CRD registers.
func widgetResourceList() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: widgetGroupVersion,
		APIResources: []metav1.APIResource{
			{
				Name:         "widgets",
				SingularName: "widget",
				Namespaced:   true,
				Kind:         "Widget",
				Group:        "example.com",
				Version:      "v1",
			},
		},
	}
}

// newFakeDiscovery builds a mutable FakeDiscovery whose Resources list can be appended to at runtime
// to simulate a CRD registering after the discovery/RESTMapper cache has already been warmed.
func newFakeDiscovery(initial []*metav1.APIResourceList) *discoveryfake.FakeDiscovery {
	return &discoveryfake.FakeDiscovery{
		Fake: &clienttesting.Fake{
			Resources: initial,
		},
	}
}

// TestNewCachedClients_ReturnsDeferredMapperAndCachedDiscovery verifies the shape of the value
// NewCachedClients hands back: a *restmapper.DeferredDiscoveryRESTMapper and a non-nil
// discovery.CachedDiscoveryInterface. These are the two objects the cdc must keep and invalidate.
func TestNewCachedClients_ReturnsDeferredMapperAndCachedDiscovery(t *testing.T) {
	cc, err := NewCachedClients(&rest.Config{Host: "https://127.0.0.1:6443"})
	if err != nil {
		t.Fatalf("NewCachedClients: %v", err)
	}

	if cc.mapper == nil {
		t.Fatal("expected non-nil mapper")
	}
	// mapper is concretely a *restmapper.DeferredDiscoveryRESTMapper.
	var _ *restmapper.DeferredDiscoveryRESTMapper = cc.mapper

	if cc.discoveryClient == nil {
		t.Fatal("expected non-nil discoveryClient")
	}
	// discoveryClient satisfies discovery.CachedDiscoveryInterface (compile-time + runtime).
	var _ discovery.CachedDiscoveryInterface = cc.discoveryClient
}

// TestDeferredMapper_StaleUntilInvalidated is THE CORE staleness proof. It reproduces the bug the
// cdc exhibits: after a new CRD registers, the DeferredDiscoveryRESTMapper (backed by a memcache
// discovery client) keeps answering RESTMapping with a NoMatch error until the discovery cache is
// invalidated. Without that invalidation, the umbrella's crdExists lookup misses the new kind and
// Pass B skips the component — exactly the reported symptom. This is precisely what the CRD
// informer's mapper.Reset() wiring (which the cdc does NOT install) is meant to trigger.
func TestDeferredMapper_StaleUntilInvalidated(t *testing.T) {
	fake := newFakeDiscovery(coreOnlyResources())
	memcache := memory.NewMemCacheClient(fake)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache)

	// (a) Before the CRD exists, RESTMapping must miss.
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err == nil {
		t.Fatal("expected NoMatch before CRD registers, got nil error")
	} else if !meta.IsNoMatchError(err) {
		t.Fatalf("expected NoMatch error before CRD registers, got: %v", err)
	}

	// (b) A new CRD registers: append the Widget resource to discovery. The underlying discovery
	// server now knows Widget, but the memcache + deferred mapper have already cached the miss.
	fake.Resources = append(fake.Resources, widgetResourceList())

	// (c) THE BUG: RESTMapping still misses because the cache is stale. The memcache now reports Fresh
	// (the (a) lookup populated it), so the mapper's self-heal-on-miss path is disarmed and it keeps
	// serving the pre-CRD delegate.
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err == nil {
		t.Fatal("expected STALE NoMatch after CRD registers but before invalidation, got nil error " +
			"(cache unexpectedly fresh)")
	} else if !meta.IsNoMatchError(err) {
		t.Fatalf("expected STALE NoMatch after CRD registers but before invalidation, got: %v", err)
	}

	// (d) THE FIX MECHANISM: reset the deferred mapper. Reset() internally invalidates the underlying
	// cached (memcache) discovery client AND drops the mapper's cached delegate, forcing a fresh
	// discovery on the next request. This single call is exactly what the CRD informer invokes as its
	// invalidator — and exactly what the cdc fails to wire.
	mapper.Reset()

	// (e) RESTMapping now resolves the freshly-registered Widget kind.
	m, err := mapper.RESTMapping(widgetGroupKind, "v1")
	if err != nil {
		t.Fatalf("expected RESTMapping to succeed after invalidation+reset, got: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil RESTMapping after invalidation+reset")
	}
	if got := m.Resource.Resource; got != "widgets" {
		t.Fatalf("expected resolved resource %q, got %q", "widgets", got)
	}
	if got := m.GroupVersionKind.Kind; got != "Widget" {
		t.Fatalf("expected resolved kind %q, got %q", "Widget", got)
	}
}

// TestDeferredMapper_StaleWhileFresh_HealsWhenNotFresh documents empirically WHY the staleness
// happens and WHERE the invalidation must land: the DeferredDiscoveryRESTMapper only auto-re-discovers
// on a miss when its underlying cached discovery client reports NOT fresh (see RESTMapping's
// `!d.cl.Fresh()` guard). Once the memcache has been populated it reports Fresh, so repeated lookups
// keep serving the stale delegate no matter how many times they are retried — this is the cdc bug.
// Clearing the memcache's freshness bit (memcache.Invalidate(), which is exactly what mapper.Reset()
// does under the hood, and what the CRD informer must ultimately trigger) makes the very next lookup
// self-heal. mapper.Reset() and memcache.Invalidate() are therefore equivalent triggers here; doing
// NEITHER is what leaves the mapper stale.
func TestDeferredMapper_StaleWhileFresh_HealsWhenNotFresh(t *testing.T) {
	fake := newFakeDiscovery(coreOnlyResources())
	memcache := memory.NewMemCacheClient(fake)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache)

	// Warm the mapper's delegate AND the memcache with a lookup that misses. After this, the memcache
	// reports Fresh, so the auto-heal-on-miss path is disarmed.
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); !meta.IsNoMatchError(err) {
		t.Fatalf("expected NoMatch on warm-up, got: %v", err)
	}
	if !memcache.Fresh() {
		t.Fatal("expected memcache to report Fresh after warm-up lookup populated it")
	}

	// CRD registers.
	fake.Resources = append(fake.Resources, widgetResourceList())

	// Do NOTHING to invalidate. Repeated lookups keep missing because the memcache still reports Fresh,
	// so the mapper never re-discovers — this is the persistent staleness the cdc exhibits.
	for i := 0; i < 3; i++ {
		_, err := mapper.RESTMapping(widgetGroupKind, "v1")
		if err == nil {
			t.Fatalf("attempt %d: expected persistent stale NoMatch with no invalidation, got success", i)
		}
		if !meta.IsNoMatchError(err) {
			t.Fatalf("attempt %d: expected NoMatch, got: %v", i, err)
		}
	}
	if !memcache.Fresh() {
		t.Fatal("expected memcache to still report Fresh (nothing invalidated it)")
	}

	// Clear the freshness bit. On the next miss the mapper sees !Fresh(), auto-resets, and re-discovers.
	memcache.Invalidate()
	if memcache.Fresh() {
		t.Fatal("expected memcache to report NOT fresh after Invalidate()")
	}
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err != nil {
		t.Fatalf("expected RESTMapping to self-heal after Invalidate() cleared freshness, got: %v", err)
	}
}
