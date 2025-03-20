# Build the manager binary
FROM registry.access.redhat.com/ubi9/go-toolset:1.20 as builder

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Add the vendored dependencies
COPY vendor vendor

# Copy the go source
COPY api api
COPY cmd cmd
COPY internal internal

# Copy Makefile
COPY Makefile Makefile

# Copy the .git directory which is needed to store the build info
COPY .git .git

ARG TARGET

# Build
RUN git config --global --add safe.directory ${PWD} && make ${TARGET}

RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && \
    chmod +x ./kubectl

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.3

ARG TARGET

COPY --from=builder /opt/app-root/src/${TARGET} /usr/local/bin/manager
COPY --from=builder /opt/app-root/src/kubectl /usr/local/bin/kubectl

RUN microdnf update -y && \
    microdnf install -y shadow-utils && \
    microdnf clean all

RUN ["groupadd", "--system", "-g", "201", "amd-gpu"]
RUN ["useradd", "--system", "-u", "201", "-g", "201", "-s", "/sbin/nologin", "amd-gpu"]

USER 201:201

ENTRYPOINT ["/usr/local/bin/manager"]
