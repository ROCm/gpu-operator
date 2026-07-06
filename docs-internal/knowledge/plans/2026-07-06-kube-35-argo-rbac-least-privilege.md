# Fix: Replace Wildcard Argo RBAC on Operator SA with Least-Privilege Rules (SEC-00455)

- **Date:** 2026-07-06
- **Author:** Uday Bhaskar Biluri
- **Related PR(s):** #1575
- **Related issue(s) / JIRA:** [KUBE-35](https://amd.atlassian.net/browse/KUBE-35) (Mythos AI scan SEC-00455); SWSPLAT equivalent [SWSPLAT-29028](https://amd.atlassian.net/browse/SWSPLAT-29028)

## Context

Mythos AI security scan finding SEC-00455 (High, P2) flagged that the operator's
`controller-manager` ServiceAccount is bound to a `ClusterRole` granting **wildcard
RBAC**: `argoproj.io: */*`, `batch/jobs: *`, and `pods/log: *`. Because Argo Workflow
objects can themselves spawn privileged pods, a compromised operator SA could schedule
arbitrary workloads cluster-wide, bypassing namespace-scoped admission policies.

The wildcard grant exists in **two source files**, both bound to the same operator
`controller-manager` SA and which must be changed together:

- `hack/k8s-patch/template-patch/argo-rbac.yaml` — the source that `make` copies into
  `helm-charts-k8s/templates/argo-rbac.yaml` for Helm installs (see `Makefile` target
  that runs `cp hack/k8s-patch/template-patch/* helm-charts-k8s/templates/`).
- `config/manifests/argo-workflow-rbac.yaml` — OLM bundle only, pulled in via
  `config/manifests/kustomization.yaml` and rendered into
  `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml`.

The separate Argo *controller* RBAC in `helm-charts-k8s/templates/remediation-deployment.yaml`
is the workflow-controller's own SA (not the operator SA) and is out of scope.

### Why the minimal set is larger than a naive Go-code trace suggests

The engineering triage on the ticket derived a minimal verb set from static tracing of
`internal/controllers/remediation_handler.go` and concluded `pods/log` had "no operator
usage" and could be dropped. That conclusion is **incomplete**: the remediation workflow
pods run under the **same `controller-manager` SA** (set in `populateWorkflow` via
`getServiceAccountName`, which picks the `*controller-manager` SA). The shell scripts
under `internal/controllers/remediation/scripts/` therefore execute `kubectl` with this
SA's permissions. Verifying those scripts shows:

- `test.sh` runs `kubectl logs -n <ns> job/<name>` → **requires `pods/log: get`**.
- `test.sh` also runs `kubectl apply`/`get` on Jobs → requires `batch/jobs: get, create`.

So `pods/log` must be **retained (narrowed to `get`)**, not dropped. The remaining
script permissions (`nodes`, `pods`, `configmaps`, `events`) are provided by other roles
already bound to the SA (`manager-role`, `event-recorder-clusterrole`) and are unaffected
by this change.

## Approach

Replace the wildcard rules in both source files with an explicit least-privilege set,
derived from Go client calls **and** the workflow scripts:

```yaml
- apiGroups: [argoproj.io]
  resources: [workflows, workflowtemplates]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [batch]
  resources: [jobs]
  verbs: [get, list, watch, create, delete]
- apiGroups: [""]
  resources: [pods/log]
  verbs: [get]
- apiGroups: [""]
  resources: [serviceaccounts]
  verbs: [get, list, watch]   # unchanged (already scoped)
```

Rationale per resource:

- **workflows / workflowtemplates**: operator does create/get/delete/update/list
  (`remediation_handler.go`); `list`+`watch` retained for controller-runtime cache reads.
- **batch/jobs**: operator does get/delete/create; `test.sh` does create/get. `list`/`watch`
  kept for completeness of cache-backed reads.
- **pods/log**: `get` only — used by `test.sh` (`kubectl logs`). `list` is not a meaningful
  verb for the logs subresource.
- **cronworkflows**: no usage in Go or scripts → not granted.
- **serviceaccounts**: unchanged.

Then regenerate the derived artifacts so all copies stay in sync:

- `make helm-k8s` → regenerates `helm-charts-k8s/templates/argo-rbac.yaml` and the chart `*.tgz`.
- `make bundle` → regenerates the RBAC block in the OLM CSV.

### Scope decision: keep `ClusterRole`, do not namespace-scope

The scan suggested narrowing the binding to a namespaced `RoleBinding`. This is **not
viable** without breaking remediation:

- `drain.sh` runs `kubectl get pods --all-namespaces` (cluster-wide pod list).
- `nodes` are cluster-scoped by nature (taint/label/get in several scripts).

So the operator SA fundamentally needs cluster-scoped access. The hardening here narrows
**verbs/resources**, not scope. Keeping `ClusterRole` preserves current behavior and avoids
a larger, higher-risk refactor.

### Alternatives considered

- **Drop `pods/log` entirely** (per the ticket's literal suggestion) — rejected: breaks
  `test.sh` log capture, which runs under the operator SA. Narrowed to `get` instead.
- **Namespace-scope via `RoleBinding`** — rejected: breaks `drain.sh` (all-namespace pod
  list) and node-level operations; larger refactor with regression risk.
- **Dedicated minimal SA for workflow pods** (instead of `controller-manager`) — deferred:
  the cleanest long-term posture, but a significant behavioral change out of scope for this
  security fix. Noted as a possible follow-up.
- **Add a conftest/rego policy denying wildcard-verb RBAC** (ticket suggestion) — deferred:
  the repo currently has no conftest/policy tooling; net-new CI infrastructure is out of
  scope for this fix.

## Scope

- **In scope:**
  - `hack/k8s-patch/template-patch/argo-rbac.yaml` (Helm source)
  - `config/manifests/argo-workflow-rbac.yaml` (OLM bundle source)
  - Regenerated artifacts: `helm-charts-k8s/templates/argo-rbac.yaml`, chart `*.tgz`,
    and `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml`.
- **Out of scope:**
  - Argo workflow-controller RBAC (`remediation-deployment.yaml`) — different SA.
  - Namespace-scoping the binding (see decision above).
  - conftest/rego wildcard-deny policy (possible follow-up).
  - Dedicated workflow SA refactor (possible follow-up).

## Validation

- **Static review:** confirm no `*` verbs/resources remain in either source file or the
  regenerated CSV block; confirm generated Helm template and CSV match the source edits.
- **Functional (remediation e2e):** run a full node-remediation workflow and confirm each
  step succeeds under the tightened SA — specifically the steps that exercise the narrowed
  permissions:
  - `test.sh`: ConfigMap + Job creation, `kubectl get job`, and `kubectl logs job/...`
    (validates `batch/jobs` + `pods/log: get`).
  - `drain.sh`: all-namespace pod list + delete (validates cluster-scoped `pods`).
  - `notify.sh`: Event creation (validates `event-recorder` role still covers it).
  - workflow create/get/delete lifecycle (validates `argoproj.io` verbs).
  - Relevant suites: `tests/pytests/{k8,openshift}/gpu-operator/test_node_remediation.py`,
    `test_anr_deployment.py`, `test_npd_anr_combined.py`.
- **Install paths:** verify both a Helm install and an OLM bundle install come up with the
  new role and remediation still functions.

## Risks and rollback

- **Risk:** a workflow step uses a permission not captured by the static + script analysis
  (analysis was not validated end-to-end at authoring time). Mitigation: run the full
  remediation e2e above before merge; watch for RBAC `forbidden` errors in workflow pod logs.
- **Risk:** the two source files drift (one updated, the other not). Mitigation: this plan
  requires editing both and regenerating; reviewer checks the CSV diff matches.
- **Risk:** generated files hand-edited instead of regenerated (the CSV is a protected
  generated file). Mitigation: regenerate via `make bundle` / `make helm-k8s`, do not
  hand-edit.
- **Rollback:** revert the two source-file edits and regenerate the artifacts (or revert the
  PR). No runtime/cluster state persists; the change is purely RBAC manifest content.
