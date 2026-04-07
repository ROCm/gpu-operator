# AMD GPU Operator Features Support Matrix

This document provides a comprehensive overview of all AMD GPU Operator features and their support status across different AMD GPU product lines.

## Feature Support Matrix

| Feature Name | Feature Description | Instinct GPUs | MI350P | Radeon |
|--------------|---------------------|---------------|---------|--------|
| **Core Components** |
| Device Plugin | Kubernetes device plugin for GPU resource allocation and scheduling | ✓ | | ✓ |
| Node Labeller | Automatic node labeling with GPU capabilities and properties | ✓ | | ✓ |
| Metrics Exporter | Prometheus-compatible GPU metrics collection and export | ✓ | | ✓ |
| Controller Manager | Central control component managing operator lifecycle and custom resources | ✓ | | ✓ |
| **Driver Management** |
| Inbox Driver Support | Use pre-installed or OS-provided AMD GPU drivers | ✓ | | ✓ |
| Out-of-tree Driver Install | Automated installation of specific ROCm version drivers | ✓ | | ✓ |
| Driver Blacklisting | Blacklisting of inbox drivers (Vanilla: manual ramfs update & reboot; OpenShift: automated via Machine Config Operator) | ✓ | | ✓ |
| Driver Upgrades | In-place driver version upgrades across worker nodes | ✓ | | ✓ |
| Precompiled Driver Images | Support for pre-built driver container images | ✓ | | ✓ |
| **GPU Partitioning** |
| Device Config Manager (DCM) | GPU partitioning configuration and management (Radeon: Not supported on Navi3x, Navi4x, Navi5x) | ✓ | | ✗ |
| Memory Partitions (NPS) | NPS1, NPS2, NPS4 memory partitioning modes (MI300X); NPS1, NPS2, NPS4, NPS8 (MI350P) | ✓ | | ✗ |
| Compute Partitions | SPX, DPX, QPX, CPX compute partitioning modes (MI300X); SPX, DPX, TPX, QPX, CPX (MI350P) | ✓ | | ✗ |
| Systemd Integration | Systemd service file integration for partition management | ✓ | | ✗ |
| **Resource Allocation** |
| Single Resource Naming | All GPUs/partitions reported as `amd.com/gpu` | ✓ | | ✓ |
| Mixed Resource Naming | GPUs reported by partition type (e.g., `amd.com/cpx_nps4`) | ✓ | | ✗ |
| Homogeneous Node Support | Support for nodes with uniform GPU configurations | ✓ | | ✓ |
| DRA (Dynamic Resource Allocation) | Kubernetes Dynamic Resource Allocation API support | ✓ | ✓ | ✓ |
| **Virtualization Management through K8s** |
| KubeVirt Integration | GPU passthrough for virtual machines managed by KubeVirt | ✓** | | ✗ |
| VF Passthrough (SR-IOV) | Virtual Function passthrough using AMD MxGPU GIM driver (Radeon: Navi48/Navi31 do not support SR-IOV) | ✓** | | ✗ |
| PF Passthrough | Physical Function passthrough for exclusive GPU access (Radeon: PF passthrough on KVM/bare-metal only, not K8s) | ✓** | | ✗ |
| GIM Driver Management | Automated installation of AMD MxGPU GIM driver | ✓** | | ✗ |
| VFIO Binding | Automatic binding of PF/VF devices to vfio-pci kernel module | ✓** | | ✗ |
| **Health & Monitoring** |
| Node Problem Detector (NPD) | Integration with NPD for GPU health monitoring (to be GAd in next GPU Operator release) | ✓ | ✗ | ✗ |
| GPU Health Monitoring | Detection and reporting of GPU errors and health issues | ✓ | | ✓ |
| **Auto-Remediation** |
| Automated Node Remediation (ANR) | Workflow-based automatic recovery for unhealthy GPU nodes (to be GAd in next GPU Operator release) | ✓ | ✗ | ✗ |
| Argo Workflows Integration | Remediation powered by Argo Workflows engine | ✓ | ✗ | ✗ |
| Conditional Workflows | Error-specific remediation workflows based on AFID codes | ✓ | ✗ | ✗ |
| Node Drain Policy | Configurable pod eviction during remediation | ✓ | ✗ | ✗ |
| Recovery Policies | Retry limits and time windows for remediation attempts | ✓ | ✗ | ✗ |
| Physical Action Notifications | Alerts for issues requiring manual hardware intervention | ✓ | ✗ | ✗ |
| GPU Validation Testing | Post-remediation GPU health validation | ✓ | ✗ | ✗ |
| **Test Runner** |
| Manual Test Execution | Scheduled or on-demand GPU validation tests | ✓ | | ✓ |
| Pre-Start Job Testing | Init container tests for GPU workload pods | ✓ | | ✓ |
| RVS Test Framework | ROCm Validation Suite test execution | ✓ | | ✓ |
| AGFHC Test Framework | AMD GPU Field Health Check toolkit (requires authorization) | ✓ | | ✗ |
| Test Results as Events | Kubernetes events reporting test outcomes | ✓ | | ✓ |
| **Network & Deployment** |
| Air-Gapped Installation | Deployment in environments without external network access | ✓ | | ✓ |
| **Platform Support** |
| Vanilla Kubernetes | Support for standard Kubernetes distributions | ✓ | | ✓ |
| Red Hat OpenShift | Full support for OpenShift Container Platform | ✓ | | ✓* |
| **Operating System Support** |
| Ubuntu 22.04 | Support for Ubuntu 22.04 LTS | ✓ | | ✓ |
| Ubuntu 24.04 | Support for Ubuntu 24.04 LTS (Radeon Priority #1) | ✓ | | ✓ |
| Debian | Support for Debian-based distributions | ✓ | | ✗ |
| **Metrics & Monitoring** |
| Kube RBAC Proxy | Secure access control for metrics endpoints using Kubernetes RBAC | ✓ | | ✓ |

## Legend

- **✓** - Supported and production-ready
- **✓\*** - Supported with specific requirements or in beta
- **✓\*\*** - Alpha feature
- **✗** - Not supported
- Empty cell - Support status to be determined

## Important Notes

### MI350P and Radeon GPU Support

**Prerequisites and Assumptions:**
This feature matrix assumes that the lower-level tools (RVS, AGFHC, AMD-SMI) have already been validated for MI350P and Radeon GPUs prior to GPU Operator support.

**Unified ROCm Stack:**
The AMD GPU Operator follows a unified release strategy with a single ROCm stack supporting all GPU platforms (Instinct, MI350P, and Radeon). There will be **one GPU Operator release** supporting all platforms, not separate releases for different GPU families. This ensures consistency and simplifies deployment across heterogeneous GPU environments.

### MI350P and Radeon Limitations

- **OpenShift Support (Radeon):** Will be released in future GPU Operator releases when needed. Priority #1 is Vanilla Kubernetes on Ubuntu 24.04.
- **NPD & Auto-Remediation:** Dependent on Service Action Guide support for MI350P and Radeon. Will not be included in the first MI350P or Radeon GPU Operator release.
