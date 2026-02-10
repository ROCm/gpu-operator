# Base image
FROM ubuntu:22.04

# Install required system packages
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    ca-certificates \
    iptables \
    iproute2 \
    dnsmasq \
    skopeo \
    zstd \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-4 && \
    chmod 700 get_helm.sh && \
    ./get_helm.sh

# Install k3s and create symlinks for kubectl and crictl, prepare k3s directories
RUN curl -sfL https://github.com/k3s-io/k3s/releases/download/v1.35.0+k3s1/k3s -o /usr/local/bin/k3s && \
    chmod +x /usr/local/bin/k3s && \
    ln -s /usr/local/bin/k3s /usr/local/bin/kubectl && \
    ln -s /usr/local/bin/k3s /usr/local/bin/crictl && \
    mkdir -p /var/lib/rancher/k3s/server && \
    mkdir -p /var/lib/rancher/k3s/agent

RUN mkdir -p /images && \
    skopeo copy docker://ghcr.io/k8snetworkplumbingwg/multus-cni:v4.2.2 oci-archive:/images/multus-cni-v4.2.2.tar && \
    zstd -19 /images/multus-cni-v4.2.2.tar -o /images/multus-cni-v4.2.2.tar.zst && \
    rm /images/multus-cni-v4.2.2.tar && \
    skopeo copy docker://registry.k8s.io/nfd/node-feature-discovery:v0.16.1 oci-archive:/images/nfd-v0.16.1.tar && \
    zstd -19 /images/nfd-v0.16.1.tar -o /images/nfd-v0.16.1.tar.zst && \
    rm /images/nfd-v0.16.1.tar

# Set kubeconfig environment variable
ENV KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Copy and configure entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Set container entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
