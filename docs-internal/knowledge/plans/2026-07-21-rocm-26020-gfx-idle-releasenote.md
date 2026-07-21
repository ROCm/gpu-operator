# ROCM-26020: move Radeon gfx_activity idle uptick to Fixes (v1.5.1)

- **Date:** 2026-07-21
- **Author:** Bhanu Kiran Atturu
- **Related PR(s):** pensando/gpu-operator#1621
- **Related issue(s) / JIRA:** ROCM-26020

## Context

The v1.5.1 release notes listed the Radeon AI `gfx_activity` idle uptick under
Known Limitations. The bundled Device Metrics Exporter now fixes it (gpuagent
Navi48 idle gfx cold-read fix, DME commit 9f8bc9e3c), so the entry belongs under
Fixes.

## Approach

- `docs/releasenotes.md`: move the `gfx_activity` idle-uptick entry from the
  v1.5.1 Known Limitations section to the v1.5.1 Fixes section, reworded to state
  that `gfx_activity` now shows true idle values (per review feedback).

## Scope

- **In scope:** operator v1.5.1 release-notes wording.
- **Out of scope:** the DME-side release note (tracked in the DME repo) and the
  underlying gpuagent fix.

## Validation

- Documentation only; no build. Wording confirmed against reviewer feedback on
  the PR.

## Risks and rollback

- Known risks: none (docs only).
- Rollback: revert the commit.
