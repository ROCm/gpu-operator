# Auto-bump ANR default test-runner image with PROJECT_VERSION

- **Date:** 2026-07-11
- **Author:** Sun, Yan
- **Related PR(s):** #1598
- **Related issue(s) / JIRA:** [GPUOP-975](https://pensando.atlassian.net/browse/GPUOP-975)

## Context

The auto node remediation (ANR) workflow launches the test-runner as a
standalone Job. When `spec.remediationWorkflow.testerImage` is unset, the
operator falls back to `DefaultTestRunnerImage` in
`internal/controllers/remediation_handler.go`, which was hardcoded to
`docker.io/rocm/test-runner:v1.4.1`.

Unlike the three other default images (config-manager, metrics-exporter,
and the DeviceConfig test-runner), this ANR default was **not** covered by
the `make update-version` sed automation, so every release bumped the
others while ANR stayed pinned to v1.4.1.

That stale v1.4.1 build (cut Dec 2025) predates R9700S (device 0x7551)
GPU-model auto-detection (PR #1210 in device-metrics-exporter, merged
2026-05-29). On R9700S the old binary fails Device-ID lookup, falls back to
the generic iet_single.conf, hangs to the 900 s timeout, and ANR
remediation is declared failed — the failure captured in GPUOP-975. The
DeviceConfig path in the same CI job used a current image and passed,
confirming the only gap was the frozen ANR default.

## Approach

- Add one `sed` line to the `update-version` target in the `Makefile` so the
  ANR `DefaultTestRunnerImage` is rewritten to `${PROJECT_VERSION}` at release
  time, exactly like the existing config-manager / metrics-exporter /
  DeviceConfig test-runner defaults.
- Reset the checked-in default in
  `internal/controllers/remediation_handler.go` to the `v0.0.1` placeholder,
  matching the convention the other three release-versioned defaults already
  use. The Go const is only a fallback for CRs that omit `testerImage`;
  helm-deployed DeviceConfigs always carry an explicit `testerImage` set by
  `make update-registry` (which runs `yq ... .remediationWorkflow.testerImage
  = $(TEST_RUNNER_IMG)`), so the real image is populated at deploy time.
- Manually set the documented default in
  `docs/autoremediation/auto-remediation.md` (the `testerImage` example and
  the `TesterImage` field description) to the current shipping tag `v1.5.1`.

### Alternatives considered

- **Hardcode a real tag (e.g. v1.5.1) in the Go source.** Rejected — it makes
  the ANR default inconsistent with the other three release-versioned defaults
  (all `v0.0.1` placeholders) and re-freezes the same class of drift the next
  time someone forgets to bump it. The root cause is the missing automation,
  not the specific pinned value.
- **Also automate the docs value via sed.** Rejected per maintainer
  preference: only the Go default is hooked to `PROJECT_VERSION`; docs are
  updated manually to the current release.

## Scope

- **In scope:** ANR `DefaultTestRunnerImage` default value + its release
  automation; the documented default in the auto-remediation doc.
- **Out of scope:** The device-metrics-exporter test-runner code itself
  (already fixed via PR #1210). The DeviceConfig test-runner default (already
  automated). No behavior change when `testerImage` is explicitly set.

## Validation

- Unit tests: existing suite; no test hardcodes the old v1.4.1 value.
- Automation check: `make update-version PROJECT_VERSION=v1.6.0` rewrites
  `DefaultTestRunnerImage` to `docker.io/rocm/test-runner:v1.6.0` and
  `go fmt` keeps the const block aligned (verified via dry run, then
  reverted).
- Manual: confirmed the docs default reads `v1.5.1`.

## Risks and rollback

- Known risks: minimal — the default only applies when `testerImage` is
  unset; explicit overrides are unaffected. Value now tracks the release
  version, so a release must publish the matching test-runner tag.
- Rollback plan: revert the three-file diff to restore the previous
  hardcoded default.
