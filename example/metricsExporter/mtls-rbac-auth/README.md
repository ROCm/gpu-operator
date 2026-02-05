# Metrics Exporter with Mutual TLS (mTLS) Authentication

This example demonstrates how to securely expose the AMD GPU Metrics Exporter using kube-rbac-proxy with mutual TLS (mTLS) authentication. In this mode, clients must present a valid TLS certificate signed by a trusted CA, and kube-rbac-proxy verifies the certificate and uses the Common Name (CN) for authorization. In the static authorization mode, kube-rbac-proxy authorizes the client using a static config and avoids initiating a SubjectAccessReview to the K8s API server.

This example supports:

- Curl-based access from within/outside the cluster
- Prometheus scraping using client certificate authentication

**Note**: This mode does not use Kubernetes tokens. Even if provided, Bearer tokens are ignored. Authentication and authorization rely entirely on the client certificate's CN and Kubernetes SubjectAccessReview (SAR)/Static Authorization.

## Prerequisites

- AMD GPU Operator is deployed.
- Prometheus Operator is deployed in your cluster in monitoring namespace (optional, if testing Prometheus integration).

## 1. Generate TLS Certificates for Server and Client

Create a Certificate Authority (CA), server certificate for kube-rbac-proxy, and a client certificate for Prometheus:

```bash
# Generate CA
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -subj "/CN=my-ca" -days 3650 -out ca.crt
```

Prometheus requires the server and client certificates to include a Subject Alternative Name (SAN) field. We'll first create a san file to define the SAN extension, then generate the certificate.

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

# Generate server cert for kube-rbac-proxy
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -config san-server.cnf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extensions v3_req -extfile san-server.cnf
```

Create the client certificate:

```bash
# Create SAN config file
cat <<EOF > san-client.cnf
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = prometheus-client

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = prometheus-client
EOF

# Generate client key and CSR
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr -config san-client.cnf

# Sign the client CSR with the CA, including SAN
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt -days 365 -sha256 \
  -extensions v3_req -extfile san-client.cnf
```

## 2. Create Kubernetes Secrets and ConfigMaps

Server TLS Secret (for kube-rbac-proxy):

```bash
kubectl create secret tls server-metrics-tls \
  --cert=server.crt --key=server.key -n kube-amd-gpu
```

Client CA ConfigMap (for kube-rbac-proxy to verify incoming client certs):

```bash
kubectl create configmap client-ca \
  --from-file=ca.crt=ca.crt -n kube-amd-gpu
```

Client TLS Secret (for Prometheus, in GPU Operator namespace):

```bash
kubectl create secret generic prom-client-cert \
  --from-file=client.crt=client.crt \
  --from-file=client.key=client.key -n kube-amd-gpu
```

Server CA ConfigMap (for Prometheus, in GPU Operator namespace):

```bash
kubectl create configmap prom-server-ca \
  --from-file=ca.crt=ca.crt -n kube-amd-gpu
```

## 3. Create RBAC for the Client Certificate CN

The CN from the client certificate must be authorized to access `/metrics`. We use Kubernetes RBAC to allow this by granting the `GET` permission to the CN extracted from the client cert.

**Note**: This section only applies for the rbac authorization using `SubjectAccessReview` requests made to the API Server. Ignore these configs for Static Authorization.

Create a ClusterRole and bind it to the CN prometheus-client:

```bash
kubectl apply -f clusterrole.yaml
kubectl apply -f clusterrolebinding.yaml
```

ClusterRole snippet to access metrics:

```yaml
rules:
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
```

ClusterRoleBinding binds the Role to the `prometheus-client` user, which is the CN in the client certificate.

```yaml
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: metrics
subjects:
- kind: User
  name: prometheus-client  # must match the CN in client certificate
