# Remediation Commands

Fix commands organized by phase. Use these after diagnosing the root cause.

---

## Phase 2 — Controller Taint Remediation

```bash
# Add toleration to controller deployment
kubectl --kubeconfig=$KUBECONFIG patch deployment amd-gpu-operator-controller-manager \
  -n $NS --type=json -p='[{"op":"add",
  "path":"/spec/template/spec/tolerations/-",
  "value":{"key":"<taint-key>","operator":"Exists","effect":"NoSchedule"}}]'

# Or remove the node taint
kubectl taint nodes <node> <taint-key>=<value>:<effect>-
```

---

## Phase 4 — Selector/NFD Remediation

### Pattern A: Fix selector value mismatch

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"selector":{"nodeSelector":
  {"feature.node.kubernetes.io/amd-gpu":"true"}}}}'
```

### Pattern B: Fix vGPU selector

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"selector":{"nodeSelector":
  {"feature.node.kubernetes.io/amd-vgpu":"true"}}}}'
```

### Pattern C: Add new GPU device ID to NodeFeatureRule

Edit the NodeFeatureRule to add new device ID to matchExpressions list.

### Pattern D: Delete duplicate DeviceConfig

```bash
kubectl --kubeconfig=$KUBECONFIG delete deviceconfig <duplicate-name> -n $NS
```

### Pattern E: Remove per-component selector

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=json \
  -p='[{"op":"remove","path":"/spec/metricsExporter/selector"}]'
```

---

## Phase 5 — Pending Pods Remediation

### Add tolerations to components

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge -p='
spec:
  devicePlugin:
    tolerations:
    - key: "<taint-key>"
      operator: "Exists"
      effect: "NoSchedule"
  metricsExporter:
    tolerations:
    - key: "<taint-key>"
      operator: "Exists"
      effect: "NoSchedule"
  driver:
    tolerations:
    - key: "<taint-key>"
      operator: "Exists"
      effect: "NoSchedule"'
```

### Fix ConfigMap reference

```bash
# Option 1: Create missing ConfigMap
kubectl --kubeconfig=$KUBECONFIG create configmap <expected-name> \
  -n $NS --from-file=config.yaml=./my-config.yaml

# Option 2: Fix DeviceConfig to reference existing ConfigMap
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"metricsExporter":{"config":{"name":"<actual-configmap-name>"}}}}'
```

---

## Phase 7 — KMM Driver Remediation

### Pattern A: Use compatible driver version

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"driver":{"version":"<compatible-version>"}}}'
```

### Pattern B: Fix DNS (use registry IP or add CoreDNS stub zone)

```bash
# Use registry IP directly
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"driver":{"image":"<registry-ip>:<port>/path/to/image"}}}'
```

### Pattern C: Enable insecure registry

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"driver":{"imageRegistryTLS":
  {"insecure":true,"insecureSkipTLSVerify":true}}}}'
```

### Pattern D: Fix secret name typo

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=merge \
  -p='{"spec":{"driver":{"imageRegistrySecret":
  {"name":"<correct-secret-name>"}}}}'
```

### Pattern E: Create missing registry secret

```bash
kubectl --kubeconfig=$KUBECONFIG create secret docker-registry <secret-name> \
  -n $NS \
  --docker-server=<registry-host> \
  --docker-username=<user> \
  --docker-password=<password>
```

---

## Phase 10 — Operational Remediation

### Helm uninstall with --no-hooks

```bash
helm uninstall -n $NS amd-gpu-operator --no-hooks
```

### Remove DeviceConfig finalizers (single)

```bash
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```

### Remove DeviceConfig finalizers (all, correct namespace handling)

```bash
kubectl --kubeconfig=$KUBECONFIG get deviceconfigs.amd.com -A \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' | \
  while IFS=$'\t' read -r dc_ns dc_name; do
    kubectl --kubeconfig=$KUBECONFIG patch deviceconfig "$dc_name" -n "$dc_ns" --type=json \
      -p '[{"op":"remove","path":"/metadata/finalizers"}]'
  done
```

### Complete cleanup after bad install

```bash
# Step 1: Uninstall with --no-hooks
helm uninstall -n $NS amd-gpu-operator --no-hooks

# Step 2: Remove finalizers from all DeviceConfigs
kubectl --kubeconfig=$KUBECONFIG get deviceconfigs.amd.com -A \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' | \
  while IFS=$'\t' read -r dc_ns dc_name; do
    kubectl --kubeconfig=$KUBECONFIG patch deviceconfig "$dc_name" -n "$dc_ns" --type=json \
      -p '[{"op":"remove","path":"/metadata/finalizers"}]'
  done

# Step 3: Delete all DeviceConfigs
kubectl --kubeconfig=$KUBECONFIG delete deviceconfigs.amd.com --all -A

# Step 4: Delete GPU Operator CRDs
kubectl --kubeconfig=$KUBECONFIG delete crd deviceconfigs.amd.com
kubectl --kubeconfig=$KUBECONFIG delete crd $(kubectl get crd | grep amd.com | awk '{print $1}')

# Step 5: Verify cleanup
kubectl --kubeconfig=$KUBECONFIG get all -n $NS
kubectl --kubeconfig=$KUBECONFIG get crd | grep amd.com

# Step 6: Reinstall with correct image
helm install amd-gpu-operator rocm/gpu-operator-charts -n $NS --create-namespace \
  --set controller.manager.image.repository=<correct-image> \
  --set controller.manager.image.tag=<correct-tag>
```

---

## Phase 11 — Helm Install Remediation

### Check kubectl access

```bash
kubectl --kubeconfig=$KUBECONFIG cluster-info
kubectl --kubeconfig=$KUBECONFIG auth can-i '*' '*' --all-namespaces
```

### Install cert-manager

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

### Fix stale webhook

```bash
kubectl --kubeconfig=$KUBECONFIG delete validatingwebhookconfigurations \
  amd-gpu-operator-validating-webhook-configuration
helm install amd-gpu-operator rocm/gpu-operator-charts -n $NS --create-namespace
```

---

## Phase 12 — Helm Upgrade Remediation

### Delete failed hooks

```bash
kubectl --kubeconfig=$KUBECONFIG delete jobs -n $NS -l helm.sh/hook=pre-upgrade
```

### Retry upgrade

```bash
helm upgrade amd-gpu-operator rocm/gpu-operator-charts -n $NS --force
```

### Rollback

```bash
helm rollback amd-gpu-operator -n $NS
```
