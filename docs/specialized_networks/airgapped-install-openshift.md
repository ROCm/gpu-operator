# Air-gapped Installation Guide for Openshift Environments

This guide explains how to install the AMD GPU Operator in an air-gapped environment where the Openshift cluster has no external network connectivity.

## Prerequisites

1. OpenShift 4.16+
2. Assume users have followed [OpenShift Official Documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/disconnected_environments/mirroring-in-disconnected-environments) to install the air-gapped cluster and setup a Mirror Registry in Air-gapped environment.

![Air-gapped Installation Diagram](../_static/ocp_airgapped.png)

```{Note}
  * In general users action item is only to provide the `ImageSetConfiguration` to configure the operator catalogs and images for mirroring the artifacts in the mirror container registry.
  * Users may need to take extra step to manually copy mirrored artifacts to air-gapped system, in case the jump host is not allowed to directly push image to the mirror container registry.
  * Most of the steps described in the graph above is automatically completed by the `oc-mirror` and other RedHat provided tool, which can be downloaded from [OpenShift official website](https://console.redhat.com/openshift/downloads).
```

Here is an example of AMD GPU Operator required `ImageSetConfiguration` for users to mirror required catalogs and images into their mirror registry. 

```{Warning}
1. The following `ImageSetConfiguration` file is just an example and it is incomplete.
2. Users need to configure the `storageConfig` part, for directly pushing artifacts to the mirror container registry or saving into local file storage.
3. Users may merge the `mirror` part of this example file with their own `ImageSetConfiguration`.
4. The detailed explanation of `ImageSetConfiguration` can be found from [OpenShift official documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/disconnected_environments/mirroring-in-disconnected-environments#using-oc-mirror_about-installing-oc-mirror-v2).
```

```yaml
kind: ImageSetConfiguration
apiVersion: mirror.openshift.io/v1alpha2
storageConfig:
  # Configured by users
  # option 1: directly mirrored images into your mirror registry
  #registry:
  #  imageURL: <your mirror registry URL>/oc-mirror
  # option 2: save all mirrored images into local filesystem
  #local:
  #  path: /path/to/local

mirror:
  # in this file we use 4.19 as an example
  # please adjust the OpenShift version if needed
  platform:
    graph: true 
    channels:
      - name: stable-4.19
        type: ocp

  operators:
    - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.19
      packages:
        # Node Feature Discovery (NFD)
        - name: nfd
          minVersion: "4.19.0-202509300824" # adjust the version if needed
          channels:
            - name: stable
        # Kernel Module Management (KMM)
        - name: kernel-module-management
          minVersion: "2.4.1" # adjust the version if needed
          channels:
            - name: stable
            - name: release-2.4

    # AMD GPU Operator (Certified)
    # To get full list of released version
    # Either go to OperatorHub
    # Or check https://github.com/redhat-openshift-ecosystem/certified-operators/tree/main/operators/amd-gpu-operator
    - catalog: registry.redhat.io/redhat/certified-operator-index:v4.19
      packages:
        - name: amd-gpu-operator
          minVersion: "1.3.2" # adjust the version if needed
          channels:
            - name: alpha
  # adjust the image tag if needed
  additionalImages:
    - name: registry.redhat.io/ubi9/ubi:latest
    - name: docker.io/rocm/gpu-operator:v1.3.1
    - name: docker.io/rocm/gpu-operator-utils:v1.3.1
    - name: docker.io/library/busybox:1.36
    - name: docker.io/rocm/device-metrics-exporter:v1.3.1
    - name: docker.io/rocm/test-runner:v1.3.1
    - name: docker.io/rocm/device-config-manager:v1.3.1
    - name: docker.io/rocm/rocm-terminal:latest
    - name: docker.io/rocm/k8s-device-plugin:latest
    - name: docker.io/rocm/k8s-node-labeller:latest

helm: {}
```

3. After mirroring setup, assume users installed NFD, KMM and enabled internal image registry in air-gapped cluster, see [OpenShift OLM Installation](../installation/openshift-olm.md##configure-rnternal-registry) for details.

4. Users installed AMD GPU Operator in Air-gapped cluster without creating DeviceConfig.

## Installation Steps

### 1. Build precompiled driver image

Please build the pre-compiled driver image in the build cluster that has Internet access by following [Preparing Pre-compiled Driver Images](../drivers/precompiled-driver.md) and follow the steps for OpenShift section.

After successfully pushing the driver image, save it by running:

* If you are using OpenShift internal registry
```bash
podman login -u deployer -p $(oc create token deployer) image-registry.openshift-image-registry.svc:5000
podman pull image-registry.openshift-image-registry.svc:5000/default/amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0
podman save image-registry.openshift-image-registry.svc:5000/default/amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0 -o driver-image.tar
```

* If you are using other image registry
```bash
podman login -u username -p password/token registry.example.com
podman pull registry.example.com/amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0
podman save registry.example.com/amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0 -o driver-image.tar
```

### 2. Import pre-compiled driver image

A. Import images 

```{Note}
1. This step is for using the pre-compiled driver image within the cOpenShift internal registry (this is the OpenShift built-in image registry, not the mirror registry for Air-gapped installation). 
2. For users who already push the pre-compiled driver image to other registry, they don't have to manually load it in internal registry, just skip to step 3 to specify the image URL in `spec.driver.image`.
```

* Import pre-compiled driver image

After copying the image files to the air-gapped cluster, please switch to the air-gapped cluster and use podman to load the image, re-tag if needed then push the image to desired image registry:
  * Load the image file: `podman load -i driver-image.tar`
  * Re-tag if needed `podman tag <old tag> <new tag>`, remember to tag the image to the gpu operator's namespace, e.g. if you are using gpu operator in `openshift-amd-gpu`, please tag the image to`image-registry.openshift-image-registry.svc:5000/openshift-amd-gpu/amdgpu_kmod`. 
  * Use podman to login to the image registry if needed, for OpenShift internal registry:
  ```bash
  podman login -u builder -p $(oc create token builder) image-registry.openshift-image-registry.svc:5000
  ```
  * Push the image: `podman push <new tag>`

B. Once imported, verify that the required images are located in the internal registry. 

For example, if you are using internal registry:
```bash
$ oc get is -n openshift-amd-gpu
NAME                      IMAGE REPOSITORY                                                                        TAGS                                            UPDATED
amdgpu_kmod               image-registry.openshift-image-registry.svc:5000/openshift-amd-gpu/amdgpu_kmod               coreos-9.6-5.14.0-570.19.1.el9_6.x86_64-6.4.1   3 days ago
```

### 3. Deployment of DeviceConfig in air-gapped environment

A. Once all the required images and the precompiled driver are present in the internal registry we can now deploy the modified DeviceConfig. Note: the image variables are pointing to the internal registry instead the external ROCm repository.  
```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
  namespace: openshift-amd-gpu
spec:
  driver:
    # 1. specify image here if you are NOT using OpenShift internal registry
    # 2. specify the image without tag
    #image: registry.example.com/amdgpu_kmod
    enable: true
    version: "7.0"
  devicePlugin:
    enableNodeLabeller: true
  metricsExporter:
    enable: true
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```