```

## 4. Apply DeviceConfig

This config enables:

- kube-rbac-proxy with mTLS
- Automatic ServiceMonitor with client certificate authentication

kube-rbac-proxy config (part of DeviceConfig):

```yaml
rbacConfig:
  enable: true
  secret:
    name: server-metrics-tls
  clientCAConfigMap:
    name: client-ca
```

ServiceMonitor config (part of DeviceConfig):

```yaml
prometheus:
  serviceMonitor:
    enable: true
    interval: 60s
    honorLabels: true
    labels:
      "example": "prom-mtls"
    attachMetadata:
      node: true
    tlsConfig:
      # Prometheus Operator needs permissions to read secrets/configmaps across namespaces.
      # Ensure it has the necessary RBAC or configure it to allow cross-namespace access.
      ca:
        configMap:
          key: ca.crt
          name: prom-server-ca
      cert:
        secret:
          key: client.crt
          name: prom-client-cert
      keySecret:
        key: client.key
        name: prom-client-cert
      serverName: my-metrics-service # Must match the CN or SAN in the server certificate
      insecureSkipVerify: false
```

### Using mTLS with Static Authorization

This mode simplifies the authorization flow by bypassing all RBAC lookups to the Kubernetes API server. Instead of checking the client's identity via `SubjectAccessReview`, the kube-rbac-proxy directly compares the Common Name (CN) in the client certificate against a preconfigured value. Mutual TLS (mTLS) is still required, Prometheus must present valid client certificates. kube-rbac-proxy compares the client CommonName (CN) to a configured string (`clientName`) in the `staticAuthorization` section. If it matches, access is allowed.

To enable this mode, add the `staticAuthorization` block under `rbacConfig`. Prometheus config remains the same.

```yaml
metricsExporter:
  rbacConfig:
    enable: true
    secret:
      name: server-metrics-tls
    clientCAConfigMap:
      name: client-ca
    staticAuthorization:
      enable: true
      clientName: "prometheus-client"
```

Apply the DeviceConfig:

```bash
kubectl apply -f deviceconfig.yaml
```

## 5. Scraping the Metrics

### Scraping using `curl`

Get the metrics endpoint IP and port. You can use either:

- For ClusterIP service: `kubectl get endpoints -n kube-amd-gpu` to find EndpointIP:ClusterPort
- For NodePort service: Use NodeIP:NodePort combination for the Node where the Endpoint is scheduled on

Use the client certificate and key to make an authenticated request:

```bash
curl --cert ./client.crt --key ./client.key --cacert ./ca.crt -v -s -k -H "Accept: */*" https://<ip:port>/metrics
```

You should receive metrics if the client cert is valid and RBAC grants access (or static authorization matches).

### Scraping using Prometheus

To configure Prometheus to scrape the secured endpoint, you need to ensure it discovers the `ServiceMonitor` created by the GPU Operator. Configure the Prometheus spec:

```yaml
# prometheus spec:
spec:
  serviceMonitorNamespaceSelector: {}  # Select ServiceMonitors from all namespaces
                                       # Or restrict to 'kube-amd-gpu' if preferred:
                                       # matchLabels:
                                       #   kubernetes.io/metadata.name: kube-amd-gpu
  serviceMonitorSelector:              # Match the custom label added in DeviceConfig
    matchLabels:
      example: prom-mtls
```

Refer to the Prometheus section in the [token-based-auth](../token-based-auth/README.md) example to edit the Prometheus object. Once Prometheus Operator discovers the `ServiceMonitor` and has the required permissions, it will configure the underlying Prometheus instance to scrape the `/metrics` endpoint using the mTLS configuration specified in the `ServiceMonitor`'s `tlsConfig`. You should see the targets being discovered and scraped in the Prometheus UI.

## Summary

This example walks through a secure mTLS configuration where:

- kube-rbac-proxy verifies both server and client identities
- Prometheus authenticates with a client certificate
- RBAC policies grant access based on the certificate CN
- Tokens are not used or required in this setup. If static authentication is enabled, SAR requests to the API server are avoided too.
