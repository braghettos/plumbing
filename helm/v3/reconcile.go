package helm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
//  4. GET each target object's live state (the BEFORE snapshot for step 6) and
//     capture its live krateo.io/traceparent. Absent (IsNotFound) => will be
//     created. Then neutralize two volatile, non-semantic sources of per-render
//     diff so they cannot force a revision: copy the live traceparent onto the
//     target (4.5) and fold Secret stringData into data as the apiserver does (4.6).
//  5. KubeClient.UpdateThreeWayMerge(current, target, force) — helm's 3-way merge.
//     This CREATES resources deleted out-of-band and PATCHES drifted ones,
//     converging the cluster. The merge refreshes every target Info's Object in
//     place to its POST-merge server state (patch branch: Refresh; no-op: Get).
//  6. changed := any Created || any Deleted || any surviving target whose content
//     actually changed. We compare each child's BEFORE (step 4 live) and AFTER
//     (step 5 refreshed) state with write-volatile and server-owned fields
//     stripped (see strippedJSON/semanticallyChanged) — NOT a resourceVersion
//     delta and NOT len(Result.Updated), both of which over-count server-owned
//     no-op re-applies (a Namespace's apiserver-restored metadata.name, a
//     helm-forced managed-by) and churned a revision every cycle at steady state.
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
	actionConfig, err := c.newActionConfig(ctx, c.namespace, c.restConfig, cfg.ActionConfig)
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

	// 4.7. Drop "current" resources superseded by a GVK migration. When a child changes
	// apiVersion between the stored release and the fresh render — e.g. a CRD version bump
	// (composition.krateo.io/v1-2-2 -> v1-3-2) after a component version change — the stored
	// manifest still pins the OLD GVK while the target carries the NEW one. Left in `current`,
	// helm's three-way merge sees the old-GVK entry as "removed" and tries to DELETE it, but the
	// old CRD version is no longer served, so the delete fails NotFound and the whole reconcile
	// errors — wedging this AND every future reconcile, because the stored manifest never
	// advances past the dead GVK. These are the SAME logical object migrating versions: the
	// target's new-GVK entry updates the migrated live object (helm GETs each target and patches
	// when it exists), so the stale old-GVK entry must be skipped, not deleted. Match on
	// Kind+Namespace+Name (version-independent) with a differing apiVersion.
	if len(current) > 0 {
		targetGVK := make(map[string]string, len(target))
		for _, info := range target {
			gvk := info.Mapping.GroupVersionKind
			targetGVK[gvk.Kind+"\x00"+info.Namespace+"\x00"+info.Name] = gvk.GroupVersion().String()
		}
		kept := make(kube.ResourceList, 0, len(current))
		for _, info := range current {
			gvk := info.Mapping.GroupVersionKind
			if tv, ok := targetGVK[gvk.Kind+"\x00"+info.Namespace+"\x00"+info.Name]; ok && tv != gvk.GroupVersion().String() {
				continue // superseded by a GVK migration — target updates the migrated live object
			}
			kept = append(kept, info)
		}
		current = kept
	}

	// 5. Self-healing merge: creates children deleted out-of-band, reverts field drift. This is the
	// only mutation. UpdateThreeWayMerge (NOT Update) so UNSTRUCTURED/CR children get helm's
	// three-way-with-live merge; the concrete *kube.Client implements the compat-split
	// kube.InterfaceThreeWayMerge, so type-assert to reach it (no fork), falling back to the 2-way
	// Update if a KubeClient ever doesn't implement it. helm refreshes every target Info's Object to
	// its POST-merge server state (patch branch: Refresh; no-op branch: Get) — step 6 relies on that.
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

	// 6. Decide whether the server-PERSISTED state actually changed — not merely whether the merge
	// bumped a resourceVersion. The old RV-delta over-counted: helm's merge patches (and bumps RV
	// on) fields the apiserver then no-ops or re-defaults — removing a Namespace's server-owned
	// kubernetes.io/metadata.name (auto-restored) or app.kubernetes.io/managed-by (helm-forced) —
	// so a composition with such a child churned a helm revision every cycle at steady state
	// (portal's demo-system Namespace, the umbrella's managed-by). Instead we compare each child's
	// content BEFORE (liveObj, step 4) and AFTER (info.Object, refreshed by the merge above) with
	// write-volatile + server-owned metadata stripped (see strippedJSON): identical => the merge
	// fought a server-owned field and the apiserver reverted it — a no-op, not a real change;
	// different => a genuine create/heal. This uses helm's OWN merge (no separate dry-run, so no
	// patch-type mismatch) and immunizes against ANY server-owned/defaulted field. It also subsumes
	// the per-field neutralizations (traceparent copy in 4.5, stringData fold in 4.6) — those keep
	// the before/after identical for their children. created/deleted come from the merge result.
	created := len(res.Created)
	deleted := len(res.Deleted)
	patched := 0
	for _, info := range target {
		key := infoKey(info)
		if absent[key] {
			continue // counted under res.Created
		}
		changedChild, cerr := semanticallyChanged(liveObj[key], info.Object)
		if cerr != nil {
			// Undetermined -> fail safe by treating it as a change.
			patched++
			continue
		}
		if changedChild {
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
		// Steady state: the merge only touched server-owned fields the apiserver reverted. No
		// revision, no hooks.
		return result, nil
	}

	// 7. A real change happened. Run ONE real Upgrade to persist a revision and run hooks with
	// correct ordering + delete-policy. Step 5 already converged the cluster, so this is a near-no-op.
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
// semanticallyChanged reports whether a child's content actually changed across the merge — i.e.
// whether `before` (the live object read in step 4) and `after` (the same object refreshed by the
// merge in step 5) differ once write-volatile and server-owned metadata are stripped (see
// strippedJSON). A merge that only re-added an apiserver-owned field (kubernetes.io/metadata.name)
// or re-forced app.kubernetes.io/managed-by leaves before == after here (both already carried the
// server value), so it reads as no-change even though the resourceVersion bumped; a real create or
// drift-heal leaves them different.
func semanticallyChanged(before, after runtime.Object) (bool, error) {
	beforeJSON, err := strippedJSON(before)
	if err != nil {
		return false, fmt.Errorf("strip before: %w", err)
	}
	afterJSON, err := strippedJSON(after)
	if err != nil {
		return false, fmt.Errorf("strip after: %w", err)
	}
	patch, err := jsonpatch.CreateMergePatch(beforeJSON, afterJSON)
	if err != nil {
		return false, fmt.Errorf("compare: %w", err)
	}
	return !isEmptyPatch(patch), nil
}

// isEmptyPatch reports whether a JSON merge patch is a no-op ("" or "{}").
func isEmptyPatch(patch []byte) bool {
	s := strings.TrimSpace(string(patch))
	return s == "" || s == "{}"
}

// strippedJSON marshals obj to JSON with never-drift fields removed (see semanticallyChanged):
// volatile server metadata (resourceVersion, generation, uid, creationTimestamp, managedFields),
// helm-owned ownership metadata (the three-way merge preserves these regardless), and status.
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
