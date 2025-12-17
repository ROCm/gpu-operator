# Kubernetes (Helm)

This guide walks through installing the AMD GPU Operator on a Kubernetes cluster using Helm.

<style>
.bd-main .bd-content .bd-article-container {
  max-width: 100%; /*Override the page width to 100%;*/
}

.bd-sidebar-secondary {
  display: none; /*Disable the secondary sidebar from displaying;*/
}
</style>

## Prerequisites

### System Requirements

- Kubernetes cluster v1.29.0 or later
- Helm v3.2.0 or later
- `kubectl` command-line tool configured with access to the cluster
- Cluster admin privileges

### Cluster Requirements

- A functioning Kubernetes cluster with:
  - All system pods running and ready
  - Properly configured Container Network Interface (CNI)
  - Worker nodes with AMD GPUs

### Required Access

- Access to pull images from:
  - AMD's container registry or your configured registry
  - Public container registries (Docker Hub, Quay.io)

## Pre-Installation Steps

### 1. Verify Cluster Status

Check that your cluster is healthy and running:

```bash
kubectl get nodes
kubectl get pods -A
```

Expected output should show:

- All nodes in `Ready` state
- System pods running (kube-system namespace)
- CNI pods running (e.g., Flannel, Calico)

Example of a healthy cluster:

```bash
NAMESPACE      NAME                                          READY   STATUS    RESTARTS   AGE
kube-flannel   kube-flannel-ds-7krtk                         1/1     Running   0          10d
kube-system    coredns-7db6d8ff4d-644fp                      1/1     Running   0          2d20h
kube-system    kube-apiserver-control-plane                  1/1     Running   0          64d
kube-system    kube-controller-manager-control-plane         1/1     Running   0          64d
kube-system    kube-scheduler-control-plane                  1/1     Running   0          64d
```

### 2. Install Cert-Manager

```{note}
If `cert-manager` is already installed in your cluster, you can skip this step.
```

The AMD GPU Operator requires `cert-manager` for TLS certificate management.

- Add the `cert-manager` repository:

```bash
helm repo add jetstack https://charts.jetstack.io --force-update
```

- Install `cert-manager`:

```bash
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.15.1 \
  --set crds.enabled=true
```

- Verify the installation:

```bash
kubectl get pods -n cert-manager
```

Expected output:

```bash
NAME                                       READY   STATUS    RESTARTS   AGE
cert-manager-84489bc478-qjwmw             1/1     Running   0          2m
cert-manager-cainjector-7477d56b47-v8nq8  1/1     Running   0          2m
cert-manager-webhook-6d5cb854fc-h6vbk     1/1     Running   0          2m
```

## Installing Operator

### 1. Add the AMD Helm Repository

```bash
helm repo add rocm https://rocm.github.io/gpu-operator
helm repo update
```

### 2. Install the Operator

