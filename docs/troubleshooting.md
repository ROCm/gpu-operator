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

## Potential Issues with default ``DeviceConfig``

* Please refer to {ref}`typical-deployment-scenarios` for more information and get corresponding ```helm install``` commands and configs that fits your specific use case.

* If operand pods (e.g. device plugin, metrics exporter) are stuck in ``Init:0/1`` state, it means your GPU worker doesn't have GPU driver loaded or driver was not loaded properly.

  * If you try to use inbox or pre-installed driver please check the node ``dmesg`` to see why the driver was not loaded properly.

  * If you want to deploy out-of-tree driver, we suggest check the [Driver Installation Guide](./drivers/installation) then modify the default ``DeviceConfig`` to ask Operator to install the out-of-tree GPU driver for your worker nodes.

```bash
kubectl edit deviceconfigs -n kube-amd-gpu default
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
