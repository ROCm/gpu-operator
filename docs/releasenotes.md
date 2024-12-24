# GPU Operator v1.1.0 Release Notes

The GPU Operator v1.1.0 release adds support for Red Hat OpenShift versions 4.16 and 4.17. The AMD GPU Operator has gone through a rigourous validation process and is now *certified* for use on OpenShift. It can now be deployed via [the Red Hat Catalog](https://catalog.redhat.com/software/container-stacks/detail/6722781e65e61b6d4caccef8).

```{note}
The latest AMD GPU Operator OLM Bundle for OpenShift is tagged with version v1.1.1 as the operator image has been updated to include a minor driver fix.
```

## Release Highlights

- The AMD GPU Operator has now been certified for use with Red Hat OpenShift v4.16 and v4.17
- Updated documentation with installationa and configuration steps for Red Hat OpenShift

## Platform Support

### New Platform Support

- **Red Hat OpenShift 4.16-4.17**
  - Supported features:
    - Driver management
    - Workload scheduling
    - Metrics monitoring
  - Requirements: Red Hat OpenShift version 4.16 or 4.17
</br>

## Known Limitations

1. **Due to issue with KMM 2.2 deletion of DeviceConfig Custom Resource gets stuck in Red Hat OpenShift**
   - *Impact:* Not able to delete the DeviceConfig Custom Resource if the node reboots during uninstall.
   - *Affected Configurations:* This issue only affects Red Hat OpenShift
   - *Workaround:* This issue will be fixed in the next release of KMM. For the time being you can use a previous version of KMM aside from 2.2 or manually remove the status from NMC:
    1. List all the NMC resources and pick up the correct NMC (there is one nmc per node, named the same as the node it related to).

        ```bash
        oc get nmc -A
        ```

    2. Edit the NMC.

        ```bash
        oc edit nmc <nmc name>
        ```

    3. Remove from NMC status for all the data related to your module and save. That should allow the module to be finally deleted.

</br></br>

# GPU Operator v1.0.0 Release Notes

This release is the first major release of AMD GPU Operator. The AMD GPU Operator simplifies the deployment and management of AMD Instinct™ GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

## Release Highlights

- Manage AMD GPU drivers with desired versions on Kubernetes cluster nodes
- Customized scheduling of AMD GPU workloads within Kubernetes cluster
- Metrics and statistics monitoring solution for AMD GPU hardware and workloads
- Support specialized networking environment like HTTP proxy or Air-gapped network

## Hardware Support

### New Hardware Support

- **AMD Instinct™ MI300**
  - Required driver version: ROCm 6.2+

- **AMD Instinct™ MI250**
  - Required driver version: ROCm 6.2+

- **AMD Instinct™ MI210**
  - Required driver version: ROCm 6.2+

## Platform Support

### New Platform Support

- **Kubernetes 1.29+**
  - Supported features:
    - Driver management
    - Workload scheduling
    - Metrics monitoring
  - Requirements: Kubernetes version 1.29+

## Breaking Changes

Not Applicable as this is the initial release.
</br>

## New Features

### Feature Category

- **Driver management**
  - *Managed Driver Installations:* Users will be able to install ROCm 6.2+ dkms driver on Kubernetes worker nodes, they can also optionally choose to use inbox or pre-installed driver on the worker nodes
  - *DeviceConfig Custom Resource:* Users can configure a new DeviceConfig CRD (Custom Resource Definition) to define the driver management behavior of the GPU Operator

- **GPU Workload Scheduling**
  - *Custom Resource Allocation "amd.com/gpu":* After the deployment of the GPU Operator a new custom resource allocation will be present on each GPU node, `amd.com/gpu`, which will list the allocatable GPU resources on the node for which GPU workloads can be scheduled against
  - *Assign Multiple GPUs:* Users can easily specify the number of AMD GPUs required by each workload in the [deployment/pod spec](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/usage.html#creating-a-gpu-enabled-pod) and the Kubernetes scheduler wiill automatically take care of assigning the correct GPU resources

- **Metrics Monitoring for GPUs and Workloads**:
  - *Out-of-box Metrics:* Users can optionally enable the AMD Device Metrics Exporter when installing the AMD GPU Operator to enable a robust out-of-box monitoring solution for prometheus to consume
  - *Custom Metrics Configurations:* Users can utilize a [configmap](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/metrics/exporter.html#configure-metrics-exporter) to customize the configuration and behavior of Device Metrics Exporter

- **Specialized Network Setups**:
  - *Air-gapped Installation:* Users can install the GPU Operator in a secure [air-gapped environment](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/specialized_networks/airgapped-install.html) where the Kubernetes cluster has no external network connectivity
  - *HTTP Proxy Support:* The AMD GPU Operator supports usage within a Kubernetes cluster that is behind an [HTTP Proxy](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/specialized_networks/http-proxy.html). Support for HTTPS Proxy will be added in a future version of the GPU Operator.

## Known Limitations

1. **GPU operator driver installs only DKMS package**
   - *Impact:* Applications which require ROCM packages will need to install respective packages.
   - *Affected Configurations:* All configurations
   - *Workaround:* None as this is the intended behaviour

2. **When Using Operator to install amdgpu 6.1.3/6.2 a reboot is required to complete install**
   - *Impact:* Node requires a reboot when upgrade is initiated due to ROCm bug. Driver install failures may be seen in dmesg
   - *Affected configurations:* Nodes with driver version >= ROCm 6.2.x
   - *Workaround:* Reboot the nodes upgraded manually to finish the driver install. This has been fixed in ROCm 6.3+

3. **GPU Operator unable to install amdgpu driver if existing driver is already installed**
   - *Impact:* Driver install will fail if amdgpu in-box Driver is present/already installed
   - *Affected Configurations:* All configurations
   - *Workaround:* When installing the amdgpu drivers using the GPU Operator, worker nodes should have amdgpu blacklisted or amdgpu drivers should not be pre-installed on the node. [Blacklist in-box driver](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/drivers/installation.html#blacklist-inbox-driver) so that it is not loaded or remove the pre-installed driver

4. **When GPU Operator is used in SKIP driver install mode, if amdgpu module is removed with device plugin installed it will not reflect active GPU available on the server**
   - *Impact:* Scheduling Workloads will have impact as it will scheduled on nodes which does have active GPU.
   - *Affected Configurations:* All configurations
   - *Workaround:* Restart the Device plugin pod deployed.

5. **Worker nodes where Kernel needs to be upgraded needs to taken out of the cluster and readded with Operator installed**
   - *Impact:* Node upgrade will not proceed automatically and requires manual intervention
   - *Affected Configurations:* All configurations
   - *Workaround:* Manually mark the node as unschedulable, preventing new pods from being scheduled on it, by cordoning it off:

    ```bash
    kubectl cordon <node-name>
    ```

6. **When GPU Operator is installed with Exporter enabled, upgrade of driver is blocked as exporter is actively using the amdgpu module**
   - *Impact:* Driver upgrade is blocked
   - *Affected Configurations:* All configurations
   - *Workaround:* Disable the Metrics Exporter on specific node to allow driver upgrade as follows:

    1. Label all nodes with new label:

       ```bash
       kubectl label nodes --all amd.com/device-metrics-exporter=true
       ```

    2. Patch DeviceConfig to include new selectors for metrics exporter:

        ```bash
        kubectl patch deviceconfig gpu-operator -n kube-amd-gpu --type='merge' -p {"spec":{"metricsExporter":{"selector":{"feature.node.kubernetes.io/amd-gpu":"true","amd.com/device-metrics-exporter":"true"}}}}'
        ```
  
    3. Remove the amd.com/device-metrics-exporter label for the specific node you would like to disable the exporter on:

        ```bash
        kubectl label node [node-to-exclude] amd.com/device-metrics-exporter-
        ```
