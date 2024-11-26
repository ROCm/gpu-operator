# Preparing Pre-compiled Driver Images

## Overview

The AMD GPU Operator uses the Kernel Module Management (KMM) Operator to deploy AMD GPU drivers on worker nodes. Due to kernel compatibility requirements, each driver image must match the worker node's exact environment:

- Linux distribution
- OS release version  
- Kernel version

## How KMM Selects Driver Images

KMM determines the appropriate driver image based on the combination of:

1. Worker node OS information
2. Requested ROCm driver version

### Image Tag Format

KMM looks for images with tags in these formats:

| OS | Tag Format | Example |
|----|------------|---------|
| Ubuntu | `ubuntu-<OS version>-<kernel>-<driver version>` | `ubuntu-22.04-6.8.0-40-generic-6.1.3` |

When a DeviceConfig is created, KMM will:

1. Check if a matching driver image exists in the registry
2. If not found, build the driver image in-cluster using the AMD GPU Operator's Dockerfile
3. If found, directly use the existing image to install the driver

## Building Pre-compiled Driver Images

### Dockerfile Example

```dockerfile
FROM ubuntu:$$VERSION as builder
ARG KERNEL_FULL_VERSION
ARG DRIVERS_VERSION
ARG REPO_URL

# Install build dependencies
RUN apt-get update && apt-get install -y bc \
    bison \
    flex \
    libelf-dev \
    gnupg \
    wget \
    git \
    make \
    gcc \
    linux-headers-${KERNEL_FULL_VERSION} \
    linux-modules-extra-${KERNEL_FULL_VERSION}

# Configure AMD GPU repository
RUN mkdir --parents --mode=0755 /etc/apt/keyrings
RUN wget ${REPO_URL}/rocm/rocm.gpg.key -O - | \
    gpg --dearmor | tee /etc/apt/keyrings/rocm.gpg > /dev/null
RUN echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/rocm.gpg] ${REPO_URL}/amdgpu/${DRIVERS_VERSION}/ubuntu $$DRIVER_LABEL main" \
    | tee /etc/apt/sources.list.d/amdgpu.list

# Install and configure driver
RUN apt-get update && apt-get install -y amdgpu-dkms
RUN depmod ${KERNEL_FULL_VERSION}

# Create final image
FROM ubuntu:$$VERSION
ARG KERNEL_FULL_VERSION

RUN apt-get update && apt-get install -y kmod

# Set up module directory structure
RUN mkdir -p /opt/lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/
COPY --from=builder /lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/amd* /opt/lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/
COPY --from=builder /lib/modules/${KERNEL_FULL_VERSION}/modules.* /opt/lib/modules/${KERNEL_FULL_VERSION}/
RUN ln -s /lib/modules/${KERNEL_FULL_VERSION}/kernel /opt/lib/modules/${KERNEL_FULL_VERSION}/kernel

# Set up firmware directory
RUN mkdir -p /firmwareDir/updates/amdgpu
COPY --from=builder /lib/firmware/updates/amdgpu /firmwareDir/updates/amdgpu
```

### Build Steps

- Choose a base image matching your worker nodes' OS (example: `ubuntu:22.04`)
- Install `amdgpu-dkms` package using the OS package manager
- Update Module Dependencies: run `depmod ${KERNEL_FULL_VERSION}`
- Configure the final image
  - Install `kmod` (required for modprobe operations)
  - Copy required files to these locations:
    - Kernel modules: `/opt/lib/modules/${KERNEL_FULL_VERSION}/`
    - Firmware files: `/firmwareDir/updates/amdgpu/`

#### Build the final image

```bash
docker build \
  --build-arg KERNEL_FULL_VERSION=$(uname -r) \
  --build-arg DRIVERS_VERSION=6.1.3 \
  --build-arg REPO_URL=https://repo.example.com \
  -t amdgpu-driver .
```

#### Tag the image

See [examples](#image-tag-format) to tag the image with the correct tag name:

```bash
docker tag amdgpu-driver registry.example.com/amdgpu-driver:ubuntu-22.04-6.8.0-40-generic-6.1.3
```

#### Push to the image to a registry

```bash
docker push registry.example.com/amdgpu-driver:ubuntu-22.04-6.8.0-40-generic-6.1.3
```

## Using Pre-compiled Images

Configure your DeviceConfig to use the pre-compiled images:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
  namespace: kube-amd-gpu
spec:
  driver:
    # Registry path without tag - operator manages tags
    image: registry.example.com/amdgpu-driver
    
    # Registry credentials if required
    imageRegistrySecret:
      name: docker-auth
      
    # Driver version
    version: "6.2.2"
    
  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest
    
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

> **Important**: Do not include the image tag in the `image` field - the operator automatically appends the appropriate tag based on the node's OS and kernel version.

Create registry credentials, if needed:

```bash
kubectl create secret docker-registry docker-auth \
  -n kube-amd-gpu \
  --docker-server=registry.example.com \
  --docker-username=xxx \
  --docker-password=xxx
```

- if you are hosting driver images in DockerHub, you don't need to specify the parameter ```--docker-server```
