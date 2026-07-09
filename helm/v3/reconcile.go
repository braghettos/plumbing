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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
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

	// 4. GET each target object's live state (for the change-detection diff in step 5) and
	//    capture its current krateo.io/traceparent (for the copy in step 4.5).
	//    Missing (IsNotFound) => absent, will be created by the merge.
	absent := make(map[string]bool, len(target))
	liveTraceparent := make(map[string]string, len(target))
	liveObj := make(map[string]runtime.Object, len(target))
	for _, info := range target {
		key := infoKey(info)
		helper := resource.NewHelper(info.Client, info.Mapping)
		live, gerr := helper.Get(info.Namespace, info.Name)
		if gerr != nil {
			// Treat any get failure (typically NotFound) as absent; the merge will
			// create it and surface a real error if the create fails.
			absent[key] = true
			continue
		}
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

	// 5. Change detection via SERVER-SIDE diff, BEFORE any mutation. "changed" must mean the
	// server-persisted state would actually differ — not merely that a re-apply bumps a
	// resourceVersion. The previous RV-delta signal over-counted server-normalized re-applies:
	// helm's kube merge patches (and thus bumps RV on) fields the apiserver then no-ops or
	// re-defaults — e.g. removing kubernetes.io/metadata.name from a Namespace (auto-restored),
	// or app.kubernetes.io/managed-by (helm-forced) — so a composition with such a child upgraded
	// a revision every cycle even at steady state (portal's demo-system Namespace, the umbrella's
	// managed-by). For each existing child we compute the three-way JSON merge patch
	// (stored-original, target, live); an empty patch means the declared state already matches
	// (live-only defaults are preserved -> no diff). A non-empty patch MIGHT still be a server
	// no-op, so we confirm it by dry-run applying the patch and comparing the server's would-be
	// result to live: equal => the apiserver normalizes it away (not a real change), differ => a
	// genuine change. This subsumes the per-field neutralizations (traceparent copy in 4.5 keeps
	// those children's patches empty; stringData fold in 4.6 likewise) and immunizes against ANY
	// server-owned/defaulted field. created/deleted are derived structurally.
	currentByKey := make(map[string]runtime.Object, len(current))
	for _, cinfo := range current {
		currentByKey[infoKey(cinfo)] = cinfo.Object
	}
	targetKeys := make(map[string]bool, len(target))
	for _, info := range target {
		targetKeys[infoKey(info)] = true
	}

	created, deleted, patched := 0, 0, 0
	for k := range currentByKey {
		if !targetKeys[k] {
			deleted++ // in the stored manifest but no longer rendered -> the merge deletes it
		}
	}
	for _, info := range target {
		key := infoKey(info)
		if absent[key] {
			created++ // not live (never created, or deleted out-of-band) -> the merge creates it
			continue
		}
		real, derr := c.childWouldChange(ctx, currentByKey[key], info, liveObj[key])
		if derr != nil {
			// Undetermined -> fail safe by treating it as a change (heal + reconcile).
			patched++
			if debugDiffEnabled() {
				c.debugLog("reconcile-diff: %s wouldChange error: %v (treated as changed)", key, derr)
			}
			continue
		}
		if real {
			patched++
		}
		if debugDiffEnabled() {
			c.debugLog("reconcile-diff: %s real=%v", key, real)
		}
	}

	changed := created > 0 || deleted > 0 || patched > 0
	if debugDiffEnabled() {
		c.debugLog("reconcile-diff: SUMMARY created=%d deleted=%d changed-children=%d changed=%v", created, deleted, patched, changed)
	}

	result := &helmconfig.ReconcileResult{
		Changed:        changed,
		Created:        created,
		Deleted:        deleted,
		PatchedUpdated: patched,
		Release:        stored,
	}

	if !changed {
		// Steady state: the server-persisted state already matches the rendered chart. No
		// mutation, no revision, no hooks.
		return result, nil
	}

	// 6. A real change was detected. Run the self-healing three-way merge to converge live to the
	// rendered chart: creates children deleted out-of-band, reverts field drift. UpdateThreeWayMerge
	// (NOT Update) so UNSTRUCTURED/CR children get helm's three-way-with-live merge; the concrete
	// *kube.Client implements the compat-split kube.InterfaceThreeWayMerge, so type-assert to reach
	// it (no fork), falling back to the 2-way Update if a KubeClient ever doesn't implement it.
	tw, ok := kc.(kube.InterfaceThreeWayMerge)
	if ok {
		_, err = tw.UpdateThreeWayMerge(current, target, cfg.Force)
	} else {
		_, err = kc.Update(current, target, cfg.Force)
	}
	if err != nil {
		return nil, fmt.Errorf("reconcile: kube update (self-heal apply): %w", err)
	}

	// 7. Persist a revision + run hooks (with correct ordering/delete-policy) via one real Upgrade.
	// Step 6 already converged the cluster, so this is a near-no-op re-apply.
	rel, err := c.Upgrade(ctx, releaseName, chartRef, cfg)
	if err != nil {
		return result, fmt.Errorf("reconcile: real upgrade after detected change: %w", err)
	}
	result.Release = rel
	return result, nil
}

