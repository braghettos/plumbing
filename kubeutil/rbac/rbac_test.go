package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	xcontext "github.com/krateoplatformops/plumbing/context"
	"github.com/krateoplatformops/plumbing/endpoints"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestUserCanUsesCache(t *testing.T) {
	t.Setenv("TEST_MODE", "true")
	SetUserCanCacheTTL(0)
	SetUserCanCacheTTL(30 * time.Second)
	defer SetUserCanCacheTTL(defaultUserCanCacheTTL)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews":
			http.Error(w, "not supported", http.StatusNotFound)
			return
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}`))
	}))
	defer server.Close()

	ctx := xcontext.BuildContext(context.Background(),
		xcontext.WithUserConfig(endpoints.Endpoint{
			ServerURL: server.URL,
		}))

	opts := UserCanTarget{
		Verb: "get",
		GroupResource: schema.GroupResource{
			Group:    "",
			Resource: "pods",
		},
		Namespace: "default",
	}

	res := UserCan(ctx, []UserCanTarget{opts})
	if _, ok := res[opts]; !ok {
		t.Fatal("expected first call to be allowed")
	}
	res = UserCan(ctx, []UserCanTarget{opts})
	if _, ok := res[opts]; !ok {
		t.Fatal("expected second call to be allowed")
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 API call due to cache hit, got %d", got)
	}
}

func TestUserCanWithoutCacheCallsAPIEachTime(t *testing.T) {
	t.Setenv("TEST_MODE", "true")
	SetUserCanCacheTTL(0)
	defer SetUserCanCacheTTL(defaultUserCanCacheTTL)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews":
			_, _ = w.Write([]byte(`{
				"apiVersion":"authorization.k8s.io/v1",
				"kind":"SelfSubjectRulesReview",
				"status":{
					"resourceRules":[
						{"verbs":["list"],"apiGroups":[""],"resources":["pods"]}
					],
					"incomplete":false
				}
			}`))
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			calls.Add(1)
			_, _ = w.Write([]byte(`{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx := xcontext.BuildContext(context.Background(),
		xcontext.WithUserConfig(endpoints.Endpoint{
			ServerURL: server.URL,
		}))

	opts := UserCanTarget{
		Verb: "list",
		GroupResource: schema.GroupResource{
			Group:    "",
			Resource: "pods",
		},
		Namespace: "default",
	}

	res := UserCan(ctx, []UserCanTarget{opts})
	if _, ok := res[opts]; !ok {
		t.Fatal("expected first call to be allowed")
	}
	res = UserCan(ctx, []UserCanTarget{opts})
	if _, ok := res[opts]; !ok {
		t.Fatal("expected second call to be allowed")
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected 0 access review calls when rules review is sufficient, got %d", got)
	}
}

