# Base image
ARG BASE_IMAGE=ubuntu:22.04
FROM ${BASE_IMAGE}

ARG K3S_VERSION=v1.35.0+k3s1
ARG MULTUS_CNI_VERSION=v4.2.2
ARG NFD_VERSION=v0.16.1

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
    jq \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-4 && \
    chmod 700 get_helm.sh && \
    ./get_helm.sh

# Install k3s and create symlinks for kubectl and crictl, prepare k3s directories
RUN curl -sfL https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s -o /usr/local/bin/k3s && \
    chmod +x /usr/local/bin/k3s && \
    ln -s /usr/local/bin/k3s /usr/local/bin/kubectl && \
    ln -s /usr/local/bin/k3s /usr/local/bin/crictl && \
    mkdir -p /var/lib/rancher/k3s/server && \
    mkdir -p /var/lib/rancher/k3s/agent

RUN mkdir -p /images && \
    skopeo copy docker://ghcr.io/k8snetworkplumbingwg/multus-cni:${MULTUS_CNI_VERSION} oci-archive:/images/multus-cni-${MULTUS_CNI_VERSION}.tar && \
    zstd -19 /images/multus-cni-${MULTUS_CNI_VERSION}.tar -o /images/multus-cni-${MULTUS_CNI_VERSION}.tar.zst && \
    rm /images/multus-cni-${MULTUS_CNI_VERSION}.tar && \
    skopeo copy docker://registry.k8s.io/nfd/node-feature-discovery:${NFD_VERSION} oci-archive:/images/nfd-${NFD_VERSION}.tar && \
    zstd -19 /images/nfd-${NFD_VERSION}.tar -o /images/nfd-${NFD_VERSION}.tar.zst && \
    rm /images/nfd-${NFD_VERSION}.tar

# Set kubeconfig environment variable
ENV KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Copy and configure entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Set container entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
