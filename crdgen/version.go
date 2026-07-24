package crdgen

import (
	"regexp"
	"strings"
)

var nonAlphaNumVersion = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeVersionName converts an arbitrary version string (e.g. an OAS info.version like "1.2.3")
// into the legal Kubernetes CRD version name crdgen emits for it (e.g. "v1-2-3"). Callers deriving a
// GVK/GVR outside crdgen MUST use this so their version matches the generated CRD's version name.
func NormalizeVersionName(ver string) string {
	return normalizeVersion(ver, '-')
}

// normalizeVersion lowercases ver, replaces every run of non-alphanumerics with replaceChar, trims
// that char from the ends, and prefixes "v" when the result starts with a digit. For a Kubernetes
// CRD version name use replaceChar '-' (see NormalizeVersionName); '-' is the only separator a CRD
// version name allows ([a-z]([-a-z0-9]*[a-z0-9])?).
func normalizeVersion(ver string, replaceChar rune) string {
	ver = strings.ToLower(ver)
	ver = nonAlphaNumVersion.ReplaceAllString(ver, string(replaceChar))
	ver = strings.Trim(ver, string(replaceChar))
	if len(ver) > 0 && ver[0] >= '0' && ver[0] <= '9' {
		ver = "v" + ver
	}
	return ver
}
