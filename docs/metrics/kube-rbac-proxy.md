# Kube-RBAC-Proxy with Metrics Exporter

The **kube-rbac-proxy** sidecar container is used to secure the metrics endpoint by enforcing Role-Based Access Control (RBAC) or Static Authorization based on the Kubernetes authentication model.

By enabling kube-rbac-proxy, you ensure that only authorized users (or authorized client certificates) can access the `/metrics` endpoint.

## Configure Kube-RBAC-Proxy

To enable and configure the **kube-rbac-proxy** sidecar container, add the `rbacConfig` section to the **Metrics Exporter** configuration in the **DeviceConfig** CR. Here's a quick overview of the settings for kube-rbac-proxy:

- **enable**: Set to `true` to enable the kube-rbac-proxy sidecar container.
- **image**: Specify the image for the kube-rbac-proxy container. If not specified, the default image is used.
- **secret**: Kubernetes Secret containing server TLS cert (`tls.crt`) and key (`tls.key`).
- **disableHttps**: If set to `true`, the HTTPS protection for the metrics endpoint is disabled. By default, this is `false`, and HTTPS is enabled for secure communication.
- **clientCAConfigMap**: Kubernetes ConfigMap containing client CA cert (`ca.crt`) for mutual TLS validation.
- **staticAuthorization.enable**: Enables static authorization mode based on client certificate Common Name (CN).
- **staticAuthorization.clientName**: The expected Common Name (CN) to authorize when static authorization is enabled.

It is mandatory to provide a valid TLS server certificate (via a Secret) if HTTPS is enabled.

## Authentication and Authorization Modes

Kube-rbac-proxy supports these distinct modes for authentication and authorization, each suited to different security and operational needs:

### Token-Based Authentication

The default authentication mode uses Kubernetes Bearer tokens. When a client requests the metrics endpoint, kube-rbac-proxy first performs a **TokenReview** by sending the token to the Kubernetes API Server for validation. If the token is valid, kube-rbac-proxy then performs a **SubjectAccessReview (SAR)** to check if the authenticated user has permission to perform a `GET` request on the `/metrics` endpoint.

This method is straightforward and leverages native Kubernetes RBAC policies, making it suitable for most typical Kubernetes environments.

### Mutual TLS (mTLS) Authentication

In mutual TLS authentication, clients authenticate themselves using TLS certificates rather than Bearer tokens. When mTLS is enabled, kube-rbac-proxy validates the client's certificate against the configured CA (clientCAConfigMap). In this mode, **TokenReview** is completely bypassed, and even if a Bearer token is provided, it will be ignored in favor of the client certificate validation.

#### Certificate-Based RBAC Authorization

In certificate-based RBAC authorization, kube-rbac-proxy validates the client's TLS certificate against the provided CA and then performs a standard **SubjectAccessReview** using the certificate's Common Name (CN) as the username. This method combines the security benefits of certificate-based authentication with Kubernetes' native RBAC system.

The client must present a valid certificate isigned by the configured CA, and the CN from this certificate is used to determine if the client has the necessary RBAC permissions to access the metrics endpoint. This approach provides stronger authentication than token-based methods while still leveraging your existing Kubernetes RBAC policies for authorization decisions.

#### Static Authorization

When using Static Authorization, kube-rbac-proxy validates the client's certificate against the CA and checks if the Common Name (CN) in the client certificate matches the specified `staticAuthorization.clientName`. If they match, access is granted without performing a **SubjectAccessReview** against the Kubernetes API server.

This method bypasses both TokenReview and SubjectAccessReview, offering a completely standalone authentication and authorization mechanism that doesn't rely on the Kubernetes API server for validation. It's useful in scenarios where you want to restrict access to specific, pre-approved clients identified by their certificate CN. It also reduces the load on the API server and works in scenarios where connectivity from the metrics endpoint to the API server is not guaranteed always.

**Note**: We recommend using mTLS authentication whenever possible as it's more secure than service account tokens. Certificates are harder to compromise than tokens and provide stronger identity verification through cryptographic means.

## Setting Up TLS Certificates

If you want to provide custom TLS certificates, create a Kubernetes secret containing the **TLS certificate** (`tls.crt`) and **private key** (`tls.key`), and reference this secret in the `rbacConfig.secret.name` field.

```bash
kubectl create secret tls my-tls-secret --cert=path/to/cert.crt --key=path/to/cert.key -n kube-amd-gpu
```

For enabling mTLS, you must also create a ConfigMap containing the client CA:

```bash
kubectl create configmap my-client-ca --from-file=ca.crt=path/to/ca.crt -n kube-amd-gpu
```

## DeviceConfig Configuration Examples

Token-Based Authorization:
```yaml
metricsExporter:
  rbacConfig:
    enable: true  # Enable the kube-rbac-proxy sidecar
    image: "quay.io/brancz/kube-rbac-proxy:v0.18.1"  # Image for the kube-rbac-proxy sidecar container
    secret:
      name: "my-tls-secret"  # Secret containing the TLS certificate and key
    disableHttps: false  # Set to true if you want to disable HTTPS (not recommended)
```

mTLS with Certificate based RBAC Authorization:
```yaml
metricsExporter:
  rbacConfig:
    enable: true  # Enable the kube-rbac-proxy sidecar
    image: "quay.io/brancz/kube-rbac-proxy:v0.18.1"  # Image for the kube-rbac-proxy sidecar container
    secret:
      name: "my-tls-secret"  # Secret containing the TLS certificate and key
    clientCAConfigMap:
      name: "my-client-ca"   # ConfigMap containing the CA certificate that issued the client certificate
```

mTLS with Static Authorization:
```yaml
metricsExporter:
  rbacConfig:
    enable: true  # Enable the kube-rbac-proxy sidecar
    image: "quay.io/brancz/kube-rbac-proxy:v0.18.1"  # Image for the kube-rbac-proxy sidecar container
    secret:
      name: "my-tls-secret"  # Secret containing the TLS certificate and key
    clientCAConfigMap:
      name: "my-client-ca"   # ConfigMap containing the CA certificate that issued the client certificate
    staticAuthorization:
      enable: true  # Enable static authorization based on client certificate CN
      clientName: "prometheus-client"  # The exact CN value that must appear in the client certificate to grant access
```

## Accessing Metrics

For a complete guide on how to access the metrics securely (including the generation of tokens, certificates, applying RBAC roles, and accessing the metrics inside and outside the cluster), please refer to the example scenarios [README](https://github.com/rocm/gpu-operator/blob/main/example/) in the repository.

## Conclusion

Kube-rbac-proxy provides versatile options to secure your GPU metrics endpoints. You can choose from simple token-based authentication for easy integration, mutual TLS for stronger security, or static authorization for performance-critical scenarios. By following these steps, you will have a fully functional setup for accessing metrics from your AMD GPU cluster using the **Metrics Exporter** and **kube-rbac-proxy**.

For more detailed configuration guidance, refer to the example scenarios [README](https://github.com/rocm/gpu-operator/blob/main/example/).
