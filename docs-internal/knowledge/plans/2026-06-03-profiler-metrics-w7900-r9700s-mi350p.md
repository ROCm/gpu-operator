# Profiler metrics enablement: W7900, R9700S, MI350P

- **Date:** 2026-06-03
- **Author:** Srivatsa Sangli
- **Related PR(s):** TBD
- **Related issue(s) / JIRA:** GPUOP-842

## Context

Three GPU series (W7900 RDNA3, R9700S RDNA4, MI350P CDNA4) had
`profiler_metrics: false` in `amdgpu-features.json`, causing the test suite
to skip all profiler metric validation on those platforms. Live DME pod
inspection confirmed profiler metrics are exported with real non-zero values
on these GPUs.

Additionally, two gaps were found in the W7900 `metrics-support.json` entries
via cross-checking DME curl output against the test support matrix:

- `GPU_ENERGY_CONSUMED` was missing for W7900 (DME exports ~6.57e10 J
  with real values on all 4 GPUs).
- `GPU_PROF_VALU_PIPE_ISSUE_UTIL` was incorrectly listed for W7900; DME
  logs confirm "Platform doesn't support field name: GPU_VALU_PIPE_ISSUE_UTIL"
  on RDNA3.

R9700S additions to `GPU_PROF_OCCUPANCY_PER_ACTIVE_CU` and
`GPU_PROF_OCCUPANCY_PER_CU` reflect verified DME export behavior on RDNA4
hardware (distinct from the gfx1201 rocprofiler-sdk SQ bug documented in
plan `2026-05-28-gpuop-825-profiler-radeon-support.md`, which covered
R9600D).

## Approach

- Set `profiler_metrics: true` for W7900, R9700S, and MI350P in
  `amdgpu-features.json` so the test suite includes profiler metric checks.
- Add W7900 to `GPU_ENERGY_CONSUMED` gpu list in `metrics-support.json`.
- Remove W7900 from `GPU_PROF_VALU_PIPE_ISSUE_UTIL` gpu list (not supported
  on RDNA3 per DME logs).
- Add R9700S to `GPU_PROF_OCCUPANCY_PER_ACTIVE_CU` and
  `GPU_PROF_OCCUPANCY_PER_CU` gpu lists (verified exported on RDNA4).

### Alternatives considered

- Wait for a load-run before enabling profiler tests — rejected because the
  flag gate was blocking all profiler coverage; any per-metric issues will
  surface in CI runs.

## Scope

- **In scope:** `amdgpu-features.json` feature flags; `metrics-support.json`
  GPU support lists for the affected metrics.
- **Out of scope:** Exporter code changes; upstream rocprofiler-sdk fixes;
  SM_ACTIVE / OCCUPANCY_PER_ACTIVE_CU / OCCUPANCY_PER_CU for W7900 (deferred
  pending load-run confirmation).

## Validation

- Unit tests: N/A (JSON config changes only).
- Integration / e2e tests: W7900 and R9700S metrics test runs in CI will
  now exercise profiler metric validation.
- Manual / hardware steps: Cross-checked DME pod `curl http://localhost:5000/metrics`
  output against `metrics-support.json` on a 4x W7900 cluster
  (ctr-smc-s99-28). DME pod logs scanned for "Platform doesn't support"
  messages to identify unsupported metrics.

## Risks and rollback

- Known risks: If W7900 or R9700S profiler metrics are flaky under certain
  driver versions, CI may show intermittent failures on those platforms.
  Per-metric `skip-validation: yes` can be added without reverting the
  whole change.
- Rollback plan: Revert the commit; or set `profiler_metrics: false` for
  the affected GPU series in `amdgpu-features.json`.
