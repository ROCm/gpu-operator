# Release Notes

## GPU Operator v1.2.2 Release Notes

The AMD GPU Operator v1.2.2 release introduces new features to support Device Metrics Exporter's integration with [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) from ServiceMonitor custom resource and also introduces several bug fixes.

### Release Highlights

- **Enhanced Metrics Integration with Prometheus Operator**
  
  This release introduces a streamlined method for integrating the metrics endpoint of the metrics exporter with the Prometheus Operator.

  Users can now leverage the `DeviceConfig` custom resource to specify the necessary configuration for metrics collection. The GPU Operator will automatically read the relevant `DeviceConfig` and manage the creation and lifecycle of a corresponding ServiceMonitor custom resource.

  This automation simplifies the process of exposing metrics to the Prometheus Operator, allowing for easier scraping and monitoring of GPU-related metrics within your Kubernetes environment.

### Documentation Updates

- Updated [Release notes](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/releasenotes.html) detailing new features in v1.2.2.

### Known Limitations

> **Note:** All current and historical limitations for the GPU Operator, including their latest statuses and any associated workarounds or fixes, are tracked in the following documentation page: [Known Issues and Limitations](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html).  
   Please refer to this page regularly for the most up-to-date information.

### Fixes

1. **Node labeller failed to report node labels when users are using `DeviceConfig` with `spec.driver.enable=false` and customized node selector in `spec.selector`** [[#183]](https://github.com/ROCm/gpu-operator/issues/183)
   - *Issue*: When users are using inbox driver, they will set `spec.driver.enable=false` within the `DeviceConfig` spec. If they are also using customized node selector in `spec.selector`, once node labeller was brought up its GPU properties labels are not showing up among Node resource labels.
   - *Root Cause*: When users are using `spec.driver.enable=false` and customized non-default selector `spec.selector`, the operator controller manager is using the wrong selector to clean up node labeller's labels on non-GPU nodes.
   - *Resolution*: This issue has been fixed in v1.2.2. Users can upgrade to v1.2.2 and GPU properties node labels will show up once node labeller was brought up again.

2. **Users self-defined node labels under domain `amd.com` are unexpectly removed** [[#151]](https://github.com/ROCm/gpu-operator/issues/151)
   - *Issue*: When users created some node labels under amd.com domain (e.g. amd.com/gpu: "true") for their own usage, it is unexpectly getting removed during bootstrapping.
   - *Root Cause*:
     - When node labeller pod launched it will remove all node labels within `amd.com` and `beta.amd.com` from current node then post the labels managed by itself.
     - When operator is executing the reconcile function, the removal of `DevicePlugin` or will remove all node labels under `amd.com` or `beta.amd.com` domain even if they are not managed by node labeller.
   - *Resolution*: This issue has been fixed in v1.2.2 for both operator and node labeller side. Users can upgrade to v1.2.2 operator helm chart and use latest node labeller image then only node labeller managed labels will be auto removed. Other users defined labels under `amd.com` or `beta.amd.com` won't be auto removed by operator or node labeller. 

3. **During automatic driver upgrade nodes can get stuck in reboot-in-progress**
   - *Issue*: When users upgrade the driver version by using `DeviceConfig` automatic upgrade feature with `spec.driver.upgradePolicy.enable=true` and `spec.driver.upgradePolicy.rebootRequired=true`, some nodes may get stuck at reboot-in-progress state.
   - *Root Cause*:
     - Upgrademgr was checking the generationID of `DeviceConfig` to make sure any spec change during upgrade won't interfere existing upgrade. But if CR changes even for other parts of the device config spec which are unrelated to upgrade, this check will be a problem as new driver upgrade will not start for unrelated CR changes.
     - During the driver upgrade when node reboot happened, the controller manager pod could also get affected and rescheduled to another node. When it comes back, in the init phase, it checks for reboot-in-progress and attempts to delete reboot pod. But it is possible that reboot pod has terminated by then already.
   - *Resolution*: The controller manager's upgrade manager module implementation has been patched to fix this issue in release v1.2.2, by upgrading to new controller manager image this issue should have been fixed.

</br></br>

## GPU Operator v1.2.1 Release Notes

The AMD GPU Operator v1.2.1 release introduces expanded platform support and new features to enhance GPU workload management. Notably, this release adds support for OpenShift and Microsoft Azure Kubernetes Service (AKS), and introduces two new **beta features**:

- **Exporting test runner logs to external storage** (AWS S3, Azure Blob, MinIO) for improved audit and analysis workflows.
- **Custom labels for exported metrics** to enhance observability and workload tagging.

**Note:** These beta features are intended for early access and feedback. Users are encouraged to evaluate them in non-production environments and provide feedback to help shape their evolution in future stable releases.

### Release Highlights

- **Expanded Platform Support**
  - *OpenShift Support*:
  
    - All GPU Operator features introduced in v1.2.0 and v1.2.1 are now available for OpenShift versions 4.16, 4.17, and 4.18.
    - Users running GPU Operator v1.1.1 can seamlessly upgrade to v1.2.1 using the OpenShift OperatorHub. The upgrade path ensures continuity of existing features while enabling access to new enhancements introduced in this release.

  - *Microsoft AKS Support*:

    - The GPU Operator now supports deployment on Microsoft Azure Kubernetes Service (AKS), enabling users to manage GPU workloads on Azure.

- **Exporting Test Runner Logs to External Storage (Beta)**
  - The test runner can now export logs to external storage solutions such as AWS S3, Azure Blob Storage, and MinIO.
  - This feature allows users to store and analyze test results more effectively, facilitating improved diagnostics and auditing.

- **Support for Custom Labels in Metrics (Beta)**
  - Users can now add up to 10 custom labels to the metrics exported by the GPU Operator.
  - Custom labels provide enhanced flexibility and control over the monitoring and management of GPU workloads.
  - *Limitations*:
    - Custom labels cannot overwrite automatically generated labels, except for the `CLUSTER_NAME` label, which can be customized.

### Platform Support

- **OpenShift Support**
  - Full support for OpenShift versions 4.16, 4.17, and 4.18, including features such as GPU health monitoring, automated component and driver upgrades, and the test runner.

- **Microsoft AKS Support**
  - Support for deploying and managing GPU workloads on Microsoft Azure Kubernetes Service (AKS), encompassing all features introduced in previous releases.

### Documentation Updates

- Updated [Release notes](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/releasenotes.html) detailing new features in v1.2.1.
- Resolved GitHub issue [[#93]](https://github.com/ROCm/gpu-operator/issues/93) related to blacklisting in-tree drivers when creating MachineConfig manually.
- Enhanced documentation for the new feature of exporting test runner logs to external storage solutions, including detailed configuration instructions and examples of supported storage providers.
- Updated [Known Issues and Limitations](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html) section to highlight current limitations and resolved issues.
- A comprehensive list of known issues, feature requests, and enhancements under consideration can be found on the official [GPU Operator GitHub Issues page](https://github.com/ROCm/gpu-operator/issues). The AMD team actively monitors and prioritizes these issues for future GPU Operator releases. Users are encouraged to review, comment on, or open issues to help guide ongoing development and improvements.

### Known Limitations

1. **Pods Terminate on GPU Worker Node Reboot During Driver Upgrade**

    - *Impact:* During the GPU driver upgrade process, a node reboot is intentionally triggered to complete the driver installation. However, an issue may arise if the node responsible for building the driver reboots unintentionally before the build is completed and propagated to other nodes. This premature or unintentional reboot disrupts the build process, potentially causing pod terminations on that node and preventing successful upgrades across the cluster.
    - *Root Cause:* The GPU Operator initiates the node reboot only after the driver build has been completed. However, if the node reboots unexpectedly while still performing the build (e.g., due to external triggers or misconfigurations), it can interrupt the process and affect cluster-wide upgrade stability.
    - *Recommendation:* Users should ensure the node performing the driver build remains stable and does not reboot unintentionally during the upgrade process. A targeted fix to address this specific scenario is planned for a future release.

2. **Driver Upgrade Fails with `rebootRequired: true` and `maxParallelUpgrades` Equal to All Workers**
   - *Issue*: When the GPU Operator was configured with `rebootRequired: true` and `maxParallelUpgrades` set to the total number of worker nodes, the driver upgrade process would fail.
   - *Root Cause*: Upgrading all nodes simultaneously adds taints to all of them, causing the image registry pod to be unschedulable, which in turn causes issues for the driver upgrade.
   - *Recommendation*: Avoid setting `maxParallelUpgrades` equal to the total number of worker nodes. For example, in a cluster with two worker (GPU) nodes, set `maxParallelUpgrades` to 1 to avoid this situation.

> **Note:** All current and historical limitations for the GPU Operator, including their latest statuses and any associated workarounds or fixes, are tracked in the following documentation page: [Known Issues and Limitations](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html).  
   Please refer to this page regularly for the most up-to-date information.

### Fixes

1. **Failure to Blacklist In-Tree Driver When Creating MachineConfig Manually** [[#93]](https://github.com/ROCm/gpu-operator/issues/93)
   - *Issue*: When creating a MachineConfig manually, the GPU Operator failed to blacklist the in-tree driver, as it kept deleting the `/etc/modprobe.d/blacklist-amdgpu.conf` file.
   - *Root Cause*: OpenShift's MachineConfigOperator (MCO) fully manages the CoreOS system’s configuration. Users should use MCO to configure blacklists.
   - *Resolution*: OpenShift users should apply blacklist configurations through MCO. The GPU Operator will no longer delete files created by MCO.

</br></br>

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
