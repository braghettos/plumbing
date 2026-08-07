---
type: Configuration
title: plumbing configuration
description: The whole config surface the library itself reads — two runtime env vars, one conventional env-key constant, and the test-only build tags.
resource: github.com/krateo-platformops/plumbing
tags: [env, build-tags]
timestamp: 2026-08-07T00:00:00Z
---

# Configuration

plumbing is a library: it has no config file, no flags, no values. Configuration is
whatever each package's `Options`/functional-option arguments say (see
[api](./api.md)). The library itself reads exactly this from the environment:

| Env var | Read by | Effect |
|---|---|---|
| `HELM_DRIVER` | `helm/v3` ([client.go](../helm/v3/client.go)) | Helm storage driver for release records (`secret` when unset — the Helm SDK default). |
| `CLUSTER_NAME` | `kubeutil` ([detect_cluster.go](../kubeutil/detect_cluster.go)) | First choice for `DetectClusterName`; falls back to the rest-config host, then the local hostname. |
| `KUBERNETES_SERVICE_HOST` / `KUBERNETES_SERVICE_PORT` | `signup` ([signup.go](../signup/signup.go)) | In-cluster apiserver URL when `Options.ServerURL` is empty (standard in-cluster env). |

One near-miss worth stating: `jwtutil.JwtSecretEnvKey` (`AUTHN_JWT_SECRET`) is a
**shared env-key NAME constant**, not an env read — `CreateToken` takes the signing
key via `CreateTokenOptions.SigningKey` and errors if it is empty; consumers
(authn, snowplow) read the env var themselves under this agreed name.

The `env` package (`env.Bool`, `env.String`, `env.Duration`, …) is a typed
env-reading helper **for consumers**; it does not make the library read anything on
its own.

## Build tags

No non-test code carries a build tag. Test-only:

| Tag | Selects | Needs |
|---|---|---|
| `envtest` | the functional cache-staleness tests in `helm/v3` | a kubebuilder envtest control plane (`make test-envtest` installs it, pinned in the [Makefile](../Makefile)) |
| `integration` | tests in `endpoints` and `helm/v3` that hit a real cluster | a reachable kubeconfig cluster; not run in CI |
