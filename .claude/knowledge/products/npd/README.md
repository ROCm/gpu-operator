# Node Problem Detector (NPD) - Product Knowledge

🚧 **Placeholder - To Be Populated**

## Planned Content

This directory will contain product-level documentation about how NPD integration works:

### NPD Architecture

- NPD DaemonSet components
- Monitor types (kernel, system-log, custom-plugin)
- Condition and event generation
- Integration with Kubernetes node status

### amdgpuhealth Tool

- Purpose and functionality
- Metrics endpoint querying
- Threshold-based condition generation
- Binary location and permissions

### Custom Plugin Monitors

- Plugin configuration format
- Invoke intervals and timeouts
- Condition update mechanisms
- Message change handling

### ConfigMap Structure

- Required vs optional monitors
- NPD hardcoded requirements (kernel-monitor, system-log-monitor)
- Custom plugin configuration
- Volume mounting considerations

## For Now

See test-related NPD knowledge in:

- `../../testing/npd/` (to be created)
- `../../skills/pytest-npd-dev.md`
