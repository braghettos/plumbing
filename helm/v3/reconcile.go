package helm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	helmconfig "github.com/krateoplatformops/plumbing/helm"
	"helm.sh/helm/v3/pkg/kube"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"
)

// traceparentAnnotation is the W3C trace-context annotation the helm post-renderer
// stamps on every *.krateo.io child resource on each render. It changes every
// reconcile, so it must be neutralized before the change-detection merge (see step
// 4.5 in Reconcile) or it would defeat the apply-if-changed no-churn guarantee.
const traceparentAnnotation = "krateo.io/traceparent"

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

	// 4. Snapshot live resourceVersion for each target object BEFORE Update, and
	//    capture each live child's current krateo.io/traceparent (see step 4.5).
	//    Missing (IsNotFound) => absent, will be created by Update.
	snapshot := make(map[string]string, len(target))
	absent := make(map[string]bool, len(target))
	liveTraceparent := make(map[string]string, len(target))
	liveObj := make(map[string]runtime.Object, len(target))
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
		liveObj[key] = live
		if tp := annotationOf(live, traceparentAnnotation); tp != "" {
			liveTraceparent[key] = tp
		}
	}

	// 4.5. Neutralize the volatile traceparent stamp. The helm post-renderer stamps a
	// FRESH krateo.io/traceparent on every *.krateo.io child on each render (trace-context
	// propagation). That value changes every reconcile, so without this the three-way merge
	// below would patch it every cycle — an RV bump that reads as a real "change" (step 6),
	// forcing a helm revision + hooks even though nothing else moved (the exact per-minute
	// revision churn this reconcile exists to kill; observed on krateo.io-child compositions
	// like snowplow's authn ServiceAccount and the installer umbrella). We copy the LIVE
	// child's existing traceparent onto the rendered target so the annotation contributes no
	// diff: when traceparent is the ONLY difference the merge patch is empty and helm skips the
	// write entirely (RV stable -> changed=false -> no revision, no hooks). Genuine drift in
	// every OTHER field still heals. A real change re-stamps a fresh traceparent through the
	// actual Upgrade in step 7. Freshly-created children (no live value) keep the target stamp.
	for _, info := range target {
		key := infoKey(info)
		if absent[key] {
			continue
		}
		if tp, ok := liveTraceparent[key]; ok {
			setAnnotation(info.Object, traceparentAnnotation, tp)
		}
	}

	// 4.6. Normalize Secret stringData -> data. A chart that writes a Secret via
	// stringData never converges under an apply loop: the apiserver base64-encodes
	// stringData into data and DROPS stringData, so the next render re-adds stringData
	// while live only has data — an eternal diff the three-way merge patches every cycle
	// (revision churn even when the secret VALUE is unchanged; observed on the installer
	// umbrella's jwt-sign-key). We fold stringData into data (exactly as the apiserver
	// does: data[k] = base64(stringData[k]), stringData wins on key collisions) on the
	// rendered target so it matches the stored representation and contributes no diff when
	// the value is stable. A real value change still differs in data -> changed=true. This
	// runs on every target (a create-time secret is equally valid expressed as data).
	for _, info := range target {
		normalizeSecretStringData(info.Object)
	}

	// 4.7. DEBUG diagnostic (RECONCILE_DEBUG_DIFF env). For each target that already exists,
	// emit the live->target JSON merge patch AFTER the traceparent copy (4.5) + stringData fold
	// (4.6), with volatile server fields and helm-owned metadata stripped. This shows EXACTLY
	// what the change-detection merge will act on per child: an empty patch means the RV-delta
	// (step 6) over-counts a no-op apply; a `krateo.io/traceparent` patch means the 4.5 copy did
	// not neutralize it; any spec/other field is a genuine hidden driver. Off unless the env is set.
	if debugDiffEnabled() {
		for _, info := range target {
			key := infoKey(info)
			if absent[key] {
				c.debugLog("reconcile-diff: %s ABSENT (will create)", key)
				continue
			}
			patch, derr := liveToTargetMergePatch(liveObj[key], info.Object)
			if derr != nil {
				c.debugLog("reconcile-diff: %s diff-error: %v", key, derr)
				continue
			}
			if len(patch) > 0 && string(patch) != "{}" {
				c.debugLog("reconcile-diff: %s would-patch %s", key, string(patch))
			} else {
				c.debugLog("reconcile-diff: %s clean (no field diff; any RV bump is a no-op apply artifact)", key)
			}
		}
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
			if debugDiffEnabled() {
				// Correlate with the 4.7 diff: a child that logged "clean" there but bumped
				// RV here proves the merge/apply over-counts (RV moved with no real field diff).
				c.debugLog("reconcile-diff: %s RV-BUMPED %s -> %s (counted as changed)", key, snapshot[key], newRV)
			}
		}
	}

	changed := created > 0 || deleted > 0 || patched > 0
	if debugDiffEnabled() {
		c.debugLog("reconcile-diff: SUMMARY created=%d deleted=%d patched=%d changed=%v", created, deleted, patched, changed)
	}

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

