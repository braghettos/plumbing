package helm

import (
	"errors"
	"testing"

	"helm.sh/helm/v3/pkg/release"
)

func relWithStatus(s release.Status) *release.Release {
	return &release.Release{Info: &release.Info{Status: s}}
}

// selectCurrentForBuild must pick the exact revision helm's Upgrade builds as "current"
// (upgrade.go prepareUpgrade). The wedge fixed here: a `failed` revision on top of a `deployed`
// one made the old code repair Last() (failed) while helm builds Deployed() — so the manifest
// referencing a pruned child apiVersion never got repaired and reconcile stayed dead-locked.
func TestSelectCurrentForBuild(t *testing.T) {
	deployed := relWithStatus(release.StatusDeployed)
	failedLast := relWithStatus(release.StatusFailed)

	// last is Deployed -> use last, and never query Deployed().
	called := false
	if got := selectCurrentForBuild(deployed, func() (*release.Release, error) { called = true; return nil, nil }); got != deployed || called {
		t.Fatalf("last=deployed: want last without querying deployed (got=%v called=%v)", got, called)
	}

	// last is failed but a Deployed revision exists -> use Deployed (the revision helm builds).
	if got := selectCurrentForBuild(failedLast, func() (*release.Release, error) { return deployed, nil }); got != deployed {
		t.Fatal("last=failed + deployed present: want the deployed revision (the wedge bug repaired the wrong one)")
	}

	// last is failed and no Deployed revision -> fall back to last (matches helm's fallback).
	if got := selectCurrentForBuild(failedLast, func() (*release.Release, error) { return nil, errors.New("no deployed release") }); got != failedLast {
		t.Fatal("last=failed + no deployed: want last (helm fallback)")
	}

	// nil Info must not panic and is treated as not-Deployed.
	if got := selectCurrentForBuild(&release.Release{}, func() (*release.Release, error) { return deployed, nil }); got != deployed {
		t.Fatal("last with nil Info: want deployed")
	}
}
