package crdgen

import "testing"

// TestNormalizeVersionName covers the re-export consumers import from the crdgen package: it must produce
// the same legal k8s CRD version name that crdgen emits internally for a given version string.
func TestNormalizeVersionName(t *testing.T) {
	cases := map[string]string{
		"1.2.3":       "v1-2-3",
		"v1":          "v1",
		"2.0.0-beta":  "v2-0-0-beta",
		"2024-01-01":  "v2024-01-01",
		"v1alpha1":    "v1alpha1",
		"":            "",
		"release/1.0": "release-1-0",
	}
	for in, want := range cases {
		if got := NormalizeVersionName(in); got != want {
			t.Errorf("NormalizeVersionName(%q) = %q, want %q", in, got, want)
		}
	}
}
