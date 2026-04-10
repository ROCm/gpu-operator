# Test Plan: ECC Deferred Error Count Metrics

**PRD Reference**: PRD-GPU-20260406-01
**Feature**: ECC Deferred Error Metrics for Device Metrics Exporter
**Created**: 2026-04-06
**Status**: Draft - Pending Approval

---

## 1. Test Plan Overview

### 1.1 PRD Reference

- **PRD ID**: PRD-GPU-20260406-01
- **PRD Title**: ECC Deferred Error Count Metrics
- **PRD Location**: `/home/srivatsa/ws-3/device-metrics-exporter/kb_source/prds/2026/Q2/PRD-GPU-20260406-01-ecc-deferred-errors.md`

### 1.2 Feature Summary

Add 19 new GPU metrics to the device-metrics-exporter for monitoring ECC deferred errors:

- **1 Total Metric**: `amd_gpu_ecc_deferred_total` - Total deferred errors across all blocks
- **18 Per-Block Metrics**: `amd_gpu_ecc_deferred_<block>` - Deferred errors per GPU hardware component

**Purpose**: Memory reliability monitoring and predictive failure analysis. These metrics complete the ECC error observability by adding the third error type (deferred) alongside existing correctable and uncorrectable error metrics.

**Key Characteristic**: Static metrics (accumulated counters) that do NOT change with workload activity.

### 1.3 Test Scope

**In Scope:**

- Functional validation of all 19 metrics
- Metric accuracy validation against AMD-SMI ground truth
- Configuration-based metric filtering (enable/disable)
- Platform-specific validation (MI2xx, MI3xx, baremetal, SR-IOV)
- Partitioned GPU validation (SPX/CPX/DPX/QPX modes)
- Multi-GPU scenarios
- Negative testing (unsupported platforms, invalid configs)
- ECC error injection testing (metricsclient)
- Integration with existing ECC metrics
- Documentation updates

**Out of Scope:**

- GPU health service integration (deferred errors explicitly NOT used for health determination)
- Performance impact testing (covered by standard performance regression suite)
- GPUAgent submodule implementation testing (assumes gpuagent changes are tested separately)
- Real hardware error injection with AMDGPURAS (metricsclient sufficient for functional validation)
- Non-MI2xx/MI3xx platforms

### 1.4 Testing Approach

**Strategy**: Blackbox automation testing focused on:

1. **Functional Testing**: Verify metrics appear with correct values
2. **Integration Testing**: Verify interaction with AMD-SMI and gpuagent
3. **Platform Testing**: Cross-platform validation (baremetal, SR-IOV, partitioned GPUs)
4. **Negative Testing**: Error handling and graceful degradation
5. **Configuration Testing**: Config-driven metric control

**Test Data Source**:

- AMD-SMI API as ground truth (`amd-smi metric --json`)
- metricsclient for safe ECC error injection

**Test Architecture**:

- AMD-SMI and gpu-agent run INSIDE the device-metrics-exporter pod
- Tests execute `amd-smi` via `kubectl exec` into the exporter pod
- Metrics accessed via Kubernetes NodePort (32500) or ClusterIP (5000)

**Automation**: All tests designed for CI/CD integration, no manual testing required.

---

## 2. Requirements Traceability Matrix

