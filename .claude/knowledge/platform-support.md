# Platform Support - GPU Operator and Device Metrics Exporter

This document defines the supported platforms, GPU models, Kubernetes versions, and OpenShift versions for testing.

**Reference**: https://instinct.docs.amd.com/projects/gpu-operator/en/latest/index.html

---

## Supported GPU Models

The AMD GPU Operator supports **six Instinct GPU generations**:

| GPU Model | Support Status | Generation |
|-----------|----------------|------------|
| MI355X | ✅ Fully Supported | Latest |
| MI350X | ✅ Fully Supported | Latest |
| MI325X | ✅ Fully Supported | MI3xx |
| MI300X | ✅ Fully Supported | MI3xx |
| MI250 | ✅ Fully Supported | MI2xx |
| MI210 | ✅ Fully Supported | MI2xx |

**Note**: MI250X (variant of MI250) is also supported.

---

## Kubernetes Support

**Supported Kubernetes Versions**: 1.29 - 1.35

| Kubernetes Version | Support Status | Notes |
|-------------------|----------------|-------|
| 1.29 | ✅ Supported | |
| 1.30 | ✅ Supported | |
| 1.31 | ✅ Supported | |
| 1.32 | ✅ Supported | |
| 1.33 | ✅ Supported | |
| 1.34 | ✅ Supported | |
| 1.35 | ✅ Supported | Current |

**Note**: Documentation shows K8s 1.29-1.34 but user specifies support extends to 1.35.

---

## OpenShift Support

**Supported OpenShift Versions**: 4.20 and 4.21

| OpenShift Version | Support Status | Notes |
|------------------|----------------|-------|
| 4.16 | ✅ Supported (per docs) | RHCOS |
| 4.17 | ✅ Supported (per docs) | RHCOS |
| 4.18 | ✅ Supported (per docs) | RHCOS |
| 4.19 | ✅ Supported (per docs) | RHCOS |
| 4.20 | ✅ Supported | RHCOS |
| 4.21 | ✅ Supported | Current |

**Note**: Documentation shows OpenShift 4.16-4.20 but user specifies support extends to 4.21.

---

## Operating System Support

### Ubuntu

| OS Version | Kubernetes Support | Notes |
|-----------|-------------------|-------|
| Ubuntu 22.04 LTS | K8s 1.29-1.34 | Fully supported |
| Ubuntu 24.04 LTS | K8s 1.29-1.34 | Fully supported |

### Debian

| OS Version | Kubernetes Support | Notes |
|-----------|-------------------|-------|
| Debian 12 | K8s 1.29-1.34 | Driver management not supported |

### Red Hat

| OS Version | OpenShift Support | Notes |
|-----------|------------------|-------|
| Red Hat Core OS (RHCOS) | OpenShift 4.16-4.20 | Fully supported |

---

## Test Coverage Requirements

### GPU Platform Coverage

Test plans should cover at minimum:

- **MI2xx Family**: MI210, MI250, MI250X
- **MI3xx Family**: MI300X, MI325X
- **Latest Generation**: MI350X, MI355X (when available)

### Kubernetes Version Coverage

Test plans should validate on:

- **Minimum Version**: K8s 1.29
- **Current Version**: K8s 1.35
- **Representative Mid-Version**: K8s 1.31 or 1.32

### OpenShift Version Coverage

Test plans should validate on:

- **Current Version**: OpenShift 4.20
- **Latest Version**: OpenShift 4.21

### Deployment Mode Coverage

Test plans should cover:

- **Baremetal** (Kubernetes on Ubuntu/Debian)
- **SR-IOV/Hypervisor** (if available)
- **OpenShift** (RHCOS)
- **Partitioned GPUs** (SPX, CPX, DPX, QPX modes on MI3xx)

---

## Additional Requirements

### Tools

- **Helm**: v3.2.0+ required
- **CLI Tools**: kubectl or oc (OpenShift CLI)

### Driver Compatibility

For detailed driver version compatibility, refer to the separate ROCm documentation matrix.

---

## Test Plan Platform Matrix

When creating test plans, ensure coverage across these dimensions:

| Dimension | Coverage Required |
|-----------|------------------|
| GPU Models | MI210, MI250X, MI300X, MI325X minimum |
| Kubernetes | 1.29 (min), 1.35 (current), 1.31/1.32 (mid) |
| OpenShift | 4.20, 4.21 |
| OS | Ubuntu 22.04, Ubuntu 24.04, RHCOS |
| Deployment | Baremetal, SR-IOV, OpenShift |
| Partition Modes | SPX, CPX, DPX, QPX (MI3xx only) |

---

## References

- **GPU Operator Documentation**: https://instinct.docs.amd.com/projects/gpu-operator/en/latest/index.html
- **ROCm Compatibility Matrix**: See AMD ROCm documentation
