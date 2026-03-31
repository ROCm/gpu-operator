# Troubleshooting

This guide provides steps to diagnose and resolve common issues with the AMD GPU Operator.

## Checking Operator Status

To check the status of the AMD GPU Operator:

```bash
kubectl get pods -n kube-amd-gpu
```

## Collecting Logs

To collect logs from the AMD GPU Operator:

```bash
kubectl logs -n kube-amd-gpu <pod-name>
```

## Potential Issues with ``DeviceConfig``

* Please refer to {ref}`typical-deployment-scenarios` for more information and get corresponding ```helm install``` commands and configs that fits your specific use case.

* If operand pods (e.g. device plugin, metrics exporter) are stuck in ``Init:0/1`` state, it means your GPU worker doesn't have GPU driver loaded or driver was not loaded properly.

  * If you try to use inbox or pre-installed driver please check the node ``dmesg`` to see why the driver was not loaded properly.

  * If you want to deploy out-of-tree driver, we suggest check the [Driver Installation Guide](./drivers/installation) then modify the default ``DeviceConfig`` to ask Operator to install the out-of-tree GPU driver for your worker nodes.

```bash
kubectl edit deviceconfigs -n kube-amd-gpu default
```

* Verify that the DeviceConfig has been applied successfully across all nodes by checking its status. Any configuration issues (such as field validation errors) will be reported in the status section with the `OperatorReady` condition set to `False`. Use the following command to view the status:

```bash
kubectl get deviceconfigs -n kube-amd-gpu default -o yaml
```

```yaml
status:
  conditions:
  - lastTransitionTime: "2026-03-10T09:56:53Z"
    message: ""
    reason: OperatorReady
    status: "True"
    type: Ready
  devicePlugin:
    availableNumber: 1
    desiredNumber: 1
    nodesMatchingSelectorNumber: 1
  metricsExporter:
    availableNumber: 1
    desiredNumber: 1
    nodesMatchingSelectorNumber: 1
  observedGeneration: 1
```

## Debugging Driver Installation

If the AMD GPU driver build fails:

* Check the status of the build pod:

```bash
kubectl get pods -n kube-amd-gpu
```

* View the build pod logs:

```bash
kubectl logs -n kube-amd-gpu <build-pod-name>
```

* Check events for more information:

```bash
kubectl get events -n kube-amd-gpu
```

## Helm Uninstall Hangs or Times Out

If `helm uninstall` hangs and eventually times out with an error like:

```text
Error: 1 error occurred:
        * timed out waiting for the condition
```

This is caused by the pre-delete hook Job that runs before uninstall. The hook uses the same operator manager image specified during `helm install`. If that image does not exist or cannot be pulled (e.g., due to a typo in the image repository or tag), the hook Job will be stuck in `ImagePullBackOff` and the uninstall will never complete.

To work around this, run uninstall with `--no-hooks` to skip the pre-delete hook:

```bash
helm uninstall -n kube-amd-gpu amd-gpu-operator --no-hooks
```

```{note}
The pre-delete hook is responsible for cleaning up `DeviceConfig` custom resources. When using `--no-hooks`, you may need to manually delete any remaining `DeviceConfig` resources. See the next section for details.
```

## DeviceConfig Stuck in Terminating State

When the operator is not running (e.g., after `helm uninstall --no-hooks`, or if the operator pod was deleted), deleting a `DeviceConfig` CR will hang indefinitely:

```bash
# This will hang because the finalizer cannot be removed
kubectl delete deviceconfigs.amd.com --all -A
```

The operator adds a finalizer (`amd.node.kubernetes.io/deviceconfig-finalizer`) to every `DeviceConfig` resource. During normal deletion the operator's controller removes this finalizer after cleaning up owned resources (DaemonSets, KMM Modules, node labels, etc.). If the operator is not running, nothing removes the finalizer and the resource is stuck in `Terminating`.

To resolve this, patch the resource to remove the finalizer manually:

```bash
kubectl patch deviceconfig <name> -n kube-amd-gpu --type=json \
  -p '[{"op": "remove", "path": "/metadata/finalizers"}]'
```

Or, to remove the finalizer from all `DeviceConfig` resources at once:

```bash
kubectl get deviceconfigs.amd.com -A -o name | \
  xargs -I{} kubectl patch {} -n kube-amd-gpu --type=json \
  -p '[{"op": "remove", "path": "/metadata/finalizers"}]'
```

```{warning}
Removing the finalizer skips the operator's cleanup logic. Resources that were managed by the operator (DaemonSets, Services, KMM Modules, node labels) will not be automatically deleted. After removing the finalizer, verify that no orphaned resources remain:

    kubectl get daemonsets -n kube-amd-gpu
    kubectl get modules -n kube-amd-gpu
```

## Using Techsupport-dump Tool

The [techsupport-dump script](https://github.com/ROCm/gpu-operator/blob/main/tools/techsupport_dump.sh) can be used to collect system state and logs for debugging:

```bash
./tools/techsupport_dump.sh [-w] [-o yaml/json] [-k kubeconfig] <node-name/all>
```

Options:

* `-w`: wide option
* `-o yaml/json`: output format (default: json)
* `-k kubeconfig`: path to kubeconfig (default: ~/.kube/config)

Please file an issue with collected techsupport bundle on our [GitHub Issues](https://github.com/ROCm/gpu-operator/issues) page
