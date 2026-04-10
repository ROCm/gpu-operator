# KB: Driver Reload Timing After DCM Partition Operations

## Issue

Tests fail with "Failed to parse amd-smi-partition JSON" error after partition operations complete or fail.

## Root Cause

When GPU partition operations fail or complete, the amdgpu driver may be unloaded/reloading. If the next test starts before driver reload completes, `amd-smi partition -c --json` fails because the driver is not ready.

## Symptoms

1. Cascading test failures: Test1 fails → Test2/3/4 all fail with JSON parse error
2. Error message: `Failed to parse amd_smi_partition JSON document, error : Expecting value: line 1 column 1 (char 0)`
3. Previous test showed partition failure or timeout
4. `lsmod | grep amdgpu` shows driver not loaded on affected node

## Solution

### Wait for Driver Reload

After untainting nodes (which triggers KMM to reload driver), wait for device-plugin pods to be Running:

```python
K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=True)
```

This helper:

- Waits up to 120s (6 retries * 20s)
- Checks device-plugin pods are Running
- Device-plugin Running = driver loaded and amd-smi ready

### Where to Add Wait Logic

#### 1. Start of run_partition_test_scenario()

**Before** collecting pre-partition status:

```python

# Ensure driver is ready before collecting pre-partition status

# Previous test may have left driver unloaded if partition failed

Logger.info("Verifying driver is ready before starting partition test...")
K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=True)

pre_partition_status = {}
for node in gpu_nodes:
    node_name = node['metadata']['labels']['kubernetes.io/hostname']
    pre_partition_status[node_name] = get_partition_status_from_pod(environment, namespace, node_name)
```

#### 2. After untaint in run_partition_test_scenario()

**After** partition completes and nodes are untainted:

```python
_untaint_all_nodes()

# Wait for device-plugin pods to come back after untaint

K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=True)

# Config-manager MUST be running for partition feature

devicecfg_pods = [common.PodInfo('config-manager', len(gpu_nodes), 1)]
failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20)
debug_on_failure(environment, not failed_pods, f"Config-manager not Running after driver reload: {failed_pods}")
```

#### 3. After untaint in test_negative_partitioning()

**After** invalid partition is rejected:

```python
_untaint_all_nodes()
K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=False)

# Config-manager MUST be running

devicecfg_pods = [common.PodInfo('config-manager', len(gpu_nodes), 1)]
failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20)
debug_on_failure(environment, not failed_pods, f"Config-manager not Running after driver reload: {failed_pods}")
```

#### 4. In reset_dcm_profile() finally block

**After** cleanup untaint (ensure runs even on failure):

```python
finally:

    # Cleanup: Remove labels and untaint

    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
        k8_util.k8_untaint_node(node_name, effects=["NoSchedule", "NoExecute"])

    # Wait for driver reload - don't fail since we're in cleanup

    K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=False)

    # Config-manager MUST be running

    devicecfg_pods = [common.PodInfo('config-manager', len(gpu_nodes), 1)]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20)
    debug_on_failure(environment, not failed_pods, f"Config-manager not Running: {failed_pods}")
```

## Implementation Details

### K8Helper.wait_for_driver_reload() (lib/util.py)

```python
@staticmethod
def wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=True):
    """Wait for driver reload to complete after untainting nodes.

    After untainting nodes, KMM reloads the amdgpu driver. This function waits for
    device-plugin pods to be Running, which confirms the driver is loaded and ready
    for amd-smi queries.

    Args:
        environment: Test environment fixture.
        gpu_nodes: List of GPU node objects from k8_get_gpu_nodes().
        fail_on_timeout: If True, call triage() on timeout. If False, just log error.

    Returns:
        bool: True if pods are Running, False if timeout occurred.
    """
    global Logger
    import time
    Logger.info("Waiting for device-plugin to be Running (confirms driver reload complete)...")
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
    ]
    max_retries = 6  # 6 retries * 20s sleep = 120s total wait
    retry_delay = 20
    failed_pods = None
    for retry in range(max_retries):
        failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace,
                                                   devicecfg_pods, sleep_time=retry_delay)
        if not failed_pods:
            Logger.info("Device-plugin pods Running - driver reload complete")
            return True
        if retry < max_retries - 1:
            Logger.warning(f"Pods not ready (retry {retry+1}/{max_retries}): {failed_pods}, retrying in {retry_delay}s...")
            time.sleep(retry_delay)
        else:
            Logger.error(f"Pods failed to become Ready after {max_retries * retry_delay}s: {failed_pods}")

    # Timeout occurred

    if fail_on_timeout:
        K8Helper.triage(environment, False,
                       f"Device-plugin not Running after driver reload: {failed_pods}")
    return False
```

## Example Failure (Job 30081390)

Before fix was applied:

1. Test `QPX_NPS1`: Partition fails, DCM reports failure, driver unloaded
2. Test `DPX_NPS2`: Starts immediately, tries to get pre-partition status, amd-smi fails (driver unloaded), JSON parse error
3. Test `DPX_NPS1`: Same error (driver still not loaded)
4. Test `CPX_NPS1`: Same error
5. Test `CPX_NPS2`: Same error

All subsequent tests cascade-fail due to unloaded driver from first failure.

## Verification

After applying fix, verify:

1. Partition test can recover from previous test failure
2. `lsmod | grep amdgpu` shows driver loaded before amd-smi query
3. device-plugin pods show Running status before test proceeds
4. No cascading failures in test suite

## Related Issues

- Partition timeout increased from 300s → 400s for driver reload on MI325X
- amd-smi retry extended from 50s → 150s to handle driver reload
- verify_label() must check all nodes, not just first one

## Files Modified

- `tests/pytests/lib/util.py`: Added `K8Helper.wait_for_driver_reload()`
- `tests/pytests/k8/gpu-operator/test_config_manager.py`: Added driver wait at 4 locations
  - Start of `run_partition_test_scenario()`
  - After untaint in `run_partition_test_scenario()`
  - After untaint in `test_negative_partitioning()`
  - After untaint in `reset_dcm_profile()` finally block

## Commits

- `7537b2b2`: Initial driver reload wait implementation
- `68133f59`: Added driver readiness check at start of partition tests
