# Metrics Exporter with kube-rbac-proxy

## Introduction

This repository provides a simple setup for running a metrics exporter in the AMD GPU cluster using kube-rbac-proxy. The kube-rbac-proxy acts as a reverse proxy, ensuring that the metrics endpoint is secured and adheres to the Kubernetes Role-Based Access Control (RBAC) policies.

In this guide, you will find step-by-step instructions on how to deploy and configure the metrics exporter alongside kube-rbac-proxy in your Kubernetes cluster. Follow the instructions below to get started with monitoring your applications securely.

## Getting Started

### 1. Deploy the AMD GPU Operator

First, deploy the AMD GPU Operator as mentioned in the project README.md.

### 2. Apply Device Config Example

Next, apply the `deviceconfig_example.yaml` provided in this example folder. You can edit fields such as `nodePort` and `clusterIPPort` to change the ports on which the metrics service is exposed externally and internally. For the `rbacConfig`, you can install certificates in the cluster in the GPU Operator Namespace and add the name in the secrets section of the `rbacConfig` if you want the kube-rbac-proxy container to use the certificates for TLS.

```bash
kubectl apply -f deviceconfig_example.yaml
```

### 3. Metrics-Reader Namespace

A `metrics-reader` namespace is required to isolate the resources related to reading metrics from the rest of the cluster. We assign a `ClusterRole` to this namespace and bind it to the default service account to grant the necessary permissions. The `ClusterRole` allows the service account to perform a `GET` request on the `/metrics` URL resource, enabling access to the metrics endpoint securely.

```bash
kubectl create namespace metrics-reader
kubectl apply -f client-rbac.yaml
```

### 4. Accessing Metrics Endpoint Inside the Cluster

To access the metrics endpoint from within the cluster, apply the `client.yaml` job provided. Edit the file to add the local service endpoint IP in the curl command. You can obtain the endpoint IP for the service with the following command:

```bash
kubectl get endpoints -n kube-amd-gpu test-device-config-metrics-exporter
```

Note down the IP in the ENDPOINTS. Once the client.yaml is updated, run:

```bash
kubectl apply -f client.yaml
kubectl logs job/amd-curl -n metrics-reader
```

This command will show you the metrics retrieved from the metrics exporter.

### 5. Pulling Metrics from Outside the Cluster

If you want to pull metrics from outside the cluster using the NodePort service, you can do so with the following steps:

#### 1. Create a token for the reader service account

```bash
kubectl create token -n metrics-reader default --duration=24h
```

#### 2. Save the generated token in a variable called TOKEN

```bash
TOKEN=<your-token-here>
```

#### 3. Use curl to access the metrics endpoint

```bash
curl -v -s -k -H "Authorization: Bearer $TOKEN" https://<node-ip>:<nodePort>/metrics
```

Replace `<node-ip>` with the IP address of the worker node where the metrics pod is deployed and `<nodePort>` with the NodePort you've configured.

## Conclusion

By following these steps, you will have a fully functional setup for accessing metrics from your AMD GPU cluster using the metrics exporter and kube-rbac-proxy. This setup ensures secure access to metrics while leveraging Kubernetes' RBAC capabilities.
