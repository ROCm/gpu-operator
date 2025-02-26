# Release Notes

## GPU Operator v1.2.0 Release Notes

The GPU Operator v1.2.0 release introduces significant new features, including **GPU health monitoring**, **automated component and driver upgrades**, and a **test runner** for enhanced validation and troubleshooting. These improvements aim to increase reliability, streamline upgrades, and provide enhanced visibility into GPU health.

### Release Highlights

- **GPU Health Monitoring**
  - Real-time health checks via **metrics exporter**
  - Integration with **Kubernetes Device Plugin** for automatic removal of unhealthy GPUs from compute node schedulable resources
  - Customizable health thresholds via K8s ConfigMaps

- **GPU Operator and Automated Driver Upgrades**
  - Automatic and manual upgrades of the device plugin, node labeller, test runner and metrics exporter via configurable upgrade policies
  - Automatic driver upgrades is now supported with node cordon, drain, version tracking and optional node reboot

- **Test Runner for GPU Diagnostics**
  - Automated testing of unhealthy GPUs
  - Pre-start job tests embedded in workload pods
  - Manual and scheduled GPU tests with event logging and result tracking

### Platform Support

- No new platform support has been added in this release. While the GPU Operator now supports OpenShift 4.17, the newly introduced features in this release (GPU Health Monitoring, Automatic Driver & Component Upgrade, and Test Runner) are currently only available for vanilla Kubernetes deployments. These features are not yet supported on OpenShift, and OpenShift support will be introduced in the next minor release.

### Known Limitations

1. **Incomplete Cleanup on Manual Module Removal**
   - *Impact:* When AMD GPU drivers are manually removed (instead of using the operator for uninstallation), not all GPU modules are cleaned up completely.
   - *Recommendation:* Always use the GPU Operator for installing and uninstalling drivers to ensure complete cleanup.

2. **Inconsistent Node Detection on Reboot**
   - *Impact:* In some reboot sequences, Kubernetes fails to detect that a worker node has reloaded, which prevents the operator from installing the updated driver. This happens as a result of the node rebooting and coming back online too quickly before the default time check interval of 50s.
   - *Recommendation:* Consider tuning the kubelet and controller-manager flags (such as --node-status-update-frequency=10s, --node-monitor-grace-period=40s, and --node-monitor-period=5s) to improve node status detection. Refer to [Kubernetes documentation](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/) for more details.

3. **Inconsistent Metrics Fetch Using NodePort**
   - *Impact:* When accessing metrics via a Nodeport service (NodeIP:NodePort) from within a cluster, Kubernetes' built-in load balancing may sometimes route requests to different pods, leading to occasional inconsistencies in the returned metrics. This behavior is inherent to Kubernetes networking and is not a defect in the GPU Operator.
   - *Recommendation:* Only use the internal PodIP and configured pod port (default: 5000) when retrieving metrics from within the cluster instead of the NodePort. Refer to the Metrics Exporter document section for more details.

4. **Helm Install Fails if GPU Operator Image is Unavailable**
   - *Impact:* If the image provided via --set controllerManager.manager.image.repository and --set controllerManager.manager.image.tag does not exist, the controller manager pod may enter a CrashLoopBackOff state and hinder uninstallation unless --no-hooks is used.
   - *Recommendation:* Ensure that the correct GPU Operator controller image is available in your registry before installation. To uninstall the operator after seeing a *ErrImagePull* error, use --no-hooks to bypass all pre-uninstall helm hooks.

5. **Driver Reboot Requirement with ROCm 6.3.x**
   - *Impact:* While using ROCm 6.3+ drivers, the operator may not complete the driver upgrade properly unless a node reboot is performed.
   - *Recommendation:* Manually reboot the affected nodes after the upgrade to complete driver installation. Alternatively, we recommend setting rebootRequired to true in the upgrade policy for driver upgrades. This ensures that a reboot is triggered after the driver upgrade, guaranteeing that the new driver is fully loaded and applied. This workaround should be used until the underlying issue is resolved in a future release.

6. **Driver Upgrade Timing Issue**
   - *Impact:* During an upgrade, if a node's ready status fluctuates (e.g., from Ready to NotReady to Ready) before the driver version label is updated by the operator, the old driver might remain installed. The node might continue running the previous driver version even after an upgrade has been initiated.
   - *Recommendation:* Ensure nodes are fully stable before triggering an upgrade, and if necessary, manually update node labels to enforce the new driver version. Refer to driver upgrade documentation for more details.

### Fixes

1. **Driver Upgrade Failure with Exporter Enabled**
   - Previously, enabling the exporter alongside the operator caused driver upgrades to fail.
   - *Status:* This issue has been fixed in v1.2.0.

</br></br>

## GPU Operator v1.1.0 Release Notes

