---
name: Component Implementation Details
description: Deep dive into each GPU Operator component's implementation and behavior
type: reference
---

# Component Implementation Details

## 1. Driver Management (KMM Integration)

### Purpose
Install, manage, and upgrade amdgpu kernel driver modules on GPU worker nodes.

### Implementation
- **Handler**: `internal/kmmmodule/`
- **Integration**: Creates/manages KMM `Module` CRD
- **Two modes**:
  1. Real KMM integration (default)
  2. NoOp mode (when KMM_WATCH_ENABLED=false)

### Driver Types
1. **container** (default): Standard amdgpu-dkms for bare metal or guest VMs
2. **vf-passthrough**: MxGPU GIM driver for VF generation + vfio-pci binding
3. **pf-passthrough**: Direct PF mount to vfio-pci

### Image Management
- **Repository format**: `<registry>/<namespace>/amdgpu_kmod` (NO TAG)
- **Tag format**: `<distro>-<release>-<kernel>-<driver_version>`
  - Example: `coreos-416.94-5.14.0-427.28.1.el9_4.x86_64-6.2.2`
  - Example: `ubuntu-22.04-5.15.0-94-generic-6.1.3`
- **Build methods**:
  - From installer packages (radeon repo)
  - From source images (OpenShift, when useSourceImage=true)

### Driver Versions
- ROCm-based versioning (e.g., "6.2.2", "6.3.0")
- New versioning schema from ROCm 7.1+ (e.g., "30.20.1")

### Upgrade Process
1. **WorkerMgr** tracks per-node state in `status.nodeModuleStatus`
2. States: NotStarted → Started → InstallInProgress → InstallComplete → InProgress → Complete (or Failed)
3. Coordinated by `internal/controllers/upgrademgr.go`
4. Policies:
   - MaxParallelUpgrades: limits concurrent node upgrades
   - MaxUnavailableNodes: halts upgrades if too many failures
   - NodeDrainPolicy: evicts pods before upgrade
   - RebootRequired: triggers node reboot post-upgrade

### Secure Boot Support
- Image signing via private/public key pair
- Keys stored in Kubernetes Secrets
- Referenced via `imageSign.keySecret` and `imageSign.certSecret`

### Kernel Module Configuration
- **LoadArgs**: Arguments for `modprobe <args> module_name`
- **UnloadArgs**: Arguments for `modprobe -r <args> module_name`
- **Parameters**: Module parameters `modprobe module_name <parameters>`

---

## 2. Device Plugin

### Purpose
Registers AMD GPUs as Kubernetes allocatable resources using Device Plugin API.

### Implementation
- **Handler**: `internal/plugin/`
- **DaemonSet**: One pod per GPU node
- **Communication**: Unix socket to kubelet (`/var/lib/kubelet/device-plugins`)
- **Image**: `rocm/k8s-device-plugin:latest`

### Resource Naming Strategies

#### Single (Homogeneous Nodes)
- All GPUs reported under `amd.com/gpu`
- Example: 8 GPUs unpartitioned → `amd.com/gpu: 8`
- Example: 8 GPUs CPX-NPS4 → `amd.com/gpu: 64`
- **Limitation**: Does NOT support heterogeneous partitioning

#### Mixed (Homogeneous and Heterogeneous)
- GPUs reported by partition type
- Example homogeneous: 8 GPUs CPX-NPS4 → `amd.com/cpx_nps4: 64`
- Example heterogeneous:
  - 5 GPUs SPX-NPS1 → `amd.com/spx_nps1: 5`
  - 3 GPUs CPX-NPS1 → `amd.com/cpx_nps1: 24`

**Partition naming**: `<compute_partition>_<memory_partition>`
- Compute: SPX, DPX, QPX, CPX
- Memory: NPS1, NPS2, NPS4

### Pod Spec Example
```yaml
resources:
  limits:
    amd.com/gpu: 1           # Single strategy or non-partitioned
    # OR
    amd.com/cpx_nps4: 4      # Mixed strategy with partitioned GPUs
```

### Node Labeller Component
Runs alongside device plugin to add detailed GPU properties.

**Default labels**:
- `amd.com/gpu.vram.<index>`: VRAM size
- `amd.com/gpu.cu-count.<index>`: Compute units
- `amd.com/gpu.simd-count.<index>`: SIMD count
- `amd.com/gpu.device-id.<index>`: PCI device ID
- `amd.com/gpu.family.<index>`: GPU family
- `amd.com/gpu.product-name.<index>`: Product name
- `amd.com/gpu.driver-version`: Driver version

**Optional labels** (via NodeLabellerArguments):
- `compute-memory-partition`
- `compute-partitioning-supported`
- `memory-partitioning-supported`

---

## 3. DRA (Dynamic Resource Allocation) Driver

### Purpose
Modern alternative to Device Plugin using Kubernetes DRA API for more flexible GPU allocation.

### Implementation
- **Handler**: Creates DaemonSet for DRA driver
- **DaemonSet**: One pod per GPU node
- **Image**: `rocm/k8s-gpu-dra-driver:latest`
- **Requirements**: Kubernetes 1.32+, DynamicResourceAllocation feature gate, CDI enabled in runtime

