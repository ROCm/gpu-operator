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

## Debugging Driver Installation

If the AMD GPU driver build fails:

- Check the status of the build pod:

```bash
kubectl get pods -n kube-amd-gpu
```

- View the build pod logs:

```bash
kubectl logs -n kube-amd-gpu <build-pod-name>
```

- Check events for more information:

```bash
kubectl get events -n kube-amd-gpu
```

## Using Techsupport-dump Tool

The techsupport-dump tool can be used to collect system state and logs for debugging:

```bash
./tools/techsupport_dump.sh [-w] [-o yaml/json] [-k kubeconfig] <node-name/all>
```

Options:

- `-w`: wide option
- `-o yaml/json`: output format (default: json)
- `-k kubeconfig`: path to kubeconfig (default: ~/.kube/config)
