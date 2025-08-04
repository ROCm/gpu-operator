# Node Problem Detector Integration

Node-problem-detector(NPD) aims to make various node problems visible to the upstream layers in the cluster management stack. It is a daemon that runs on each node, detects node problems and reports them to apiserver. NPD can be extended to detect AMD GPU problems.

## Node-Problem-Detector Installation

Many Kubernetes clusters like GKE, AKS, etc. come with NPD enabled by default. If not already present, easiest way to install is to use Helm chart. Follow the official [Node-problem-detector installation guide](https://github.com/kubernetes/node-problem-detector?tab=readme-ov-file#installation) for more information about installation.

## Custom Plugin Monitor

Custom plugin monitor is a plugin mechanism for node-problem-detector. It will extend node-problem-detector to execute any monitor scripts written in any language. The monitor scripts must conform to the plugin protocol in exit code and standard output. For more info about the plugin protocol, please refer to the [node-problem-detector plugin interface](https://docs.google.com/document/d/1jK_5YloSYtboj-DtfjmYKxfNnUxCAvohLnsH5aGCAYQ/edit#).

Exit codes 0, 1, and 2 are used for plugin monitor. Exit code 0 is treated as working state. Exit code 1 is treated as problem state. Exit code 2 is used for any unknown error. When plugin monitor detects exit code 1, it sets NodeCondition based on the rules defined in custom plugin monitor config file

## Node-Problem-Detector Integration
We provide a small utility, `amdgpuhealth`, queries various AMD GPU metrics from `device-metrics-exporter` and `Prometheus` endpoint. Based on user-configured thresholds, it determines if any AMD GPU is in problem state. NPD custom plugin monitor can invoke this program at configurable intervals to monitor various metrics and assess overall health of AMD GPUs. 

The utility `amdgpuhealth` is packaged with device-metrics-exporter docker image and will be copied to host path `/var/lib/amd-metrics-exporter`. NPD needs to mount this host path to be able to use the utility via custom plugin monitor.

Example usage of amdgpuhealth CLI:

```bash
./amdgpuhealth query counter-metric -m <metric_name> -t <threshold_value>

./amdgpuhealth query gauge-metric -m <metric_name> -d <duration> -t <threshold_value>
```

In the above examples, the program queries either a counter or gauge metric. You can define a threshold for each metric. If the reported AMD GPU metric value exceeds the threshold, `amdgpuhealth` prints an error message to standard output and exits with code 1. The NPD plugin uses this exit code and output to update the node condition's status and message respectively, indicating problem with AMD GPU.

Example custom plugin monitor config:
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

The above NPD config rule #1 queries counter metric `GPU_ECC_UNCORRECT_UMC` value every 30 seconds. If the value crosses threshold(set to 1), then NPD marks the node condition as True.

If you want to query average value of gauge metrics over a period of time, you need to setup Prometheus to scrape metrics from `amd-device-metrics-exporter` endpoint and store them. Rule #2 in above config queries gauge metric `GPUMetricField_GPU_EDGE_TEMPERATURE` value from Promethues server. It queries average temperature for the last 1 hour from the Prometheus endpoint mentioned in CLI argument.

## Handling Authorization Tokens

If your AMD Device Metrics Exporter or Prometheus endpoints require token-based authorization, Node Problem Detector(NPD) must include the token as an HTTP header in its requests. Since authorization tokens are sensitive, they should be stored in secure way. We recommend using **Kubernetes Secrets** to store the token information and mount them as volumes in the NPD pod.

1. **Creating a Authorization token Secret for AMD Device Metrics Exporter endpoint:**

You can create a Kubernetes Secret to store the token for the AMD Device Metrics Exporter endpoint in two ways:

**From a file:**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> amd-exporter-auth-token --from-file=token=<path-to-token-file>
```

**From a string literal**

```bash
kubectl create secret genreic -n <NPD_NAMESPACE> amd-exporter-auth-token --from-literal=token=<your-auth-token>
```

Mount this secret as a volume in your NPD deployment yaml. The same path must be specified as CLI argument in the NPD custom plugin monitor config yaml.

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


2. **Creating a Authorization token Secret for Prometheus endpoint:**

Similarly create secret for Prometheus endpoint. This will be needed for gauge metrics

**From a file**

```bash
kubectl create secret generic -n <NPD_NAMESPACE> prometheus-auth-token --from-file=token=<path-to-token-file>
```

**From a string literal**

```bash
kubectl create secret genreic -n <NPD_NAMESPACE> prometheus-auth-token --from-literal=token=<your-auth-token>
```

Mount this secret as a volume in your NPD deployment yaml. Pass the mount path in NPD custom plgin monitor json as CLI argument.

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

## Handling Mutual TLS (mTLS) Authentication

If your AMD Device Metrics Exporter or Prometheus endpoints require TLS/mTLS, Node Problem Detector(NPD) must have necessary certificates to be able to communicate with the endpoints.

For TLS, NPD needs to have server endpoint's Root CA certificate to authenticate the server's certificate. Root CA certificate must be stored as Kubernetes Secrets and mounted as volumes in the NPD pod.

1. **Creating Secret for AMD Device Metrics Exporter endpoint Root CA**

Please make sure the key in the secret is set to `ca.crt`
```bash
kubectl create secret generic -n <NPD_NAMESPACE> amd-exporter-rootca --from-file=ca.crt=<path-to-ca-cert>
```

Mount this secret as a volume in your NPD deployment yaml. Same mount path needs to be passed as CLI argument. Example:

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

2. **Creating Secret for Prometheus endpoint Root CA**

```bash
kubectl create secret generic -n <NPD_NAESPACE> prometheus-rootca --from-file=ca.crt=<path-to-ca-cert>
```

Mount this secret as a volume in your NPD deployment yaml. Pass the mount path in CLI argument followed by the key `ca.crt`. Example below:

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

For mTLS, NPD needs to have a certificate and it's corresponding private key. Certificate information can be stored as Kubernetes TLS Secret and mounted as colume in the NPD pod.

1. **Creating Secret for NPD identity certificate**

Please make sure you use the keys `tls.crt` and `tls.key` for certificate and key respectively

```bash
kubectl create secret tls -n <NPD_NAMESPACE> npd-identity --tls.crt=<path-to-your-certificate> --tls.key=<path-to-your-private-key>
```

Mount the secret as a volume in your NPD deployment yaml. Pass the mount path as CLI argument. Example below:

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
