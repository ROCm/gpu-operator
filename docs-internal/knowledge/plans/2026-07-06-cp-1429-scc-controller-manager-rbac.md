# Cherry-pick #1429: SecurityContextConstraints RBAC for controller-manager

- **Date:** 2026-07-06
- **Author:** Yan Sun
- **Related PR(s):** #1431 (cherry-pick), #1429 (original, merged 2026-05-02)
- **Related issue(s) / JIRA:** GPUOP-664

## Context

PR #1429 added `use` permission on the `privileged` SecurityContextConstraint
for the controller-manager ServiceAccount on OpenShift, but merged to `main`
before the `pr-plan-check` workflow existed, so it has no associated plan
file. PR #1431 is the automated cherry-pick of #1429 (via
`CP.O2O.pensando.gpu-operator.main.1429`) and is blocked by `pr-plan-check`
for the same reason. This plan file retroactively documents the change so
#1431 can pass the gate; no product behavior changes as a result of this
plan file itself.

The cherry-pick also required a manual rebase onto current `main` to resolve
conflicts in generated files (`bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml`
`createdAt` and `helm-charts-k8s/Chart.lock` `generated` timestamps) that had
drifted since #1429 was branched.

## Approach

- Add `+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use`
  marker to `internal/controllers/device_config_reconciler.go`, next to the
  existing markers.
- Regenerate `config/rbac/role.yaml`, `helm-charts-k8s/templates/manager-rbac.yaml`,
  and `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml` via
  `make manifests` / `make bundle-build` / `make helm-k8s` so the new rule is
  auto-derived rather than hand-written in three places.
- Aligns controller-manager with all other operator service accounts
  (config-manager, node-labeller, dra-driver, metrics-exporter, test-runner,
  kmm-device-plugin, kmm-module-loader) that already carry this SCC rule.

### Alternatives considered

- **Hand-edit the three generated RBAC artifacts directly**: Rejected — the
  repo's generated-file convention (see root `CLAUDE.md`) requires deriving
  RBAC surfaces from the `+kubebuilder:rbac` marker via `controller-gen`, to
  keep the three artifacts from drifting relative to one another.
- **Bind the SCC via a separate ClusterRoleBinding/manifest outside the
  operator's own ClusterRole**: Rejected — every other operator SA already
  gets this permission through the same marker-driven ClusterRole; a
  one-off binding would be inconsistent and harder to discover.

## Scope

- **In scope:** `internal/controllers/device_config_reconciler.go` (RBAC
  marker), `config/rbac/role.yaml`, `helm-charts-k8s/templates/manager-rbac.yaml`,
  `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml` (regenerated
  artifacts). This plan file itself, to satisfy `pr-plan-check` for #1431.
- **Out of scope:** Any other service account's permissions; any non-RBAC
  behavior of `device_config_reconciler.go`.

## Validation

- `go build ./...` passes on the rebased branch.
- `make manifests` produces no diff against the committed `config/rbac/role.yaml`
  (confirms the checked-in file matches marker-derived output exactly).
- `make bundle-build` and `make helm-k8s` reproduce the committed content of
  the OLM CSV and Helm chart RBAC template byte-for-byte, aside from the
  expected `createdAt` / `generated` regeneration timestamps.
- Manual: deploy on an OpenShift cluster and confirm the controller-manager
  pod starts without an SCC admission error (carried over from #1429's test
  plan).

## Risks and rollback

- **Risk:** None beyond the original #1429 change — this is a straight
  cherry-pick plus a rebase to resolve timestamp-only conflicts in generated
  files.
- **Rollback:** Revert the single squashed commit on the cherry-pick branch;
  no other systems depend on this plan file.
