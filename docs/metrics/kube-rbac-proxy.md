# Kube-RBAC-Proxy with Metrics Exporter

The **kube-rbac-proxy** sidecar container is used to secure the metrics endpoint by enforcing **Role-Based Access Control (RBAC)**. By enabling the **kube-rbac-proxy**, only authorized users can access the `/metrics` URL, ensuring the security of your metrics data.

## Configure Kube-RBAC-Proxy

To enable and configure the **kube-rbac-proxy** sidecar container, add the `rbacConfig` section to the **Metrics Exporter** configuration in the **DeviceConfig** CR. Here's a quick overview of the settings for **kube-rbac-proxy**:

- **enable**: Set to `true` to enable the **kube-rbac-proxy** sidecar container.
- **image**: Specify the image for the **kube-rbac-proxy** container. If not specified, the default image is used.
- **secret**: Provide the secret name that contains the TLS certificates and private keys for securing the metrics endpoint with HTTPS.
- **disableHttps**: If set to `true`, the HTTPS protection for the metrics endpoint is disabled. By default, this is `false`, and HTTPS is enabled for secure communication.

### Example: DeviceConfig CR with kube-rbac-proxy

```yaml
metricsExporter:
    enable: true
    serviceType: "NodePort"
    nodePort: 32500
    image: "amd/device-metrics-exporter/exporter:v1"
    
    # Enable Kube-RBAC-Proxy
    rbacConfig:
        enable: true  # Enable the kube-rbac-proxy sidecar
        image: "quay.io/brancz/kube-rbac-proxy:v0.18.1"  # Image for the kube-rbac-proxy sidecar container
        secret:
            name: "my-tls-secret"  # Secret containing the TLS certificate and key for kube-rbac-proxy
        disableHttps: false  # Set to true if you want to disable HTTPS protection
```

## Provide Custom TLS Certificates

If you want to provide custom TLS certificates, create a Kubernetes secret containing the **TLS certificate** (`tls.crt`) and **private key** (`tls.key`), and reference this secret in the `rbacConfig.secret.name` field.

### Example: Create TLS Secret

To create the secret containing your custom certificates, run the following command:

```bash
kubectl create secret tls my-tls-secret --cert=path/to/cert.crt --key=path/to/cert.key -n kube-amd-gpu
```

### Apply the Secret and CRD Update

Once the TLS secret is created, the **DeviceConfig** CR will automatically apply the secret to the **kube-rbac-proxy** sidecar, securing the metrics endpoint with TLS.

## Accessing Metrics

For a complete guide on how to access the metrics securely (including the generation of tokens, applying RBAC roles, and accessing the metrics inside and outside the cluster), please refer to the example [README](https://github.com/rocm/gpu-operator/blob/main/example/metricsExporter/README.md) in the repository. This includes detailed steps on:

- Deploying the metrics-reader roles
- Generating tokens for the service account
- Accessing the metrics from inside and outside the Kubernetes cluster

## Conclusion

By following these steps, you will have a fully functional setup for accessing metrics from your AMD GPU cluster using the **Metrics Exporter** and **kube-rbac-proxy**. The **kube-rbac-proxy** ensures that only authorized users can access the metrics, and the setup supports both internal and external access with appropriate security mechanisms (including TLS and RBAC).

For more detailed configuration guidance, refer to the example [README](https://github.com/rocm/gpu-operator/blob/main/example/metricsExporter/README.md) for information on token generation, cluster role deployment, and accessing metrics both inside and outside the cluster.
