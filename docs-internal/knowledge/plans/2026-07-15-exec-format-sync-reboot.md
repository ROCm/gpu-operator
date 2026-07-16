# Narrow the exec-format-error corruption window on driver-upgrade reboot

- **Date:** 2026-07-15
- **Author:** yansun1996
- **Related PR(s):** #<tbd>
- **Related issue(s) / JIRA:** exec-format-error RCA (driver-upgrade / containerd overlay snapshot corruption)

## Context

During an operator-managed driver upgrade, a GPU node is cordoned, drained,
and rebooted via a privileged pod that runs `sudo reboot` through
`nsenter --all --target=1` (`internal/controllers/upgrademgr.go`). Validation
reproduced dependent operand pods (DRA driver, metrics exporter) failing with:

```
exec /usr/bin/gpu-kubeletplugin: exec format error
exec /home/amd/tools/entrypoint.sh: exec format error
```

The affected images were valid when pulled into a clean environment. On the
GPU node, containerd's unpacked overlayfs snapshots contained zero-byte or
truncated executables (`/usr/bin/gpu-kubeletplugin: 0 bytes`,
`/lib64/libcap.so.2: file too short`) while containerd metadata still described
the images as complete. The RCA established two interacting layers:

1. **Exposure (operator layer):** operand images materialize on the node in the
   reboot-adjacent window. The reboot is issued with no filesystem flush, so
   dirty overlay `upperdir` pages for a just-unpacked image may never reach the
   block device before the node goes down.
2. **Corruption (node layer):** after reboot, containerd trusts and reuses the
   node-local snapshot state instead of re-materializing it. Why the snapshot is
   invalid (containerd 2.1.6 snapshot commit/recovery, overlayfs crash
   consistency, ext4/storage durability, firmware) is **unresolved** and below
   the operator.

This change addresses the part the operator owns: the reboot it issues is
ungraceful (no `sync`), which is the durability gap that lets a reported-complete
image come back corrupt. It cannot prove the corruption is closed — a cold
power-cycle that drops the drive write cache is below the OS — but it removes the
operator's contribution to the exposure.

A second, independent correctness gap is fixed here: the pre-reboot pod
drain/delete list in `getPodsToDrainOrDelete` names five operands
(metrics-exporter, device-config-manager, device-plugin, node-labeller,
test-runner) but omits the DRA driver (`-dra-driver`), which was added as an
operand later. DRA is therefore not explicitly torn down before reboot.

## Approach

- **Flush filesystems before reboot (upgrade path).** Prepend `sync` to the
  reboot pod command so dirty pages — including containerd/CRI-O overlay snapshot
  data — are pushed to the block device before the node goes down.
  - Upgrade path (`upgrademgr.go`, `getRebootPod`): change `... -- sudo reboot`
    to run `sh -c "sync; sudo reboot"` inside the existing
    `nsenter --all --target=1` host context.
  - The remediation reboot path (`reboot.sh`) is intentionally left unchanged.
- **Use plain global `sync`, not `sync -f <path>`.** `sync()` is a whole-kernel
  operation and flushes every mounted filesystem regardless of container runtime
  or its configured storage root. This is deliberately runtime-agnostic:
  containerd (`/var/lib/containerd`), CRI-O / OpenShift
  (`/var/lib/containers/storage`), k3s, and custom `root =` configs are all
  covered without any runtime detection. `sync` runs host-side because the
  reboot pod already enters the host mount namespace via `nsenter`.
- **A single `sync` is sufficient.** On modern Linux (≥ 1.3.20) the `sync`
  syscall blocks until writeback completes and issues the device FLUSH/FUA
  barrier — `sync(2)`: "the same guarantees as `fsync()` called on every file in
  the system." The traditional `sync; sync` idiom is pre-1995 folklore (older
  kernels returned before writeback finished); the second call adds no
  durability. systemd's own shutdown re-syncs and unmounts, giving a second
  barrier after ours.
- **Add the DRA driver to the pre-reboot drain/delete list** in
  `getPodsToDrainOrDelete` so operand teardown before reboot is explicit and
  consistent with the other five operands.

### Recommended node prerequisite (not an operator code change)