func TestUserCanManyUsesRulesReview(t *testing.T) {
	t.Setenv("TEST_MODE", "true")
	SetUserCanCacheTTL(0)
	defer SetUserCanCacheTTL(defaultUserCanCacheTTL)

	var rulesCalls atomic.Int32
	var accessCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews":
			rulesCalls.Add(1)
			_, _ = w.Write([]byte(`{
				"apiVersion":"authorization.k8s.io/v1",
				"kind":"SelfSubjectRulesReview",
				"status":{
					"resourceRules":[
						{"verbs":["get"],"apiGroups":[""],"resources":["pods"]},
						{"verbs":["list"],"apiGroups":["apps"],"resources":["deployments"]}
					],
					"incomplete":false
				}
			}`))
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			accessCalls.Add(1)
			t.Fatal("did not expect per-target access reviews when rules review is complete")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx := xcontext.BuildContext(context.Background(),
		xcontext.WithUserConfig(endpoints.Endpoint{
			ServerURL: server.URL,
		}))

	allowed := UserCan(ctx, []UserCanTarget{
		{Verb: "get", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"},
		{Verb: "list", GroupResource: schema.GroupResource{Group: "apps", Resource: "deployments"}, Namespace: "default"},
		{Verb: "delete", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"},
	})

	if len(allowed) != 3 {
		t.Fatalf("expected 3 results, got %d", len(allowed))
	}
	if !allowed[UserCanTarget{Verb: "get", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"}] {
		t.Fatal("expected get pods/default to be allowed")
	}
	if !allowed[UserCanTarget{Verb: "list", GroupResource: schema.GroupResource{Group: "apps", Resource: "deployments"}, Namespace: "default"}] {
		t.Fatal("expected list deployments.apps/default to be allowed")
	}
	if allowed[UserCanTarget{Verb: "delete", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"}] {
		t.Fatal("expected delete pods/default to be denied")
	}
	if rulesCalls.Load() != 1 {
		t.Fatalf("expected 1 rules review call, got %d", rulesCalls.Load())
	}
	if accessCalls.Load() != 0 {
		t.Fatalf("expected 0 access review calls, got %d", accessCalls.Load())
	}
}

func TestUserCanManyFallsBackToAccessReviewWhenRulesReviewIncomplete(t *testing.T) {
	t.Setenv("TEST_MODE", "true")
	SetUserCanCacheTTL(0)
	defer SetUserCanCacheTTL(defaultUserCanCacheTTL)

	var rulesCalls atomic.Int32
	var accessCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews":
			rulesCalls.Add(1)
			_, _ = w.Write([]byte(`{
				"apiVersion":"authorization.k8s.io/v1",
				"kind":"SelfSubjectRulesReview",
				"status":{"resourceRules":[],"incomplete":true}
			}`))
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			call := accessCalls.Add(1)
			if call == 1 {
				_, _ = w.Write([]byte(`{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}`))
				return
			}
			_, _ = w.Write([]byte(`{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":false}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx := xcontext.BuildContext(context.Background(),
		xcontext.WithUserConfig(endpoints.Endpoint{
			ServerURL: server.URL,
		}))

	allowed := UserCan(ctx, []UserCanTarget{
		{Verb: "get", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"},
		{Verb: "get", GroupResource: schema.GroupResource{Resource: "secrets"}, Namespace: "default"},
	})

	if len(allowed) != 2 {
		t.Fatalf("expected 2 results, got %d", len(allowed))
	}
	if !allowed[UserCanTarget{Verb: "get", GroupResource: schema.GroupResource{Resource: "pods"}, Namespace: "default"}] {
		t.Fatal("expected get pods/default to be allowed")
	}
	if allowed[UserCanTarget{Verb: "get", GroupResource: schema.GroupResource{Resource: "secrets"}, Namespace: "default"}] {
		t.Fatal("expected get secrets/default to be denied")
	}
	if rulesCalls.Load() != 1 {
		t.Fatalf("expected 1 rules review call, got %d", rulesCalls.Load())
	}
	if accessCalls.Load() != 2 {
		t.Fatalf("expected 2 access review calls, got %d", accessCalls.Load())
	}
}

func TestNewAuthorizerOptionsOverrideEnvDefaults(t *testing.T) {
	t.Setenv("RBAC_USERCAN_CACHE_TTL", "1m")
	t.Setenv("RBAC_USERCAN_CACHE_MAX_ENTRIES", "100")

	auth := NewAuthorizer(
		WithCacheTTL(2*time.Second),
		WithCacheMaxEntries(1),
	)
	defer auth.Close()

	if got := time.Duration(auth.cacheTTL.Load()); got != 2*time.Second {
		t.Fatalf("expected cache TTL override to be 2s, got %s", got)
	}

	key1 := userCanCacheKey{Verb: "get", Resource: "pods", Namespace: "default"}
	key2 := userCanCacheKey{Verb: "list", Resource: "pods", Namespace: "default"}

	auth.storeCache(key1, true, time.Minute)
	auth.storeCache(key2, true, time.Minute)

	if _, found := auth.cache.Get(key1); found {
		t.Fatal("expected first cache entry to be evicted by max entries override")
	}
	if _, found := auth.cache.Get(key2); !found {
		t.Fatal("expected second cache entry to be retained")
	}
}
