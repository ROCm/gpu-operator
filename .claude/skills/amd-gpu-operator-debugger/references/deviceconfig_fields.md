# DeviceConfig Field Reference

The `DeviceConfig` CR is the primary configuration surface for the AMD GPU Operator.
Every component deployed by the operator is controlled by fields in this resource.

## Minimal DeviceConfig (inbox/pre-installed driver)

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: default
  namespace: kube-amd-gpu
spec:
  driver:
    enable: false          # use pre-installed amdgpu driver
  selector:
    nodeSelector:
      feature.node.kubernetes.io/amd-gpu: "true"
```

## Full DeviceConfig (out-of-tree driver via KMM)

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: gpu-config
  namespace: kube-amd-gpu
spec:
  # ── Driver (managed by KMM) ────────────────────────────────────
  driver:
    enable: true                       # true = KMM manages driver
    version: "6.8.0"                  # ROCm version string
    image: "myregistry.com/amd/gpu-driver"  # repo WITHOUT tag
    imageRegistrySecret:
      name: gpu-registry-creds        # K8s docker-registry secret
    blacklist: true                   # add amdgpu to modprobe blacklist
    imageRegistryTLS:
      insecure: false                 # true = allow HTTP registry
      insecureSkipTLSVerify: false    # true = skip TLS cert verify
    tolerations:                      # KMM driver DaemonSet tolerations
    - key: "team"
      operator: "Equal"
      value: "ml-training"
      effect: "NoSchedule"

  # ── Node Selector (main selector for all operands) ─────────────
  selector:
    nodeSelector:
      feature.node.kubernetes.io/amd-gpu: "true"  # standard NFD label
      # For vGPU/SR-IOV nodes: feature.node.kubernetes.io/amd-vgpu: "true"

  # ── Device Plugin ──────────────────────────────────────────────
  devicePlugin:
    devicePluginImage: "rocm/k8s-device-plugin:latest"
    nodeLabellerImage: "rocm/k8s-device-plugin:labeller-latest"
    enableNodeLabeller: true
    tolerations: []          # DaemonSet tolerations (if GPU nodes are tainted)

  # ── Metrics Exporter ───────────────────────────────────────────
  metricsExporter:
    enable: true
    serviceType: ClusterIP   # or NodePort
    port: 5000
    nodePort: 32500          # only relevant for NodePort
    config:
      name: ""               # ConfigMap name in operator namespace (optional)
    selector:                # OPTIONAL per-component override; overrides main selector
      nodeSelector:
        gpu-monitoring: "enabled"  # must match actual node labels!
    tolerations: []

  # ── Config Manager ─────────────────────────────────────────────
  configManager:
    enable: false
    config:
      name: ""               # ConfigMap name — MUST exist in kube-amd-gpu ns

  # ── Test Runner ────────────────────────────────────────────────
  testRunner:
    enable: false
    config:
      name: ""               # ConfigMap name — MUST exist in kube-amd-gpu ns
```

## Common Misconfiguration → Symptom Map

| Field | Misconfiguration | Symptom |
| --- | --- | --- |
| `spec.selector.nodeSelector` | Wrong label value (e.g. `"enabled"` vs `"true"`) | All DaemonSets DESIRED=0 |
| `spec.selector.nodeSelector` | Using `amd-gpu` for SR-IOV VF nodes | No pods on vGPU nodes |
| `spec.metricsExporter.selector` | Custom label not present on GPU nodes | Exporter DESIRED=0, plugin OK |
| `spec.driver.enable` | `true` but no KMM operator installed | No Module CR created, no driver |
| `spec.driver.image` | Tag included in image path | KMM builder fails to resolve image |
| `spec.driver.imageRegistrySecret.name` | Typo or secret not created | KMM: "cannot find secret" |
| `spec.driver.imageRegistryTLS` | Missing when registry is HTTP | KMM: "http response to https client" |
| `spec.driver.tolerations` | Missing when GPU nodes are tainted | KMM-worker pods Pending |
| `spec.devicePlugin.tolerations` | Missing when GPU nodes are tainted | Device plugin pods Pending |
| `spec.metricsExporter.tolerations` | Missing when GPU nodes are tainted | Exporter pods Pending |
| `spec.configManager.config.name` | References non-existent ConfigMap | Config-manager CreateContainerConfigError |
| `spec.metricsExporter.config.name` | References non-existent ConfigMap | Exporter CreateContainerConfigError |
| `spec.testRunner.config.name` | References non-existent ConfigMap | TestRunner CreateContainerConfigError |

## DeviceConfig Status Fields

```bash
kubectl get deviceconfigs -n kube-amd-gpu default -o yaml
```

```yaml
status:
  conditions:
  - type: Ready
    status: "True"           # "False" means config error — check message:
    message: ""
    reason: OperatorReady
  devicePlugin:
    desiredNumber: 2         # nodes matching selector
    availableNumber: 2       # nodes where plugin is running
    nodesMatchingSelectorNumber: 2
  metricsExporter:
    desiredNumber: 2
    availableNumber: 2
    nodesMatchingSelectorNumber: 2
  observedGeneration: 1
```

`desiredNumber: 0` with selector configured → selector doesn't match any node.
`desiredNumber > availableNumber` → pods exist but not all are Ready.

## Multiple DeviceConfigs (Conflict Rules)

- Multiple DeviceConfig objects can co-exist only if their `spec.selector` targets
  **non-overlapping sets of nodes**.
- If two DeviceConfigs match the same node, the controller logs a conflict and skips
  creating operands for the second one.
- Check: `kubectl get deviceconfig -n kube-amd-gpu`
- Check controller logs for: `overlapping selector`, `conflict`, `Skipping operand`

## Finalizer

Every DeviceConfig gets finalizer `amd.node.kubernetes.io/deviceconfig-finalizer`.
If the operator is not running when you delete the CR, the finalizer blocks deletion.

```bash
# Force remove finalizer (manual cleanup required afterwards)
kubectl patch deviceconfig <name> -n kube-amd-gpu --type=json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```
