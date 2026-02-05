# Device Plugin

## Configure device plugin

To start the Device Plugin along with the GPU Operator configure fields under the ``` spec/devicePlugin ``` field in deviceconfig Custom Resource(CR)

```yaml
  devicePlugin:
    # Specify the device plugin image
    # default value is rocm/k8s-device-plugin:latest
    devicePluginImage: rocm/k8s-device-plugin:latest

    # The device plugin arguments is used to pass supported flags and their values while starting device plugin daemonset
    devicePluginArguments:
      resource_naming_strategy: single

    # Specify the node labeller image
    # default value is rocm/k8s-device-plugin:labeller-latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
  
    # The node labeller arguments is used to pass supported flags while starting node labeller daemonset
    nodeLabellerArguments:
     - compute-partitioning-supported
     - memory-partitioning-supported
     - compute-memory-partition

    # Specify whether to bring up node labeller component
    # default value is true
    enableNodeLabeller: True

```

The **device-plugin** pods start after updating the **DeviceConfig** CR

```bash
#kubectl get pods -n kube-amd-gpu
NAME                                                              READY   STATUS    RESTARTS       AGE
amd-gpu-operator-gpu-operator-charts-controller-manager-77tpmgn   1/1     Running   0              4h9m
amd-gpu-operator-kmm-controller-6d459dffcf-lbgtt                  1/1     Running   0              4h9m
amd-gpu-operator-kmm-webhook-server-5fdc8b995-qgj49               1/1     Running   0              4h9m
amd-gpu-operator-node-feature-discovery-gc-78989c896-7lh8t        1/1     Running   0              3h48m
amd-gpu-operator-node-feature-discovery-master-b8bffc48b-6rnz6    1/1     Running   0              4h9m
amd-gpu-operator-node-feature-discovery-worker-m9lwn              1/1     Running   0              4h9m
test-deviceconfig-device-plugin-rk5f4                             1/1     Running   0              134m
test-deviceconfig-node-labeller-bxk7x                             1/1     Running   0              134m
```

<div style="background-color: #d0e7f; border-left: 6px solid #2196F3; padding: 10px;">
<strong>Note:</strong> The Device Plugin name will be prefixed with the name of your DeviceConfig custom resource
</div></br>

## Device Plugin DeviceConfig

| Field Name                       | Details                                      |
|----------------------------------|----------------------------------------------|
| **DevicePluginImage**            | Device plugin image                          |
| **DevicePluginImagePullPolicy**  | One of Always, Never, IfNotPresent.          |
| **NodeLabellerImage**            | Node labeller image                          |
| **NodeLabellerImagePullPolicy**  | One of Always, Never, IfNotPresent.          |
| **EnableNodeLabeller**           | Enable/Disable node labeller with True/False |
| **DevicePluginArguments**        | The flag/values to pass on to Device Plugin  |
| **NodeLabellerArguments**        | The flags to pass on to Node Labeller        |

</br>

1. Both the `ImagePullPolicy` fields default to `Always` if `:latest` tag is specified on the respective Image, or defaults to `IfNotPresent` otherwise. This is default k8s behaviour for `ImagePullPolicy`

2. `DevicePluginArguments` is of type `map[string]string`. Currently supported key value pairs to set under `DevicePluginArguments` are:
   -> "resource_naming_strategy": {"single", "mixed"}

3. `NodeLabellerArguments` is of type `[]string`. Currently supported flags to set under `NodeLabellerArguments` are:
   - {"compute-memory-partition", "compute-partitioning-supported", "memory-partitioning-supported"}
   - For the above new partition labels, the labels being set under this field will be applied by nodelabeller on the node

   The below labels are enabled by nodelabeller by default internally:
   - {"vram", "cu-count", "simd-count", "device-id", "family", "product-name", "driver-version"}

## How to choose Resource Naming Strategy

To customize the way device plugin reports gpu resources to kubernetes as allocatable k8s resources, use the `single` or `mixed` resource naming strategy in **DeviceConfig** CR
Before understanding each strategy, please note the definition of homogeneous and heterogeneous nodes

Homogeneous node: A node whose gpu's follow the same compute-memory partition style
    -> Example: A node of 8 GPU's where all 8 GPU's are following CPX-NPS4 partition style

Heterogeneous node: A node whose gpu's follow different compute-memory partition styles
    -> Example: A node of 8 GPU's where 5 GPU's are following SPX-NPS1 and 3 GPU's are following CPX-NPS1

### Single

In `single` mode, the device plugin reports all gpu's (regardless of whether they are whole gpu's or partitions of a gpu) under the resource name `amd.com/gpu`
This mode is supported for homogeneous nodes but not supported for heterogeneous nodes

A node which has 8 GPUs where all GPUs are not partitioned will report its resources as:

```bash
amd.com/gpu: 8
```

A node which has 8 GPUs where all GPUs are partitioned using CPX-NPS4 style will report its resources as:

```bash
amd.com/gpu: 64
```

### Mixed

In `mixed` mode, the device plugin reports all gpu's under a name which matches its partition style.
This mode is supported for both homogeneous nodes and heterogeneous nodes

A node which has 8 GPUs which are all partitioned using CPX-NPS4 style will report its resources as:

```bash
amd.com/cpx_nps4: 64
```

A node which has 8 GPUs where 5 GPU's are following SPX-NPS1 and 3 GPU's are following CPX-NPS1 will report its resources as:

```bash
amd.com/spx_nps1: 5
amd.com/cpx_nps1: 24
```

#### **Notes**

- If `resource_naming_strategy` is not passed using `DevicePluginArguments` field in CR, then device plugin will internally default to `single` resource naming strategy. This maintains backwards compatibility with earlier release of device plugin with reported resource name of `amd.com/gpu`
- If a node has GPUs which do not support partitioning, such as MI210, then the GPUs are reported under resource name `amd.com/gpu` regardless of the resource naming strategy
- These different naming styles of resources, for example, `amd.com/cpx_nps1` should be followed when requesting for resources in a pod spec
