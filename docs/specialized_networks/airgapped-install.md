# Air-gapped Installation Guide

This guide explains how to install the AMD GPU Operator in an air-gapped environment where the Kubernetes cluster has no external network connectivity.

## Prerequisites

- Kubernetes v1.29.0+
- Helm v3.2.0+
- Access to an internal container registry

### Required Images

The following images must be mirrored to your internal registry:

```bash
# Core Operator Images
docker.io/rocm/gpu-operator:<version>
docker.io/rocm/gpu-operator-utils:<version>

# Device Plugin Images
docker.io/rocm/k8s-device-plugin:latest
docker.io/rocm/k8s-node-labeller:latest

# Metrics and Testing Images
docker.io/rocm/device-metrics-exporter:<version>
docker.io/rocm/test-runner:<version>
docker.io/rocm/device-config-manager:<version>

# Kernel Module Management (KMM) Images
docker.io/rocm/kernel-module-management-operator:<version>
docker.io/rocm/kernel-module-management-webhook-server:<version>
docker.io/rocm/kernel-module-management-worker:<version>
docker.io/rocm/kernel-module-management-signimage:<version>

# Build and Base Images
gcr.io/kaniko-project/executor:v1.23.2
docker.io/ubuntu:<Ubuntu OS version>
docker.io/busybox:1.36

# Node Feature Discovery
registry.k8s.io/nfd/node-feature-discovery:v0.18.3

# Cert-Manager Images
quay.io/jetstack/cert-manager-controller:v1.15.1
quay.io/jetstack/cert-manager-webhook:v1.15.1
quay.io/jetstack/cert-manager-cainjector:v1.15.1
quay.io/jetstack/cert-manager-acmesolver:v1.15.1

# Argo workflow controller image (for Auto Node Remediation)
quay.io/argoproj/workflow-controller:v4.0.3
```

### Required RPM/DEB Packages

For driver compilation, ensure these packages are available in your internal package repository:

#### RHEL/CentOS

```bash
kernel-devel
kernel-headers
gcc
make
elfutils-libelf-devel
```

#### Ubuntu

```bash
linux-headers-$(uname -r)
build-essential
```

## Installation Steps

### 1. Mirror Required Images

On a connected system, run the following script to pull, tag, and push all required images:

