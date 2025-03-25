ifneq ("$(wildcard ../../dev.env)","")
    include ../../dev.env
endif

E2E_INIT_CONTAINER_IMAGE ?= busybox:1.36
E2E_KUBE_RBAC_PROXY_CURL_IMAGE ?= curlimages/curl:7.78.0
E2E_UBUNTU_BASE_IMAGE ?= ubuntu:22.04
E2E_MINIO_IMAGE ?= minio/minio:latest
E2E_EXPORTER_IMAGE ?= rocm/device-metrics-exporter:v1.2.0
E2E_EXPORTER_IMAGE_2 ?= rocm/device-metrics-exporter:v1.1.1-beta.0
E2E_DEVICE_PLUGIN_IMAGE ?= rocm/k8s-device-plugin:latest
E2E_NODE_LABELLER_IMAGE ?= rocm/k8s-device-plugin:labeller-latest
E2E_DEVICE_PLUGIN_IMAGE_2 ?= rocm/k8s-device-plugin:1.31.0.6
E2E_NODE_LABELLER_IMAGE_2 ?= rocm/k8s-device-plugin:labeller-1.31.0.6
E2E_TEST_RUNNER_IMAGE ?= rocm/test-runner:v1.2.0-beta.0

export E2E_INIT_CONTAINER_IMAGE
export E2E_KUBE_RBAC_PROXY_CURL_IMAGE
export E2E_UBUNTU_BASE_IMAGE
export E2E_MINIO_IMAGE
export E2E_EXPORTER_IMAGE
export E2E_EXPORTER_IMAGE_2
export E2E_DEVICE_PLUGIN_IMAGE
export E2E_NODE_LABELLER_IMAGE
export E2E_DEVICE_PLUGIN_IMAGE_2
export E2E_NODE_LABELLER_IMAGE_2
export E2E_TEST_RUNNER_IMAGE

export E2E_DCM_IMAGE
export E2E_NODEAPP_IMG
export E2E_DRIVER_IMAGE_REPO

.DEFAULT: all
.PHONY: all lint

TEST_ARGS = ""
ifdef OPENSHIFT
	TEST_ARGS += "-openshift"
endif

ifdef SIM_ENABLE
	TEST_ARGS += "-simEnable"
endif

ifdef CI_ENV
	TEST_ARGS += "-ciEnv"
endif

all:
	go test -test.timeout=360m -v $(TEST_ARGS);
lint:
	@go fmt ./...
	@goimports -w ./
	@go vet ./...
dcm_e2e:
	go test -test.timeout=360m -check.f TestDCM.* -v -deviceConfigName test-deviceconfig