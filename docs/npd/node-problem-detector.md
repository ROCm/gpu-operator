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

oc create clusterrole npd-node-status-patch \
  --verb=get,list,watch,patch,update --resource=nodes,nodes/status

oc create clusterrolebinding npd-node-status-patch-binding \
  --clusterrole=npd-node-status-patch \
  --serviceaccount=node-problem-detector:npd

oc create clusterrole npd-events \
  --verb=create,patch --resource=events

oc create clusterrolebinding npd-events-binding \
  --clusterrole=npd-events \
  --serviceaccount=node-problem-detector:npd
```

NPD also needs `get` access to a few non-resource URLs exposed by the AMD Device Metrics Exporter (`/metrics`, `/gpumetrics`, `/inbandraserrors`). Create the ClusterRole from YAML since `oc create clusterrole` does not support `--non-resource-url`:

```bash
cat <<EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: npd-metrics-endpoints
rules:
- nonResourceURLs: ["/metrics", "/gpumetrics", "/inbandraserrors"]
  verbs: ["get"]
EOF

oc create clusterrolebinding npd-metrics-endpoints-binding \
  --clusterrole=npd-metrics-endpoints \
  --serviceaccount=node-problem-detector:npd
```

> **Note:** NPD requires permission to patch `nodes/status` so it can add and update node conditions when problems are detected. The `events` permissions let NPD publish problem events, and access to the non-resource URLs above is required for the `amdgpuhealth` plugin to query the AMD Device Metrics Exporter. Missing any of these will cause NPD to silently fail to report node health to the cluster.

#### Install NPD using Helm

```bash
helm install npd oci://ghcr.io/deliveryhero/helm-charts/node-problem-detector \
  --version 2.4.0 \
  -n node-problem-detector \
  --set serviceAccount.name=npd \
  --set serviceAccount.create=false \
  --set rbac.create=false
```

The `serviceAccount.name=npd` and `serviceAccount.create=false` flags ensure the chart uses the `npd` service account created in the previous section instead of creating a new one. Setting `rbac.create=false` prevents the chart from creating its own `ClusterRole`/`ClusterRoleBinding`, since the required permissions (privileged SCC, pod/endpoint access, and `nodes/status` patch) were already granted to the `npd` service account above.

## Custom Plugin Monitor

The **custom plugin monitor** is NPD's plugin mechanism. It lets you run monitor scripts in any language. Scripts must follow the [NPD plugin interface](https://docs.google.com/document/d/1jK_5YloSYtboj-DtfjmYKxfNnUxCAvohLnsH5aGCAYQ/edit#) for exit codes and standard output.

| Exit code | Meaning |
| --------- | ------- |
| 0 | Healthy |
| 1 | Problem |
| 2 | Unknown error |

When a plugin returns exit code 1, NPD updates the node condition according to the rules in your custom plugin monitor config.

## Integration with Auto Node Remediation feature

NPD is the detection layer for the AMD GPU Operator's [Auto Node Remediation](../autoremediation/auto-remediation.md) feature: NPD evaluates GPU health on each node and publishes a per-error-type node condition, and the operator's remediation controller watches those conditions and triggers the appropriate remediation workflow when one transitions to `True`.

### Prerequisites

Before integrating NPD with auto node remediation, ensure the following are in place:

- **AMD Device Metrics Exporter (DME)** is installed on every GPU node. DME provides the `amdgpuhealth` binary at `/var/lib/amd-metrics-exporter` and exposes the `/metrics`, `/gpumetrics`, and `/inbandraserrors` endpoints that the NPD plugin queries. Without DME, the integration cannot function. See the [Device Metrics Exporter documentation](../metricsexporter/metricsexporter.md) for installation steps.
- **NPD is installed** with the RBAC granted in the [Installation](#installation) section above (including `nodes/status` patch, `events` create/patch, and `get` on the DME non-resource URLs).
- **Auto node remediation is enabled** on the `DeviceConfig` and a remediation ConfigMap is configured. See the [Auto Node Remediation](../autoremediation/auto-remediation.md) doc.

### Integration steps

1. **Mount the DME host path** (`/var/lib/amd-metrics-exporter`) into the NPD container so the `amdgpuhealth` binary is reachable.
2. **Mount the SAG-derived NPD ConfigMap** as `custom-plugin-monitor.json` and pass it via `--config.custom-plugin-monitor=...`. The SAG-derived ConfigMap is not publicly distributed — contact the AMD support team to obtain it (see the [important note](#example-custom-plugin-monitor-config) under the example config).
3. **Add the `amd-gpu-unhealthy:NoSchedule` toleration** to the NPD DaemonSet so it keeps running on nodes that auto-remediation has tainted (this is required — see [NPD Configuration in the Auto Remediation doc](../autoremediation/auto-remediation.md#other-configuration-options)).
4. **Align node-condition names with the remediation ConfigMap.** Each rule in the NPD plugin monitor config emits a node condition whose `condition` value must exactly match a `nodeCondition` entry in the remediation ConfigMap. The SAG-derived NPD config and SAG-derived remediation ConfigMap are designed to align; if you customize either, keep the names in sync.
5. **Use `type: permanent` rules** in the plugin monitor config. Auto-remediation's final workflow step verifies that the condition has flipped back to `False` after recovery; only `permanent` rules track condition state across invocations the way auto-remediation expects.
6. **(Optional) Authorization / TLS**: if DME or Prometheus require tokens or mTLS, configure them as described in the [Authorization tokens](#authorization-tokens) and [Mutual TLS (mTLS) authentication](#mutual-tls-mtls-authentication) sections.

```{note}
The Kubernetes installation steps above use the `kube-system` namespace by convention, while the OpenShift steps use a dedicated `node-problem-detector` namespace. Either is fine — just make sure the DaemonSet mounts, RBAC, and toleration below all target the same namespace where you installed NPD.
```

### NPD DaemonSet snippet

To make NPD GPU-aware, configure its custom plugin monitor to invoke the **`amdgpuhealth`** utility, which queries AMD GPU metrics from DME (and optionally Prometheus) and exits non-zero when a metric crosses a configured threshold. NPD then translates that exit code into a node condition that auto node remediation can act on.

For a full example, see the [NPD DaemonSet config](https://github.com/ROCm/gpu-operator/blob/main/tests/e2e/yamls/config/npd/node-problem-detector.yaml). The snippet below shows the mounts, the SAG ConfigMap, and the required toleration:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
spec:
  template:
    spec:
      tolerations:
      - key: amd-gpu-unhealthy
        operator: Exists
        effect: NoSchedule
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

```{important}
The `amd-gpu-unhealthy:NoSchedule` toleration is **required**. During remediation the GPU Operator taints the affected node with this taint to evict workloads; if NPD does not tolerate it, NPD itself gets evicted and the workflow's final "Verify Condition" step has no detector to confirm that the node has recovered, leaving the node stuck in a tainted state.
```

### Configuring NPD via the Helm chart

The DaemonSet snippet above can also be expressed as Helm values when installing the chart. Save the following as `npd-values.yaml`:

```yaml
tolerations:
  - key: amd-gpu-unhealthy
    operator: Exists
    effect: NoSchedule

