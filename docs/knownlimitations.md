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
   - ***Workaround:*** When installing the amdgpu drivers using the GPU Operator, worker nodes should have amdgpu blacklisted or amdgpu drivers should not be pre-installed on the node. [Blacklist in-box driver](https://dcgpu.docs.amd.com/projects/gpu-operator/en/release-v1.0.0/drivers/installation.html#blacklist-inbox-driver) so that it is not loaded or remove the pre-installed driver
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

</br>

6. **When GPU Operator is installed with Exporter enabled, upgrade of driver is blocked as exporter is actively using the amdgpu module**
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

7. **Due to issue with KMM 2.2 deletion of DeviceConfig Custom Resource gets stuck in Red Hat OpenShift**
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

</br>

8. **Driver Upgrade Issue when maxParallel Upgrades is equal to total number of worker nodes in Red Hat OpenShift**
   - ***Impact:*** Not able to perform driver upgrade
   - ***Affected Configurations:*** This issue only affects Red Hat OpenShift when Image registry pod is running on one of the worker nodes or kmm build pod is required to be run on one of the worker nodes
   - ***Workaround:*** Please set maxParallel Upgrades to a number less than total number of worker nodes

</br>

9. **Driver Install/Upgrade Issue if one of the nodes where KMM is running build pod gets rebooted accidentally when rebootRequired is set to false**
   - ***Impact:*** Not able to perform driver install/upgrade
   - ***Affected Configurations:*** All configurations
   - ***Workaround:*** Please retrigger driver install/upgrade and ensure to not reboot node manually when rebootRequired is false

10. **The Device Config Manager requires running a docker container if you wish to run it in standalone mode (without Kubernetes).**

    - *Impact:* Users wishing to use a standalone version of the Device Config Manager will need to run a standalone docker image and configure the partitions using config.json file.
    - *Root Cause:* DCM does not currently support standalone installation via a Debian package like other standalone components of the GPU Operator. We will be adding a Debian package to support standalone bare metal installations in the next release of DCM.
    - *Recommendation:* Those wishing to use GPU partitioning in a bare metal environment should instead use the standalone docker image for DCM. Alternatively users can use amd-smi to change partitioning modes. See [amdgpu-docs documentation](https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/mi300x/quick-start-guide.html) for how to do this.

11. **The GPU Operator will report an error when ROCm driver install version doesn't match the version string in the [Radeon Repo](https://repo.radeon.com/rocm/apt/).**

    - *Impact:* The DeviceConfig will report an error if you specify `"6.4.0"` or `"6.3.0"` for the `spec.driver.version`.
    - *Root Cause:* The version specified in the CR would still have to match the version string on Radeon repo.
    - *Recommendation:* Although this will be fixed in a future version of the GPU Operator, for the time being you will instead need to specific `"6.4"` or `"6.3"` when installing those versions of the ROCm amdgpu driver.