// childWouldChange reports whether applying the rendered target to this live child would actually
// change the SERVER-PERSISTED state (a real change), as opposed to a server-normalized no-op that
// merely bumps resourceVersion.
//
// Cheap pre-filter: the three-way JSON merge patch (stored-original, target, live). An empty patch
// means the declared state already matches and live-only fields are preserved -> no change, skip
// the API round-trip. A NON-empty patch might still be a server no-op (e.g. it removes the
// apiserver-owned kubernetes.io/metadata.name label, or a Namespace's spec.finalizers, which the
// server re-adds/preserves), so we confirm the way `kubectl diff` does: a SERVER-SIDE APPLY dry-run
// of the target (not the raw merge patch — that would delete live-only fields the server keeps).
// SSA runs the apiserver's defaulting/admission/field-ownership merge, so the returned object is
// exactly what WOULD be stored; comparing it to live (ignoring purely volatile metadata) tells us
// if anything real would change. This is why the raw-merge-patch dry-run gave false positives on a
// Namespace: it applied "remove metadata.name/spec", which the server honored in the dry-run.
func (c *client) childWouldChange(ctx context.Context, original runtime.Object, info *resource.Info, live runtime.Object) (bool, error) {
	originalJSON := []byte("{}")
	if original != nil {
		b, err := json.Marshal(original)
		if err != nil {
			return false, fmt.Errorf("marshal original: %w", err)
		}
		originalJSON = b
	}
	targetJSON, err := json.Marshal(info.Object)
	if err != nil {
		return false, fmt.Errorf("marshal target: %w", err)
	}
	liveJSON, err := json.Marshal(live)
	if err != nil {
		return false, fmt.Errorf("marshal live: %w", err)
	}

	patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(originalJSON, targetJSON, liveJSON)
	if err != nil {
		return false, fmt.Errorf("three-way patch: %w", err)
	}
	if isEmptyPatch(patch) {
		return false, nil
	}

	// Non-empty patch: confirm via a server-side apply dry-run of the target (kubectl-diff style).
	force := true
	helper := resource.NewHelper(info.Client, info.Mapping)
	result, err := helper.Patch(info.Namespace, info.Name, types.ApplyPatchType, targetJSON, &metav1.PatchOptions{
		DryRun:       []string{metav1.DryRunAll},
		FieldManager: reconcileFieldManager,
		Force:        &force,
	})
	if err != nil {
		return false, fmt.Errorf("server-side apply dry-run: %w", err)
	}
	resStripped, err := strippedJSON(result)
	if err != nil {
		return false, fmt.Errorf("strip result: %w", err)
	}
	liveStripped, err := strippedJSON(live)
	if err != nil {
		return false, fmt.Errorf("strip live: %w", err)
	}
	rp, err := jsonpatch.CreateMergePatch(liveStripped, resStripped)
	if err != nil {
		return false, fmt.Errorf("compare patch: %w", err)
	}
	return !isEmptyPatch(rp), nil
}

// reconcileFieldManager is the field-manager used for the change-detection server-side-apply
// dry-run. It never persists (dry-run only), so it does not take real field ownership.
const reconcileFieldManager = "krateo-cdc-reconcile-diff"

// isEmptyPatch reports whether a JSON merge patch is a no-op ("" or "{}").
func isEmptyPatch(patch []byte) bool {
	s := strings.TrimSpace(string(patch))
	return s == "" || s == "{}"
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