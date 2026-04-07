# AMD GPU Operator — Failure Pattern Quick Reference

Use this table to map observable symptoms to root causes without reading the full runbook.
Each row links to a Phase in SKILL.md for the detailed fix.

---

## Full Symptom → Root Cause Table

| Observable Symptom | Where to Look | Root Cause | Phase | Key Signal |
| --- | --- | --- | --- | --- |
| Controller pod stuck **Pending** | `describe pod` → Events | Node has custom taint; controller deployment lacks toleration | 2 | `0/N nodes available: taint ... not tolerated` |
| Controller pod stuck **Pending** (no taints) | `describe pod` → Events | Insufficient node resources (CPU/memory) | 2 | `Insufficient cpu/memory` |
| All DaemonSets **DESIRED=0** | `get deviceconfig -o yaml` → `spec.selector` | DeviceConfig `nodeSelector` value wrong vs NFD labels | 4A | Label is `amd-gpu: "true"` but selector says `"enabled"` |
| All DaemonSets **DESIRED=0** on SR-IOV/vGPU node | `get node --show-labels` | DeviceConfig selects `amd-gpu` (physical) but node has `amd-vgpu` | 4B | Node has `amd-vgpu: "true"`, selector uses `amd-gpu: "true"` |
| All DaemonSets **DESIRED=0** on new GPU hardware | `get node --show-labels` + `get nodefeaturerule` | New GPU device ID (e.g. MI325X = `74e0`) missing from NFD NodeFeatureRule | 4C | `pci-1002.present: true` set; `amd-gpu: "true"` absent |
| Second DeviceConfig deploys **nothing** | Controller logs | Overlapping selector with first DeviceConfig | 4D | Controller logs: `overlapping selector` |
| Exporter **DESIRED=0**, device plugin OK | `get deviceconfig -o yaml` → `spec.metricsExporter.selector` | Per-component exporter selector uses label not present on GPU nodes | 4E | `spec.metricsExporter.selector.nodeSelector` has wrong label |
| Operand pods stuck **Pending** (all affected) | `describe pod` → Events | GPU worker node tainted; DaemonSet tolerations missing | 5A | `0/N nodes available: taint ... not tolerated` |
| **config-manager** pods `CreateContainerConfigError` | `describe pod` + `get configmap` | `spec.configManager.config.name` references non-existent ConfigMap | 5B | Event: `configmap "X" not found`; typo in name |
| **metrics-exporter** pods `CreateContainerConfigError` | `describe pod` + `get configmap` | `spec.metricsExporter.config.name` references non-existent ConfigMap | 5B | Event: `configmap "X" not found`; typo in name |
| **test-runner** pods `CreateContainerConfigError` | `describe pod` + `get configmap` | `spec.testRunner.config.name` references non-existent ConfigMap | 5B | Event: `configmap "X" not found`; typo in name |
| KMM driver pods **CrashLoopBackOff** | KMM worker pod logs | Process using GPU; KMM can't unload old driver to load new one | 6a | `modprobe: ERROR: could not remove 'amdgpu': Device or resource busy` |
| Device plugin/exporter stuck **Init 0/1** (KMM mode) | Init container logs + KMM controller logs | `amdgpu` kernel module not loaded; KMM driver pipeline not complete | 6a | Init logs: `amdgpu driver is not loaded`; check KMM |
| Device plugin/exporter stuck **Init 0/1** (inbox mode) | Init container logs + node labels | `amdgpu` module not pre-installed or failed to load | 6b | Init logs: `amdgpu driver is not loaded`; no KMM involved |
| No driver DaemonSet; **builder pod Failed** | Builder pod logs | Kernel version incompatible with driver source (e.g. RC kernel vs stable driver) | 7A | Build logs: `unknown type '__no_sanitize_address'`; `exit status 1` |
| No driver DaemonSet; KMM loops with **DNS timeout** | KMM controller logs | Cluster DNS (CoreDNS) cannot resolve image registry hostname | 7B | KMM logs: `lookup registry.example.com ... i/o timeout` |
| No driver DaemonSet; KMM **HTTPS error** | KMM controller logs | Registry is HTTP-only; KMM defaults to HTTPS | 7C | KMM logs: `http: server gave http response to https client` |
| No driver DaemonSet; KMM **cannot find secret** (typo) | KMM logs + `get secret` | `spec.driver.imageRegistrySecret.name` typo; similar secret exists | 7D | KMM logs: `cannot find secret gpu-registry-creds`; similar names visible |
| No driver DaemonSet; KMM **cannot find secret** (missing) | KMM logs + `get secret` | Registry secret was never created in operator namespace | 7E | KMM logs: `cannot find secret gpu-registry-creds`; no similar secrets |
| Device plugin Running but `amd.com/gpu: "0"` | Node allocatable + dmesg | `amdgpu` kernel module crashed after initial load (HW fault, firmware, PCIe) | 8 | dmesg: `VM_L2_PROTECTION_FAULT`; `GPU reset failed`; `probe failed -110` |
| `helm install` **hangs or fails** | `kubectl get pods -A` + `get validatingwebhookconfigurations` | cert-manager not running or webhook CA bundle stale | 12 | cert-manager pods not Ready; webhook CA bundle empty |
| `helm upgrade` **hangs or fails** | `get jobs -n $NS` | Pre-upgrade hook Job failed or CRD upgrade hook stuck | 13 | Hook job status: Failed, ImagePullBackOff, or Pending |
| `helm uninstall` **times out** | `get jobs -n $NS` | Pre-delete hook Job stuck in `ImagePullBackOff` (bad operator image ref) | 10 | Job pod: `ImagePullBackOff` on hook job |
| DeviceConfig stuck **Terminating** | `get deviceconfigs` | Finalizer `amd.node.kubernetes.io/deviceconfig-finalizer` not removed (operator down) | 10 | CR shows `deletionTimestamp` but finalizers list non-empty |

