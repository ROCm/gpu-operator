# Device Config Manager

## Overview

The Device Config Manager (DCM) is a component of the GPU Operator that is used to handle the configuration of AMD Instinct GPUs, specifically in regards to GPU partitioning. In the future, DCM will also be expanded to handle the configuration of AMD's AI-NIC. Like other GPU Operator components DCM runs as a daemonset on each GPU node in your cluster. DCM can be enabled via the GPU Operator's custom resource called "DeviceConfig". The current goal of the Device Config Manager is to handle the configuration and implementation of GPU partitioning on your Kubernetes cluster, allowing for partitioning modes to be set on each GPU Node based on partition profiles that you specify via a Kubernetes config-map.

## GPU Partition Overview

For an overview of GPU partitioning on AMD GPUs and what modes are currently supported see the [AMD Datacenter GPU Driver Docs - GPU Partitioning](https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/index.html) docs.

## Configuring the Device Config Manager

The Device Config Manager can be enabled by setting the `spec/configManager/enable` flag in the DeviceConfig Custom Resource (CR) to `True`. Below is an example excerpt from the DeviceConfig:

```yaml
  configManager:
    # To enable/disable the metrics exporter, enable to partition
    enable: True

    # image for the device-config-manager container
    image: "rocm/device-config-manager:v1.3.1"

    # image pull policy for config manager. Accepted values are Always, IfNotPresent, Never
    imagePullPolicy: IfNotPresent

    # specify configmap name which stores profile config info
    config: 
      name: "config-manager-config"

    # DCM pod deployed either as a standalone pod or through the GPU operator will have 
    # a toleration attached to it. User can specify additional tolerations if required
    # key: amd-dcm , value: up , Operator: Equal, effect: NoExecute 

    # OPTIONAL
    # toleration field for dcm pod to bypass nodes with specific taints
    configManagerTolerations:
      - key: "key1"
        operator: "Equal" 
        value: "value1"
        effect: "NoExecute"

```

```{note}
1. The `ImagePullPolicy` field default to `Always` if `latest` image tag is used, otherwise it will default to `IfNotPresent`. This is default k8s behavior for `ImagePullPolicy`.

2. The `ConfigMap` name is of type `string`. Ensure you change the `spec/configManager/config/name` to match the name of the config map you will be using in the GPU Operator namespace. Device-Config-Manager pod needs a configmap to be present or else the pod does not come up.

3. You can also specify any tolerations for DCM in the DeviceConfig if your cluster is using specific taints.

4. The Device Config Manager name will be prefixed with the name of your DeviceConfig custom resource (eg. `gpu-operator-device-config-manager`)
```

The **device-config-manager** pod will start after apply or updating the **DeviceConfig** CR to enable it.

```bash
> kubectl get pods -n kube-amd-gpu

NAME                                                                            READY     STATUS    RESTARTS    AGE
kube-amd-gpu   amd-gpu-operator-gpu-operator-charts-controller-manager-6drmvl7   1/1     Running       0       3h14m
kube-amd-gpu   amd-gpu-operator-kmm-controller-6d459dffcf-ltf5h                  1/1     Running       0       3h14m
kube-amd-gpu   amd-gpu-operator-kmm-webhook-server-5fdc8b995-c8crh               1/1     Running       0       3h14m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-gc-78989c896-2zmnl        1/1     Running       0       3h14m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-master-b8bffc48b-xkqkx    1/1     Running       0       3h14m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-kb5tk              1/1     Running       0       3h14m
kube-amd-gpu   gpu-operator-device-config-manager-hn9rb                          1/1     Running       0       3h14m
kube-amd-gpu   gpu-operator-device-plugin-zft6k                                  1/1     Running       0       3h14m
```

After DCM has completed the partitioning and once device plugin is brought up again, the resources (whether single gpus or partitioned gpus) are represented on the k8s node as per this documentation:
[Device Plugin Resources](../device_plugin/device-plugin.md)