# Node Problem Detector Integration

**Node Problem Detector (NPD)** surfaces node problems to the rest of the cluster management stack. It runs as a daemon on each node, detects issues, and reports them to the API server. NPD can be extended to detect AMD GPU problems.

## Installation

Many Kubernetes clusters (for example, GKE and AKS) ship with NPD enabled by default. If it is not present, the simplest option is to install it via a Helm chart. For details, see the official [Node Problem Detector installation guide](https://github.com/kubernetes/node-problem-detector?tab=readme-ov-file#installation).

### Node Problem Detector Installation Steps - Kubernetes

NPD is typically installed in the `kube-system` namespace. Use the same namespace for consistency with common deployments.

#### Create a service account

The service account must be able to access metrics exporter endpoints. See the example [RBAC config](https://github.com/ROCm/gpu-operator/blob/main/tests/e2e/yamls/config/npd/node-problem-detector-rbac.yaml).

The ClusterRole must include the following permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-problem-detector
rules:
- apiGroups: [""]
  resources: ["nodes", "pods", "services"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch"]
- nonResourceURLs: ["/metrics", "/gpumetrics", "/inbandraserrors"]
  verbs: ["get"]
```

```bash
kubectl apply -f node-problem-detector-rbac.yaml
```

#### Create a custom plugin monitor config

Add a config file that defines the custom plugin monitor rules used for AMD GPU checks. See the [Custom Plugin Monitor](#custom-plugin-monitor) section and the [example DaemonSet config](https://github.com/ROCm/gpu-operator/blob/main/tests/e2e/yamls/config/npd/node-problem-detector.yaml) for the structure and mount details.

#### Install NPD

#### Option A — Helm chart

```bash
helm repo add deliveryhero https://charts.deliveryhero.io/
helm install --generate-name deliveryhero/node-problem-detector
```

#### Option B — Standalone YAML

Use the [node-problem-detector deployment manifest](https://github.com/kubernetes/node-problem-detector/blob/master/deployment/node-problem-detector.yaml):

```bash
kubectl create -f node-problem-detector.yaml
```

### Node Problem Detector Installation Steps - OpenShift

#### Create namespace and service account

```bash
oc create namespace node-problem-detector
oc create serviceaccount npd -n node-problem-detector
```

#### Grant required access to the service account

```bash
oc create clusterrolebinding npd-privileged-scc \
  --clusterrole=system:openshift:scc:privileged \
  --serviceaccount=node-problem-detector:npd

oc create clusterrole npd-pod-endpoint-access \
  --verb=get,list,watch --resource=pods,endpoints

oc create clusterrolebinding npd-pod-endpoint-access-binding \
  --clusterrole=npd-pod-endpoint-access \
  --serviceaccount=node-problem-detector:npd
```

#### Install NPD using Helm

```bash
helm install npd oci://ghcr.io/deliveryhero/helm-charts/node-problem-detector \
  --version 2.4.0 \
  -n node-problem-detector \
  --set serviceAccount.name=npd \
  --set serviceAccount.create=false
```

## Custom Plugin Monitor

The **custom plugin monitor** is NPD's plugin mechanism. It lets you run monitor scripts in any language. Scripts must follow the [NPD plugin interface](https://docs.google.com/document/d/1jK_5YloSYtboj-DtfjmYKxfNnUxCAvohLnsH5aGCAYQ/edit#) for exit codes and standard output.

| Exit code | Meaning |
| --------- | ------- |
| 0 | Healthy |
| 1 | Problem |
| 2 | Unknown error |

When a plugin returns exit code 1, NPD updates the node condition according to the rules in your custom plugin monitor config.

## AMD GPU integration

The **`amdgpuhealth`** utility queries AMD GPU metrics from the device metrics exporter and (optionally) Prometheus. You configure thresholds; if a metric exceeds its threshold, the tool reports a problem. NPD's custom plugin monitor can run `amdgpuhealth` on a schedule to assess AMD GPU health.

- **Location:** `amdgpuhealth` is included in the device-metrics-exporter image and is written to the host at `/var/lib/amd-metrics-exporter`.
- **NPD:** The NPD DaemonSet must mount that host path so the `amdgpuhealth` binary is available inside the NPD container.

For a full example, see the [NPD DaemonSet config](https://github.com/ROCm/gpu-operator/blob/main/tests/e2e/yamls/config/npd/node-problem-detector.yaml). The snippet below shows only the relevant mounts:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
spec:
  template:
    spec:
      containers:
      - name: node-problem-detector
        command:
        - /node-problem-detector
        - --logtostderr
        - --config.custom-plugin-monitor=/config/custom-plugin-monitor.json
        volumeMounts:
        - name: config
          mountPath: /config
          readOnly: true
        - name: amdexporter
          mountPath: /var/lib/amd-metrics-exporter
      volumes:
      - name: config
        configMap:
          name: node-problem-detector-config
          items:
          - key: custom-plugin-monitor.json
            path: custom-plugin-monitor.json
      - name: amdexporter
        hostPath:
          path: /var/lib/amd-metrics-exporter
```

### Using the `amdgpuhealth` CLI

```bash
./amdgpuhealth query counter-metric -m <metric_name> -t <threshold_value>
./amdgpuhealth query gauge-metric -m <metric_name> -d <duration> -t <threshold_value>
```

- **Counter metrics:** Use a threshold; if the metric value exceeds it, the tool exits with code 1.
- **Gauge metrics:** Use a threshold and optional duration; the tool can evaluate the average over that period.

When `amdgpuhealth` exits with code 1, it prints an error to stdout. NPD uses that exit code and message to set the node condition status and message.

### Example custom plugin monitor config

```json
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
      "type": "AMDGPUProblem",
      "reason": "AMDGPUIsUp",
      "message": "AMD GPU is up"
    }
  ],
  "rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=GPU_ECC_UNCORRECT_UMC",
        "-t=1"
      ],
      "timeout": "10s"
    },
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "gauge-metric",
        "-m=GPUMetricField_GPU_EDGE_TEMPERATURE",
        "-t=100",
        "-d=1h",
        "--prometheus-endpoint=http://localhost:9090"
      ],
      "timeout": "10s"
    }
  ]
}
```

- **Rule 1:** Runs every 30 seconds and checks the counter metric `GPU_ECC_UNCORRECT_UMC`. If the value exceeds the threshold (1), NPD sets the node condition to indicate a problem.
- **Rule 2:** Checks the gauge metric `GPUMetricField_GPU_EDGE_TEMPERATURE` from Prometheus. To use gauge metrics over a time window, Prometheus must be scraping the `amd-device-metrics-exporter` endpoint. This rule evaluates the average temperature over the last hour using the Prometheus endpoint given in the CLI args.

## Authorization tokens

If the AMD Device Metrics Exporter or Prometheus endpoints use token-based authorization, NPD must send the token in an HTTP header. Store the token in a **Kubernetes Secret** and mount it into the NPD pod.

### Token for AMD Device Metrics Exporter

Create a Secret in the NPD namespace:

**From a file:**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> amd-exporter-auth-token --from-file=token=<path-to-token-file>
```

**From a literal value:**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> amd-exporter-auth-token --from-literal=token=<your-auth-token>
```

Mount this Secret as a volume in the NPD deployment. In the custom plugin monitor config, pass the mount path as the CLI argument for the exporter token.

```json
"rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=GPU_ECC_UNCORRECT_UMC",
        "-t=1",
        "--exporter-bearer-token=<token-mount-path>"
      ],
      "timeout": "10s"
    }
]
```

### Token for Prometheus

Required when querying gauge metrics from Prometheus. Create a Secret the same way:

**From a file:**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> prometheus-auth-token --from-file=token=<path-to-token-file>
```

**From a literal value:**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> prometheus-auth-token --from-literal=token=<your-auth-token>
```

Mount the Secret in the NPD pod and pass the mount path in the custom plugin monitor config as the Prometheus token argument.

```json
"rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "gauge-metric",
        "-m=GPUMetricField_GPU_EDGE_TEMPERATURE",
        "-t=100",
        "--prometheus-bearer-token=<token-mount-path>"
      ],
      "timeout": "10s"
    }
]
```

## Mutual TLS (mTLS) authentication

If the AMD Device Metrics Exporter or Prometheus endpoints use TLS or mTLS, NPD must have the right certificates to connect.

**TLS (server authorization):** NPD needs the server’s Root CA certificate to verify the server. Store it in a Kubernetes Secret and mount it into the NPD pod.

### Root CA for AMD Device Metrics Exporter

The Secret key must be `ca.crt`:

```bash
kubectl create secret generic -n <NPD_NAMESPACE> amd-exporter-rootca --from-file=ca.crt=<path-to-ca-cert>
```

Mount this Secret in the NPD deployment and pass the mount path as the CLI argument. Example:

```json
"rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=GPU_ECC_UNCORRECT_UMC",
        "-t=1",
        "--exporter-root-ca=<rootca-mount-path>"
      ],
      "timeout": "10s"
    }
]
```

### Root CA for Prometheus

```bash
kubectl create secret generic -n <NPD_NAMESPACE> prometheus-rootca --from-file=ca.crt=<path-to-ca-cert>
```

Mount the Secret in the NPD pod and pass the path (including `ca.crt`) in the CLI argument. Example:

```json
"rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "gauge-metric",
        "-m=GPUMetricField_GPU_EDGE_TEMPERATURE",
        "-t=100",
        "--prometheus-root-ca=<rootca-mount-path>/ca.crt"
      ],
      "timeout": "10s"
    }
]
```

**mTLS (client authorization):** NPD also needs a client certificate and its private key. Store them in a Kubernetes TLS Secret and mount it into the NPD pod.

### NPD client identity (mTLS)

Use the keys `tls.crt` and `tls.key` for the certificate and private key:

```bash
kubectl create secret tls -n <NPD_NAMESPACE> npd-identity --cert=<path-to-your-certificate> --key=<path-to-your-private-key>
```

Mount the Secret in the NPD deployment and pass the mount path in the CLI arguments. Example:

```json
"rules": [
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=GPU_ECC_UNCORRECT_UMC",
        "-t=1",
        "--exporter-root-ca=<rootca-mount-path>",
        "--client-cert=<client-cert-mount-path>"
      ],
      "timeout": "10s"
    },
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "./config/amdgpuhealth",
      "args": [
        "query",
        "gauge-metric",
        "-m=GPUMetricField_GPU_EDGE_TEMPERATURE",
        "-t=100",
        "--prometheus-root-ca=<rootca-mount-path>/ca.crt",
        "--client-cert=<client-cert-mount-path>"
      ],
      "timeout": "10s"
    }
]
```
