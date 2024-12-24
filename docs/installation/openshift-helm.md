# OpenShift (Helm)

```{warning}
Installing via Helm is not a recommended method for Red Hat OpenShift. Users wishing to use the AMD GPU with OpenShift should consider using the OLM method instead.
```

This guide walks through installing the AMD GPU Operator on an OpenShift cluster using Helm.

## Prerequisites

### OpenShift Requirements

- OpenShift Container Platform 4.16 or later
- Cluster administrator privileges
- Helm v3.2.0 or later
- `oc` CLI tool configured with cluster access

### Required OpenShift Operators

The following operators must be enabled in your OpenShift cluster (enabled by default):

- **Service-CA Operator**
  - Required for certificate signing and webhook authentication
  - Verifies communication between kube-api-server and KMM webhook server

- **MachineConfig Operator**
  - Required for configuring the blacklist for `amdgpu` driver
  - Manages node-level configuration

- **Cluster Image Registry Operator**
  - Required for driver image builds within OpenShift
  - Manages internal image registry storage
  - Steps to enable image registry operator if it is disabled (example using emptyDir):
    - Configure registry storage: ```oc patch configs.imageregistry.operator.openshift.io cluster --type merge --patch '{"spec":{"storage":{"emptyDir":{}}}}'```
    - Enable the registry: ```oc patch configs.imageregistry.operator.openshift.io cluster --type merge --patch '{"spec":{"managementState":"Managed"}}'```
    - Verify the registry pod is running: ```oc get pods -n openshift-image-registry```

## Installation Methods

There are two ways to install the AMD GPU Operator on OpenShift:

1. [All-in-One Installation](#method-1-all-in-one-installation)
2. [Component-by-Component Installation](#method-2-component-by-component-installation)

### Method 1: All-in-One Installation

This method installs the operator and all dependencies using a single Helm chart.

- Install the operator and dependencies:

```bash
helm install amd-gpu-operator rocm/gpu-operator-helm \
  --namespace kube-amd-gpu\
  --create-namespace \
  --set platform=openshift
```

- Verify the installation:

```bash
oc get pods -n kube-amd-gpu
```

Expected output:

```bash
NAME                                                   READY   STATUS    RESTARTS   AGE
nfd-master-67b568b89c-lvk9k                            1/1     Running   0          2m
nfd-worker-nkrgl                                       1/1     Running   0          2m
amd-gpu-operator-controller-manager-56844b49b4-tk75f   1/1     Running   0          2m
amd-gpu-kmm-controller-78ddd75846-kxd8n                1/1     Running   0          2m
amd-gpu-kmm-webhook-server-749cb8b565-ktbsp            1/1     Running   0          2m
amd-gpu-nfd-controller-manager-77764d98c5-h76pp        2/2     Running   0          2m
```

### Method 2: Component-by-Component Installation

This method allows more control over the installation process by installing dependencies separately.

#### Step 1: Install Node Feature Discovery (NFD) Operator

1. Navigate to OpenShift Web Console → OperatorHub
2. Search for "Node Feature Discovery"
3. Select and install the Red Hat version of the operator
4. Choose the default installation options

#### Step 2: Install Kernel Module Management (KMM) Operator

1. Navigate to OpenShift Web Console → OperatorHub
2. Search for "Kernel Module Management"
3. Select and install the Red Hat version (without Hub label)
4. Choose the default installation options

#### Step 3: Install AMD GPU Operator

Install the operator while skipping the already-installed dependencies:

```bash
helm install amd-gpu-operator rocm/gpu-operator-helm \
  --namespace kube-amd-gpu \
  --create-namespace \
  --set platform=openshift \
  --set nfd.enabled=false \
  --set kmm.enabled=false
```

## Post-Installation Configuration

### 1. Configure Node Feature Discovery

Create an NFD rule to detect AMD GPUs:

```yaml
apiVersion: nfd.openshift.io/v1
kind: NodeFeatureDiscovery
metadata:
  name: amd-gpu-nfd-instance
  namespace: kube-amd-gpu
spec:
  operand:
    image: quay.io/openshift/origin-node-feature-discovery:4.16
    imagePullPolicy: IfNotPresent
    servicePort: 12000
  workerConfig:
    configData: |
      core:
        sleepInterval: 60s
      sources:
        pci:
          deviceClassWhitelist:
            - "0200"
            - "03"
            - "12"
          deviceLabelFields:
            - "vendor"
            - "device"
        custom:
        - name: amd-gpu
          labels:
            feature.node.kubernetes.io/amd-gpu: "true"
          matchAny:
            - matchFeatures:
                - feature: pci.device
                  matchExpressions:
                    vendor: {op: In, value: ["1002"]}
                    device: {op: In, value: [
                      "74a0", # MI300A
                      "74a1", # MI300X
                      "740f", # MI210
                      "7408", # MI250X
                      "740c", # MI250/MI250X
                      "738c", # MI100
                      "738e"  # MI100
                    ]}
```

### 2. Create blacklist (for installing out-of-tree kernel module)

Create a Machine Config Operator custom resource to add `amdgpu` kernel module into the modprobe blacklist, here is an example of custom resource `MachineConfig`, please set `master` for the label `machineconfiguration.openshift.io/role` if you run Single Node OpenShift or `worker` in other scenarios with dedicated controllers.

```{warning}
After adding `amdgpu` kernel module to blacklist by using `MachineConfig` custom resource, **the Machine Config Operator will automatically reboot selected nodes.**
```

```yaml
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: worker
  name: amdgpu-module-blacklist
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
        - path: "/etc/modprobe.d/amdgpu-blacklist.conf"
          mode: 420
          overwrite: true
          contents:
            source: "data:text/plain;base64,YmxhY2tsaXN0IGFtZGdwdQo="
```

### 3. Create DeviceConfig Resource

Create a `DeviceConfig` to trigger driver installation:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: amd-gpu-config
  namespace: kube-amd-gpu
spec:
  driver:
    enable: true
    image: image-registry.openshift-image-registry.svc:5000/amdgpu_kmod
    version: 6.2.2
  selector:
    feature.node.kubernetes.io/amd-gpu: "true"
```

## Verification

### 1. Check Node Labels

Verify GPU detection:

```bash
oc get nodes -l feature.node.kubernetes.io/amd-gpu=true
```

### 2. Check Component Status

- Verify all pods are running:
  
```bash
oc get pods -n kube-amd-gpu
```

- Check GPU resource availability:

```bash
oc get node -o json | jq '.items[].status.capacity."amd.com/gpu"'
```

### 3. Check Driver Status

Monitor driver installation:

```bash
oc logs -n kube-amd-gpu-l app=kmm-worker
```

## Troubleshooting

### Common Issues

1. **Certificate Issues**
   - Check Service-CA operator status
   - Verify webhook certificates are properly mounted

2. **Driver Build Failures**
   - Check builder pod logs
   - Verify registry access
   - Check available storage

3. **Node Labeling Issues**
   - Verify NFD operator status
   - Check NFD worker pods on GPU nodes
   - Review NFD rule syntax

For detailed troubleshooting, run the support tool:

```bash
./tools/techsupport_dump.sh -w -o yaml <node-name>
```

## Uninstallation

Please refer to the [Uninstallation](../uninstallation/uninstallation) document for uninstalling related resources.
