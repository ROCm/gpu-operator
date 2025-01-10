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

Add the Helm repository:
```bash
helm repo add rocm https://rocm.github.io/gpu-operator
helm repo update
```

Install the AMD GPU Operator:
```bash
helm install amd-gpu-operator rocm/gpu-operator-charts --namespace kube-amd-gpu --create-namespace
```

### Grafana Dashboards

Following dashboards are provided for visualizing GPU metrics collected from device-metrics-exporter:

* Overview Dashboard: Provides a comprehensive view of the GPU cluster.
* GPU Detail Dashboard: Offers a detailed look at individual GPUs.
* Job Detail Dashboard: Presents detailed GPU usage for specific jobs in SLURM and Kubernetes environments.
* Node Detail Dashboard: Displays detailed GPU usage at the host level.

## Support

For bugs and feature requests, please file an issue on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page.

## License

The AMD GPU Operator is licensed under the [Apache License 2.0](LICENSE).
