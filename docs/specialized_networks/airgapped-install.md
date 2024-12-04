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
rocm/gpu-operator:<version>
rocm/gpu-operator-bundle:<version>
rocm/gpu-operator-catalog:<version>

# Device Plugin Images
rocm/k8s-device-plugin:<version>
rocm/k8s-device-plugin-labeller:<version>

# Dependency Images
quay.io/jetstack/cert-manager-controller:<version>
quay.io/jetstack/cert-manager-webhook:<version>
quay.io/jetstack/cert-manager-cainjector:<version>

### Required DEB Packages
# For driver compilation, ensure these packages are available in 
# your internal package repository:

#### Ubuntu
linux-headers-$(uname -r)
build-essential
```

## Installation Steps

### 1. Mirror Required Images

- Download images on a connected system:

```bash
# Example for core operator images
docker pull rocm/gpu-operator:<version>
docker pull rocm/k8s-device-plugin:<version>
```

- Tag images for your internal registry:

```bash
docker tag rocm/gpu-operator:<version> internal-registry.example.com/rocm/gpu-operator:<version>
docker tag rocm/k8s-device-plugin:<version> internal-registry.example.com/rocm/k8s-device-plugin:<version>
```

- Push to your internal registry:

```bash
docker push internal-registry.example.com/rocm/gpu-operator:<version>
docker push internal-registry.example.com/rocm/k8s-device-plugin:<version>
```

### 2. Configure Internal Package Repository

1. Create an internal package repository mirror containing required build packages
2. Configure worker nodes to use the internal repository
3. Verify package availability:

```bash
# Ubuntu
apt list linux-headers-$(uname -r) build-essential
```

### 3. Install Cert-Manager

- Create custom values file for cert-manager:

```yaml
# cert-manager-values.yaml
global:
  imageRegistry: internal-registry.example.com
```

- Install cert-manager using internal images:

```bash
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.15.1 \
  --set installCRDs=true \
  -f cert-manager-values.yaml
```

### 4. Install AMD GPU Operator

- Create custom values file for the operator:

```yaml
# operator-values.yaml
global:
  imageRegistry: internal-registry.example.com

driver:
  repository: internal-registry.example.com/rocm/gpu-operator
  version: "<version>"

devicePlugin:
  repository: internal-registry.example.com/rocm/k8s-device-plugin
  version: "<version>"

# Additional configuration for internal repositories
buildArgs:
  ROCM_REPO_URL: "http://internal-repo.example.com/rocm"
  ROCM_REPO_KEY: "http://internal-repo.example.com/rocm/rocm.gpg.key"
```

- Install the operator:

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts \
  --namespace kube-amd-gpu \
  --create-namespace \
  -f operator-values.yaml
```

### 5. Configure DeviceConfig

Create a DeviceConfig that references your internal registry:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: amd-gpu-config
  namespace: kube-amd-gpu
spec:
  driver:
    image: internal-registry.example.com/rocm/gpu-driver
    version: "<version>"
    
  devicePlugin:
    devicePluginImage: internal-registry.example.com/rocm/k8s-device-plugin:latest
    nodeLabellerImage: internal-registry.example.com/rocm/k8s-device-plugin-labeller:latest
    
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
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
