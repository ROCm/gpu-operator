FROM registry.access.redhat.com/ubi9/ubi-minimal:9.3

LABEL name="amd-gpu-operator-utils"
LABEL maintainer="sriram.ravishankar@amd.com,yan.sun3@amd.com"
LABEL vendor="Advanced Micro Devices, Inc."
LABEL version="v1.3.1"
LABEL release="v1.3.1"
LABEL summary="AMD GPU Operator Utility Image"
LABEL description="The AMD GPU Operator utils image is a utility image used by the AMD GPU Operator. The operator controller employs this image as the container's base layer to automate tasks on target worker nodes."

# Install nsenter from util-linux package
RUN microdnf install -y util-linux && \
    cp /usr/bin/nsenter /nsenter && \
    microdnf clean all

ADD LICENSE /licenses/LICENSE

# Set entrypoint to nsenter
ENTRYPOINT ["/nsenter"]
