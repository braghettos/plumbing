package rbac

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	cachepkg "github.com/krateoplatformops/plumbing/cache"
	xcontext "github.com/krateoplatformops/plumbing/context"
	"github.com/krateoplatformops/plumbing/endpoints"
	"github.com/krateoplatformops/plumbing/env"
	"github.com/krateoplatformops/plumbing/kubeconfig"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

type UserCanTarget struct {
	Verb          string
	GroupResource schema.GroupResource
	Namespace     string
}

type Authorizer struct {
	cache    *cachepkg.TTLCache[userCanCacheKey, bool]
	cacheTTL atomic.Int64
}

type AuthorizerOption func(*authorizerConfig)

type authorizerConfig struct {
	cacheTTL   time.Duration
	maxEntries int
}

// UserCan evaluates multiple authorization checks for the current user and
// returns a decision map keyed by target for O(1) lookups by the caller.
//
// Behavior:
//   - The user endpoint is always resolved from context via xcontext.UserConfig.
//   - The package-level default Authorizer reuses cached entries per target when
//     RBAC_USERCAN_CACHE_TTL is enabled.
//   - Pending checks are grouped by namespace and first evaluated through
//     SelfSubjectRulesReview.
//   - If a rules review fails or is incomplete for some targets, the function
//     falls back to precise per-target SelfSubjectAccessReview calls.
//
// Failure mode:
//   - Missing user config or internal errors never panic; unresolved or denied
//     targets remain false in the returned map.
func UserCan(ctx context.Context, targets []UserCanTarget) map[UserCanTarget]bool {
	return defaultAuthorizer.UserCan(ctx, targets)
}

const defaultUserCanCacheTTL = 10 * time.Second
const defaultUserCanCacheMaxEntries = 4096

var defaultAuthorizer *Authorizer

func init() {
	defaultAuthorizer = NewAuthorizer()
}

type userCanCacheKey struct {
	Endpoint  endpoints.Endpoint
	Verb      string
	Group     string
	Resource  string
	Namespace string
}

type userCanPendingCheck struct {
	index int
	key   userCanCacheKey
}

// NewAuthorizer builds an RBAC authorizer with its own bounded TTL cache.
//
// Defaults are read from:
//   - RBAC_USERCAN_CACHE_TTL
//   - RBAC_USERCAN_CACHE_MAX_ENTRIES
//
// Explicit AuthorizerOption values override the environment-derived defaults.
func NewAuthorizer(opts ...AuthorizerOption) *Authorizer {
	cfg := authorizerConfig{
		cacheTTL:   env.Duration("RBAC_USERCAN_CACHE_TTL", defaultUserCanCacheTTL),
		maxEntries: env.Int("RBAC_USERCAN_CACHE_MAX_ENTRIES", defaultUserCanCacheMaxEntries),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	auth := &Authorizer{
		cache: cachepkg.NewTTL[userCanCacheKey, bool](
			cachepkg.WithMaxEntries(cfg.maxEntries),
		),
	}
	auth.cacheTTL.Store(cfg.cacheTTL.Nanoseconds())
	return auth
}

func WithCacheTTL(ttl time.Duration) AuthorizerOption {
	return func(cfg *authorizerConfig) {
		cfg.cacheTTL = ttl
	}
}

func WithCacheMaxEntries(maxEntries int) AuthorizerOption {
	return func(cfg *authorizerConfig) {
		cfg.maxEntries = maxEntries
	}
}

func SetUserCanCacheTTL(ttl time.Duration) {
	defaultAuthorizer.SetCacheTTL(ttl)
}

func (a *Authorizer) SetCacheTTL(ttl time.Duration) {
	a.cacheTTL.Store(ttl.Nanoseconds())
	if ttl <= 0 && a.cache != nil {
		a.cache.Clear()
	}
}

func (a *Authorizer) Close() {
	if a.cache != nil {
		a.cache.Close()
	}
}

// UserCan evaluates multiple authorization checks for the current user using
// the receiver's cache configuration and returns a decision map keyed by target.
func (a *Authorizer) UserCan(ctx context.Context, targets []UserCanTarget) map[UserCanTarget]bool {
	log := xcontext.Logger(ctx)
	if len(targets) == 0 {
		return nil
	}

	result := make(map[UserCanTarget]bool, len(targets))
	ep, ok := resolveUserEndpoint(ctx)
	if !ok {
		log.Error("unable to get user endpoint")
		return result
	}

	allowed := make([]bool, len(targets))
	ttl := time.Duration(a.cacheTTL.Load())
	pending := make([]userCanPendingCheck, 0, len(targets))
	grouped := make(map[string][]userCanPendingCheck, len(targets))

	for i, tgt := range targets {
		cacheKey := newUserCanCacheKey(ep, tgt)
		if ttl > 0 && a.cache != nil {
			if cachedAllowed, hit := a.cache.Get(cacheKey); hit {
				allowed[i] = cachedAllowed
				continue
			}
		}

		item := userCanPendingCheck{
			index: i,
			key:   cacheKey,
		}
		pending = append(pending, item)
		grouped[tgt.Namespace] = append(grouped[tgt.Namespace], item)
	}

	if len(pending) == 0 {
		return buildUserCanResult(targets, allowed)
	}

	clientset, err := newUserClientset(ctx, ep)
	if err != nil {
		log.Error("unable to create kubernetes clientset", slog.Any("err", err))
		return buildUserCanResult(targets, allowed)
	}

	for namespace, items := range grouped {
		rulesReview, err := performSelfSubjectRulesReview(ctx, clientset, namespace)
		if err != nil {
			log.Debug("SelfSubjectRulesReview failed, falling back to per-target access reviews",
				slog.String("namespace", namespace),
				slog.Any("err", err))
			a.resolvePendingWithAccessReviews(ctx, clientset, targets, allowed, items, ttl)
			continue
		}

		log.Debug("SelfSubjectRulesReview result",
			slog.String("namespace", namespace),
			slog.Bool("incomplete", rulesReview.Status.Incomplete))

		fallback := items[:0]
		for _, item := range items {
			target := targets[item.index]
			if rulesAllowTarget(rulesReview.Status.ResourceRules, target) {
				allowed[item.index] = true
				log.Debug("UserCan result from rules review",
					slog.String("source", "rules-review"),
					slog.Bool("allowed", true))
				a.storeCache(item.key, true, ttl)
				continue
			}
			if !rulesReview.Status.Incomplete {
				log.Debug("UserCan result from rules review",
					slog.String("source", "rules-review"),
					slog.Bool("allowed", false))
				a.storeCache(item.key, false, ttl)
				continue
			}
			fallback = append(fallback, item)
		}

		if len(fallback) > 0 {
			a.resolvePendingWithAccessReviews(ctx, clientset, targets, allowed, fallback, ttl)
		}
	}

	return buildUserCanResult(targets, allowed)
}

func newUserCanCacheKey(ep endpoints.Endpoint, target UserCanTarget) userCanCacheKey {
	return userCanCacheKey{
		Endpoint:  ep,
		Verb:      target.Verb,
		Group:     target.GroupResource.Group,
		Resource:  target.GroupResource.Resource,
		Namespace: target.Namespace,
	}
}

func resolveUserEndpoint(ctx context.Context) (endpoints.Endpoint, bool) {
	ep, err := xcontext.UserConfig(ctx)
	if err != nil {
		return endpoints.Endpoint{}, false
	}
	return ep, true
}

func newUserClientset(ctx context.Context, ep endpoints.Endpoint) (*kubernetes.Clientset, error) {
	rc, err := kubeconfig.NewClientConfig(ctx, ep)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(rc)
}

func performSelfSubjectAccessReview(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	target UserCanTarget,
) (*authv1.SelfSubjectAccessReview, error) {
	selfCheck := authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:     target.GroupResource.Group,
				Resource:  target.GroupResource.Resource,
				Namespace: target.Namespace,
				Verb:      target.Verb,
			},
		},
	}

	return clientset.AuthorizationV1().SelfSubjectAccessReviews().
		Create(ctx, &selfCheck, metav1.CreateOptions{})
}

