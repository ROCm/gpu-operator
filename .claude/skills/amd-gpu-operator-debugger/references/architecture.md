# AMD GPU Operator — Component Architecture

## Operators Involved

The AMD GPU Operator stack consists of three independent operators that must all be
running for GPUs to be fully available:

```text
┌─────────────────────────────────────────────────────────────────┐
│  AMD GPU Operator (amd-gpu-operator-controller-manager)         │
│  Namespace: kube-amd-gpu                                        │
│  Watches: DeviceConfig CR                                       │
│  Creates/manages:                                               │
│    - device plugin DaemonSet       (amd-device-plugin-*)       │
│    - metrics exporter DaemonSet    (amd-metrics-exporter-*)    │
│    - config manager DaemonSet      (amd-config-manager-*)      │
│    - test runner DaemonSet         (amd-test-runner-*)         │
│    - KMM Module CR  ← only when spec.driver.enable=true        │
└──────────────────────────┬──────────────────────────────────────┘
                           │ creates Module CR
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  KMM Operator (kmm-operator-controller-manager)                 │
│  Namespace: kube-amd-gpu (or kmm-operator-system)              │
│  Watches: Module CR                                             │
│  Creates:                                                       │
│    - Builder pod: compiles amdgpu.ko for current kernel        │
│    - Driver DaemonSet (kmm-worker pods): loads amdgpu.ko       │
│      on each matching GPU node after successful build           │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  NFD (Node Feature Discovery)                                   │
│  Namespace: nfd (or node-feature-discovery)                     │
│  Runs INDEPENDENTLY of KMM/driver loading                       │
│  Scans PCI bus for AMD GPU vendor IDs / device IDs             │
│  Applies node labels used by DeviceConfig selector:            │
│    feature.node.kubernetes.io/amd-gpu: "true"   (physical GPU) │
│    feature.node.kubernetes.io/amd-vgpu: "true"  (SR-IOV VF)   │
│  Labels appear after NFD operator install + NFD rule push      │
└─────────────────────────────────────────────────────────────────┘
```

## Key Distinction from NVIDIA GPU Operator

| Aspect | NVIDIA GPU Operator | AMD GPU Operator |
| --- | --- | --- |
| Driver management | Ships driver DaemonSet directly | Delegates to KMM operator |
| Driver DaemonSet owner | nvidia-gpu-operator | KMM (kmm-worker pods) |
| Driver packaging | Container image | Compiles kernel module via builder pod |
| Init container dependency | GPU Feature Discovery | Waits for amdgpu.ko to load |

AMD GPU Operator does **not** ship a driver DaemonSet. The driver load/unload pod
(`kmm-worker`) is created and owned by KMM after a successful out-of-tree build.

## Driver Modes

### Mode A: Out-of-tree driver (spec.driver.enable=true)

```text
DeviceConfig.spec.driver.enable=true
  → GPU Operator creates Module CR
    → KMM creates builder pod (compiles amdgpu.ko for node's kernel)
      → KMM creates driver DaemonSet (kmm-worker pods)
        → kmm-worker pod loads amdgpu.ko on the node
          → /dev/kfd and /dev/dri/ appear
            → device plugin init container detects driver, main container starts
              → device plugin registers amd.com/gpu resources with kubelet
```

**When this breaks:** see SKILL.md Phase 7 (KMM failures).

### Mode B: Inbox/pre-installed driver (spec.driver.enable=false)

```text
DeviceConfig.spec.driver.enable=false (or omitted)
  → GPU Operator skips Module CR creation
  → amdgpu.ko must already be loaded on the node (via DKMS, distro package, etc.)
  → device plugin init container checks for loaded driver
    → if driver present: main container starts normally
    → if driver absent: pod stays at Init 0/1 forever
```

**When this breaks:** see SKILL.md Phase 6b (inbox driver not loaded).

## Pod Taxonomy

| Pod name pattern | Type | Managed by | Purpose |
| --- | --- | --- | --- |
| `amd-gpu-operator-controller-manager-*` | Deployment | Helm | Operator main loop |
| `kmm-operator-controller-manager-*` | Deployment | KMM Helm | KMM main loop |
| `kmm-webhook-service-*` | Deployment | KMM Helm | Validates Module CRs |
| `nfd-master-*` | Deployment | NFD Helm | Aggregates node features |
| `nfd-worker-*` | DaemonSet | NFD Helm | Scans each node's hardware |
| `<dc-name>-gpu-driver-build-*` | Pod (Job) | KMM | Compiles amdgpu.ko |
| `<dc-name>-gpu-driver-*` (kmm-worker) | DaemonSet | KMM | Loads amdgpu.ko on node |
| `amd-device-plugin-*` | DaemonSet | AMD GPU Operator | Registers GPUs with kubelet (traditional) |
| `<dc-name>-dra-driver-*` | DaemonSet | AMD GPU Operator | Registers GPUs via DRA (K8s 1.32+) |
| `amd-metrics-exporter-*` | DaemonSet | AMD GPU Operator | Exposes GPU metrics |
| `amd-config-manager-*` | DaemonSet | AMD GPU Operator | Applies GPU tuning config |
| `amd-test-runner-*` | DaemonSet | AMD GPU Operator | Runs GPU validation tests |
| `node-problem-detector-*` | DaemonSet | NPD Helm (separate) | Detects GPU issues, sets node conditions |
| `argo-workflow-controller-*` | Deployment | Argo/GPU Operator Helm | Manages remediation workflows |
| `<node>-remediation-*` | Workflow | GPU Operator | Remediates GPU issues on specific node |

## Init Container Dependency Chain

All GPU operator operand pods (device plugin, metrics exporter, etc.) have an
init container named `wait-for-driver` that polls until it can confirm the
`amdgpu` kernel module is loaded:

```text
init container: wait-for-driver
  └─ loops checking: lsmod | grep amdgpu
  └─ exits 0 when driver is loaded
  └─ if driver never loads: pod stays at Init 0/1 indefinitely
```

This means **ALL operand pod Init 0/1 issues trace back to the amdgpu driver not
being loaded**, regardless of whether KMM or inbox driver mode is used.

## Node Labels Applied by the Operator

After successful deployment, healthy GPU nodes have these labels:

```yaml
feature.node.kubernetes.io/amd-gpu: "true"        # set by NFD
amd.com/gpu-device-plugin: "true"                 # set by device plugin
amd.com/amdgpu-driver: "6.8.0"                   # set after KMM driver loads
```

The `amd.com/amdgpu-driver` label is **diagnostic**: if it's missing on a node that
NFD labeled with `amd-gpu: "true"`, the driver did not load successfully.

## Component Enablement

All operator-managed components (device plugin, DRA driver, metrics exporter, config manager,
test runner, auto remediation) can be enabled/disabled via DeviceConfig fields. See
`references/dra_npd_anr.md` for detailed diagnostics on components that may not be
present in all clusters:

- **DRA Driver** (`spec.draDriver.enable`) - Alternative to Device Plugin; requires K8s 1.32+
- **Node Problem Detector** - Separate installation; detects GPU issues via metrics
- **Auto Node Remediation** (`spec.remediationWorkflow.enable`) - Requires NPD + Argo Workflows

**Mutual exclusions:**
- Device Plugin and DRA Driver cannot both be enabled on the same DeviceConfig
- Only one remediation workflow can run per node at a time
