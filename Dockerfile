ARG GOLANG_BASE_IMG=golang:1.23

# Build the manager binary
FROM ${GOLANG_BASE_IMG} as builder

USER root

WORKDIR /opt/app-root/src

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

# Copy the License
COPY LICENSE LICENSE

# Copy the helm charts
COPY helm-charts-k8s helm-charts-k8s
COPY helm-charts-openshift helm-charts-openshift
# need to decompress nfd subchart for k8s chart, in preparation for copying out CRD
RUN cd helm-charts-k8s/charts && \
    tar -xvzf node-feature-discovery-chart-0.16.1.tgz

ARG TARGET

# Build
RUN git config --global --add safe.directory ${PWD} && make ${TARGET}

RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && \
    chmod +x ./kubectl

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.3

ARG TARGET

COPY --from=builder /opt/app-root/src/${TARGET} /usr/local/bin/manager
COPY --from=builder /opt/app-root/src/kubectl /usr/local/bin/kubectl
COPY --from=builder /opt/app-root/src/LICENSE /licenses/LICENSE
COPY --from=builder /opt/app-root/src/helm-charts-k8s/crds/deviceconfig-crd.yaml \
    /opt/app-root/src/helm-charts-k8s/charts/node-feature-discovery/crds/nfd-api-crds.yaml \
    /opt/app-root/src/helm-charts-k8s/charts/kmm/crds/module-crd.yaml \
    /opt/app-root/src/helm-charts-k8s/charts/kmm/crds/nodemodulesconfig-crd.yaml \
    /opt/helm-charts-crds-k8s/
COPY --from=builder /opt/app-root/src/helm-charts-openshift/crds/deviceconfig-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/nfd/crds/nodefeature-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/nfd/crds/nodefeaturediscovery-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/nfd/crds/nodefeaturerule-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/nfd/crds/noderesourcetopology-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/kmm/crds/module-crd.yaml \
    /opt/app-root/src/helm-charts-openshift/charts/kmm/crds/nodemodulesconfig-crd.yaml \
    /opt/helm-charts-crds-openshift/

RUN microdnf update -y && \
    microdnf install -y shadow-utils jq && \
    microdnf clean all

RUN ["groupadd", "--system", "-g", "201", "amd-gpu"]
RUN ["useradd", "--system", "-u", "201", "-g", "201", "-s", "/sbin/nologin", "amd-gpu"]

USER 201:201

ENTRYPOINT ["/usr/local/bin/manager"]
