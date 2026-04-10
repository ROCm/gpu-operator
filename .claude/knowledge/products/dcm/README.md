# Device Config Manager (DCM) - Product Knowledge

🚧 **Placeholder - To Be Populated**

## Planned Content

This directory will contain product-level documentation about how DCM works:

### Architecture

- DCM DaemonSet architecture
- How DCM monitors node labels
- Interaction with KMM and device-plugin
- State machine for partition operations

### Partition Modes

- GPU partition types (SPX, DPX, QPX, CPX)
- Memory partition types (NPS1, NPS2, NPS4, NPS8)
- Supported combinations by GPU series (MI300X, MI325X, MI350X)
- Physical vs logical partition concepts

### ConfigMap Format

- Profile structure and validation rules
- GPU count requirements
- Skipped GPUs configuration
- Systemd service integration

### Control Plane

- Node label usage (`dcm.amd.com/gpu-config-profile`, state labels)
- Taint mechanism (`amd-dcm=up:NoExecute`)
- DeviceConfig CR integration
- Toleration requirements

### Workflow

- Complete partition operation flow
- What happens during partition
- Driver unload/reload behavior
- Recovery from failures

## For Now

See test-related DCM knowledge in `../../testing/dcm/`

Product documentation sources:

- `/docs/dcm/device-config-manager.md`
- `/docs/dcm/applying-partition-profiles.rst`
- `/docs/dcm/device-config-manager-configmap.md`
