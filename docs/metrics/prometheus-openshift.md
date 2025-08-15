# Prometheus Integration with Metrics Exporter for Openshift environments

The AMD GPU Operator integrates with Prometheus to enable monitoring of GPU metrics across the Kubernetes cluster. Metrics exposed by the Device Metrics Exporter can be automatically discovered and scraped by Prometheus through the creation of a ServiceMonitor resource.

Prometheus integration is managed via the **ServiceMonitor** configuration in the DeviceConfig Custom Resource (CR). When enabled, the operator automatically creates a ServiceMonitor tailored to the metrics exported by the Device Metrics Exporter. The integration supports various authentication and authorization methods, including Bearer Tokens and mutual TLS (mTLS), providing flexibility to accommodate different security requirements.

Openshift has its own integrated Prometheus instances which we will utilize instead of a separate operator that vanilla k8s environments would utilize. Additionally, Openshift natively supports Perses for dashboards instead of grafana which is supported with our vanilla k8s deployment guide. 

## Prerequisites

Before enabling Prometheus integration, ensure you have:
- Ensure you have enabled and configured the openshift-user-workload-monitoring
- Have labeled the kube-amd-gpu namespace with `openshift.io/cluster-monitoring=true`
- The Device Metrics Exporter enabled in your GPU Operator deployment.
- Properly configured kube-rbac-proxy in the DeviceConfig CR if the exporter endpoint is protected (Optional).

The AMD GPU Operator relies on the ServiceMonitor CRD being available to inject the CRs when enabled. This CRD is installed by the Prometheus Operator.

## Configuring Openshift for user workload monitoring

Redhat has provided documentation to configure user workload monitoring on openshift; please follow the documentation here:
https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html-multi/monitoring/index#user-workload-monitoring-first-steps

## DeviceConfig Configuration

To integrate Prometheus, configure the following section in the DeviceConfig CR under `metricsExporter.prometheus.serviceMonitor`:

```yaml
metricsExporter:
  enable: true
  prometheus:
    serviceMonitor:
      enable: true
      interval: "60s" # Scrape frequency
      attachMetadata:
        node: true
      honorLabels: false
      honorTimestamps: true
      labels:
        release: prometheus-operator # Prometheus release label for target discovery
```

- **enable**: Enable or disable Prometheus ServiceMonitor creation.
- **interval**: Frequency at which Prometheus scrapes metrics (e.g., "30s", "1m"). Defaulted to the interval configured in Prometheus global scope.
- **attachMetadata.node**: Attaches node metadata to discovered targets.
- **honorLabels**: Retain scraped metric labels over the target labels if conflicts arise.
- **honorTimestamps**: Retain timestamps from scraped metrics.
- **labels**: Custom labels added to the ServiceMonitor to facilitate Prometheus discovery.

## Authentication and TLS Options

The ServiceMonitor configuration supports various authentication and security methods for secure metrics collection:

```yaml
metricsExporter:
    prometheus:
        serviceMonitor:
            enable: true
            bearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token" # Deprecated
            authorization:
                credentials:
                    name: metrics-token
                    key: token
            tlsConfig:
                insecureSkipVerify: false
                serverName: metrics-server.example.com
                ca:
                    configMap:
                        name: server-ca
                        key: ca.crt
                cert:
                    secret:
                        name: prometheus-client-cert
                        key: tls.crt
                keySecret:
                    name: prometheus-client-cert
                    key: tls.key
```

- **bearerTokenFile**: (Deprecated) Path to a file containing the bearer token for authentication. Retained for legacy use case. Use authorization block instead to pass tokens.
- **authorization**: Configures token-based authorization. Reference to the token stored in a Kubernetes Secret
- **tlsConfig**: Configures TLS for secure connections:
    - **insecureSkipVerify**: When true, skips certificate verification (not recommended for production)
    - **serverName**: Server name used for certificate validation
    - **ca**: ConfigMap containing the CA certificate for server verification
    - **cert**: Secret containing the client certificate for mTLS
    - **keySecret**: Secret containing the client key for mTLS
    - **caFile/certFile/keyFile**: File equivalents for certificates/keys mounted in Prometheus pod.

