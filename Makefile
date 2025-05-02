# Import development related environment variables from dev.env
ifneq ("$(wildcard dev.env)","")
    include dev.env
endif

# PROJECT_VERSION defines the project version.
# Update this value when you upgrade the version of your project.
PROJECT_VERSION ?= v1.2.0

####################################
# GPU Operator Image Build variables
# Note: when using images from DockerHub, please make sure to input the full DockerHub registry URL (docker.io) into DOCKER_REGISTRY
# user's container runtime may not set DockerHub as default registry and auto-search on DockerHub
GOFLAGS := "-mod=mod"
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
DOCKER_REGISTRY ?= docker.io/rocm
IMAGE_NAME ?= gpu-operator
IMAGE_TAG_BASE ?= $(DOCKER_REGISTRY)/$(IMAGE_NAME)
IMAGE_TAG ?= dev
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)
# name used for saving the container images as tar.gz
DOCKER_CONTAINER_IMG = $(IMAGE_NAME)-$(IMAGE_TAG)
HOURLY_TAG_LABEL ?= latest

# KMM related images
KMM_IMAGE_TAG ?= latest
KMM_SIGNER_IMG ?= $(DOCKER_REGISTRY)/kernel-module-management-signimage:$(KMM_IMAGE_TAG)
KMM_WORKER_IMG ?= $(DOCKER_REGISTRY)/kernel-module-management-worker:$(KMM_IMAGE_TAG)
KMM_BUILDER_IMG ?= gcr.io/kaniko-project/executor:v1.23.2
KMM_WEBHOOK_IMG_NAME ?= $(DOCKER_REGISTRY)/kernel-module-management-webhook-server
KMM_OPERATOR_IMG_NAME ?= $(DOCKER_REGISTRY)/kernel-module-management-operator

#######################
# Helm Charts variables
YAML_FILES=bundle/manifests/amd-gpu-operator-node-metrics_rbac.authorization.k8s.io_v1_rolebinding.yaml bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml bundle/manifests/amd-gpu-operator-node-labeller_rbac.authorization.k8s.io_v1_clusterrolebinding.yaml bundle/manifests/amd-gpu-operator-node-metrics_monitoring.coreos.com_v1_servicemonitor.yaml config/samples/amd.com_deviceconfigs.yaml config/manifests/bases/amd-gpu-operator.clusterserviceversion.yaml example/deviceconfig_example.yaml config/default/kustomization.yaml
CRD_YAML_FILES = deviceconfig-crd.yaml
K8S_KMM_CRD_YAML_FILES=module-crd.yaml nodemodulesconfig-crd.yaml
OPENSHIFT_KMM_CRD_YAML_FILES=module-crd.yaml nodemodulesconfig-crd.yaml
OPENSHIFT_CLUSTER_NFD_CRD_YAML_FILES=nodefeature-crd.yaml nodefeaturediscovery-crd.yaml nodefeaturerule-crd.yaml

ifdef OPENSHIFT
$(info selected openshift)
GPU_OPERATOR_CHART ?= ./helm-charts-openshift/gpu-operator-helm-openshift-$(PROJECT_VERSION).tgz
KUBECTL_CMD=oc
HELM_OC_CMD=--set platform=openshift
else
GPU_OPERATOR_CHART ?= ./helm-charts-k8s/gpu-operator-helm-k8s-$(PROJECT_VERSION).tgz
$(info selected k8s)
KUBECTL_CMD=kubectl
endif

ifdef SKIP_NFD
	ifdef OPENSHIFT
		SKIP_NFD_CMD=--set nfd.enabled=false
	else
		SKIP_NFD_CMD=--set node-feature-discovery.enabled=false
	endif
endif

ifdef SKIP_KMM
	SKIP_KMM_CMD=--set kmm.enabled=false
endif

ifdef SIM_ENABLE
	SIM_ENABLE_CMD=--set controllerManager.env.simEnable=true
endif

#################################
# OpenShift OLM Bundle varaiables
# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(PROJECT_VERSION)
INDEX_IMG := $(IMAGE_TAG_BASE)-index:$(PROJECT_VERSION)
BUNDLE_NAMESPACE ?= default # the namespace to deploy the OLM bundle 

# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
	BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
	BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(shell echo $(PROJECT_VERSION) | sed 's/^v//') $(BUNDLE_METADATA_OPTS)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

##################################
# Docker shell container variables
# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
DOCKER_GID := $(shell stat -c '%g' /var/run/docker.sock)
USER_UID := $(shell id -u)
USER_GID := $(shell id -g)
DOCKER_BUILDER_TAG := v1.1
DOCKER_BUILDER_IMAGE := $(DOCKER_REGISTRY)/gpu-operator-build:$(DOCKER_BUILDER_TAG)
CONTAINER_WORKDIR := /gpu-operator
BUILD_BASE_IMG ?= ubuntu:22.04
GOLANG_BASE_IMG ?= golang:1.23

##################
# Makefile targets

##@ QuickStart
.PHONY: default
default: docker-build-env ## Quick start to build everything from docker shell container.
	@echo "Starting a shell in the Docker build container..."
	@docker run --rm -it --privileged \
	    --network host \
		--name gpu-operator-build \
		-e "USER_NAME=$(shell whoami)" \
		-e "USER_UID=$(shell id -u)" \
		-e "USER_GID=$(shell id -g)" \
		-v $(CURDIR):/gpu-operator \
		-v $(CURDIR):/home/$(shell whoami)/go/src/github.com/ROCm/gpu-operator \
		-v $(HOME)/.ssh:/home/$(shell whoami)/.ssh \
		-w $(CONTAINER_WORKDIR) \
		$(DOCKER_BUILDER_IMAGE) \
		bash -c "source ~/.bashrc && cd /gpu-operator && git config --global --add safe.directory /gpu-operator && make all && GOFLAGS=-mod=mod go run tools/build/copyright/main.go && make fmt"

.PHONY: docker/shell
docker/shell: docker-build-env ## Bring up and attach to a container that has dev environment configured.
	@echo "Starting a shell in the Docker build container..."
	@docker run --rm -it --privileged \
		--name gpu-operator-build \
		-e "USER_NAME=$(shell whoami)" \
		-e "USER_UID=$(shell id -u)" \
		-e "USER_GID=$(shell id -g)" \
		-v $(CURDIR):/gpu-operator \
		-v $(CURDIR):/home/$(shell whoami)/go/src/github.com/ROCm/gpu-operator \
		-v $(HOME)/.ssh:/home/$(shell whoami)/.ssh \
		-w $(CONTAINER_WORKDIR) \
		$(DOCKER_BUILDER_IMAGE) \
		bash -c "cd /gpu-operator && git config --global --add safe.directory /gpu-operator && bash"

.PHONY: all
all: generate manager manifests helm-k8s helm-openshift bundle-build docker-build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9\/\-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: update-registry
update-registry:
	# updating registry information in yaml files
	sed -i -e 's|image:.*$$|image: ${IMG}|' bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml
	sed -i -e 's|repository:.*$$|repository: ${IMAGE_TAG_BASE}|' \
	hack/k8s-patch/metadata-patch/values.yaml \
	hack/openshift-patch/metadata-patch/values.yaml
	sed -i -e "s/newTag:.*$$/newTag: ${IMAGE_TAG}/" -e "s/tag:.*$$/tag: ${IMAGE_TAG}/" \
	-e 's|newName:.*$$|newName: ${IMAGE_TAG_BASE}|' \
	config/manager-base/kustomization.yaml config/manager/kustomization.yaml \
	hack/k8s-patch/metadata-patch/values.yaml helm-charts-k8s/values.yaml \
	hack/openshift-patch/metadata-patch/values.yaml helm-charts-openshift/values.yaml \
	example/deviceconfig_example.yaml
	sed -i -e 's|tag:.*$$|tag: ${KMM_IMAGE_TAG}|' \
	-e 's|repository:.*operator.*$$|repository: ${KMM_OPERATOR_IMG_NAME}|' \
	-e 's|repository:.*webhook.*$$|repository: ${KMM_WEBHOOK_IMG_NAME}|' \
	-e 's|relatedImageBuild:.*$$|relatedImageBuild: ${KMM_BUILDER_IMG}|' \
	-e 's|relatedImageSign:.*$$|relatedImageSign: ${KMM_SIGNER_IMG}|' \
	-e 's|relatedImageWorker:.*$$|relatedImageWorker: ${KMM_WORKER_IMG}|' \
	hack/k8s-patch/k8s-kmm-patch/metadata-patch/values.yaml

