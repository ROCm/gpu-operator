# Fix ANR Remediation Workflow Test Step on OpenShift — Add `workflows/finalizers` RBAC

- **Date:** 2026-07-09
- **Author:** Uday Bhaskar Biluri
- **Related PR(s):** #1593
- **Related issue(s) / JIRA:** [GPUOP-969](https://pensando.atlassian.net/browse/GPUOP-969); regression from [KUBE-35](https://amd.atlassian.net/browse/KUBE-35) / PR #1575 (see `docs-internal/knowledge/plans/2026-07-06-kube-35-argo-rbac-least-privilege.md`)

## Context

All ANR test cases that run a full remediation workflow fail on OpenShift. The
workflow `test` step — which creates the test-runner `Job` and `ConfigMap` — is
rejected by the API server immediately:

```
Error from server (Forbidden): configmaps "<name>-cm" is forbidden:
  cannot set blockOwnerDeletion if an ownerReference refers to a resource
  you can't set finalizers on: , <nil>
Error from server (Forbidden): jobs.batch "<name>-test" is forbidden: ...
```

Affected tests (all OpenShift clusters — MI210, MI350P, W7900; oc-7 CI 2026-07-07):

- `test_node_remediation::test_anr_workflow`
- `test_node_remediation::test_tester_image_frameworks[RVS]`
- `test_node_remediation::test_tester_image_frameworks[AGFHC]`
- `test_node_remediation::test_node_label_removal`

**Root cause.** `internal/controllers/remediation/scripts/test.sh` creates the
`ConfigMap` and `Job` with an `ownerReference` back to the parent Argo `Workflow`
using `blockOwnerDeletion: true` / `controller: true`. Kubernetes' garbage-collector
admission plugin performs a single authorization check when `blockOwnerDeletion`
is set: the requester must have `update` on the owner type's `finalizers`
subresource — here `argoproj.io/workflows/finalizers`.

The least-privilege Argo RBAC introduced by KUBE-35 (PR #1575) replaced the prior
wildcard grant with an explicit rule set that covers `workflows` and
`workflowtemplates` but not the distinct `workflows/finalizers` subresource. The
operator runs the workflow pods under the same `controller-manager` SA, so the
`test.sh` create calls fail the finalizers check. OpenShift enforces this strictly;
vanilla Kubernetes CI does not, which is why the regression went unnoticed.

## Approach

Add a single rule granting `update` on the `workflows/finalizers` subresource to
both Argo RBAC source files (both bound to the same operator `controller-manager`
SA and which must stay in sync):

```yaml
- apiGroups: [argoproj.io]
  resources: [workflows/finalizers]
  verbs: [update]
```

This is both necessary and sufficient: the blockOwnerDeletion admission check only
requires the `update` verb on the `finalizers` subresource — no `create`/`patch`/
`delete` on finalizers, and no additional verbs on `jobs`/`configmaps` (those
creates were failing only because of the finalizers check, not missing job/CM
permissions).

After editing the two source files, regenerate the derived artifacts (do not
hand-edit the generated CSV / Helm template — they are protected generated files):

- `make helm-k8s` — copies `hack/k8s-patch/template-patch/*` into
  `helm-charts-k8s/templates/` and rebuilds the chart `*.tgz`.
- `make bundle-build` — regenerates the RBAC block in the OLM CSV.

### Alternatives considered

- **Grant broader verbs on `workflows/finalizers` (e.g. `create`, `patch`)** —
  rejected: the admission check only exercises `update`; anything more re-widens
  the grant the KUBE-35 hardening deliberately narrowed.
- **Revert to the pre-KUBE-35 wildcard Argo RBAC** — rejected: reintroduces the
  SEC-00455 security finding.
- **Drop `blockOwnerDeletion` from `test.sh` owner references** — rejected: the
  owner reference provides automatic garbage collection of the test `Job`/`ConfigMap`
  when the parent workflow is deleted; changing runtime behavior is riskier than a
  one-line RBAC addition.

## Scope

- **In scope:**
  - `config/manifests/argo-workflow-rbac.yaml` (OLM bundle source)
  - `hack/k8s-patch/template-patch/argo-rbac.yaml` (Helm source)
  - Regenerated artifacts: `helm-charts-k8s/templates/argo-rbac.yaml`, chart `*.tgz`,
    and `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml`.
- **Out of scope:**
  - Argo workflow-controller RBAC (`remediation-deployment.yaml`) — different SA.
  - Any change to `test.sh` owner-reference semantics.
  - Namespace-scoping / dedicated workflow SA (tracked as KUBE-35 follow-ups).

## Validation

- **Static review:** confirm the `workflows/finalizers: [update]` rule appears in
  both source files, the regenerated Helm template, and the CSV RBAC block; confirm
  nothing else changed.
- **Integration / e2e tests:** on an OpenShift cluster with `remediation.enabled: true`,
  run `tests/pytests/openshift/gpu-operator/test_node_remediation.py::test_anr_workflow`
  (plus the tester-image-framework and node-label-removal cases) and confirm the
  `test` step creates the `ConfigMap` + `Job`, the runner completes, and the workflow
  reaches a Succeeded state with no `Forbidden` error.
- **Install paths:** verify both a Helm install and an OLM bundle install come up
  with the updated role and remediation functions.

## Risks and rollback

- **Known risks:** Low — purely additive RBAC (one subresource, single `update`
  verb). Minor risk that the two source files drift; mitigated by regenerating and
  reviewing the CSV/Helm diff against the source edits.
- **Rollback plan:** revert the two source-file edits and regenerate the artifacts
  (or revert the PR). No cluster/runtime state persists; the change is purely RBAC
  manifest content.
