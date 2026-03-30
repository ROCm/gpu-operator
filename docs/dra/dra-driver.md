# DRA (Dynamic Resource Allocation) Driver

## Overview

The AMD GPU Operator supports [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/), a Kubernetes API for requesting and sharing resources between pods and containers. DRA is an alternative to the traditional [Device Plugin](../device_plugin/device-plugin.md) approach for making AMD GPUs available to workloads.

The DRA driver is built on the [AMD GPU DRA Driver](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/README.md), which implements the Kubernetes DRA interface for AMD Instinct GPUs.

> **Note:** DRA requires Kubernetes 1.32 or later with the `DynamicResourceAllocation` feature gate enabled for Kubernetes 1.32/1.33.
>
> **Important:** The DRA driver and Device Plugin **cannot be enabled at the same time** on the same `DeviceConfig`. The operator validates this and will reject configurations where both are enabled.

For a detailed comparison of DRA vs Device Plugin capabilities, refer to the [AMD GPU DRA Driver documentation](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/README.md).

## Prerequisites

- Kubernetes 1.32+ with the `DynamicResourceAllocation` feature gate enabled
- AMD GPU Operator installed via Helm
- AMD GPU driver (amdgpu) must be installed on the worker nodes — the DRA driver requires the amdgpu kernel module to be loaded in order to discover GPUs and publish `ResourceSlices`
- **CDI (Container Device Interface) must be enabled** in the container runtime. CDI is enabled by default in containerd 2.0+ and CRI-O. If you are running older versions, enable CDI manually — refer to your container runtime's documentation for instructions.

## Enabling the DRA Driver

### Option 1: Enable during Helm install with default CR

You can enable the DRA driver in the default `DeviceConfig` at install time:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --set deviceConfig.spec.devicePlugin.enableDevicePlugin=false \
  --set deviceConfig.spec.draDriver.enable=true
```

By default, the operator uses the `rocm/k8s-gpu-dra-driver:latest` image from [Docker Hub](https://hub.docker.com/r/rocm/k8s-gpu-dra-driver). To specify a custom DRA driver image:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --set deviceConfig.spec.devicePlugin.enableDevicePlugin=false \
  --set deviceConfig.spec.draDriver.enable=true \
  --set deviceConfig.spec.draDriver.image=rocm/k8s-gpu-dra-driver:latest
```

This will create a default `DeviceConfig` with the DRA driver enabled and the device plugin disabled.

### Option 2: Enable via DeviceConfig CR

If the operator is already installed, you can enable the DRA driver by editing the `DeviceConfig`:

```bash
kubectl edit deviceconfigs -n kube-amd-gpu default
```

Set the `draDriver` section:

> **Note:** If no `image` is specified, the operator defaults to `rocm/k8s-gpu-dra-driver:latest`.

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: default
  namespace: kube-amd-gpu
spec:
  draDriver:
    enable: true
    image: rocm/k8s-gpu-dra-driver:latest
  devicePlugin:
    enableDevicePlugin: false
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

> **Note:** If the device plugin is currently enabled, you must disable it before enabling the DRA driver. The operator enforces mutual exclusion between these two components.

### Option 3: Apply a DeviceConfig YAML

Create a file `dra-deviceconfig.yaml`:

> **Note:** The `image` field is optional. If omitted, the operator defaults to `rocm/k8s-gpu-dra-driver:latest`.

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: dra-config
  namespace: kube-amd-gpu
spec:
  draDriver:
    enable: true
    image: rocm/k8s-gpu-dra-driver:latest
    imagePullPolicy: IfNotPresent
  devicePlugin:
    enableDevicePlugin: false
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

```bash
kubectl apply -f dra-deviceconfig.yaml
```

## Verifying the DRA Driver

After enabling, verify the DRA driver daemonset is running:

```bash
kubectl get daemonsets -n kube-amd-gpu
```

You should see a daemonset named `<deviceconfig-name>-dra-driver` (e.g., `default-dra-driver`) with pods running on each GPU node:

```bash
$ kubectl get pods -n kube-amd-gpu -l daemonset-name=default-dra-driver
NAME                           READY   STATUS    RESTARTS   AGE
default-dra-driver-abc12       1/1     Running   0          2m
```

Verify the `DeviceClass` exists:

```bash
$ kubectl get deviceclass gpu.amd.com
NAME          AGE
gpu.amd.com   5m
```

Check that `ResourceSlices` are being published by the driver:

```bash
$ kubectl get resourceslices
NAME                                     DRIVER        NODE          AGE
gpu-worker-1-gpu.amd.com-gpu-0-0qkr2    gpu.amd.com   gpu-worker-1  2m
```

## DeviceClass

The operator's Helm chart creates a `DeviceClass` named `gpu.amd.com` by default. This `DeviceClass` uses the following CEL selector expression:

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

### Disabling automatic DeviceClass creation

