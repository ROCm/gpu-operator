FROM registry.access.redhat.com/ubi9/ubi-minimal:9.5

LABEL name="amd-gpu-operator-utils"
LABEL maintainer="sriram.ravishankar@amd.com,yan.sun3@amd.com"
LABEL vendor="Advanced Micro Devices, Inc."
LABEL version="v1.4.0"
LABEL release="v1.4.0"
LABEL summary="AMD GPU Operator Utility Image"
LABEL description="The AMD GPU Operator utils image is a utility image used by the AMD GPU Operator. The operator controller employs this image as the container's base layer to automate tasks on target worker nodes."

# Install nsenter from util-linux package
RUN microdnf install -y util-linux pciutils kmod tar jq && \
    cp /usr/bin/nsenter /nsenter && \
    microdnf clean all

ADD LICENSE /licenses/LICENSE

# Install kubectl and oc
RUN mkdir -p /oc && cd /oc && \
    curl -SsLO 'https://mirror.openshift.com/pub/openshift-v4/clients/ocp/latest/openshift-client-linux-amd64-rhel9.tar.gz' && \
    tar -xzf openshift-client-linux-amd64-rhel9.tar.gz -C /oc && \
    cp ./kubectl /usr/local/bin && chmod +x /usr/local/bin/kubectl && \
    cp ./oc /usr/local/bin && chmod +x /usr/local/bin/oc && \
    rm -rf /oc

# Set entrypoint to nsenter
ENTRYPOINT ["/nsenter"]
