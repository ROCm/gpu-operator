# Upgrading GPU Operator Components

This guide outlines the steps to upgrade the Device Plugin, Node labeller and Metrics Exporter Daemonsets managed by the AMD GPU Operator on a Kubernetes cluster.

These components need a upgrade policy to be mentioned to decide how the daemonset upgrade will be done.

- DevicePlugin and Nodelabeller have a common UpgradePolicy Spec in DevicePlugin Spec

- Metrics Exporter has its own UpgradePolicy Spec in Metrics Exporter Spec

- `UpgradePolicy` has 2 fields, `UpgradeStrategy` (string) and `MaxUnavailable` (int)

- `UpgradeStrategy` can be either `RollingUpdate` or `OnDelete`

- `RollingUpdate` uses `MaxUnavailable` field (1 pod will go down for upgrade at a time by default, can be set by user). If user sets MaxUnavailable to 2,
    2 pods will go down for upgrade at once and then the next 2 and so on. This is triggered by CR update shown in Upgrade Steps section

- `OnDelete`: Upgrade of image will happen for the pod only when user manually deletes the pod. When it comes back up, it comes back with the new image.
    In this case, CR update will not trigger any upgrade without user intervention of deleting each pod.

```{note}
**MaxUnavailable** field is meaningful only when **UpgradeStrategy** is set to `RollingUpdate`. If *UpgradeStrategy* is set to `OnDelete` and **MaxUnavailable** is set to an integer, behaviour of `OnDelete` is still as explained above
```

## Upgrade Steps

### 1. Verify Cluster Readiness

Ensure the cluster is healthy and CR is already applied and ready for the upgrade. A typical cluster of 3 worker nodes with CR applied will look like this before an upgrade:

```bash
kube-amd-gpu   amd-gpu-operator-controller-manager-5b94bdd6dd-wnx5x             1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-kmm-controller-6746f8cbc7-lpjxd                 1/1     Running   0              60m
kube-amd-gpu   amd-gpu-operator-kmm-webhook-server-6ff4c684bd-bgrs4             1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-gc-78989c896-m66jp       1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-master-b8bffc48b-r2p79   1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-2j2mq             1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-phb74             1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-qsb7d             1/1     Running   0              81m
kube-amd-gpu   amd-gpu-operator-node-feature-discovery-worker-zchc4             1/1     Running   0              81m
kube-amd-gpu   test-deviceconfig-device-plugin-fvdgv                            1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-device-plugin-hfdbg                            1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-device-plugin-l55g6                            1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-metrics-exporter-79wvs                         1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-metrics-exporter-7qcws                         1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-metrics-exporter-nrk7v                         1/1     Running   0              36s
kube-amd-gpu   test-deviceconfig-node-labeller-2r7dz                            1/1     Running   0              42s
kube-amd-gpu   test-deviceconfig-node-labeller-45kxp                            1/1     Running   0              42s
kube-amd-gpu   test-deviceconfig-node-labeller-6x5kg                            1/1     Running   0              42s
```

All pods should be in the `Running` state. Resolve any issues such as restarts or errors before proceeding.

### 2. Check Current Image of Device Plugin before Upgrade

The current image the Device Plugin Daemonset is using can be checked by using `kubectl describe <pod-name> -n kube-amd-gpu` on one of the device plugin pods.

```bash
device-plugin:
    Container ID:   containerd://b1aaa67ebdd87d4ef0f2a32b76b428068d24c28ced3e86c3c5caba39bb5689a4
    Image:          rocm/k8s-device-plugin:1.31.0.0
```

### 3. Upgrade the Image of Device Plugin Daemonset

In the Custom Resource, we have the `UpgradePolicy` field in the DevicePluginSpec of type `DaemonSetUpgradeSpec` to support daemonset upgrades. This leverages standard k8s daemonset upgrade support whose details can be found at: https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set/

To upgrade the device plugin image, we need to update the DevicePluginSpec.DevicePluginImage and set the DevicePluginSpec.UpgradePolicy in the CR.

Example:

Old CR:

```yaml
    devicePlugin:
        devicePluginImage: rocm/k8s-device-plugin:1.31.0.0
```

Updated CR:

```yaml
    devicePlugin:
        devicePluginImage: rocm/k8s-device-plugin:latest
        upgradePolicy:
          upgradeStrategy: RollingUpdate
          maxUnavailable: 1
```

Once the new CR is applied, each device plugin pod will go down 1 at a time and come back with the new image mentioned in the CR.

The new image the Device Plugin Daemonset is using can be checked by using `kubectl describe <pod-name> -n kube-amd-gpu` on one of the device plugin pods.

```yaml
device-plugin:
    Container ID:   containerd://8b35722a47100f61e9ea4fee4ecf61faa078b7ab36084b2dd0ed8ba00179a883
    Image:          rocm/k8s-device-plugin:latest
```

### 4. How to Upgrade Image of NodeLabeller and Metrics Exporter Daemonset

-> The upgrade for Nodelabeller works the exact same way as for DevicePlugin. The upgradePolicy mentioned in the DevicePluginSpec applies for both DevicePlugin Daemonset as well as Nodelabeller Daemonset. The only difference is that, in this case, the user will change devicePluginSpec.NodeLabellerImage to trigger the upgrade

-> The upgrade for MetricsExporter needs an UpgradePolicy mentioned in the MetricsExporterSpec. The upgradePolicy has the same 2 fields here as well and the behaviour is the same

Example:

Old CR:

```yaml
  metricsExporter:
    enable: True
    serviceType: "ClusterIP"
    port: 5000
    image: rocm/device-metrics-exporter:v1.1.0
```

Updated CR:

```yaml
  metricsExporter:
    enable: True
    serviceType: "ClusterIP"
    port: 5000
    image: rocm/device-metrics-exporter:v1.2.0
    upgradePolicy:
      upgradeStrategy: OnDelete
```

Once the new CR is applied, each metrics exporter pod has to be brought down manually by user intervention to trigger upgrade for that pod. This is because, in this case, `OnDelete` option is used as upgradeStrategy. The image can be verified the same way as device plugin pod.

#### **Notes**

- If no UpgradePolicy is mentioned for any of the components but their image is changed in the CR update, the daemonset will get upgraded according to the defaults, which is `UpgradeStrategy` set to `RollingUpdate` and `MaxUnavailable` set to 1.
