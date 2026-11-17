# Node Labeller

The node labeller adds GPU information, such as the device ID, product name, and
memory size, to Kubernetes worker node labels. The AMD GPU Operator manages the
labeller through `spec.devicePlugin` in your DeviceConfig.

Use this guide to select worker nodes, understand when GPU labels are removed,
and recover missing or stale labels. For container image and argument options,
see the [Device Plugin configuration reference](device-plugin.md#device-plugin-deviceconfig).

```{note}
The cleanup changes described here are listed in the
[unreleased changes](../releasenotes.md#unreleased). Use an operator build that
includes the fix; earlier releases can remove valid labels from custom-selected
worker nodes that lack AMD discovery labels.
```

## Select Worker Nodes With a Custom Label

You can use your own label to select a GPU pool. This is useful when worker nodes
already have an AMD GPU driver and do not use Node Feature Discovery (NFD) to
select GPUs.

Before applying this example:

- Install the AMD GPU Operator and confirm that the `kube-amd-gpu` namespace exists.
- Confirm that the selected worker nodes have a working AMD GPU driver.
- Check existing DeviceConfig resources, including the default config, and keep their
  selected worker nodes separate. A worker node should belong to one DeviceConfig.

List the existing configurations and label your worker node. Replace
`gpu-worker-01` with its actual name:

```bash
kubectl get deviceconfigs.amd.com -A -o yaml
kubectl label node gpu-worker-01 example.com/gpu-pool=custom
```

Save the following as `custom-gpu-pool.yaml`:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: custom-gpu-pool
  namespace: kube-amd-gpu
spec:
  selector:
    example.com/gpu-pool: custom
  driver:
    enable: false
  devicePlugin:
    enableNodeLabeller: true
```

Apply the configuration and inspect the labels:

```bash
kubectl apply -f custom-gpu-pool.yaml
kubectl get daemonset -n kube-amd-gpu custom-gpu-pool-node-labeller
kubectl get node gpu-worker-01 --show-labels
```

Allow time for the labeller pod to start. A working labeller adds labels such as
`amd.com/gpu.device-id`. With the cleanup fix, the operator preserves these labels
even when the worker node has no AMD NFD discovery labels. Setting
`driver.enable: false` skips driver installation; it does not install or repair
the driver already on the worker node.

## Understand Automatic Cleanup

The operator uses these AMD NFD labels for GPU discovery:

- `feature.node.kubernetes.io/amd-gpu`.
- `feature.node.kubernetes.io/amd-vgpu`.

The labels actually present on the worker node determine cleanup behavior.
Installing NFD alone does not guarantee that it detects and labels your GPUs.

| Your DeviceConfig selector | Automatic cleanup behavior |
| --- | --- |
| Includes either AMD NFD label | The operator can remove stale labeller labels from worker nodes missing **both** discovery labels, subject to the checks below. |
| Uses only custom labels | The operator does not treat missing AMD NFD labels as a reason to remove labeller labels. |

For an NFD-based selector, a cleanup candidate must still match every non-NFD
constraint in that selector. When driver management is enabled, existing Kernel
Module Management (KMM) readiness labels also protect the worker node from this
cleanup.

The operator also preserves labels if another DeviceConfig selects the worker
node. This protection applies across namespaces and before the other config
becomes Ready. A config that is still present can protect a worker node even
while its labeller is disabled or the config is being deleted. Keep configuration
selectors disjoint; this protection does not allow two configs to manage the same
worker node.

Creating, deleting, or changing the selector of a DeviceConfig prompts other
enabled NFD-based configs to reconsider stale labels. Automatic cleanup still
requires an eligible config to reach the labeller step. For example, a config
that selects no worker nodes stops before that step. Do not rely on changing a
custom selector alone to remove labels from worker nodes you release.

## Disable the Labeller or Delete a DeviceConfig

To stop labelling while keeping the other components of your DeviceConfig,
disable the labeller:

```bash
kubectl patch deviceconfig custom-gpu-pool -n kube-amd-gpu --type=merge \
  -p '{"spec":{"devicePlugin":{"enableNodeLabeller":false}}}'
```

The operator removes the labeller DaemonSet and its recognized GPU labels from
currently selected worker nodes. This affects the **entire selected pool**.
Other user-defined labels under `amd.com` and `beta.amd.com` are preserved.

Deleting a DeviceConfig tears down the components it manages and performs label
cleanup. Use deletion when you intend to remove that configuration, rather than
only stop labelling.

If you installed the operator with `kmm.watch=false`, deletion also cleans
labeller labels from selected worker nodes, while preserving those selected by
another DeviceConfig. The operator retains KMM version labels and existing KMM
resources so an independent KMM installation can continue to manage them. A
label-cleanup error leaves the DeviceConfig pending deletion so the operator can
retry. See the [KMM installation options](../installation/kubernetes-helm.md)
for how `kmm.watch` differs from `kmm.enabled`.

## Recover Missing Labels

If labels disappeared from a healthy, custom-selected worker node:

1. Upgrade to an operator build containing the cleanup fix.
2. Verify that the intended DeviceConfig still selects the worker node and has
   `enableNodeLabeller: true`.
3. Find the labeller pod on that worker node and inspect its logs:

   ```bash
   kubectl get pods -n kube-amd-gpu -o wide
   kubectl logs -n kube-amd-gpu NODE_LABELLER_POD
   ```

   Replace `NODE_LABELLER_POD` with the affected labeller pod's name. If discovery
   succeeds but labels are still missing, delete that pod so the DaemonSet
   recreates it and the labeller can publish the labels again:

   ```bash
   kubectl delete pod -n kube-amd-gpu NODE_LABELLER_POD
   kubectl get node gpu-worker-01 --show-labels
   ```

If labels never appear, check the AMD GPU driver and labeller container image
compatibility. The Kubernetes 1.36 labeller event-processing issue discussed in
[ROCm/k8s-device-plugin#255](https://github.com/ROCm/k8s-device-plugin/pull/255)
concerns label generation. The operator cleanup fix does not update the labeller
container image or fix that separate problem.

## Remove Stale Labels From Released Worker Nodes

Before retiring a custom-selected pool, disable the labeller and verify label
removal **while the old selector still matches the worker nodes**. Then change
the selector or remove the pool label. This avoids leaving labels on worker nodes
that the config no longer selects.

If you have already released a worker node:

1. Inspect its labels and all DeviceConfig selectors:

   ```bash
   kubectl get node gpu-worker-01 --show-labels
   kubectl get deviceconfigs.amd.com -A -o yaml
   ```

2. Confirm that no remaining DeviceConfig selects it and that its GPU labels are
   obsolete. Check workloads that select those labels before removing them.
3. Remove each confirmed stale labeller label by its exact key. For example, the
   following removes only the device-ID label:

   ```bash
   kubectl label node gpu-worker-01 amd.com/gpu.device-id-
   ```

   Review any other stale labeller labels individually. Avoid deleting all labels
   under `amd.com` or `beta.amd.com`; those domains can also contain user labels.
4. Inspect the worker node again to verify the intended labels were removed.

## Before Rolling Back the Operator

Rolling back to a build without the fix can cause valid GPU labels to disappear
again when custom-selected worker nodes lack AMD NFD discovery labels. An
NFD-based config in another namespace can also trigger that deletion.

Before rollback, save your DeviceConfig resources and review which pools depend on custom
selection. If you plan to use NFD discovery, verify that NFD actually detects and
labels the GPUs and that your configs select separate pools. Restoring saved
configuration does not recreate labels that were removed; a working labeller
must publish them again. Earlier builds can also leave stale labeller labels
after DeviceConfig deletion with `kmm.watch=false`.
