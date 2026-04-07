---
name: GPU Operator Architecture Overview
description: High-level architecture and component interaction in AMD GPU Operator
type: reference
---

# AMD GPU Operator Architecture Overview

## Project Overview
The AMD GPU Operator is a Kubernetes operator that simplifies deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters. It automates driver installation, GPU resource allocation, health monitoring, and remediation.

**Repository**: github.com/ROCm/gpu-operator  
**Domain**: sigs.x-k8s.io  
**Custom Resource**: DeviceConfig (amd.com/v1alpha1)  
**Primary CRD**: DeviceConfig - describes how to enable and configure AMD GPU devices

## Core Architecture

### Controller Pattern
- Built using kubebuilder v3 and operator-framework SDK
- Main reconciler: `DeviceConfigReconciler` in internal/controllers/
- Uses controller-runtime for Kubernetes API interaction
- Watches DeviceConfig custom resources
- Orchestrates dependent components (KMM, device plugin, DRA driver, etc.)

### Key Dependencies
1. **Node Feature Discovery (NFD)** - Auto-detects AMD GPUs and labels nodes
   - Sets label: `feature.node.kubernetes.io/amd-gpu: "true"`
   - Different for OpenShift vs vanilla K8s

2. **Kernel Module Management (KMM)** - Manages GPU driver kernel modules
   - Handles loading/unloading of amdgpu kernel module
   - Supports driver upgrades
   - AMD-optimized KMM for K8s, Red Hat KMM for OpenShift

3. **Argo Workflows** - Powers auto-remediation workflows
   - Version 4.0.3
   - Installed by GPU Operator (customized without server component)

## Component Hierarchy

```
GPU Operator Controller Manager (main entry: cmd/main.go)
├── DeviceConfigReconciler (reconciles DeviceConfig CR)
│   ├── KMMHandler (driver installation)
│   ├── DevicePlugin/DRADriver (resource allocation - mutually exclusive)
│   ├── NodeLabeller (detailed GPU labels)
│   ├── MetricsExporter (Prometheus metrics)
│   ├── ConfigManager (GPU partitioning)
│   ├── TestRunner (GPU validation/testing)
│   └── RemediationHandler (auto-remediation workflows)
└── WorkerMgr (manages upgrade state per node)
```

## Main Components

### 1. Driver Management (KMM Integration)
- **Purpose**: Install/manage amdgpu kernel module
- **Types**: container (default), vf-passthrough, pf-passthrough
- **Image naming**: `<registry>/<namespace>/amdgpu_kmod:<distro>-<kernel>-<version>`
- **Upgrade support**: Rolling upgrades with configurable parallelism
- **Secure boot**: Image signing support via certificates

### 2. Resource Allocation (Device Plugin OR DRA Driver)

**Device Plugin** (traditional approach):
- Implements Kubernetes Device Plugin API
- Registers `amd.com/gpu` resources
- Resource naming strategies: `single` (homogeneous) or `mixed` (heterogeneous)
- Supports GPU partitioning resource names (e.g., `amd.com/cpx_nps4`)

**DRA Driver** (modern approach, K8s 1.32+):
- Implements Dynamic Resource Allocation API
- Publishes ResourceSlices for scheduler-driven allocation
- Supports fine-grained device selection via CEL expressions
- Enables GPU sharing between containers in same pod
- **Cannot coexist with Device Plugin** - operator enforces mutual exclusion

### 3. Node Labeller
- Adds detailed GPU properties to node labels
- Default labels: vram, cu-count, simd-count, device-id, family, product-name, driver-version
- Optional labels: compute-memory-partition, compute/memory-partitioning-supported

### 4. Device Config Manager (DCM)
- **Purpose**: GPU partitioning configuration
- **Partition types**:
  - Memory: NPS1, NPS2, NPS4
  - Compute: SPX, DPX, QPX, CPX
- ConfigMap-driven configuration
- Systemd integration for service management

### 5. Metrics Exporter
- Prometheus-compatible metrics endpoint
- GPU telemetry: temperature, utilization, memory, power, PCIe bandwidth
- Optional kube-rbac-proxy sidecar for RBAC/mTLS
- ServiceMonitor CRD support for Prometheus Operator

### 6. Test Runner
- Leverages ROCm Validation Suite (RVS)
- Optional AGFHC (AMD GPU Field Health Check) - requires authorization
- Execution modes:
  - Automatic: triggered on unhealthy GPUs
  - Manual/Scheduled: via ConfigMap
  - Pre-start: init containers in workload pods

### 7. Auto-Remediation System
- **Trigger**: Node conditions from Node Problem Detector (NPD)
- **Workflow engine**: Argo Workflows
- **ConfigMap**: Maps error codes (AFID) to remediation workflows
- **Default template steps**:
  1. Label node
  2. Taint node (amd-gpu-unhealthy:NoSchedule)
  3. Drain GPU workloads
  4. Notify if physical action needed
  5. Suspend for manual intervention (optional)
  6. Reboot node
  7. Run validation tests
  8. Verify condition resolved
  9. Remove taint
  10. Remove labels

## Data Flow

### Driver Installation Flow
```
DeviceConfig created → KMM creates Module CR → Driver image built/pulled → 
KMM DaemonSet loads module → Node ready for GPU workloads
```

### GPU Allocation Flow (Device Plugin)
```
Device Plugin pod starts → Discovers GPUs → Registers with kubelet → 
Resources appear in node.status.allocatable → Pods can request amd.com/gpu
```

### GPU Allocation Flow (DRA)
```
DRA Driver pod starts → Discovers GPUs via amdgpu driver → 
Publishes ResourceSlices → Scheduler allocates via ResourceClaim → 
CDI spec injected into container
```

### Remediation Flow
```
Metrics Exporter reports error → NPD sets node condition → 
GPU Operator observes condition → Creates Argo Workflow → 
Workflow executes remediation steps → Node restored or marked for manual intervention
```

## Configuration Model

All configuration via **DeviceConfig** Custom Resource:
- `spec.selector`: Node selector (which nodes to manage)
- `spec.driver`: Driver installation config
- `spec.devicePlugin`: Device Plugin config
- `spec.draDriver`: DRA driver config
- `spec.configManager`: GPU partitioning config
- `spec.metricsExporter`: Metrics config
- `spec.testRunner`: Test runner config
- `spec.remediationWorkflow`: Auto-remediation config
- `spec.commonConfig`: Shared settings (init containers, secrets)

## Platform Differences

### Vanilla Kubernetes
- Uses AMD-optimized KMM
- NFD from kubernetes-sigs
- Manual Argo Workflows installation

### OpenShift
- Uses Red Hat KMM Operator
- Uses OpenShift NFD Operator
- Image registry: `image-registry.openshift-image-registry.svc:5000`
- Machine Config Operator for driver blacklist
- May have Argo Workflows from OpenShift AI Operator

## Key Design Patterns

1. **Reconciliation loops**: Continuous state convergence
2. **DaemonSets**: All operand components run as DaemonSets
3. **Mutual exclusion**: Device Plugin ⊕ DRA Driver
4. **ConfigMap-driven behavior**: DCM profiles, remediation mappings, test configs
5. **Event-driven remediation**: NPD conditions trigger Argo Workflows
6. **Upgrade coordination**: WorkerMgr tracks per-node upgrade state
7. **Namespace isolation**: All components in GPU Operator namespace