### Key Capabilities
1. **ResourceSlices**: Published per GPU device
2. **Scheduler-driven allocation**: Kube-scheduler handles GPU assignment
3. **Fine-grained selection**: CEL expressions in DeviceClass
4. **GPU sharing**: Multiple containers in same pod can share GPUs
5. **CDI integration**: Uses Container Device Interface for device injection

### DeviceClass
Created by operator (or user-managed):
```yaml
apiVersion: resource.k8s.io/v1beta1
kind: DeviceClass
metadata:
  name: gpu.amd.com
spec:
  selectors:
    - cel:
        expression: "device.driver == 'gpu.amd.com'"
```

### Workload Example
```yaml
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaimTemplate
metadata:
  name: single-gpu
spec:
  spec:
    devices:
      requests:
      - name: gpu
        deviceClassName: gpu.amd.com
---
apiVersion: v1
kind: Pod
spec:
  resourceClaims:
  - name: gpu-claim
    resourceClaimTemplateName: single-gpu
  containers:
  - name: workload
    image: rocm/pytorch:latest
```

### Mutual Exclusion with Device Plugin
- **Enforced by operator**: Validation rejects DeviceConfig with both enabled
- **Reason**: Conflicts in resource advertising and allocation
- **Migration**: Disable one before enabling the other

---

## 4. Device Config Manager (DCM)

### Purpose
Configure GPU partitioning on AMD Instinct GPUs.

### Implementation
- **Handler**: `internal/configmanager/`
- **DaemonSet**: One pod per GPU node
- **Image**: `rocm/device-config-manager:v1.5.0`
- **Configuration**: ConfigMap with partition profiles (config.json)

### Partition Types

**Memory Partitioning**:
- NPS1: 1 NUMA partition (all memory in one domain)
- NPS2: 2 NUMA partitions
- NPS4: 4 NUMA partitions

**Compute Partitioning**:
- SPX: Single Partition (1 partition)
- DPX: Dual Partition (2 partitions)
- QPX: Quad Partition (4 partitions)
- CPX: Compute Partition (8 partitions on MI300)

### ConfigMap Structure
Referenced via `spec.configManager.config.name`:
- **Default**: "default-dcm-config" (created by operator if missing)
- **Custom**: User-provided ConfigMap with partition profiles

Example config.json:
```json
{
  "partitions": [
    {
      "nodeSelector": {"kubernetes.io/hostname": "gpu-worker-1"},
      "compute": "CPX",
      "memory": "NPS4"
    }
  ]
}
```

### Systemd Integration
DCM can start/stop systemd services for partition application.

### Workflow
1. DCM reads ConfigMap
2. Applies partition configuration via ROCm SMI
3. May require node reboot for changes to take effect
4. Device Plugin updates resources after partitioning

---

## 5. Metrics Exporter

### Purpose
Export GPU telemetry metrics in Prometheus format.

### Implementation
- **Handler**: `internal/metricsexporter/`
- **DaemonSet**: One pod per GPU node
- **Image**: `rocm/device-metrics-exporter:v1.5.0`
- **Port**: Default 5000 (configurable)

### Metrics Categories
- **Temperature**: GPU temperature sensors
- **Utilization**: GPU usage percentage
- **Memory**: VRAM usage, available memory
- **Power**: Power consumption in watts
- **PCIe**: Bandwidth utilization
- **Performance**: Clocks, throttling

### Service Types
1. **ClusterIP** (default): In-cluster access only
2. **NodePort**: External access on port 30000-32767

### Prometheus Integration

#### ServiceMonitor (Prometheus Operator)
```yaml
spec:
  metricsExporter:
    prometheus:
      serviceMonitor:
        enable: true
        interval: "30s"
        honorLabels: true
        labels:
          release: prometheus
```

#### Manual Prometheus Config
Scrape endpoint: `http://<node-ip>:<nodePort>/metrics` or `http://<service>:<port>/metrics`

### kube-rbac-proxy Sidecar
Optional RBAC/mTLS protection:
- Runs alongside metrics exporter
- Enforces RBAC on metrics endpoint
- Optional mTLS with client certificate validation
- Self-signed certs generated by default

### Custom Metrics
Via ConfigMap (spec.metricsExporter.config.name):
- Define which metrics to collect
- Add custom labels
- Filter metrics

---

## 6. Test Runner

### Purpose
Hardware validation, diagnostics, and benchmarking for GPU nodes.

### Implementation
- **Handler**: `internal/testrunner/`
- **DaemonSet**: One pod per GPU node
- **Image**: `docker.io/rocm/test-runner:v1.4.1`

### Test Frameworks

#### RVS (ROCm Validation Suite)
- **Publicly available**
- Tests: GPU stress, PCIe bandwidth, memory, burn-in
- Included in public test runner image

#### AGFHC (AMD GPU Field Health Check)
- **Requires authorization** from AMD
- Advanced diagnostics and field tests
- Full test runner image (with AGFHC) available on request

### Execution Modes

