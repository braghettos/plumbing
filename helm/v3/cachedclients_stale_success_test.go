package helm

import (
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
)

// TestDeferredMapper_SuccessfulMapping_StaleUntilReset pins the most dangerous staleness class the
// rest of the suite structurally cannot catch: a kind that RESOLVED successfully, whose CRD is then
// removed (or version-bumped so this exact mapping disappears). DeferredDiscoveryRESTMapper.RESTMapping
// only auto-heals on the `err != nil && !cl.Fresh()` branch, so a *successful* stale mapping
// short-circuits the heal and is served forever from the cached delegate.
//
// The load-bearing consequence: a bare memcache.Invalidate() is INSUFFICIENT here — the mapper's
// delegate is not rebuilt while it still answers — whereas mapper.Reset() (which nils the delegate)
// recovers. This is precisely why the cache-staleness fix must call mapper.Reset() via the CRD
// informer, not merely invalidate the discovery cache. (The complementary miss->register->resolve
// path is covered by TestDeferredMapper_StaleUntilInvalidated in cachedclients_staleness_test.go.)
func TestDeferredMapper_SuccessfulMapping_StaleUntilReset(t *testing.T) {
	// Start WITH the Widget CRD present in discovery.
	initial := append(coreOnlyResources(), widgetResourceList())
	fake := newFakeDiscovery(initial)
	memcache := memory.NewMemCacheClient(fake)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache)

	// Warm up: the kind resolves, the mapper caches its delegate, and memcache becomes Fresh.
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err != nil {
		t.Fatalf("precondition: Widget should resolve while its CRD is present, got: %v", err)
	}

	// Simulate the CRD going away (deleted, or its version bumped so this exact GVK no longer exists).
	fake.Resources = coreOnlyResources()

	// Bare cache invalidation WITHOUT a mapper Reset: the cached delegate still answers Widget, so
	// RESTMapping returns err==nil and never reaches the freshness-guarded heal. The stale SUCCESS
	// is served. This assertion documents (not endorses) that Invalidate alone is not enough.
	memcache.Invalidate()
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err != nil {
		t.Fatalf("stale-success: after a bare Invalidate the cached delegate should STILL resolve "+
			"Widget (heal is skipped on the success path), but got: %v", err)
	}

	// Only an explicit Reset (delegate=nil) forces a rebuild from the now-empty discovery -> NoMatch.
	mapper.Reset()
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); !meta.IsNoMatchError(err) {
		t.Fatalf("after Reset the removed Widget must no longer map; want NoMatch, got: %v", err)
	}
}

// TestDeferredMapper_ConcurrentResetAndMapping exercises concurrent Reset() + RESTMapping() on a single
// SHARED DeferredDiscoveryRESTMapper — the exact reuse pattern the cdc has, where one CachedClients.mapper
// is shared across many composition reconciles (readers) while the CRD informer / Install-retry calls
// Reset() (writer). It must not panic, deadlock, or corrupt the mapper. Run the package with -race to
// catch data races on the shared instance.
func TestDeferredMapper_ConcurrentResetAndMapping(t *testing.T) {
	fake := newFakeDiscovery(coreOnlyResources())
	memcache := memory.NewMemCacheClient(fake)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache)

	// Widget is present throughout: a well-behaved shared mapper must keep resolving it despite the
	// concurrent Reset storm (each Reset just forces a re-discovery, never a permanent loss).
	fake.Resources = append(fake.Resources, widgetResourceList())

	var wg sync.WaitGroup
	const readers = 8
	const iters = 200

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_, _ = mapper.RESTMapping(widgetGroupKind, "v1")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < iters; j++ {
			mapper.Reset()
		}
	}()
	wg.Wait()

	// After the storm, a final lookup must still succeed: no permanent corruption from concurrent access.
	if _, err := mapper.RESTMapping(widgetGroupKind, "v1"); err != nil {
		t.Fatalf("after a concurrent Reset/RESTMapping storm, Widget should still resolve, got: %v", err)
	}
}
