# DRA Driver, NPD, and ANR Diagnostics

Diagnostic procedures for DRA driver, Node Problem Detector (NPD), and Auto Node Remediation (ANR). These components may not be present in all clusters.

---

## DRA Driver (Dynamic Resource Allocation)

Alternative to Device Plugin for GPU resource allocation (Kubernetes 1.32+). Cannot run simultaneously with Device Plugin on the same DeviceConfig.

### Check if DRA is enabled

```bash
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o yaml | grep -A 5 "draDriver:"
```

### Verify DRA driver is running

```bash
kubectl --kubeconfig=$KUBECONFIG get daemonsets -n $NS | grep dra-driver
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -l app=dra-driver
```

### Verify DRA resources

```bash
# DeviceClass
kubectl --kubeconfig=$KUBECONFIG get deviceclass gpu.amd.com

# ResourceSlices (one per GPU per node)
kubectl --kubeconfig=$KUBECONFIG get resourceslices
```

### Common issues

| Symptom | Root Cause | Fix |
| --- | --- | --- |
| DRA pods not starting | K8s < 1.32 or feature gate disabled | Upgrade to K8s 1.32+, enable `DynamicResourceAllocation` |
| No ResourceSlices | amdgpu driver not loaded | Check `lsmod \| grep amdgpu` on node |
| Validation error | Both DRA and Device Plugin enabled | Disable one via DeviceConfig |
| ServiceAccount missing | RBAC not created | Check ServiceAccount and ClusterRole |

### DRA Driver Logs

```bash
DRA_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -l app=dra-driver -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$DRA_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $DRA_POD --tail=50
fi
```

---

## Node Problem Detector (NPD)

Detects GPU issues via metrics and reports as node conditions. Typically installed in `kube-system` namespace.

### Check if installed

```bash
kubectl --kubeconfig=$KUBECONFIG get daemonsets -A | grep node-problem-detector
kubectl --kubeconfig=$KUBECONFIG get pods -n kube-system -l app=node-problem-detector
```

### Verify AMD GPU monitoring

```bash
# Check for amdgpuhealth plugin config
kubectl --kubeconfig=$KUBECONFIG get configmap -n kube-system node-problem-detector-config -o yaml | grep amdgpuhealth

# Check binary mount
kubectl --kubeconfig=$KUBECONFIG get daemonset -n kube-system node-problem-detector -o yaml | grep amd-metrics-exporter
```

### Check node conditions

```bash
kubectl --kubeconfig=$KUBECONFIG get node $NODE -o json | jq '.status.conditions[] | select(.type | startswith("AMDGPU"))'
```

### NPD Common issues

| Symptom | Root Cause | Fix |
| --- | --- | --- |
| NPD not found | Not installed | Install: `helm install npd deliveryhero/node-problem-detector` |
| No GPU conditions | Plugin not configured | Add custom plugin monitor config with amdgpuhealth rules |
| Binary not found | Metrics exporter not mounting | Check `/var/lib/amd-metrics-exporter` on host |
| Can't access metrics | RBAC missing | Add nonResourceURLs to ClusterRole |
| Can't tolerate taint | Missing toleration | Add `amd-gpu-unhealthy:NoSchedule` toleration |

### NPD Logs

```bash
NPD_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n kube-system -l app=node-problem-detector --field-selector spec.nodeName=$NODE -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$NPD_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n kube-system $NPD_POD --tail=100 | grep -i amdgpu
fi
```

---

## Auto Node Remediation (ANR)

Triggers Argo Workflows to remediate GPU issues detected by NPD. Requires NPD and Argo Workflows.

### Check if enabled

```bash
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o yaml | grep -A 10 "remediationWorkflow:"
```

### Verify Argo Workflows

```bash
# Controller
kubectl --kubeconfig=$KUBECONFIG get deployment -A | grep workflow-controller

# CRDs
kubectl --kubeconfig=$KUBECONFIG get crd | grep workflows.argoproj.io
```

### Check remediation ConfigMap

```bash
kubectl --kubeconfig=$KUBECONFIG get configmap -n $NS default-conditional-workflow-mappings -o yaml
```

### Check active workflows

```bash
# All workflows
kubectl --kubeconfig=$KUBECONFIG get workflows -n $NS

# For specific node
kubectl --kubeconfig=$KUBECONFIG get workflows -n $NS -l node=$NODE

# Status
kubectl --kubeconfig=$KUBECONFIG get workflows -n $NS -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.phase}{"\n"}{end}'
```

### Check workflow status

```bash
# Describe workflow
kubectl --kubeconfig=$KUBECONFIG describe workflow -n $NS <workflow-name>

# Workflow logs
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <workflow-pod-name>

# Node taint
kubectl --kubeconfig=$KUBECONFIG get node $NODE -o jsonpath='{.spec.taints[?(@.key=="amd-gpu-unhealthy")]}'
```

### Argo Workflow Common issues

| Symptom | Root Cause | Fix |
| --- | --- | --- |
| Argo not found | Not installed | Install: `helm install argo-workflow argo/argo-workflows` |
| No workflows triggering | Remediation disabled | Set `remediationWorkflow.enable: true` |
| Workflow Pending | MaxParallelWorkflows limit | Wait or increase limit |
| Workflow suspended | Physical action needed | Resume: `kubectl label node $NODE operator.amd.com/gpu-force-resume-workflow=true` |
| Workflow pods fail | Image pull error | Check `testerImage` in DeviceConfig |
| Node stays tainted | Workflow failed | Check logs, manually remove taint |

### Resume suspended workflow

```bash
# Resume
kubectl --kubeconfig=$KUBECONFIG label node $NODE operator.amd.com/gpu-force-resume-workflow=true

# Abort
kubectl --kubeconfig=$KUBECONFIG label node $NODE operator.amd.com/gpu-abort-workflow=true
```

### Remove taint manually

```bash
kubectl --kubeconfig=$KUBECONFIG taint node $NODE amd-gpu-unhealthy:NoSchedule-
```

---

## Quick Detection

```bash
echo "=== DRA Driver ==="
kubectl --kubeconfig=$KUBECONFIG get daemonsets -n $NS | grep dra-driver || echo "Not enabled"

echo "=== NPD ==="
kubectl --kubeconfig=$KUBECONFIG get daemonsets -A | grep node-problem-detector || echo "Not installed"

echo "=== ANR ==="
kubectl --kubeconfig=$KUBECONFIG get deployment -A | grep workflow-controller || echo "Not installed"
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o jsonpath='{.items[*].spec.remediationWorkflow.enable}' | grep -q true && echo "Enabled" || echo "Disabled"
```
