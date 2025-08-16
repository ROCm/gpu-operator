# AMD GPU Operator Documentation

The AMD GPU Operator simplifies the deployment and management of AMD Instinct GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

## Features

- Automated driver installation and management
- Easy deployment of the AMD GPU device plugin
- Metrics collection and export
- Support for both vanilla Kubernetes and OpenShift environments
- Simplified GPU resource allocation for containers
- Automatic worker node labeling for GPU-enabled nodes
- GPU health monitoring and troubleshooting

## Compatibility

### Supported Hardware

| **GPUs** | |
| --- | --- |
| AMD Instinct™ MI355X | ✅ Supported |
| AMD Instinct™ MI350X | ✅ Supported |
| AMD Instinct™ MI325X | ✅ Supported |
| AMD Instinct™ MI300X | ✅ Supported |
| AMD Instinct™ MI250 | ✅ Supported |
| AMD Instinct™ MI210 | ✅ Supported |

### OS & Platform Support Matrix

Below is a matrix of supported Operating systems and the corresponding Kubernetes version that have been validated to work. We will continue to add more Operating Systems and future versions of Kubernetes with each release of the AMD GPU Operator and Metrics Exporter.

<table style="border-collapse: collapse; margin-left: 0; margin-right: auto;">
  <thead style="background-color: #2c2c2c; color: white;">
    <tr>
      <th style="border: 1px solid grey;">Operating System</th>
      <th style="border: 1px solid grey;">Kubernetes</th>
      <th style="border: 1px solid grey;">Red Hat OpenShift</th>
    </tr>
  </thead>
  <tbody>
    <tr style="background-color: white; color: black;">
      <td style="background-color: #2c2c2c; color: white; border: 1px solid grey;">Ubuntu 22.04 LTS</td>
      <td style="border: 1px solid grey;">1.29—1.33</td>
      <td style="border: 1px solid grey;"></td>
    </tr>
    <tr style="background-color: lightgrey; color: black;">
      <td style="background-color: #2c2c2c; color: white; border: 1px solid grey;">Ubuntu 24.04 LTS</td>
      <td style="border: 1px solid grey;">1.29—1.33</td>
      <td style="border: 1px solid grey;"></td>
    </tr>
    <tr style="background-color: white; color: black;">
      <td style="background-color: #2c2c2c; color: white; border: 1px solid grey;">Red Hat Core OS (RHCOS)</td>
      <td style="border: 1px solid grey;"></td>
      <td style="border: 1px solid grey;">4.16—4.19</td>
    </tr>
  </tbody>
</table>


Please refer to the [ROCM documentation](https://rocm.docs.amd.com/en/latest/compatibility/compatibility-matrix.html) for the compatibility matrix for the AMD GPU DKMS driver.

## Prerequisites

- Helm v3.2.0+
- `kubectl` or `oc` CLI tool configured to access your cluster

## Support

For bugs and feature requests, please file an issue on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page.
