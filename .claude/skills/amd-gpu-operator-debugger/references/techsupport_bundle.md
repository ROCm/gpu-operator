# Techsupport Bundle Collection

When the root cause is not clear after manual triage, collect a full support bundle using the operator's built-in techsupport script.

## Collection Commands

```bash
# Clone the operator repo (or use existing checkout)
git clone https://github.com/ROCm/gpu-operator.git
cd gpu-operator

# Collect for a specific node
./tools/techsupport_dump.sh -o yaml -k $KUBECONFIG <node-name>

# Collect for all nodes
./tools/techsupport_dump.sh -w -o yaml -k $KUBECONFIG all
```

## What Gets Collected

The techsupport bundle includes:

- All operator pod logs (controller, KMM, NFD)
- All operand DaemonSet pod logs (device plugin, metrics exporter, config manager, test runner)
- KMM Module CR status
- DeviceConfig CR status and spec
- Node labels and taints
- Node allocatable resources
- DaemonSet status (DESIRED/READY counts)
- Builder pod logs (if KMM driver build was attempted)
- Driver DaemonSet pod logs (if driver was loaded)
- Kernel module status (`lsmod | grep amdgpu`)
- GPU device files (`/dev/kfd`, `/dev/dri/`)
- dmesg output (kernel logs)
- PCI device information (`lspci -vvv`)

## Submitting the Bundle

Attach the resulting tarball when filing a bug report at:

<https://github.com/ROCm/gpu-operator/issues>

Include a brief description of:

1. The symptom you're seeing (e.g., "No GPU resources allocatable")
2. What you've already tried
3. The environment (Kubernetes version, OS, GPU model)
