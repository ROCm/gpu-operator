# Metrics Exporter with TLS & Token Authentication

This example demonstrates how to securely expose the AMD GPU Metrics Exporter using kube-rbac-proxy with TLS enabled and access control enforced via Kubernetes Bearer Tokens. This setup includes both:

- Curl-based scraping (from inside and outside the cluster)
- Prometheus ServiceMonitor integration

## Prerequisites

- AMD GPU Operator is deployed.
- Prometheus Operator is deployed in your cluster in `monitoring` namespace (Optional, if Prometheus ServiceMonitor integration is being tested).

## 1. Generate TLS Certificates

**Note:** This step is optional if you already have certificates that you've generated previously or if you have certificates issued by a third party Certificate Authority (CA). In those cases, you can use your existing certificates instead of generating new self-signed ones.

Create a TLS private key and server certificate for the kube-rbac-proxy. We’ll use openssl to generate a self-signed CA and server cert:

```bash
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -subj "/CN=my-ca" -days 3650 -out ca.crt
```

Prometheus requires the certificates to include a Subject Alternative Name (SAN) field. We'll first create a san file to define the SAN extension, then generate the certificate.

```bash
# Create a SAN config file
cat <<EOF > san-server.cnf
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = my-metrics-service

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = my-metrics-service
EOF

openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -config san-server.cnf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extensions v3_req -extfile san-server.cnf
```

## 2. Create RBAC for Token-Based Access

We create a service account in the `metrics-reader` namespace and assign RBAC permissions. We specifically grant permissions to perform a  `GET` on the `/metrics` non-Resource URL:

```yaml
rules:
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
```

Bind the clusterrole to the default ServiceAccount in the metrics-reader namespace:

```yaml
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: metrics
subjects:
- kind: ServiceAccount
  name: default
  namespace: metrics-reader
```

To create the clusterrole and binding:

```bash
kubectl create namespace metrics-reader
kubectl apply -f clusterrole.yaml
kubectl apply -f clusterrolebinding.yaml
```

## 3. Generate ServiceAccount token

Get the token for the `default` service account in the metrics-reader namespace:

```bash
TOKEN=$(kubectl create token default -n metrics-reader --duration=24h | tr -d '\n')
```

## 4. Create Kubernetes Secrets and ConfigMaps

Server TLS Secret (for kube-rbac-proxy):

```bash
kubectl create secret tls server-metrics-tls --cert=server.crt --key=server.key -n kube-amd-gpu
```

Client Token Secret (for Prometheus, in GPU Operator namespace):

```bash
kubectl create secret generic prom-token --from-literal=token="$TOKEN" -n kube-amd-gpu
```

Client CA Certificate ConfigMap (for Prometheus, in GPU Operator namespace):

```bash
kubectl create configmap prom-server-ca --from-file=ca.crt=ca.crt -n kube-amd-gpu
```

## 5. Apply DeviceConfig

This DeviceConfig enables:

- kube-rbac-proxy serving https over TLS
- Automatic ServiceMonitor creation with token-based auth and TLS

kube-rbac-proxy config (part of DeviceConfig):

```yaml
rbacConfig:
  enable: true
  secret:
    name: gpu-metrics-tls
```

ServiceMonitor Config (part of DeviceConfig):

```yaml
prometheus:
      serviceMonitor:
        enable: true
        interval: 60s
        honorLabels: true
        labels:
          "example": "prom-token"
        attachMetadata:
          node: true
        tlsConfig:
          ca:
            configMap:
              key: ca.crt
              name: prom-server-ca
          serverName: my-metrics-service
          insecureSkipVerify: false
        authorization:
          type: Bearer
          credentials:
            key: token
            name: prom-token
```

Apply the `DeviceConfig`:

```bash
kubectl apply -f deviceconfig.yaml
```

## 6. Scraping the metrics

### Scraping using `curl`

Get the metrics endpoint IP and port. You can use either:

- For ClusterIP service: `kubectl get endpoints -n kube-amd-gpu` to find EndpointIP:ClusterPort
- For NodePort service: Use NodeIP:NodePort combination for the Node where the Endpoint is scheduled on

Use the token to make an authenticated request:

```bash
curl --cacert ./ca.crt -v -s -k -H "Authorization: Bearer $TOKEN" https://<ip:port>/metrics
```

### Scraping using Prometheus

To configure Prometheus to scrape the secured endpoint, you need to ensure it discovers the `ServiceMonitor` created by the GPU Operator.

First, find the name of your Prometheus resource in the `monitoring` namespace:

```bash
# List all Prometheus resources in the monitoring namespace
kubectl get prometheus -n monitoring

# The output will show something like:
# NAME                                  VERSION   DESIRED   READY   RECONCILED   AVAILABLE   AGE
# prometheus-operator-kube-p-prometheus v3.2.1    1         1       True         True        2d19h
```

Once you have the name, edit the Prometheus resource:

```bash
kubectl edit prometheus -n monitoring <your-prometheus-name>
```

Ensure the `spec` includes selectors that match the `ServiceMonitor` created by the `DeviceConfig`. The `DeviceConfig` in this example adds the label `example: prom-token` to the `ServiceMonitor`. Add or update the following selectors in the Prometheus `spec`:

```yaml
# prometheus spec:
spec:
  serviceMonitorNamespaceSelector: {}  # Select ServiceMonitors from all namespaces
                                       # Or restrict to 'kube-amd-gpu' if preferred:
                                       # matchLabels:
                                       #   kubernetes.io/metadata.name: kube-amd-gpu
  serviceMonitorSelector:              # Match the custom label added in DeviceConfig
    matchLabels:
      example: prom-token
```

**Note:** The `ServiceMonitor` created by the `DeviceConfig` already references the `prom-token` secret and `prom-server-ca` configmap located in the `kube-amd-gpu` namespace. Prometheus Operator automatically handles accessing these resources from the `kube-amd-gpu` namespace based on the `ServiceMonitor` definition. If you wish to create them in the Prometheus `monitoring` namespace instead, ensure you mount them in the Prometheus object, and refer to their mount locations (`/etc/prometheus/secrets/<secret-name>/<key>` and `/etc/prometheus/configmaps/<map-name>/<key>`) in the Prometheus Pod when configuring the TLS/Auth sections in the `ServiceMonitor` (use `bearerTokenFile` and `caFile/certFile/keyFile`). To mount them in Prometheus:

```yaml
spec:
  secrets:
  - prom-token
  configMaps:
  - prom-server-ca
```

After saving the changes, Prometheus Operator will reconfigure Prometheus. You should see the GPU metrics endpoint appear as a target in the Prometheus UI (`Status` -> `Targets`).

## Summary

This example walks through a secure token-based authentication setup where:

- kube-rbac-proxy secures metrics endpoints with TLS
- RBAC policies control access to metrics via Kubernetes service accounts
- Authentication uses Bearer tokens for authorization
- Both curl requests and Prometheus can securely scrape metrics
- A ServiceMonitor with proper TLS and token configuration enables automatic discovery

This provides a robust way to secure metrics endpoints while allowing authorized access to monitoring systems. The [mtls-rbac-auth](../mtls-rbac-auth/README.md) example will build on this and demonstrate how to simplify authentication with mTLS using certificates, removing the need for ServiceAccount tokens and TokenReview API access. The static authorization section will further demonstate how to simplify RBAC, removing the need for SubjectAccessReview API access entirely.
