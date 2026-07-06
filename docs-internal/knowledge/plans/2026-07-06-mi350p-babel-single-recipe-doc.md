# MI350P RVS Recipe Table Correction: babel_single

- **Date:** 2026-07-06
- **Author:** yansun1996
- **Related PR(s):** TBD
- **Related issue(s) / JIRA:** N/A

## Context

Systest reported a test runner failure on MI350P nodes:

```
Trigger AUTO_UNHEALTHY_GPU_WATCH cannot find corresponding test config file
/opt/rocm/share/rocm-validation-suite/conf/MI350P-600W/babel.conf
```

Investigation confirmed that the RVS tarball (`amdrocm7-rvs-1.4.24-454`) ships
`babel_single.conf` (not `babel.conf`) inside both `MI350P-450W/` and
`MI350P-600W/` conf folders. The appendix docs incorrectly showed `✓` under the
`babel` column for both MI350P power profiles, misleading operators into
specifying `babel` as the recipe name in their config maps.

## Approach

- Add a `babel_single` column to the RVS test recipe table in
  `docs/test/appendix-test-recipe.md`.
- Remove the `✓` from the `babel` column for `MI350P-450W` and `MI350P-600W`.
- Add `✓` in the new `babel_single` column for both MI350P rows.
- All other GPU rows are unaffected.

### Alternatives considered

- Fallback in test runner code — rejected; the operator config should use the
  correct recipe name. Docs are the right fix.

## Scope

- **In scope:** `docs/test/appendix-test-recipe.md` table update only.
- **Out of scope:** RVS conf file naming, test runner fallback logic.

## Validation

- Verified actual RVS conf folder contents in the `test-runner:rc1-test` image
  on `smc300x-ccs-aus-gpuf268` (8× MI350P):
  - `MI350P-450W/`: `babel_single.conf`, `gst_single.conf`, `iet_stress.conf`
  - `MI350P-600W/`: `babel_single.conf`, `gst_single.conf`, `iet_stress.conf`
- Doc-only change; no code paths affected.

## Risks and rollback

- Known risks: none — doc-only change.
- Rollback plan: revert the single file edit.
