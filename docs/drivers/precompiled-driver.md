# Preparing Pre-compiled Driver Images

## Overview

The AMD GPU Operator uses the Kernel Module Management (KMM) Operator to deploy AMD GPU drivers on worker nodes. Due to kernel compatibility requirements, each driver image must match the worker node's exact environment:

- Linux distribution
- OS release version  
- Kernel version

Users could prepare pre-compiled driver images in advance and import them into the cluster to let KMM skip the driver build stage within the cluster and directly use driver images to load amdgpu kernel modules into the worker nodes.

## How KMM Selects Driver Images

KMM determines the appropriate driver image based on the combination of:

1. Worker node OS information
2. Requested ROCm driver version

### Image Tag Format

KMM looks for driver images based on tags, the controller will use these methods to determine the image tag:

1. Parse the node's `osImage` field to determine the OS and version `kubectl get node -oyaml | grep -i osImage`:

| osImage | OS | version |
|---------|-----------|-------------------|
| `Ubuntu 24.04.1 LTS` | `Ubuntu` | `24.04` |
| `Red Hat Enterprise Linux CoreOS 9.6.20250916-0 (Plow)` | `coreos` | `9.6` |

2. Read the node's `kernelVersion` field to determine to kernel version `kubectl get node -oyaml | grep -i kernelVersion`.
3. Read user configured amdgpu driver version from `DeviceConfig` field `spec.driver.version`.


| OS | Tag Format | Example Image Tag |
|----|------------|-------------------|
| `ubuntu` | `ubuntu-<OS version>-<kernel>-<driver version>` | `ubuntu-22.04-6.8.0-40-generic-6.1.3` |
| `coreos` | `coreos-<OS version>-<kernel>-<driver version>` | `coreos-9.6-5.14.0-427.28.1.el9_4.x86_64-6.2.2` |

When a DeviceConfig is created with driver management enabled (`spec.driver.enable=true`), KMM will:

1. Check if a matching driver image exists in the registry
2. If not found, build the driver image in-cluster using the AMD GPU Operator's Dockerfile
3. If found, directly use the existing image to install the driver

## Building Pre-compiled Driver Images

### Ubuntu

Follow these image build steps to get a pre-compiled driver images, make sure your system matched with [ROCm required Linux system requirement](https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/system-requirements.html).  

1. Prepare the Dockerfile

```dockerfile
ARG OS_VERSION
FROM ubuntu:${OS_VERSION} as builder
ARG OS_CODENAME
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
RUN echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/rocm.gpg] ${REPO_URL}/amdgpu/${DRIVERS_VERSION}/ubuntu ${OS_CODENAME} main" \
    | tee /etc/apt/sources.list.d/amdgpu.list

# Install and configure driver
RUN apt-get update && apt-get install -y amdgpu-dkms
RUN depmod ${KERNEL_FULL_VERSION}

# Create final image
ARG OS_VERSION
FROM ubuntu:${OS_VERSION}
ARG KERNEL_FULL_VERSION

RUN apt-get update && apt-get install -y kmod

# Set up module directory structure
RUN mkdir -p /opt/lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/
COPY --from=builder /lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/amd* /opt/lib/modules/${KERNEL_FULL_VERSION}/updates/dkms/
COPY --from=builder /lib/modules/${KERNEL_FULL_VERSION}/modules.* /opt/lib/modules/${KERNEL_FULL_VERSION}/
COPY --from=builder /lib/modules/${KERNEL_FULL_VERSION}/kernel /opt/lib/modules/${KERNEL_FULL_VERSION}/kernel

# Set up firmware directory
RUN mkdir -p /firmwareDir/updates/amdgpu
COPY --from=builder /lib/firmware/updates/amdgpu /firmwareDir/updates/amdgpu
```

Build Steps Explanation:

- Choose a base image matching your worker nodes' OS (example: `ubuntu:22.04`)
- Install `amdgpu-dkms` package using the OS package manager
- Update Module Dependencies: run `depmod ${KERNEL_FULL_VERSION}`
- Configure the final image
  - Install `kmod` (required for modprobe operations)
  - Copy required files to these locations, required by KMM:
    - Kernel modules: `/opt/lib/modules/${KERNEL_FULL_VERSION}/`
    - Firmware files: `/firmwareDir/updates/amdgpu/`

