//go:build integration
// +build integration

package helm

import (
	"context"
	"fmt"
	"testing"

	helmconfig "github.com/krateoplatformops/plumbing/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const driftHooksChartDir = "testdata/charts/drift-hooks"

// helmRevisionCount returns the number of stored helm release revisions
// (secret-backed) for releaseName. A no-op reconcile must NOT increase it.
func helmRevisionCount(t *testing.T, cs *kubernetes.Clientset, ns, releaseName string) int {
	t.Helper()
	list, err := cs.CoreV1().Secrets(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("name=%s,owner=helm", releaseName),
	})
	require.NoError(t, err)
	return len(list.Items)
}

// hookFireCount returns the number of hook-rev-* ConfigMaps, i.e. how many
// times the pre-upgrade hook has fired for this release.
func hookFireCount(t *testing.T, cs *kubernetes.Clientset, ns string) int {
	t.Helper()
	list, err := cs.CoreV1().ConfigMaps(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: "drift-hooks/hook=pre-upgrade",
	})
	require.NoError(t, err)
	return len(list.Items)
}

func liveColor(t *testing.T, cs *kubernetes.Clientset, ns, name string) string {
	t.Helper()
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return cm.Data["color"]
}

func liveTraceparent(t *testing.T, cs *kubernetes.Clientset, ns, name string) string {
	t.Helper()
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return cm.Annotations["krateo.io/traceparent"]
}

