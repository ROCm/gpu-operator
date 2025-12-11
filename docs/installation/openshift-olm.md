# OpenShift (OLM)

This guide explains how to deploy the AMD GPU Operator on OpenShift using the Operator Lifecycle Manager (OLM).

```{note}
For installing AMD GPU Operator in air-gapped OpenShift cluster, please also refer to [OpenShift Air-Gapped Installation](../specialized_networks/airgapped-install-openshift.md)
```

## Prerequisites

Before installing the AMD GPU Operator, ensure your OpenShift cluster meets the following requirements:

### Required Operators

The following operators must be enabled in your OpenShift cluster (these are typically enabled by default):

#### Service CA Operator

- Required for certificate signing and authentication between the kube-apiserver and KMM webhook server
- Verify status:

```bash
oc get pods -A | grep service-ca
```

#### Operator Lifecycle Manager (OLM)

- Required for managing operator installation and dependencies
- Verify status:

```bash
oc get pods -A | grep operator-lifecycle
```

#### MachineConfig Operator

- Required for configuring the blacklist for `amdgpu` driver
- Verify status:

```bash
oc get pods -A | grep machine-config
```

#### Cluster Image Registry Operator

- Required for driver image building and storage within OpenShift cluster
- Verify status:

```bash
oc get pods -A | grep image-registry
```

### Configure Internal Registry

If you plan to build driver images within the cluster, you must enable the OpenShift internal registry:

- Verify current registry status:

```bash
oc get pods -n openshift-image-registry
```

- Configure registry storage (example using emptyDir):

```bash
oc patch configs.imageregistry.operator.openshift.io cluster --type merge \
  --patch '{"spec":{"storage":{"emptyDir":{}}}}'
```

- Enable the registry:

```bash
oc patch configs.imageregistry.operator.openshift.io cluster --type merge \
  --patch '{"spec":{"managementState":"Managed"}}'
```

- Verify the registry pod is running:

```bash
oc get pods -n openshift-image-registry
```

## Installation

### 1. Install Required Dependencies

- Install Node Feature Discovery (NFD) Operator

1. Navigate to the OpenShift Web Console
2. Go to OperatorHub
3. Search for "Node Feature Discovery"
4. Select and install the RedHat version of the operator

- Install Kernel Module Management (KMM) Operator

1. Navigate to the OpenShift Web Console
2. Go to OperatorHub
3. Search for "Kernel Module Management"
4. Select and install the RedHat version (without Hub label)

### 2. Install AMD GPU Operator

1. Navigate to the OpenShift Web Console
2. Go to OperatorHub
3. Search for "amd"
4. Select and install the certified AMD GPU Operator

## Configuration

### 1. Create Node Feature Discovery Rule

Create an NFD custom resource to detect AMD GPU hardware, based on different deployment scenarios you need to choose creating `NodeFeatureDiscovery` or `NodeFeatureRule`.

* If your OpenShift cluster doesn't have `NodeFeatureDiscovery` deployed

Please create the ```NodeFeatureDiscovery``` under the namespace where NFD operator is running:

```{note}

When you are using OpenShift 4.16 you need to specify the NFD operand image in the following `NodeFeatureDiscovery` custom resource. Starting from OpenShift 4.17 you don't have to specify the operand image since the NFD operator will automatically select corresponding operand image.

    spec:
      operand:
        image: quay.io/openshift/origin-node-feature-discovery:latest
        imagePullPolicy: IfNotPresent
        servicePort: 12000
```

```yaml
apiVersion: nfd.openshift.io/v1
kind: NodeFeatureDiscovery
metadata:
  name: amd-gpu-operator-nfd-instance
  namespace: openshift-nfd
spec:
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
                      "75a3", # MI355X
                      "75a0", # MI350X
                      "74a5", # MI325X
                      "74a2", # MI308X
                      "74a8", # MI308X-HF
                      "74a0", # MI300A
                      "74a1", # MI300X
                      "74a9", # MI300X-HF
                      "740f", # MI210
                      "7408", # MI250X
                      "740c", # MI250/MI250X
                      "738c", # MI100
                      "738e"  # MI100
                    ]}
        - name: amd-vgpu
          labels:
            feature.node.kubernetes.io/amd-vgpu: "true"
          matchAny:
            - matchFeatures:
                - feature: pci.device
                  matchExpressions:
                    vendor: {op: In, value: ["1002"]}
                    device: {op: In, value: [
                      "75b3", # MI355X VF
                      "75b0", # MI350X VF
                      "74b9", # MI325X VF
                      "74b6", # MI308X VF
                      "74bc", # MI308X-HF VF
                      "74b5", # MI300X VF
                      "74bd", # MI300X-HF VF
                      "7410"  # MI210 VF
                    ]}
```

