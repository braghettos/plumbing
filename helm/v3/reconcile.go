package helm

import (
	"context"
	"fmt"
	"strings"

	helmconfig "github.com/krateoplatformops/plumbing/helm"
	"helm.sh/helm/v3/pkg/kube"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"
)

// Reconcile performs a fork-free, self-healing "apply-if-changed" reconcile of
// a helm release using ONLY helm's exported Go API.
//
// The flow:
//  1. GetRelease -> the stored release. Its .Manifest is the CURRENT (last
//     applied) manifest. If the release does not exist we cannot self-heal an
//     absent release; we delegate to Upgrade (Install:true) and report Changed.
//  2. Render the TARGET manifest via a server-side dry-run Upgrade. This runs
//     the exact render pipeline (post-renderer, template funcs, lookups,
//     server validation) and returns Release.Manifest WITHOUT writing a
//     revision or running hooks.
//  3. Build helm kube ResourceLists from the stored (current) and rendered
//     (target) manifests using KubeClient.Build.
//  4. Snapshot each target object's live metadata.resourceVersion (absent =>
//     will be created).
//  5. KubeClient.Update(current, target, force) — helm's 3-way merge. This
//     CREATES resources that were deleted out-of-band and PATCHES resources
//     that drifted, converging the cluster. Update refreshes every target
//     Info's Object in place (patch branch: Refresh(patchedObj); no-op branch:
//     Get()).
//  6. changed := any Created || any Deleted || any surviving target whose
//     refreshed resourceVersion differs from the snapshot (a real patch bumps
//     the RV; a no-op leaves it identical). We deliberately do NOT use
//     len(Result.Updated) — helm appends every visited target to Updated even
//     for no-op patches.
//  7. If changed, run a real Upgrade ONCE to write the revision and run hooks
//     with correct ordering/delete-policy (delegated to action.Upgrade rather
//     than reimplementing hook handling). Step 5 already converged the cluster,
//     so this Upgrade is a near-no-op re-apply.
//  8. If NOT changed, return Changed:false — no Releases write, no hooks.
func (c *client) Reconcile(ctx context.Context, releaseName, chartRef string, cfg *helmconfig.UpgradeConfig) (*helmconfig.ReconcileResult, error) {
	if cfg == nil {
		cfg = &helmconfig.UpgradeConfig{}
	}
	if cfg.ActionConfig == nil {
		cfg.ActionConfig = &helmconfig.ActionConfig{}
	}

	// 1. Stored (current) release.
	stored, err := c.GetRelease(ctx, releaseName, &helmconfig.GetConfig{})
	if err != nil {
		return nil, fmt.Errorf("reconcile: getting stored release: %w", err)
	}
	if stored == nil {
		// No release yet: there is nothing to self-heal against. Perform a real
		// install-or-upgrade and report it as a change. The caller (cdc Create
		// path) normally handles this, but we keep Reconcile total.
		installCfg := *cfg
		installCfg.Install = true
		rel, err := c.Upgrade(ctx, releaseName, chartRef, &installCfg)
		if err != nil {
			return nil, fmt.Errorf("reconcile: initial upgrade (no stored release): %w", err)
		}
		return &helmconfig.ReconcileResult{Changed: true, Release: rel}, nil
	}
	currentManifest := stored.Manifest

	// 2. Render the target manifest via a server-side dry-run Upgrade (no
	//    revision written, no hooks run). This is ONLY how we obtain the
	//    rendered target cheaply through the public API; it is NOT an
	//    up-to-date gate.
	dryCfg := *cfg
	dryActionCfg := *cfg.ActionConfig
	dryActionCfg.DryRun = helmconfig.DryRunServer
	dryCfg.ActionConfig = &dryActionCfg
	rendered, err := c.Upgrade(ctx, releaseName, chartRef, &dryCfg)
	if err != nil {
		return nil, fmt.Errorf("reconcile: rendering target (dry-run): %w", err)
	}
	targetManifest := rendered.Manifest

	// 3. Build current + target resource lists via helm's own kube client.
	actionConfig, err := c.newActionConfig(c.namespace, c.restConfig)
	if err != nil {
		return nil, fmt.Errorf("reconcile: building action config: %w", err)
	}
	kc := actionConfig.KubeClient

	current, err := kc.Build(strings.NewReader(currentManifest), false)
	if err != nil {
		return nil, fmt.Errorf("reconcile: building current resource list: %w", err)
	}
	target, err := kc.Build(strings.NewReader(targetManifest), true)
	if err != nil {
		return nil, fmt.Errorf("reconcile: building target resource list: %w", err)
	}

	// 4. Snapshot live resourceVersion for each target object BEFORE Update.
	//    Missing (IsNotFound) => absent, will be created by Update.
	snapshot := make(map[string]string, len(target))
	absent := make(map[string]bool, len(target))
	for _, info := range target {
		key := infoKey(info)
		helper := resource.NewHelper(info.Client, info.Mapping)
		live, gerr := helper.Get(info.Namespace, info.Name)
		if gerr != nil {
			// Treat any get failure (typically NotFound) as absent; Update will
			// create it and surface a real error if the create fails.
			absent[key] = true
			continue
		}
		snapshot[key] = resourceVersionOf(live)
	}

	// 5. Self-healing merge: creates deleted children, patches drifted ones.
	// UpdateThreeWayMerge (NOT Update) so UNSTRUCTURED/CR children get helm's
	// three-way-with-live merge (createPatch -> CreateThreeWayJSONMergePatch(lastApplied,
	// target, LIVE)): a declared field mutated out-of-band differs from LIVE and is reverted
	// to target. The plain Update() path passes threeWayMergeForUnstructured=false → a 2-way
	// (lastApplied vs target) patch that ignores live, so it does NOT revert CR field drift.
	// UpdateThreeWayMerge lives on kube.InterfaceThreeWayMerge, a compat split from kube.Interface
	// (helm v3.20.2 interface.go:83-87, "avoid breaking backwards compatibility for Interface
	// implementers"; TODO Helm 4 folds it in). The concrete *kube.Client implements it
	// (var _ InterfaceThreeWayMerge = (*Client)(nil), client.go:133), so type-assert to reach it —
	// no fork. Fall back to the 2-way Update if a KubeClient ever doesn't implement it (drift-heal
	// for CRs degrades, but delete-heal + apply still work).
	tw, ok := kc.(kube.InterfaceThreeWayMerge)
	var res *kube.Result
	if ok {
		res, err = tw.UpdateThreeWayMerge(current, target, cfg.Force)
	} else {
		res, err = kc.Update(current, target, cfg.Force)
	}
	if err != nil {
		return nil, fmt.Errorf("reconcile: kube update (self-heal apply): %w", err)
	}

	// 6. Decide whether the cluster was actually mutated.
	created := len(res.Created)
	deleted := len(res.Deleted)
	patched := 0
	for _, info := range target {
		key := infoKey(info)
		if absent[key] {
			// Counted under Created via res.Created; skip here.
			continue
		}
		newRV := resourceVersionOf(info.Object)
		if newRV == "" {
			// Could not read refreshed RV; be conservative and treat as changed.
			patched++
			continue
		}
		if newRV != snapshot[key] {
			patched++
		}
	}

	changed := created > 0 || deleted > 0 || patched > 0

	result := &helmconfig.ReconcileResult{
		Changed:        changed,
		Created:        created,
		Deleted:        deleted,
		PatchedUpdated: patched,
		Release:        stored,
	}

	if !changed {
		// Steady state: cluster already matched. No revision, no hooks.
		return result, nil
	}

	// 7. A real change happened. Run ONE real Upgrade to persist a revision and
	//    run hooks with correct ordering + delete-policy. Step 5 already
	//    converged the cluster, so this is a near-no-op re-apply.
	rel, err := c.Upgrade(ctx, releaseName, chartRef, cfg)
	if err != nil {
		return result, fmt.Errorf("reconcile: real upgrade after detected change: %w", err)
	}
	result.Release = rel
	return result, nil
}

// infoKey uniquely identifies a resource.Info across the current/target lists.
func infoKey(info *resource.Info) string {
	gvk := info.Object.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s/%s/%s/%s", gvk.GroupVersion().String(), gvk.Kind, info.Namespace, info.Name)
}

// resourceVersionOf extracts metadata.resourceVersion from a runtime.Object
// (typed or unstructured) via apimachinery's meta accessor. Returns "" when it
// cannot be read.
func resourceVersionOf(obj runtime.Object) string {
	acc, err := apimeta.Accessor(obj)
	if err != nil {
		return ""
	}
	return acc.GetResourceVersion()
}