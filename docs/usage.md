# Quick Start Guide

Getting up and running with the AMD GPU Operator and Device Metrics Exporter on Kubernets is quick and easy. Below is a short guide on how to get started using the helm installation method on a standard Kubernetes install. Note that more detailed instructions on the different installation methods can be found on this site:
</br>[GPU Operator Kubernets Helm Install](../docs/installation/kubernetes-helm.md)
</br>[GPU Operator Red Hat OpenShift Install](../docs/installation/openshift-olm.md)

## Installing the GPU Operator

1. The GPU Operator uses [cert-manager](https://cert-manager.io/) to manage certificates for MTLS communication between services. If you haven't already installed `cert-manager` as a prerequisite on your Kubernetes cluster, you'll need to install it as follows:

    ```bash
    # Add and update the cert-manager repository
    helm repo add jetstack https://charts.jetstack.io --force-update

    # Install cert-manager
    helm install cert-manager jetstack/cert-manager \
      --namespace cert-manager \
      --create-namespace \
      --version v1.15.1 \
      --set crds.enabled=true
    ```

    </br>

2. Once `cert-manager` is installed, you're just a few commands away from installing the GPU Operating and having a fully managed GPU infrastructure:

    ```bash
    # Add the Helm repository
    helm repo add rocm https://rocm.github.io/gpu-operator
    helm repo update

    # Install the GPU Operator
    helm install amd-gpu-operator rocm/gpu-operator-charts \
      --namespace kube-amd-gpu --create-namespace --version=v1.3.0
    ```

    ```{note}
    You can use `--set crds.defaultCR.install=false` to skip the installation of default `DeviceConfig` and create your own customized `DeviceConfig` for managing the cluster.
    ```

3. You should now see the GPU Operator component pods starting up in the namespace you specified above, `kube-amd-gpu`. There will be a default `DeviceConfig` deployed as well with the installation. Here is an example of one control plane node and one GPU worker node:

  ```bash
  $ kubectl get deviceconfigs -n kube-amd-gpu
  NAME      AGE
  default   10m

  $ kubectl get pods -n kube-amd-gpu
  NAME                                                              READY   STATUS     AGE
  amd-gpu-operator-gpu-operator-charts-controller-manager-74nm5wt   1/1     Running    10m
  amd-gpu-operator-kmm-controller-5c895cd594-h65nm                  1/1     Running    10m
  amd-gpu-operator-kmm-webhook-server-76d6765d5b-g5g74              1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-gc-64c9b7dcd9-gz4g4       1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-master-7d69c9b6f9-hcrxm   1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-worker-jlzbs              1/1     Running    10m
  default-device-plugin-9r9bh                                       1/1     Running    10m
  default-metrics-exporter-6c7z5                                    1/1     Running    10m
  default-node-labeller-xtwbm                                       1/1     Running    10m
  ```

  * Controller components: `gpu-operator-charts-controller-manager`, `kmm-controller` and `kmm-webhook-server`
    
    ```{note}
    In case you found they are in a pending state, check the description of the pod for specific reason.

      `kubectl describe pod -n kube-amd-gpu <pod name>`
    ```
    ```{tip}
      We suggest you label some nodes in your cluster as the control-plane nodes for those controller and webhook pods to run on:

      `kubectl label nodes <node-name> node-role.kubernetes.io/control-plane=`
    ```

  * Operands: `default-device-plugin`, `default-node-labeller` and `default-metrics-exporter`

    ```{note}
    Potential Failures with default `DeviceConfig`: 
    1. Operand pods are stuck in ```Init:0/1``` state: It means your GPU worker doesn't have inbox GPU driver loaded. We suggest check the [Driver Installation Guide](./drivers/installation.md) then modify the default `DeviceConfig` to ask Operator to install the out-of-tree GPU driver for your worker nodes.
    `kubectl edit deviceconfigs -n kube-amd-gpu default`
    2. No operand pods showed up: It is possible that default `DeviceConfig` selector `feature.node.kubernetes.io/amd-gpu: "true"` cannot find any matched node.
      * Check node label `kubectl get node -oyaml | grep -e "amd-gpu:" -e "amd-vgpu:"`
      * If you are using GPU in the VM, you may need to change the default `DeviceConfig` selector to `feature.node.kubernetes.io/amd-vgpu: "true"`
      * You can always customize the node selector of the `DeviceConfig`.
    ```
4. For a full list of configurable options refer to the [Full Reference Config](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/fulldeviceconfig.html) documenattion. An [example DeviceConfig](https://github.com/ROCm/gpu-operator/blob/release-v1.1.0/example/deviceconfig_example.yaml) is supplied in the ROCm/gpu-operator repository which can be used to get going:

    ```bash
    # Apply the example DeviceConfig to enable the Device Plugin, Node Labeller and Metrics Exporter plugins
    kubectl apply -f https://raw.githubusercontent.com/ROCm/gpu-operator/refs/heads/main/example/deviceconfig_example.yaml
    ```

    ````{note}
      If you are using a previous version of the GPU Operator you need to apply the deviceconfig_example.yaml file from that specific branch (v1.1.0 in this example) by doing the following:

      ```bash
      kubectl apply -f https://raw.githubusercontent.com/ROCm/gpu-operator/refs/heads/release-v1.1.0/example/deviceconfig_example.yaml
      ```
    ````

</br>
That's it! The GPU Operator components should now all be running. You can verify this by checking the namespace where the gpu-operator components are installed (default: `kube-amd-gpu`):

```bash
kubectl get pods -n kube-amd-gpu
```

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
