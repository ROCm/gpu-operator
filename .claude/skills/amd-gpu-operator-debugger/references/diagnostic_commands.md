# AMD GPU Operator — Diagnostic Commands

All commands use `$KUBECONFIG`, `$NS` (default: `kube-amd-gpu`), and `$NODE` variables.
Set them before running:

```bash
export KUBECONFIG=~/.kube/config
export NS=kube-amd-gpu
export NODE=gpu-worker-0
```

---

## Global Cluster State

```bash
# All pods in operator namespace
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -o wide

# Pod status with restart counts
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS \
  -o custom-columns='NAME:.metadata.name,STATUS:.status.phase,
  READY:.status.containerStatuses[0].ready,
  RESTARTS:.status.containerStatuses[0].restartCount,
  NODE:.spec.nodeName'

# KMM pods (may be in same or separate namespace)
kubectl --kubeconfig=$KUBECONFIG get pods -A | grep -E "kmm"

# NFD pods
kubectl --kubeconfig=$KUBECONFIG get pods -A | grep nfd

# All DaemonSets (check DESIRED vs READY)
kubectl --kubeconfig=$KUBECONFIG get daemonset -n $NS

# All events (sorted by time)
kubectl --kubeconfig=$KUBECONFIG get events -n $NS \
  --sort-by='.lastTimestamp' | tail -40
```

---

## DeviceConfig

```bash
# List all DeviceConfigs
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS

# Full YAML (includes status section)
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o yaml

# Status only (OperatorReady condition + counts)
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS \
  -o jsonpath='{.items[*].status}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "Failed to retrieve DeviceConfig status"

# Edit a DeviceConfig interactively
kubectl --kubeconfig=$KUBECONFIG edit deviceconfigs -n $NS default

# Selector used by each DeviceConfig
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o \
  jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.selector}{"\n"}{end}'

# Driver config
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o \
  jsonpath='{range .items[*]}{.metadata.name}{"\n"}{.spec.driver}{"\n"}{end}'
```

---

## Node Inspection

```bash
# All nodes with labels
kubectl --kubeconfig=$KUBECONFIG get nodes --show-labels

# GPU nodes only (NFD labeled)
kubectl --kubeconfig=$KUBECONFIG get nodes \
  -l feature.node.kubernetes.io/amd-gpu=true

# vGPU/SR-IOV nodes
kubectl --kubeconfig=$KUBECONFIG get nodes \
  -l feature.node.kubernetes.io/amd-vgpu=true

# Taints on all nodes
kubectl --kubeconfig=$KUBECONFIG get nodes -o json 2>/dev/null | \
  python3 -c "
import sys, json
data = sys.stdin.read()
if data.strip():
    for n in json.loads(data)['items']:
        taints = n['spec'].get('taints', [])
        print(n['metadata']['name'], taints if taints else '(no taints)')
else:
    print('No nodes found or kubectl failed')
" 2>/dev/null || echo "Failed to retrieve node taints"

# Allocatable GPU resources per node
kubectl --kubeconfig=$KUBECONFIG get nodes -o json 2>/dev/null | \
  python3 -c "
import sys, json
data = sys.stdin.read()
if data.strip():
    for n in json.loads(data)['items']:
        name = n['metadata']['name']
        gpus = n['status']['allocatable'].get('amd.com/gpu', '0')
        print(name, 'amd.com/gpu:', gpus)
else:
    print('No nodes found or kubectl failed')
" 2>/dev/null || echo "Failed to retrieve allocatable GPU resources"

# Detailed node info (conditions, events, labels)
kubectl --kubeconfig=$KUBECONFIG describe node $NODE

# Node driver label (present only after KMM driver loads)
kubectl --kubeconfig=$KUBECONFIG get node $NODE --show-labels | \
  tr ',' '\n' | grep -E "amdgpu|amd-gpu|pci"
```

---

## KMM (Kernel Module Management)

