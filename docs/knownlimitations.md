# Known Issues and Limitations

1. **GPU operator driver installs only DKMS package**
   - *****Impact:***** Applications which require ROCM packages will need to install respective packages.
   - ***Affected Configurations:*** All configurations
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