The GPU Operator v1.1.0 release adds support for Red Hat OpenShift versions 4.16 and 4.17. The AMD GPU Operator has gone through a rigourous validation process and is now *certified* for use on OpenShift. It can now be deployed via [the Red Hat Catalog](https://catalog.redhat.com/software/container-stacks/detail/6722781e65e61b6d4caccef8).

```{note}
The latest AMD GPU Operator OLM Bundle for OpenShift is tagged with version v1.1.1 as the operator image has been updated to include a minor driver fix.
```

### Release Highlights

- The AMD GPU Operator has now been certified for use with Red Hat OpenShift v4.16 and v4.17
- Updated documentation with installationa and configuration steps for Red Hat OpenShift

### Platform Support

#### New Platform Support

- **Red Hat OpenShift 4.16-4.17**
  - Supported features:
    - Driver management
    - Workload scheduling
    - Metrics monitoring
  - Requirements: Red Hat OpenShift version 4.16 or 4.17
</br>

### Known Limitations

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

## GPU Operator v1.0.0 Release Notes

This release is the first major release of AMD GPU Operator. The AMD GPU Operator simplifies the deployment and management of AMD Instinct™ GPU accelerators within Kubernetes clusters. This project enables seamless configuration and operation of GPU-accelerated workloads, including machine learning, Generative AI, and other GPU-intensive applications.

### Release Highlights

- Manage AMD GPU drivers with desired versions on Kubernetes cluster nodes
- Customized scheduling of AMD GPU workloads within Kubernetes cluster
- Metrics and statistics monitoring solution for AMD GPU hardware and workloads
- Support specialized networking environment like HTTP proxy or Air-gapped network

### Hardware Support

#### New Hardware Support

- **AMD Instinct™ MI300**
  - Required driver version: ROCm 6.2+

- **AMD Instinct™ MI250**
  - Required driver version: ROCm 6.2+

- **AMD Instinct™ MI210**
  - Required driver version: ROCm 6.2+

#### Platform Support

#### New Platform Support

- **Kubernetes 1.29+**
  - Supported features:
    - Driver management
    - Workload scheduling
    - Metrics monitoring
  - Requirements: Kubernetes version 1.29+

#### Breaking Changes

Not Applicable as this is the initial release.

#### New Features

##### Feature Category

- **Driver management**
  - *Managed Driver Installations:* Users will be able to install ROCm 6.2+ dkms driver on Kubernetes worker nodes, they can also optionally choose to use inbox or pre-installed driver on the worker nodes
  - *DeviceConfig Custom Resource:* Users can configure a new DeviceConfig CRD (Custom Resource Definition) to define the driver management behavior of the GPU Operator

- **GPU Workload Scheduling**
  - *Custom Resource Allocation "amd.com/gpu":* After the deployment of the GPU Operator a new custom resource allocation will be present on each GPU node, `amd.com/gpu`, which will list the allocatable GPU resources on the node for which GPU workloads can be scheduled against
  - *Assign Multiple GPUs:* Users can easily specify the number of AMD GPUs required by each workload in the [deployment/pod spec](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/usage.html#creating-a-gpu-enabled-pod) and the Kubernetes scheduler wiill automatically take care of assigning the correct GPU resources

- **Metrics Monitoring for GPUs and Workloads**:
  - *Out-of-box Metrics:* Users can optionally enable the AMD Device Metrics Exporter when installing the AMD GPU Operator to enable a robust out-of-box monitoring solution for prometheus to consume
  - *Custom Metrics Configurations:* Users can utilize a [configmap](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/metrics/exporter.html#configure-metrics-exporter) to customize the configuration and behavior of Device Metrics Exporter

- **Specialized Network Setups**:
  - *Air-gapped Installation:* Users can install the GPU Operator in a secure [air-gapped environment](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/specialized_networks/airgapped-install.html) where the Kubernetes cluster has no external network connectivity
  - *HTTP Proxy Support:* The AMD GPU Operator supports usage within a Kubernetes cluster that is behind an [HTTP Proxy](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/specialized_networks/http-proxy.html). Support for HTTPS Proxy will be added in a future version of the GPU Operator.

### Known Limitations

- **GPU operator driver installs only DKMS package**
  - *Impact:* Applications which require ROCM packages will need to install respective packages.
  - *Affected Configurations:* All configurations
  - *Workaround:* None as this is the intended behaviour

- **When Using Operator to install amdgpu 6.1.3/6.2 a reboot is required to complete install**
  - *Impact:* Node requires a reboot when upgrade is initiated due to ROCm bug. Driver install failures may be seen in dmesg
  - *Affected configurations:* Nodes with driver version >= ROCm 6.2.x
  - *Workaround:* Reboot the nodes upgraded manually to finish the driver install. This has been fixed in ROCm 6.3+

- **GPU Operator unable to install amdgpu driver if existing driver is already installed**
  - *Impact:* Driver install will fail if amdgpu in-box Driver is present/already installed
  - *Affected Configurations:* All configurations
  - *Workaround:* When installing the amdgpu drivers using the GPU Operator, worker nodes should have amdgpu blacklisted or amdgpu drivers should not be pre-installed on the node. [Blacklist in-box driver](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/drivers/installation.html#blacklist-inbox-driver) so that it is not loaded or remove the pre-installed driver

- **When GPU Operator is used in SKIP driver install mode, if amdgpu module is removed with device plugin installed it will not reflect active GPU available on the server**
  - *Impact:* Scheduling Workloads will have impact as it will scheduled on nodes which does have active GPU.
  - *Affected Configurations:* All configurations
  - *Workaround:* Restart the Device plugin pod deployed.

- **Worker nodes where Kernel needs to be upgraded needs to taken out of the cluster and readded with Operator installed**
  - *Impact:* Node upgrade will not proceed automatically and requires manual intervention
  - *Affected Configurations:* All configurations
  - *Workaround:* Manually mark the node as unschedulable, preventing new pods from being scheduled on it, by cordoning it off:

  ```bash
  kubectl cordon <node-name>
  ```

- **When GPU Operator is installed with Exporter enabled, upgrade of driver is blocked as exporter is actively using the amdgpu module**
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
