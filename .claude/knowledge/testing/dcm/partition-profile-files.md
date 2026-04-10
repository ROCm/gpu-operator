# KB: Partition Profile File Formats and Validation

## Overview

DCM partition profiles define how GPUs should be partitioned. Profiles are stored in JSON files and loaded into ConfigMaps for DCM to consume.

## Profile File Location

Test profiles: `tests/pytests/k8/gpu-operator/lib/files/partitioning_check_<GPU_SERIES>_<GPU_COUNT>.json`

Examples:

- `partitioning_check_MI300X_8.json`
- `partitioning_check_MI325X_8.json`
- `partitioning_check_MI350X_8.json`

## Profile Structure

### Valid Profile Example

```json
{
  "gpu-config-profiles": {
    "QPX_NPS1": {
      "skippedGPUs": {
        "ids": []
      },
      "profiles": [
        {
          "computePartition": "QPX",
          "memoryPartition": "NPS1",
          "numGPUsAssigned": 8
        }
      ]
    },
    "DPX_NPS2": {
      "skippedGPUs": {
        "ids": []
      },
      "profiles": [
        {
          "computePartition": "DPX",
          "memoryPartition": "NPS2",
          "numGPUsAssigned": 8
        }
      ]
    },
    "SPX_NPS1": {
      "skippedGPUs": {
        "ids": []
      },
      "profiles": [
        {
          "computePartition": "SPX",
          "memoryPartition": "NPS1",
          "numGPUsAssigned": 8
        }
      ]
    }
  }
}
```

### Heterogeneous Profile Example (Not Recommended)

```json
{
  "gpu-config-profiles": {
    "heterogeneous": {
      "skippedGPUs": {
        "ids": []
      },
      "profiles": [
        {
          "computePartition": "SPX",
          "memoryPartition": "NPS1",
          "numGPUsAssigned": 4
        },
        {
          "computePartition": "CPX",
          "memoryPartition": "NPS1",
          "numGPUsAssigned": 4
        }
      ]
    }
  }
}
```

**Note**: Heterogeneous partitioning not recommended by AMD. NPS4 does not work with heterogeneous schemes.

## Profile Field Definitions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `computePartition` | string | Yes | Compute partition type: `SPX`, `DPX`, `QPX`, `CPX` |
| `memoryPartition` | string | Yes | Memory partition type: `NPS1`, `NPS2`, `NPS4`, `NPS8` |
| `numGPUsAssigned` | integer | Yes | Number of GPUs to partition with this config |
| `skippedGPUs.ids` | array | No | GPU IDs to skip (0-indexed) |

## Partition Type Support by GPU Series

### MI300X

- **Compute**: SPX, CPX
- **Memory**: NPS1, NPS4
- **Supported Combos**:
  - SPX_NPS1 (default)
  - CPX_NPS1
  - CPX_NPS4

### MI325X

- **Compute**: SPX, DPX, QPX, CPX
- **Memory**: NPS1, NPS2
- **Supported Combos**:
  - SPX_NPS1 (default)
  - DPX_NPS1, DPX_NPS2
  - QPX_NPS1, QPX_NPS2
  - CPX_NPS1, CPX_NPS2

### MI350X

- **Compute**: SPX, DPX, QPX, CPX
- **Memory**: NPS1, NPS2
- **Supported Combos**:
  - SPX_NPS1 (default)
  - DPX_NPS1, DPX_NPS2
  - QPX_NPS1, QPX_NPS2
  - CPX_NPS1, CPX_NPS2

## Partition Naming Convention

Profile names use format: `<COMPUTE>_<MEMORY>`

Examples:

- `SPX_NPS1` - Single partition, NPS1 memory
- `QPX_NPS2` - Quad partition, NPS2 memory
- `CPX_NPS4` - Compute partition (8 partitions), NPS4 memory

## Validation Rules

### Rule 1: GPU Count Must Match

```bash
Sum of numGPUsAssigned + len(skippedGPUs.ids) = Total GPU count on node
```

Example for 8-GPU node:

- ✅ Valid: `numGPUsAssigned: 8, skippedGPUs: []`
- ✅ Valid: `numGPUsAssigned: 6, skippedGPUs: [0, 1]`
- ❌ Invalid: `numGPUsAssigned: 6, skippedGPUs: []` (missing 2 GPUs)
- ❌ Invalid: `numGPUsAssigned: 10, skippedGPUs: []` (too many GPUs)