```bash
# Module CRs
kubectl --kubeconfig=$KUBECONFIG get module -n $NS -o yaml
kubectl --kubeconfig=$KUBECONFIG get module -A -o yaml 2>/dev/null

# KMM controller logs
KMM_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS | \
  grep kmm-operator-controller | awk '{print $1}' | head -1)
if [ -n "$KMM_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $KMM_POD --tail=80
else
  echo "No KMM controller pod found"
fi

# KMM webhook logs
WEBHOOK_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS | \
  grep kmm-webhook | awk '{print $1}' | head -1)
if [ -n "$WEBHOOK_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $WEBHOOK_POD --tail=30
else
  echo "No KMM webhook pod found"
fi

# Builder pod (check status and logs after build failure)
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS | grep build
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <builder-pod> --tail=100

# Driver DaemonSet (kmm-worker pods)
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -l app=kmm-worker -o wide
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <kmm-worker-pod>

# All KMM-related events
kubectl --kubeconfig=$KUBECONFIG get events -n $NS | grep -i "module\|build\|kmm"
```

---

## Operand Pod Debugging

```bash
# Pending pods
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS \
  --field-selector=status.phase=Pending

# Describe a stuck pod (events are key)
kubectl --kubeconfig=$KUBECONFIG describe pod -n $NS <pod-name>

# Init container logs (for Init 0/1 state)
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <pod-name> -c wait-for-driver
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <pod-name> --all-containers=true

# Previous container logs (if pod restarted)
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <pod-name> --previous

# Device plugin logs (on specific node)
PLUGIN_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -o wide | \
  grep device-plugin | grep $NODE | awk '{print $1}' | head -1)
if [ -n "$PLUGIN_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $PLUGIN_POD
else
  echo "No device-plugin pod found on node $NODE"
fi

# Metrics exporter logs
EXPORTER_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS -o wide | \
  grep metrics-exporter | grep $NODE | awk '{print $1}' | head -1)
if [ -n "$EXPORTER_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $EXPORTER_POD
else
  echo "No metrics-exporter pod found on node $NODE"
fi
```

---

## ConfigMap Checks

```bash
# All ConfigMaps in operator namespace
kubectl --kubeconfig=$KUBECONFIG get configmap -n $NS

# ConfigMap content
kubectl --kubeconfig=$KUBECONFIG get configmap <name> -n $NS -o yaml

# What ConfigMap does DeviceConfig reference?
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o yaml | \
  grep -A5 "config:"
```

---

## Registry / Secret Checks

```bash
# Secrets in operator namespace
kubectl --kubeconfig=$KUBECONFIG get secret -n $NS

# What imageRegistrySecret does DeviceConfig reference?
kubectl --kubeconfig=$KUBECONFIG get deviceconfig -n $NS -o \
  jsonpath='{.items[*].spec.driver.imageRegistrySecret}'

# Test registry DNS resolution from inside cluster
kubectl --kubeconfig=$KUBECONFIG run dns-test --rm -it \
  --image=busybox --restart=Never -- nslookup <registry-hostname>

# Test registry reachability (TLS)
kubectl --kubeconfig=$KUBECONFIG run conn-test --rm -it \
  --image=curlimages/curl:latest --restart=Never -- \
  curl -v https://<registry-hostname>/v2/
```

---

## NFD (Node Feature Discovery)

```bash
# NFD pods
kubectl --kubeconfig=$KUBECONFIG get pods -n nfd -o wide 2>/dev/null || \
  kubectl --kubeconfig=$KUBECONFIG get pods -A | grep nfd

# NodeFeatureRule (AMD GPU device ID allowlist)
kubectl --kubeconfig=$KUBECONFIG get nodefeaturerule -A -o yaml

# Check node PCI labels (set by NFD)
kubectl --kubeconfig=$KUBECONFIG get node $NODE --show-labels | \
  tr ',' '\n' | grep pci

# If pci-1002.present=true but amd-gpu=true is absent:
#   → device ID not in NodeFeatureRule matchExpressions
```