extraVolumes:
  - name: amdexporter
    hostPath:
      path: /var/lib/amd-metrics-exporter
  - name: config
    configMap:
      name: node-problem-detector-config
      items:
        - key: custom-plugin-monitor.json
          path: custom-plugin-monitor.json

extraVolumeMounts:
  - name: amdexporter
    mountPath: /var/lib/amd-metrics-exporter
  - name: config
    mountPath: /config
    readOnly: true

settings:
  custom_plugin_monitor:
    - /config/custom-plugin-monitor.json
```

Then install (or upgrade) the chart with `-f npd-values.yaml`, e.g.:

```bash
helm upgrade --install npd oci://ghcr.io/deliveryhero/helm-charts/node-problem-detector \
  --version 2.4.0 \
  -n node-problem-detector \
  --set serviceAccount.name=npd \
  --set serviceAccount.create=false \
  --set rbac.create=false \
  -f npd-values.yaml
```

```{note}
Helm value names (e.g., `extraVolumes`, `settings.custom_plugin_monitor`) vary across NPD chart versions. Always check the chart's `values.yaml` for the exact keys and adapt accordingly.
```

### Verify the integration

After applying the configuration, confirm NPD is wired in correctly:

1. **NPD pods are running** on every GPU node:

   ```bash
   kubectl get pods -n <npd-namespace> -o wide
   ```

2. **NPD logs show `amdgpuhealth` invocations** without permission/connection errors:

   ```bash
   kubectl logs -n <npd-namespace> <npd-pod>
   ```

3. **Node conditions appear** for the AMD GPU error types defined in the SAG-derived plugin monitor config:

   ```bash
   kubectl describe node <gpu-node> | sed -n '/Conditions:/,/Addresses:/p'
   ```

   Each rule's `condition` should be listed (status `False` while healthy).

4. **End-to-end check:** when a GPU error is injected (or a threshold is intentionally tripped), the corresponding node condition should transition to `True`, and the GPU Operator should create an Argo `Workflow` for that node:

   ```bash
   kubectl get workflows -A
   kubectl get events -A --field-selector reason=amd-gpu-remediation-required
   ```

If the condition flips to `True` but no workflow is created, check that the `condition` name emitted by NPD matches a `nodeCondition` entry in the remediation ConfigMap.

### Air-gapped / disconnected clusters

When integrating NPD with auto remediation in an air-gapped environment, ensure the following images and assets are mirrored into the local registry alongside the GPU Operator images covered in the [air-gapped install docs](../specialized_networks/airgapped-install.md):

- The **NPD container image** referenced by your NPD DaemonSet / Helm chart values.
- The **AMD Device Metrics Exporter image** (provides `amdgpuhealth` and the metrics endpoints).
- The **SAG-derived NPD ConfigMap** (delivered via the AMD support team — see the [important note](#example-custom-plugin-monitor-config)). If it is delivered as a container image, mirror that image as well.
- The **auto node remediation ConfigMap image** (`spec.remediationWorkflow.configMapImage` on the `DeviceConfig`).

### Using the standalone `amdgpuhealth` CLI

```bash
./amdgpuhealth query counter-metric -m <metric_name> -t <threshold_value>
./amdgpuhealth query gauge-metric -m <metric_name> -d <duration> -t <threshold_value>
./amdgpuhealth query inband-ras-errors -s CPER_SEVERITY_FATAL --afid <afid_num> -t <threshold_value>
```

In all three modes, `amdgpuhealth` follows the standard NPD plugin contract: it exits with code `0` when everything is healthy, code `1` when a problem is detected, and code `2` for unknown errors (e.g., the metric source is unreachable or the query itself failed). NPD only triggers corrective action — updating the node condition to `True` so that auto node remediation can take over — on exit code `1`. Exit code `2` is treated as an unknown state and no remediation action is taken.

- **Counter metrics:** Compares the metric value against the configured threshold. The tool exits `0` while the value stays at or below the threshold, and exits `1` once it exceeds it; exit code `2` is returned if the metric cannot be retrieved.
- **Gauge metrics:** Evaluates the metric (optionally averaged over a duration) against the threshold. Exit code is `0` when the value is within the threshold, `1` when the threshold is breached, and `2` if the metric or Prometheus endpoint cannot be queried.
- **Inband RAS errors:** Continuously checks for fatal/critical inband RAS errors as they occur. The tool exits `0` when no qualifying errors are present, `1` as soon as a fatal/critical RAS error is detected so the node is marked unhealthy and corrective action can be initiated, and `2` if the RAS error source cannot be read.

When `amdgpuhealth` exits with code `1`, it also prints an error message to stdout. NPD uses the exit code and that message to set the node condition's status and message, which the auto node remediation controller then consumes.

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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=gpu_ecc_uncorrect_total",
        "-t=0"
      ],
      "timeout": "10s"
    },
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
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

- **Rule 1:** Runs every 30 seconds and checks the counter metric `gpu_ecc_uncorrect_total`. If the value exceeds the threshold (0), NPD sets the node condition to indicate a problem.
- **Rule 2:** Checks the gauge metric `GPUMetricField_GPU_EDGE_TEMPERATURE` from Prometheus. To use gauge metrics over a time window, Prometheus must be scraping the `amd-device-metrics-exporter` endpoint. This rule evaluates the average temperature over the last hour using the Prometheus endpoint given in the CLI args.

```{important}
The configuration shown above is only an illustrative example. The actual NPD custom plugin monitor configuration used in production is derived from the **AMD Service Action Guide (SAG)**, which defines the authoritative set of node conditions, metrics, thresholds, and corresponding remediation actions for AMD GPUs. Always refer to the SAG-derived configuration (and any updates published with it) rather than treating the snippet above as the complete or canonical config.