.PHONY: update-version
update-version:
	# updating project version in manifests
	sed -i -e 's|appVersion:.*$$|appVersion: "${PROJECT_VERSION}"|' hack/k8s-patch/metadata-patch/Chart.yaml
	sed -i '0,/version:/s|version:.*|version: ${PROJECT_VERSION}|' hack/k8s-patch/metadata-patch/Chart.yaml
	sed -i -e 's|appVersion:.*$$|appVersion: "${PROJECT_VERSION}"|' hack/openshift-patch/metadata-patch/Chart.yaml
	sed -i '0,/version:/s|version:.*|version: ${PROJECT_VERSION}|' hack/openshift-patch/metadata-patch/Chart.yaml

.PHONY: manifests
manifests: controller-gen update-registry update-version ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role paths="./internal/controllers" output:rbac:artifacts:config=config/rbac

.PHONY: generate
generate: controller-gen mockgen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	go generate ./...

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

UNIT_TEST ?= ./internal/controllers ./internal/kmmmodule ./internal

.PHONY: unit-test
unit-test: vet ## Run the unit tests.
	go test $(UNIT_TEST) -v -coverprofile cover.out

.PHONY: e2e
e2e: ## Run the e2e tests. Make sure you have ~/.kube/config configured for your test cluster.
	$(info deploying ${GPU_OPERATOR_CHART})
	${MAKE} helm-install
	export OPENSHIFT
	export SIM_ENABLE
	${MAKE} -C tests/e2e/nodeapp
	${MAKE} -C tests/e2e
	${MAKE} helm-uninstall

.PHONY: dcm_e2e
dcm_e2e:
	$(info deploying ${GPU_OPERATOR_CHART})
	${MAKE} helm-install
	export OPENSHIFT
	export SIM_ENABLE
	${MAKE} -C tests/e2e/nodeapp
	${MAKE} -C tests/e2e dcm_e2e
	${MAKE} helm-uninstall

GOFILES_NO_VENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code.
	@if [ `gofmt -l $(GOFILES_NO_VENDOR) | wc -l` -ne 0 ]; then \
		echo There are some malformed files, please make sure to run \'make fmt\'; \
		gofmt -l $(GOFILES_NO_VENDOR); \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run -v --timeout 5m0s

##@ Build

manager: $(shell find -name "*.go") go.mod go.sum  ## Build manager binary.
	go build -ldflags="-X main.Version=$(PROJECT_VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTag=$(HOURLY_TAG_LABEL)" -o $@ ./cmd

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	docker build -t $(IMG) --label HOURLY_TAG=$(HOURLY_TAG_LABEL) --build-arg TARGET=manager --build-arg GOLANG_BASE_IMG=$(GOLANG_BASE_IMG) .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push $(IMG)

.PHONY: docker-save
docker-save: ## Save the container image with the manager.
	docker save $(IMG) | gzip > $(DOCKER_CONTAINER_IMG).tar.gz

.PHONY: docker-build-env
docker-build-env: ## Build the docker shell container.
	@echo "Building the Docker environment..."
	@if [ -n $(INSECURE_REGISTRY) ]; then \
    docker build \
        -t $(DOCKER_BUILDER_IMAGE) \
        --build-arg BUILD_BASE_IMG=$(BUILD_BASE_IMG) \
        --build-arg INSECURE_REGISTRY=$(INSECURE_REGISTRY) \
        -f Dockerfile.build .; \
	else \
		docker build \
			-t $(DOCKER_BUILDER_IMAGE) \
			--build-arg BUILD_BASE_IMG=$(BUILD_BASE_IMG) \
			-f Dockerfile.build .; \
	fi


.PHONY: helm
helm:
	if [ -z ${OPENSHIFT} ]; then \
		$(MAKE) helm-k8s; \
	else \
		$(MAKE) helm-openshift; \
	fi

