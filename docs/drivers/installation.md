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

- These commands need to either be run as `root` or by using `sudo`
- Create blacklist configuration file on worker nodes:

```bash
echo "blacklist amdgpu" > /etc/modprobe.d/blacklist-amdgpu.conf
```

- After blacklist configuration file, you need to rebuild the initramfs for the change to take effect:

```bash
echo update-initramfs -u -k all
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

#### Method 3 - (Openshift) Use Machine Config Operator

Please refer to [OpenShift installation Guide](../installation/openshift-olm) to see the example `MachineConfig` custom resource to add blacklist via Machine Config Operator.

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
     image: docker.io/rocm/device-metrics-exporter:v1.4.0

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
    # Specify the driver version by using ROCm version
    # Starting from ROCm 7.1 the amdgpu version is using new versioning schema
    # please refer to https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/user-kernel-space-compat-matrix.html
    version: "30.20.1"
    # blacklist is required for installing out-of-tree amdgpu kernel module, this depends on spec.deviceplugin.enableNodeLabeller to work
    # Not working for OpenShift cluster. OpenShift users please use the Machine Config Operator (MCO) resource to configure amdgpu blacklist.
    # Example MCO resource is available at https://instinct.docs.amd.com/projects/gpu-operator/en/latest/installation/openshift-olmhtml#create-blacklist-for-installing-out-of-tree-kernel-module
    blacklist: true
    # Specify your repository to host driver image
    # Note:
    # 1. DO NOT include the image tag as AMD GPU Operator will automatically manage the image tag for you
    # 2. Updating the driver image repository is not supported. Please delete the existing DeviceConfig and create a new one with the updated image repository
    image: docker.io/username/repo
    # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
    # you can create the docker-registry type secret by running command like:
    # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
    # Make sure you created the secret within the namespace that KMM operator is running
    imageRegistrySecret:
      name: mysecret
    # (Optional) Currently only for OpenShift cluster, set to true to use source code image to build driver within the cluster
    # default is false and operator will use debian or rpm package from radeon repo to install driver
    useSourceImage: false
    # (Optional) configure the driver image build within the cluster
    imageBuild:
      # configure the registry to search for base image for building driver
      # e.g. if you are using worker node with ubuntu 22.04 and baseImageRegistry is docker.io
      # image builder will use docker.io/ubuntu:22.04 as base image
      baseImageRegistry: docker.io
      # sourceImageRepo: specify the amdgpu source code image repo for building driver
      # the Operator will decide the image tag based on user provided driver version and system OS version
      # e.g. if you input docker.io/rocm/amdgpu-driver the image tag will be coreos-<rhel version>-<driver version>
      # NOTE: currently only work for OpenShift cluster
      # NOTE: will be used when spec.driver.useSourceImage is true
      sourceImageRepo: docker.io/rocm/amdgpu-driver
      baseImageRegistryTLS:
        insecure: False # If True, check for the container image using plain HTTP
        insecureSkipTLSVerify: False # If True, skip any TLS server certificate validation (useful for self-signed certificates)

  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
        
  # Specify the metrics exporter config
  metricsExporter:
     enable: true
     serviceType: "NodePort"
     # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
     nodePort: 32500
     image: docker.io/rocm/device-metrics-exporter:v1.4.0

  # Specifythe node to be managed by this DeviceConfig Custom Resource
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

```{note}
When configuring `spec.driver.imageBuild`, consider the following registry authentication requirements:

1. **Single Secret for Multiple Registries**: If your base OS image or source image is hosted in a registry requiring pull secrets, use `spec.driver.imageRegistrySecret` to inject credentials. This secret was originally designed for the repository in `spec.driver.image`, but can be combined to support multiple registries.

