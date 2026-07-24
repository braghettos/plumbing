package crdgen

import (
	"fmt"
	"os"

	"github.com/krateoplatformops/plumbing/crdgen/coders"
	"github.com/krateoplatformops/plumbing/crdgen/tools"
	"github.com/krateoplatformops/plumbing/env"
)

// NormalizeVersionName re-exports coders.NormalizeVersionName so callers importing only the crdgen
// package can normalize a version string (e.g. an OAS info.version) to the CRD version name crdgen
// would emit for it (e.g. "1.2.3" -> "v1-2-3"). Use it when deriving a GVK/GVR that must match the
// generated CRD's version.
func NormalizeVersionName(ver string) string {
	return coders.NormalizeVersionName(ver)
}

type Options struct {
	Group        string
	Version      string
	Kind         string
	Categories   []string
	SpecSchema   []byte
	StatusSchema []byte
	Managed      bool
}

func Generate(opts Options) (dat []byte, err error) {
	// Direct JSON-Schema -> structural-OpenAPI-v3 transpiler (crdgen/transpile.go). Opt-in via
	// CRDGEN_TRANSPILER=direct while it is validated against the legacy Go-struct/controller-gen
	// path (see crdgen/docs/ref-resolution-redesign.md). Faithful for $ref-heavy schemas and gated
	// on the apiserver's own CRD validation.
	if os.Getenv("CRDGEN_TRANSPILER") == "direct" {
		return generateDirect(opts)
	}

	os.Setenv(coders.EnvFormatCode, "1")

	// Use MkdirTemp instead of TempDir to create a unique temporary directory
	// for each generation. This prevents race conditions when multiple
	// generations run concurrently.
	rootdir, err := os.MkdirTemp("", "crdgen-*")
	if err != nil {
		return
	}
	// Clean up the entire temporary directory after generation
	defer os.RemoveAll(rootdir)

	err = coders.GenAll(rootdir, &coders.Options{
		Group:        opts.Group,
		Version:      opts.Version,
		Kind:         opts.Kind,
		Categories:   opts.Categories,
		SpecSchema:   opts.SpecSchema,
		StatusSchema: opts.StatusSchema,
		Managed:      opts.Managed,
	})
	if err != nil {
		return
	}

	srcdir := coders.SourceDir(rootdir, opts.Kind)
	if env.True(coders.EnvKeepCode) {
		fmt.Fprintf(os.Stderr, "generated code dir: %s\n", srcdir)
	}

	err = tools.Tidy(srcdir)
	if err != nil {
		return
	}

	dat, err = tools.GenerateCRDs(srcdir)
	if err != nil {
		return
	}
	// Make controller-gen's output structurally valid: fill in `type: object` on
	// object-shaped nodes it left type-less, and turn empty/opaque nodes into open
	// objects. Without this the API server rejects the CRD for rich source schemas
	// (e.g. loft/vcluster) with "type: Required value: must not be empty ...".
	return sanitizeCRD(dat), nil
}
