FROM registry.access.redhat.com/ubi9/ubi:9.3

# Install nsenter from util-linux package
RUN dnf install -y util-linux && \
    cp /usr/bin/nsenter /nsenter && \
    dnf clean all

# Set entrypoint to nsenter
ENTRYPOINT ["/nsenter"]