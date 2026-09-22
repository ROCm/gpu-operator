# AGFHC 1.34.2 recipe matrix doc fix

- **Date:** 2026-09-22
- **Related PR(s):** TBD (target: pensando/gpu-operator main)
- **Related issue(s) / JIRA:** NO-JIRA

## Context

The AGFHC recipe × GPU support matrix in `docs/test/agfhc.md` had gaps
introduced when the doc was rewritten from a single-GPU (MI300X-only)
table to a multi-GPU matrix. The `acf_lvl*` recipe columns were dropped
and the MI350P row was incomplete.

Verified by pulling the shipped test-runner image
(`amdpsdo/test-runner:agfhc-v1.5.2-12`, AGFHC 1.34.2) and listing
`/opt/amd/agfhc/recipes/<gpu>/*.yml` for every GPU directory.

## Approach

- Restore `acf_lvl1`–`acf_lvl4` columns to the recipe support matrix
- Fix MI350P row: mark `all_lvl5`, `single_pass`, `hbm_lvl5`, `acf_lvl*`
  as supported (present in shipped image)
- Add MI350P to the partition profiles table (SPX/NPS1, 1 and 4 GPUs)
- Add `acf_lvl*` entries to the recipe name/title legend

### Alternatives considered

- Regenerate entire doc from scratch — rejected, too much churn for a
  targeted fix. Automation guide kept as a local reference for future
  releases.

## Scope

- **In scope:** `docs/test/agfhc.md` recipe matrix, partition profiles,
  recipe legend.
- **Out of scope:** CLI arguments table (unchanged in 1.34.2), RVS docs,
  MI358X (not yet user-facing), partition profiles for non-MI350P GPUs
  (unchanged).

## Validation

- Pulled `amdpsdo/test-runner:agfhc-v1.5.2-12` and ran:
  ```
  docker run --rm --entrypoint sh <image> -c \
    'ls /opt/amd/agfhc/recipes/<gpu>/*.yml'
  ```
  for every GPU directory. Cross-referenced with
  `github.amd.com/dctools/agfhc` `recipes/` directory via API.
- AGFHC release notes (v1.32.0 → v1.34.2) confirmed no new recipes or
  platforms — changes were ROCm 10 compat + internal fixes only.

## Risks and rollback

- Low risk — doc-only change, no code impact.
- MI350P recipe additions are based on image filesystem presence; if any
  were intentionally excluded by the AGFHC team as untested, they can be
  reverted per-cell in a follow-up.
- Rollback: `git revert <commit>`.
