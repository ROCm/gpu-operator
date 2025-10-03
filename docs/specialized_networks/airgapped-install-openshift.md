# Air-gapped Installation Guide for Openshift Environments

This guide explains how to install the AMD GPU Operator in an air-gapped environment where the Openshift cluster has no external network connectivity. 
This procedure assumes that the system has internet access during the image creation and mirroring process. We are using the OpenShift internal repository for convenience, but the procedure should be similar for external repositories like quay and docker; however, the process as a whole may differ.
Currently we only support GPU operator installation in air-gapped environment with a pre-compiled driver. To build(pre-compile) driver one of the system (it can be in staging environment) should have internet access during image creation and mirroring process.

## Prerequisites

- OpenShift 4.16+
- Internal repository is configured, see https://instinct.docs.amd.com/projects/gpu-operator/en/latest/installation/openshift-olm.html#configure-internal-registry for details.
- Internet Access during operator install, driver compilation and image import processes. 
- NFD, KMM and GPU Operator installed via OperatorHub 

### Required Images

The following images must be mirrored to your internal registry, see section 2.A in this document for details. 

```
rocm/k8s-device-plugin:rhubi-latest
rocm/k8s-node-labeller:rhubi-latest
```
## Installation Steps

### 1. Build precompiled driver image

Since this image is built in situ this procedure will differ from the images for the various GPU Operator components such as the labeler and device-plugin

A. Use basic DeviceConfig Custom Resource (CR), this will trigger a build when created and put the precompiled driver in the default imagestream location (image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/amdgpu_kmod) 

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: devconf
  namespace: kube-amd-gpu
spec:
  driver:
    enable: true
    version: "7.0"

  devicePlugin:
    devicePluginImage: rocm/k8s-device-plugin:rhubi-latest
    nodeLabellerImage: rocm/k8s-device-plugin:labeller-rhubi-latest

  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

B. Create the CR to trigger the build process.
```bash
$ oc create -f myDeviceConfig.y -n kube-amd-gpu
deviceconfig.amd.com/devconf created
```

C. Observe the build process complete. 
```bash
$ oc get pods -n kube-amd-gpu | grep build
devconf-build-trzb6-build                              1/1     Running    0          12s

# observe build using oc log command
$ oc logs devconf-build-trzb6-build -n kube-amd-gpu
```

D. Once the build is complete, verify that the precompiled image is located in the internal registry.
```bash
$ oc get is -n kube-amd-gpu
NAME                      IMAGE REPOSITORY                                                                        TAGS                                            UPDATED
amdgpu_kmod               image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/amdgpu_kmod               coreos-9.6-5.14.0-570.19.1.el9_6.x86_64-6.4.1   3 days ago
```

### 2. Import required images

A. Import the device-labeller and device-plugin images from docker into your internal registry 
```bash
oc import-image rocm/k8s-device-plugin:rhubi-latest -n kube-amd-gpu --confirm 
oc import-image rocm/k8s-node-labeller:rhubi-latest -n kube-amd-gpu --confirm
```

B. Once imported, verify that the required images are located in the internal registry. 
```bash
$ oc get is -n kube-amd-gpu
NAME                      IMAGE REPOSITORY                                                                        TAGS                                            UPDATED
amdgpu_kmod               image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/amdgpu_kmod               coreos-9.6-5.14.0-570.19.1.el9_6.x86_64-6.4.1   3 days ago
k8s-device-plugin         image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/k8s-device-plugin         rhubi-latest                                    2 hours ago
k8s-node-labeller         image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/k8s-node-labeller         rhubi-latest                                    2 hours ago
```

### 3. Deployment of DeviceConfig in disconnected environment

A. Once all the required images and the precompiled driver are present in the internal registry we can now deploy the modified DeviceConfig. Note: the image variables are pointing to the internal registry instead the external rcom repository.  
```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: devconf
  namespace: kube-amd-gpu
spec:
  driver:
    image: image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/amdgpu_kmod
    enable: true
    version: "7.0"

  devicePlugin:
    devicePluginImage: image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/k8s-device-plugin:rhubi-latest
    nodeLabellerImage: image-registry.openshift-image-registry.svc:5000/kube-amd-gpu/k8s-node-labeller:rhubi-latest

  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```