// TestControllerReconcile proves the fork-free apply-if-changed Reconcile on a
// real kind cluster: (c) FIELD DRIFT self-heal and (d) HOOKS ONLY ON CHANGE.
func TestControllerReconcile(t *testing.T) {
	f := features.New("Reconcile apply-if-changed").
		Assess("field-drift self-heal and hooks-only-on-change", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			cs, err := kubernetes.NewForConfig(c.Client().RESTConfig())
			require.NoError(t, err)

			cli, err := NewClient(c.Client().RESTConfig(),
				WithCache(),
				WithLogger(func(format string, v ...interface{}) {
					fmt.Println(fmt.Sprintf(format, v...))
				}),
				WithNamespace(namespace),
			)
			require.NoError(t, err)
			defer cli.Close()

			chartURL := chartArchiveURL(t, driftHooksChartDir)
			releaseName := "drift-hooks"
			cmName := releaseName + "-config"

			upCfg := func() *helmconfig.UpgradeConfig {
				return &helmconfig.UpgradeConfig{
					ActionConfig: &helmconfig.ActionConfig{
						TakeOwnership: true,
						Values: map[string]interface{}{
							"color": "blue",
						},
					},
				}
			}

			// --- Initial install (revision 1). ---
			rel, err := cli.Install(ctx, releaseName, chartURL, &helmconfig.InstallConfig{ActionConfig: upCfg().ActionConfig})
			require.NoError(t, err)
			require.Equal(t, 1, rel.Revision)
			t.Cleanup(func() { _ = cli.Uninstall(context.Background(), releaseName, &helmconfig.UninstallConfig{IgnoreNotFound: true}) })

			require.Equal(t, "blue", liveColor(t, cs, namespace, cmName))
			revAfterInstall := helmRevisionCount(t, cs, namespace, releaseName)
			hooksAfterInstall := hookFireCount(t, cs, namespace)
			t.Logf("[install] revisions=%d hookFires=%d color=%s", revAfterInstall, hooksAfterInstall, liveColor(t, cs, namespace, cmName))

			// --- Steady-state reconcile (no drift): must be a no-op. ---
			res, err := cli.Reconcile(ctx, releaseName, chartURL, upCfg())
			require.NoError(t, err)
			assert.False(t, res.Changed, "steady-state reconcile must not report a change")
			revSteady := helmRevisionCount(t, cs, namespace, releaseName)
			hooksSteady := hookFireCount(t, cs, namespace)
			t.Logf("[steady] Changed=%v revisions=%d (was %d) hookFires=%d (was %d)",
				res.Changed, revSteady, revAfterInstall, hooksSteady, hooksAfterInstall)
			assert.Equal(t, revAfterInstall, revSteady, "no-op reconcile must not add a helm revision")
			assert.Equal(t, hooksAfterInstall, hooksSteady, "no-op reconcile must not fire the pre-upgrade hook")

			// =========================================================
			// (c) FIELD DRIFT: mutate data.color out-of-band, reconcile must patch it back.
			// =========================================================
			cm, err := cs.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
			require.NoError(t, err)
			t.Logf("[drift] color BEFORE out-of-band mutation = %q", cm.Data["color"])
			cm.Data["color"] = "red-DRIFTED"
			_, err = cs.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.Equal(t, "red-DRIFTED", liveColor(t, cs, namespace, cmName))
			t.Logf("[drift] color AFTER out-of-band mutation = %q", liveColor(t, cs, namespace, cmName))

			revBeforeHeal := helmRevisionCount(t, cs, namespace, releaseName)
			hooksBeforeHeal := hookFireCount(t, cs, namespace)

			res, err = cli.Reconcile(ctx, releaseName, chartURL, upCfg())
			require.NoError(t, err)
			assert.True(t, res.Changed, "reconcile must detect+heal the field drift")
			assert.GreaterOrEqual(t, res.PatchedUpdated, 1, "the drifted ConfigMap must be patched")
			healedColor := liveColor(t, cs, namespace, cmName)
			revAfterHeal := helmRevisionCount(t, cs, namespace, releaseName)
			hooksAfterHeal := hookFireCount(t, cs, namespace)
			t.Logf("[heal] Changed=%v patched=%d color=%q revisions=%d (was %d) hookFires=%d (was %d)",
				res.Changed, res.PatchedUpdated, healedColor, revAfterHeal, revBeforeHeal, hooksAfterHeal, hooksBeforeHeal)

			assert.Equal(t, "blue", healedColor, "field drift must be self-healed back to declared value")
			assert.Equal(t, revBeforeHeal+1, revAfterHeal, "a real change must bump the helm revision exactly once")
			// (d) the real change fired the pre-upgrade hook exactly once.
			assert.Equal(t, hooksBeforeHeal+1, hooksAfterHeal, "a real change must fire the pre-upgrade hook exactly once")

			// --- Following steady-state reconcile: no further bump, no further hook. ---
			res, err = cli.Reconcile(ctx, releaseName, chartURL, upCfg())
			require.NoError(t, err)
			assert.False(t, res.Changed, "post-heal steady-state reconcile must be a no-op")
			revSettled := helmRevisionCount(t, cs, namespace, releaseName)
			hooksSettled := hookFireCount(t, cs, namespace)
			t.Logf("[settled] Changed=%v color=%q revisions=%d (was %d) hookFires=%d (was %d)",
				res.Changed, liveColor(t, cs, namespace, cmName), revSettled, revAfterHeal, hooksSettled, hooksAfterHeal)
			assert.Equal(t, revAfterHeal, revSettled, "steady-state after heal must not add a revision")
			assert.Equal(t, hooksAfterHeal, hooksSettled, "steady-state after heal must not fire the hook again")

			// =========================================================
			// (d) HOOKS ONLY ON CHANGE — explicit second real change.
			// A no-op loop (5x) must not fire the hook; a genuine value change must.
			// =========================================================
			hooksPreLoop := hookFireCount(t, cs, namespace)
			revPreLoop := helmRevisionCount(t, cs, namespace, releaseName)
			for i := 0; i < 5; i++ {
				r, err := cli.Reconcile(ctx, releaseName, chartURL, upCfg())
				require.NoError(t, err)
				assert.False(t, r.Changed, "no-op reconcile #%d must not change", i)
			}
			hooksPostLoop := hookFireCount(t, cs, namespace)
			revPostLoop := helmRevisionCount(t, cs, namespace, releaseName)
			t.Logf("[hook-loop] after 5 no-op reconciles: hookFires %d->%d revisions %d->%d",
				hooksPreLoop, hooksPostLoop, revPreLoop, revPostLoop)
			assert.Equal(t, hooksPreLoop, hooksPostLoop, "5 no-op reconciles must not fire the hook")
			assert.Equal(t, revPreLoop, revPostLoop, "5 no-op reconciles must not add revisions")

			// Now a genuine declared-value change (color blue->green): hook must fire once.
			changeCfg := &helmconfig.UpgradeConfig{
				ActionConfig: &helmconfig.ActionConfig{
					TakeOwnership: true,
					Values:        map[string]interface{}{"color": "green"},
				},
			}
			res, err = cli.Reconcile(ctx, releaseName, chartURL, changeCfg)
			require.NoError(t, err)
			assert.True(t, res.Changed, "declared value change must be detected")
			hooksPostChange := hookFireCount(t, cs, namespace)
			t.Logf("[hook-change] color now %q hookFires %d->%d",
				liveColor(t, cs, namespace, cmName), hooksPostLoop, hooksPostChange)
			assert.Equal(t, "green", liveColor(t, cs, namespace, cmName))
			assert.Equal(t, hooksPostLoop+1, hooksPostChange, "a genuine change must fire the hook exactly once")

			// =========================================================
			// (e) TRACEPARENT NO-CHURN — a change confined to the volatile
			// krateo.io/traceparent annotation must be a no-op. This is the exact
			// interaction that made snowplow + the installer umbrella keep churning:
			// the post-renderer stamps a fresh traceparent on every *.krateo.io child
			// each render, and without step 4.5 the three-way merge patches it every
			// cycle (revision++). The chart's ConfigMap now carries
			// krateo.io/traceparent={{ .Values.traceparent }} to simulate that stamp.
			// =========================================================
			cfgTP := func(color, tp string) *helmconfig.UpgradeConfig {
				return &helmconfig.UpgradeConfig{
					ActionConfig: &helmconfig.ActionConfig{
						TakeOwnership: true,
						Values:        map[string]interface{}{"color": color, "traceparent": tp},
					},
				}
			}

			// Establish a baseline traceparent (real change: "" -> TP-A).
			res, err = cli.Reconcile(ctx, releaseName, chartURL, cfgTP("green", "TP-A"))
			require.NoError(t, err)
			assert.True(t, res.Changed, "adding the first traceparent value is a real change")
			assert.Equal(t, "TP-A", liveTraceparent(t, cs, namespace, cmName))
			revBaseTP := helmRevisionCount(t, cs, namespace, releaseName)
			hooksBaseTP := hookFireCount(t, cs, namespace)

			// Now render a DIFFERENT traceparent each cycle with NO other change.
			// Every one must be a no-op: no revision, no hook, and the live value must
			// stay at the baseline (step 4.5 copies live TP-A onto the target).
			for i, tp := range []string{"TP-B", "TP-C", "TP-D", "TP-E", "TP-F"} {
				r, err := cli.Reconcile(ctx, releaseName, chartURL, cfgTP("green", tp))
				require.NoError(t, err)
				assert.False(t, r.Changed, "traceparent-only reconcile #%d (render=%s) must be a no-op", i, tp)
			}
			revAfterTP := helmRevisionCount(t, cs, namespace, releaseName)
			hooksAfterTP := hookFireCount(t, cs, namespace)
			liveTP := liveTraceparent(t, cs, namespace, cmName)
			t.Logf("[traceparent] after 5 differing-traceparent reconciles: revisions %d->%d hookFires %d->%d liveTraceparent=%q",
				revBaseTP, revAfterTP, hooksBaseTP, hooksAfterTP, liveTP)
			assert.Equal(t, revBaseTP, revAfterTP, "traceparent-only churn must NOT add helm revisions")
			assert.Equal(t, hooksBaseTP, hooksAfterTP, "traceparent-only churn must NOT fire hooks")
			assert.Equal(t, "TP-A", liveTP, "live traceparent must stay at baseline (never patched by the volatile render)")

			// Contrast: a real change alongside a new traceparent must still go through
			// (and the actual upgrade re-stamps traceparent to the new value).
			res, err = cli.Reconcile(ctx, releaseName, chartURL, cfgTP("yellow", "TP-REAL"))
			require.NoError(t, err)
			assert.True(t, res.Changed, "a real data change must be detected even amid a traceparent change")
			assert.Equal(t, "yellow", liveColor(t, cs, namespace, cmName))
			assert.Equal(t, "TP-REAL", liveTraceparent(t, cs, namespace, cmName), "a real upgrade re-stamps the fresh traceparent")
			assert.Equal(t, revAfterTP+1, helmRevisionCount(t, cs, namespace, releaseName), "the real change bumps the revision exactly once")

			return ctx
		}).
		Feature()

	testenv.Test(t, f)
}