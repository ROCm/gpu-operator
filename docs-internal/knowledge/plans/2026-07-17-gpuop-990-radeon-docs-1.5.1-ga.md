# GPUOP-990: Claim Radeon support + 1.5.1 GA docs updates

- **Jira:** [GPUOP-990](https://pensando.atlassian.net/browse/GPUOP-990)
- **Type:** Bug (documentation) · **Labels:** `documentation`, `v1.5.1`
- **Component:** operator

## Context

The public docs (`instinct.docs.amd.com/projects/gpu-operator`) describe the
GPU Operator as managing **AMD Instinct** accelerators only, even though the
v1.5.1 release adds **AMD Radeon™ AI PRO** (RDNA 4) support. For the 1.5.1 GA
cut, the docs must:

1. Claim Radeon support wherever the product is framed as Instinct-only.
2. Update the supported Kubernetes / OpenShift version matrix (k8s 1.36,
   OpenShift 4.22).
3. Promote the v1.5.1 release notes from **beta** to **GA** and capture the
   fixes shipping in the GA cut.

## Approach

Documentation-only change. Edits are scoped to product-level framing and the
version matrix; feature-specific "Instinct" references (DCM partitioning,
XGMI topology scheduling, AFID event lists, KubeVirt VF examples) are left
unchanged because those are CDNA-only features that genuinely do not apply to
Radeon (RDNA) GPUs.

Brand naming follows the official AMD product page: **AMD Radeon™ AI PRO**
(models R9700 / R9700S).

### Changes

- **`docs/index.md`**
  - Intro line: "AMD Instinct™ and AMD Radeon™ GPU accelerators".
  - OS/Platform matrix: Kubernetes `1.29-1.35` → `1.29-1.36`; RHCOS/OpenShift
    `4.16—4.20` → `4.16—4.22`.
  - (Hardware table already lists the Radeon AI PRO SKUs.)
- **`README.md`**
  - Intro line mirrors `index.md` (Instinct + Radeon).
- **`docs/installation/kubernetes-helm.md`**
  - Prerequisite: `v1.29.0 to v1.35.x` → `v1.29.0 to v1.36.x`.
- **`docs/releasenotes.md`** (v1.5.1 section)
  - Heading + summary: drop "(beta)", GA wording.
  - Instinct/Radeon brand casing normalized (™, "AI PRO").
  - Per-platform ANR notes: "in this beta release" → "in this release".
  - Platform Support: MI350P `k8s 1.29–1.36, OpenShift 4.21–4.22`;
    Radeon AI PRO `k8s 1.29–1.36`.
  - Fixes: add the driver-upgrade reboot `exec format error` fix
    (sync-before-reboot + DRA in drain list), linked to PR #1604. Customer
    name intentionally omitted.

## Scope

**In scope:** documentation strings — product framing, version matrix,
v1.5.1 release notes (beta→GA, versions, fixes).

**Out of scope:** any code change; feature-specific Instinct/CDNA references;
the upgrade-reboot fix itself (shipped separately in PR #1604).

## Validation

- `grep` audit confirms remaining "Instinct" mentions are either
  feature-specific (CDNA-only) or doc-site URLs, not product-support framing.
- Sphinx docs build (`docs/`) renders without errors.
- Manual review of rendered release notes and index compatibility tables.

## Risks / Rollback

- **Risk:** version-matrix claims (k8s 1.36 / OpenShift 4.22) must match what
  QA actually validated for 1.5.1 GA. If validation differs, correct the
  matrix values before merge.
- **Rollback:** revert the doc commits; no runtime impact.
