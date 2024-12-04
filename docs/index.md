# AMD GPU Operator Documentation

The AMD GPU Operator simplifies the deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

## Features

- Automated driver installation and management
- Easy deployment of the AMD GPU device plugin
- Metrics collection and export
- Support for Vanilla Kubernetes
- Simplified GPU resource allocation for containers
- Automatic worker node labeling for GPU-enabled nodes

## Compatibility

- **Kubernetes**: 1.29.0
- Please refer to the [ROCm documentation](https://rocm.docs.amd.com/en/latest/compatibility/compatibility-matrix.html) for the compatibility matrix for the AMD GPU DKMS driver.

## Prerequisites

- Helm v3.2.0+
- `kubectl` CLI tool configured to access your cluster

## Quick Start

- Add the Helm repository:

```bash
helm repo add rocm https://rocm.github.io/gpu-operator
helm repo update
```

- Install the AMD GPU Operator:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts --namespace kube-amd-gpu --create-namespace
```

- Verify the installation:

```bash
kubectl get pods -n kube-amd-gpu
```

## Support

For bugs and feature requests, please file an issue on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page.
