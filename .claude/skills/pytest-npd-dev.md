# Skill: pytest-npd-dev

## Purpose

Specialized pytest development and debugging for Node Problem Detector (NPD) integration with AMD GPU health monitoring. Use this skill when developing, debugging, or extending NPD-related tests.

## Capabilities

- Deploy and configure NPD DaemonSets with custom plugin monitors
- Test amdgpuhealth tool integration with device-metrics-exporter
- Debug NPD condition detection and Kubernetes event generation
- Troubleshoot ConfigMap/DaemonSet volume mounting issues
- Validate NPD functionality across K8s and OpenShift platforms
- Triage NPD test failures from job logs

## Key Components

### 1. NPD Architecture

- **NPD DaemonSet**: Runs on each node to detect and report problems
- **Custom Plugin Monitor**: `amdgpuhealth.json` defines GPU health checks
- **amdgpuhealth Binary**: Query tool for device-metrics-exporter metrics
  - Location: `/var/lib/amd-metrics-exporter/amdgpuhealth`
  - Queries metrics endpoint and compares against thresholds
  - Generates NPD conditions based on metric values
- **ConfigMap**: Contains NPD monitor configurations
- **Required configs**: kernel-monitor.json, system-log-monitor.json (NPD v0.8.15 hardcoded)

### 2. amdgpuhealth Tool

- **Source**: External repo, integrated via NPD testing
- **Purpose**: Query metrics and generate NPD conditions
- **Configuration**: Needs metrics endpoint URL
  - NodePort mode: `http://localhost:32500/metrics`
  - ClusterIP mode: `http://localhost:5000/metrics`
- **Command pattern**: `/var/lib/amd-metrics-exporter/amdgpuhealth query <metric-type> -m=<metric-name> -t=<threshold>`
- **Metric types**: `gauge-metric`, `counter-metric`

### 3. Test Structure

Primary test file: `tests/pytests/k8/gpu-operator/test_node_problem_detector.py`
Shared library: `tests/pytests/lib/npd_util.py`

Key test functions:

- `test_npd_basic`: Basic NPD deployment and pod readiness
- `test_exporter_amdgpuhealth_hostpath`: Validates amdgpuhealth binary placement and socket
- `test_npd_multi_condition_workload`: Parametrized tests for multiple GPU conditions

### 4. NPD Configuration Patterns

#### ConfigMap Structure

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-problem-detector-config
data:
  kernel-monitor.json: |
    {"plugin": "kmsg", "logPath": "/dev/kmsg", "rules": []}
  system-log-monitor.json: |
    {"plugin": "journald", "rules": []}
  amdgpuhealth.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "30s",
        "timeout": "5m",
        "enable_message_change_based_condition_update": false
      },
      "source": "amdgpu-health-monitor",
      "conditions": [...]
    }
```

#### DaemonSet Volume Mounting

**CRITICAL**: When `items` field is specified, only those keys are mounted!

```yaml
volumeMounts:
  - name: config
    mountPath: /config
    readOnly: true
volumes:
  - name: config
    configMap:
      name: node-problem-detector-config
      items:
        - key: kernel-monitor.json
          path: kernel-monitor.json
        - key: system-log-monitor.json
          path: system-log-monitor.json
        - key: amdgpuhealth.json
          path: amdgpuhealth.json
```

### 5. Metric Naming Convention

- **Current standard**: `gpu_*` prefix (e.g., `gpu_gfx_activity`)
- **Legacy**: Some tests may use `amd_gpu_*` prefix
- Always verify metric names against device-metrics-exporter actual output
- Reference: `kb_source/common/device-metrics-exporter.md`

### 6. Common Debugging Patterns

#### ConfigMap Not Mounted

**Symptoms**: NPD pod CrashLoopBackOff, "Failed to read configuration file"
**Check**:

1. ConfigMap contains all required keys (kernel-monitor.json, system-log-monitor.json, amdgpuhealth.json)
2. DaemonSet volume `items` list includes all keys
3. Pod logs: `kubectl logs -n node-problem-detector <pod> | grep -i config`

#### amdgpuhealth Can't Find Endpoint

**Symptoms**: "unable to get metrics endpoint url"
**Root cause**: Missing configuration file with endpoint URL
**Expected behavior**: device-metrics-exporter should write config file (e.g., .cobra.yaml) with:

- NodePort: `http://localhost:32500/metrics`
- ClusterIP: `http://localhost:5000/metrics`