---

## Decision Tree

```text
Are all pods Running and GPUs allocatable?
└─ NO ──┬─ Controller pod Pending?
        │   └─ YES → Phase 2 (controller taint)
        │
        ├─ DaemonSets DESIRED=0?
        │   └─ YES → Phase 4 (selector/NFD)
        │       ├─ vGPU/SR-IOV node?  → 4B
        │       ├─ New GPU model?     → 4C
        │       ├─ Multiple DCs?      → 4D
        │       └─ Exporter only?     → 4E
        │
        ├─ DaemonSet DESIRED>0, pods Pending?
        │   └─ YES → Phase 5
        │       ├─ Taint in events?           → 5A
        │       └─ ConfigMap error in events? → 5B (check which component)
        │
        ├─ Pods at Init 0/1?
        │   └─ YES → Phase 6 (driver not loaded)
        │       ├─ spec.driver.enable=true?
        │       │   ├─ Builder pod Failed?   → Phase 7A (kernel compat)
        │       │   ├─ KMM DNS timeout?      → Phase 7B
        │       │   ├─ KMM HTTPS error?      → Phase 7C
        │       │   └─ KMM secret error?     → Phase 7D or 7E
        │       └─ spec.driver.enable=false? → Phase 6b (inbox driver)
        │
        └─ Device plugin Running, amd.com/gpu=0?
            └─ YES → Phase 8 (kernel crash)
                └─ Must check dmesg → Phase 9 (debug container)

Operational issues:
├─ helm install hangs/fails? → Phase 11 (kubectl access, admin role, cert-manager, webhooks)
├─ helm upgrade hangs/fails? → Phase 12 (pre-upgrade hooks, CRD jobs)
├─ helm uninstall hangs? → Phase 10 (pre-delete hook job)
└─ DeviceConfig stuck Terminating? → Phase 10 (finalizer)
```

---

## Healthy Cluster Reference State

### With Device Plugin (Traditional)

```bash
$ kubectl get pods -n kube-amd-gpu -o wide
NAME                                          READY   STATUS    NODE
amd-gpu-operator-controller-manager-0        1/1     Running   control-plane
kmm-operator-controller-manager-0            1/1     Running   control-plane
kmm-webhook-service-0                        1/1     Running   control-plane
amd-device-plugin-<hash>                     1/1     Running   gpu-worker-0
amd-metrics-exporter-<hash>                  1/1     Running   gpu-worker-0

$ kubectl get node gpu-worker-0 -o jsonpath='{.status.allocatable.amd\.com/gpu}'
8

$ kubectl get node gpu-worker-0 --show-labels | tr ',' '\n' | grep -E "amd|gpu"
feature.node.kubernetes.io/amd-gpu=true
amd.com/gpu-device-plugin=true
amd.com/amdgpu-driver=6.8.0
```

Key healthy indicators (Device Plugin):

- `amd.com/amdgpu-driver` label present on GPU nodes
- `amd.com/gpu` allocatable count equals physical GPU count (e.g. `8`)
- All DaemonSets: `DESIRED == READY`
- DeviceConfig status: `OperatorReady: True`

### With DRA Driver (K8s 1.32+)

```bash
$ kubectl get pods -n kube-amd-gpu -o wide
NAME                                          READY   STATUS    NODE
amd-gpu-operator-controller-manager-0        1/1     Running   control-plane
kmm-operator-controller-manager-0            1/1     Running   control-plane
kmm-webhook-service-0                        1/1     Running   control-plane
default-dra-driver-<hash>                    1/1     Running   gpu-worker-0
amd-metrics-exporter-<hash>                  1/1     Running   gpu-worker-0

$ kubectl get deviceclass gpu.amd.com
NAME          AGE
gpu.amd.com   10m

$ kubectl get resourceslices
NAME                                     DRIVER        NODE          AGE
gpu-worker-0-gpu.amd.com-gpu-0-abc123   gpu.amd.com   gpu-worker-0  5m
gpu-worker-0-gpu.amd.com-gpu-1-def456   gpu.amd.com   gpu-worker-0  5m
...

$ kubectl get node gpu-worker-0 --show-labels | tr ',' '\n' | grep -E "amd|gpu"
feature.node.kubernetes.io/amd-gpu=true
amd.com/amdgpu-driver=6.8.0
```

Key healthy indicators (DRA Driver):

- `amd.com/amdgpu-driver` label present on GPU nodes
- `DeviceClass` gpu.amd.com exists
- `ResourceSlices` published (one per GPU on each node)
- DRA driver DaemonSet: `DESIRED == READY`
- DeviceConfig status: `OperatorReady: True`
- **Note:** No `amd.com/gpu` allocatable resources (DRA uses ResourceSlices instead)
