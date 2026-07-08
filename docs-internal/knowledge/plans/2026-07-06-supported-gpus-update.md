# Supported GPUs Documentation Update: Add MI350P and Radeon AI PRO GPUs

- **Date:** 2026-07-06
- **Author:** Claude Code agent
- **Related PR(s):** feat/supported-gpus-docs
- **Related issue(s) / JIRA:** N/A

## Context

The `docs/index.md` Supported Hardware table listed only AMD Instinct MI210,
MI250, MI300X, MI325X, MI350X, and MI355X. Several GPU models that have been
actively validated in the CI pipeline and test infrastructure were missing:

**MI350P** — CDNA4 GPU:
- Dedicated platform-release CI job in `tests/jobs/platform-release/mi350p/`
- Pytest fixtures in `test_config_manager.py` and `test_dra_partitioning.py`
- Entry in `amdgpu-features.json` and `metrics-support.json`
- v1.5.1-beta release notes explicitly call out MI350P on both K8s and OpenShift

**Radeon AI PRO R9700S** — RDNA4 GPU:
- Active platform-release CI job (`tests/jobs/platform-release/radeon/`, gpu-series=R9700S)
- Entry in `amdgpu-features.json` and `metrics-support.json`
- Added in commit `9c5ef17e` (R9700 NFD rules) and subsequent Radeon fix commits

**Radeon AI PRO R9600D** — RDNA4 GPU:
- Dedicated test support added in commit `9ecda4ee`
- Profiler metrics support confirmed in commit `e695467e`
- Tested on physical hardware (internal lab node, 8x R9600D)

**Radeon Pro W7900** — RDNA3 GPU:
- Active platform-release CI jobs (K8s and OpenShift, gpu-series=W7900)
- Entry in `amdgpu-features.json`
- Profiler metrics support confirmed in commit `bdcde161`

## Approach

Add MI350P, R9700S, R9600D, and W7900 to the existing `### Supported Hardware`
table in `docs/index.md`, preserving the original two-column format. Instinct
GPUs remain at the top in version-descending order; Radeon AI PRO GPUs follow.

A follow-up commit temporarily added a third **GPU Operator Version** column
mapping each GPU to the operator version that first introduced support. That
column was removed after review to keep the table minimal — the final table
stays two-column (`GPU`, `Support status`).

## Scope

- **In scope:** `docs/index.md` Supported Hardware table only.
- **Out of scope:** Architecture columns, OS/Platform matrix, release notes,
  per-GPU operator-version column, any code changes.

## Validation

- Visual review of the rendered Markdown table.
- Cross-check each GPU against `tests/pytests/lib/files/amdgpu-features.json`
  and platform-release CI job directories.

## Risks and rollback

- **Risk:** Minimal — purely additive documentation change.
- **Rollback:** Remove the added rows from `docs/index.md`. No code changes involved.
