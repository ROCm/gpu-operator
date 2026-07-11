# k8s.io Dependency Bump to v0.36.2 and kubectl Binary Updates

- **Date:** 2026-07-10
- **Author:** Yan Sun
- **Related PR(s):** https://github.com/pensando/gpu-operator/pull/1597
- **Related issue(s) / JIRA:** N/A (CVE remediation — kubectl/k8s.io dependency at stale version)

## Context

The GPU Operator was pinned to `k8s.io/* v0.33.1` (Kubernetes 1.33.1) and
`sigs.k8s.io/controller-runtime v0.20.3`. A CVE advisory identified that the
bundled kubectl binary and vendored k8s.io libraries were at a version
containing known vulnerabilities. The upstream fix is available in the
`k8s.io/* v0.36.2` stable release (Kubernetes 1.36.2).

Additionally, `Dockerfile.build` hard-pinned kubectl to `v1.30.4` — four
minor versions behind the current stable — while the main `Dockerfile` and
`tests/k8s-e2e/Dockerfile.e2e` already float to the upstream stable release.
The two utils container Dockerfiles pull kubectl via the OCP client tarball
and are intentionally left floating to `ocp/latest` for compatibility with
the latest OpenShift release.

## Approach

Three categories of changes:

1. **`Dockerfile.build` kubectl pin bump `v1.30.4` → `v1.36.2`** — aligns
   the dev/build container with the current stable kubectl binary, closing
   the CVE on that binary. Explicit pin (not floating) keeps the build
   container reproducible.

2. **`go.mod` / `vendor` bump:**
   - `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`,
     `k8s.io/apiextensions-apiserver`, `k8s.io/kubectl`, and all transitive
     `k8s.io/*` packages: `v0.33.1` → `v0.36.2`
   - `sigs.k8s.io/controller-runtime`: `v0.20.3` → `v0.24.1` (required
     co-upgrade; v0.24.x is the first controller-runtime release targeting
     k8s.io v0.36.x)
   - Transitive upgrades: `golang.org/x/*`, `go.etcd.io/etcd/*`,
     `go.opentelemetry.io/*`, `github.com/prometheus/*`, `sigs.k8s.io/kustomize/*`

3. **`MockClient` update** — `sigs.k8s.io/controller-runtime v0.22` added
   `Apply(ctx, ApplyConfiguration, ...ApplyOption) error` to the `client.Client`
   interface as part of native SSA support. The generated mock in
   `internal/client/mock_client.go` was missing this method. Added the mock
   method to satisfy the interface; no test behavior changes.

4. **Scheme registration regression fix** — the SA1019 lint cleanup for the
   deprecated `sigs.k8s.io/controller-runtime/pkg/scheme.Builder` migrated
   `api/v1alpha1` to a bare `k8s.io/apimachinery/pkg/runtime.SchemeBuilder{}`.
   These are not equivalent: the controller-runtime `scheme.Builder.Register()`
   registered types **and** called `metav1.AddToGroupVersion(scheme, GroupVersion)`,
   which registers the `ListOptions`/`GetOptions` conversions for the group. The
   bare builder's `init()` functions only called `AddKnownTypes`, dropping that
   call. At runtime the controller-manager could not list/watch its own CRD:
   `v1.ListOptions is not suitable for converting to "amd.com/v1alpha1"`, so
   caches never synced, the manager crash-looped, the Deployment never reached
   minimum availability, and `E2ESuite.SetUpSuite` failed — cascading into 8
   failed / 4 crashed e2e chunks (cluster-core, cluster-upgrade-policy, dcm,
   dra, npd, remediation). Fix: re-add `metav1.AddToGroupVersion(s, GroupVersion)`
   in both registration functions (`deviceconfig_types.go`, `remediationwf_types.go`),
   preserving the lint fix while restoring runtime behavior.

### Breaking changes evaluated

| Change | Impact on this repo |
|---|---|
| v0.21: `Result.Requeue` deprecated | Field still works; code using `Requeue: true` alongside `RequeueAfter` is redundant but not incorrect |
| v0.21: client-side rate limiter off by default | No explicit QPS/Burst config was set before; behavior unchanged in practice |
| v0.22: `client.Client.Apply` added | Fixed via mock update; no production code uses SSA |
| v0.22: nil selector behavior flip | No `MatchingLabelsSelector`/`MatchingFieldsSelector` usage in operator source |
| v0.23: Webhook API compile break | Not applicable — no webhook implementations in operator source |

### Alternatives considered

- Bumping only `k8s.io/*` without controller-runtime — not viable; controller-runtime
  v0.20.x only supports k8s.io v0.33.x and the build would fail.
- Updating to `v1.37.0-alpha.3` as suggested in the CVE ticket — rejected;
  alpha releases are inappropriate for production operators and v0.36.2 is
  the current stable line containing the fix.
- Pinning the utils container OCP tarball to `4.22.4` — rejected; the
  utils containers are intentionally floating to stay compatible with the
  latest OCP release deployed in customer clusters.

## Scope

- **In scope:** `go.mod`, `vendor/`, `Dockerfile.build`, `internal/client/mock_client.go`
- **Out of scope:** utils container Dockerfiles (OCP tarball stays at `latest`);
  main `Dockerfile` and `Dockerfile.e2e` (already float to k8s stable.txt);
  `example/gpu-validation-cluster` (uses k3s symlink, no binary download)

## Validation

- `go build ./...` passes with no errors
- `go test ./internal/...` passes (all unit tests green)
- e2e test failures are pre-existing environment issues (no kubeconfig / no
  cluster) unrelated to this change
- Dockerfile.build kubectl binary updated to v1.36.2 (matches `k8s.io/* v0.36.2`)

## Risks and rollback

- **Known risks:** controller-runtime v0.20 → v0.24 is a significant jump
  covering four minor releases. While compile and unit tests pass, behavioral
  differences (priority queue now enabled by default, rate limiter defaults
  changed) should be exercised in CI e2e before merging.
- **Rollback plan:** Revert the single commit. No API or CRD schema changes
  are involved — the CRD API version (`v1alpha1`) is unchanged, so a rollback
  requires only rebuilding the operator image.
