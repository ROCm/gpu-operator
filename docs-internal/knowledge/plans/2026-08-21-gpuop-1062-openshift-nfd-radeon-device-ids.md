# GPUOP-1062: OpenShift NFD device-ID drift (Radeon GPUs undetected)

- **Date:** 2026-08-21
- **Author:** Nitish Bhat
- **Related PR(s):** pensando/gpu-operator (this PR) + ROCm/gpu-operator counterpart
- **Related issue(s) / JIRA:** [GPUOP-1062](https://pensando.atlassian.net/browse/GPUOP-1062)

## Context

Deploying the GPU Operator via OLM on an OpenShift cluster with Radeon GPUs
detects zero GPUs. Reported against an 8x Radeon AI PRO R9700 (`0x7551`) SNO
cluster (jobd 33207089), where all ~300 pytest cases failed with
`No nodes with AMD/GPU found in the cluster`.

The tests resolve GPU nodes through `k8_get_gpu_nodes()`, which matches on
`feature.node.kubernetes.io/amd-gpu` / `amd-vgpu`
(`tests/pytests/lib/k8_util.py:176`). Those labels are produced by a
NodeFeatureRule whose device-ID list did not contain `7551`, so no node was
ever labelled and every test bailed at setup.

### Correcting the reported diagnosis

The Jira description states the OLM bundle ships a NodeFeatureRule that covers
only Instinct device IDs, and recommends syncing it with the helm chart.
**That artifact does not exist.** Verified:

- `bundle/manifests/` contains 7 files, none a NodeFeatureRule.
- `config/` contains no NodeFeatureRule, so nothing generates one into the bundle.
- `hack/openshift-patch/olm-bundle-patch/` contains only a PrometheusRule.
- The operator does not create one at runtime (no `NodeFeatureRule` reference
  in non-test Go code).

The OLM bundle ships **no** NodeFeatureRule by design. On OpenShift the user is
expected to create one by hand, and the only source for its contents is
`docs/installation/openshift-olm.md`. That doc holds two copy-paste YAML blocks
— a `NodeFeatureDiscovery` CR and a `NodeFeatureRule` CR — and **both** list
Instinct device IDs only. That is the actual defect.

### Root cause

The AMD GPU PCI device-ID list is duplicated across several hand-maintained
files with nothing enforcing agreement. Commit `9c5ef17e` ("Add Radeon AI PRO
R9700 to gpu-nfd-default-rule files") updated exactly two of them and left the
documentation untouched. GPUOP-990 (the 1.5.1 Radeon docs pass) updated product
framing and the version matrix but likewise never touched the device lists.

Drift measured against the source of truth:

- `amd-gpu` — 16 IDs missing from the docs:
  `7460 7448 744b 744a 7449 745e 73a2 73a3 73ab 73a1 7551 7550 744c 73af 73bf 7590`
- `amd-vgpu` — 2 IDs missing from the docs: `7461` (V710 MxGPU), `73ae` (V620 MxGPU)

### Which file is authoritative

`hack/k8s-patch/template-patch/gpu-nfd-default-rule.yaml` is the source of
truth. `make helm-k8s` runs `clean-helm` (`rm -rf helm-charts-k8s`) and then
repopulates the tree via
`cp hack/k8s-patch/template-patch/* helm-charts-k8s/templates/`, so
`helm-charts-k8s/templates/gpu-nfd-default-rule.yaml` is a pure build output —
`config/` generates no such file, meaning it exists in the helm tree solely
because of that copy. It is committed to git, but editing it is futile.

## Approach

Fix the documentation, then add a guard so the lists cannot silently diverge
again. The comparison logic lives in one script; the Makefile target, the
Claude Code hook, and the CI workflow are all thin callers of it — the same
"thin caller" conclusion reached in `2026-06-16-buildx-cache-support.md`.

1. **`hack/check-nfd-device-ids.py`** — extracts `{rule name -> set of device
   IDs}` from every copy and diffs each against the source of truth. Python 3
   stdlib only. The files use different YAML shapes (source: one ID per
   `matchFeatures` entry; docs: a multi-line inline list), so the script
   normalizes to a set per rule rather than doing a textual diff. Reports the
   exact missing/extra IDs per rule per file and exits non-zero on drift.

   Files checked:

   | File | Role |
   | --- | --- |
   | `hack/k8s-patch/template-patch/gpu-nfd-default-rule.yaml` | source of truth |
   | `helm-charts-k8s/templates/gpu-nfd-default-rule.yaml` | build output; drift means the wrong file was edited or `make helm-k8s` was not re-run before committing |
   | `docs/installation/openshift-olm.md` (both blocks) | hand-maintained docs |

2. **`make check-nfd-device-ids`** — thin wrapper, so the check is runnable by
   any contributor with no Claude Code and no CI.

3. **`.claude/hooks/check-nfd-drift.sh`** — `PostToolUse` on `Write|Edit`,
   wired in `.claude/settings.json`. Catches drift in-session, at the moment it
   is introduced, rather than at PR time. Follows the conventions already
   established by `protect-generated.sh`: the matcher can only filter on tool
   name, so the script does a cheap path check first and exits 0 immediately
   for edits to unrelated files; and it **fails open** on a missing `python3`
   or an unparseable payload, so a hook fault can never wedge editing.

4. **`.github/workflows/nfd-device-id-check.yml`** — calls the Makefile target.
   Checked in to **both** repos. GitHub Actions is already active on each:
   pensando runs `pr-plan-check` on every PR to `main` (intended as a required
   status check), and ROCm/gpu-operator runs GitHub Actions PR sanity checks
   (ROCm/gpu-operator#576).

   Deliberately carries no branch filter, so one file serves both default
   branches and also covers backport PRs onto release branches. It is likewise
   not filtered on paths: a path-filtered workflow reports no status on
   unrelated PRs, which stalls merges once it is a required check. The check
   runs in about a second.

### Alternatives considered

- **Sync a NodeFeatureRule in the OLM bundle** (as the Jira recommends) —
  rejected: no such artifact exists, and `bundle/manifests/**` is generated and
  hook-protected regardless.
- **Generate the doc YAML blocks from the source of truth** (marker comments +
  `make ...-sync`) — rejected for now: makes part of a hand-written doc
  generated, for a drift problem that a report-only check plus an in-session
  hook already closes.
- **Hoist device IDs into a single data file** rendered into both the helm
  template and the docs — rejected: modifies a shipping artifact, which is
  disproportionate risk for a documentation bug.
- **A `CLAUDE.md` bullet describing the sync requirement** — rejected: costs
  context on every session regardless of relevance, whereas the hook fires only
  when a tracked file is actually edited.
- **Extending `protect-generated.sh` to block edits to
  `helm-charts-k8s/templates/**`** — rejected as redundant: the drift check
  already flags a hand-edited helm copy, since it would no longer match source.

## Scope

- **In scope:**
  - `docs/installation/openshift-olm.md` — add the missing IDs to both YAML
    blocks. Device-list changes only; no prose added.
  - `docs/troubleshooting.md` — entry for `pci-1002.present=true` with no
    `amd-gpu` label (Jira item 4). This also gives the existing
    "For more detailed troubleshooting steps" pointer at the end of
    `docs/installation/kubernetes-helm.md` something real to land on; that
    pointer previously led to a page with no NFD content at all.
  - `hack/check-nfd-device-ids.py`, `make check-nfd-device-ids`.
  - `.claude/hooks/check-nfd-drift.sh` + `.claude/settings.json` wiring.
  - `.github/workflows/nfd-device-id-check.yml` (both repos).

- **Out of scope:**
  - Prose in `docs/installation/openshift-olm.md` stating that the OLM bundle
    ships no NodeFeatureRule. Drafted and then dropped to keep the diff to the
    device lists; the fact is recorded here and in the troubleshooting entry.
  - `tests/e2e/yamls/charts/gpu-operator/templates/nfd-default-rule.yaml` — a
    pinned v1.0.0 baseline chart used by the upgrade tests
    (`02_cluster_upgrade_policy_test.go`). Its list is stale **by design**;
    refreshing it would invalidate upgrade coverage. Excluded from the checker.
  - `docs/kubevirt/kubevirt.md` — contains `lspci` sample output and
    placeholders, not a NodeFeatureRule list.
  - `tests/e2e/yamls/openshift/nfd-instance.yaml` — labels by vendor only
    (`device` commented out) with no custom rules, so it yields
    `pci-1002.present` but never `amd-gpu`. This matches the symptom described
    in the Jira, but is not the cause of the reported failures: the Go test
    consuming it is unconditionally skipped
    (`01_cluster_core_test.go:90`) and selects on `pci-1002.present` on
    OpenShift anyway. Worth a separate look.
  - The 1.5.1 backport requested in the Jira comments — separate cherry-pick.
  - Any runtime/operator code change.

## Validation

- **Checker, negative:** run `make check-nfd-device-ids` before the doc fix;
  it must fail and list exactly the 16 `amd-gpu` and 2 `amd-vgpu` IDs above.
  This is what drives the doc edit, so the fix cannot be partial.
- **Checker, positive:** run again after the doc fix; must pass.
- **Checker, regression:** temporarily delete one ID from the source of truth
  and confirm the extra-ID direction is reported too, then restore.
- **Hook:** edit a tracked file in a Claude Code session and confirm the drift
  report surfaces; edit an unrelated file and confirm no output and no delay.
- **Hook fail-open:** run the hook with a malformed payload and with `python3`
  masked off `PATH`; must exit 0 in both cases.
- **Docs build:** `make docs` renders without errors.
- **Field check:** on the R9700S cluster, apply the corrected NodeFeatureRule
  and confirm `feature.node.kubernetes.io/amd-gpu=true` appears, then re-run a
  sanity subset that previously failed at `No nodes with AMD/GPU found`.

## Risks and rollback

- **Risk:** a device ID transcribed incorrectly into the docs would send users
  a broken rule. Mitigated by deriving the doc edit from the checker output
  rather than by hand, and by the checker gating the result.
- **Risk:** the hook runs on every `Write`/`Edit`. Mitigated by the early path
  check and by failing open; worst case is a stray message, never a block.
- **Risk:** the parser is regex-driven, so an unusual future YAML shape could
  be silently skipped. Mitigated by the checker erroring if a file yields **no**
  rules at all, so a total parse failure cannot masquerade as "in sync".
- **Rollback:** revert the commits. Documentation-only plus developer tooling;
  no runtime impact, and nothing ships in the operator image or the bundle.
