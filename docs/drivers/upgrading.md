# Driver Upgrade Guide

This guide walks through the process of upgrading AMD GPU drivers on worker nodes.

## Overview

The upgrade process involves:

1. Verifying current installation
2. Updating the driver version
3. Managing workloads
4. Updating node labels
5. Performing the upgrade

## Step-by-Step Upgrade Process

### 1. Check Current Driver Version

Verify the existing driver version label on your worker nodes:

```bash
kubectl get node <worker-node> -o yaml
```

Look for the label in this format:

```text
kmm.node.kubernetes.io/version-module.<deviceconfig-namespace>.<deviceconfig-name>=<version>
```

Example:

```text
kmm.node.kubernetes.io/version-module.kube-amd-gpu.test-device-config=6.1.3
```

### 2. Update DeviceConfig

Update the `driversVersion` field in your DeviceConfig:

```bash
kubectl edit deviceconfigs <config-name> -n kube-amd-gpu
```

The operator will automatically:

1. Look for the new driver image in the registry
2. Build the image if it doesn't exist
3. Push the built image to your specified registry

#### Image Tag Format

The operator uses specific tag formats based on the OS:

| OS | Tag Format | Example |
|----|------------|---------|
| Ubuntu | `ubuntu-<version>-<kernel>-<driver>` | `ubuntu-22.04-6.8.0-40-generic-6.1.3` |
| RHEL CoreOS | `coreos-<version>-<kernel>-<driver>` | `coreos-416.94-5.14.0-427.28.1.el9_4.x86_64-6.2.2` |

> **Warning**: If a node's ready status changes during upgrade (Ready → NotReady → Ready) before its driver version label is updated, the old driver won't be reinstalled. Complete the upgrade steps for these nodes to install the new driver.

### 3. Stop Workloads

Stop all workloads using the AMD GPU driver on the target node before proceeding.

### 4. Update Node Labels

You have two options for updating node labels:

#### Option A: Direct Update (Recommended)

If no additional maintenance is needed, directly update the version label:

```bash
# Old label format:
kmm.node.kubernetes.io/version-module.<namespace>.<config-name>=<old-version>
# New label format:
kmm.node.kubernetes.io/version-module.<namespace>.<config-name>=<new-version>
```

#### Option B: Remove and Add (If maintenance is needed)

- Remove old version label:

```bash
kubectl label node <worker-node> \
  kmm.node.kubernetes.io/version-module.<namespace>.<config-name>-
```

- Perform required maintenance

- Add new version label:

```bash
kubectl label node <worker-node> \
 kmm.node.kubernetes.io/version-module.<namespace>.<config-name>=<new-version>
```

### 5. Restart Workloads

After the new driver is installed successfully, restart your GPU workloads on the upgraded node.

## Verification

To verify the upgrade, check node labels:

```bash
kubectl get node <worker-node> --show-labels | grep kmm.node.kubernetes.io
```

- Verify driver version:

```bash
kubectl get deviceconfigs <config-name> -n kube-amd-gpu -o yaml
```

- Check driver status:

```bash
kubectl get deviceconfigs <config-name> -n kube-amd-gpu -o jsonpath='{.status}'
```
