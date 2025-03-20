# AMD GPU Operator

:book: GPU Operator Documentation Site: https://instinct.docs.amd.com/projects/gpu-operator

## Introduction

AMD GPU Operator simplifies the deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

## Components

* AMD GPU Operator Controller
* K8s Device Plugin
* K8s Node Labeller
* Device Metrics Exporter
* Node Feature Discovery Operator
* Kernel Module Management Operator

## Features

* Streamlined GPU driver installation and management
* Comprehensive metrics collection and export
* Easy deployment of AMD GPU device plugin for Kubernetes
* Automated labeling of nodes with AMD GPU capabilities
* Compatibility with standard Kubernetes environments
* Efficient GPU resource allocation for containerized workloads

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

Basic installation:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  --version=v1.0.0
```

```{note}
Installation Options
  - Skip NFD installation: `--set node-feature-discovery.enabled=false`
  - Skip KMM installation: `--set kmm.enabled=false`
```

```{warning}
  It is strongly recommended to use AMD-optimized KMM images included in the operator release.
```

### 3. Install Custom Resource

After the installation of AMD GPU Operator, you need to create the `DeviceConfig` custom resource in order to trigger the operator to start to work. By preparing the `DeviceConfig` in the YAML file, you can create the resouce by running ```kubectl apply -f deviceconfigs.yaml```. For custom resource definition and more detailed information, please refer to [Custom Resource Installation Guide](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/installation/kubernetes-helm.html#install-custom-resource).

### Grafana Dashboards

Following dashboards are provided for visualizing GPU metrics collected from device-metrics-exporter:

* Overview Dashboard: Provides a comprehensive view of the GPU cluster.
* GPU Detail Dashboard: Offers a detailed look at individual GPUs.
* Job Detail Dashboard: Presents detailed GPU usage for specific jobs in SLURM and Kubernetes environments.
* Node Detail Dashboard: Displays detailed GPU usage at the host level.

## Contributing

Please refer to our [Developer Guide](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/contributing/developer-guide.html).

## Support

For bugs and feature requests, please file an issue on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page.

## License

The AMD GPU Operator is licensed under the [Apache License 2.0](LICENSE).

## gpu-operator-charts

![Version: v1.0.0](https://img.shields.io/badge/Version-v1.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v1.0.0](https://img.shields.io/badge/AppVersion-v1.0.0-informational?style=flat-square)

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
| https://kubernetes-sigs.github.io/node-feature-discovery/charts | node-feature-discovery | v0.16.1 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| kmm.controller.manager.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.controller.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.controller.manager.env.relatedImageBuild | string | `"gcr.io/kaniko-project/executor:v1.23.2"` |  |
| kmm.controller.manager.env.relatedImageBuildPullSecret | string | `""` |  |
| kmm.controller.manager.env.relatedImageSign | string | `"docker.io/rocm/kernel-module-management-signimage:v1.0.0"` |  |
| kmm.controller.manager.env.relatedImageSignPullSecret | string | `""` |  |
| kmm.controller.manager.env.relatedImageWorker | string | `"docker.io/rocm/kernel-module-management-worker:v1.0.0"` |  |
| kmm.controller.manager.env.relatedImageWorkerPullSecret | string | `""` |  |
| kmm.controller.manager.image.repository | string | `"docker.io/rocm/kernel-module-management-operator"` |  |
| kmm.controller.manager.image.tag | string | `"v1.0.0"` |  |
| kmm.controller.manager.imagePullPolicy | string | `"Always"` |  |
| kmm.controller.manager.imagePullSecrets | string | `""` |  |
| kmm.controller.manager.resources.limits.cpu | string | `"500m"` |  |
| kmm.controller.manager.resources.limits.memory | string | `"384Mi"` |  |
| kmm.controller.manager.resources.requests.cpu | string | `"10m"` |  |
| kmm.controller.manager.resources.requests.memory | string | `"64Mi"` |  |
| kmm.controller.nodeAffinity.nodeSelectorTerms[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kmm.controller.nodeAffinity.nodeSelectorTerms[0].operator | string | `"Exists"` |  |
| kmm.controller.nodeAffinity.nodeSelectorTerms[1].key | string | `"node-role.kubernetes.io/master"` |  |
| kmm.controller.nodeAffinity.nodeSelectorTerms[1].operator | string | `"Exists"` |  |
| kmm.controller.nodeSelector | object | `{}` |  |
| kmm.controller.replicas | int | `1` |  |
| kmm.controller.serviceAccount.annotations | object | `{}` |  |
| kmm.controllerMetricsService.ports[0].name | string | `"https"` |  |
| kmm.controllerMetricsService.ports[0].port | int | `8443` |  |
| kmm.controllerMetricsService.ports[0].protocol | string | `"TCP"` |  |
| kmm.controllerMetricsService.ports[0].targetPort | string | `"https"` |  |
| kmm.controllerMetricsService.type | string | `"ClusterIP"` |  |
| kmm.kubernetesClusterDomain | string | `"cluster.local"` |  |
| kmm.managerConfig.controllerConfigYaml | string | `"healthProbeBindAddress: :8081\nwebhookPort: 9443\nleaderElection:\n  enabled: true\n  resourceID: kmm.sigs.x-k8s.io\nmetrics:\n  enableAuthnAuthz: true\n  bindAddress: 0.0.0.0:8443\n  secureServing: true\nworker:\n  runAsUser: 0\n  seLinuxType: spc_t\n  firmwareHostPath: /var/lib/firmware"` |  |
| kmm.webhookServer.nodeAffinity.nodeSelectorTerms[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kmm.webhookServer.nodeAffinity.nodeSelectorTerms[0].operator | string | `"Exists"` |  |
| kmm.webhookServer.nodeAffinity.nodeSelectorTerms[1].key | string | `"node-role.kubernetes.io/master"` |  |
| kmm.webhookServer.nodeAffinity.nodeSelectorTerms[1].operator | string | `"Exists"` |  |
| kmm.webhookServer.nodeSelector | object | `{}` |  |
| kmm.webhookServer.replicas | int | `1` |  |
| kmm.webhookServer.webhookServer.args[0] | string | `"--config=controller_config.yaml"` |  |
| kmm.webhookServer.webhookServer.args[1] | string | `"--enable-module"` |  |
| kmm.webhookServer.webhookServer.args[2] | string | `"--enable-namespace"` |  |
| kmm.webhookServer.webhookServer.args[3] | string | `"--enable-preflightvalidation"` |  |
| kmm.webhookServer.webhookServer.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| kmm.webhookServer.webhookServer.image.repository | string | `"docker.io/rocm/kernel-module-management-webhook-server"` |  |
| kmm.webhookServer.webhookServer.image.tag | string | `"v1.0.0"` |  |
| kmm.webhookServer.webhookServer.imagePullPolicy | string | `"Always"` |  |
| kmm.webhookServer.webhookServer.imagePullSecrets | string | `""` |  |
| kmm.webhookServer.webhookServer.resources.limits.cpu | string | `"500m"` |  |
| kmm.webhookServer.webhookServer.resources.limits.memory | string | `"384Mi"` |  |
| kmm.webhookServer.webhookServer.resources.requests.cpu | string | `"10m"` |  |
| kmm.webhookServer.webhookServer.resources.requests.memory | string | `"64Mi"` |  |
| kmm.webhookService.ports[0].port | int | `443` |  |
| kmm.webhookService.ports[0].protocol | string | `"TCP"` |  |
| kmm.webhookService.ports[0].targetPort | int | `9443` |  |
| kmm.webhookService.type | string | `"ClusterIP"` |  |
