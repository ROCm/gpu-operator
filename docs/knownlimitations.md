# Known Issues and Limitations

1. **GPU operator driver installs only DKMS package**
   - *****Impact:***** Applications which require ROCM packages will need to install respective packages.
   - ***Affectioned Configurations:*** All configurations
   - ***Workaround:*** None as this is the intended behaviour
</br></br>

2. **When Using Operator to install amdgpu 6.1.3/6.2 a reboot is required to complete install**
   - ***Impact:*** Node requires a reboot when upgrade is initiated due to ROCm bug. Driver install failures may be seen in dmesg
   - ***Affected configurations:*** Nodes with driver version >= ROCm 6.2.x
   - ***Workaround:*** Reboot the nodes upgraded manually to finish the driver install. This has been fixed in ROCm 6.3+
</br></br>

3. **GPU Operator unable to install amdgpu driver if existing driver is already installed**
   - ***Impact:*** Driver install will fail if amdgpu in-box Driver is present/already installed
   - ***Affected Configurations:*** All configurations
   - ***Workaround:*** When installing the amdgpu drivers using the GPU Operator, worker nodes should have amdgpu blacklisted or amdgpu drivers should not be pre-installed on the node. [Blacklist in-box driver](https://instinct.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/drivers/installation.html#blacklist-inbox-driver) so that it is not loaded or remove the pre-installed driver
</br></br>

4. **When GPU Operator is used in SKIP driver install mode, if amdgpu module is removed with device plugin installed it will not reflect active GPU available on the server**
   - ***Impact:*** Scheduling Workloads will have impact as it will scheduled on nodes which does have active GPU.
   - ***Affected Configurations:*** All configurations
   - ***Workaround:*** Restart the Device plugin pod deployed.
</br></br>

5. **Worker nodes where Kernel needs to be upgraded needs to taken out of the cluster and readded with Operator installed**
   - ***Impact:*** Node upgrade will not proceed automatically and requires manual intervention
   - ***Affected Configurations:*** All configurations
   - ***Workaround:*** Manually mark the node as unschedulable, preventing new pods from being scheduled on it, by cordoning it off:

   ```bash
   kubectl cordon <node-name>
   ```

   </br></br>

6. **Due to issue with KMM 2.2 deletion of DeviceConfig Custom Resource gets stuck in Red Hat OpenShift**
   - ***Impact:*** Not able to delete the DeviceConfig Custom Resource if the node reboots during uninstall.
   - ***Affected Configurations:*** This issue only affects Red Hat OpenShift
   - ***Workaround:*** This issue will be fixed in the next release of KMM. For the time being you can use a previous version of KMM aside from 2.2 or manually remove the status from NMC:
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

7. **Incomplete Cleanup on Manual Module Removal**
   - **Impact:** When AMD GPU drivers are manually removed (instead of using the operator for uninstallation), not all GPU modules are cleaned up completely.
   - **Recommendation:** Always use the GPU Operator for installing and uninstalling drivers to ensure complete cleanup.
</br></br>

8. **Inconsistent Node Detection on Reboot**
   - **Impact:** In some reboot sequences, Kubernetes fails to detect that a worker node has reloaded, which prevents the operator from installing the updated driver. This happens as a result of the node rebooting and coming back online too quickly before the default time check interval of 50s.
   - **Recommendation:** Consider tuning the kubelet and controller-manager flags (such as --node-status-update-frequency=10s, --node-monitor-grace-period=40s, and --node-monitor-period=5s) to improve node status detection. Refer to [Kubernetes documentation](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/) for more details.
</br></br>

9. **Inconsistent Metrics Fetch Using NodePort**
   - **Impact:** When accessing metrics via a Nodeport service (NodeIP:NodePort) from within a cluster, Kuberntes' built-in load balancing may sometimes route requests to different pods, leading to occasional inconsistencies in the returned metrics. This behavior is inherent to Kubernetes networking and is not a defect in the GPU Operator.
   - **Recommendation:** Only use the internal PodIP and configured pod port (default: 5000) when retrieving metrics from within the cluster instead of the NodePort. Refer to the Metrics Exporter document section for more details.
</br></br>

10. **Helm Install Fails if GPU Operator Image is Unavailable**
    - **Impact:** If the image provided via --set controllerManager.manager.image.repository and --set controllerManager.manager.image.tag does not exist, the controller manager pod may enter a CrashLoopBackOff state and hinder uninstallation unless --no-hooks is used.
    - **Recommendation:** Ensure that the correct GPU Operator controller image is available in your registry before installation. To uninstall the operator after seeing a *ErrImagePull* error, use --no-hooks to bypass all pre-uninstall helm hooks.
</br></br>

11. **Driver Reboot Requirement with ROCm 6.3.x**
    - **Impact:** While using ROCm 6.3+ drivers, the operator may not complete the driver upgrade properly unless a node reboot is performed.
    - **Recommendation:** Manually reboot the affected nodes after the upgrade to complete driver installation. Alternatively, we recommend setting rebootRequired to true in the upgrade policy for driver upgrades. This ensures that a reboot is triggered after the driver upgrade, guaranteeing that the new driver is fully loaded and applied. This workaround should be used until the underlying issue is resolved in a future release.
</br></br>

12. **Driver Upgrade Timing Issue**

    - **Impact:** During an upgrade, if a node's ready status fluctuates (e.g., from Ready to NotReady to Ready) before the driver version label is updated by the operator, the old driver might remain installed. The node might continue running the previous driver version even after an upgrade has been initiated.
    - **Recommendation:** Ensure nodes are fully stable before triggering an upgrade, and if necessary, manually update node labels to enforce the new driver version. Refer to driver upgrade documentation for more details.
</br></br>

## Fixed Issues

1. **When GPU Operator is installed with Exporter enabled, upgrade of driver is blocked as exporter is actively using the amdgpu module <span style="color:red">(Fixed in v1.2.0)</span>**
   - ***Impact:*** Driver upgrade is blocked
   - ***Affected Configurations:*** All configurations
   - ***Workaround:*** Disable the Metrics Exporter on specific node to allow driver upgrade as follows:

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

</br>
