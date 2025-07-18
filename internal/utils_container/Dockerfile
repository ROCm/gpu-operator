FROM registry.access.redhat.com/ubi9/ubi-minimal:9.5

# Install nsenter from util-linux package
RUN microdnf install -y util-linux pciutils kmod && \
    cp /usr/bin/nsenter /nsenter && \
    microdnf clean all

# Set entrypoint to nsenter
ENTRYPOINT ["/nsenter"]
