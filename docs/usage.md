# Usage Guide

This guide provides information on how to use the AMD GPU Operator in your Kubernetes environment.

## Creating a GPU-enabled Pod

To create a pod that uses a GPU, specify the GPU resource in your pod specification:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
    - name: gpu-container
      image: rocm/pytorch:latest
      resources:
        limits:
          amd.com/gpu: 1 # requesting 1 GPU
```

Save this YAML to a file (e.g., `gpu-pod.yaml`) and create the pod:

```bash
kubectl apply -f gpu-pod.yaml
```

## Checking GPU Status

To check the status of GPUs in your cluster:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,GPUs:.status.capacity.'amd\.com/gpu'
```

## Using amd-smi

To run `amd-smi` in a pod:

- Create a YAML file named `amd-smi.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
 name: amd-smi
spec:
 containers:
 - image: docker.io/rocm/pytorch:latest
   name: amd-smi
   command: ["/bin/bash"]
   args: ["-c","amd-smi version && amd-smi monitor -ptum"]
   resources:
    limits:
      amd.com/gpu: 1
    requests:
      amd.com/gpu: 1
 restartPolicy: Never
```

- Create the pod:

```bash
kubectl create -f amd-smi.yaml
```

- Check the logs and verify the output `amd-smi` reflects the expected ROCm version and GPU presence:

```bash
kubectl logs amd-smi
AMDSMI Tool: 24.6.2+2b02a07 | AMDSMI Library version: 24.6.2.0 | ROCm version: 6.2.2
GPU  POWER  GPU_TEMP  MEM_TEMP  GFX_UTIL  GFX_CLOCK  MEM_UTIL  MEM_CLOCK
  0  126 W     40 °C     32 °C       1 %    182 MHz       0 %    900 MHz
```

## Using rocminfo

To run `rocminfo` in a pod:

- Create a YAML file named `rocminfo.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rocminfo
spec:
  containers:
  - image: rocm/pytorch:latest
    name: rocminfo
    command: ["/bin/sh","-c"]
    args: ["rocminfo"]
    resources:
      limits:
        amd.com/gpu: 1
  restartPolicy: Never
```

- Create the pod:

```bash
kubectl create -f rocminfo.yaml
```

- Check the logs and verify the output:

```bash
kubectl logs rocminfo
```

## Configuring GPU Resources

Configuration parameters are documented in the [Custom Resource Installation Guide](./drivers/installation)
