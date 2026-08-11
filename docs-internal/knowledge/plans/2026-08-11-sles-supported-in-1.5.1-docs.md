# Declare SLES as a supported platform in 1.5.1 docs

- **Date:** 2026-08-11
- **Author:** Nitish Bhat
- **Related PR(s):** #<main-pr>, #<v1.5.1-pr>
- **Related issue(s) / JIRA:** #gpu-operator-dev Slack request (Shrey Ajmera / Praveen kumar Shanmugam, 2026-08-10)

## Context

The code to support SUSE Linux Enterprise Server (SLES) GPU worker nodes
landed in the 1.5.1 release via three merged PRs, all confirmed present on
`origin/v1.5.1`:

- #1456 — base SLES support (cherry-pick of ROCm#365)
- #1494 — SLES 16.0 support + extended SLES 15 SP7 driver versions
- #1537 — SLES driver version check via `registry.suse.com` (ROCm#572)

However, the user-facing docs never declared SLES as a *supported platform*.
The "OS & Platform Support Matrix" in `docs/index.md`, the v1.5.1 release
notes, and the precompiled-driver OS reference table all still list only
Ubuntu / Debian / RHCOS. SUSE has published their own docs referencing this
support, so the gap is purely in our published support declaration.

## Approach

Add SLES 15 SP7 and SLES 16.0 as supported platforms in three docs:

- `docs/index.md` — two new rows in the OS & Platform Support Matrix,
  Kubernetes `1.29-1.36` (matching every other row).
- `docs/releasenotes.md` — a SLES highlight bullet and a Platform Support
  line under the v1.5.1 section.
- `docs/drivers/precompiled-driver.md` — SLES entries in the `osImage`→OS
  table and the tag-format table, with a verified example tag.

Versions, default driver version (`31.30`), and the tag format
(`sles-<codestream>-<GA kernel>-default-<driver version>`) are taken
directly from `internal/utils.go` (`slesDefaultDriverVersions`,
`slesRegistryManifestFmt`, `slesCodestream`) and the existing
`docs/specialized_networks/airgapped-install.md`, not invented.

### Alternatives considered

- **Per-OS Kubernetes range narrower than 1.29-1.36** — rejected. The K8s
  range is an operator-level claim; SLES code paths have no K8s-version
  branching, so SLES behaves identically to Ubuntu across the range.
  Listing a narrower range would be inconsistent and misleading.

## Scope

- **In scope:** Docs-only declaration of existing, already-merged SLES
  support on `main` and `v1.5.1`.
- **Out of scope:** Any SLES code change (already merged); rendering/theme
  changes to the support matrix table.

## Validation

- Docs spellcheck: "SUSE" and "codestream" already appear in passing docs
  on the branch, so `.wordlist.txt` / `.spellcheck.yaml` are satisfied.
- Facts cross-checked against `internal/utils.go` and the SLES plan files
  (`2026-06-03-cp-sles-16-support.md`, `2026-06-11-sles-registry-version-check.md`).
- Manual: local docs-site render to confirm the HTML table row displays
  correctly (recommended before merge).

## Risks and rollback

- **Risk:** Implies SLES received the same validation depth as Ubuntu
  across all K8s minors. Mitigated: the same caveat applies to every row
  in the matrix; claim is consistent with existing convention.
- **Rollback:** Revert the docs commit; no runtime impact.
