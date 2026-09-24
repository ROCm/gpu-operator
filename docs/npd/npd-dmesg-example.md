# NPD dmesg Kernel-Crash Detection Example

This page shows how to extend the Node Problem Detector (NPD) configuration to watch
the kernel ring buffer (`/dev/kmsg`) for AMD GPU crash patterns, alongside the standard
`amdgpuhealth` custom plugin monitor. The dmesg rules emit permanent node conditions
that the GPU Operator's auto-remediation controller can act on.

```{note}
This example extends the setup described in
[Node Problem Detector Integration](node-problem-detector.md).
Complete that setup first — RBAC, AMD Device Metrics Exporter, and the base DaemonSet
must all be in place before adding dmesg monitoring.
```

## How dmesg monitoring works with the GPU Operator

NPD's `system-log-monitor` reads `/dev/kmsg` and matches log lines against regex rules.
When a line matches a `permanent` rule, NPD sets the named node condition to `True`.
The GPU Operator's remediation controller watches node conditions and triggers an Argo
workflow when it sees a condition that matches a `nodeCondition` entry in the
remediation ConfigMap.

```
/dev/kmsg (kernel ring buffer)
        │
        ▼
NPD system-log-monitor ──► NodeCondition = True (e.g. AMDGPUKernelCrash)
        │
        ▼
GPU Operator remediation controller ──► Argo Workflow
```

## Step 1 — Add dmesg rules to the NPD ConfigMap

Extend the existing `node-problem-detector-config` ConfigMap with a second key,
`kernel-monitor.json`. The `system-log-monitor` plugin reads this file.

The condition name (`AMDGPUKernelCrash` in the example below) must match the
`nodeCondition` field in the GPU Operator remediation ConfigMap so the operator
knows which workflow to trigger.

```yaml
# node-problem-detector-config.yaml (extended)
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-problem-detector-config
  namespace: kube-system
data:
  # Existing key — custom plugin monitor for amdgpuhealth metric checks
  custom-plugin-monitor.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "30s",
        "timeout": "15s",
        "max_output_length": 80,
        "concurrency": 3,
        "enable_message_change_based_condition_update": false
      },
      "source": "amdgpu-custom-plugin-monitor",
      "metricsReporting": true,
      "conditions": [
        {
          "type": "AMDGPUUnhealthy",
          "reason": "AMDGPUIsUp",
          "message": "AMDGPU is up"
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "condition": "AMDGPUUnhealthy",
          "reason": "AMDGPUIsDown",
          "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
          "args": [
            "query",
            "counter-metric",
            "-m=GPU_ECC_UNCORRECT_UMC",
            "-t=1"
          ],
          "timeout": "15s"
        }
      ]
    }

  # New key — system-log monitor for dmesg / kmsg GPU crash patterns
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
          "reason": "AMDGPUReset",
          "pattern": "amdgpu.*GPU reset begin.*"
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

### Rule reference

| `type` | Effect |
|--------|--------|
| `temporary` | Emits a one-shot Kubernetes `Event`. Does not flip a node condition. |
| `permanent` | Sets the named `condition` to `True` and keeps it set. Use this for conditions the remediation controller watches. |

| Pattern | What it matches in dmesg |
|---------|--------------------------|
| `amdgpu.*GPU fault detected` | GPU page fault logged by the `amdgpu` kernel driver |
| `amdgpu.*GPU hang detected` | GPU hang or lockup |
| `amdgpu.*GPU reset begin` | Driver-initiated GPU reset |
| `amdgpu.*RAS ERROR` | Uncorrectable RAS error reported to the kernel |

`lookback: "5m"` replays the last 5 minutes of the ring buffer on startup, so crashes
that occurred just before NPD launched are not missed.

## Step 2 — Add the system-log monitor to the DaemonSet

Add `--config.system-log-monitor` and mount `/dev/kmsg`. The existing
`--config.custom-plugin-monitor` flag and all other mounts stay unchanged.

```yaml
# node-problem-detector.yaml (extended)
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
        # Required: keeps NPD running on nodes tainted by auto-remediation
        - key: amd-gpu-unhealthy
          operator: Exists
          effect: NoSchedule
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
        # dmesg / kernel ring buffer monitoring
        - --config.system-log-monitor=/config/kernel-monitor.json
        # amdgpuhealth metric monitoring
        - --config.custom-plugin-monitor=/config/custom-plugin-monitor.json
        securityContext:
          privileged: true
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        resources:
          limits:
            cpu: 20m
            memory: 100Mi
          requests:
            cpu: 10m
            memory: 80Mi
        volumeMounts:
        - name: log
          mountPath: /var/log
        - name: kmsg
          mountPath: /dev/kmsg
          readOnly: true
        - name: localtime
          mountPath: /etc/localtime
          readOnly: true
        - name: config
          mountPath: /config
          readOnly: true
        - name: amdexporter
          mountPath: /var/lib/amd-metrics-exporter
      volumes:
      - name: log
        hostPath:
          path: /var/log/
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
          - key: custom-plugin-monitor.json
            path: custom-plugin-monitor.json
          - key: kernel-monitor.json
            path: kernel-monitor.json
      - name: amdexporter
        hostPath:
          path: /var/lib/amd-metrics-exporter
```

```{important}
The `amd-gpu-unhealthy:NoSchedule` toleration is required. When auto-remediation taints
a node to evict workloads, NPD must keep running so its final condition check can
confirm that the node has recovered. Without this toleration, NPD is evicted and the
remediation workflow gets stuck waiting for the condition to flip back to `False`.
```

## Step 3 — Wire the condition into the remediation ConfigMap

Add an entry for `AMDGPUKernelCrash` to the GPU Operator remediation ConfigMap so the
operator knows which Argo workflow to run when NPD sets that condition.

```yaml
remediation:
  - nodeCondition: AMDGPUKernelCrash
    workflowTemplate: default-template
    validationTestsProfile:
      framework: AGFHC
      recipe: all_lvl4
      iterations: 1
      stopOnFailure: true
      timeoutSeconds: 4800
    physicalActionNeeded: false
    skipRebootStep: false
```

See the [Auto Node Remediation](../autoremediation/auto-remediation.md) documentation
for the full remediation ConfigMap schema and available fields.

## Step 4 — Apply and verify

```bash
kubectl apply -f node-problem-detector-config.yaml
kubectl rollout restart daemonset/node-problem-detector -n kube-system

# Confirm NPD pods are running on GPU nodes
kubectl get pods -n kube-system -l app=node-problem-detector -o wide

# Stream NPD logs to see both monitors active
kubectl logs -n kube-system -l app=node-problem-detector -f

# Check node conditions — both AMDGPUUnhealthy and AMDGPUKernelCrash should appear
kubectl describe node <gpu-node> | sed -n '/Conditions:/,/Addresses:/p'
```

When healthy, both conditions are `False`:

```
Conditions:
  Type                Status  Reason                  Message
  ----                ------  ------                  -------
  AMDGPUUnhealthy     False   AMDGPUIsUp              AMDGPU is up
  AMDGPUKernelCrash   False   NoAMDGPUKernelCrash     no AMD GPU kernel crash detected
```

When a matching dmesg line appears, `AMDGPUKernelCrash` flips to `True` and the GPU
Operator triggers an Argo workflow:

```bash
kubectl get workflows -A
kubectl get events -A --field-selector reason=amd-gpu-remediation-required
```
