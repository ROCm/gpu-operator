# KB: verify_label() Must Check All GPU Nodes

## Issue

In multi-node GPU clusters, partition tests pass even when some nodes fail partition operation.

## Root Cause

Original `verify_label()` implementation only checked `gpu_nodes[0]` (first node) for partition success state. If first node succeeded but other nodes failed, test would incorrectly pass.

## Symptoms

1. Test reports partition success but some nodes show failure in logs
2. Kubernetes events show mixed success/failure across nodes
3. `kubectl get nodes` shows different `dcm.amd.com/gpu-config-profile-state` values
4. Subsequent tests fail because some nodes are in bad state

## Incorrect Implementation

```python
def verify_label(environment, profile):
    i = 0
    while i < 40:
        ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
        if gpu_nodes:

            # BUG: Only checks first node!

            prof = gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile', 'NA')
            stat = gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state', 'unknown')

            if stat == "failure":
                debug_on_failure(environment, False, f"DCM partition operation failed")
                return

            if prof == profile and stat == "success":
                break

        i += 1
        time.sleep(10)
```

## Correct Implementation

```python
def verify_label(environment, profile):
    """Verify that partition profile change succeeded via node labels.

    Polls node labels until DCM marks the partition change as successful or failed
    on ALL GPU nodes. DCM updates these labels after partition completes:
        dcm.amd.com/gpu-config-profile: <profile>
        dcm.amd.com/gpu-config-profile-state: success|failure

    Polls for up to 400 seconds (40 attempts x 10 seconds) to allow partition
    operation to complete, which can take several minutes depending on GPU series.
    Fails immediately if DCM reports failure state on any node.

    Args:
        environment: Test environment for error reporting.
        profile: Expected partition profile name (e.g., "QPX_NPS1").

    Raises:
        AssertionError: If success state not observed within 400 seconds, or if failure detected.
    """
    i = 0
    while i < 40:
        ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
        if gpu_nodes:
            all_nodes_success = True
            all_nodes_status = []

            # Check ALL GPU nodes, not just the first one

            for node in gpu_nodes:
                node_name = node['metadata']['labels'].get('kubernetes.io/hostname', 'unknown')
                prof = node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile', 'NA')
                stat = node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state', 'unknown')
                all_nodes_status.append(f"{node_name}: profile={prof}, state={stat}")

                # Fail immediately if any node reports failure

                if stat == "failure":
                    Logger.error(f"DCM reported failure on node {node_name}: profile={prof}, state={stat}")
                    debug_on_failure(environment, False,
                                    f"DCM partition operation failed on {node_name}: profile={prof}, state={stat}")
                    return

                # Check if this node matches expected state

                if not (prof == profile and stat == "success"):
                    all_nodes_success = False

            # If all nodes show success, we're done

            if all_nodes_success:
                Logger.info(f"Partition profile {profile} applied successfully on all {len(gpu_nodes)} nodes")
                for status in all_nodes_status:
                    Logger.debug(f"  {status}")
                break
            else:
                Logger.debug(f"Attempt {i+1}/40 - waiting for all nodes to reach success state:")
                for status in all_nodes_status:
                    Logger.debug(f"  {status}")

        i += 1
        time.sleep(10)

    # Timeout - show final state of all nodes

    if i >= 40:
        Logger.error("Timeout waiting for partition profile to apply on all nodes. Final state:")
        for status in all_nodes_status:
            Logger.error(f"  {status}")
        debug_on_failure(environment, False,
                         f"Didn't find gpu-config-profile-state=success on all nodes after 400s")
```

## Key Changes

1. **Iterate through ALL gpu_nodes**: `for node in gpu_nodes:`
2. **Collect status for each node**: Build `all_nodes_status` list with per-node details
3. **Fail if ANY node fails**: Check `stat == "failure"` for each node
4. **Succeed only if ALL succeed**: `all_nodes_success` requires every node to match
5. **Enhanced logging**: Show which nodes are pending, failed, or succeeded

## Validation

After fixing, verify:

1. Test fails if any node shows `state=failure`
2. Test waits for all nodes to reach `state=success`
3. Logs show status for each node during polling
4. Multi-node clusters show consistent partition state

## Related Code Patterns

When applying partition profiles or taints, always iterate over all nodes:

### Correct: Label All Nodes

```python
labels_dict = {"dcm.amd.com/gpu-config-profile": profile}
for node in gpu_nodes:
    node_name = node['metadata']['labels']['kubernetes.io/hostname']
    k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
```

### Correct: Taint All Nodes

```python
for node in gpu_nodes:
    node_name = node['metadata']['labels']['kubernetes.io/hostname']
    k8_util.k8_taint_node(node_name, taint_add=True, effect="NoExecute")
```

### Correct: Untaint All Nodes

```python
def _untaint_all_nodes():
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        k8_util.k8_untaint_node(node_name, effects=["NoSchedule", "NoExecute"])
```

## Files Modified

- `tests/pytests/k8/gpu-operator/test_config_manager.py`: Lines 958-1022 (verify_label function)

## Commit

- `aefdfe6a`: fix: verify partition profile on all GPU nodes, not just first node
