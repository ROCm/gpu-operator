# Driver Installation Guide

This guide explains how to install AMD GPU drivers using the AMD GPU Operator on Kubernetes clusters.

## Prerequisites

Before installing the AMD GPU driver:

1. Ensure the AMD GPU Operator and its dependencies are successfully deployed
2. Have cluster admin permissions
3. Have access to an image registry for driver images (if trying to install out-of-tree driver by operator)

## Installation Steps

### 1. Blacklist Inbox Driver

#### Method 1 - Manual Blacklist

Before installing the out-of-tree AMD GPU driver, you must blacklist the inbox AMD GPU driver:

- Create blacklist configuration file on worker nodes:

```bash
echo "blacklist amdgpu" > /etc/modprobe.d/blacklist-amdgpu.conf
```

- Reboot the worker node to apply the blacklist
- Verify the blacklisting:

```bash
lsmod | grep amdgpu
```

This command should return no results, indicating the module is not loaded.

#### Method 2 - Use Operator to add blacklist

When you try to create a `DeviceConfig` custom resource, you may consider set `spec.driver.blacklist=true` to ask for AMD GPU operator to add the `amdgpu` to blacklist for you, then you can reboot all selected worker node to apply the new modprobe blacklist.

```{note}
If `amdgpu` remains loaded after reboot, and worker nodes keep using inbox / pre-installed driver, run `sudo update-initramfs -u` to update the initial ramdisk with the new modprobe configuration.
```

### 2. Create DeviceConfig Resource

#### Inbox or Pre-Installed AMD GPU Drivers

In order to directly use inbox or pre-installed AMD GPU drivers on the worker node, the operator's driver installation need to be skipped, thus ```spec.driver.enable=false``` need to be specified. By deploying the following custom resource, the operator will directly deploy device plugin, node labeller and metrics exporter on all selected AMD GPU worker nodes.

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
  # use the namespace where AMD GPU Operator is running
  namespace: kube-amd-gpu
spec:
  driver:
    # disable the installation of our-of-tree amdgpu kernel module
    enable: false

  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
        
  # Specify the metrics exporter config
  metricsExporter:
     enable: true
     serviceType: "NodePort"
     # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
     nodePort: 32500
     image: docker.io/rocm/device-metrics-exporter:v1.2.0

  # Specifythe node to be managed by this DeviceConfig Custom Resource
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

#### Install out-of-tree AMD GPU Drivers with Operator

If you want to use the operator to install out-of-tree version AMD GPU drivers (e.g. install specific ROCm verison driver), you need to configure custom resource to trigger the operator to install the specific ROCm version AMD GPU driver. By creating the following custom resource with ```spec.driver.enable=true```, the operator will call KMM operator to trigger the driver installation on the selected worker nodes.

```{note}
In order to install the out-of-tree version AMD GPU drivers, blacklisting the inbox or pre-installed AMD GPU driver is required, AMD GPU operator can help you push the blacklist option to worker nodes. Please set `spec.driver.blacklist=true`, create the custom resource and reboot the selected worker nodes to apply the new blacklist config. If `amdgpu` remains loaded after reboot and worker nodes keep using inbox / pre-installed driver, run `sudo update-initramfs -u` to update the initial ramdisk with the new modprobe configuration.
```

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: gpu-operator
  # use the namespace where AMD GPU Operator is running
  namespace: kube-amd-gpu
spec:
  driver:
    # enable operator to install out-of-tree amdgpu kernel module
    enable: true
    # blacklist is required for installing out-of-tree amdgpu kernel module
    blacklist: true
    # Specify your repository to host driver image
    # DO NOT include the image tag as AMD GPU Operator will automatically manage the image tag for you
    image: docker.io/username/repo
    # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
    # you can create the docker-registry type secret by running command like:
    # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
    # Make sure you created the secret within the namespace that KMM operator is running
    imageRegistrySecret:
      name: mysecret
    # Specify the driver version by using ROCm version
    version: "6.2.1"

  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
        
  # Specify the metrics exporter config
  metricsExporter:
     enable: true
     serviceType: "NodePort"
     # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
     nodePort: 32500
     image: docker.io/rocm/device-metrics-exporter:v1.2.0

  # Specifythe node to be managed by this DeviceConfig Custom Resource
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

#### Configuration Reference

To list existing `DeviceConfig` resources run `kubectl get deviceconfigs -A`

To check the full spec of `DeviceConfig` definition run `kubectl get crds deviceconfigs.amd.com -oyaml`

#### `metadata` Parameters

| Parameter | Description |
|-----------|-------------|
| `name` | Unique identifier for the resource |
| `namespace` | Namespace where the operator is running |

#### `spec.driver` Parameters

