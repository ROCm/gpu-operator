# Collect test-runner logs in techsupport bundles

- **Date:** 2026-10-15
- **Author:** Yan Sun
- **Related PR(s):** [pensando/gpu-operator#1692](https://github.com/pensando/gpu-operator/pull/1692)
- **Related issue(s) / JIRA:** N/A

## Context

RVS test runner saves compressed result and stdout files under a hostPath, but
the techsupport bundle only collected Kubernetes pod logs. Manual Jobs can run
in arbitrary namespaces and use recipe-derived pod names, while pre-start
checks run in init containers. The prior namespace/name-based lookup therefore
missed some runner logs, and completed Jobs can disappear before support data is
collected.

## Approach

- Find test-runner pods across namespaces on each selected node by regular or
  init container name, rather than by namespace or generated pod name.
- Save init-container logs separately so pre-start output is retained even
  while the workload container is waiting for init completion.
- Copy the test-runner log directory from each selected node through the
  existing read-only host-root mount. Use `/var/log/amd-test-runner` by
  default, with `AMD_TEST_RUNNER_LOG_DIR` for clusters configured with another
  hostPath. Preserve files and subdirectories, including the original gzip
  artifacts.
- Normalize the configured path and reject root-equivalent paths or parent
  traversal so an override cannot archive the entire host filesystem.
- Isolate runner pod log collection so its namespace selection cannot affect
  the workflow and operator collectors that run afterward.
- Document the default and environment override in the troubleshooting guide.

The trigger value (`MANUAL`, `AUTO_UNHEALTHY_GPU_WATCH`, or
`PRE_START_JOB_CHECK`) changes how the runner starts, not how its configured
host log directory is mounted. This keeps host-file collection independent of
the trigger type.

### Alternatives considered

- **Look only for `test-runner-*` pod names in the operator namespace** —
  rejected; manual and pre-start pods use caller-selected names and may run in
  another namespace.
- **Read the DeviceConfig and every runner pod to discover host paths** —
  rejected in favor of one documented default plus an explicit environment
  override, which is simpler to operate and reduces Kubernetes API calls.
- **Collect only `kubectl logs` output** — rejected; completed jobs' generated
  gzip artifacts are stored on the node and are not recoverable from pod logs.

## Scope

- **In scope:** `tools/techsupport_dump.sh`, the techsupport section in
  `docs/troubleshooting.md`, and this internal plan.
- **Out of scope:** changing test-runner log generation, pruning or rotating
  host logs, and changing DeviceConfig defaults.

## Validation

- **Unit tests:** none; the shell collector has no unit-test harness.
- **Syntax and whitespace:** `bash -n tools/techsupport_dump.sh` and
  `git diff --check`.
- **Live:** ran the updated collector on the single-node Kubernetes v1.37
  cluster. The bundle included manual and pre-start gzip artifacts, all of
  which passed gzip integrity checks; the pre-start init-container output was
  present. The operator test-runner DaemonSet was not active, so auto-health
  collection was checked against the operator's pod and hostPath definitions
  but was not triggered live.

## Risks and rollback

- **Risks:** Collection time and bundle size grow with the contents of the
  configured directory on each selected node. A non-default hostPath must be
  supplied through `AMD_TEST_RUNNER_LOG_DIR` when running techsupport.
- **Rollback:** Revert the collector and troubleshooting-doc changes. No
  operator workload or cluster configuration is changed by this patch.