```bash
#!/bin/bash
# Configuration - Update these variables according to your environment
INTERNAL_REGISTRY="internal-registry.example.com"
OPERATOR_VERSION="v1.4.1"  # GPU operator version, e.g., "v1.5.0"
UBUNTU_VERSION="22.04"  # e.g., "22.04"
KANIKO_VERSION="v1.23.2"
NFD_VERSION="v0.18.3"
CERT_MANAGER_VERSION="v1.15.1"
BUSYBOX_VERSION="1.36"

# Operator images with version tag
OPERATOR_VERSIONED_IMAGES=(
  "gpu-operator"
  "gpu-operator-utils"
  "k8s-device-plugin"
  "device-metrics-exporter"
  "test-runner"
  "device-config-manager"
  "kernel-module-management-operator"
  "kernel-module-management-webhook-server"
  "kernel-module-management-worker"
  "kernel-module-management-signimage"
)

# Operator images with latest tag
ROCM_LATEST_IMAGES=(
  "k8s-device-plugin:latest"
  "k8s-node-labeller:latest"
)

# Pull, tag, and push ROCm versioned images
for img in "${OPERATOR_VERSIONED_IMAGES[@]}"; do
  docker pull docker.io/rocm/${img}:${OPERATOR_VERSION}
  docker tag rocm/${img}:${OPERATOR_VERSION} ${INTERNAL_REGISTRY}/rocm/${img}:${OPERATOR_VERSION}
  docker push ${INTERNAL_REGISTRY}/rocm/${img}:${OPERATOR_VERSION}
done

# Pull, tag, and push ROCm latest images
for img in "${ROCM_LATEST_IMAGES[@]}"; do
  docker pull docker.io/rocm/${img}
  docker tag rocm/${img} ${INTERNAL_REGISTRY}/rocm/${img}
  docker push ${INTERNAL_REGISTRY}/rocm/${img}
done

# Third-party images (kaniko, ubuntu, busybox, NFD)
docker pull gcr.io/kaniko-project/executor:${KANIKO_VERSION}
docker tag gcr.io/kaniko-project/executor:${KANIKO_VERSION} ${INTERNAL_REGISTRY}/kaniko-project/executor:${KANIKO_VERSION}
docker push ${INTERNAL_REGISTRY}/kaniko-project/executor:${KANIKO_VERSION}

docker pull docker.io/ubuntu:${UBUNTU_VERSION}
docker tag ubuntu:${UBUNTU_VERSION} ${INTERNAL_REGISTRY}/ubuntu:${UBUNTU_VERSION}
docker push ${INTERNAL_REGISTRY}/ubuntu:${UBUNTU_VERSION}

docker pull docker.io/busybox:${BUSYBOX_VERSION}
docker tag busybox:${BUSYBOX_VERSION} ${INTERNAL_REGISTRY}/busybox:${BUSYBOX_VERSION}
docker push ${INTERNAL_REGISTRY}/busybox:${BUSYBOX_VERSION}

docker pull registry.k8s.io/nfd/node-feature-discovery:${NFD_VERSION}
docker tag registry.k8s.io/nfd/node-feature-discovery:${NFD_VERSION} ${INTERNAL_REGISTRY}/nfd/node-feature-discovery:${NFD_VERSION}
docker push ${INTERNAL_REGISTRY}/nfd/node-feature-discovery:${NFD_VERSION}

# Cert-manager images
CERT_MANAGER_IMAGES=(
  "cert-manager-controller"
  "cert-manager-webhook"
  "cert-manager-cainjector"
  "cert-manager-acmesolver"
)

for img in "${CERT_MANAGER_IMAGES[@]}"; do
  docker pull quay.io/jetstack/${img}:${CERT_MANAGER_VERSION}
  docker tag quay.io/jetstack/${img}:${CERT_MANAGER_VERSION} ${INTERNAL_REGISTRY}/jetstack/${img}:${CERT_MANAGER_VERSION}
  docker push ${INTERNAL_REGISTRY}/jetstack/${img}:${CERT_MANAGER_VERSION}
done
```

### 2. Configure Internal Package Repository

1. Create an internal package repository mirror containing required build packages
2. Configure worker nodes to use the internal repository
3. Verify package availability:

```bash
# RHEL/CentOS
yum list kernel-devel kernel-headers gcc make elfutils-libelf-devel

# Ubuntu
apt list linux-headers-$(uname -r) build-essential
```

### 3. Install Cert-Manager

- Download the cert-manager helm chart on a connected system and transfer it to your air-gapped environment

- Install cert-manager using internal images:

```bash
helm install cert-manager ./cert-manager-v1.15.1.tgz \
  --namespace cert-manager \
  --create-namespace \
  --set installCRDs=true \
  --set image.repository=internal-registry.example.com/jetstack/cert-manager-controller \
  --set webhook.image.repository=internal-registry.example.com/jetstack/cert-manager-webhook \
  --set cainjector.image.repository=internal-registry.example.com/jetstack/cert-manager-cainjector \
  --set acmesolver.image.repository=internal-registry.example.com/jetstack/cert-manager-acmesolver
```

### 4. Install AMD GPU Operator

- Download the GPU operator helm chart on a connected system and transfer it to your air-gapped environment

- Create custom values file for the operator:

