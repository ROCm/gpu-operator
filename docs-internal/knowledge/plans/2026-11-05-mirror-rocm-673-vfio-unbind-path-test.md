# Mirror ROCm #673: correct the VFIO unbind path test

- **Date:** 2026-11-05
- **Author:** Yan Sun (@yansun1996)
- **Related PR(s):** [ROCm/gpu-operator#673](https://github.com/ROCm/gpu-operator/pull/673)
- **Source author:** John Hull (@johnahull)
- **Source commit:** `951dc835ab5473dbde62db1416065c819fa1ddd8`

## Context

The VFIO worker checks whether a selected PCI device has an existing driver
before unbinding it and binding it to `vfio-pci`. The current condition has
no spaces around the shell test delimiters: `if [-e ".../driver/unbind"]; then`.
Bash attempts to execute a command named `[-e`, reports `command not found`,
and skips the unbind operation even when the path exists. The existing driver
can consequently remain attached when the worker attempts the VFIO binding.

The shared script serves both PF and VF passthrough. This PR brings the
focused fix proposed in ROCm #673 into Pensando and records the required plan.

## Approach

Cherry-pick the source commit with `git cherry-pick -x`, preserving John Hull's
authorship and recording the original commit. The corrected condition is
`if [ -e ".../driver/unbind" ]; then`. Keep the source patch unchanged and add
this plan in a separate commit.

### Alternatives considered

- Reimplement the correction locally: unnecessary because the author's patch
  applies cleanly and directly fixes the shell command.
- Combine related GIM artifact or VFIO selection changes: keep those changes
  in separate PRs so this correction remains independently reviewable.

## Scope

- **In scope:** The one-line correction in
  `internal/controllers/workermgr/scripts/vfio_bind.sh` and this plan file.
- **Out of scope:** PCI device selection, VFIO configuration, GIM artifacts,
  KMM builds, node selectors, dependency updates, and hardware test changes.
- Target Pensando `main`. This fix can merge independently of ROCm #674 and #675.

## Validation

- Run `bash -n internal/controllers/workermgr/scripts/vfio_bind.sh`.
- Evaluate the exact before/after conditions with temporary existing and
  missing paths. Confirm the old condition reports `[-e: command not found`,
  while the corrected condition selects the expected branch without errors.
  This check does not execute PCI unbind or VFIO bind operations.
- Compare the cherry-picked commit's stable patch ID with the source commit.
- Run `git diff --check` and the repository's plan-file gate against the
  final branch.
- Record local results in the PR description. A hardware VFIO/KubeVirt E2E
  run is not part of local validation; hardware follow-up can verify that a
  selected device bound to another driver transitions to `vfio-pci`.

## Risks and rollback

- The fix enables the intended unbind operation for already-selected devices
  with an existing driver. Workloads using those devices can be affected by
  the requested passthrough transition, as intended by the existing worker.
- Revert the cherry-picked code commit, or revert the PR's merged change,
  to restore the previous behavior. This reintroduces the skipped-unbind bug.
