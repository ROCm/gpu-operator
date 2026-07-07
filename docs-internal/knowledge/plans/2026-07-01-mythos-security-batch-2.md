# Mythos AI-scan security fixes — batch 2 (KUBE-34, KUBE-32)

- **Date:** 2026-07-01
- **Author:** Nitish Bhat
- **Related PR(s):** pensando #1571 (ROCm counterpart: ROCm/gpu-operator #589)
- **Related issue(s) / JIRA:** KUBE-34, KUBE-32

## Context

Second batch of Mythos AI security-scan fixes for the GPU operator.
Both findings apply to the pensando fork.

## Approach

- **KUBE-34** — the RHEL KMM driver-build template passed the Red Hat
  subscription password via `subscription-manager --password` inside a
  `RUN` layer (would land in the build ConfigMap, process table, image
  history, and logs; `; exit 0` masked failures). The path was already
  dead: the `"rhel"` case in `resolveDockerfile` was commented out,
  `DockerfileTemplate.rhel` was not embedded or referenced, and the
  `RedhatSubscription*` CRD fields it read no longer exist, so
  `osDistro == "rhel"` already fell through to "not supported OS".
  Per team confirmation RHEL support is not being revived, so the dead
  template and commented block are removed.
- **KUBE-32** — the KMM driver-build templates fetch the apt/dnf GPG
  trust root from a URL that can be supplied via `DeviceConfig`
  (`gpgKeyURL`/`packageRepoURL`/`amdgpuInstallerRepoURL`). This is by
  design (optional fields for custom-mirror / air-gapped installs), and
  the only actor who can set them already controls the driver's package
  source via DeviceConfig write access. The scan's suggested fix (reject
  a CR-supplied key URL) would break air-gapped support. Instead, the
  trust model is documented on the three fields, and the CRD / helm CRD /
  CSV descriptors are regenerated to match.

### Alternatives considered

- KUBE-34: harden the RHEL template in place (BuildKit secret mount +
  activation keys) — rejected; the path is disabled and does not compile,
  so this would be speculative, untestable code.
- KUBE-32: remove `gpgKeyURL` and hardcode the AMD key (the scan's
  recommendation) — rejected; it would break the air-gapped/custom-mirror
  feature this field was added for.

## Scope

- **In scope:** removal of `DockerfileTemplate.rhel` + the dead RHEL
  block in `internal/kmmmodule/kmmmodule.go`; doc-only additions to the
  three URL fields in `api/v1alpha1/deviceconfig_types.go` and the
  regenerated CRD/helm-CRD/CSV/bundle artifacts.
- **Out of scope:** any behavior change to the driver build; reviving
  RHEL support.

## Validation

- Unit tests: `go build ./...`, `go vet ./internal/kmmmodule/`, and
  `go test ./internal/kmmmodule/` pass.
- Integration / e2e tests: generated artifacts produced by
  `make manifests` + `make bundle-build` on this branch;
  `operator-sdk bundle validate ./bundle` passes.
- Manual / hardware steps: none.

## Risks and rollback

- Known risks: very low — KUBE-34 removes unreachable code; KUBE-32 is
  documentation-only on live fields with no behavior change.
- Rollback plan: revert the two commits on this branch.
