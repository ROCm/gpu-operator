# Techsupport: collect kubelet device-manager state and host GPU topology

- **Date:** 2026-08-24
- **Author:** Nitish Bhat
- **Related PR(s):** #1661
- **Related issue(s) / JIRA:** [GPUOP-1060](https://pensando.atlassian.net/browse/GPUOP-1060)

## Context

GPUOP-1060 reported that the device-plugin "fails to re-register amd.com/gpu
after partition cycling on MI350X". The techsupport bundle from the failing run
(jobd 33190761) could not settle the question, because of what it does *not*
contain.

The bundle shows the plugin side is healthy — it registers with kubelet and
enumerates GPUs on every restart:

```
plugin.go:140] gpu: Registration for endpoint amd.com_gpu
plugin.go:233] Found 1 AMDGPUs
```

…and then stops. There is nothing from kubelet, which is where the fault
actually lies. Establishing that required standing up a cluster and reading
kubelet's own state directly, where the decisive comparison turned out to be:

```
kubelet_internal_checkpoint:  registered: amd.com/gpu count=32
node status:                  amd.com/gpu = 0
```

kubelet held the devices and did not publish them. No amount of device-plugin
log reading could have shown that.

Two structural gaps made this worse:

1. The techsupport debug DaemonSet had **no tolerations**, so it was evicted by
   `amd-dcm=up:NoExecute` during GPU partitioning — node-level data went missing
   at exactly the moment a partitioning failure was under investigation. The
   metrics-exporter path already carries a documented `missing-data-reason.txt`
   for this same eviction.
2. `amd-smi partition` was only reachable through the metrics-exporter pod,
   which is subject to that same eviction.

## Approach

Extend the debug DaemonSet so node-level state is actually reachable, and
collect the kubelet-side evidence.

- DaemonSet gains `tolerations: [{operator: Exists}]`, `hostPID: true`, and a
  read-only `hostPath: /` mount at `/host`.
- Host commands run via `nsenter -t 1 -m -u -i -n -p`; on-disk state is read
  through `/host`.
- New `<node>/kubelet/` directory:
  - `kubelet-devicemanager.log` — journal filtered to device-manager /
    device-plugin traffic over 3h. Surfaces `Endpoint became unhealthy`.
  - `kubelet-journal.log` — bounded raw journal (60 min, 5000 lines).
  - `device-plugins-dir.txt` — socket names and mtimes.
  - `kubelet_internal_checkpoint.json` — kubelet's device registry; also
    exposes stale `PodDeviceEntries` from deleted pods.
  - `kubelet-version-flags.txt`.
- New `<node>/gpu-topology/` directory: KFD node count, `amdgpu_xcp` device
  count, amdgpu module version, and `amd-smi partition` read from the host.

Journal output is deliberately bounded. Techsupport is collected once per failed
testcase (~28 bundles in the reference run); an unbounded kubelet journal is tens
of MB per node.

### Alternatives considered

- **Unbounded `journalctl -u kubelet`** — rejected. The first implementation
  produced a single 54 MB file, which at ~28 bundles per CI run would push the
  artifact into gigabytes.
- **Grep-only, no raw journal** — rejected. The filter can miss context
  immediately around a failure; a small bounded raw slice is cheap insurance.
- **A dedicated image with `journalctl` instead of busybox + `nsenter`** —
  rejected. Adds an image dependency to a tool whose value is that it runs
  anywhere; `nsenter` into PID 1 reuses the host's own systemd.
- **Mount only `/var/lib/kubelet` and `/var/log` rather than `/`** — rejected.
  `journalctl` needs the host's binary and journal layout; a single read-only
  root mount is simpler and no more privileged than the existing
  `privileged: true` container.

## Scope

- **In scope:** `tools/techsupport_dump.sh` — debug DaemonSet spec and the
  node-level collection block.
- **Out of scope:** the underlying kubelet defect (tracked in GPUOP-1060; likely
  kubernetes/kubernetes#121994, fixed in k8s v1.37 and not backported to 1.36.x),
  any device-plugin change, and any DCM change.

## Validation

- **Unit tests:** none — shell collector with no test harness.
- **Manual / hardware:** run end-to-end on a 2-node cluster (Kubernetes 1.36.2,
  gpu-operator v1.5.1, KMM container driver, 8x MI308X worker). Verified:
  - Collection succeeds on both control-plane and worker nodes.
  - Every new file is populated with real data, e.g.
    `kfd_topology_nodes: 10`, `amdgpu_version: 6.19.14.31400100`,
    checkpoint containing `RegisteredDevices` and a stale `PodDeviceEntry`,
    and 4619 journal lines including 9 `Endpoint became unhealthy` events.
  - Complete bundle is 832K.
- **Syntax:** `bash -n tools/techsupport_dump.sh`.

## Risks and rollback

- **Risks:**
  - The DaemonSet now tolerates all taints, so it schedules onto nodes it
    previously skipped. It is short-lived (`sleep 1h`, deleted on script exit)
    and only reads, so impact is limited to briefly occupying a pod slot.
  - Mounting host root read-only at `/host` widens what the collector can see.
    The container was already `privileged: true`, so this does not raise the
    privilege ceiling, but it does make host paths reachable.
  - `nsenter` may be unavailable in a future busybox variant. Every command is
    wrapped in `|| true`, so a failure yields an empty file rather than a failed
    bundle.
- **Rollback:** revert the single commit. No CRD, chart, or runtime component is
  touched; nothing depends on the new files existing.