| Requirement ID | Description | Test Scenarios | Priority |
|----------------|-------------|----------------|----------|
| REQ-1.1 | Export 19 deferred error metrics (1 total + 18 per-block) | TS-1.1, TS-1.2 | P0 |
| REQ-1.2 | Metrics follow existing ECC naming pattern `amd_gpu_ecc_deferred_*` | TS-1.1, TS-1.2 | P0 |
| REQ-1.3 | Metrics have required labels (gpu_id, gpu_uuid, hostname) | TS-1.3 | P0 |
| REQ-2.1 | Metric values sourced from AMD-SMI `amdsmi_error_count_t.deferred_count` | TS-2.1 | P0 |
| REQ-2.2 | Metric values match AMD-SMI output exactly (zero tolerance) | TS-2.2 | P0 |
| REQ-3.1 | Configuration enable/disable controls metric export | TS-3.1, TS-3.2 | P0 |
| REQ-3.2 | Config reload (3s auto-reload) applies changes | TS-3.3 | P1 |
| REQ-4.1 | Support MI2xx and MI3xx platforms (baremetal) | TS-4.1 | P0 |
| REQ-4.2 | Support SR-IOV/hypervisor deployment (if AMD-SMI supports) | TS-4.2 | P1 |
| REQ-4.3 | Graceful handling of unsupported platforms (field logger) | TS-4.3 | P1 |
| REQ-5.1 | Multi-GPU support (correct gpu_id, gpu_uuid per GPU) | TS-5.1 | P0 |
| REQ-5.2 | Partitioned GPU: Only partition 0 exports ECC metrics | TS-5.2, TS-5.3 | P0 |
| REQ-5.3 | Partitioned GPU: Partitions 1-7 skip gracefully (field logger) | TS-5.4 | P1 |
| REQ-6.1 | Static metric behavior (values persist, don't change with workload) | TS-6.1 | P1 |
| REQ-6.2 | Metrics increment when ECC errors injected | TS-6.2 | P0 |
| REQ-7.1 | No impact on GPU health service determination | TS-7.1 | P0 |
| REQ-8.1 | Documentation updated (metricslist.md, metricsmap.md, releasenotes.md) | TS-8.1 | P1 |

---

## 3. Test Scenarios

### 3.1 Functional Validation

#### TS-1.1: Verify All 19 Metrics Export

**Priority**: P0 (Blocker)
**PRD Reference**: Section 1.1, 5.5.1
**Requirements**: REQ-1.1

**Objective**: Verify all 19 deferred error metrics appear in Prometheus `/metrics` endpoint when enabled in config.

**Prerequisites**:

- Device-metrics-exporter deployed on system with AMD GPU
- MI2xx or MI3xx GPU (partition 0 if partitioned)
- Config file includes all 19 `GPU_ECC_DEFERRED_*` fields
- Exporter running and healthy

**Test Steps**:

1. Enable all 19 deferred error metrics in `/etc/metrics/config.json`:

   ```json
   "fields": [
     "GPU_ECC_DEFERRED_TOTAL",
     "GPU_ECC_DEFERRED_SDMA", "GPU_ECC_DEFERRED_GFX",
     "GPU_ECC_DEFERRED_MMHUB", "GPU_ECC_DEFERRED_ATHUB",
     "GPU_ECC_DEFERRED_BIF", "GPU_ECC_DEFERRED_HDP",
     "GPU_ECC_DEFERRED_XGMI_WAFL", "GPU_ECC_DEFERRED_DF",
     "GPU_ECC_DEFERRED_SMN", "GPU_ECC_DEFERRED_SEM",
     "GPU_ECC_DEFERRED_MP0", "GPU_ECC_DEFERRED_MP1",
     "GPU_ECC_DEFERRED_FUSE", "GPU_ECC_DEFERRED_UMC",
     "GPU_ECC_DEFERRED_MCA", "GPU_ECC_DEFERRED_VCN",
     "GPU_ECC_DEFERRED_JPEG", "GPU_ECC_DEFERRED_IH",
     "GPU_ECC_DEFERRED_MPIO"
   ]
```

2. Restart exporter or wait 3s for auto-reload
3. Query metrics endpoint: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred`
4. Count number of unique metric names (HELP lines)

**Expected Results**:

- **Exactly 19 unique deferred error metrics** appear in output
- Metric names follow pattern: `amd_gpu_ecc_deferred_total`, `amd_gpu_ecc_deferred_<block>`
- Each metric has both HELP and TYPE directives
- All metrics have TYPE=gauge

**Validation Command**:

```bash
curl -s http://<node-ip>:32500/metrics | grep "^# HELP amd_gpu_ecc_deferred" | wc -l

# Expected: 19

```

**Note**: Use NodePort 32500 for external access or ClusterIP port 5000 via kubectl port-forward.

---

#### TS-1.2: Verify Metric Naming Convention

**Priority**: P0 (Blocker)
**PRD Reference**: Section 1.1, 2.3
**Requirements**: REQ-1.1, REQ-1.2

**Objective**: Verify metric names follow Prometheus naming conventions and match PRD specification.

**Prerequisites**: Same as TS-1.1

**Test Steps**:

1. Query metrics endpoint and extract metric names
2. Compare against expected names from PRD Section 1.1 table
3. Verify naming pattern consistency with existing ECC metrics

**Expected Results**:

- Metric names match PRD specification exactly (case-sensitive)
- Pattern: `amd_gpu_ecc_deferred_<block>` (lowercase, underscore-separated)
- Block names match: `total`, `sdma`, `gfx`, `mmhub`, `athub`, `bif`, `hdp`, `xgmi_wafl`, `df`, `smn`, `sem`, `mp0`, `mp1`, `fuse`, `umc`, `mca`, `vcn`, `jpeg`, `ih`, `mpio`
- Consistent with existing `amd_gpu_ecc_correct_*` and `amd_gpu_ecc_uncorrect_*` patterns

**Validation**:

```bash

# Expected metric names (exact list)

expected=(
  "amd_gpu_ecc_deferred_total"
  "amd_gpu_ecc_deferred_sdma" "amd_gpu_ecc_deferred_gfx"
  "amd_gpu_ecc_deferred_mmhub" "amd_gpu_ecc_deferred_athub"
  "amd_gpu_ecc_deferred_bif" "amd_gpu_ecc_deferred_hdp"
  "amd_gpu_ecc_deferred_xgmi_wafl" "amd_gpu_ecc_deferred_df"
  "amd_gpu_ecc_deferred_smn" "amd_gpu_ecc_deferred_sem"
  "amd_gpu_ecc_deferred_mp0" "amd_gpu_ecc_deferred_mp1"
  "amd_gpu_ecc_deferred_fuse" "amd_gpu_ecc_deferred_umc"
  "amd_gpu_ecc_deferred_mca" "amd_gpu_ecc_deferred_vcn"
  "amd_gpu_ecc_deferred_jpeg" "amd_gpu_ecc_deferred_ih"
  "amd_gpu_ecc_deferred_mpio"
)

# Verify each metric exists in output

```

---

#### TS-1.3: Verify Metric Labels

**Priority**: P0 (Blocker)
**PRD Reference**: Section 2.3
**Requirements**: REQ-1.3

**Objective**: Verify all deferred error metrics have required Prometheus labels.

**Prerequisites**: Same as TS-1.1

**Test Steps**:

1. Query metrics endpoint
2. Parse metric labels for each deferred error metric
3. Verify required labels present: `gpu_id`, `gpu_uuid`, `hostname`

**Expected Results**:

- All 19 metrics have labels: `{gpu_id="X",gpu_uuid="GPU-...",hostname="..."}`
- `gpu_id` matches GPU index (0, 1, 2, ...)
- `gpu_uuid` follows format: `GPU-<8hex>-<4hex>-<4hex>-<4hex>-<12hex>`
- `hostname` matches system hostname

**Example Expected Output**:

```bash
amd_gpu_ecc_deferred_total{gpu_id="0",gpu_uuid="GPU-12345678-1234-1234-1234-123456789abc",hostname="gpu-node-01"} 42
```

---

### 3.2 Metric Accuracy Validation

#### TS-2.1: Verify AMD-SMI Data Source Integration

**Priority**: P0 (Blocker)
**PRD Reference**: Section 2.1.2, 2.2, 5.5.1
**Requirements**: REQ-2.1

**Objective**: Verify exporter correctly reads deferred error counts from AMD-SMI API.

**Prerequisites**:

- AMD-SMI installed and functional
- GPUAgent configured to call `amdsmi_get_gpu_ecc_count()`
- Exporter connected to gpuagent gRPC service

**Test Steps**:

1. Query AMD-SMI for ECC error counts from inside exporter pod:

   ```bash
   kubectl exec -n <namespace> <exporter-pod> -c metrics-exporter-container -- amd-smi metric --json
```

2. Extract deferred error counts per block from AMD-SMI JSON output
3. Query exporter metrics endpoint via NodePort
4. Compare Prometheus metric values with AMD-SMI values

**Expected Results**:

- AMD-SMI JSON output contains `deferred_count` field for each block
- Exporter retrieves values via gpuagent gRPC (`stats.TotalDeferredErrors`, `stats.UMCDeferredErrors`, etc.)
- Values flow correctly: AMD-SMI → gpuagent → exporter → Prometheus

**Data Flow Verification**:

```bash
AMD-SMI amdsmi_error_count_t.deferred_count (inside exporter pod)
  → gpuagent smi_fill_ecc_stats_() (inside exporter pod)
  → GPUStats.UMCDeferredErrors (proto)
  → exporter logWithValidateAndExport()
  → Prometheus amd_gpu_ecc_deferred_umc (accessible via NodePort 32500)
```

**Reference**: See `collect_metrics_samples()` in `tests/pytests/lib/metric_util.py` for actual implementation pattern.

---

#### TS-2.2: Validate Metric Accuracy vs AMD-SMI

**Priority**: P0 (Blocker)
**PRD Reference**: Section 5.5.2
**Requirements**: REQ-2.2

**Objective**: Verify Prometheus metrics match AMD-SMI output exactly (zero tolerance).

**Prerequisites**:

- System with stable ECC error counts (or injected errors via metricsclient)
- AMD-SMI installed
- Exporter running

**Test Steps**:

1. Inject known deferred error count using metricsclient (e.g., UMC block = 42)
2. Query AMD-SMI from inside exporter pod:

   ```bash
   kubectl exec -n <namespace> <exporter-pod> -c metrics-exporter-container -- amd-smi metric --json
```

   Parse JSON for UMC deferred_count
3. Query Prometheus: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred_umc`
4. Compare values

**Expected Results**:

- **Exact match** required (zero tolerance)
- If AMD-SMI shows `UMC deferred_count: 42`, Prometheus `amd_gpu_ecc_deferred_umc` must be `42`
- No rounding, no sampling variance (static accumulated counter)

**Validation for All 19 Metrics**:

- Repeat for each of the 19 metrics
- Verify `amd_gpu_ecc_deferred_total` equals sum of all per-block metrics

**Automated Test**:

```python

# Pseudocode

amd_smi_values = parse_amd_smi_output()
prometheus_values = parse_prometheus_metrics()

for metric in all_19_metrics:
    assert prometheus_values[metric] == amd_smi_values[metric], \
        f"Mismatch: {metric} Prometheus={prometheus_values[metric]} AMD-SMI={amd_smi_values[metric]}"
```

---

### 3.3 Configuration Testing

#### TS-3.1: Enable Deferred Error Metrics via Config

**Priority**: P0 (Blocker)
**PRD Reference**: Section 5.5.1, 6.4
**Requirements**: REQ-3.1

**Objective**: Verify deferred error metrics can be enabled via configuration.

**Prerequisites**:

- Fresh exporter deployment
- Config file editable

**Test Steps**:

1. Start with config that does NOT include `GPU_ECC_DEFERRED_*` fields
2. Verify metrics NOT present: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred` (no output)
3. Add `GPU_ECC_DEFERRED_TOTAL` to config fields array
4. Restart exporter or wait 3s for auto-reload
5. Verify metric appears: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred_total`

**Expected Results**:

- Without config: No deferred error metrics exported
- After enabling in config: Metrics appear in `/metrics` endpoint
- Only enabled metrics appear (selective control)

---

#### TS-3.2: Disable Deferred Error Metrics via Config

**Priority**: P0 (Blocker)
**PRD Reference**: Section 5.5.3
**Requirements**: REQ-3.1

**Objective**: Verify deferred error metrics can be disabled via configuration.

**Prerequisites**:

- Exporter running with deferred error metrics enabled

**Test Steps**:

1. Verify metrics present: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred`
2. Remove all `GPU_ECC_DEFERRED_*` from config fields array
3. Restart exporter or wait 3s for auto-reload
4. Verify metrics NOT present: `curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred` (no output)

**Expected Results**:

- Metrics disappear from `/metrics` endpoint after config removal
- No errors in exporter logs
- Other non-ECC metrics continue to export normally

---

#### TS-3.3: Config Auto-Reload (3s Hot Reload)

**Priority**: P1 (Critical)
**PRD Reference**: Section 5.5.1
**Requirements**: REQ-3.2

**Objective**: Verify config changes apply automatically within 3 seconds without restart.

**Prerequisites**:

- Exporter running with config auto-reload enabled (default)

**Test Steps**:

1. Start with deferred metrics disabled
2. Curl `/metrics` → verify no deferred metrics
3. Edit config to add `GPU_ECC_DEFERRED_UMC`
4. Wait 3 seconds (no restart)
5. Curl `/metrics` → verify `amd_gpu_ecc_deferred_umc` appears

**Expected Results**:

- Config change detected within 3 seconds
- Metric appears without manual exporter restart
- Timestamp on metric shows it started exporting after config change

---

### 3.4 Platform-Specific Validation

#### TS-4.1: MI2xx and MI3xx Platform Support

**Priority**: P0 (Blocker)
**PRD Reference**: Section 3.3, 5.5.4
**Requirements**: REQ-4.1

**Objective**: Verify deferred error metrics work on MI2xx and MI3xx platforms.

**Prerequisites**:

- Test environments with supported GPU models (see platform coverage matrix below)
- amdgpu driver 6.4.x or higher
- Kubernetes 1.29-1.35 or OpenShift 4.20-4.21

**Test Steps**:

1. Deploy exporter on each platform
2. Enable all 19 deferred error metrics
3. Query `/metrics` endpoint via NodePort
4. Verify all 19 metrics appear with non-zero or zero values

**Expected Results**:

- All supported GPU models export 19 metrics successfully
- Values match AMD-SMI JSON output on each platform

**Platform Coverage Matrix** (Reference: https://instinct.docs.amd.com/projects/gpu-operator/en/latest/):
| GPU Model | Generation | Driver | Expected Behavior |
|-----------|------------|--------|-------------------|
| MI355X | Latest | amdgpu 6.4.x+ | ✓ All 19 metrics |
| MI350X | Latest | amdgpu 6.4.x+ | ✓ All 19 metrics |
| MI325X | MI3xx | amdgpu 6.4.x+ | ✓ All 19 metrics |
| MI300X | MI3xx | amdgpu 6.4.x+ | ✓ All 19 metrics |
| MI250 / MI250X | MI2xx | amdgpu 6.4.x+ | ✓ All 19 metrics |
| MI210 | MI2xx | amdgpu 6.4.x+ | ✓ All 19 metrics |

**Kubernetes/OpenShift Coverage**:
| Platform | Versions | Status |
|----------|----------|--------|
| Kubernetes | 1.29 - 1.35 | ✓ Supported |
| OpenShift (RHCOS) | 4.20, 4.21 | ✓ Supported |

---

#### TS-4.2: SR-IOV/Hypervisor Platform Support

**Priority**: P1 (Critical)
**PRD Reference**: Section 2.1.3, 3.4, 5.5.4
**Requirements**: REQ-4.2

**Objective**: Verify deferred error metrics work in SR-IOV/hypervisor environments.

**Prerequisites**:

- SR-IOV enabled GPU
- GIM driver 8.3.0.K or higher
- GPUAgent built with gimamdsmi support

**Test Steps**:

1. Deploy exporter in SR-IOV environment
2. Enable deferred error metrics in config
3. Verify AMD-SMI (GIM variant) populates `deferred_count` field
4. Query `/metrics` endpoint
5. Compare with baremetal behavior

**Expected Results**:

- If GIM AMD-SMI supports `deferred_count`: All 19 metrics export (mark metricslist.md Hypervisor column as ✓)
- If GIM AMD-SMI does NOT support: Field logger marks as unsupported, metrics skipped (mark metricslist.md Hypervisor column as ✗)
- No crashes or errors in either case

**Note**: PRD marks this as "TBD" pending actual SR-IOV testing. Update metricslist.md documentation based on test results.

---

#### TS-4.3: Unsupported Platform Graceful Handling

**Priority**: P1 (Critical)
**PRD Reference**: Section 3.5, 5.5.3
**Requirements**: REQ-4.3

**Objective**: Verify field logger pattern handles unsupported platforms gracefully.

**Prerequisites**:

- Platform where AMD-SMI returns 0 for `deferred_count` (or older driver)

**Test Steps**:

1. Deploy exporter on platform with no deferred error support
2. Enable deferred error metrics in config
3. Query AMD-SMI from inside exporter pod:

   ```bash
   kubectl exec -n <namespace> <exporter-pod> -c metrics-exporter-container -- amd-smi metric --json
```

   Verify deferred_count is 0 for all blocks in JSON output
4. Query `/metrics` endpoint via NodePort
5. Check exporter logs for field logger messages

**Expected Results**:

- Field logger logs once per metric: `"field amd_gpu_ecc_deferred_<block> not supported on this platform"`
- Metrics NOT exported in `/metrics` endpoint (skipped)
- No repeated error logging on subsequent scrapes
- Exporter continues running normally
- Other metrics continue to export

---

### 3.5 Multi-GPU and Partitioned GPU Testing

#### TS-5.1: Multi-GPU Support

**Priority**: P0 (Blocker)
**PRD Reference**: Section 2.3, 5.5.5
**Requirements**: REQ-5.1

**Objective**: Verify deferred error metrics export correctly for all GPUs in multi-GPU system.

**Prerequisites**:

- System with 2+ AMD GPUs (non-partitioned)
- All GPUs are MI2xx or MI3xx

**Test Steps**:

1. Enable deferred error metrics for all GPUs (config: `"selector": "all"`)
2. Query `/metrics` endpoint
3. Parse metrics and group by `gpu_id` label
4. Verify each GPU has complete set of 19 metrics

**Expected Results**:

- Each GPU exports all 19 deferred error metrics
- `gpu_id` label correctly identifies each GPU (0, 1, 2, ...)
- `gpu_uuid` label unique per GPU
- Values independent per GPU (GPU 0 errors ≠ GPU 1 errors)

**Example Multi-GPU Output**:

```bash
amd_gpu_ecc_deferred_total{gpu_id="0",gpu_uuid="GPU-...-abc",hostname="node1"} 42
amd_gpu_ecc_deferred_total{gpu_id="1",gpu_uuid="GPU-...-def",hostname="node1"} 15
amd_gpu_ecc_deferred_umc{gpu_id="0",gpu_uuid="GPU-...-abc",hostname="node1"} 37
amd_gpu_ecc_deferred_umc{gpu_id="1",gpu_uuid="GPU-...-def",hostname="node1"} 10
```

**Reference**: See PRD Section 5.5.5 and [kb_source/exporter/gpu-metrics-details.md](../../../device-metrics-exporter/kb_source/exporter/gpu-metrics-details.md) for multi-GPU ECC error injection procedures.

---

#### TS-5.2: Partitioned GPU - SPX Mode (Non-Partitioned)

**Priority**: P0 (Blocker)
**PRD Reference**: Section 3.3, 5.5.4
**Requirements**: REQ-5.2

**Objective**: Verify SPX mode (non-partitioned) exports all deferred error metrics.

**Prerequisites**:

- MI3xx GPU configured in SPX mode (single partition)
- Exporter configured to monitor GPU

**Test Steps**:

1. Verify GPU partition mode: `amd-smi list` → check partition count = 1
2. Enable deferred error metrics in config
3. Query `/metrics` endpoint
4. Verify all 19 metrics present for partition 0

**Expected Results**:

- Single GPU (partition 0) exports all 19 deferred error metrics
- No partition-related field logger messages
- Behavior identical to non-partitionable GPUs (MI210/MI250)

---

#### TS-5.3: Partitioned GPU - CPX/DPX/QPX Partition 0

**Priority**: P0 (Blocker)
**PRD Reference**: Section 3.3, 5.5.4, 8.6
**Requirements**: REQ-5.2

**Objective**: Verify partition 0 in CPX/DPX/QPX modes exports all deferred error metrics.

**Prerequisites**:

- MI3xx GPU configured in CPX (2), DPX (4), or QPX (8) partition mode
- Exporter configured to monitor partition 0

**Test Steps**:

1. Verify GPU partition mode: `amd-smi list` → check partition count
2. Enable deferred error metrics in config for partition 0
3. Query `/metrics` endpoint
4. Verify all 19 metrics present for gpu_id corresponding to partition 0

**Expected Results**:

- **CPX (2 partitions)**: Partition 0 exports all 19 metrics
- **DPX (4 partitions)**: Partition 0 exports all 19 metrics
- **QPX (8 partitions)**: Partition 0 exports all 19 metrics
- Values match AMD-SMI output for partition 0
- No field logger warnings for partition 0

---

#### TS-5.4: Partitioned GPU - Secondary Partitions Graceful Skip

**Priority**: P1 (Critical)
**PRD Reference**: Section 3.3, 5.5.4, 8.6
**Requirements**: REQ-5.3

**Objective**: Verify secondary partitions (1-7) gracefully skip ECC deferred error metrics.

**Prerequisites**:

- MI3xx GPU in CPX/DPX/QPX mode (multiple partitions)
- Exporter configured to monitor all partitions

**Test Steps**:

1. Configure exporter to monitor all partitions (e.g., QPX with 8 partitions)
2. Enable deferred error metrics in config
3. Query `/metrics` endpoint
4. Verify metrics appear ONLY for partition 0
5. Check exporter logs for field logger messages

**Expected Results**:

- **Partition 0**: All 19 metrics exported
- **Partitions 1-7**: No deferred error metrics in output
- Field logger logs once per partition: `"field amd_gpu_ecc_deferred_* not supported on partition X"`
- No crashes or repeated errors
- Other metrics (non-ECC) continue to export for all partitions

**Partition Coverage Test Matrix**:
| Mode | Partitions | Expected Behavior |
|------|------------|-------------------|
| SPX | 1 | Partition 0: ✓ All metrics |
| CPX | 2 | Partition 0: ✓ All metrics, Partition 1: ✗ Skip |
| DPX | 4 | Partition 0: ✓ All metrics, Partitions 1-3: ✗ Skip |
| QPX | 8 | Partition 0: ✓ All metrics, Partitions 1-7: ✗ Skip |

---

### 3.6 Static Metric Behavior and Error Injection

#### TS-6.1: Static Metric Behavior (Workload Independence)

**Priority**: P1 (Critical)
**PRD Reference**: Section 1.3, 5.5.1
**Requirements**: REQ-6.1

**Objective**: Verify deferred error metrics are static (don't change with workload activity).

**Prerequisites**:

- System with baseline deferred error counts
- Ability to run GPU workloads

**Test Steps**:

1. Query baseline deferred error counts: `curl localhost:2112/metrics | grep amd_gpu_ecc_deferred`
2. Record all 19 metric values
3. Run GPU workload (e.g., matrix multiplication, AI training)
4. Query metrics during workload
5. Stop workload and query metrics again
6. Compare values

**Expected Results**:

- Metric values **DO NOT change** with workload activity
- Values remain constant unless actual ECC errors occur in hardware
- Behavior contrasts with dynamic metrics (power, temperature, utilization)

**Static vs Dynamic Classification**:

- **Static**: Deferred errors (accumulated counters, hardware events)
- **Dynamic**: Power, temperature, memory usage (workload-dependent)

**Reference**: See PRD Section 5.5.1 and [kb_source/exporter/gpu-metrics-details.md](../../../device-metrics-exporter/kb_source/exporter/gpu-metrics-details.md) for detailed explanation of static vs dynamic metrics.

---

#### TS-6.2: ECC Error Injection Testing

**Priority**: P0 (Blocker)
**PRD Reference**: Section 5.5.5
**Requirements**: REQ-6.2

**Objective**: Verify deferred error metrics increment when errors are injected.

**Prerequisites**:

- `metricsclient` tool installed and functional
- System with controllable ECC error injection

**Test Steps**:

1. **Baseline**: Query current deferred error counts

   ```bash
   curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred_umc

   # amd_gpu_ecc_deferred_umc{gpu_id="0"} 0

```

2. **Inject**: Use metricsclient to inject UMC deferred errors

   ```bash
   metricsclient set-error --gpu-id 0 --error-type GPU_ECC_DEFERRED_UMC --count 5
```

3. **Verify**: Query metrics and confirm increment

   ```bash
   curl -s http://<node-ip>:32500/metrics | grep amd_gpu_ecc_deferred_umc

   # amd_gpu_ecc_deferred_umc{gpu_id="0"} 5

```

4. **Total Check**: Verify `amd_gpu_ecc_deferred_total` also incremented by 5

5. **Reset**: Reset errors to baseline

   ```bash
   metricsclient reset-error --gpu-id 0
```

**Expected Results**:

- Metric value increments from 0 to 5
- `amd_gpu_ecc_deferred_total` updates to reflect new errors
- Increment matches injection count exactly
- Reset restores baseline values

**Multi-Block Injection Test**:

- Inject errors in multiple blocks (UMC=5, GFX=3, SDMA=2)
- Verify each per-block metric increments correctly
- Verify `amd_gpu_ecc_deferred_total` = sum of all blocks (5+3+2=10)

**Reference**: See [kb_source/exporter/gpu-metrics-details.md](../../../device-metrics-exporter/kb_source/exporter/gpu-metrics-details.md) - ECC Error Injection section for detailed metricsclient procedures.

---

### 3.7 Integration and Negative Testing

#### TS-7.1: No Impact on GPU Health Service

**Priority**: P0 (Blocker)
**PRD Reference**: Section 1.3, 1.4
**Requirements**: REQ-7.1

**Objective**: Verify deferred error metrics do NOT affect GPU health service determination.

**Prerequisites**:

- GPU health service enabled
- Deferred error metrics enabled

**Test Steps**:

1. Query GPU health status (healthy baseline)
2. Inject high deferred error count (e.g., UMC=1000)
3. Query GPU health status again
4. Verify health status unchanged (still healthy)
5. Compare with correctable/uncorrectable error behavior (which DO affect health)

**Expected Results**:

- GPU health service ignores deferred error counts
- Health status based ONLY on correctable/uncorrectable errors (existing behavior)
- Deferred errors are "standard metrics", NOT "critical metrics"
- No changes to `gpumetricssvc.proto` or health determination logic

**Documentation Check**:

- Verify `internal/metricsmap.md` does NOT list deferred errors in Critical Metrics section
- Verify PRD Section 1.4 explicitly states "NOT Critical"

---

#### TS-7.2: Negative Test - Invalid Config Values

**Priority**: P2 (Normal)
**PRD Reference**: Section 5.5.3
**Requirements**: None (error handling)

**Objective**: Verify exporter handles invalid config gracefully.

**Prerequisites**:

- Exporter running

**Test Steps**:

1. Add invalid field name to config: `"GPU_ECC_DEFERRED_INVALID_BLOCK"`
2. Reload config
3. Check exporter logs for warning/error
4. Verify exporter continues running
5. Verify valid fields still export correctly

**Expected Results**:

- Exporter logs warning about unknown field
- Invalid field ignored
- Valid fields continue to export normally
- No crash or service disruption

---

### 3.8 Documentation Validation

#### TS-8.1: Documentation Updates Complete

**Priority**: P1 (Critical)
**PRD Reference**: Section 6
**Requirements**: REQ-8.1

**Objective**: Verify all required documentation updated.

**Prerequisites**:

- Code implementation complete
- SR-IOV testing complete (to confirm Hypervisor column values)

**Test Steps**:

1. Check `docs/configuration/metricslist.md`:
   - All 19 deferred error metrics listed in ECC Error Metrics section
   - Hypervisor column marked ✓ or ✗ based on SR-IOV test results
   - Platform tags `[MI2xx, MI3xx]` present

2. Check `docs/index.md`:
   - Compatibility matrix updated if driver requirements changed
   - Driver version requirements: amdgpu 6.4.x+ (baremetal), GIM 8.3.0.K+ (SR-IOV)

3. Check `docs/releasenotes.md`:
   - Release notes entry for deferred error metrics
   - Lists all 19 metrics
   - Notes partition 0 limitation

4. Check `internal/metricsmap.md`:
   - All 19 metrics mapped to proto fields and AMD-SMI fields
   - Metrics NOT in Critical Metrics list
   - Platform column shows MI2xx, MI3xx

5. Check `example/config.json`:
   - Example configuration includes deferred error metrics

**Expected Results**:

- ✓ All 5 documentation files updated
- ✓ Hypervisor support status confirmed (not "TBD")
- ✓ Partition limitation documented
- ✓ No references to deferred errors as "critical metrics"
- ✓ References to `kb_source/exporter/gpu-metrics-details.md` for ECC special cases

---

## 4. Test Data Requirements

### 4.1 Test Environments

**Hardware Requirements**:

- **Primary Test Platform**: MI300X or MI325X GPU (baremetal, non-partitioned)
- **Secondary Test Platform**: MI210 or MI250X GPU
- **Latest Generation Test**: MI350X or MI355X GPU (when available)
- **Partitioned GPU Test**: MI300X in QPX mode (8 partitions)
- **Multi-GPU Test**: System with 2-4 GPUs (MI300X or MI325X)
- **SR-IOV Test**: SR-IOV enabled GPU with GIM driver

**Software Requirements**:

- amdgpu driver 6.4.x or higher (baremetal)
- GIM driver 8.3.0.K or higher (SR-IOV)
- AMD-SMI latest version (must support `amd-smi metric --json`)
- Kubernetes 1.29-1.35 OR OpenShift 4.20-4.21
- Device-metrics-exporter with deferred error metrics implementation
- metricsclient tool for error injection
- kubectl or oc CLI for cluster access

### 4.2 Baseline Data

**Baseline ECC Error Counts**:

- Collect baseline deferred error counts from production systems
- Expected: Most systems have 0 deferred errors (healthy state)
- If non-zero: Document baseline values before testing

### 4.3 Injected Error Data

**metricsclient Injection Scenarios**:

- **Single Block**: UMC block only (most common ECC error location)
- **Multiple Blocks**: UMC + GFX + SDMA
- **High Count**: Inject 1000+ errors to test large counter values
- **Multi-GPU**: Inject different counts per GPU (GPU 0: 100, GPU 1: 50)

**Reference**: [kb_source/exporter/gpu-metrics-details.md](../../../device-metrics-exporter/kb_source/exporter/gpu-metrics-details.md) - ECC Error Injection section for detailed procedures.

### 4.4 Configuration Test Data

**Config Variations**:

1. All 19 metrics enabled
2. Only total metric enabled (`GPU_ECC_DEFERRED_TOTAL`)
3. Subset of per-block metrics (e.g., UMC, GFX, DF only)
4. All metrics disabled (empty fields array)
5. Mixed with other ECC metrics (correctable + uncorrectable + deferred)

---

## 5. Test Environment Requirements

### 5.1 Hardware

| Component | Requirement | Quantity | Purpose |
|-----------|-------------|----------|---------|
| MI300X/MI325X GPU | Non-partitioned, Kubernetes | 1 | Primary functional testing |
| MI300X GPU | QPX mode (8 partitions) | 1 | Partition testing |
| MI210/MI250X GPU | Kubernetes | 1 | MI2xx platform coverage |
| MI350X/MI355X GPU | Kubernetes | 1 | Latest generation coverage |
| Multi-GPU System | 2-4 GPUs (MI3xx) | 1 | Multi-GPU testing |
| SR-IOV GPU | GIM driver, OpenShift | 1 | Hypervisor/SR-IOV testing |

### 5.2 Software

| Component | Version | Purpose |
|-----------|---------|---------|
| amdgpu driver | 6.4.x or higher | Baremetal AMD-SMI support |
| GIM driver | 8.3.0.K or higher | SR-IOV AMD-SMI support |
| AMD-SMI | Latest | Ground truth (`amd-smi metric --json`) |
| Kubernetes | 1.29 - 1.35 | K8s deployment testing |
| OpenShift | 4.20, 4.21 | OpenShift deployment testing |
| metricsclient | Latest | Safe ECC error injection |
| GPUAgent | With deferred error support | gRPC service (inside exporter pod) |
| Device Metrics Exporter | Feature branch | Implementation under test |
| kubectl / oc | Latest | Cluster access and pod exec commands |

### 5.3 Network and Access

- Access to `/metrics` endpoint via Kubernetes NodePort (32500) or ClusterIP (5000)
- kubectl/oc access for pod exec commands (to run `amd-smi` inside exporter pod)
- Access to metricsclient tool for error injection
- Exporter logs accessible for field logger verification
- Network connectivity to Kubernetes API server

---

## 6. Test Coverage Matrix

| Component | Functional | Integration | Platform | Deployment | Partition | Negative | Coverage % |
|-----------|------------|-------------|----------|------------|-----------|----------|------------|
| 19 Metrics Export | TS-1.1, TS-1.2, TS-1.3 | - | - | - | - | - | 100% |
| Metric Accuracy | TS-2.2 | TS-2.1 | TS-4.1 | - | - | - | 100% |
| Configuration | TS-3.1, TS-3.2 | TS-3.3 | - | - | - | TS-7.2 | 100% |
| Platforms | - | - | TS-4.1, TS-4.2 | - | - | TS-4.3 | 100% |
| Multi-GPU | TS-5.1 | - | - | - | - | - | 100% |
| Partitions | - | - | - | - | TS-5.2, TS-5.3, TS-5.4 | - | 100% |
| Static Behavior | TS-6.1 | - | - | - | - | - | 100% |
| Error Injection | TS-6.2 | - | - | - | - | - | 100% |
| Health Service | TS-7.1 | - | - | - | - | - | 100% |
| Documentation | TS-8.1 | - | - | - | - | - | 100% |

**Overall Coverage Target**: 100% of PRD requirements

---

## 7. Test Priorities and Execution Order

### 7.1 P0 Tests (Blocker - Must Pass Before Release)

**Execution Order**:

1. **TS-1.1**: Verify all 19 metrics export
2. **TS-1.2**: Verify metric naming convention
3. **TS-1.3**: Verify metric labels
4. **TS-2.1**: Verify AMD-SMI integration
5. **TS-2.2**: Validate metric accuracy vs AMD-SMI
6. **TS-3.1**: Enable metrics via config
7. **TS-3.2**: Disable metrics via config
8. **TS-4.1**: MI2xx and MI3xx platform support
9. **TS-5.1**: Multi-GPU support
10. **TS-5.2**: Partitioned GPU - SPX mode
11. **TS-5.3**: Partitioned GPU - Partition 0 in CPX/DPX/QPX
12. **TS-6.2**: ECC error injection testing
13. **TS-7.1**: No impact on GPU health service

**Estimated Execution Time**: 4-6 hours (automated)

### 7.2 P1 Tests (Critical - Should Pass Before Release)

**Execution Order**:

14. **TS-3.3**: Config auto-reload
15. **TS-4.2**: SR-IOV/hypervisor support
16. **TS-4.3**: Unsupported platform handling
17. **TS-5.4**: Secondary partitions graceful skip
18. **TS-6.1**: Static metric behavior
19. **TS-8.1**: Documentation updates

**Estimated Execution Time**: 2-3 hours (automated + manual doc review)

### 7.3 P2 Tests (Normal - Nice to Have)

**Execution Order**:

20. **TS-7.2**: Invalid config values

**Estimated Execution Time**: 30 minutes

### 7.4 Total Estimated Test Execution Time

- **Automated Tests**: 6-8 hours
- **Manual Documentation Review**: 1 hour
- **Total**: 7-9 hours (one working day)

---

## 8. Risks and Dependencies

### 8.1 External Dependencies

**Dependency 1: GPUAgent Submodule**

- **Risk**: GPUAgent proto and implementation changes must be complete and tested
- **Mitigation**: Coordinate with ROCm team early, test gpuagent independently
- **Blockers**: TS-2.1, TS-2.2 blocked until gpuagent changes available

**Dependency 2: AMD-SMI Field Availability**

- **Risk**: `deferred_count` field may not be populated on all platforms/drivers
- **Mitigation**: Field logger pattern handles gracefully, test on multiple platforms
- **Impact**: May affect TS-4.1, TS-4.2 results

**Dependency 3: SR-IOV Test Environment**

- **Risk**: SR-IOV test environment may not be available
- **Mitigation**: Mark SR-IOV support as "TBD" until environment available
- **Impact**: TS-4.2 may be delayed

### 8.2 Platform Availability

**Risk**: Limited access to partitioned GPU configurations (CPX/DPX/QPX)

- **Mitigation**: Prioritize QPX testing (covers all partition scenarios)
- **Impact**: TS-5.2, TS-5.3, TS-5.4 may need to be scheduled based on hardware availability

### 8.3 Data Availability

**Risk**: Real ECC error injection may be risky or unavailable

- **Mitigation**: Use metricsclient for safe mock injection (preferred method)
- **Impact**: Minimal - metricsclient sufficient for functional validation

### 8.4 Known Limitations

**Limitation 1: Primary Partition Only**

- **Impact**: Cannot test ECC metrics on secondary partitions (expected behavior)
- **Validation**: Ensure field logger marks partitions 1-7 as unsupported

**Limitation 2: No Health Service Integration**

- **Impact**: Cannot validate health service behavior with deferred errors
- **Validation**: TS-7.1 verifies deferred errors do NOT affect health (negative test)

---

## 9. Test Execution Strategy

### 9.1 CI/CD Integration

**Automated Test Execution**:

- All test scenarios (TS-1.1 through TS-7.2) designed for automation
- No manual testing required for functional validation
- Integrate into existing device-metrics-exporter CI pipeline

**Test Stages**:

1. **Unit Tests**: Metric registration, field mapping, proto compilation
2. **Integration Tests**: AMD-SMI integration, gpuagent gRPC communication
3. **Functional Tests**: Config control, metric export, accuracy validation
4. **Platform Tests**: Multi-GPU, partitioned GPU, SR-IOV (if available)

### 9.2 Manual Testing

**Manual Testing Required**:

- TS-8.1: Documentation review (human verification)
- SR-IOV testing if automated environment unavailable

**Estimated Manual Effort**: 1-2 hours

### 9.3 Regression Testing

**Existing ECC Metrics Regression**:

- Verify correctable and uncorrectable ECC metrics still work correctly
- Verify deferred error metrics don't interfere with existing metrics
- Run existing ECC metric test suite alongside new tests

### 9.4 Performance Testing

**Included in Standard Performance Suite**:

- Metric collection overhead (target: < 1% CPU)
- Multi-GPU scalability (8+ GPUs)
- 24-hour stress test (no memory leaks)

**Not Specific to This Feature**: Use existing performance regression tests.

---

## 10. Acceptance Criteria

### 10.1 Functional Acceptance

- ✅ All 19 deferred error metrics export correctly on MI2xx and MI3xx platforms
- ✅ Metric values match AMD-SMI output exactly (zero tolerance)
- ✅ Configuration enable/disable controls work correctly
- ✅ Multi-GPU support: Each GPU exports independent metric values
- ✅ Partitioned GPU: Partition 0 exports metrics, partitions 1-7 skip gracefully
- ✅ Field logger handles unsupported platforms without errors
- ✅ No impact on GPU health service determination

### 10.2 Platform Acceptance

- ✅ All 6 GPU models supported: MI355X, MI350X, MI325X, MI300X, MI250/MI250X, MI210
- ✅ Kubernetes 1.29-1.35 tested and working
- ✅ OpenShift 4.20, 4.21 tested and working
- ✅ SR-IOV support confirmed or documented as unsupported
- ✅ SPX/CPX/DPX/QPX partition modes tested and working

### 10.3 Feature Acceptance

- ✅ All P0 tests pass (13 test scenarios)
- ✅ All P1 tests pass (6 test scenarios)
- ✅ No regressions in existing ECC metrics
- ✅ Documentation updated:
  - `docs/configuration/metricslist.md` (with SR-IOV results)
  - `docs/index.md` compatibility matrix (if driver/platform requirements changed)
  - `docs/releasenotes.md`
  - `internal/metricsmap.md` (NOT in Critical Metrics section)
  - Reference to `kb_source/exporter/gpu-metrics-details.md` for ECC special cases
- ✅ Example config includes deferred error metrics
- ✅ No performance regression (< 1% overhead)

### 10.4 Test Automation Acceptance

- ✅ All functional tests automated and integrated into CI/CD
- ✅ Test execution time < 10 hours
- ✅ Test results reproducible across runs
- ✅ Clear pass/fail criteria for each test scenario

---

## 11. Post-Release Validation

### 11.1 Production Monitoring

**Metrics to Monitor**:

- Adoption rate: How many deployments enable deferred error metrics?
- Error detection rate: How often do deferred errors appear in production?
- Performance impact: Actual CPU/memory overhead in production

### 11.2 Customer Feedback

**Feedback Areas**:

- Metric usefulness for reliability monitoring
- Alerting thresholds and best practices
- Documentation clarity
- Feature requests (e.g., rate of change metrics)

### 11.3 Documentation Updates

**Post-Release Documentation**:

- Add customer use cases and examples
- Document recommended Prometheus alerting rules
- Add troubleshooting guide for deferred error metrics
- Update FAQ if common questions arise

---

## 12. References

### 12.1 PRD and Requirements

- **PRD-GPU-20260406-01**: `/home/srivatsa/ws-3/device-metrics-exporter/kb_source/prds/2026/Q2/PRD-GPU-20260406-01-ecc-deferred-errors.md`
- **PRD Section 5.5**: System Test Validation Criteria (primary source for test requirements)

### 12.2 Device Metrics Exporter Documentation

- `docs/configuration/metricslist.md` - User-facing metrics catalog
- `docs/index.md` - Compatibility matrix and platform requirements
- `internal/metricsmap.md` - Internal metric mappings and critical metrics list
- `kb_source/exporter/gpu-metrics-details.md` - Static/dynamic metrics, ECC error injection, special cases

### 12.3 Testing Tools and Procedures

- `metricsclient` - ECC error injection tool (safe, recommended)
- `amd-smi metric --json` - AMD-SMI command-line tool (ground truth for validation)
- `collect_metrics_samples()` - Function in `tests/pytests/lib/metric_util.py` for parallel metric collection
- `kubectl exec` - Execute AMD-SMI inside exporter pod for ground truth validation
- [kb_source/exporter/gpu-metrics-details.md](../../../device-metrics-exporter/kb_source/exporter/gpu-metrics-details.md) - Detailed ECC error injection procedures
- [kb_source/common/device-metrics-exporter.md](../../../kb_source/common/device-metrics-exporter.md) - Testing infrastructure and workflow
- [kb_source/common/platform-support.md](../../../kb_source/common/platform-support.md) - Platform coverage requirements

### 12.4 Related Test Plans

- Existing ECC metrics test plan (correctable/uncorrectable errors)
- Device-metrics-exporter performance test suite
- Multi-GPU test suite

---

## 13. Approval Sign-Off

**Test Plan Review**:

- [ ] Engineering Lead: _______________ Date: ___________
- [ ] Test Lead: _______________ Date: ___________
- [ ] Product Manager: _______________ Date: ___________

**Test Plan Approval**:

- [ ] Ready for Implementation: Yes / No
- [ ] Modifications Required: _______________________________________________

**Notes**:
_______________________________________________________________________________
_______________________________________________________________________________

---

**Next Steps After Approval**:

1. Use `/pytest-dev` skill to implement testcases from this approved test plan
2. Execute test suite on target platforms
3. Update documentation based on test results (especially SR-IOV Hypervisor column)
4. Review test results with stakeholders before release

---

**Test Plan Version**: 1.0
**Last Updated**: 2026-04-06
**Approvers**: Pending
