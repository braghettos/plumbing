package helm

import (
	"testing"

	helmconfig "github.com/krateo-platformops/plumbing/helm"
	"helm.sh/helm/v3/pkg/action"
)

// Regression guard: applyUpgradeConfig used to omit TakeOwnership entirely, so a caller's opt-in to
// adopt pre-existing, un-owned resources was silently dropped on every Upgrade — Helm's ownership
// gate (existingResourceConflict -> "invalid ownership metadata") fired despite the opt-in. cdc sets
// UpgradeConfig.TakeOwnership=true on all its upgrade paths precisely to self-heal; it must reach the
// action.
func TestApplyUpgradeConfig_PropagatesTakeOwnership(t *testing.T) {
	up := &action.Upgrade{}
	applyUpgradeConfig(up, "ns", &helmconfig.UpgradeConfig{ActionConfig: &helmconfig.ActionConfig{TakeOwnership: true}})
	if !up.TakeOwnership {
		t.Fatal("applyUpgradeConfig dropped TakeOwnership=true — Helm's ownership gate will fire despite the opt-in")
	}

	off := &action.Upgrade{}
	applyUpgradeConfig(off, "ns", &helmconfig.UpgradeConfig{ActionConfig: &helmconfig.ActionConfig{TakeOwnership: false}})
	if off.TakeOwnership {
		t.Fatal("applyUpgradeConfig set TakeOwnership when the caller did not request it")
	}
}

// The Install path already honored it; keep both covered so the two apply* functions can't drift.
func TestApplyInstallConfig_PropagatesTakeOwnership(t *testing.T) {
	in := &action.Install{}
	applyInstallConfig(in, "rel", "ns", &helmconfig.InstallConfig{ActionConfig: &helmconfig.ActionConfig{TakeOwnership: true}})
	if !in.TakeOwnership {
		t.Fatal("applyInstallConfig dropped TakeOwnership=true")
	}
}
