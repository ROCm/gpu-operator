# Driver Upgrade Guide

This guide walks through the process of upgrading AMD GPU drivers on worker nodes.

## Overview

The driver can be upgraded in the following methods.

1. Automatic Upgrade Process
2. Manual Upgrade Process

## 1. Automatic Upgrade Process

Automatic upgrade is enabled if the deviceconfig has field `upgradePolicy` enabled.
If this field is not configured, then user has to follow the manual steps as shown in next section
If this field is configured and the `version` field is changed in driver spec, automatic driver upgrade progress is initiated.

The following operations are sequentially executed by the gpu operator for each selected node

1. The node is cordoned so that no pods can be scheduled on this node
2. The existing pods (that require amd gpus) are drained/deleted based on the config in the upgrade policy.
3. The desired driver version label is updated as shown below.

   ```bash
   kmm.node.kubernetes.io/version-module.<namespace>.<config-name>=<new-version>
   ```

4. KMM operator unloads the old driver version and loads the new driver version.
5. If the node requires reboot post installation (configurable in upgradePolicy), the node is rebooted
6. Once the node is rebooted and the desired driver is loaded, the node is uncordoned and available for scheduling.

The following are the steps to perform the automatic driver upgrade

1. Set the desired driver version and configure upgrade policy
2. Track the upgrade status through CR status

### 1. Set desired driver version and configure upgrade policy

The following sample config shows the relevant fields to start automatic driver upgrade across the nodes in the cluster with default upgrade configuration.

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
  # use the namespace where AMD GPU Operator is running
  namespace: kube-amd-gpu
spec:
  driver:
    version: 6.3.2
    enable: true
    upgradePolicy:
      enable: true
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

Upgrade configuration reference

To check the full spec of upgrade configuration run kubectl get crds deviceconfigs.amd.com -oyaml

#### `driver.upgradePolicy` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `enable` | Enable this upgrade policy | `false` |
| `maxParallelUpgrades` | Maximum number of nodes which will be upgraded in parallel | `1` |
| `maxUnavailableNodes` | Maximum number (or Percentage) of nodes which can be unavailable (cordoned) in the cluster | `25%` |
| `rebootRequired` | Reboot the node after driver upgrade is done. Waits for 60 mins post reboot before declaring as failed | `false` |

**Warning**: When using ROCm drivers version 6.3 and below, a known issue may prevent the driver upgrade from fully completing unless the node is rebooted. As a workaround, we strongly recommend setting the `rebootRequired` field to `true` in your upgrade policy. This ensures that a reboot is triggered after the driver upgrade, allowing the new driver to be fully loaded. This workaround should be applied until a permanent fix is provided in a future release.

#### `driver.upgradePolicy.nodeDrainPolicy` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `force` | Allow drain to proceed on the node even if there are managed pods such as daemon-sets. In such cases drain will not proceed unless this option is set to true | `true` |
| `timeout` | The length of time to wait before giving up. Zero means infinite | `300s` |

#### `driver.upgradePolicy.podDeletionPolicy` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `force` | Force delete all pods that use amd gpus | `true` |
| `timeout` | The length of time to wait before giving up. Zero means infinite | `300s` |

### 2. Track the upgrade status through CR status

The `status.nodeModuleStatus.<worker-node>.status` captures the status of the upgrade process for each node

```yaml
status:
  nodeModuleStatus:
    worker-10-11-71-66:
      containerImage: registry.test.io:5000/driver-image:ubuntu-22.04-5.15.0-124-generic-6.3.2
      kernelVersion: 5.15.0-124-generic
      lastTransitionTime: 2024-12-05 05:35:04 +0000 UTC
      status: Upgrade-Complete
    worker-10-11-71-67:
      containerImage: registry.test.io:1234/driver-image:ubuntu-22.04-5.15.0-124-generic-6.3.2
      kernelVersion: 5.15.0-124-generic
      lastTransitionTime: 2024-12-05 05:35:04 +0000 UTC
      status: Upgrade-Complete
    worker-10-11-71-69:
      containerImage: registry.test.io:1234/driver-image:ubuntu-22.04-5.15.0-124-generic-6.3.2
      kernelVersion: 5.15.0-124-generic
      lastTransitionTime: 2024-12-05 05:34:53 +0000 UTC
      status: Upgrade-Complete
    worker-10-11-77-194:
      containerImage: registry.test.io:1234/driver-image:ubuntu-22.04-5.15.0-119-generic-6.3.2
      kernelVersion: 5.15.0-119-generic
      lastTransitionTime: 2024-12-05 05:37:14 +0000 UTC
      status: Upgrade-Complete
```

The following are the different node states during the upgrade process

| State | Description |
|-----------|---------|
| `Install-In-Progress` | Driver is being installed on the node for the first time |
| `Install-Complete` | Driver install is complete |
| `Upgrade-Not-Started` | Automatic upgrade enabled and driver version change is detected. All nodes move to this state |
| `Upgrade-In-Progress` | Selected nodes conforming to upgrade policy will be attempted for driver upgrade |
| `Upgrade-Complete` | Driver upgrade is successfully complete on the node |
| `Cordon-Failed` | Cordoning of the node failed |
| `Uncordon-Failed` | Uncordoning of the node failed |
| `Drain-Failed` | Drain node or Delete pods operation failed|
| `Reboot-In-Progress` | Driver upgrade is done and reboot is in progress |
| `Reboot-Failed` | Driver upgrade is done and reboot attempt failed |
| `Upgrade-Failed` | Driver upgrade failed for any other reasons |

The following are considered during the automatic upgrade process

1. Selection of a node should satisfy both `maxUnavailableNodes` and `maxParallelUpgrades` criteria
2. All nodes in failed state is considered while calculating `maxUnavailableNodes`
3. When a driver upgrade on a node fails, the node will be in cordoned state. User has to fix the issue and uncordon the node manually. Such nodes will be automatically picked up for automatic driver upgrade operation.

## 2. Manual Upgrade Process

The manual upgrade process involves the following steps:

1. Verifying current installation
2. Updating the driver version
3. Managing workloads
4. Updating node labels
5. Performing the upgrade

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
