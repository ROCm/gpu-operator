# Upgrading GPU Operator

This guide outlines the steps to upgrade the AMD GPU Operator on a Kubernetes cluster using Helm. **This is only applicable for Kubernetes Helm chart deployments and not for OLM-based OpenShift deployments.**

## Upgrade Steps

### 1. Verify Cluster Readiness

Ensure the cluster is healthy and ready for the upgrade. A typical system will look like this before an upgrade:

```bash
NAMESPACE      NAME                                                              READY   STATUS    AGE
cert-manager   cert-manager-5d45994f57-95pkz                                     1/1     Running   8d
cert-manager   cert-manager-cainjector-5d69455fd6-kczfq                          1/1     Running   8d
cert-manager   cert-manager-webhook-56f4567ccb-gkpk8                             1/1     Running   8d
kube-amd-gpu   amd-gpu-operator-controller-manager-848455579d-p6hlm              1/1     Running   20m
kube-amd-gpu   amd-gpu-operator-kmm-controller-5cb9f6c9c7-5mn5z                  1/1     Running   20m
kube-amd-gpu   amd-gpu-operator-kmm-webhook-server-6c4c4d4dd9-6bl62              1/1     Running   20m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-gc-64c9b7dcd9-fd426       1/1     Running   20m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-master-7d69c9b6f9-hx55c   1/1     Running   20m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-d4845              1/1     Running   20m
kube-flannel   kube-flannel-ds-gtmt9                                             1/1     Running   11d
kube-system    coredns-7c65d6cfc9-w4ktn                                          1/1     Running   11d
kube-system    etcd-localhost.localdomain                                        1/1     Running   11d
kube-system    kube-apiserver-localhost.localdomain                              1/1     Running   11d
kube-system    kube-controller-manager-localhost.localdomain                     1/1     Running   11d
kube-system    kube-scheduler-localhost.localdomain                              1/1     Running   11d
```

All pods should be in the `Running` state. Resolve any issues such as restarts or errors before proceeding.

### 2. Understand Upgrade Safeguards

#### Pre-Upgrade Hook

The AMD GPU Operator includes a **pre-upgrade** hook that prevents upgrades if any **driver upgrades** are active. This ensures stability by blocking the upgrade when the operator is actively managing driver installations.

- **Manual Driver Upgrades in KMM:** Manual driver upgrades initiated by users through KMM are allowed but not recommended during an operator upgrade.
- **Skipping the Hook:** If necessary, you can bypass the pre-upgrade hook (not recommended) by adding ```--no-hooks```

#### Error Scenario

If the pre-upgrade hook detects active driver upgrades, the Helm upgrade process will fail with:

```bash
Error: UPGRADE FAILED: pre-upgrade hooks failed: 1 error occurred:
    * job pre-upgrade-check failed: BackoffLimitExceeded
```

To resolve:

1. Inspect the failed `pre-upgrade-check` Job:

   ```bash
   kubectl logs job/pre-upgrade-check -n kube-amd-gpu
   ```

2. Resolve any active driver upgrades and retry the upgrade.

### 3. Perform the Upgrade

Upgrade the operator using the following command:

```bash
helm upgrade amd-gpu-operator helm-charts-k8s/gpu-operator-helm-k8s-v1.0.0.tgz \
  --namespace kube-amd-gpu --set fullnameOverride=amd-gpu-operator-gpu-operator-charts \
  --set nameOverride=gpu-operator-charts
```

- The ```fullnameOverride``` and ```nameOverride``` parameters are used to ensure consistent naming between the previous and new chart deployments, avoiding conflicts caused by name mismatches during the upgrade process. The ```fullnameOverride``` explicitly sets the fully qualified name of the resources created by the chart, such as service accounts and deployments. The ```nameOverride``` adjusts the base name of the chart without affecting resource-specific names.
- By default, the default ```values.yaml``` from the new helm charts will be applied
- (Optional) You can prepare a new ```values.yaml``` with customized values and apply it along with ```helm upgrade``` command. The node feature discovery and kmm controller images can be changed before running the helm-upgrade. This will upgrade the nfd and kmm operators respectively when helm upgrade is run. For example:

```bash
helm upgrade amd-gpu-operator helm-charts-k8s/gpu-operator-helm-k8s-v1.0.0.tgz \
  --namespace kube-amd-gpu \
  -f new_values.yaml
```

If you encounter the pre-upgrade hook failure and wish to bypass it, please use ```--no-hooks``` option:

```bash
helm upgrade amd-gpu-operator helm-charts-k8s/gpu-operator-helm-k8s-v1.0.0.tgz \
  --namespace kube-amd-gpu \
  --no-hooks
```

### 4. Verify Post-Upgrade State

After the upgrade, ensure all components are running:

```bash
kubectl get pods -n kube-amd-gpu
```

Verify that nodes are labeled and GPUs are detected:

```bash
kubectl get nodes -oyaml | grep "amd.com/gpu"
kubectl get deviceconfigs -n kube-amd-gpu -oyaml
```

#### **Notes**

- Avoid upgrading during active driver upgrades initiated by the operator.
- Use `--no-hooks` only if necessary and after assessing the potential impact.
- For additional troubleshooting, check operator logs:

  ```bash
  kubectl logs -n kube-amd-gpu amd-gpu-operator-controller-manager-848455579d-p6hlm
  ```
