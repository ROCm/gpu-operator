# Device Metrics Exporter - Testing Knowledge Base

This document contains key information for testing the device-metrics-exporter component.

## Metrics Endpoint

### Kubernetes Deployment

**ClusterIP Mode** (default):

- **Port**: 5000
- **Access**: From within the cluster only
- **Usage**: `curl -s http://<pod-ip>:5000/metrics`
- **Common in pytest**: Port-forward or exec into pod to access

**NodePort Mode**:

- **Port**: 32500
- **Access**: From outside the cluster via node IP
- **Usage**: `curl -s http://<node-ip>:32500/metrics`
- **Common in pytest**: Direct access from test runner to node

### Bare Metal / Standalone Deployment

**Default Port**: 2112 (as documented in PRD examples)

- **Usage**: `curl -s http://localhost:2112/metrics`

## Pytest Testing Patterns

### Accessing Metrics in K8s Tests

**ClusterIP (Port 5000)**:

```python

# Option 1: Exec into pod

kubectl exec -n <namespace> <pod-name> -- curl -s http://localhost:5000/metrics

# Option 2: Port-forward (from conftest fixture)

# Forward 5000 -> local port, then access via localhost

```

**NodePort (Port 32500)**:

```python

# Direct access from test runner

node_ip = get_node_ip()
metrics_url = f"http://{node_ip}:32500/metrics"
response = requests.get(metrics_url)
```

## Port Summary

| Deployment Mode | Port | Access | Typical Usage |
|----------------|------|--------|---------------|
| Kubernetes ClusterIP | 5000 | Internal | kubectl exec, port-forward |
| Kubernetes NodePort | 32500 | External | Direct curl from test runner |
| Bare Metal / Standalone | 2112 | Local/External | Direct curl |

## Test Workflow for Metric Validation

### Ground Truth: AMD-SMI

AMD-SMI is used as the ground truth for validating device-metrics-exporter metric values.

**Command Used in Tests**:

```bash
amd-smi metric --json
```

**NOT** `amd-smi metric -e` (as shown in some PRD examples).

### Test Architecture

**Important**: From a testing perspective, the following components are ALL inside the device-metrics-exporter pod/image:

- **device-metrics-exporter** - The exporter service
- **gpu-agent** - The gRPC service (not a separate component)
- **amd-smi** - AMD System Management Interface tool

**Test Pattern** (from `tests/pytests/lib/metric_util.py`):

```python

# Execute amd-smi INSIDE the metrics-exporter pod

cmd = ["amd-smi", "metric", "--json"]
ret_code, resp_stdout, resp_stderr = k8_util.exec_command_in_pod(
    environment.gpu_operator_namespace,
    cmd,
    exporter_pod_name,
    "metrics-exporter-container"
)
```

### Metric Collection Function

**Function**: `collect_metrics_samples()` in `tests/pytests/lib/metric_util.py`

**What it does**:

1. Collects AMD-SMI metrics via `kubectl exec` into exporter pod (`amd-smi metric --json`)
2. Collects exporter metrics via HTTP GET to NodePort (`curl http://node-ip:32500/metrics`)
3. Optionally collects gpuctl metrics if enabled (`gpuctl show gpu --json`)
4. Runs all collections in parallel threads with configurable sampling (default: 10 samples, 15s interval)

**Usage Pattern**:

```python
collected_metrics = collect_metrics_samples(
    gpu_cluster,
    gpu_nodes,
    exporter_port_map,
    environment,
    ctxt_name
)

# Returns dict with 'amd_smi', 'exporter_metrics', 'gpuctl' per node

```

### Validation Workflow

1. **Exec into exporter pod** → Run `amd-smi metric --json` (ground truth)
2. **HTTP GET to NodePort** → Fetch `/metrics` endpoint (exporter output)
3. **Parse and Compare** → Validate exporter values match AMD-SMI values

**Key Point**: AMD-SMI runs INSIDE the same pod as the exporter, not on the host node.

## AMD-SMI JSON Output Structure

### Sample Output Format

AMD-SMI metric --json returns a structured JSON with the following hierarchy:

