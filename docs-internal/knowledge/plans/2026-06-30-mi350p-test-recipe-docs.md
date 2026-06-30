# MI350P Test Recipe Documentation

- **Date:** 2026-06-30
- **Author:** yan.sun3@amd.com
- **Related PR(s):** #1567
- **Related issue(s) / JIRA:** N/A

## Context

MI350P (AMD Instinct MI350P, device 0x75a8) support was added to the
test runner in device-metrics-exporter PR #1410 and #1421. Recipes were
validated on real hardware (3× AMD Instinct MI350P node, TheRock 7.14.0rc0,
RVS 1.4.24, AGFHC 1.32.0). The gpu-operator docs need to reflect which
recipes are available and verified for MI350P.

## Approach

Two documentation files updated:

**`docs/test/appendix-test-recipe.md`** (RVS appendix):
- MI350P ships in two TDP variants with separate RVS recipe folders
  (`MI350P-450W` and `MI350P-600W`), so it is listed as two rows
- Both variants support: `babel_single`, `gst_single`, `iet_stress`

**`docs/test/agfhc.md`** (AGFHC recipe matrix):
- MI350P row added with verified recipes marked
- `all_lvl5` and `single_pass` intentionally left blank — `minihpl`
  binary in AGFHC 1.32.0 is incompatible with TheRock 7.14 rocblas
  kernel layout; pending fix from AGFHC team
- `hbm_lvl5`, xgmi, burnin, hsio, rochpl_isolation left blank — these
  recipes do not exist for MI350P in AGFHC 1.32.0
- MI350P not added to partition profile table — not validated