func performSelfSubjectRulesReview(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) (*authv1.SelfSubjectRulesReview, error) {
	review := authv1.SelfSubjectRulesReview{
		Spec: authv1.SelfSubjectRulesReviewSpec{
			Namespace: namespace,
		},
	}

	return clientset.AuthorizationV1().SelfSubjectRulesReviews().
		Create(ctx, &review, metav1.CreateOptions{})
}

func (a *Authorizer) resolvePendingWithAccessReviews(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	targets []UserCanTarget,
	allowed []bool,
	pending []userCanPendingCheck,
	ttl time.Duration,
) {
	for _, item := range pending {
		xcontext.Logger(ctx).Debug("UserCan requesting SelfSubjectAccessReview",
			slog.String("source", "k8s-api"))
		resp, err := performSelfSubjectAccessReview(ctx, clientset, targets[item.index])
		if err != nil {
			continue
		}
		xcontext.Logger(ctx).Debug("SelfSubjectAccessReviews result", slog.Any("response", resp))
		allowed[item.index] = resp.Status.Allowed
		a.storeCache(item.key, resp.Status.Allowed, ttl)
	}
}

func (a *Authorizer) storeCache(key userCanCacheKey, allowed bool, ttl time.Duration) {
	if ttl <= 0 || a.cache == nil {
		return
	}
	a.cache.Set(key, allowed, ttl)
}

func rulesAllowTarget(rules []authv1.ResourceRule, target UserCanTarget) bool {
	for _, rule := range rules {
		if len(rule.ResourceNames) > 0 {
			continue
		}
		if !stringSliceContains(rule.Verbs, target.Verb) {
			continue
		}
		if !stringSliceContains(rule.APIGroups, target.GroupResource.Group) {
			continue
		}
		if !stringSliceContains(rule.Resources, target.GroupResource.Resource) {
			continue
		}
		return true
	}
	return false
}

func stringSliceContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == "*" || value == wanted {
			return true
		}
	}
	return false
}

func buildUserCanResult(targets []UserCanTarget, allowed []bool) map[UserCanTarget]bool {
	res := make(map[UserCanTarget]bool, len(targets))
	for i, target := range targets {
		res[target] = allowed[i]
	}
	return res
}