.PHONY: helm-k8s
helm-k8s: helmify manifests kustomize clean-helm-k8s gen-kmm-charts-k8s ## Build helm charts for Kubernetes.
	$(KUSTOMIZE) build config/default | $(HELMIFY) helm-charts-k8s
	# Patching k8s helm chart metadata
	cp $(shell pwd)/hack/k8s-patch/metadata-patch/*.yaml $(shell pwd)/helm-charts-k8s/
	# Patching k8s helm chart template
	cp $(shell pwd)/hack/k8s-patch/template-patch/* $(shell pwd)/helm-charts-k8s/templates/
	# Removing OpenShift related rbac from vanilla k8s helm charts
	rm $(shell pwd)/helm-charts-k8s/templates/kmm-device-plugin-rbac.yaml
	rm $(shell pwd)/helm-charts-k8s/templates/kmm-module-loader-rbac.yaml
	# Patching k8s helm chart kmm subchart
	cp $(shell pwd)/hack/k8s-patch/k8s-kmm-patch/metadata-patch/*.yaml $(shell pwd)/helm-charts-k8s/charts/kmm/
	cp $(shell pwd)/hack/k8s-patch/k8s-kmm-patch/template-patch/*.yaml $(shell pwd)/helm-charts-k8s/charts/kmm/templates/
	cd $(shell pwd)/helm-charts-k8s; helm dependency update; helm lint; cd ..;
	mkdir $(shell pwd)/helm-charts-k8s/crds
	echo "moving crd yaml files to crds folder"
	@for file in $(CRD_YAML_FILES); do \
		helm template amd-gpu helm-charts-k8s -s templates/$$file > $(shell pwd)/helm-charts-k8s/crds/$$file; \
	done
	rm $(shell pwd)/helm-charts-k8s/templates/*crd.yaml
	$(MAKE) helm-docs
	echo "dependency update, lint and pack charts"
	cd $(shell pwd)/helm-charts-k8s; helm dependency update; helm lint; cd ..; helm package helm-charts-k8s/ --destination ./helm-charts-k8s
	mv $(shell pwd)/helm-charts-k8s/gpu-operator-charts-$(PROJECT_VERSION).tgz $(shell pwd)/helm-charts-k8s/gpu-operator-helm-k8s-$(PROJECT_VERSION).tgz

.PHONY: helm-openshift
helm-openshift: helmify manifests kustomize clean-helm-openshift gen-nfd-charts-openshift gen-kmm-charts-openshift
	$(KUSTOMIZE) build config/default | $(HELMIFY) helm-charts-openshift
	# Patching openshift helm chart metadata
	cp $(shell pwd)/hack/openshift-patch/metadata-patch/*.yaml $(shell pwd)/helm-charts-openshift/
	# Patching openshift helm chart template
	cp $(shell pwd)/hack/openshift-patch/template-patch/*.yaml $(shell pwd)/helm-charts-openshift/templates/
	# Patching openshift helm chart nfd subchart
	cp $(shell pwd)/hack/openshift-patch/openshift-nfd-patch/crds/* $(shell pwd)/helm-charts-openshift/charts/nfd/crds/
	cp $(shell pwd)/hack/openshift-patch/openshift-nfd-patch/metadata-patch/* $(shell pwd)/helm-charts-openshift/charts/nfd/
	# Patching openshift helm chart kmm subchart
	cp $(shell pwd)/hack/openshift-patch/openshift-kmm-patch/template-patch/* $(shell pwd)/helm-charts-openshift/charts/kmm/templates/
	cp $(shell pwd)/hack/openshift-patch/openshift-kmm-patch/metadata-patch/*.yaml $(shell pwd)/helm-charts-openshift/charts/kmm/
	# opeartor already has device-plugin rbac yaml, removing the redundant rbac yaml from subchart
	rm $(shell pwd)/helm-charts-openshift/charts/kmm/templates/device-plugin-rbac.yaml
	# opeartor already has module-loader rbac yaml, removing the redundant rbac yaml from subchart
	rm $(shell pwd)/helm-charts-openshift/charts/kmm/templates/module-loader-rbac.yaml
	cd $(shell pwd)/helm-charts-openshift; helm dependency update; helm lint; cd ..;
	mkdir $(shell pwd)/helm-charts-openshift/crds
	echo "moving crd yaml files to crds folder"
	@for file in $(CRD_YAML_FILES); do \
		helm template amd-gpu helm-charts-openshift -s templates/$$file > $(shell pwd)/helm-charts-openshift/crds/$$file; \
	done
	rm $(shell pwd)/helm-charts-openshift/templates/*crd.yaml
	echo "dependency update, lint and pack charts"
	cd $(shell pwd)/helm-charts-openshift; helm dependency update; helm lint; cd ..; helm package helm-charts-openshift/ --destination ./helm-charts-openshift
	mv $(shell pwd)/helm-charts-openshift/gpu-operator-charts-$(PROJECT_VERSION).tgz $(shell pwd)/helm-charts-openshift/gpu-operator-helm-openshift-$(PROJECT_VERSION).tgz

.PHONY: bundle-build
bundle-build: operator-sdk manifests kustomize ## OpenShift Build OLM bundle.
	rm -fr ./bundle
	VERSION=$(shell echo $(PROJECT_VERSION) | sed 's/^v//') ${OPERATOR_SDK} generate kustomize manifests --apis-dir api
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd config/manager-base && $(KUSTOMIZE) edit set image controller=$(IMG)
	OPERATOR_SDK="${OPERATOR_SDK}" \
		     BUNDLE_GEN_FLAGS="${BUNDLE_GEN_FLAGS} --extra-service-accounts amd-gpu-operator-kmm-device-plugin,amd-gpu-operator-kmm-module-loader,amd-gpu-operator-node-labeller,amd-gpu-operator-metrics-exporter,amd-gpu-operator-metrics-exporter-rbac-proxy,amd-gpu-operator-test-runner,amd-gpu-operator-config-manager,amd-gpu-operator-utils-container" \
		     PKG=amd-gpu-operator \
		     SOURCE_DIR=$(dir $(realpath $(lastword $(MAKEFILE_LIST)))) \
		     KUBECTL_CMD=${KUBECTL_CMD} ./hack/generate-bundle
	${OPERATOR_SDK} bundle validate ./bundle
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

KUSTOMIZE_CONFIG_CRD ?= config/crd
.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	${KUBECTL_CMD} apply -k $(KUSTOMIZE_CONFIG_CRD)

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	${KUBECTL_CMD} delete -k $(KUSTOMIZE_CONFIG_CRD) --ignore-not-found=$(ignore-not-found)

KUSTOMIZE_CONFIG_DEFAULT ?= config/default
KUSTOMIZE_CONFIG_HUB_DEFAULT ?= config/default-hub

.PHONY: deploy
deploy: helm-install ## Deploy Helm Charts.

.PHONY: undeploy
undeploy: helm-uninstall ## Undeploy Helm Charts.

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
.PHONY: controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.0)

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
.PHONY: golangci-lint
golangci-lint:
	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63.4)

HELMDOCS = $(shell pwd)/bin/helm-docs
.PHONY: helm-docs
helm-docs:
	$(call go-get-tool,$(HELMDOCS),github.com/norwoodj/helm-docs/cmd/helm-docs@v1.12.0)
	$(HELMDOCS) -c $(shell pwd)/helm-charts-k8s/ -g $(shell pwd)/helm-charts-k8s -u --ignore-non-descriptions
	cat $(shell pwd)/README.md $(shell pwd)/helm-charts-k8s/README.md | sed 's/# gpu-operator-charts/\n## gpu-operator-charts/' > /tmp/README.md
	sed -i -e :a -e '/^\n*$$/{$$d;N;};/\n$$/ba' /tmp/README.md
	mv /tmp/README.md $(shell pwd)/helm-charts-k8s/README.md

.PHONY: mockgen
mockgen:
	go install go.uber.org/mock/mockgen@v0.3.0

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize:
	@if [ ! -f ${KUSTOMIZE} ]; then \
		BINDIR=$(shell pwd)/bin ./hack/download-kustomize; \
	fi

# go-get-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
    if [ -z "$$GOBIN" ]; then \
        GOBIN=$$(go env GOPATH)/bin; \
    else \
        GOBIN=$$GOBIN; \
    fi; \
    [ -f $(1) ] || { \
    set -e; \
    echo "Downloading $(2)"; \
    echo "Running: GOBIN=$(PROJECT_DIR)/bin go install $(2)"; \
    GOBIN=$(PROJECT_DIR)/bin go install $(2); \
    }
endef

OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
.PHONY: operator-sdk
operator-sdk:
	@if [ ! -f ${OPERATOR_SDK} ]; then \
		set -e ;\
		echo "Downloading ${OPERATOR_SDK}"; \
		mkdir -p $(dir ${OPERATOR_SDK}) ;\
		curl -Lo ${OPERATOR_SDK} 'https://github.com/operator-framework/operator-sdk/releases/download/v1.32.0/operator-sdk_linux_amd64'; \
		chmod +x ${OPERATOR_SDK}; \
	fi

.PHONY: bundle-push
bundle-push:
	docker push $(BUNDLE_IMG)

.PHONY: bundle-save
bundle-save:
	docker save $(BUNDLE_IMG) | gzip > $(IMAGE_NAME)-olm-bundle.tar.gz

.PHONY: bundle-scorecard-test
bundle-scorecard-test:
	${OPERATOR_SDK} scorecard --config bundle/tests/scorecard/config.yaml --kubeconfig ~/.kube/config $(BUNDLE_IMG)

.PHONY: bundle-deploy
bundle-deploy: ## OpenShift deploy OLM bundle.
	${OPERATOR_SDK} run bundle $(BUNDLE_IMG) --namespace=${BUNDLE_NAMESPACE}

.PHONY: bundle-deploy-upgrade
bundle-deploy-upgrade:
	${OPERATOR_SDK} run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-cleanup
bundle-cleanup: ## OpenShift undeploy OLM bundle.
	${OPERATOR_SDK} cleanup amd-gpu-operator --namespace=${BUNDLE_NAMESPACE}

.PHONY: opm
OPM = ./bin/opm
opm:
	@if [ ! -f ${OPM} ]; then \
                set -e ;\
                echo "Downloading ${OPM}"; \
                mkdir -p $(dir ${OPM}) ;\
                curl -Lo ${OPM}.tar 'https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/latest-4.8/opm-linux.tar.gz'; \
		tar -C $(dir ${OPM}) -xzf ${OPM}.tar; \
                chmod +x ${OPM}; \
		rm -f ${OPM}.tar; \
        fi

.PHONY: index
index: opm
	${OPM} index add --bundles ${BUNDLE_IMG} --tag ${INDEX_IMG}

HELMIFY = $(shell pwd)/bin/helmify
.PHONY: helmify
helmify:
	@if [ ! -f "$(shell pwd)/bin/helmify" ]; then \
		echo "helmify not found. Downloading..."; \
		curl -Lo $(shell pwd)/bin/helmify.tar.gz https://github.com/arttor/helmify/releases/download/v0.4.13/helmify_Linux_x86_64.tar.gz; \
		tar -xzf $(shell pwd)/bin/helmify.tar.gz -C $(shell pwd)/bin; \
		chmod +x $(shell pwd)/bin/helmify; \
		rm $(shell pwd)/bin/helmify.tar.gz; \
	else \
		echo "helmify already exists."; \
	fi

.PHONY: helm-install
helm-install: ## Deploy Helm Charts.
	if [ -z ${OPENSHIFT} ]; then \
		$(MAKE) helm-install-k8s; \
	else \
		$(MAKE) helm-install-openshift; \
	fi

.PHONY: helm-uninstall
helm-uninstall: ## Undeploy Helm Charts.
	if [ -z ${OPENSHIFT} ]; then \
		$(MAKE) helm-uninstall-k8s; \
	else \
		$(MAKE) helm-uninstall-openshift; \
	fi

helm-install-openshift:
	helm install amd-gpu-operator ${GPU_OPERATOR_CHART} -n kube-amd-gpu --create-namespace ${SKIP_NFD_CMD} ${SKIP_KMM_CMD} ${HELM_OC_CMD} ${SIM_ENABLE_CMD}

helm-uninstall-openshift:
	echo "Deleting all CRs before uninstalling operator..."
	${KUBECTL_CMD} delete deviceconfigs.amd.com -n kube-amd-gpu --all
	${KUBECTL_CMD} delete nodefeaturediscoveries.nfd.openshift.io -n kube-amd-gpu --all
	echo "Uninstalling operator..."
	helm uninstall amd-gpu-operator -n kube-amd-gpu

helm-install-k8s:
	helm install -f helm-charts-k8s/values.yaml amd-gpu-operator ${GPU_OPERATOR_CHART} -n kube-amd-gpu --create-namespace ${SKIP_NFD_CMD} ${SKIP_KMM_CMD} ${HELM_OC_CMD} ${SIM_ENABLE_CMD}

helm-uninstall-k8s:
	echo "Deleting all device configs before uninstalling operator..."
	${KUBECTL_CMD} delete deviceconfigs.amd.com -n kube-amd-gpu --all
	echo "Uninstalling operator..."
	helm uninstall amd-gpu-operator -n kube-amd-gpu

gen-nfd-charts-openshift:
	rm -rf /tmp/nfd && git clone https://github.com/openshift/cluster-nfd-operator /tmp/nfd; cd /tmp/nfd; git checkout release-4.16
	$(KUSTOMIZE) build /tmp/nfd/config/default | $(HELMIFY) helm-charts-openshift/charts/nfd
	cp $(shell pwd)/hack/openshift-patch/openshift-nfd-patch/metadata-patch/Chart.yaml $(shell pwd)/helm-charts-openshift/charts/nfd/
	mkdir helm-charts-openshift/charts/nfd/crds
	@for file in $(OPENSHIFT_CLUSTER_NFD_CRD_YAML_FILES); do \
		helm template amd-gpu helm-charts-openshift/charts/nfd -s templates/$$file > helm-charts-openshift/charts/nfd/crds/$$file; \
	done
	rm helm-charts-openshift/charts/nfd/templates/*crd.yaml
	rm -rf /tmp/nfd

gen-kmm-charts-openshift:
	rm -rf /tmp/kmm && git clone https://github.com/rh-ecosystem-edge/kernel-module-management.git /tmp/kmm; cd /tmp/kmm; git checkout release-2.3
	$(KUSTOMIZE) build /tmp/kmm/config/default | $(HELMIFY) helm-charts-openshift/charts/kmm
	cp $(shell pwd)/hack/openshift-patch/openshift-kmm-patch/metadata-patch/Chart.yaml $(shell pwd)/helm-charts-openshift/charts/kmm/
	mkdir helm-charts-openshift/charts/kmm/crds
	@for file in $(OPENSHIFT_KMM_CRD_YAML_FILES); do \
		helm template amd-gpu helm-charts-openshift/charts/kmm -s templates/$$file > helm-charts-openshift/charts/kmm/crds/$$file; \
		rm helm-charts-openshift/charts/kmm/templates/$$file; \
	done
	rm -rf /tmp/kmm

gen-kmm-charts-k8s:
ifdef JOB_ID
	@echo "Running in CI"
	$(KUSTOMIZE) build /ws/builder/kernel-module-management/config/default | $(HELMIFY) helm-charts-k8s/charts/kmm
else
	$(KUSTOMIZE) build $(shell pwd)/hack/kmmConfig/default | $(HELMIFY) helm-charts-k8s/charts/kmm
endif
	cp $(shell pwd)/hack/k8s-patch/k8s-kmm-patch/metadata-patch/Chart.yaml $(shell pwd)/helm-charts-k8s/charts/kmm/
	mkdir helm-charts-k8s/charts/kmm/crds
	@for file in $(K8S_KMM_CRD_YAML_FILES); do \
		helm template amd-gpu helm-charts-k8s/charts/kmm -s templates/$$file > helm-charts-k8s/charts/kmm/crds/$$file; \
		rm helm-charts-k8s/charts/kmm/templates/$$file; \
	done

cert-manager-install: ## Deploy cert-manager.
	helm repo add jetstack https://charts.jetstack.io --force-update
	helm install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --version v1.15.1 --set crds.enabled=true

cert-manager-uninstall: ## Undeploy cert-manager.
	helm uninstall cert-manager -n cert-manager
	${KUBECTL_CMD} delete crd issuers.cert-manager.io clusterissuers.cert-manager.io certificates.cert-manager.io certificaterequests.cert-manager.io orders.acme.cert-manager.io challenges.acme.cert-manager.io

clean-helm-openshift:
	rm -rf $(shell pwd)/helm-charts-openshift

clean-helm-k8s:
	rm -rf $(shell pwd)/helm-charts-k8s

copyrights:
	GOFLAGS=-mod=mod go run tools/build/copyright/main.go && ${MAKE} fmt && ./tools/build/check-local-files.sh