If you manage the `DeviceClass` independently (e.g., in a GitOps workflow or when using the standalone DRA driver helm chart), you can disable the operator from creating it:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --set draDriver.deviceClass.create=false
```

Or during upgrade:

```bash
helm upgrade amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --set draDriver.deviceClass.create=false
```

## Requesting GPUs with DRA

With DRA enabled, workloads request GPUs using `ResourceClaim` and `ResourceClaimTemplate` objects instead of `resources.limits`.

For workload examples including single GPU, multi-GPU, and GPU sharing scenarios, see the [DRA driver examples](https://github.com/ROCm/k8s-gpu-dra-driver/tree/main/example) in the upstream repository.

## Migrating from Device Plugin to DRA

To migrate an existing deployment from the traditional device plugin to the DRA driver:

1. **Disable the device plugin** by editing the `DeviceConfig`:

   ```bash
   kubectl patch deviceconfig default -n kube-amd-gpu --type=merge \
     -p '{"spec":{"devicePlugin":{"enableDevicePlugin":false}}}'
   ```

2. **Enable the DRA driver**:

   ```bash
   kubectl patch deviceconfig default -n kube-amd-gpu --type=merge \
     -p '{"spec":{"draDriver":{"enable":true}}}'
   ```

3. **Update workload specifications** to use `ResourceClaim` / `ResourceClaimTemplate` instead of `resources.limits.amd.com/gpu`.

4. **Verify** the DRA driver pods are running and `ResourceSlices` are published.

> **Warning:** Workloads using `amd.com/gpu` will no longer be able to access GPUs once the device plugin is disabled. Update all workload specs before completing the migration.

## DRA Driver DeviceConfig Fields

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `enable` | bool | `false` | Enable or disable the DRA driver |
| `image` | string | `rocm/k8s-gpu-dra-driver:latest` | DRA driver container image. If not specified, defaults to `rocm/k8s-gpu-dra-driver:latest` |
| `imagePullPolicy` | string | `IfNotPresent` | Image pull policy: Always, IfNotPresent, or Never |
| `tolerations` | list | `[]` | Tolerations for the DRA driver DaemonSet pods |
| `imageRegistrySecret` | object | `{}` | Image pull secret for private registries, e.g. `{"name": "mySecret"}` |
| `cmdLineArguments` | map | `{}` | Additional command-line flags passed to the DRA driver binary. Keys are flag names (without leading `--`) and values are the flag values. For all available flags, see the [DRA driver CLI options reference](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/docs/cli-options.md) |
| `selector` | map | `{}` | Node selector for the DRA driver DaemonSet; if not specified, reuses `spec.selector` |
| `upgradePolicy.upgradeStrategy` | string | `RollingUpdate` | DaemonSet upgrade strategy: `RollingUpdate` or `OnDelete` |
| `upgradePolicy.maxUnavailable` | int | `1` | Maximum pods unavailable during a rolling update |

### Passing Command-Line Arguments

The `cmdLineArguments` field lets you pass flags directly to the `gpu-kubeletplugin` binary. Specify each flag as a key-value pair where the key is the flag name without the leading `--`:

```yaml
spec:
  draDriver:
    enable: true
    cmdLineArguments:
      cdi-root: /etc/cdi
      healthcheck-port: "8080"
      v: "4"
      logging-format: json
```

For the full list of supported flags and their descriptions, refer to the [DRA driver CLI options reference](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/docs/cli-options.md).

## Helm Chart Values

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `deviceConfig.spec.draDriver.enable` | bool | `false` | Enable DRA driver in default DeviceConfig |
| `deviceConfig.spec.draDriver.image` | string | `rocm/k8s-gpu-dra-driver:latest` | DRA driver image |
| `deviceConfig.spec.draDriver.imagePullPolicy` | string | `IfNotPresent` | Image pull policy |
| `deviceConfig.spec.draDriver.tolerations` | list | `[]` | Tolerations |
| `deviceConfig.spec.draDriver.imageRegistrySecret` | object | `{}` | Image pull secret |
| `deviceConfig.spec.draDriver.cmdLineArguments` | object | `{}` | Command-line arguments |
| `deviceConfig.spec.draDriver.selector` | object | `{}` | Node selector; if not specified, reuses `spec.selector` |
| `draDriver.deviceClass.create` | bool | `true` | Whether to create the `gpu.amd.com` DeviceClass |
| `draDriver.serviceAccount.annotations` | object | `{}` | Annotations for the DRA driver ServiceAccount |

## Troubleshooting

### DRA driver pods not starting

- Verify your Kubernetes version is 1.32+ and `DynamicResourceAllocation` feature gate is enabled
- Check the DRA driver DaemonSet events: `kubectl describe daemonset <name>-dra-driver -n kube-amd-gpu`
- Ensure the DRA driver ServiceAccount and RBAC resources exist:

  ```bash
  kubectl get sa amd-gpu-operator-dra-driver -n kube-amd-gpu
  kubectl get clusterrole amd-gpu-operator-dra-driver-role
  ```

### No ResourceSlices published

- **The AMD GPU driver (amdgpu) must be installed** on the worker node. The DRA driver relies on the amdgpu kernel module to enumerate GPU devices. Without it, the DRA driver pod will run but not publish any `ResourceSlices`.
  - Verify the driver is loaded: `lsmod | grep amdgpu` on the worker node
  - If using the operator for driver management, ensure `spec.driver.enable: true` in your `DeviceConfig`
- The DRA driver pod must be running and healthy on the node
- Check pod logs: `kubectl logs <dra-driver-pod> -n kube-amd-gpu`
- Verify AMD GPUs are detected on the node: `kubectl get node <node> -o yaml | grep amd-gpu`

### Validation error: "DRADriver and DevicePlugin cannot be enabled at the same time"

The operator enforces mutual exclusion. Disable the device plugin before enabling the DRA driver (or vice versa). See the [Migration section](#migrating-from-device-plugin-to-dra) above.

## Further Reading

- [AMD GPU DRA Driver (upstream)](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/README.md) — detailed DRA driver documentation, architecture, and advanced configuration
- [DRA Driver CLI Options Reference](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/docs/cli-options.md) — all command-line flags accepted by the `gpu-kubeletplugin` binary
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) — Kubernetes-native DRA concepts and API reference
- [Device Plugin Documentation](../device_plugin/device-plugin.md) — traditional device plugin approach
- [Full DeviceConfig Reference](../fulldeviceconfig.rst) — all available DeviceConfig fields