2. Trigger the build with the Dockerfile 

Make sure the build node has the same OS and kernel with your production nodes.

See [examples](#image-tag-format) to tag the image with the correct tag name.

```bash
source /etc/os-release
export AMDGPU_VERSION=7.0
docker build \
  --build-arg OS_VERSION=${VERSION_ID} \
  --build-arg OS_CODENAME=${VERSION_CODENAME} \
  --build-arg KERNEL_FULL_VERSION=$(uname -r) \
  --build-arg DRIVERS_VERSION=${AMDGPU_VERSION} \
  --build-arg REPO_URL=https://repo.radeon.com \
  -t registry.example.com/amdgpu-driver:ubuntu-${VERSION_ID}-$(uname -r)-${AMDGPU_VERSION} .
```

3. Push to the image to a registry

```bash
docker push registry.example.com/amdgpu-driver:ubuntu-${VERSION_ID}-$(uname -r)-${AMDGPU_VERSION}
```

### OpenShift - Red Hat Enterprise Linux CoreOS

Follow these image build steps to get a pre-compiled driver images for OpenShift cluster, make sure your RHEL version and driver version matched with [ROCm required Linux system requirement](https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/system-requirements.html).

1. Collect System Information

Please collect system information from OpenShift build node before configuring the build process:

* kernel version: `uname -r`
* kernel compatible OpenShift DriverToolkit image: `oc adm release info --image-for driver-toolkit`

2. Prepare image registry:

Please decide where you want to push your pre-compiled driver image:

  * Case 1: Use OpenShift internal registry:
    * Enable internal registry (skip this step if you already enabled registry):
    ```bash
    oc patch configs.imageregistry.operator.openshift.io cluster --type merge \
      --patch '{"spec":{"storage":{"emptyDir":{}}}}'
    oc patch configs.imageregistry.operator.openshift.io cluster --type merge \
    --patch '{"spec":{"managementState":"Managed"}}'
    # make sure the image registry pods are running
    oc get pods -n openshift-image-registry
    ```
    * Create ImageStream
    ```bash
    oc create imagestream amdgpu_kmod
    ```
  * Case 2: Use external image registry:
    * Create secret to push image if required:
    ```bash
    kubectl create secret docker-registry docker-auth \
      --docker-server=registry.example.com \
      --docker-username=xxx \
      --docker-password=xxx
    ```
3. Create OpenShift `BuildConfig`

Please create the following YAML file, the full example is assuming you are using OpenShift internal image registry and build config will be saved in default namespace.

* If you want to configure the build in other namespace, please change the namespace accordingly in the example steps.
* If you want to use other image registry, please replace the `spec.output` part with this:

```yaml
spec:
  output:
    pushSecret:
      name: docker-auth
    to:
      kind: DockerImage
      # follow the Image Tag Format section to get your image ta
      name: registry.example.com/amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0
```

Full example:

```yaml
kind: BuildConfig
apiVersion: build.openshift.io/v1
metadata:
  name: amd-gpu-operator-build
  namespace: default
  labels:
    app.kubernetes.io/component: build
spec:
  runPolicy: Serial
  nodeSelector: null
  output:
    to:
      kind: ImageStreamTag
      # follow the Image Tag Format section to get your image tag
      name: amdgpu_kmod:coreos-9.6-5.14.0-570.45.1.el9_6.x86_64-7.0
  successfulBuildsHistoryLimit: 5
  failedBuildsHistoryLimit: 5
  strategy:
    type: Docker
    dockerStrategy:
      buildArgs:
        - name: DRIVERS_VERSION # amdgpu version
          value: '7.0'
        - name: REPO_URL
          value: 'https://repo.radeon.com'
        - name: KERNEL_VERSION
          value: 5.14.0-570.45.1.el9_6.x86_64
        - name: KERNEL_FULL_VERSION
          value: 5.14.0-570.45.1.el9_6.x86_64
        - name: DTK_AUTO
          # DriverToolkit image, get it from `oc adm release info --image-for driver-toolkit`
          value: 'quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:b3af1db51aa8a453fbba972e0039a496f0848eb15e6b411ef0bbb7d5ed864ac7'
  serviceAccount: builder
  source:
    type: Dockerfile
    dockerfile: |-
      ARG DTK_AUTO
      FROM ${DTK_AUTO} as builder
      ARG KERNEL_VERSION
      ARG DRIVERS_VERSION
      ARG REPO_URL
      RUN dnf install https://dl.fedoraproject.org/pub/epel/epel-release-latest-9.noarch.rpm -y && \
          crb enable && \
          sed -i "s/\$releasever/9/g" /etc/yum.repos.d/epel*.repo && \
          dnf install dnf-plugin-config-manager -y && \
          dnf clean all
      RUN dnf install -y 'dnf-command(config-manager)' && \
          dnf config-manager --add-repo=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/ && \
          dnf config-manager --add-repo=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/ && \
          rpm --import https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official && \
          dnf clean all
      RUN source /etc/os-release && \
          echo -e "[amdgpu] \n\
      name=amdgpu \n\
      baseurl=${REPO_URL}/amdgpu/${DRIVERS_VERSION}/el/${VERSION_ID}/main/x86_64/ \n\
      enabled=1 \n\
      priority=50 \n\
      gpgcheck=1 \n\
      gpgkey=${REPO_URL}/rocm/rocm.gpg.key" > /etc/yum.repos.d/amdgpu.repo
      RUN dnf clean all && \
          cat /etc/yum.repos.d/amdgpu.repo && \
          dnf install amdgpu-dkms -y && \
          depmod ${KERNEL_VERSION} && \
          find /lib/modules/${KERNEL_VERSION} -name "*.ko.xz" -exec xz -d {} \; && \
          depmod ${KERNEL_VERSION}
      RUN mkdir -p /modules_files && \
          mkdir -p /amdgpu_ko_files && \
          mkdir -p /kernel_files && \
          cp /lib/modules/${KERNEL_VERSION}/modules.* /modules_files/ && \
          cp -r /lib/modules/${KERNEL_VERSION}/extra/* /amdgpu_ko_files/ && \
          cp -r /lib/modules/${KERNEL_VERSION}/kernel/* /kernel_files/
      FROM registry.redhat.io/ubi9/ubi-minimal
      ARG KERNEL_VERSION
      RUN microdnf install -y kmod
      COPY --from=builder /amdgpu_ko_files /opt/lib/modules/${KERNEL_VERSION}/extra
      COPY --from=builder /kernel_files /opt/lib/modules/${KERNEL_VERSION}/kernel
      COPY --from=builder /modules_files /opt/lib/modules/${KERNEL_VERSION}/
      COPY --from=builder /lib/firmware/updates/amdgpu /firmwareDir/updates/amdgpu
```

4. Trigger driver image build

* Option 1 - Web Console:
  * Login to OpenShift web console with username and password
  * Select `Builds` then select `BuildConfigs` in the navigation bar
  * Click `Create BuildConfig` then select YAML view, copy over the YAML file created in last step
  * Select the `BuildConfig` in the list, click `Actions` then select `Start Build`
  * Select `Builds` in the current `BuildConfig` page, a new build should be triggered and in running status.
  * Wait for it to be completed, you can also monitor the progress in `Logs` section, in the end it should show push is successful.
  * Delete the `BuildConfig` if needed.
* Option 2 - Command Line Interface (CLI):
  * Create the `BuildConfig` by using the YAML file created in the last step: `oc apply -f build-config.yaml`
  * Start the build: `oc start-build amd-gpu-operator-build`
  * Check the build status: `oc get build` and `oc get pods | grep build`
  * Wait for it to complete, the logs should show that push is successful
  * Delete the `BuildConfig` if needed: `oc delete -f build-config.yaml`

## Using Pre-compiled Images

In previous section [Building Pre-compiled Driver Images](#building-pre-compiled-driver-images) we pushed driver image to `registry.example.com/amdgpu-driver`. Now you can configure your `DeviceConfig` to use the pre-compiled images:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-deviceconfig
  namespace: kube-amd-gpu
spec:
  driver:
    # Registry path without tag - operator manages tags
    # If you use OpenShift internal image registry, by default the operator will auto select the internal image registry URL
    image: registry.example.com/amdgpu_kmod
    
    # Registry credentials if required
    imageRegistrySecret:
      name: docker-auth
    # Driver version
    # NOTE: Starting from ROCm 7.1 the amdgpu version is using new versioning schema
    # please refer to https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/user-kernel-space-compat-matrix.html
    version: "7.0"
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