#### 1. Automatic (Unhealthy GPUs)
- Triggered by auto-remediation workflows
- Runs when GPU issues detected
- Part of validation step in remediation

#### 2. Manual/Scheduled
- Configured via ConfigMap
- Run on-demand or scheduled
- Results reported as Kubernetes Events

#### 3. Pre-start Jobs (Init Containers)
- Embedded in workload pods as init containers
- Validates GPU health before workload starts
- Prevents long-running job failures

### Configuration
ConfigMap (spec.testRunner.config):
- Test profiles (which tests to run)
- Test parameters (iterations, timeout, etc.)
- Framework selection (RVS or AGFHC)

### Logs Export
- **HostPath**: `/var/log/amd-test-runner` (default)
- **Cloud export**: Via secrets for S3, Azure Blob, etc.
- Persistent across pod restarts

---

## 7. Auto-Remediation System

### Purpose
Automatically remediate GPU node issues using Argo Workflows.

### Implementation
- **Handler**: `internal/controllers/remediation_handler.go` (~75KB)
- **Engine**: Argo Workflows v4.0.3
- **Trigger**: Node conditions from Node Problem Detector (NPD)

### Components

#### Node Problem Detector (NPD)
- **External dependency**: Must be installed separately
- **Purpose**: Monitors nodes for GPU issues
- **Output**: Sets node conditions (e.g., "AMDGPUXgmi", "AMDGPUMemory")
- **Integration**: Queries metrics from Device Metrics Exporter

#### Remediation Handler
- Watches for node conditions matching ConfigMap
- Creates Argo Workflow for each matching condition
- Applies taints/labels to affected nodes
- Tracks workflow lifecycle
- Removes taints/labels on success

### Workflow ConfigMap
**Default**: `default-conditional-workflow-mappings` (operator-created)

Structure:
```yaml
- nodeCondition: AMDGPUXgmi             # AFID error code
  workflowTemplate: default-template     # Argo template to run
  validationTestsProfile:                # Post-remediation tests
    framework: AGFHC
    recipe: all_lvl4
    iterations: 1
    stopOnFailure: true
    timeoutSeconds: 4800
  physicalActionNeeded: true             # Manual intervention required?
  notifyRemediationMessage: "..."        # Instructions for admin
  notifyTestFailureMessage: "..."        # Escalation instructions
  recoveryPolicy:                        # Retry limits
    maxAllowedRunsPerWindow: 3
    windowSize: 15m
  skipRebootStep: false                  # Skip reboot in workflow?
```

### Default Workflow Template Steps

1. **Label Node**: Apply custom labels (from spec.remediationWorkflow.nodeRemediationLabels)
2. **Taint Node**: `amd-gpu-unhealthy:<condition>:NoSchedule`
3. **Drain Workloads**: Evict GPU pods (respects NodeDrainPolicy)
4. **Notify Admin**: Kubernetes Event if physicalActionNeeded=true
5. **Suspend Workflow**: Pause for manual intervention (if needed)
6. **Reboot Node**: Reinitialize GPU hardware (unless skipRebootStep=true)
7. **Validate GPUs**: Run test suite (framework/recipe from ConfigMap)
8. **Verify Condition**: Check node condition resolved (status=False)
9. **Remove Taint**: Restore node to schedulable state
10. **Remove Labels**: Clean up remediation labels

### Workflow Control

#### Suspension Triggers
- **Physical action required**: physicalActionNeeded=true
- **Retry limit exceeded**: RecoveryPolicy threshold hit

#### Resumption
Apply label: `operator.amd.com/gpu-force-resume-workflow=true`

#### Abort
Apply label: `operator.amd.com/gpu-abort-workflow=true`

### Parallelism Control
- **MaxParallelWorkflows**: Limits concurrent workflows
- Excess workflows queued in Pending state
- Prevents cluster-wide disruption

### TTL for Failed Workflows
- Default: 24 hours
- Configurable via `ttlForFailedWorkflows` (e.g., "5h", "30m")
- Allows post-mortem analysis

---

## Component Interactions

### Boot-up Sequence
1. NFD detects GPUs → labels nodes
2. Operator reconciles DeviceConfig → creates KMM Module
3. KMM loads driver → GPUs available to OS
4. Device Plugin/DRA Driver discovers GPUs → registers resources
5. Node Labeller adds detailed labels
6. Metrics Exporter starts collecting telemetry
7. DCM applies partitioning (if configured)
8. Test Runner ready for validation

### Remediation Flow
1. Metrics Exporter reports error metrics
2. NPD queries metrics → sets node condition
3. Remediation Handler observes condition → creates Workflow
4. Workflow executes steps → remediates or suspends
5. Admin intervenes (if needed) → resumes workflow
6. Validation tests pass → condition cleared
7. Taint removed → node schedulable again

### Upgrade Flow
1. User updates driver version in DeviceConfig
2. WorkerMgr identifies nodes needing upgrade
3. For each node (respecting MaxParallelUpgrades):
   - Cordon node
   - Drain pods (or delete GPU pods)
   - KMM upgrades Module
   - Reboot node (if RebootRequired=true)
   - Uncordon node
   - Mark upgrade complete
4. Move to next node