2. **Combining Multiple Registry Credentials**: If base and source images require different secrets, combine them into a single Kubernetes secret:

    ```bash
    REGISTRY1=https://index.docker.io/v1/
    USER1=my-username-1
    PWD1=my-password-1
    REGISTRY2=another-registry.io:5000
    USER2=my-username-2
    PWD2=my-password-2
    cat > config.json <<EOF
    {
      "auths": {
      "${REGISTRY1}": {
      "auth": "$(echo -n "${USER1}:${PWD1}" | base64 -w0)"
      },
      "${REGISTRY2}": {
      "auth": "$(echo -n "${USER2}:${PWD2}" | base64 -w0)"
      }
      }
    }
    EOF
    unset REGISTRY1 USER1 PWD1 REGISTRY2 USER2 PWD2

    kubectl delete secret generic mysecret -n kube-amd-gpu --ignore-not-found
    kubectl create secret generic mysecret -n kube-amd-gpu \
      --type=kubernetes.io/dockerconfigjson \
      --from-file=.dockerconfigjson=config.json
    ```

3. **OpenShift Internal Registry**: For OpenShift users using the internal image registry to pull/push compiled driver images while needing another secret for base/source images, combine credentials with the OpenShift internal builder secret:

    ```bash
    #!/bin/bash
    set -e
    NS=openshift-amd-gpu
    REGISTRY1=https://index.docker.io/v1/
    USER1=my-username-1    # put registry username here
    PWD1=my-password-1     # put registry password or token here
    SECRET_NAME=mysecret   # change this to desired secret name

    # 1. ensure builder has push rights
    oc policy add-role-to-user system:image-builder -z builder -n $NS

    # 2. create long-lived token secret
    oc apply -f - <<EOF
    apiVersion: v1
    kind: Secret
    metadata:
      name: builder-token
      namespace: ${NS}
      annotations:
      kubernetes.io/service-account.name: builder
    type: kubernetes.io/service-account-token
    EOF

    # 3. wait for token to be populated
    for i in {1..10}; do
      BUILDER_TOKEN=$(oc get secret builder-token -n ${NS} -o jsonpath='{.data.token}' 2>/dev/null)
      [[ -n "$BUILDER_TOKEN" ]] && break
      sleep 1
    done
    [ -z "$BUILDER_TOKEN" ] && { echo "❌ token not ready"; exit 1; }

    # 4. generate combined docker config
    cat > config.json <<EOF
    {
      "auths": {
      "image-registry.openshift-image-registry.svc:5000": {
      "auth": "$(echo -n "<token>:$(echo $BUILDER_TOKEN | base64 -d)" | base64 -w0)"
      },
      "image-registry.openshift-image-registry.svc.cluster.local:5000": {
      "auth": "$(echo -n "<token>:$(echo $BUILDER_TOKEN | base64 -d)" | base64 -w0)"
      },
      "${REGISTRY1}": {
      "auth": "$(echo -n "${USER1}:${PWD1}" | base64 -w0)"
      }
      }
    }
    EOF

    # 5. create kubernetes secret
    oc delete secret "${SECRET_NAME}" -n "$NS" --ignore-not-found
    oc create secret generic "${SECRET_NAME}" \
      -n "$NS" \
      --type=kubernetes.io/dockerconfigjson \
      --from-file=.dockerconfigjson=config.json

    echo "✅ Secret '${SECRET_NAME}' created and ready."
    ```
