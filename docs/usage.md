---
type: Usage
title: Using plumbing
description: go get, the explicit version-pinning conventions Krateo consumers follow, and how a plumbing bump propagates through core-provider into the installer.
resource: github.com/krateo-platformops/plumbing
tags: [go-get, versioning]
timestamp: 2026-08-07T00:00:00Z
---

# Usage

plumbing is consumed only as a Go module. There is nothing to deploy: no image, no
chart, no CRD.

## Install

```sh
go get github.com/krateo-platformops/plumbing@latest
```

or pin an exact tag (what every platform repo actually does):

```sh
go get github.com/krateo-platformops/plumbing@v1.13.2
```

The repo is public — no `GOPRIVATE`/auth setup is needed. Import any package
directly, e.g.:

```go
import (
    "github.com/krateo-platformops/plumbing/helm/v3"
    "github.com/krateo-platformops/plumbing/labels"
)
```

A compilable starting point is [examples/minimal](../examples/minimal/README.md)
(`go run ./examples/minimal` from the repo root).

## Version-pinning conventions (how Krateo consumers depend on this library)

- **Pin an exact release tag, never a pseudo-version of `main`.** Every consumer's
  `go.mod` carries `github.com/krateo-platformops/plumbing vX.Y.Z` (core-provider
  and composition-dynamic-controller both pin `v1.13.2` today). `main` is
  release-worthy but unreleased commits are not consumed.
- **Contract packages must be the SAME version on both sides.** `labels` (and any
  other cross-repo contract) only delivers its compile-time guarantee if
  core-provider and CDC resolve the same plumbing version; bumps to contract
  packages are propagated to all parties in one sweep.
- **Module identity is `krateo-platformops` (since v1.13.0).** The library migrated
  from the dead org as a major-independence move without a `/v2` suffix — the module
  path in `go.mod` is the truth; old-org import paths in downstream code are bugs to
  fix, not aliases.
- **A fix ships as a chain, not a commit.** The pattern for a plumbing fix reaching
  a cluster: tag plumbing `vX.Y.Z` → bump the consumer's `go.mod` (core-provider /
  CDC) and release it → bump the chart / installer pin. Nothing consumes plumbing
  at run time; only through released consumers.

## Build & test locally

```sh
go build ./...
make test          # fast unit suite
make test-race     # unit suite with -race (what CI runs)
make test-envtest  # functional cache-staleness tests against a real kube-apiserver
```

`make test-envtest` downloads a kubebuilder envtest control plane (pinned via the
[Makefile](../Makefile), see [configuration](./configuration.md) for the build
tags). Tests tagged `integration` additionally expect a reachable cluster and are
not run by CI.