The upstream-sanctioned fix for this exact zero-byte / `exec format error`
failure is containerd's **`image_pull_with_sync_fs = true`**
(`[plugins.'io.containerd.cri.v1.images']`), available in containerd 2.x
(including 2.1.6) and **default `false`**. It makes containerd `syncfs` after
each layer unpack, so overlay `upperdir` data is durable at unpack time — closing
the window *continuously*, not only at the operator's reboot instant, and
covering unpacks the operator did not initiate (containerd issue #9497 / PR
#9401). The operator cannot set node containerd config, so this is documented as
a recommended node prerequisite alongside the `sync`-before-reboot mitigation.
The `sync`-before-reboot change remains valuable independently (it also covers
CRI-O and any runtime without an equivalent flag).

### Alternatives considered

- **New `operator.amd.com/gpu-operand-ready` node label gating operand
  scheduling until `Upgrade-Complete`.** Rejected for this PR. Two independent
  design reviews found: (1) the existing `amd-gpu-driver-upgrade:NoSchedule`
  taint — added at cordon, removed only at `Upgrade-Complete`, untolerated by
  operands — already blocks post-reboot operand rescheduling for essentially the
  whole window, leaving the label only a narrow sliver of incremental benefit;
  (2) terminal-but-healthy nodes (`Upgrade-Timed-Out`, `*-Failed` with `.ready`
  restored) would never get the label re-added and would lose operands
  permanently — a regression; (3) adding a key to operand pod-template
  nodeSelectors forces a fleet-wide operand rolling restart on operator upgrade,
  and the cross-controller "backfill before the DaemonSet controller
  re-evaluates" ordering is a race, not a guarantee. Deferred: revisit only if
  testing shows corruption persists after a synced reboot, and then reuse the
  existing taint/state machine rather than a third readiness signal.
- **`systemctl reboot` (full ordered shutdown that stops containerd/kubelet).**
  Rejected: intentionally avoiding stopping system-level services from inside an
  `nsenter` pod (self-eviction / ordering hazard, and the reboot pod itself rides
  on kubelet). `sync; reboot` gets the durability benefit without stopping
  services.
- **`sync -f /var/lib/containerd` (scoped syncfs).** Rejected: runtime- and
  config-specific path; silently flushes the wrong filesystem (or nothing) under
  CRI-O, k3s, or a custom root. Portability of plain `sync` outweighs the
  micro-optimization on a drained, idle node.

## Scope

- **In scope:**
  - `sync` before reboot on the upgrade path (`upgrademgr.go` reboot pod).
  - DRA driver added to the pre-reboot drain/delete list (`upgrademgr.go`).
  - Documenting `image_pull_with_sync_fs = true` as a recommended node
    prerequisite.
- **Out of scope:**
  - The remediation reboot path (`reboot.sh`) — left unchanged.
  - The operand-ready scheduling gate / new node label (deferred).
  - Any containerd/overlayfs snapshot validation or post-reboot re-pull, and
    setting node containerd config from the operator.
  - The node-layer corruption mechanism itself (containerd overlay unpack
    durability / ext4 / firmware) — below the operator. Note this is resolved
    upstream by `image_pull_with_sync_fs` (see prerequisite above), which the
    operator cannot enable directly.
  - Any DeviceConfig API / CRD change (none required).

## Validation

- **Unit tests:** existing `upgrademgr` tests cover `getPodsToDrainOrDelete`;
  extend to assert DRA pods (`<dc>-dra-driver-*`) are selected for drain.
- **Build:** `go build ./...` (done for the drain change).
- **Manual / hardware:** run an operator-managed driver upgrade with
  `RebootRequired: true` on a GPU node; confirm the reboot pod command includes
  `sync` and the node journal shows the flush before reboot; confirm DRA and
  metrics-exporter operands come back without `exec format error` across repeated
  upgrade cycles. Validate on both a containerd (k8s) and CRI-O (OpenShift)
  cluster to exercise the runtime-agnostic `sync`.

## Risks and rollback

- **Known risks:**
  - `sync` guarantees data reached the block device, not the physical platter/
    NAND. A cold power-cycle that drops the drive write-back cache can still
    corrupt; that layer is below the operator. This change **narrows** the
    exposure window, it does not prove closure.
  - Global `sync` flushes all filesystems; on a drained, cordoned node the dirty
    set is small and it runs once before a multi-minute reboot, so cost is
    negligible. A pathological concurrent writer on the node could lengthen it,
    but the node is drained by this point.
- **Rollback plan:** both changes are self-contained and revert cleanly
  (restore the original reboot command, drop the DRA prefix from the drain
  list). No state, CRD, or migration to unwind.
