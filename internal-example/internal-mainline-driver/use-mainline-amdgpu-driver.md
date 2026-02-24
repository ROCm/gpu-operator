# Using Internal/Mainline AMD GPU Driver Builds

This guide explains how to use internal AMD GPU driver builds from the AMD internal artifactory with the GPU Operator.

## Overview

The GPU Operator supports using internal AMD GPU driver builds by configuring the `amdgpuInstallerRepoURL` field in the DeviceConfig Custom Resource. This allows you to specify custom build locations and versions for both AMDGPU and ROCm components.

## Configuration Example

For example, if you are trying to install the build from:
```
http://rocm-ci.amd.com/job/compute-rocm-dkms-no-npi-hipclang/14864/
```

The original manual installation commands would be:

```bash
wget http://artifactory-cdn.amd.com/artifactory/list/amdgpu-deb/amdgpu-install-internal_6.3-22.04-1_all.deb
sudo apt-get install ./amdgpu-install-internal_6.3-22.04-1_all.deb
sudo amdgpu-repo --amdgpu-build=2045781 --rocm-build=compute-rocm-dkms-no-npi-hipclang/14864
sudo amdgpu-install --usecase=rocm
```

## DeviceConfig Configuration

To use this build with the GPU Operator, you need to specify 4 items in the DeviceConfig CR's `amdgpuInstallerRepoURL` field:

1. Download link
2. Deb file name
3. AMDGPU build tag
4. ROCm build tag

The corresponding DeviceConfig would become:

```yaml
spec:
  driver:
    version: "6.3"
    amdgpuInstallerRepoURL: "http://artifactory-cdn.amd.com/artifactory/list/amdgpu-deb/amdgpu-install-internal_6.3-24.04-1_all.deb amdgpu-install-internal_6.3-24.04-1_all.deb 2045781 compute-rocm-dkms-no-npi-hipclang/14864"
```

The `amdgpuInstallerRepoURL` field format is:
```
<download-link> <deb-file-name> <amdgpu-build-tag> <rocm-build-tag>
```

## Important Considerations

### Build Time

The image builder pod takes longer to build the image when using internal builds, because ROCm internal build commands take longer to download artifacts from the internal artifactory compared to downloading from the public repo.radeon.com.

### DNS Resolution for Internal Artifactory

If your Kubernetes cluster has difficulty resolving the domain name `artifactory-cdn`, you need to configure the cluster DNS to use a DNS server that can resolve this internal domain.

To configure the cluster to use the `127.0.0.53` DNS server:

1. Setup DNS for ROCm internal artifactory by editing the CoreDNS ConfigMap:

   ```bash
   kubectl edit cm -n kube-system coredns
   ```

2. Modify the forward rule to include `127.0.0.53`:

   ```
   forward . 127.0.0.53 /etc/resolv.conf {
     max_concurrent 1000
   }
   ```

3. Save the updated ConfigMap and rollout restart the CoreDNS deployment:

   ```bash
   kubectl rollout restart deployment coredns -n kube-system
   ```

This ensures that the cluster can properly resolve the internal artifactory domain name and download the required driver packages.