**Current status**: Feature gap - exporter doesn't write config yet

#### NPD Condition Not Generated

**Check sequence**:

1. amdgpuhealth binary exists in pod
2. Metric endpoint accessible from NPD pod
3. Metric value exceeds threshold
4. NPD pod logs show condition updates
5. Kubernetes events created on node

### 7. NPD Discovery and Labeling

**GPU Operator NPD Assumption**: The gpu-operator tech-support script (`tools/techsupport_dump.sh`) discovers NPD by searching for daemonsets/pods with label `app=node-problem-detector`:

```bash

# Primary discovery via DaemonSet label

NPD_NS=$(${KUBECTL} get daemonsets --no-headers -A -l app=node-problem-detector | awk '{ print $1 }' | sort -u | head -n1)

# Fallback discovery via pod label

if [ -z "$NPD_NS" ]; then
    NPD_NS=$(${KUBECTL} get pods --no-headers -A -l app=node-problem-detector | awk '{ print $1 }' | sort -u | head -n1)
fi
```

**Critical Requirement**: NPD DaemonSets MUST have label `app=node-problem-detector` for gpu-operator tooling to discover and collect diagnostics. This affects:

- Tech-support bundle collection (`tools/techsupport_dump.sh`)
- NPD test utilities (`npd_util.py` - `NPD_APP_NAME = "node-problem-detector"`)
- Cross-platform compatibility (K8s and OpenShift)

Reference: `tools/techsupport_dump.sh` lines 108-111, 319-326

### 8. Test Development Workflow

#### Creating New Condition Tests

**IMPORTANT**: Ensure DaemonSet has label `app=node-problem-detector` for gpu-operator discovery.

1. Define condition in parametrize decorator
2. Specify metric_type (gauge-metric or counter-metric)
3. Provide metric_name (verify against exporter)
4. Set threshold value
5. Define condition_type, reasons, and messages
6. Deploy NPD with condition
7. Trigger metric change (workload or injection)
8. Verify condition and event creation

#### Shared Library Usage

`npd_util.py` provides:

- `NPD_NAMESPACE`, `NPD_APP_NAME`: Standard naming
- ConfigMap creation helpers
- DaemonSet deployment functions
- Event verification utilities

Used by both K8s and OpenShift tests - verify cross-platform compatibility when modifying.

### 8. Platform Coverage

Test NPD functionality across:

- Kubernetes 1.29-1.35
- OpenShift 4.20-4.21
- GPU models: MI355X, MI350X, MI325X, MI300X, MI250/MI250X, MI210
- Service types: NodePort, ClusterIP

Reference: `kb_source/common/platform-support.md`

## Documentation References

- NPD architecture: `docs/npd/node-problem-detector.md`
- Device exporter: `kb_source/common/device-metrics-exporter.md`
- Platform support: `kb_source/common/platform-support.md`
- Test file: `tests/pytests/k8/gpu-operator/test_node_problem_detector.py`
- Shared library: `tests/pytests/lib/npd_util.py`

## Known Issues & Gaps

1. **amdgpuhealth configuration**: Exporter doesn't write endpoint config file yet
2. **NPD hardcoded monitors**: Cannot disable kernel-monitor/system-log-monitor via flags
3. **Metric naming**: Migration from `amd_gpu_*` to `gpu_*` prefix in progress

## When to Use This Skill

- Developing new NPD condition tests
- Debugging NPD test failures in job logs
- Adding support for new GPU health metrics
- Troubleshooting ConfigMap/volume mount issues
- Investigating NPD pod CrashLoopBackOff
- Validating amdgpuhealth integration
- Extending NPD functionality for new platforms

## Example Usage Pattern

```bash

# Skill invocation when user asks:

# "Debug NPD test failure in job 30166133"

# "Add test for GPU temperature threshold"

# "Why is NPD pod crashing?"

# "Test amdgpuhealth integration with new metric"

```