| Parameter | Description | Default |
|-----------|-------------|-------------|
| `enable` | set to true for installing out-of-tree driver, <br>set it to false then operator will skip driver install <br>and directly use inbox / pre-installed driver | `true` |
| `blacklist` | set to true then operator will init node labeller daemonset <br>to add `amdgpu` into selected worker nodes modprobe blacklist,<br> set to false then operator will remove `amdgpu` <br>from selected nodes' modprobe blacklist | `false` |
| `version` | ROCm driver version (e.g., "6.2.2")<br>[See ROCm Versions](https://rocm.docs.amd.com/en/latest/release/versions.html) | Ubuntu: `6.1.3`<br>CoresOS: `6.2.2` |
| `image` | Registry URL and repository (without tag) <br>*Note: Operator manages tags automatically* | Vanilla k8s: `image-registry:5000/$MOD_NAMESPACE/amdgpu_kmod`|
| `imageRegistrySecret.name` | Name of registry credentials secret<br> to pull/push driver image | |
| `imageRegistryTLS.insecure` | If true, check if the container image<br> already exists using plain HTTP | `false` |
| `imageRegistryTLS.insecureSkipTLSVerify` | If true, skip any TLS server certificate validation | `false` |
| `imageSign.keySecret` | secret name of the private key<br> used to sign kernel modules after image building in cluster<br>see [secure boot](./secure-boot) doc for instructions to create the secret | |
| `imageSign.certSecret` | secret name of the public key<br> used to sign kernel modules after image building in cluster<br>see [secure boot](./secure-boot) doc for instructions to create the secret | |

#### `spec.devicePlugin` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `devicePluginImage` | AMD GPU device plugin image | `rocm/k8s-device-plugin:latest` |
| `nodeLabellerImage` | Node labeller image | `rocm/k8s-device-plugin:labeller-latest` |
| `imageRegistrySecret.name` | Name of registry credentials secret<br> to pull device plugin / node labeller image | |
| `enableNodeLabeller` | enable / disable node labeller | `true` |

#### `spec.metricsExporter` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `enable` | Enable/disable metrics exporter | `false` |
| `imageRegistrySecret.name` | Name of registry credentials secret<br> to pull metrics exporter image | |
| `serviceType` | Service type for metrics endpoint <br>Options: "ClusterIP" or "NodePort" | `ClusterIP` |
| `port` | clsuter IP's internal service port<br> for reaching the metrics endpoint | `5000` |
| `nodePort` | Port number when using NodePort service type | automatically assigned |
| `selector` | select which nodes to enable metrics exporter | same as `spec.selector` |

#### `spec.selector` Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `selector` | Labels to select nodes for driver installation | `feature.node.kubernetes.io/amd-gpu: "true"` |

### Registry Secret Configuration

If you're using a private registry, create a docker registry secret before deploying:

```bash
kubectl create secret docker-registry mysecret \
  -n kmm-namespace \
  --docker-server=registry.example.com \
  --docker-username=xxx \
  --docker-password=xxx
```

If you are using DockerHub to host images, you don't need to specify the ```--docker-server``` parameter when creating the secret.

### 3. Monitor Installation Status

Check the deployment status:

```bash
kubectl get deviceconfigs test-deviceconfig -n kube-amd-gpu -o yaml
```

Example status output:

```yaml
status:
  devicePlugin:
    availableNumber: 1             # Nodes with device plugin running
    desiredNumber: 1               # Target number of nodes
    nodesMatchingSelectorNumber: 1 # Nodes matching selector
  driver:
    availableNumber: 1             # Nodes with driver installed
    desiredNumber: 1               # Target number of nodes
    nodesMatchingSelectorNumber: 1 # Nodes matching selector
  nodeModuleStatus:
    worker-1:                      # Node name
      containerImage: registry.example.com/amdgpu:6.2.2-5.15.0-generic
      kernelVersion: 5.15.0-generic
      lastTransitionTime: "2024-08-12T12:37:03Z"
```

## Custom Resource Installation Validation

After applying configuration:

- Check DeviceConfig status:

```bash
kubectl get deviceconfig amd-gpu-config -n kube-amd-gpu -o yaml
```

- Verify driver deployment:

```bash
kubectl get pods -n kube-amd-gpu -l app=kmm-worker
```

- Check metrics endpoint (if enabled):

```bash
# For ClusterIP
kubectl port-forward svc/gpu-metrics -n kube-amd-gpu 9400:9400

# For NodePort
curl http://<node-ip>:<nodePort>/metrics
```

- Verify worker node labels:

```bash
kubectl get nodes -l feature.node.kubernetes.io/amd-gpu=true
```

## Driver and Module Management

### Driver Uninstallation Requirements

- Keep all resources available when uninstalling drivers by deleting DeviceConfig:
  - Image registry access
  - Driver images
  - Registry credential secrets
- Removing any of these resources may prevent proper driver uninstallation

### Module Management

- The AMD GPU Operator must exclusively manage the `amdgpu` kernel module
- DO NOT manually load/unload the module
- All changes must be made through the operator