The SAG-derived NPD ConfigMap is not publicly distributed. To obtain it, please contact the **AMD support team**.
```

## Authorization tokens

If the AMD Device Metrics Exporter or Prometheus endpoints use token-based authorization, NPD must send the token in an HTTP header. Store the token in a **Kubernetes Secret** and mount it into the NPD pod.

```{note}
When RBAC is enabled on the AMD Device Metrics Exporter, the `ClusterRole` bound to the token's ServiceAccount must grant `get` on the non-resource URLs `/metrics`, `/gpumetrics`, and `/inbandraserrors`. Without these permissions, requests carrying the bearer token will be rejected with `403 Forbidden` and the `amdgpuhealth` plugin will fail.
```

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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=gpu_ecc_uncorrect_total",
        "-t=0",
        "--exporter-bearer-token=<token-mount-path>/token"
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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
      "args": [
        "query",
        "gauge-metric",
        "-m=GPUMetricField_GPU_EDGE_TEMPERATURE",
        "-t=100",
        "--prometheus-bearer-token=<token-mount-path>/token"
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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=gpu_ecc_uncorrect_total",
        "-t=0",
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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
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
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
      "args": [
        "query",
        "counter-metric",
        "-m=gpu_ecc_uncorrect_total",
        "-t=0",
        "--exporter-root-ca=<rootca-mount-path>",
        "--client-cert=<client-cert-mount-path>"
      ],
      "timeout": "10s"
    },
    {
      "type": "permanent",
      "condition": "AMDGPUProblem",
      "reason": "AMDGPUIsDown",
      "path": "/var/lib/amd-metrics-exporter/amdgpuhealth",
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