// debugDiffEnabled reports whether the 4.7 per-child diff diagnostic is on.
func debugDiffEnabled() bool {
	v := os.Getenv("RECONCILE_DEBUG_DIFF")
	return v == "1" || v == "true" || v == "TRUE"
}

// liveToTargetMergePatch computes the JSON merge patch that would take live -> target after
// stripping fields that are never "real drift": volatile server metadata (resourceVersion,
// generation, uid, creationTimestamp, managedFields), helm-owned ownership metadata (the
// three-way merge preserves these regardless), and status. traceparent is deliberately KEPT so
// the diagnostic reveals whether the 4.5 copy neutralized it.
func liveToTargetMergePatch(live, target runtime.Object) ([]byte, error) {
	liveJSON, err := strippedJSON(live)
	if err != nil {
		return nil, fmt.Errorf("marshal live: %w", err)
	}
	targetJSON, err := strippedJSON(target)
	if err != nil {
		return nil, fmt.Errorf("marshal target: %w", err)
	}
	return jsonpatch.CreateMergePatch(liveJSON, targetJSON)
}

// strippedJSON marshals obj to JSON with never-drift fields removed (see liveToTargetMergePatch).
func strippedJSON(obj runtime.Object) ([]byte, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "status")
	if md, ok := m["metadata"].(map[string]interface{}); ok {
		for _, k := range []string{"resourceVersion", "generation", "uid", "creationTimestamp", "managedFields", "selfLink"} {
			delete(md, k)
		}
		if ann, ok := md["annotations"].(map[string]interface{}); ok {
			delete(ann, "meta.helm.sh/release-name")
			delete(ann, "meta.helm.sh/release-namespace")
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
		}
		if lbl, ok := md["labels"].(map[string]interface{}); ok {
			delete(lbl, "app.kubernetes.io/managed-by")
		}
	}
	return json.Marshal(m)
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

// annotationOf reads a single metadata annotation from a runtime.Object (typed or
// unstructured) via apimachinery's meta accessor. Returns "" when absent/unreadable.
func annotationOf(obj runtime.Object, key string) string {
	acc, err := apimeta.Accessor(obj)
	if err != nil {
		return ""
	}
	ann := acc.GetAnnotations()
	if ann == nil {
		return ""
	}
	return ann[key]
}

// setAnnotation sets a single metadata annotation on a runtime.Object (typed or
// unstructured) in place via apimachinery's meta accessor. No-op if the object has
// no accessible metadata. Mutating the target Info's Object here is reflected in the
// patch helm computes at merge time (helm re-encodes info.Object to build the diff).
func setAnnotation(obj runtime.Object, key, val string) {
	acc, err := apimeta.Accessor(obj)
	if err != nil {
		return
	}
	ann := acc.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[key] = val
	acc.SetAnnotations(ann)
}

// normalizeSecretStringData folds a core/v1 Secret's stringData into data in place,
// mirroring the apiserver's write-time behavior: each stringData[k] is base64-encoded
// into data[k] (stringData wins on collision) and stringData is removed. This keeps a
// stringData-authored Secret from reading as perpetual drift against its stored (data)
// representation. helm's kube Build yields *unstructured.Unstructured for every object,
// so a non-Secret / non-unstructured object is a no-op.
func normalizeSecretStringData(obj runtime.Object) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	if u.GetKind() != "Secret" || u.GetAPIVersion() != "v1" {
		return
	}
	stringData, found, err := unstructured.NestedStringMap(u.Object, "stringData")
	if err != nil || !found || len(stringData) == 0 {
		return
	}
	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	if data == nil {
		data = map[string]string{}
	}
	for k, v := range stringData {
		data[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	di := make(map[string]interface{}, len(data))
	for k, v := range data {
		di[k] = v
	}
	_ = unstructured.SetNestedMap(u.Object, di, "data")
	unstructured.RemoveNestedField(u.Object, "stringData")
}