---

## Helm Operations

```bash
# List installed Helm releases in operator namespace
helm list -n $NS

# Normal uninstall
helm uninstall -n $NS amd-gpu-operator

# Uninstall when pre-delete hook is stuck (ImagePullBackOff)
helm uninstall -n $NS amd-gpu-operator --no-hooks

# Check pre-delete hook Job
kubectl --kubeconfig=$KUBECONFIG get jobs -n $NS
kubectl --kubeconfig=$KUBECONFIG describe job -n $NS <hook-job-name>
```

---

## DeviceConfig Finalizer Removal

Use only when operator is not running and DeviceConfig is stuck Terminating:

```bash
# Single DeviceConfig
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS \
  --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]'

# All DeviceConfigs across all namespaces
kubectl --kubeconfig=$KUBECONFIG get deviceconfigs.amd.com -A \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' | \
  while IFS=$'\t' read -r dc_ns dc_name; do
    kubectl --kubeconfig=$KUBECONFIG patch deviceconfig "$dc_name" -n "$dc_ns" --type=json \
      -p '[{"op":"remove","path":"/metadata/finalizers"}]'
  done

# After removal: verify no orphaned resources
kubectl --kubeconfig=$KUBECONFIG get daemonsets -n $NS
kubectl --kubeconfig=$KUBECONFIG get modules -n $NS
```

---

## Techsupport Dump

```bash
# Full support bundle for one node
./tools/techsupport_dump.sh -o yaml -k $KUBECONFIG $NODE

# Full support bundle for all nodes (wide mode)
./tools/techsupport_dump.sh -w -o yaml -k $KUBECONFIG all

# Script source: https://github.com/ROCm/gpu-operator/blob/main/tools/techsupport_dump.sh
```

---

## Debug Container (Kernel-Level)

```bash
# Create privileged pod on target node
cat <<'EOF' | kubectl --kubeconfig=$KUBECONFIG apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-debug-node
  namespace: kube-amd-gpu
spec:
  nodeName: <TARGET_NODE>
  hostPID: true
  hostNetwork: true
  restartPolicy: Never
  tolerations:
  - operator: Exists
  containers:
  - name: debug
    image: ubuntu:22.04
    command: ["sleep", "3600"]
    securityContext:
      privileged: true
    volumeMounts:
    - name: host-root
      mountPath: /host
    - name: dev
      mountPath: /dev
  volumes:
  - name: host-root
    hostPath:
      path: /
  - name: dev
    hostPath:
      path: /dev
EOF

kubectl --kubeconfig=$KUBECONFIG wait pod/gpu-debug-node -n kube-amd-gpu \
  --for=condition=Ready --timeout=60s

kubectl --kubeconfig=$KUBECONFIG exec -it gpu-debug-node -n kube-amd-gpu -- bash
```

Inside the debug pod:

```bash
# Module load status
chroot /host lsmod | grep amdgpu

# GPU device files
chroot /host ls -la /dev/kfd /dev/dri/ 2>/dev/null

# Full dmesg scan for GPU/driver errors
chroot /host dmesg | grep -E "amdgpu|GPU|FAULT|reset|error" | tail -50

# Firmware check
chroot /host ls /lib/firmware/amdgpu/ | head -20

# ROCm/AMD SMI tools (if installed on host)
chroot /host rocm-smi --showallinfo 2>/dev/null || echo "rocm-smi not available"
chroot /host amd-smi list 2>/dev/null || echo "amd-smi not available"

# lspci for GPU hardware
chroot /host lspci | grep -i "amd\|radeon\|display"

# Try reloading driver
chroot /host modprobe -r amdgpu && chroot /host modprobe amdgpu && echo "reloaded OK"
```

Cleanup:

```bash
kubectl --kubeconfig=$KUBECONFIG delete pod gpu-debug-$NODE -n $NS
```
