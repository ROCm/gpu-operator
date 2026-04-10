# KB: Ensure Cleanup Runs Even on Partition Failure

## Issue

When partition tests fail, nodes remain tainted and labeled, requiring manual cleanup to restore cluster.

## Root Cause

`reset_dcm_profile()` performed cleanup (untaint, remove labels) at the end of the function. If `verify_label()` or other operations failed before reaching cleanup code, cleanup never executed.

When `verify_label()` detects failure, it calls `debug_on_failure()` → `pytest.fail()`, which raises exception and exits function before cleanup runs.

## Symptoms

1. After failed test, nodes show:
   - Taint: `amd-dcm=up:NoExecute`
   - Labels: `dcm.amd.com/gpu-config-profile=<profile>`, `dcm.amd.com/gpu-config-profile-state=failure`
2. Device-plugin pods not scheduled (evicted by taint)
3. Subsequent tests cannot run (cluster in bad state)
4. Manual intervention required:

   ```bash
   kubectl untaint nodes <node> amd-dcm=up:NoExecute
   kubectl label nodes <node> dcm.amd.com/gpu-config-profile-
   kubectl label nodes <node> dcm.amd.com/gpu-config-profile-state-
```

## Solution: Use try/finally Block

Wrap partition logic in try block, move cleanup to finally block to ensure it ALWAYS runs:

```python
def reset_dcm_profile(gpu_cluster, environment, skip_reboot=True):
    namespace = environment.gpu_operator_namespace
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()

    gpu_series = get_gpu_series(gpu_cluster, environment)
    debug_on_failure(environment, gpu_series != None, f"Missing gpu-series information")

    # Wrap partition logic in try/finally to ensure cleanup always happens

    try:
        if gpu_series and 'MI3' in gpu_series:
            if _any_gpu_partitioned(gpu_nodes):

                # Patch DeviceConfig for NoExecute toleration

                # ... patch logic ...

                # Taint nodes

                for node in gpu_nodes:
                    node_name = node['metadata']['labels']['kubernetes.io/hostname']
                    k8_util.k8_taint_node(node_name, taint_add=True, effect="NoExecute")

                # Label nodes with SPX_NPS1 profile

                labels_dict = {"dcm.amd.com/gpu-config-profile": "SPX_NPS1"}
                for node in gpu_nodes:
                    node_name = node['metadata']['labels']['kubernetes.io/hostname']
                    k8_util.k8_label_node(node_name, labels_dict, overwrite=True)

                # Wait for partition to complete (may raise exception)

                verify_label(environment, "SPX_NPS1")

                # Verify partition state

                if _any_gpu_partitioned(gpu_nodes):
                    Logger.error(f"Failed to restore DCM profile to default SPX_NPS1")
                else:
                    Logger.info(f"GPUs restored to default partition status (SPX_NPS1)")
            else:
                Logger.info(f"GPUs already in default partition status")
    finally:

        # Step 5: Cleanup - Remove DCM labels and taints

        # This MUST run even if partition fails to avoid leaving nodes tainted

        Logger.info("Cleaning up DCM labels and taints from nodes")
        labels_dict = {
            "dcm.amd.com/gpu-config-profile": None,
            "dcm.amd.com/gpu-config-profile-state": None
        }
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
            k8_util.k8_untaint_node(node_name, effects=["NoSchedule", "NoExecute"])
            Logger.info(f"Removed DCM labels and taint from {node_name}")

        # Wait for driver reload - don't fail since we're in cleanup

        K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=False)

        # Config-manager MUST be running

        devicecfg_pods = [common.PodInfo('config-manager', len(gpu_nodes), 1)]
        failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace,
                                                   devicecfg_pods, sleep_time=20)
        debug_on_failure(environment, not failed_pods,
                        f"Config-manager not Running: {failed_pods}")
```

## Key Points

### 1. try Block Contains Risky Operations

- Patching DeviceConfig
- Tainting nodes
- Labeling nodes
- Calling `verify_label()` (which can fail)
- Verifying partition state

### 2. finally Block Contains Cleanup

- Remove node labels
- Untaint nodes
- Wait for driver reload
- Verify config-manager ready

### 3. Error Handling in finally

- Use `fail_on_timeout=False` for `wait_for_driver_reload()` since we're cleaning up from potential failure
- Still call `debug_on_failure()` for config-manager as it's required for partition feature
- Log all cleanup actions for debugging

### 4. Also Fixed: Redundant verify_label() Loop

Original code called `verify_label()` inside a loop over nodes:

```python

# WRONG: verify_label already checks all nodes!

for node in gpu_nodes:
    node_name = node['metadata']['labels']['kubernetes.io/hostname']
    verify_label(environment, "SPX_NPS1")  # Called N times!
```

Correct: Call once (verify_label iterates internally):

```python

# CORRECT: verify_label checks all nodes internally

verify_label(environment, "SPX_NPS1")  # Called once
```

## Validation

After applying fix:

1. Trigger partition failure (e.g., invalid profile)
2. Verify cleanup still runs:
   - Nodes untainted: `kubectl get nodes -o json | jq '.items[].spec.taints'`
   - Labels removed: `kubectl get nodes -o json | jq '.items[].metadata.labels' | grep dcm`
3. Subsequent tests can run without manual intervention
4. Device-plugin pods restart automatically

## Related Patterns

### Negative Test Cleanup

Negative tests also need cleanup after rejected partitions:

```python
def test_negative_partitioning(...):

    # ... test logic ...

    time.sleep(20)
    _untaint_all_nodes()

    # Wait for driver and config-manager

    K8Helper.wait_for_driver_reload(environment, gpu_nodes, fail_on_timeout=False)
    devicecfg_pods = [common.PodInfo('config-manager', len(gpu_nodes), 1)]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace,
                                               devicecfg_pods, sleep_time=20)
    debug_on_failure(environment, not failed_pods, f"Config-manager not Running: {failed_pods}")
```

### Positive Test Cleanup

Partition tests use finalizers for cleanup:

```python
def run_partition_test_scenario(...):
    def _untaint_all_nodes():
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            k8_util.k8_untaint_node(node_name, effects=["NoSchedule", "NoExecute"])

    request.addfinalizer(_untaint_all_nodes)

    # ... test logic ...

```

## Files Modified

- `tests/pytests/k8/gpu-operator/test_config_manager.py`: Lines 473-577 (reset_dcm_profile function)

## Commits

- `9b28dcb6`: fix: ensure reset_dcm_profile() cleanup runs even on partition failure
- `244c46db`: fix: wait for driver reload after untaint in reset_dcm_profile()