Basic installation:
To install the latest version of the GPU Operator run the following Helm install command:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --version=v1.4.1
```

```{note}
Installation Options
  - Skip NFD installation: `--set node-feature-discovery.enabled=false`
  - Skip KMM installation: `--set kmm.enabled=false`. <br> Although KMM is a [Kubernetes-SIGs](https://github.com/kubernetes-sigs) maintained project, it is strongly recommended to use AMD optimized and published KMM images included in each operator release.
  - Disable default DeviceConfig installation: `--set crds.defaultCR.install=false`
```

```{tip}
1. Before v1.3.0 the gpu operator helm chart won't provide a default ```DeviceConfig```, you need to take extra step to create a ```DeviceConfig```.

2. Starting from v1.3.0 the ```helm install``` command would support one-step installation + configuration, which would create a default ```DeviceConfig``` with default values, which may not work for all the users with different the deployment scenarios, please refer to {ref}`typical-deployment-scenarios` for more information and get corresponding ```helm install``` commands. 
```

### 3. Helm Chart Customization Parameters

Installation with custom options:

- Prepare your custom configuration in a YAML file (e.g. ```values.yaml```), then use it with ```helm install``` command to deploy your helm charts. An example values.yaml file can be found [here](https://github.com/ROCm/gpu-operator/blob/master/example/helm_charts_k8s_values_example.yaml) for you to edit and use:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --version=v1.4.1 \
  -f values.yaml
```

The following parameters are able to be configued when using the Helm Chart. In order to view all available options, please refer to this section or run the command ```helm show values rocm/gpu-operator-charts```.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]},"weight":1}]}}` | Deployment affinity configs for controller manager |
| controllerManager.manager.image.repository | string | `"docker.io/rocm/gpu-operator"` | AMD GPU operator controller manager image repository |
| controllerManager.manager.image.tag | string | `"v1.4.1"` | AMD GPU operator controller manager image tag |
| controllerManager.manager.imagePullPolicy | string | `"Always"` | Image pull policy for AMD GPU operator controller manager pod |
| controllerManager.manager.imagePullSecrets | string | `""` | Image pull secret name for pulling AMD GPU operator controller manager image if registry needs credential to pull image |
| controllerManager.manager.resources.limits.cpu | string | `"1000m"` | CPU limits for the controller manager. Consider increasing for large clusters |
| controllerManager.manager.resources.limits.memory | string | `"1Gi"` | Memory limits for the controller manager. Consider increasing if experiencing OOM issues |
| controllerManager.manager.resources.requests.cpu | string | `"100m"` | CPU requests for the controller manager. Adjust based on observed CPU usage |
| controllerManager.manager.resources.requests.memory | string | `"256Mi"` | Memory requests for the controller manager. Adjust based on observed memory usage |
| controllerManager.nodeAffinity.nodeSelectorTerms | list | `[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"},{"key":"node-role.kubernetes.io/master","operator":"Exists"}]` | Node affinity selector terms config for the AMD GPU operator controller manager, set it to [] if you want to make affinity config empty |
| controllerManager.nodeSelector | object | `{}` | Node selector for AMD GPU operator controller manager deployment |
| installdefaultNFDRule | bool | `true` | Set to true to install default NFD rule for detecting AMD GPU hardware based on pci vendor ID and device ID |
| kmm.enabled | bool | `true` | Set to true/false to enable/disable the installation of kernel module management (KMM) operator |
| node-feature-discovery.enabled | bool | `true` | Set to true/false to enable/disable the installation of node feature discovery (NFD) operator |
| upgradeCRD | bool | `true` | CRD will be patched as pre-upgrade/pre-rollback hook when doing helm upgrade/rollback to current helm chart |
| kmm.controller.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]},"weight":1}]}}` | Affinity for the KMM controller manager deployment |
| kmm.controller.manager.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.controller.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.controller.manager.env.relatedImageBuild | string | `"gcr.io/kaniko-project/executor:v1.23.2"` | KMM kaniko builder image for building driver image within cluster |
| kmm.controller.manager.env.relatedImageBuildPullSecret | string | `""` | Image pull secret name for pulling KMM kaniko builder image if registry needs credential to pull image |
| kmm.controller.manager.env.relatedImageSign | string | `"docker.io/rocm/kernel-module-management-signimage:v1.4.1"` | KMM signer image for signing driver image's kernel module with given key pairs within cluster |
| kmm.controller.manager.env.relatedImageSignPullSecret | string | `""` | Image pull secret name for pulling KMM signer image if registry needs credential to pull image |
| kmm.controller.manager.env.relatedImageWorker | string | `"docker.io/rocm/kernel-module-management-worker:v1.4.1"` | KMM worker image for loading / unloading driver kernel module on worker nodes |
| kmm.controller.manager.env.relatedImageWorkerPullSecret | string | `""` | Image pull secret name for pulling KMM worker image if registry needs credential to pull image |
| kmm.controller.manager.image.repository | string | `"docker.io/rocm/kernel-module-management-operator"` | KMM controller manager image repository |
| kmm.controller.manager.image.tag | string | `"v1.4.1"` | KMM controller manager image tag |
| kmm.controller.manager.imagePullPolicy | string | `"Always"` | Image pull policy for KMM controller manager pod |
| kmm.controller.manager.imagePullSecrets | string | `""` | Image pull secret name for pulling KMM controller manager image if registry needs credential to pull image |
| kmm.controller.manager.resources.limits.cpu | string | `"500m"` |  |
| kmm.controller.manager.resources.limits.memory | string | `"384Mi"` |  |
| kmm.controller.manager.resources.requests.cpu | string | `"10m"` |  |
| kmm.controller.manager.resources.requests.memory | string | `"64Mi"` |  |
| kmm.controller.manager.tolerations[0].effect | string | `"NoSchedule"` |  |
| kmm.controller.manager.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kmm.controller.manager.tolerations[0].operator | string | `"Equal"` |  |
| kmm.controller.manager.tolerations[0].value | string | `""` |  |
| kmm.controller.manager.tolerations[1].effect | string | `"NoSchedule"` |  |
| kmm.controller.manager.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kmm.controller.manager.tolerations[1].operator | string | `"Equal"` |  |
| kmm.controller.manager.tolerations[1].value | string | `""` |  |
| kmm.controller.nodeAffinity.nodeSelectorTerms | list | `[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"},{"key":"node-role.kubernetes.io/master","operator":"Exists"}]` | Node affinity selector terms config for the KMM controller manager deployment, set it to [] if you want to make affinity config empty |
| kmm.controller.nodeSelector | object | `{}` | Node selector for the KMM controller manager deployment |
| kmm.controller.replicas | int | `1` |  |
| kmm.controller.serviceAccount.annotations | object | `{}` |  |
| kmm.controllerMetricsService.ports[0].name | string | `"https"` |  |
| kmm.controllerMetricsService.ports[0].port | int | `8443` |  |
| kmm.controllerMetricsService.ports[0].protocol | string | `"TCP"` |  |
| kmm.controllerMetricsService.ports[0].targetPort | string | `"https"` |  |
| kmm.controllerMetricsService.type | string | `"ClusterIP"` |  |
| kmm.kubernetesClusterDomain | string | `"cluster.local"` |  |
| kmm.managerConfig.controllerConfigYaml | string | `"healthProbeBindAddress: :8081\nwebhookPort: 9443\nleaderElection:\n  enabled: true\n  resourceID: kmm.sigs.x-k8s.io\nmetrics:\n  enableAuthnAuthz: true\n  bindAddress: 0.0.0.0:8443\n  secureServing: true\nworker:\n  runAsUser: 0\n  seLinuxType: spc_t\n  firmwareHostPath: /var/lib/firmware"` |  |
| kmm.webhookServer.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]},"weight":1}]}}` | KMM webhook's deployment affinity configs |
| kmm.enabled | bool | `true` | Set to true/false to enable/disable the installation of kernel module management (KMM) operator |
| kmm.webhookServer.nodeAffinity.nodeSelectorTerms | list | `[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"},{"key":"node-role.kubernetes.io/master","operator":"Exists"}]` | Node affinity selector terms config for the KMM webhook deployment, set it to [] if you want to make affinity config empty |
| kmm.webhookServer.nodeSelector | object | `{}` | KMM webhook's deployment node selector |
| kmm.webhookServer.replicas | int | `1` |  |
| kmm.webhookServer.webhookServer.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.webhookServer.webhookServer.args[1] | string | `"--enable-module"` |  |
| kmm.webhookServer.webhookServer.args[2] | string | `"--enable-namespace"` |  |
| kmm.webhookServer.webhookServer.args[3] | string | `"--enable-preflightvalidation"` |  |
| kmm.webhookServer.webhookServer.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.webhookServer.webhookServer.image.repository | string | `"docker.io/rocm/kernel-module-management-webhook-server"` | KMM webhook image repository |
| kmm.webhookServer.webhookServer.image.tag | string | `"v1.2.0"` | KMM webhook image tag |
| kmm.webhookServer.webhookServer.imagePullPolicy | string | `"Always"` | Image pull policy for KMM webhook pod |
| kmm.webhookServer.webhookServer.imagePullSecrets | string | `""` | Image pull secret name for pulling KMM webhook image if registry needs credential to pull image |
| kmm.webhookServer.webhookServer.resources.limits.cpu | string | `"500m"` |  |
| kmm.webhookServer.webhookServer.resources.limits.memory | string | `"384Mi"` |  |
| kmm.webhookServer.webhookServer.resources.requests.cpu | string | `"10m"` |  |
| kmm.webhookServer.webhookServer.resources.requests.memory | string | `"64Mi"` |  |
| kmm.webhookServer.webhookServer.tolerations[0].effect | string | `"NoSchedule"` |  |
| kmm.webhookServer.webhookServer.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kmm.webhookServer.webhookServer.tolerations[0].operator | string | `"Equal"` |  |
| kmm.webhookServer.webhookServer.tolerations[0].value | string | `""` |  |
| kmm.webhookServer.webhookServer.tolerations[1].effect | string | `"NoSchedule"` |  |
| kmm.webhookServer.webhookServer.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kmm.webhookServer.webhookServer.tolerations[1].operator | string | `"Equal"` |  |
| kmm.webhookServer.webhookServer.tolerations[1].value | string | `""` |  |
| kmm.webhookService.ports[0].port | int | `443` |  |
| kmm.webhookService.ports[0].protocol | string | `"TCP"` |  |
| kmm.webhookService.ports[0].targetPort | int | `9443` |  |
| kmm.webhookService.type | string | `"ClusterIP"` |  |
| node-feature-discovery.enabled | bool | `true` | Set to true/false to enable/disable the installation of node feature discovery (NFD) operator |

### 4. Verify the Operator Installation

Check that all operator components are running:

```bash
kubectl get pods -n kube-amd-gpu
```

Expected output:

```bash
NAMESPACE      NAME                                                  READY   STATUS    RESTARTS   AGE
gpu-operator   amd-gpu-operator-controller-manager-6954b68958-ljthg  1/1     Running   0          2m
gpu-operator   amd-gpu-kmm-controller-59b85d48c4-f2hn4               1/1     Running   0          2m
gpu-operator   amd-gpu-kmm-webhook-server-685b9db458-t5qp6           1/1     Running   0          2m
gpu-operator   amd-gpu-nfd-gc-98776b45f-j2hvn                        1/1     Running   0          2m
gpu-operator   amd-gpu-nfd-master-9948b7b76-ncvnz                    1/1     Running   0          2m
gpu-operator   amd-gpu-nfd-worker-dhl7q                              1/1     Running   0          2m
```

Verify that nodes with AMD GPU hardware are properly labeled:

```bash
kubectl get nodes -L feature.node.kubernetes.io/amd-gpu
```

## Resource Configuration

### Controller Manager Resource Settings

The AMD GPU Operator controller manager component has default resource limits and requests configured for typical usage scenarios. You may need to adjust these values based on your specific cluster environment:

```yaml
controllerManager:
  manager:
    resources:
      limits:
        cpu: 1000m
        memory: 1Gi
      requests:
        cpu: 100m
        memory: 256Mi
```

#### When to Adjust Resource Settings

You should consider adjusting the controller manager resource settings in these scenarios:

- **Large clusters**: If managing a large number of nodes or GPU devices, consider increasing both CPU and memory limits
- **Memory pressure**: If you observe OOM (Out of Memory) kills in controller manager pods, increase the memory limit and request
- **CPU pressure**: If the controller manager is experiencing throttling or slow response times during operations, increase the CPU limit and request
- **Resource-constrained environments**: For smaller development or test clusters, you may reduce these values to conserve resources

You can apply resource changes by updating your values.yaml file and upgrading the Helm release:

```bash
helm upgrade amd-gpu-operator amd/gpu-operator-helm \
  --debug \
  --namespace kube-amd-gpu \
  --version=v1.0.0 \
  -f values.yaml
```

## Install Custom Resource

After the installation of AMD GPU Operator:
  * If you are using default `DeviceConfig`, you can modify the default `DeviceConfig` to adjust the config for your own use case. `kubectl edit deviceconfigs -n kube-amd-gpu default`
  * If you installed without default `DeviceConfig` (either by using `--set crds.defaultCR.install=false` or installing a chart prior to v1.3.0), you need to create the `DeviceConfig` custom resource in order to trigger the operator start to work. By preparing the `DeviceConfig` in the YAML file, you can create the resouce by running ```kubectl apply -f deviceconfigs.yaml```.
  * For custom resource definition and more detailed information, please refer to [Custom Resource Installation Guide](../drivers/installation). Here are some examples for common deployment scenarios.

### Inbox or Pre-Installed AMD GPU Drivers

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
     image: docker.io/rocm/device-metrics-exporter:v1.4.1

  # Specifythe node to be managed by this DeviceConfig Custom Resource
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

### Install out-of-tree AMD GPU Drivers with Operator

If you want to use the operator to install out-of-tree version AMD GPU drivers (e.g. install specific ROCm verison driver), you need to configure custom resource to trigger the operator to install the specific ROCm version AMD GPU driver. By creating the following custom resource with ```spec.driver.enable=true```, the operator will call KMM operator to trigger the driver installation on the selected worker nodes.

```{note}
In order to install the out-of-tree version AMD GPU drivers, blacklisting the inbox or pre-installed AMD GPU driver is required, AMD GPU operator can help you push the blacklist option to worker nodes. Please set `spec.driver.blacklist=true`, create the custom resource and reboot the selected worker nodes to apply the new blacklist config. If `amdgpu` remains loaded after reboot and worker nodes keep using inbox / pre-installed driver, run `sudo update-initramfs -u` to update the initial ramdisk with the new modprobe configuration.
```

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
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
    image: docker.io/username/driverimage
    # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
    # you can create the docker-registry type secret by running command like:
    # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
    # Make sure you created the secret within the namespace that KMM operator is running
    imageRegistrySecret:
      name: mysecret
    # Specify the driver version by using ROCm version
    # Starting from ROCm 7.1 the amdgpu version is using new versioning schema
    # please refer to https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/user-kernel-space-compat-matrix.html
    version: "30.20.1"

  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
        
  # Specify the metrics exporter config
  metricsExporter:
     enable: true
     serviceType: "NodePort"
     # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
     nodePort: 32500
     image: docker.io/rocm/device-metrics-exporter:v1.4.1

  # Specifythe node to be managed by this DeviceConfig Custom Resource
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

## Post-Installation Verification

Verify driver installation status:

```bash
kubectl get deviceconfigs -n kube-amd-gpu -oyaml
```

Verify the AMD GPU allocatable resource:

```bash
kubectl get nodes -oyaml | grep "amd.com/gpu"
```

Verify the AMD GPU node label:

```bash
kubectl get nodes -oyaml | grep  "amd.com"
```

## Test GPU Workload Deployment

Create a simple test pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
 name: amd-smi
spec:
 containers:
 - image: docker.io/rocm/pytorch:latest
   name: amd-smi
   command: ["/bin/bash"]
   args: ["-c","amd-smi version && amd-smi monitor -ptum"]
   resources:
    limits:
      amd.com/gpu: 1
    requests:
      amd.com/gpu: 1
 restartPolicy: Never
```

- Create the pod:

```bash
kubectl create -f amd-smi.yaml
```

- Check the logs and verify the output `amd-smi` reflects the expected ROCm version and GPU presence:

```bash
kubectl logs amd-smi
AMDSMI Tool: 24.6.2+2b02a07 | AMDSMI Library version: 24.6.2.0 | ROCm version: 6.2.2
GPU  POWER  GPU_TEMP  MEM_TEMP  GFX_UTIL  GFX_CLOCK  MEM_UTIL  MEM_CLOCK
  0  126 W     40 °C     32 °C       1 %    182 MHz       0 %    900 MHz
```

- Delete the pod:

```bash
kubectl delete -f amd-smi.yaml
```

## Troubleshooting

If you encounter issues during installation:

- Check operator logs:

```bash
kubectl logs -n kube-amd-gpu \
  deployment/amd-gpu-operator-controller-manager
```

- Check KMM status:

```bash
kubectl get modules -n kube-amd-gpu
```

- Check NFD status:

```bash
kubectl get nodefeaturerules -n kube-amd-gpu
```

For more detailed troubleshooting steps, see our [Troubleshooting Guide](../troubleshooting).

## Uninstallation

Please refer to the [Uninstallation](../uninstallation/uninstallation) document for uninstalling related resources.
