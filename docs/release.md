---
type: Runbook
title: Releasing plumbing
description: How a plumbing release ships — tag-only Go module (plain vX.Y.Z git tags, no OCI artifact, no release workflow), and how a bump reaches consumers.
resource: github.com/krateo-platformops/plumbing
tags: [release, tags]
timestamp: 2026-08-07T00:00:00Z
---

# Release runbook

plumbing is a **tag-only Go module**. There is no release workflow, no image, no
chart, no OCI artifact — a release is a semver git tag on `main`, served to
consumers by the Go module proxy. This is the reality of the repo today: the only
workflow is [test.yaml](../.github/workflows/test.yaml) (unit + envtest on every PR
and push to `main`).

## Convention (derived from the existing tags)

- Tags are `vMAJOR.MINOR.PATCH`, **with** the `v` prefix (`v1.13.2` is current;
  the line runs back through `v1.0.0`). Note this differs from the platform's
  chart/image repos, whose tags carry no `v` — Go modules require it.
- Minor bumps for new packages/features (`v1.12.0` added five kubeutil packages),
  patch bumps for fixes (`v1.13.1` objectclient Apply fix). Contract-affecting
  changes (e.g. `labels`) always get a tag consumers can sweep to.
- Historical maintenance tags exist off-`main` (the `v1.7.x` line was extended
  through `v1.7.16` while `main` was ahead); prefer fixing on `main` and bumping
  consumers unless a consumer is pinned to an old line for cause.
- The module identity is `github.com/krateo-platformops/plumbing` since `v1.13.0`
  (org-independence migration) with no `/v2` path suffix.

## Ship a release

1. Land the change on `main` via PR; `test` (unit+race, envtest) and `lint-docs`
   must be green.
2. Tag and push:

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

3. There is no step 3 in this repo. Propagation is the consumer chain: bump
   `go.mod` in core-provider / composition-dynamic-controller (and any other
   consumer), release those, then bump the chart/installer pins — see
   [usage](./usage.md).

## Docs freshness

`docs/llms.txt` pins this bundle to the release tag; when tagging, update the pin
and review the core files' `timestamp` (CI warns when a doc timestamp trails the
latest tag).