These options allow secure metrics collection from AMD Device Metrics Exporter endpoints that are protected by the kube-rbac-proxy sidecar for authentication/authorization.

## Accessing Metrics with Openshift integrated Prometheus

Upon applying the DeviceConfig with the correct settings, the GPU Operator automatically:
- Deploys the ServiceMonitor resource in the GPU Operator namespace.
- Sets the required labels and namespace selectors in ServiceMonitor CR for Prometheus discovery.

After the **ServiceMonitor** is deployed, Prometheus automatically begins scraping metrics. Verify the integration by accessing the Openshift UI and navigating to the "Targets" page under the Observe Tab on the left-hand side of the Openshift UI. Your Device Metrics Exporter should appear as a healthy target which will be denoted under the status column as 'Up' with a green checkmark.

To access specific metrics, you can perform a query under the Metrics page under the Observe tab.

## Using with device-metrics-exporter with Perses based integrated Openshift Dashboards

TODO

### The `pod` Label Conflict

When Prometheus scrapes targets defined by a `ServiceMonitor`, it automatically attaches labels to the metrics based on the target's metadata. One such label is `pod`, which identifies the Pod being scraped (in this case, the metrics exporter Pod itself).

This creates a conflict:
1.  **Exporter Metric Label:** `pod="<workload-pod-name>"` (Indicates the actual GPU user)
2.  **Prometheus Target Label:** `pod="<metrics-exporter-pod-name>"` (Indicates the source of the metric)

### Solution 1: `honorLabels: true` (Default)

To ensure the Grafana dashboards function correctly by using the workload pod name, the `ServiceMonitor` created by the GPU Operator needs to prioritize the labels coming directly from the metrics exporter over the labels added by Prometheus during the scrape.

This is achieved by setting `honorLabels: true` in the `ServiceMonitor` configuration within the `DeviceConfig`. **This is the default setting in the GPU Operator.**

```yaml
# Example DeviceConfig snippet
spec:
  metricsExporter:
    prometheus:
      serviceMonitor:
        enable: true
        # honorLabels defaults to true, ensuring exporter's 'pod' label is kept
        # honorLabels: true 
        # ... other ServiceMonitor settings
```

**Important:** For this to work, the `device-metrics-exporter` must actually be exporting the `pod` label, which typically only happens when a workload is actively using the GPU on that node. If no workload is present, the `pod` label might be missing from the metric, and the dashboards might not display data as expected for that specific GPU/node.

### Solution 2: Relabeling

An alternative approach is to use Prometheus relabeling rules within the `ServiceMonitor` definition. This allows you to explicitly handle the conflicting `pod` label added by Prometheus.

You can rename the Prometheus-added `pod` label (identifying the exporter pod) to something else (e.g., `exporter_pod`) and then drop the original `pod` label added by Prometheus. This prevents the conflict and ensures the `pod` label from the exporter (identifying the workload) is the only one present on the final ingested metric.

Add the following `relabelings` to your `ServiceMonitor` configuration in the `DeviceConfig`:

```yaml
# Example DeviceConfig snippet
spec:
  metricsExporter:
    prometheus:
      serviceMonitor:
        enable: true
        honorLabels: false # Must be false if using relabeling to preserve exporter_pod
        relabelings:
          # Rename the Prometheus-added 'pod' label to 'exporter_pod'
          - sourceLabels: [pod]
            targetLabel: exporter_pod
            action: replace
            regex: (.*)
            replacement: $1
          # Drop the Prometheus-added 'pod' label to avoid conflict
          - action: labeldrop
            regex: pod
        # ... other ServiceMonitor settings
```

This method explicitly resolves the conflict by manipulating the labels before ingestion, ensuring the `pod` label always refers to the workload pod as intended by the `device-metrics-exporter`.

## Conclusion

The AMD GPU Operator provides native support for Prometheus integration, simplifying GPU monitoring and alerting within Kubernetes clusters. By configuring the DeviceConfig CR, you can manage GPU metrics collection tailored to your requirements and preferences.
For more detailed configuration guidance, refer to the example scenarios [README](https://github.com/rocm/gpu-operator/blob/main/example/).
