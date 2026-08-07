---
type: Library
title: plumbing — index
description: The map of the plumbing doc bundle — the shared Go library (helm client + reconcile engine, kubeutil, crdgen, jwtutil, labels and friends) consumed by the Krateo platform components.
resource: github.com/krateo-platformops/plumbing
tags: [library, helm, crdgen, kubeutil]
timestamp: 2026-08-07T00:00:00Z
---

# plumbing

plumbing is **the shared Go library of the Krateo platform**: reusable building
blocks that reduce boilerplate and — more importantly — pin cross-repo contracts in
one place. It carries the Helm client with the fork-free **apply-if-changed
`Reconcile` engine** that composition-dynamic-controller runs on, the
**`crdgen`** JSON-Schema → CRD transpiler that core-provider uses to turn a chart's
`values.schema.json` into a versioned CRD, the **`labels`** package that makes the
core-provider ↔ CDC composition-label contract a compile-time guarantee, plus
`kubeutil/*` controller helpers, `jwtutil`, and small utilities. It is a library
only: no binary, no image, no chart — releases are plain git tags.

## The bundle (start here)

- [overview](./overview.md) — package design: the helm engine (and the
  `Reconcile` flow step by step), crdgen, the kubeutil family, and how
  core-provider/CDC consume them.
- [usage](./usage.md) — `go get`, the version-pinning conventions consumers follow,
  and the bump chain into core-provider and the installer.
- [configuration](./configuration.md) — the (small) config surface the library
  itself reads: two env vars and two test build tags.
- [api](./api.md) — the exported package map, one line per package, with
  pkg.go.dev links.
- [examples](./examples.md) — the runnable example under `examples/`.
- [release](./release.md) — how a release ships (tag-only, no OCI artifact).
- [log](./log.md) — curated history.
- [llms.txt](./llms.txt) — the version-pinned agent index of this bundle.

## Deeper, code-adjacent docs

- [crdgen/docs/ref-resolution-redesign.md](../crdgen/docs/ref-resolution-redesign.md)
  — the design record (RFC, implemented) of the direct JSON-Schema →
  structural-OpenAPI-v3 transpiler that replaced the legacy crdgen path.