```

#### Configuration Reference

To list existing `DeviceConfig` resources run `kubectl get deviceconfigs -A`

To check the full spec of `DeviceConfig` definition run `kubectl get crds deviceconfigs.amd.com -oyaml`

#### `metadata` Parameters

| Parameter   | Description                                  |
| ----------- | -------------------------------------------- |
| `name`      | Unique identifier for the resource           |
| `namespace` | Namespace where the operator is running      |

#### `spec.driver` Parameters

| Parameter                                               | Description                                                                                                                                                                                                                                         | Default                                                                                                                                                   |
|---------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `enable`                                                | Set to true for installing out-of-tree driver.<br>Set to false to skip driver install and use inbox/pre-installed driver.                                                                                                                           | `true`                                                                                                                                                    |
| `blacklist`                                             | Set to true to have the operator init the node labeller DaemonSet and add `amdgpu` to the selected worker nodes' modprobe blacklist.<br>Set to false to remove `amdgpu` from the selected nodes' modprobe blacklist.                                | `false`                                                                                                                                                   |
| `version`                                               | ROCm driver version (e.g., "6.2.2").<br>See ROCm Versions: https://rocm.docs.amd.com/en/latest/release/versions.html                                                                                                                                | Ubuntu: `6.1.3`<br>CoreOS: `6.2.2`                                                                                                                        |
| `image`                                                 | Registry URL and repository (without tag).<br>Note: Operator manages tags automatically.                                                                                                                                                            | Vanilla k8s: `image-registry:5000/$MOD_NAMESPACE/amdgpu_kmod`<br>OpenShift: `image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amdgpu_kmod` |
| `imageRegistrySecret.name`                              | Name of registry credentials secret to pull/push driver image.                                                                                                                                                                                      |                                                                                                                                                           |
| `imageRegistryTLS.insecure`                             | If true, check if the container image already exists using plain HTTP.                                                                                                                                                                              | `false`                                                                                                                                                   |
| `imageRegistryTLS.insecureSkipTLSVerify`                | If true, skip any TLS server certificate validation.                                                                                                                                                                                                | `false`                                                                                                                                                   |
| `imageSign.keySecret`                                   | Secret name of the private key used to sign kernel modules after image building in cluster.<br>See secure boot doc for instructions to create the secret: `./secure-boot`.                                                                          |                                                                                                                                                           |
| `imageSign.certSecret`                                  | Secret name of the public key used to sign kernel modules after image building in cluster.<br>See secure boot doc for instructions to create the secret: `./secure-boot`.                                                                           |                                                                                                                                                           |
| `tolerations`                                           | List of tolerations that will be set for KMM Module object and components like build pod and worker pod.                                                                                                                                            |                                                                                                                                                           |
| `imageBuild.baseImageRegistry`                          | Registry hosting the base OS image.<br>Example: With Ubuntu 22.04 worker node and `docker.io`, the operator uses `docker.io/ubuntu:22.04` as base image.                                                                                            | `docker.io`                                                                                                                                               |
| `imageBuild.baseImageRegistryTLS.insecure`              | If true, check if the container image already exists using plain HTTP.                                                                                                                                                                              | `false`                                                                                                                                                   |
| `imageBuild.baseImageRegistryTLS.insecureSkipTLSVerify` | If true, skip any TLS server certificate validation.                                                                                                                                                                                                | `false`                                                                                                                                                   |
| `imageBuild.sourceImageRepo`                            | (OpenShift only) Image repository hosting the amdgpu source code image. The operator determines the image tag based on the system and `spec.driver.version`.<br>Example: ROCm 7.0 + RHEL 9.6 → `docker.io/rocm/amdgpu-driver:coreos-9.6-7.0`.       | `docker.io/rocm/amdgpu-driver`                                                                                                                            |

#### `spec.devicePlugin` Parameters

| Parameter                       | Description                                                                         | Default                                  |
|---------------------------------|-------------------------------------------------------------------------------------|------------------------------------------|
| `devicePluginImage`             | AMD GPU device plugin image                                                         | `rocm/k8s-device-plugin:latest`          |
| `nodeLabellerImage`             | Node labeller image                                                                 | `rocm/k8s-device-plugin:labeller-latest` |
| `imageRegistrySecret.name`      | Name of registry credentials secret<br> to pull device plugin / node labeller image |                                          |
| `enableNodeLabeller`            | enable / disable node labeller                                                      | `true`                                   |

#### `spec.metricsExporter` Parameters

| Parameter | Description | Default |
| --------- | ----------- | ------- |
| `enable` | Enable/disable metrics exporter | `false` |
| `imageRegistrySecret.name` | Name of registry credentials secret<br> to pull metrics exporter image | |
| `serviceType` | Service type for metrics endpoint <br>Options: "ClusterIP" or "NodePort" | `ClusterIP` |
| `port` | cluster IP's internal service port<br> for reaching the metrics endpoint | `5000` |
| `nodePort` | Port number when using NodePort service type | automatically assigned |
| `selector` | select which nodes to enable metrics exporter | same as `spec.selector` |

#### `spec.selector` Parameters

| Parameter  | Description                                     | Default                                         |
|------------|-------------------------------------------------|-------------------------------------------------|
| `selector` | Labels to select nodes for driver installation +| `feature.node.kubernetes.io/amd-gpu: "true"`    |

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
