# Align OLM bundle controller-manager resources with Helm chart

- **Date:** 2026-07-14
- **Author:** Yan Sun
- **Related PR(s):** #<tbd>
- **Related issue(s) / JIRA:** [KUBE-49](https://amd.atlassian.net/browse/KUBE-49)
  — IBM customer report: intermittent OOMKilled of the controller-manager pod,
  AMD GPU Operator 1.5.0 on OpenShift 4.20

## Context

An IBM customer running the AMD GPU Operator 1.5.0 on OpenShift 4.20 (installed
via the OLM bundle) reported intermittent `OOMKilled` events on the
controller-manager pod. Raising the memory limit to ~600Mi resolved it. IBM
asked whether a bundle update is planned.

Root cause is a resource-limit inconsistency between install methods:

| Install path | mem limit | mem request | source of truth |
|---|---|---|---|
| Helm chart (k8s) | **1Gi** | 256Mi | `helm-charts-k8s/values.yaml` |
| OLM bundle (OpenShift) | **384Mi** | 64Mi | `config/manager-base/manager.yaml` → CSV |

The OLM bundle's `384Mi`/`64Mi` values are the original kubebuilder scaffold
defaults (the block still carried the `# TODO(user): Configure the resources
accordingly` comment) and were never tuned to the operator's real footprint.
The Helm chart was tuned to `1Gi`/`256Mi` at some point but the OLM manifest
source was not kept in sync. OpenShift OLM installs therefore shipped an
under-provisioned limit, which OOMKills under load (KMM builds, ANR
remediation, DRA reconciles all drive transient memory spikes).

## Approach

Update the OLM manifest source of truth and regenerate the bundle so the
OpenShift install matches the Helm chart:

- `config/manager-base/manager.yaml` — set controller-manager `resources` to
  `limits: cpu 1000m / memory 1Gi`, `requests: cpu 100m / memory 256Mi`.
  Replace the scaffold `TODO` comment with a note to keep in sync with the
  Helm chart and why the bump was made.
- Regenerate `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml`
  (the CSV embeds the manager Deployment). Generated via the bundle tooling,
  not hand-edited — only the four resource values change in the CSV. The
  regenerated `createdAt` timestamp was reverted to keep the diff focused.

Choosing `1Gi` (rather than the customer's minimal `600Mi`) deliberately
aligns the two install paths on a single, already-validated value and leaves
generous headroom over the observed failure point.

### Alternatives considered

- **Set the OLM limit to 600Mi** (match the customer's fix exactly) — rejected:
  introduces a third distinct value and leaves the Helm/OLM inconsistency in
  place; only marginally above the observed OOM threshold.
- **Lower the Helm chart to match the OLM bundle** — rejected: wrong direction;
  it would regress the tuned Helm value and reintroduce the OOM risk everywhere.
- **Make the limit configurable in the bundle** — out of scope; OLM installs
  can already override via the Subscription/CSV, and the immediate need is a
  sane default.

## Scope

- **In scope:** controller-manager pod `resources` (limits + requests) in the
  OLM bundle manifest source and the regenerated CSV.
- **Out of scope:** Helm chart values (already at 1Gi/256Mi); resource limits
  of any other operand pod (device-plugin, node-labeller, metrics-exporter,
  etc.); making the limit user-configurable via CRD/values.

## Validation

- `operator-sdk bundle validate ./bundle` — passes (only a pre-existing
  unrelated example-annotation warning).
- Diff review: exactly two files change — `config/manager-base/manager.yaml`
  and the CSV. CSV delta is only the four resource values
  (`cpu 500m→"1"`, `memory 384Mi→1Gi`, `cpu 10m→100m`, `memory 64Mi→256Mi`;
  `1000m` normalizes to `"1"` in the CSV, functionally identical).
- Manual / hardware: deploy the regenerated bundle on OpenShift and confirm
  the controller-manager pod comes up with `limits.memory: 1Gi` and no longer
  OOMKills under the customer's workload.

## Risks and rollback

- **Risks:** higher memory *request* (64Mi → 256Mi) marginally increases
  scheduling reservation on the control-plane node; negligible on any real
  cluster. No behavioral/API change.
- **Rollback:** revert the two files and regenerate the bundle; or override the
  limit at install time via the Subscription/CSV `config.resources`.