```yaml
# operator-values.yaml
controllerManager:
  manager:
    image:
      repository: internal-registry.example.com/rocm/gpu-operator
      tag: <version>
    imagePullPolicy: IfNotPresent

deviceConfig:
  spec:
    driver:
      # enable this section if you want operator to build the driver
      # enable: true
      #image: internal-registry.example.com/rocm/driver-image
      #version: <amdgpu driver version>
    commonConfig:
      initContainerImage: internal-registry.example.com/busybox:1.36
      utilsContainer:
        image: internal-registry.example.com/rocm/gpu-operator-utils:<version>
    devicePlugin:
      devicePluginImage: internal-registry.example.com/rocm/k8s-device-plugin:<version>
      nodeLabellerImage: internal-registry.example.com/rocm/k8s-device-plugin:labeller-<version>
    metricsExporter:
      image: internal-registry.example.com/rocm/device-metrics-exporter:<version>
    testRunner:
      image: internal-registry.example.com/rocm/test-runner:<version>
    configManager:
      image: internal-registry.example.com/rocm/device-config-manager:<version>

# NFD image configuration
node-feature-discovery:
  image:
    repository: internal-registry.example.com/nfd/node-feature-discovery
    tag: v0.18.3

# KMM (Kernel Module Management) image configuration
kmm:
  controller:
    manager:
      image:
        repository: internal-registry.example.com/rocm/kernel-module-management-operator
        tag: <version>
      imagePullPolicy: IfNotPresent
      env:
        relatedImageBuild: internal-registry.example.com/kaniko-project/executor:v1.23.2
        relatedImageSign: internal-registry.example.com/rocm/kernel-module-management-signimage:<version>
        relatedImageWorker: internal-registry.example.com/rocm/kernel-module-management-worker:<version>
  webhookServer:
    webhookServer:
      image:
        repository: internal-registry.example.com/rocm/kernel-module-management-webhook-server
        tag: <version>
      imagePullPolicy: IfNotPresent
```

- Install the operator:

```bash
helm install amd-gpu-operator ./gpu-operator-<version>.tgz \
  --namespace kube-amd-gpu \
  --create-namespace \
  -f operator-values.yaml
```

### 5. Configure DeviceConfig

A default DeviceConfig is automatically created during helm installation (controlled by `crds.defaultCR.install: true` in values.yaml). The default DeviceConfig uses the image settings specified in the `deviceConfig.spec` section of your values file.

If you disabled the default DeviceConfig creation (by setting `crds.defaultCR.install: false` in your values file), or if you want to create an additional custom DeviceConfig, you can create one manually:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: amd-gpu-config
  namespace: kube-amd-gpu
spec:
  selector:
    # if using SR-IOV VM, please use feature.node.kubernetes.io/amd-vgpu: "true"
    feature.node.kubernetes.io/amd-gpu: "true"
  driver:
    enable: false  # Set to true if you want operator to build and manage out-of-tree drivers
    image: internal-registry.example.com/rocm/driver-image
    version: "<version>"
  commonConfig:
    initContainerImage: internal-registry.example.com/busybox:1.36
    utilsContainer:
      image: internal-registry.example.com/rocm/gpu-operator-utils:<version>
  devicePlugin:
    enableDevicePlugin: true
    devicePluginImage: internal-registry.example.com/rocm/k8s-device-plugin:<version>
    enableNodeLabeller: true
    nodeLabellerImage: internal-registry.example.com/rocm/k8s-device-plugin:labeller-<version>
  metricsExporter:
    enable: true
    image: internal-registry.example.com/rocm/device-metrics-exporter:<version>
  testRunner:
    enable: false
    image: internal-registry.example.com/rocm/test-runner:<version>
  configManager:
    enable: false
    image: internal-registry.example.com/rocm/device-config-manager:<version>
```

## Verification

- Check operator pod status:

```bash
kubectl get pods -n kube-amd-gpu
```

- Verify driver installation:

```bash
kubectl get deviceconfig -n kube-amd-gpu
```

- Check GPU detection:

```bash
kubectl get nodes -l feature.node.kubernetes.io/amd-gpu=true
```

## Troubleshooting

- Image Pull Errors
  - Verify internal registry connectivity
  - Check image names and tags
  - Verify registry credentials
- Driver Build Failures
  - Verify package repository connectivity
  - Check package availability
  - Verify build dependencies
- Certificate Issues
  - Check cert-manager deployment
  - Verify TLS certificates for internal services

### Collecting Logs

```bash
# Operator logs
kubectl logs -n kube-amd-gpu deployment/amd-gpu-operator-controller-manager

# Driver build logs
kubectl logs -n kube-amd-gpu <driver-build-pod>
```

Run the support tool for comprehensive diagnostics:

```bash
./tools/techsupport_dump.sh -w -o yaml <node-name>
```

## Additional Considerations

- Registry Certificates
  - Ensure registry certificates are trusted by all nodes
  - Configure container runtime to trust internal certificates
- Package Repository Security
  - Configure repository signing keys
  - Verify package integrity
- Network Requirements
  - Ensure internal DNS resolution works
  - Configure necessary firewall rules
  - Set up required proxy settings if applicable
