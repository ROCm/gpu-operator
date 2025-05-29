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

* ```pre-upgrade-check```: The AMD GPU Operator includes a **pre-upgrade** hook that prevents upgrades if any **driver upgrades** are active. This ensures stability by blocking the upgrade when the operator is actively managing driver installations.
* ```upgrade-crd```: This hook helps users to patch the new version Custom Resource Definition (CRD) to the helm deployment. Helm by default doesn't support automatic upgrade of CRD so we implemented this hook for auto-upgrade the CRDs.

- **Manual Driver Upgrades in KMM:** Manual driver upgrades initiated by users through KMM are allowed but not recommended during an operator upgrade.
- **Skipping the Hook:** If necessary, you can bypass the pre-upgrade hook (not recommended) by adding ```--no-hooks```, you would have to manually use new version's CRD to upgrade then in cluster.

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
# Fetch latest info from helm repo
helm repo update
# Perform helm upgrade
helm upgrade amd-gpu-operator rocm/gpu-operator-charts \
  -n kube-amd-gpu \
  --version=v1.3.0 \
  --debug
```

* When upgrading a Helm chart, customized operator controller image URLs set in the older version's values.yaml (via `--set` or `-f values.yaml`) will persist due to default Helm behavior.
* To ensure a successful upgrade, you must use the target version's operator image in the helm upgrade command. This is because upgrade hooks rely on the target version's images for CRD updates. For example, to upgrade to v1.3.0 when you already customized operator image URL in old version helm chart, use `--set` to ask helm for using correct version image for executing helm upgrade hooks:

```bash
# Fetch latest info from helm repo
helm repo update
# Perform helm upgrade
helm upgrade amd-gpu-operator rocm/gpu-operator-charts \
  -n kube-amd-gpu \
  --version=v1.3.0 \
  --debug \
  --set controllerManager.manager.image.repository=docker.io/rocm/gpu-operator \
  --set controllerManager.manager.image.tag=v1.3.0 
```

```{note}
Upgrade Options:
* **Error Scenario**: In case there is chart name or release name mismatch happened, you can use `--set fullnameOverride=amd-gpu-operator-gpu-operator-charts --set nameOverride=gpu-operator-charts` to resolve the conflict. The ```fullnameOverride``` and ```nameOverride``` parameters are used to ensure consistent naming between the previous and new chart deployments, avoiding conflicts caused by name mismatches during the upgrade process. The ```fullnameOverride``` explicitly sets the fully qualified name of the resources created by the chart, such as service accounts and deployments. The ```nameOverride``` adjusts the base name of the chart without affecting resource-specific names.
* By default, the default ```values.yaml``` from the new helm charts will be applied. If you have customized values.yaml applied to your older version helm chart, you need to apply it along with ```helm upgrade``` command by using ```-f values.yaml``` option. The node feature discovery and kmm controller images can be changed before running the helm-upgrade. This will upgrade the nfd and kmm operators respectively when helm upgrade is run. 
* If you encounter the pre-upgrade hook failure and wish to bypass it, please use `--no-hooks` option, then you need to manually patch to upgrade the CRDs in the cluster.
```

```{warning}
Default DeviceConfig Upgrade:
* If you are using default `DeviceConfig` from `helm install`, by default the default `DeviceConfig` resource and any customized change happened on it will persist through `helm upgrade`.
* If you want to auto upgrade the default `DeviceConfig` to new version's recommended value, you can use option ```--set crds.defaultCR.upgrade=true``` with `helm upgrade`. This option will create or patch existing default `DeviceConfig`. However, please carefully evaluate the risk before using this functionality. By using ```--set crds.defaultCR.upgrade=true``` you may:

  * Lose the customized change ever made on default `DeviceConfig`. 
  * Conflict with other existing `DeviceConfig`.
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