* If your OpenShift cluster already has `NodeFeatureDiscovery` deployed

You can alternatively create a namespaced `NodeFeatureRule` custom resource to avoid modifying `NodeFeatureDiscovery` which could possibly interrupt the existing node label.

```yaml
apiVersion: nfd.openshift.io/v1alpha1
kind: NodeFeatureRule
metadata:
  name: amd-gpu-operator-nfdrule
  namespace: openshift-amd-gpu
spec:
  rules:
    - name: amd-gpu
      labels:
        feature.node.kubernetes.io/amd-gpu: "true"
      matchAny:
        - matchFeatures:
            - feature: pci.device
              matchExpressions:
                vendor: {op: In, value: ["1002"]}
                device: {op: In, value: [
                  "75a3", # MI355X
                  "75a0", # MI350X
                  "74a5", # MI325X
                  "74a2", # MI308X
                  "74a8", # MI308X-HF
                  "74a0", # MI300A
                  "74a1", # MI300X
                  "74a9", # MI300X-HF
                  "740f", # MI210
                  "7408", # MI250X
                  "740c", # MI250/MI250X
                  "738c", # MI100
                  "738e"  # MI100
                ]}
    - name: amd-vgpu
      labels:
        feature.node.kubernetes.io/amd-vgpu: "true"
      matchAny:
        - matchFeatures:
            - feature: pci.device
              matchExpressions:
                vendor: {op: In, value: ["1002"]}
                device: {op: In, value: [
                  "75b3", # MI355X VF
                  "75b0", # MI350X VF
                  "74b9", # MI325X VF
                  "74b6", # MI308X VF
                  "74bc", # MI308X-HF VF
                  "74b5", # MI300X VF
                  "74bd", # MI300X-HF VF
                  "7410"  # MI210 VF
                ]}
```

Finally please verify the NFD label is applied:

```bash
oc get node -o yaml | grep "amd-gpu"
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

### 3. Create DeviceConfig

Create a DeviceConfig CR to trigger the GPU driver installation:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: test-cr
  namespace: openshift-amd-gpu
spec:
  driver:
    enable: true
    image: image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amdgpu_kmod
    # NOTE: Starting from ROCm 7.1 the amdgpu version is using new versioning schema
    # please refer to https://rocm.docs.amd.com/projects/install-on-linux/en/latest/reference/user-kernel-space-compat-matrix.html
    version: 30.20.1
  selector:
    "feature.node.kubernetes.io/amd-gpu": "true"
```

Things to note:
1. By default, there is no need to specify the image field in CR for Openshift. Default will be used which is: image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amdgpu_kmod

2. If users specify image, $MOD_NAMESPACE can be a place holder , KMM Operator can automatically translate it to the namespace

3. Openshift internal registry has image url restriction, OpenShift users cannot use image like `<registry URL>/<repo name>` , it requires the image URL to be `<registry URL>/<project name or namespace>/<repo name>`. However, if any other registry is being used by the user, the image URL can be of either form.

The operator will:

1. Collect worker node system specifications
2. Build or retrieve the appropriate driver image
3. Deploy the driver using KMM
4. Deploy the ROCM device plugin and node labeller

Verify the deployment:

```bash
# Check KMM worker status
oc get pods | grep kmm-worker

# Check device plugin and labeller status
oc get pods | grep test-cr

# Verify GPU resource labels
oc get node -o json | grep amd.com
```

### 4. Enable Cluster Monitoring

In order to enable the OpenShift native cluster monitoring stack to scrape metrics from metrics exporter, please:

* Label the namespace with OpenShift specific cluster monitoring label

For example if AMD GPU Operator was deployed in namespace `openshift-amd-gpu`:

```bash
oc label namespace openshift-amd-gpu openshift.io/cluster-monitoring="true"
```

* Enable the metrics exporter and configure the `serviceMonitor` in `DeviceConfig`

For example:

```yaml
spec:
  metricsExporter:
    enable: true
    prometheus:
      serviceMonitor:
        enable: true
        interval: "60s" # Metrics scrape interval
        attachMetadata:
          node: true
```

After applying this configuration, verify the metrics are being collected:

* Navigate to the OpenShift web console
* Go to **Observe** → **Targets** to confirm the metrics target is active
* Go to **Observe** → **Metrics** to query AMD GPU metrics

## Uninstallation

Please refer to the [Uninstallation](../uninstallation/uninstallation) document for uninstalling related resources.
