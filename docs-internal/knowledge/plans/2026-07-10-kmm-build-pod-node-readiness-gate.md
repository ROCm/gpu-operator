# Plan: Gate KMM Build Pod Deletion on Node Readiness

## Context

When upgrading the amdgpu driver via KMM (Kernel Module Management), Kaniko build pods
occasionally hit `ContainerStatusUnknown` due to a `NodeNotReady` event (e.g., network
instability). The pod watcher at `internal/controllers/watchers/pod.go` was deleting these
pods unconditionally and immediately.

This eager deletion triggers KMM to spawn a replacement build pod before the kubelet's
volume registry has re-synced from the `NodeNotReady` window. The replacement pod then
fails immediately with:

```
FailedMount: MountVolume.SetUp failed for volume "dockerfile":
object "kube-amd-gpu"/"ubuntu-22.04-devcfg-clusterwide-gpu-kube-amd-gpu" not registered
```

The Dockerfile ConfigMap is not lost — it persists in etcd, owned by the DeviceConfig.
The error is a kubelet VolumeManager informer-sync issue: the kubelet's in-memory volume
registry has not yet re-populated after the node recovered.

Observed in CI job 32149765 on node asrock-126-b3-1b (MI300X, ubuntu-22.04, kernel
6.8.0-100-generic) during `test_driver_upgrade_cycle[30.30]`. The build pod ran for ~20
min, was killed, and the replacement hit FailedMount immediately. The node remained in
`Upgrade-Started` for the full 20-min polling window, causing a test timeout and cascading
failures on subsequent tests.

## Approach

Gate the `ContainerStatusUnknown` pod deletion on the node's current Ready condition:

- Before deleting, fetch the node by `pod.Spec.NodeName`.
- If the node is not Ready, skip deletion and log at Info level. The pod watcher will
  fire again on the next pod status update (when the kubelet re-syncs and transitions
  the pod to Failed), at which point the node is Ready and deletion proceeds cleanly.
- If the node is Ready, the `ContainerStatusUnknown` is a genuine stuck state (not a
  transient network partition artifact) — delete as before.

This is a structural fix: it eliminates the race rather than racing to enqueue a reconcile.

**Label-value correction (found in review, PR #1595):** The build-pod gate and the
pre-existing `hasExpectedPodLabel` predicate both matched
`kmm.node.kubernetes.io/pod-type == "builder"`. The deployed KMM (ROCm fork,
`release-v1.5.0`) actually labels build pods `pod-type=build` (`PodTypeBuild = "build"`
in KMM `internal/utils/podhelper.go`; sign pods are `"sign"`). `"builder"` never
matched, so the auto-remove-unknown-build-pod feature (originally PR #133) and its
watch predicate were dead code against ROCm KMM. Corrected both sites via shared
local constants (`kmmPodTypeLabelKey`, `kmmPodTypeBuildValue`, `kmmPodTypeSignValue`);
KMM's constant is in an internal, non-importable package so the values are duplicated.
This activates the feature, and the node-readiness gate above makes that activation safe.

**Sign pods included:** Sign pods are real in this deployment (secure-boot signing via
the `ImageSign` spec on DeviceConfig) and share KMM's identical recreate-if-missing
lifecycle — `internal/sign/pod/manager.go` `Sync` recreates a missing sign pod exactly
as the build manager does. So the same delete-to-retrigger + node-readiness gate applies.
Both the gate and the watch predicate now admit `pod-type in {build, sign}` via the
shared `isKMMBuildOrSignPod` helper. Worker (workerMgr) pods are intentionally excluded:
they mount `HostPath` volumes (not the Dockerfile ConfigMap), so the FailedMount race
does not apply, and they already have a dedicated lifecycle path (`handleWorkerMgrPodEvt`)
respawned by our own workermgr reconcile rather than KMM's `Sync`.

**Alternatives considered:**
- Enqueue a DeviceConfig reconcile after deletion to re-assert the ConfigMap: rejected
  because the race between the enqueue and KMM's pod creation cannot be closed reliably.
- Add `Owns(&v1.ConfigMap{})` watch: useful defense-in-depth for accidental ConfigMap
  deletion but does not address the kubelet informer-sync issue; deferred.

## Scope

- **In scope:** `internal/controllers/watchers/pod.go` — node-readiness check before
  `ContainerStatusUnknown` deletion of KMM build and sign pods, plus the pod-type
  label-value correction (build/sign) in the gate and watch predicate.
- **Out of scope:** worker (workerMgr) pods (different volume/lifecycle), test timeout
  adjustment (separate PR), ConfigMap watch defense-in-depth.

## Validation

- `go build ./internal/controllers/watchers/...` passes.
- Unit test: add a test for `PodEventHandler.Update` covering ContainerStatusUnknown +
  NodeNotReady asserting `Delete` is NOT called (follow-up).
- Integration: re-run `test_driver_upgrade_cycle[30.30]` on asrock-126-b3-1b to confirm
  the build pod is no longer killed and replaced mid-build during a NodeNotReady event.

## Risks / Rollback

- **Risk:** If a node enters a permanently-NotReady state, the ContainerStatusUnknown pod
  is never deleted by this watcher. However, Kubernetes' pod eviction controller
  (`pod-eviction-timeout`, default 5 min) will evict it independently, and KMM's own
  reconcile loop will handle the replacement cleanly after eviction.
- **Rollback:** Revert `internal/controllers/watchers/pod.go` to restore the original
  unconditional deletion behavior.
