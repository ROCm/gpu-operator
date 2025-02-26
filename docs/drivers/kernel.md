# Kernel Upgrade Guide

The AMD GPU Operator supports kernel upgrades on cluster nodes running AMD GPUs. This guide provides detailed instructions for safely upgrading the kernel while maintaining GPU functionality.

## Prerequisites

- Operational Kubernetes cluster with AMD GPU Operator installed
- Administrative access to cluster nodes
- Access to perform node draining operations
- Access to perform kernel upgrades on nodes

## Pre-upgrade Validation

### PreFlight Validation (Optional)

Before performing the kernel upgrade, you can validate whether the AMD GPU driver module will build successfully on the new kernel version using the Kernel Module Management (KMM) Operator's PreFlight Validation feature.

For validation steps:

- Kubernetes users: See [KMM Pre-flight Validation Documentation](https://kmm.sigs.k8s.io/documentation/preflight_validation/)

## Upgrade Process

### 1. Prepare the Node

Before upgrading the kernel, drain the target node to ensure no GPU workloads are running:

```bash
kubectl drain <node-name> --ignore-daemonsets
```

This command:

- Evicts all pods from the node
- Marks the node as unschedulable
- Ensures workloads are rescheduled to other available nodes

### 2. Upgrade the Kernel

After draining the node, proceed with the kernel upgrade process specific to your Linux distribution:

For Ubuntu:

```bash
sudo apt update
sudo apt upgrade linux-image-generic
```

> **Note:** The exact upgrade commands may vary depending on your Linux distribution and configuration.

### 3. Reboot the Node

After the kernel upgrade completes, reboot the node to load the new kernel:

```bash
sudo reboot
```

### 4. Re-enable Node Scheduling

Once the node is back online with the new kernel, make it schedulable again:

```bash
kubectl uncordon <node-name>
```

The AMD GPU Operator will automatically:

1. Detect the new kernel version
2. Build and install the appropriate GPU driver
3. Restore GPU functionality on the node

## Verification

Verify the successful upgrade and GPU functionality:

- Check the node's kernel version:

```bash
kubectl debug node/<node-name> -it --image=ubuntu -- uname -r
```

- Verify GPU driver status:

```bash
kubectl get pods -n kube-amd-gpu
```

- Check GPU device availability:

```bash
kubectl get deviceconfigs.amd.com -n kube-amd-gpu
```

## Troubleshooting

If issues occur during or after the kernel upgrade:

- Check operator logs:

```bash
kubectl logs -n kube-amd-gpu <operator-pod-name>
```

- Verify driver build logs:

```bash
kubectl logs -n kube-amd-gpu <kmm-module-loader-pod-name>
```

- Common issues:

- Missing kernel headers: Install appropriate kernel-devel/headers package
- Secure Boot conflicts: Ensure proper signing keys are configured
- Build failures: Check for compiler or dependency issues in the logs
