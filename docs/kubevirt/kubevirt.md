# KubeVirt Integration

## Overview

The AMD GPU Operator now supports integration with [**KubeVirt**](https://kubevirt.io/), enabling virtual machines (VMs) running in Kubernetes to access AMD Instinct GPUs. This feature extends GPU acceleration capabilities beyond containers to virtualized workloads, making it ideal for hybrid environments that require both containerized and VM-based compute.

## Key Benefits

- **GPU Passthrough for VMs**: Assign AMD GPUs directly to KubeVirt-managed VMs.
- **Unified GPU Management**: Use the same operator to manage GPU resources for both containers and VMs.
- **Enhanced Workload Flexibility**: Run specialized workloads in VMs while leveraging GPU acceleration.

## Prerequisites

- Kubernetes v1.29.0+ with KubeVirt installed.
- VF-Passthrough requires [AMD MxGPU GIM Driver](https://github.com/amd/MxGPU-Virtualization) supported GPUs.
- VF-Passthrough requires that host should be configured properly to support SR-IOV (Single Root I/O Virtualization) related features by following [GIM driver documentation](https://instinct.docs.amd.com/projects/virt-drv/en/latest/index.html).
- Both VF-Passthrough and PF-Passthrough require the host operating system to have `vfio`-related kernel modules ready for use.

## Host Configuration

### BIOS Setting

You need to set up System BIOS to enable the virtualization related features. For example, sample System BIOS settings will look like this (depending on vendor and BIOS version):

* SR-IOV Support: Enable this option in the Advanced → PCI Subsystem Settings page.

* Above 4G Decoding: Enable this option in the Advanced → PCI Subsystem Settings page.

* PCIe ARI Support: Enable this option in the Advanced → PCI Subsystem Settings page.

* IOMMU: Enable this option in the Advanced → NB Configuration page.

* ACS Enabled: Enable this option in the Advanced → NB Configuration page.

### GRUB Config Update

* Edit GRUB Configuration File:
Use a text editor to modify the /etc/default/grub file (Following example uses “nano” text editor). Open the terminal and run the following command:
```bash
sudo nano /etc/default/grub
```

* Modify the `GRUB_CMDLINE_LINUX` Line:
Look for the line that begins with `GRUB_CMDLINE_LINUX`. Modify it to include following parameters, :
```bash
GRUB_CMDLINE_LINUX="modprobe.blacklist=amdgpu iommu=on amd_iommu=on"
```
If there are already parameters in the quotes, append your new parameters separated by spaces.
```{note}
Note: In case host machine is running Intel CPU, replace `amd_iommu` with `intel_iommu`.
```

* After modifying the configuration file, you need to update the GRUB settings by running the following command:
```bash
sudo update-grub
```  

* Reboot Your System:
For the changes to take effect, reboot your system using the following command:
```bash
sudo reboot
```

* Verifying changes:
After the system reboots, confirm that the GRUB parameters were applied successfully by running:
```bash
cat /proc/cmdline
```
When you run the command above, you should see a line that includes:
```bash
modprobe.blacklist=amdgpu iommu=on amd_iommu=on  
```
This indicates that your changes have been applied correctly. 

## Configure KubeVirt

After properly installing the KubeVirt, there will be a KubeVirt custom resource installed as well, several configs are required to do in order to enable the AMD GPU Physical Function (PF) and Virtual Function (VF) to be used by KubeVirt.

1. Enable the `HostDevices` feature gate.
2. Add the PF or VF PCI device information to the host devices permitted list.

For example, in order to add MI300X VF:
```yaml
$ kubectl get kubevirt -n kubevirt kubevirt -oyaml
apiVersion: kubevirt.io/v1
kind: KubeVirt
metadata:
  name: kubevirt
  namespace: kubevirt
spec:
  configuration:
    developerConfiguration:
      featureGates:
      - HostDevices
    permittedHostDevices:
      pciHostDevices:
      - externalResourceProvider: true
        pciVendorSelector: 1002:74b5
        resourceName: amd.com/gpu
```

## Configure GPU Operator

To enable KubeVirt support during installation, please consider using VF-Passthrough or PF-Passthrough then configure the `DeviceConfig` custom resource properly under different scenarios:

### VF-Passthrough

In order to bring up guest VM with VF based GPU-Passthrough, [AMD MxGPU GIM Driver](https://github.com/amd/MxGPU-Virtualization) needs to be installed on the GPU hosts.

#### Use inbox/pre-installed GIM driver

If you already prepared the GPU hosts with GIM driver pre-installed and want to directly use it, you don't have to ask AMD GPU Operator to install it for you:

1. Disable the out-of-tree driver management in `DeviceConfig`:
```yaml
spec:
  driver:
    enable: false
```

2. Make sure the AMD GPU VF on your host is already bound to `vfio-pci` kernel module.
```bash
$ lspci -nnk | grep 1002 -A 3
85:00.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Kernel driver in use: gim # PF device is being used by GIM driver to generate VF 
        Kernel modules: amdgpu
85:02.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X VF] [1002:74b5]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Device [1002:74a1]
        Kernel driver in use: vfio-pci # VF device is bound to vfio-pci kernel module for passthrough
        Kernel modules: amdgpu
```

3. Verify that the VF has been advertised as a resource by device plugin:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep -i allocatable -A 5
  allocatable:
    amd.com/gpu: "1"
```

#### Use out-of-tree GIM driver installed by GPU Operator

If you don't have GIM driver installed on the GPU hosts, AMD GPU Operator can help you install the out-of-tree GIM kernel module to your hosts and automatically bind the VF devices to the `vfio-pci` kernel module to make it ready for passthrough:

1. Enable the out-of-tree driver management in `DeviceConfig`:
```yaml
spec:
  driver:
    # enable out-of-tree driver management
    enable: true
    
    # specify GIM driver version (https://github.com/amd/MxGPU-Virtualization/releases)
    version: "8.1.0.K"

    # specify the driver type as vf-passthrough
    driverType: vf-passthrough
    
    # specify the VF device IDs you want to bind to vfio-pci
    # by default all the latest AMD Instinct GPU VF deviceIDs will be utilized to detect VF and bind to vfio-pci
    #vfioConfig:
    #  deviceIDs:
    #    - 74b5 # MI300X VF
    #    - 7410 # MI210 VF
    
    # Specify your driver image repository here
    # DO NOT include the image tag as AMD GPU Operator will automatically manage the image tag for you
    # e.g. docker.io/username/amdgpu-driver
    image: docker.io/username/gim-driver-image

    # Specify the credential for your private registry if it requires credential to get pull/push access
    # you can create the docker-registry type secret by running command like:
    # kubectl create secret docker-registry mySecret -n KMM-NameSpace --docker-server=https://index.docker.io/v1/ --docker-username=xxx --docker-password=xxx
    # Make sure you created the secret within the namespace that KMM operator is running
    imageRegistrySecret:
      name: my-pull-secret
```

2. Verify that the worker node is labeled with proper driver type and vfio ready labels:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep operator.amd
    gpu.operator.amd.com/kube-amd-gpu.test-deviceconfig.driver: vf-passthrough
    gpu.operator.amd.com/kube-amd-gpu.test-deviceconfig.vfio.ready: ""
```

3. Verify that the AMD GPU VF on your host is bound to `vfio-pci` kernel module.
```bash
$ lspci -nnk | grep 1002 -A 3
85:00.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Kernel driver in use: gim # PF device is being used by GIM driver to generate VF 
        Kernel modules: amdgpu
85:02.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X VF] [1002:74b5]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Device [1002:74a1]
        Kernel driver in use: vfio-pci # VF device is bound to vfio-pci kernel module for passthrough
        Kernel modules: amdgpu
```

4. Verify that the VF has been advertised as a resource by device plugin:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep -i allocatable -A 5
  allocatable:
    amd.com/gpu: "1"
```

### PF-Passthrough

In order to bring up guest VM with PF based GPU-Passthrough, you don't have to install [AMD MxGPU GIM Driver](https://github.com/amd/MxGPU-Virtualization) on the GPU hosts. However, binding the PF device to `vfio-pci` kernel module is still required.

#### Use your own method to manage the PF-Passthrough

If you are using your own method to manage the PF device and it is already bound with `vfio-pci`, please:

1. Disable the driver management of AMD GPU Operator:
```yaml
spec:
  driver:
    enable: false
```

2. Verify that the AMD GPU PF on your host is already bound to `vfio-pci` kernel module.
```bash
$ lspci -nnk | grep 1002 -A 3
85:00.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Kernel driver in use: vfio-pci # PF device is bound to vfio-pci
        Kernel modules: amdgpu
```

3. Verify that the PF has been advertised as a resource by device plugin:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep -i allocatable -A 5
  allocatable:
    amd.com/gpu: "1"
```

#### Use AMD GPU Operator to manage PF-Passthrough vfio binding
The AMD GPU Operator can help you bind the AMD GPU PF device to the `vfio-pci` kernel module on all the selected GPU hosts:

1. Configure the `DeviceConfig` custom resource to use PF-Passthrough:
```yaml
spec:
  driver:
    # enable out-of-tree driver management
    enable: true

    # specify the driver type as pf-passthrough
    driverType: pf-passthrough
    
    # specify the PF device IDs you want to bind to vfio-pci
    # by default all the latest AMD Instinct GPU PF deviceIDs will be utilized to detect PF and bind to vfio-pci
    #vfioConfig:
    #  deviceIDs:
    #    - 74a1 # MI300X PF
    #    - 740f # MI210 PF
```

2. Verify that the worker node is labeled with proper driver type and vfio ready labels:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep operator.amd
    gpu.operator.amd.com/kube-amd-gpu.test-deviceconfig.driver: pf-passthrough
    gpu.operator.amd.com/kube-amd-gpu.test-deviceconfig.vfio.ready: ""
```

3. Verify that the AMD GPU PF on your host is bound to `vfio-pci` kernel module.
```bash
$ lspci -nnk | grep 1002 -A 3
85:00.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Aqua Vanjaram [Instinct MI300X] [1002:74a1]
        Kernel driver in use: vfio-pci # PF device is bound to vfio-pci
        Kernel modules: amdgpu
```

4. Verify that the PF has been advertised as a resource by device plugin:
```yaml
$ kubectl get node <your worker node name> -oyaml | grep -i allocatable -A 5
  allocatable:
    amd.com/gpu: "1"
```


## GPU Operator Components

### Device Plugin

The Device Plugin is responsible for discovering AMD GPU devices and advertising them to Kubernetes for scheduling GPU workloads. It supports:

- **Container workloads**: Standard GPU usage for containerized applications.
- **VF Passthrough**: Virtual Function passthrough using SR-IOV enabled by the AMD GIM driver. All VFs are advertised under the resource name `amd.com/gpu`. The number of GPUs advertised corresponds to the number of unique IOMMU groups these VFs belong to, ensuring VMs are allocated VFs from distinct IOMMU groups for proper isolation.
  - *MI210 Specifics*: For MI210-based nodes, VF assignment to a VM is restricted by its XGMI fabric architecture. VFs are grouped into "hives" (typically 4 VFs per hive). A VM can be assigned 1, 2, or 4 VFs from a single hive, or all 8 VFs from both hives.
- **PF Passthrough**: Physical Function passthrough using the VFIO kernel module for exclusive GPU access. All PFs are advertised under the resource name `amd.com/gpu`.

The Device Plugin assumes homogeneous nodes, meaning a node is configured to operate in a single mode: container, vf-passthrough, or pf-passthrough. All discoverable GPU resources on that node will be of the same type.

The Device Plugin uses automatic mode detection. If no explicit operational mode is specified using the `driver_type` command-line argument, it inspects the system setup (such as the presence of /dev/kfd, virtfn* symlinks, or driver bindings) and selects the appropriate mode (container, vf-passthrough, or pf-passthrough) accordingly. This simplifies deployment and reduces manual configuration requirements.

### Node Labeler

The Node Labeler automatically assigns meaningful labels to Kubernetes nodes to reflect GPU device capabilities, operational modes, and other essential details. These labels are crucial for scheduling and managing GPU resources effectively, especially in KubeVirt passthrough scenarios.

A new label, `amd.com/gpu.mode` (and its beta counterpart `beta.amd.com/gpu.mode`), has been introduced to specify the GPU operational mode (container, vf-passthrough, or pf-passthrough) on the node. Existing labels such as `amd.com/gpu.device-id` (and `beta.amd.com/gpu.device-id`) and `amd.com/gpu.driver-version` (used for containerized workloads) have been extended to support VF and PF passthrough modes. Note that `amd.com/gpu.driver-version` is not applicable in `pf-passthrough` mode as the driver is not managed by the operator in this scenario.

Similar to the Device Plugin, the Node Labeler can auto-detect the operational mode based on node configuration, or it can be explicitly set using the `driver-type` command-line argument.

Key labels for PF and VF passthrough modes are listed below. Placeholders like `<PF_DEVICE_ID>`, `<VF_DEVICE_ID>`, `<COUNT>`, and `<GIM_DRIVER_VERSION>` represent actual device IDs (e.g., `74a1`, `74b5`), device counts, and GIM driver versions (e.g., `8.1.0.K`) respectively.

**PF Passthrough Mode Labels:**
- `amd.com/gpu.mode=pf-passthrough`
- `beta.amd.com/gpu.mode=pf-passthrough`
- `amd.com/gpu.device-id=<PF_DEVICE_ID>`
- `beta.amd.com/gpu.device-id=<PF_DEVICE_ID>`
- `beta.amd.com/gpu.device-id.<PF_DEVICE_ID>=<COUNT>`

**VF Passthrough Mode Labels:**
- `amd.com/gpu.mode=vf-passthrough`
- `beta.amd.com/gpu.mode=vf-passthrough`
- `amd.com/gpu.device-id=<VF_DEVICE_ID>`
- `beta.amd.com/gpu.device-id=<VF_DEVICE_ID>`
- `beta.amd.com/gpu.device-id.<VF_DEVICE_ID>=<COUNT>`
- `amd.com/gpu.driver-version=<GIM_DRIVER_VERSION>`

## Create Guest VM

After verifying that the PF or VF devices have been advertised by device plugin successfully, you can start to deploy the guest VMs by creating KubeVirt custom resource. By specifying the host devices into the `VirtualMachine` or `VirtualMachineInstance` definition, the guest VM would be scheduled on the GPU host where the requested GPU resources are available. Here is an example:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
...
spec:
  template:
    spec:
      domain:
        devices:
          hostDevices:
          - deviceName: amd.com/gpu
            name: gpu1
...
```

Once the KubeVirt custom resource was created, you can check its status by running these commands to make sure they are scheduled and ready:

```bash
kubectl get vm
kubectl get vmi
```

After `VirtualMachineInstance` became scheduled and ready, it doesn't mean that the guest VM has been fully launched and ready to use, you may need to wait for extra time for the guest VM to be fully ready and accessible. You can check the status of the VM logs by fetching the logs from container guest-console-log.

```bash
kubectl logs virt-launcher-ubuntu2204-lbc7f -c guest-console-log
```

## Verify Guest VM

Once the VM was up and ready to use, login into the guest VM with the credentials you specified, then verify the list of available PCI devices to make sure the GPU was passed through into the guest VM. In this example the MI300X VF has been successfully passed into the guest VM.

```bash
$ lspci -nnkk | grep -i 1002 -A 1
09:00.0 Processing accelerators [1200]: Advanced Micro Devices, Inc. [AMD/ATI] Device [1002:74b5]
        Subsystem: Advanced Micro Devices, Inc. [AMD/ATI] Device [1002:74a1]
```