### Rule 2: Skipped GPU IDs Must Be Valid

GPU IDs range from `0` to `total_gpus - 1`

Example for 8-GPU node:

- ✅ Valid: `skippedGPUs: [0, 1, 2]`
- ❌ Invalid: `skippedGPUs: [8, 9]` (GPU IDs 8, 9 don't exist)

### Rule 3: Memory Partition Compatibility

- NPS4 only supported with CPX compute partition
- Cannot mix NPS1 and NPS4 in same profile
- MI300X supports NPS4, MI325X/MI350X do not

### Rule 4: Compute/Memory Combos Must Be Supported

Check GPU documentation:

- AMD GPU Partitioning docs: https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/index.html
- Check sysfs on node:

  ```bash
  cat /sys/module/amdgpu/drivers/pci:amdgpu/<bdf>/available_compute_partition
  cat /sys/module/amdgpu/drivers/pci:amdgpu/<bdf>/available_memory_partition
```

## Invalid Profile Examples for Testing

Negative tests use invalid profiles to verify DCM validation:

### Invalid GPU Count

```json
{
  "invalidgpucount": {
    "profiles": [{
      "computePartition": "SPX",
      "memoryPartition": "NPS1",
      "numGPUsAssigned": 4  // Wrong! Node has 8 GPUs
    }]
  }
}
```

### Invalid Memory Type

```json
{
  "invalmemorytype": {
    "profiles": [{
      "computePartition": "SPX",
      "memoryPartition": "NPS99",  // Invalid memory type
      "numGPUsAssigned": 8
    }]
  }
}
```

### Invalid Compute Type

```json
{
  "invalcomputetype": {
    "profiles": [{
      "computePartition": "XXX",  // Invalid compute type
      "memoryPartition": "NPS1",
      "numGPUsAssigned": 8
    }]
  }
}
```

### Missing Required Fields

```json
{
  "invalidmissingfields-numGPUs": {
    "profiles": [{
      "computePartition": "SPX",
      "memoryPartition": "NPS1"
      // Missing: numGPUsAssigned
    }]
  }
}
```

## DCM Validation Behavior

When DCM receives invalid profile:

1. **Labels node**: `dcm.amd.com/gpu-config-profile-state=failure`
2. **Logs error**: "Profile validation failed. Could not partition"
3. **Keeps current partition**: Does not modify GPU state
4. **Kubernetes event**: Creates event with error details

Example DCM error messages:

```bash
Selected Profile invalidgpucount found in the configmap
Profile validation failed. Could not partition
numGPUsAssigned does not equal the total number of GPUs available on this node
```

## Test Implementation Pattern

### Parametrized Tests

```python
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "CPX_NPS1", "SPX_NPS1"])
def test_partitioning_no_workload_MI350X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI350X':
        pytest.skip(f"Testcases specifically designed for MI350X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload=False)
```

### Load Profile from File

```python
gpu_series = get_gpu_series(gpu_cluster, environment)
dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
file_path = os.path.join("lib", "files", f"partitioning_check_{gpu_series}_{dut_node.num_gpus}.json")

with open(file_path) as fp:
    profiles = json.load(fp)
    if not profiles.get("gpu-config-profiles"):
        pytest.fail(f"check {file_path}, something wrong with the configmap")
    elif not profiles["gpu-config-profiles"].get(profile, False):
        pytest.skip(f"Profile {profile} is not supported for {gpu_series}. Refer {file_path}")
```

### Validate Profile Support

```python

# Skip if profile not defined for this GPU series

if not profiles["gpu-config-profiles"].get(profile, False):
    pytest.skip(f"Profile {profile} is not supported for {gpu_series}")
```

## Verifying Partition State with amd-smi

After partition, verify state with amd-smi:

```bash
amd-smi partition -c --json
```

Example output:

```json
{
  "0": {
    "MEMORY_PARTITION": {
      "Accelerator Type": "QPX_NPS1"
    }
  },
  "1": {
    "MEMORY_PARTITION": {
      "Accelerator Type": "QPX_NPS1"
    }
  }
}
```

Parse format: `<COMPUTE>_<MEMORY>` (e.g., `QPX_NPS1`)

## Files

- Profile definitions: `tests/pytests/k8/gpu-operator/lib/files/partitioning_check_*.json`
- Test implementation: `tests/pytests/k8/gpu-operator/test_config_manager.py`
- Profile loading: `run_partition_test_scenario()` function
- Validation: DCM container logs, node labels, Kubernetes events
