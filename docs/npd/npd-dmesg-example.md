# NPD dmesg-Only Example

This page shows the minimal configuration to use Node Problem Detector to watch the
kernel ring buffer (`/dev/kmsg`) for AMD GPU crashes. No AMD Device Metrics Exporter
or `amdgpuhealth` binary is required — this example uses only NPD's built-in
**system-log monitor**.

## How it works

NPD reads `/dev/kmsg` continuously. When a log line matches a regex rule, NPD either
emits a one-shot `Event` (`type: temporary`) or flips a persistent `NodeCondition`
to `True` (`type: permanent`). The conditions are visible via `kubectl describe node`.

## Step 1 — RBAC

NPD needs permission to patch `nodes/status` (to write conditions) and create `events`.
No non-resource URL permissions are needed for the dmesg-only setup.

```yaml
# npd-rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-problem-detector
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-problem-detector
rules:
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: node-problem-detector
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-problem-detector
subjects:
- kind: ServiceAccount
  name: node-problem-detector
  namespace: kube-system
```

```bash
kubectl apply -f npd-rbac.yaml
```

## Step 2 — ConfigMap

The `kernel-monitor.json` key defines which dmesg patterns to watch.

```yaml
# npd-dmesg-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-problem-detector-config
  namespace: kube-system
data:
  kernel-monitor.json: |
    {
      "plugin": "kmsg",
      "logPath": "/dev/kmsg",
      "lookback": "5m",
      "bufferSize": 10,
      "source": "kernel-monitor",
      "conditions": [
        {
          "type": "AMDGPUKernelCrash",
          "reason": "NoAMDGPUKernelCrash",
          "message": "no AMD GPU kernel crash detected"
        }
      ],
      "rules": [
        {
          "type": "temporary",
          "reason": "AMDGPUHang",
          "pattern": "amdgpu.*GPU hang detected.*"
        },
        {
          "type": "temporary",
          "reason": "AMDGPUReset",
          "pattern": "amdgpu.*GPU reset begin.*"
        },
        {
          "type": "permanent",
          "condition": "AMDGPUKernelCrash",
          "reason": "AMDGPUPageFault",
          "pattern": "amdgpu.*GPU fault detected.*"
        },
        {
          "type": "permanent",
          "condition": "AMDGPUKernelCrash",
          "reason": "AMDGPUHangPermanent",
          "pattern": "amdgpu.*GPU hang detected.*"
        },
        {
          "type": "permanent",
          "condition": "AMDGPUKernelCrash",
          "reason": "AMDGPURASError",
          "pattern": "amdgpu.*RAS ERROR.*"
        }
      ]
    }
```

```bash
kubectl apply -f npd-dmesg-config.yaml
```

### Rule reference

| `type` | Effect |
|--------|--------|
| `temporary` | Emits a one-shot Kubernetes `Event`; does not change a node condition. |
| `permanent` | Sets the named `condition` to `True` and keeps it set until the node is restarted or the condition is explicitly cleared. Use this when downstream remediation needs to read the condition. |

| Pattern | What it matches in dmesg |
|---------|--------------------------|
| `amdgpu.*GPU fault detected` | GPU page fault logged by the `amdgpu` kernel driver |
| `amdgpu.*GPU hang detected` | GPU hang / lockup |
| `amdgpu.*GPU reset begin` | Driver-initiated GPU reset |
| `amdgpu.*RAS ERROR` | Uncorrectable RAS error reported to the kernel |

`lookback: "5m"` tells NPD to replay the last 5 minutes of the ring buffer on startup
so that crashes that happened just before NPD launched are not missed.

## Step 3 — DaemonSet

```yaml
# npd-dmesg.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
  labels:
    app: node-problem-detector
spec:
  selector:
    matchLabels:
      app: node-problem-detector
  template:
    metadata:
      labels:
        app: node-problem-detector
    spec:
      nodeSelector:
        feature.node.kubernetes.io/amd-gpu: "true"
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
      serviceAccountName: node-problem-detector
      containers:
      - name: node-problem-detector
        image: registry.k8s.io/node-problem-detector/node-problem-detector:v0.8.19
        command:
        - /node-problem-detector
        - --logtostderr
        - --config.system-log-monitor=/config/kernel-monitor.json
        securityContext:
          privileged: true
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        resources:
          limits:
            cpu: 10m
            memory: 80Mi
          requests:
            cpu: 10m
            memory: 80Mi
        volumeMounts:
        - name: kmsg
          mountPath: /dev/kmsg
          readOnly: true
        - name: localtime
          mountPath: /etc/localtime
          readOnly: true
        - name: config
          mountPath: /config
          readOnly: true
      volumes:
      - name: kmsg
        hostPath:
          path: /dev/kmsg
      - name: localtime
        hostPath:
          path: /etc/localtime
      - name: config
        configMap:
          name: node-problem-detector-config
          items:
          - key: kernel-monitor.json
            path: kernel-monitor.json
```

The key difference from the full integration example is:

- Only `--config.system-log-monitor` is passed — no `--config.custom-plugin-monitor`.
- `/var/log` and the `amdexporter` host path are not mounted (not needed).
- The `/dev/kmsg` mount is read-only; `privileged: true` is still required for NPD to
  open the device.

```bash
kubectl apply -f npd-dmesg.yaml
```

## Step 4 — Verify

```bash
# NPD pods running on GPU nodes
kubectl get pods -n kube-system -l app=node-problem-detector -o wide

# Stream NPD logs to see pattern matches in real time
kubectl logs -n kube-system -l app=node-problem-detector -f

# Check the node condition (False = healthy, True = crash detected)
kubectl describe node <gpu-node> | sed -n '/Conditions:/,/Addresses:/p'
```

When a matching dmesg line appears, the `AMDGPUKernelCrash` condition flips to `True`:

```
Conditions:
  Type                  Status  ...  Reason               Message
  ----                  ------  ---  ------               -------
  AMDGPUKernelCrash     True    ...  AMDGPUHangPermanent  amdgpu: GPU hang detected ...
```

## Next steps

- To also check GPU ECC and other health metrics, add a `custom-plugin-monitor` config
  and the `--config.custom-plugin-monitor` flag as described in
  [node-problem-detector.md](node-problem-detector.md).
- To trigger automatic node remediation when a condition fires, see the
  [Auto Node Remediation](../autoremediation/auto-remediation.md) documentation.
