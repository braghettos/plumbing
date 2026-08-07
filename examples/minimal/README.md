---
type: Example
title: minimal — generate a CRD from a chart's values.schema.json
description: Runnable example that feeds a JSON Schema to crdgen.Generate and prints the resulting CustomResourceDefinition YAML, the same transformation core-provider performs per CompositionDefinition.
resource: github.com/krateo-platformops/plumbing/crdgen
tags: [example, crdgen]
timestamp: 2026-08-07T00:00:00Z
---

# minimal

Feeds a chart-style `values.schema.json` (inline JSON Schema) to
[`crdgen.Generate`](../../crdgen/crdgen.go) and prints the resulting
`CustomResourceDefinition` YAML — the exact transformation the core-provider engine
performs for every `CompositionDefinition`. It also shows
`crdgen.NormalizeVersionName` (chart version `1.0.0` → CRD version `v1-0-0`) and the
`Managed` option (conditioned status subresource).

## Preconditions

- Go (version per [`go.mod`](../../go.mod)). No cluster, no network beyond the module cache.

## Run

```sh
go run ./examples/minimal
```

Expected output: a structurally valid CRD named `dummyapps.examples.krateo.io` with a
single served version `v1-0-0`.
