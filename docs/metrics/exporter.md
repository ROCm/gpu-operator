# Metrics Exporter

## Configure metrics exporter

To start the Device Metrics Exporter along with the GPU Operator configure  the ``` spec/metricsExporter/enable ``` field in deviceconfig Custom Resource(CR) to enable/disable metrics exporter

```yaml
# Specify the metrics exporter config
metricsExporter:
    # To enable/disable the metrics exporter, disabled by default
    enable: True

    # kubernetes service type for metrics exporter, clusterIP(default) or NodePort
    serviceType: "NodePort"

    # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
    nodePort: 32500

    # image for the metrics-exporter container
    image: "rocm/device-metrics-exporter:v1.2.0"
 
```

The **metrics-exporter** pods start after updating the **DeviceConfig** CR

```bash
#kubectl get pods -n kube-amd-gpu -l "app.kubernetes.io/name=metrics-exporter"
NAME                                       READY   STATUS    RESTARTS   AGE
gpu-operator-metrics-exporter-q8hbb   1/1     Running   0          74s
```

```{note}
**Note:** The Device Metrics Exporter name will be prefixed with the name of your DeviceConfig custom resource ("gpu-operator" in the default helm installation)
```

## Metrics Exporter DeviceConfig

| Field Name                 | Details                                      |
|----------------------------|----------------------------------------------|
| **Enable**                 | Enable/Disable metrics exporter              |
| **Port**                   | Service port exposed by metrics exporter     |
| **serviceType**            | service type for metrics, clusterIP/NodePort |
| **nodePort**               | Node port for  metrics exporter service      |
| **selector**               | Node selector for metrics exporter daemonset |
| **image**                  | metrics exporter image                       |
| **config**                 | metrics configurations (fields/labels)       |
|                            |                                              |
| **name**                   | configmap name for custom fields/labels      |

## Customize metrics fields/labels

To customize metrics fields/labels, create a configmap with fields/labels and use it in **DeviceConfig** CR

```bash
kubectl create configmap <name> --from-file=examples/metricsExporter/config.json
```

Example config file is available here: [config.json](https://github.com/rocm/device-metrics-exporter/blob/main/example/config.json)
