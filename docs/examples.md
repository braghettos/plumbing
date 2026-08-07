---
type: ExampleIndex
title: plumbing examples
description: Index of the runnable examples under examples/ — one line each.
resource: github.com/krateo-platformops/plumbing
tags: [examples]
timestamp: 2026-08-07T00:00:00Z
---

# Examples

- [examples/minimal](../examples/minimal/README.md) — feed a chart-style
  `values.schema.json` to `crdgen.Generate` and print the resulting CRD YAML
  (no cluster needed): `go run ./examples/minimal`.

The example is part of the module, so `go build ./...` at the repo root compiles it
and CI keeps it honest.