```json
{
  "gpu_data": [
    {
      "gpu": 0,
      "usage": {...},
      "power": {"socket_power": {"value": 41, "unit": "W"}},
      "clock": {...},
      "temperature": {
        "edge": {"value": 33, "unit": "C"},
        "hotspot": {"value": 37, "unit": "C"},
        "mem": {"value": 48, "unit": "C"}
      },
      "pcie": {...},
      "ecc": {
        "total_correctable_count": 0,
        "total_uncorrectable_count": 0,
        "total_deferred_count": 0,
        "cache_correctable_count": 0,
        "cache_uncorrectable_count": 0
      },
      "ecc_blocks": {
        "UMC": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0},
        "SDMA": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0},
        "GFX": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0},
        "MMHUB": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0},
        "PCIE_BIF": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0},
        "HDP": {"correctable_count": 0, "uncorrectable_count": 0, "deferred_count": 0}
      },
      "fan": {...},
      "mem_usage": {...},
      "energy": {...}
    }
  ]
}
```

### ECC Deferred Error Metrics Location

For the new ECC deferred error metrics:

- **Total**: `gpu_data[N].ecc.total_deferred_count`
- **Per-Block**: `gpu_data[N].ecc_blocks.<BLOCK>.deferred_count`

Where `<BLOCK>` is one of: UMC, SDMA, GFX, MMHUB, PCIE_BIF, HDP, etc.

**Note**: MI210 sample shows only 6 blocks (UMC, SDMA, GFX, MMHUB, PCIE_BIF, HDP). Other GPU models may expose additional blocks.

### Source of Truth Files

**Sample AMD-SMI Output**:

- Location: `/home/srivatsa/jobd-logs/<job-id>/logs/idle_<GPU-MODEL>_smi_metrics_*.json`
- Example: `/home/srivatsa/jobd-logs/30158283/logs/idle_MI210_smi_metrics_4.json`
- Usage: Ground truth for metric accuracy validation tests

## Metrics Mapping File

### Location

`tests/pytests/lib/files/metrics-support.json`

### Purpose

Maps exporter metric names to AMD-SMI JSON paths and GPU Agent proto fields for validation.

### Structure

```json
{
  "metrics": [
    {
      "name": "GPU_ECC_CORRECT_TOTAL",
      "skip-validation": "yes",
      "gpu-support": [
        {
          "gpu": ["MI210", "MI250", "MI325X", "MI325X-VF", "MI300X", "MI300X-VF", "MI350X", "MI350P"],
          "gpu-agent": "",
          "amd-smi": ""
        }
      ]
    }
  ]
}
```

### Key Fields

- **name**: Exporter metric field name (e.g., `GPU_ECC_DEFERRED_UMC`)
- **skip-validation**: If "yes", metric not validated against AMD-SMI in tests
- **gpu-support**: Array of GPU models and their mapping
  - **gpu**: List of supported GPU models
  - **amd-smi**: JSON path in AMD-SMI output (e.g., `ecc_blocks.UMC.deferred_count`)
  - **gpu-agent**: Proto field path (e.g., `stats.UMCDeferredErrors`)

### Usage in Tests

When implementing tests for new metrics (like deferred errors):

1. Add entries to `metrics-support.json` for all 19 metrics
2. Map AMD-SMI JSON paths: `ecc.total_deferred_count`, `ecc_blocks.<BLOCK>.deferred_count`
3. Map GPU Agent proto fields: `stats.TotalDeferredErrors`, `stats.<BLOCK>DeferredErrors`
4. Specify supported GPU models

**Example Entry for GPU_ECC_DEFERRED_UMC**:

```json
{
  "name": "GPU_ECC_DEFERRED_UMC",
  "skip-validation": "no",
  "gpu-support": [
    {
      "gpu": ["MI210", "MI250", "MI325X", "MI325X-VF", "MI300X", "MI300X-VF", "MI350X", "MI350P"],
      "amd-smi": "ecc_blocks.UMC.deferred_count",
      "gpu-agent": "stats.UMCDeferredErrors"
    }
  ]
}
```

## Notes

- **PRD Examples**: PRD documentation may show port 2112 (standalone mode) but K8s deployments use 5000/32500
- **Test Adaptation**: When implementing pytest from test plans, use K8s ports (5000/32500) not standalone port (2112)
- **Service Discovery**: Use kubernetes service discovery or node IP + NodePort for pytest access
- **AMD-SMI Command**: Use `amd-smi metric --json` (not `amd-smi metric -e`) for JSON output parsing
- **Component Location**: gpu-agent and amd-smi are INSIDE the device-metrics-exporter pod, not separate services
- **Metrics Mapping**: Always update `tests/pytests/lib/files/metrics-support.json` when adding new metrics
- **Sample Data**: Use job log samples from `/home/srivatsa/jobd-logs/` for ground truth validation
