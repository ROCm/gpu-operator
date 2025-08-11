# AMD GPU Operator

## :book: GPU Operator Documentation Site

For the most detailed and up-to-date documentation please visit our Instinct Documenation site: [https://instinct.docs.amd.com/projects/gpu-operator](https://instinct.docs.amd.com/projects/gpu-operator)

## Introduction

AMD GPU Operator simplifies the deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

## Components

* AMD GPU Operator Controller
* K8s Device Plugin
* K8s Node Labeller
* Device Metrics Exporter
* Device Test Runner
* Node Feature Discovery Operator
* Kernel Module Management Operator

## Features

* Streamlined GPU driver installation and management
* Comprehensive metrics collection and export
* Easy deployment of AMD GPU device plugin for Kubernetes
* Automated labeling of nodes with AMD GPU capabilities
* Compatibility with standard Kubernetes environments
* Efficient GPU resource allocation for containerized workloads
* GPU health monitoring and troubleshooting  

## Compatibility

* **ROCm DKMS Compatibility**: Please refer to the [ROCM official website](https://rocm.docs.amd.com/en/latest/compatibility/compatibility-matrix.html) for the compatability matrix for ROCM driver.
* **Kubernetes**: 1.29.0+

## Prerequisites

* Kubernetes v1.29.0+
* Helm v3.2.0+
* `kubectl` CLI tool configured to access your cluster
* [Cert Manager](https://cert-manager.io/docs/) Install it by running these commands if not already installed in the cluster:

```bash
helm repo add jetstack https://charts.jetstack.io --force-update

helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.15.1 \
  --set crds.enabled=true
```

## Quick Start

### 1. Add the AMD Helm Repository

```bash
helm repo add rocm https://rocm.github.io/gpu-operator
helm repo update
```

### 2. Install the Operator

#### Basic installation

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --version=v1.4.0
```

#### Installation Options

* Skip NFD installation: `--set node-feature-discovery.enabled=false`
* Skip KMM installation: `--set kmm.enabled=false`

> [!WARNING]
> It is strongly recommended to use AMD-optimized KMM images included in the operator release. This is not required when installing the GPU Operator on Red Hat OpenShift.

### 3. Install Custom Resource
After the installation of AMD GPU Operator:
  * By default there will be a default `DeviceConfig` installed. If you are using default `DeviceConfig`, you can modify the default `DeviceConfig` to adjust the config for your own use case. `kubectl edit deviceconfigs -n kube-amd-gpu default`
  * If you installed without default `DeviceConfig` (either by using `--set crds.defaultCR.install=false` or installing a chart prior to v1.3.0), you need to create the `DeviceConfig` custom resource in order to trigger the operator start to work. By preparing the `DeviceConfig` in the YAML file, you can create the resouce by running ```kubectl apply -f deviceconfigs.yaml```.
  * For custom resource definition and more detailed information, please refer to [Custom Resource Installation Guide](https://dcgpu.docs.amd.com/projects/gpu-operator/en/latest/installation/kubernetes-helm.html#install-custom-resource).

  * Potential Failures with default `DeviceConfig`: 

    a. Operand pods are stuck in ```Init:0/1``` state: It means your GPU worker doesn't have inbox GPU driver loaded. We suggest check the [Driver Installation Guide]([./drivers/installation.md](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/drivers/installation.html#driver-installation-guide)) then modify the default `DeviceConfig` to ask Operator to install the out-of-tree GPU driver for your worker nodes.
  `kubectl edit deviceconfigs -n kube-amd-gpu default`

    b. No operand pods showed up: It is possible that default `DeviceConfig` selector `feature.node.kubernetes.io/amd-gpu: "true"` cannot find any matched node.
      * Check node label `kubectl get node -oyaml | grep -e "amd-gpu:" -e "amd-vgpu:"`
      * If you are using GPU in the VM, you may need to change the default `DeviceConfig` selector to `feature.node.kubernetes.io/amd-vgpu: "true"`
      * You can always customize the node selector of the `DeviceConfig`.

### Grafana Dashboards

Following dashboards are provided for visualizing GPU metrics collected from device-metrics-exporter:

* Overview Dashboard: Provides a comprehensive view of the GPU cluster.
* GPU Detail Dashboard: Offers a detailed look at individual GPUs.
* Job Detail Dashboard: Presents detailed GPU usage for specific jobs in SLURM and Kubernetes environments.
* Node Detail Dashboard: Displays detailed GPU usage at the host level.

## Contributing

Please refer to our [Developer Guide](https://instinct.docs.amd.com/projects/gpu-operator/en/main/contributing/developer-guide.html).

## Support

For bugs and feature requests, please file an issue on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page.

## License

The AMD GPU Operator is licensed under the [Apache License 2.0](LICENSE).

## gpu-operator-charts

![Version: v1.4.0](https://img.shields.io/badge/Version-v1.4.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v1.4.0](https://img.shields.io/badge/AppVersion-v1.4.0-informational?style=flat-square)

AMD GPU Operator simplifies the deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters.

**Homepage:** <https://github.com/ROCm/gpu-operator>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Yan Sun <Yan.Sun3@amd.com> |  |  |

## Source Code

* <https://github.com/ROCm/gpu-operator>

## Requirements

Kubernetes: `>= 1.29.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://./charts/kmm | kmm | v1.0.0 |
| file://./charts/remediation | remediation-controller | v1.0.0 |
| https://kubernetes-sigs.github.io/node-feature-discovery/charts | node-feature-discovery | v0.16.1 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]},"weight":1}]}}` | Deployment affinity configs for controller manager |
| controllerManager.manager.image.repository | string | `"docker.io/rocm/gpu-operator"` | AMD GPU operator controller manager image repository |
| controllerManager.manager.image.tag | string | `"dev"` | AMD GPU operator controller manager image tag |
| controllerManager.manager.imagePullPolicy | string | `"Always"` | Image pull policy for AMD GPU operator controller manager pod |
| controllerManager.manager.imagePullSecrets | string | `""` | Image pull secret name for pulling AMD GPU operator controller manager image if registry needs credential to pull image |
| controllerManager.nodeSelector | object | `{}` | Node selector for AMD GPU operator controller manager deployment |
| crds.defaultCR.install | bool | `true` | Deploy default DeviceConfig during helm chart installation |
| crds.defaultCR.upgrade | bool | `false` | Deploy / Patch default DeviceConfig during helm chart upgrade. Be careful about this option: 1. Your customized change on default DeviceConfig may be overwritten 2. Your existing DeviceConfig may conflict with upgraded default DeviceConfig  |
| deviceConfig.spec.commonConfig.initContainerImage | string | `"busybox:1.36"` | init container image |
| deviceConfig.spec.commonConfig.utilsContainer.image | string | `"docker.io/rocm/gpu-operator-utils:v1.4.0"` | gpu operator utility container image |
| deviceConfig.spec.commonConfig.utilsContainer.imagePullPolicy | string | `"IfNotPresent"` | utility container image pull policy |
| deviceConfig.spec.commonConfig.utilsContainer.imageRegistrySecret | object | `{}` | utility container image pull secret, e.g. {"name": "mySecretName"} |
| deviceConfig.spec.configManager.config | object | `{}` | config map for config manager, e.g. {"name": "myConfigMap"} |
| deviceConfig.spec.configManager.configManagerTolerations | list | `[]` | config manager tolerations |
| deviceConfig.spec.configManager.enable | bool | `false` | enable/disable the config manager  |
| deviceConfig.spec.configManager.image | string | `"rocm/device-config-manager:v1.4.0"` | config manager image |
| deviceConfig.spec.configManager.imagePullPolicy | string | `"IfNotPresent"` | image pull policy for config manager image |
| deviceConfig.spec.configManager.imageRegistrySecret | object | `{}` | image pull secret for config manager image, e.g. {"name": "myPullSecret"} |
| deviceConfig.spec.configManager.selector | object | `{}` | node selector for config manager, if not specified it will reuse spec.selector |
| deviceConfig.spec.configManager.upgradePolicy.maxUnavailable | int | `1` | the maximum number of Pods that can be unavailable during the update process |
| deviceConfig.spec.configManager.upgradePolicy.upgradeStrategy | string | `"RollingUpdate"` | the type of daemonset upgrade, RollingUpdate or OnDelete |
| deviceConfig.spec.devicePlugin.devicePluginArguments | object | `{}` | pass supported flags and their values while starting device plugin daemonset, e.g. {"resource_naming_strategy": "single"} or {"resource_naming_strategy": "mixed"} |
| deviceConfig.spec.devicePlugin.devicePluginImage | string | `"rocm/k8s-device-plugin:latest"` | device plugin image |
| deviceConfig.spec.devicePlugin.devicePluginImagePullPolicy | string | `"IfNotPresent"` | device plugin image pull policy |
| deviceConfig.spec.devicePlugin.devicePluginTolerations | list | `[]` | device plugin tolerations |
| deviceConfig.spec.devicePlugin.enableNodeLabeller | bool | `true` | enable / disable node labeller |
| deviceConfig.spec.devicePlugin.imageRegistrySecret | object | `{}` | image pull secret for device plugin and node labeller, e.g. {"name": "mySecretName"} |
| deviceConfig.spec.devicePlugin.nodeLabellerArguments | list | `[]` | pass supported labels while starting node labeller daemonset, default ["vram", "cu-count", "simd-count", "device-id", "family", "product-name", "driver-version"], also support ["compute-memory-partition", "compute-partitioning-supported", "memory-partitioning-supported"] |
| deviceConfig.spec.devicePlugin.nodeLabellerImage | string | `"rocm/k8s-device-plugin:labeller-latest"` | node labeller image |
| deviceConfig.spec.devicePlugin.nodeLabellerImagePullPolicy | string | `"IfNotPresent"` | node labeller image pull policy |
| deviceConfig.spec.devicePlugin.nodeLabellerTolerations | list | `[]` | node labeller tolerations |
| deviceConfig.spec.devicePlugin.upgradePolicy.maxUnavailable | int | `1` | the maximum number of Pods that can be unavailable during the update process |
| deviceConfig.spec.devicePlugin.upgradePolicy.upgradeStrategy | string | `"RollingUpdate"` | the type of daemonset upgrade, RollingUpdate or OnDelete |
| deviceConfig.spec.driver.blacklist | bool | `false` | enable/disable putting a blacklist amdgpu entry in modprobe config, which requires node labeller to run |
| deviceConfig.spec.driver.enable | bool | `false` | enable/disable out-of-tree driver management, set to false to use inbox driver |
| deviceConfig.spec.driver.image | string | `"docker.io/myUserName/driverImage"` | image repository to store out-of-tree driver image, DO NOT put image tag since operator automatically manage it for users |
| deviceConfig.spec.driver.imageBuild | object | `{}` | configure the out-of-tree driver image build within the cluster. e.g. {"baseImageRegistry":"docker.io","baseImageRegistryTLS":{"baseImageRegistry":"docker.io","baseImageRegistryTLS":{"insecure":"false","insecureSkipTLSVerify":"false"}}} |
| deviceConfig.spec.driver.imageRegistrySecret | object | `{}` | image pull secret for pull/push access of the driver image repository, input secret name like {"name": "mysecret"} |
| deviceConfig.spec.driver.imageRegistryTLS.insecure | bool | `false` | set to true to use plain HTTP for driver image repository |
| deviceConfig.spec.driver.imageRegistryTLS.insecureSkipTLSVerify | bool | `false` | set to true to skip TLS validation for driver image repository |
| deviceConfig.spec.driver.imageSign | object | `{}` | specify the secrets to sign the out-of-tree kernel module inside driver image for secure boot, e.g. input private / public key secret {"keySecret":{"name":"privateKeySecret"},"certSecret":{"name":"publicKeySecret"}} |
| deviceConfig.spec.driver.tolerations | list | `[]` | configure driver tolerations so that operator can manage out-of-tree drivers on tainted nodes |
| deviceConfig.spec.driver.upgradePolicy.enable | bool | `true` | enable/disable automatic driver upgrade feature  |
| deviceConfig.spec.driver.upgradePolicy.maxParallelUpgrades | int | `3` | how many nodes can be upgraded in parallel |
| deviceConfig.spec.driver.upgradePolicy.maxUnavailableNodes | string | `"25%"` | maximum number of nodes that can be in a failed upgrade state beyond which upgrades will stop to keep cluster at a minimal healthy state |
| deviceConfig.spec.driver.upgradePolicy.nodeDrainPolicy.force | bool | `true` | whether force draining is allowed or not |
| deviceConfig.spec.driver.upgradePolicy.nodeDrainPolicy.gracePeriodSeconds | int | `-1` | the time kubernetes waits for a pod to shut down gracefully after receiving a termination signal, zero means immediate, minus value means follow pod defined grace period |
| deviceConfig.spec.driver.upgradePolicy.nodeDrainPolicy.timeoutSeconds | int | `300` | the length of time in seconds to wait before giving up drain, zero means infinite |
| deviceConfig.spec.driver.upgradePolicy.podDeletionPolicy.force | bool | `true` | whether force deletion is allowed or not |
| deviceConfig.spec.driver.upgradePolicy.podDeletionPolicy.gracePeriodSeconds | int | `-1` | the time kubernetes waits for a pod to shut down gracefully after receiving a termination signal, zero means immediate, minus value means follow pod defined grace period |
| deviceConfig.spec.driver.upgradePolicy.podDeletionPolicy.timeoutSeconds | int | `300` | the length of time in seconds to wait before giving up on pod deletion, zero means infinite |
| deviceConfig.spec.driver.upgradePolicy.rebootRequired | bool | `true` | whether reboot each worker node or not during the driver upgrade |
| deviceConfig.spec.driver.version | string | `"6.4"` | specify an out-of-tree driver version to install |
| deviceConfig.spec.metricsExporter.config | object | `{}` | name of the metrics exporter config map, e.g. {"name": "metricConfigMapName"} |
| deviceConfig.spec.metricsExporter.enable | bool | `true` | enable / disable device metrics exporter |
| deviceConfig.spec.metricsExporter.image | string | `"rocm/device-metrics-exporter:v1.4.0"` | metrics exporter image |
| deviceConfig.spec.metricsExporter.imagePullPolicy | string | `"IfNotPresent"` | metrics exporter image pull policy |
| deviceConfig.spec.metricsExporter.imageRegistrySecret | object | `{}` | metrics exporter image pull secret, e.g. {"name": "pullSecretName"} |
| deviceConfig.spec.metricsExporter.nodePort | int | `32500` | external port for pulling metrics from outside the cluster for NodePort service, in the range 30000-32767 (assigned automatically by default) |
| deviceConfig.spec.metricsExporter.port | int | `5000` | internal port used for in-cluster and node access to pull metrics from the metrics-exporter (default 5000). |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.attachMetadata | object | `{}` | define if Prometheus should attach node metadata to the target, e.g. {"node": "true"} |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.authorization | object | `{}` | optional Prometheus authorization configuration for accessing the endpoint |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.enable | bool | `false` | enable or disable ServiceMonitor creation |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.honorLabels | bool | `true` | choose the metric's labels on collisions with target labels |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.honorTimestamps | bool | `false` | control whether the scrape endpoints honor timestamps |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.interval | string | `"30s"` | frequency to scrape metrics. Accepts values with time unit suffix: "30s", "1m", "2h", "500ms" |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.labels | object | `{}` | additional labels to add to the ServiceMonitor |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.metricRelabelings | list | `[]` | relabeling rules applied to individual scraped metrics |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.relabelings | list | `[]` | relabelConfigs to apply to samples before ingestion |
| deviceConfig.spec.metricsExporter.prometheus.serviceMonitor.tlsConfig | object | `{}` | TLS settings used by Prometheus to connect to the metrics endpoint |
| deviceConfig.spec.metricsExporter.rbacConfig.clientCAConfigMap | object | `{}` | reference to a configmap containing the client CA (key: ca.crt) for mTLS client validation, e.g. {"name": "configMapName"} |
| deviceConfig.spec.metricsExporter.rbacConfig.disableHttps | bool | `false` | disable https protecting the proxy endpoint |
| deviceConfig.spec.metricsExporter.rbacConfig.enable | bool | `false` | enable/disable kube rbac proxy |
| deviceConfig.spec.metricsExporter.rbacConfig.image | string | `"quay.io/brancz/kube-rbac-proxy:v0.18.1"` | kube rbac proxy side car container image |
| deviceConfig.spec.metricsExporter.rbacConfig.secret | object | `{}` | certificate secret to mount in kube-rbac container for TLS, self signed certificates will be generated by default, e.g. {"name": "secretName"} |
| deviceConfig.spec.metricsExporter.rbacConfig.staticAuthorization.clientName | string | `""` | expected CN (Common Name) from client cert (e.g., Prometheus SA identity) |
| deviceConfig.spec.metricsExporter.rbacConfig.staticAuthorization.enable | bool | `false` | enables static authorization using client certificate CN |
| deviceConfig.spec.metricsExporter.selector | object | `{}` | metrics exporter node selector, if not specified it will reuse spec.selector |
| deviceConfig.spec.metricsExporter.serviceType | string | `"ClusterIP"` | type of service for exposing metrics endpoint, ClusterIP or NodePort |
| deviceConfig.spec.metricsExporter.tolerations | list | `[]` | metrics exporter tolerations |
| deviceConfig.spec.metricsExporter.upgradePolicy.maxUnavailable | int | `1` | the maximum number of Pods that can be unavailable during the update process |
| deviceConfig.spec.metricsExporter.upgradePolicy.upgradeStrategy | string | `"RollingUpdate"` | the type of daemonset upgrade, RollingUpdate or OnDelete |
| deviceConfig.spec.selector | object | `{"feature.node.kubernetes.io/amd-gpu":"true"}` | Set node selector for the default DeviceConfig |
| deviceConfig.spec.testRunner.config | object | `{}` | test runner config map, e.g. {"name": "myConfigMap"} |
| deviceConfig.spec.testRunner.enable | bool | `false` | enable / disable test runner |
| deviceConfig.spec.testRunner.image | string | `"docker.io/rocm/test-runner:v1.4.0"` | test runner image |
| deviceConfig.spec.testRunner.imagePullPolicy | string | `"IfNotPresent"` | test runner image pull policy |
| deviceConfig.spec.testRunner.imageRegistrySecret | object | `{}` | test runner image pull secret |
| deviceConfig.spec.testRunner.logsLocation.hostPath | string | `"/var/log/amd-test-runner"` | host directory to save test run logs |
| deviceConfig.spec.testRunner.logsLocation.logsExportSecrets | list | `[]` | a list of secrets that contain connectivity info to multiple cloud providers |
| deviceConfig.spec.testRunner.logsLocation.mountPath | string | `"/var/log/amd-test-runner"` | test runner internal mounted directory to save test run logs |
| deviceConfig.spec.testRunner.selector | object | `{}` | test runner node selector, if not specified it will reuse spec.selector |
| deviceConfig.spec.testRunner.tolerations | list | `[]` | test runner tolerations |
| deviceConfig.spec.testRunner.upgradePolicy.maxUnavailable | int | `1` | the maximum number of Pods that can be unavailable during the update process |
| deviceConfig.spec.testRunner.upgradePolicy.upgradeStrategy | string | `"RollingUpdate"` | the type of daemonset upgrade, RollingUpdate or OnDelete |
| installdefaultNFDRule | bool | `true` | Default NFD rule will detect amd gpu based on pci vendor ID |
| kmm.enabled | bool | `true` | Set to true/false to enable/disable the installation of kernel module management (KMM) operator |
| node-feature-discovery.enabled | bool | `true` | Set to true/false to enable/disable the installation of node feature discovery (NFD) operator |
| node-feature-discovery.worker.nodeSelector | object | `{}` | Set nodeSelector for NFD worker daemonset |
| node-feature-discovery.worker.tolerations | list | `[{"effect":"NoExecute","key":"amd-dcm","operator":"Equal","value":"up"},{"effect":"NoSchedule","key":"amd-gpu-unhealthy","operator":"Exists"}]` | Set tolerations for NFD worker daemonset |
| remediation.enabled | bool | `true` | Set to true/false to enable/disable the installation of remediation workflow controller |
| upgradeCRD | bool | `true` | CRD will be patched as pre-upgrade/pre-rollback hook when doing helm upgrade/rollback to current helm chart |
| kmm.controller.affinity | object | `{"nodeAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"preference":{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]},"weight":1}]}}` | Affinity for the KMM controller manager deployment |
| kmm.controller.manager.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.controller.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.controller.manager.env.relatedImageBuild | string | `"gcr.io/kaniko-project/executor:v1.23.2"` | KMM kaniko builder image for building driver image within cluster |
| kmm.controller.manager.env.relatedImageBuildPullSecret | string | `""` | Image pull secret name for pulling KMM kaniko builder image if registry needs credential to pull image |
| kmm.controller.manager.env.relatedImageSign | string | `"docker.io/rocm/kernel-module-management-signimage:latest"` | KMM signer image for signing driver image's kernel module with given key pairs within cluster |
| kmm.controller.manager.env.relatedImageSignPullSecret | string | `""` | Image pull secret name for pulling KMM signer image if registry needs credential to pull image |
| kmm.controller.manager.env.relatedImageWorker | string | `"docker.io/rocm/kernel-module-management-worker:latest"` | KMM worker image for loading / unloading driver kernel module on worker nodes |
| kmm.controller.manager.env.relatedImageWorkerPullSecret | string | `""` | Image pull secret name for pulling KMM worker image if registry needs credential to pull image |
| kmm.controller.manager.image.repository | string | `"docker.io/rocm/kernel-module-management-operator"` | KMM controller manager image repository |
| kmm.controller.manager.image.tag | string | `"latest"` | KMM controller manager image tag |
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
| kmm.webhookServer.nodeSelector | object | `{}` | KMM webhook's deployment node selector |
| kmm.webhookServer.replicas | int | `1` |  |
| kmm.webhookServer.webhookServer.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.webhookServer.webhookServer.args[1] | string | `"--enable-module"` |  |
| kmm.webhookServer.webhookServer.args[2] | string | `"--enable-namespace"` |  |
| kmm.webhookServer.webhookServer.args[3] | string | `"--enable-preflightvalidation"` |  |
| kmm.webhookServer.webhookServer.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.webhookServer.webhookServer.image.repository | string | `"docker.io/rocm/kernel-module-management-webhook-server"` | KMM webhook image repository |
| kmm.webhookServer.webhookServer.image.tag | string | `"latest"` | KMM webhook image tag |
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
| remediation-controller.controller.image | string | `"quay.io/argoproj/workflow-controller:v3.6.5"` |  |

