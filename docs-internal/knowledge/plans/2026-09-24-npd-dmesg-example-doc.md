# Plan: NPD dmesg-only example documentation

## Context

The existing `docs/npd/node-problem-detector.md` covers the full AMD GPU Operator NPD
integration (custom plugin monitor + `amdgpuhealth`). Users who only want to watch the
kernel ring buffer for GPU crash patterns have no minimal reference — the full doc
requires AMD Device Metrics Exporter to be installed first.

## Approach

Add `docs/npd/npd-dmesg-example.md` with a self-contained, three-manifest walkthrough
(RBAC → ConfigMap → DaemonSet) that uses only NPD's built-in `system-log-monitor`
(`--config.system-log-monitor`) and the `kmsg` plugin. No `amdgpuhealth`, no DME
mount, no custom plugin monitor.

The doc includes:
- Minimal RBAC (no non-resource URL permissions needed)
- `kernel-monitor.json` ConfigMap with `amdgpu.*` dmesg regex rules for GPU page
  faults, hangs, resets, and RAS errors
- DaemonSet with only `/dev/kmsg` mounted (no `/var/log`, no `amdexporter`)
- Verify steps and a pointer to the full integration doc and auto-remediation doc

No changes to existing files.

## Scope

- **In scope:** `docs/npd/npd-dmesg-example.md` (new file)
- **Out of scope:** Changes to `node-problem-detector.md`, test YAMLs, or operator code

## Validation

- Markdown renders without errors (`markdownlint`)
- YAML blocks in the doc are valid YAML (manual check)
- Links to existing docs (`node-problem-detector.md`, `../autoremediation/auto-remediation.md`) resolve

## Risks / Rollback

Low risk — doc-only change. Revert by deleting the file.
