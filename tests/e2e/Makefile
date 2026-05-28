ifneq ("$(wildcard ../../dev.env)","")
    include ../../dev.env
endif

E2E_INIT_CONTAINER_IMAGE ?= busybox:1.36
E2E_ROCM_CONTAINER_IMAGE ?= rocm/rocm-terminal:latest
E2E_KUBE_RBAC_PROXY_CURL_IMAGE ?= curlimages/curl:7.78.0
E2E_UBUNTU_BASE_IMAGE ?= ubuntu:22.04
E2E_MINIO_IMAGE ?= minio/minio:latest
E2E_EXPORTER_MOCK_IMAGE ?= rocm/device-metrics-exporter:v1.4.0
E2E_EXPORTER_MOCK_IMAGE_2 ?= rocm/device-metrics-exporter:v1.3.1
E2E_EXPORTER_IMAGE ?= rocm/device-metrics-exporter:$(PROJECT_VERSION)
E2E_DEVICE_PLUGIN_IMAGE ?= rocm/k8s-device-plugin:latest
E2E_NODE_LABELLER_IMAGE ?= rocm/k8s-device-plugin:labeller-latest
E2E_DEVICE_PLUGIN_IMAGE_2 ?= rocm/k8s-device-plugin:1.31.0.6
E2E_NODE_LABELLER_IMAGE_2 ?= rocm/k8s-device-plugin:labeller-1.31.0.6
E2E_TEST_RUNNER_IMAGE ?= rocm/test-runner:v1.4.0
E2E_KUBEVIRT_DEVICE_PLUGIN_IMAGE ?= rocm/k8s-device-plugin:latest
E2E_KUBEVIRT_NODE_LABELLER_IMAGE ?= rocm/k8s-device-plugin:labeller-latest
E2E_UTILS_CONTAINER_IMAGE ?= docker.io/rocm/gpu-operator-utils:v1.4.0
E2E_ANR_CONFIGMAP_IMAGE ?= docker.io/rocm/amd-gpu-operator-remediation-config-utils:latest
E2E_NODE_DIAG_IMAGE ?= busybox:1.36
E2E_DRA_DRIVER_IMAGE ?= rocm/k8s-gpu-dra-driver:latest

export E2E_INIT_CONTAINER_IMAGE
export E2E_KUBE_RBAC_PROXY_CURL_IMAGE
export E2E_UBUNTU_BASE_IMAGE
export E2E_MINIO_IMAGE
export E2E_EXPORTER_MOCK_IMAGE
export E2E_EXPORTER_MOCK_IMAGE_2
export E2E_EXPORTER_IMAGE
export E2E_ROCM_CONTAINER_IMAGE
export E2E_DEVICE_PLUGIN_IMAGE
export E2E_NODE_LABELLER_IMAGE
export E2E_DEVICE_PLUGIN_IMAGE_2
export E2E_NODE_LABELLER_IMAGE_2
export E2E_TEST_RUNNER_IMAGE
export E2E_AGFHC_TEST_RUNNER_IMAGE
export E2E_KUBEVIRT_DEVICE_PLUGIN_IMAGE
export E2E_KUBEVIRT_NODE_LABELLER_IMAGE
export E2E_UTILS_CONTAINER_IMAGE
export E2E_ANR_CONFIGMAP_IMAGE
export E2E_NODE_DIAG_IMAGE
export E2E_DRA_DRIVER_IMAGE

export E2E_DCM_IMAGE
export E2E_NODEAPP_IMG
export E2E_DRIVER_IMAGE_REPO

.DEFAULT: all
.PHONY: all lint dcm_e2e agfhc smoke-advisory chunk-cleanup chunk-report

TEST_ARGS ?= ""
ifdef OPENSHIFT
	TEST_ARGS += "-openshift"
endif

ifdef SIM_ENABLE
	TEST_ARGS += "-simEnable"
endif

ifdef CI_ENV
	TEST_ARGS += "-ciEnv"
endif

# ---------------------------------------------------------------------------
# Chunked e2e suite — one chunk == one *_test.go file. The filesystem IS
# the catalog: chunks are auto-discovered from $(wildcard *_test.go) minus
# the SCAFFOLD list. Lexical sort dictates `make all` run order; files that
# need to run before others (driver-install) get a NN_ numeric prefix that
# sorts ahead of the alphabetic files (digits < letters).
#
# Add a chunk: drop <name>_test.go in this directory. Done.
# Force an ordering: rename to NN_<name>_test.go (and CHUNK_SUITE_ key if
# needed). Today only the driver-install suite needs explicit ordering.
#
# Timeout: hardcoded -test.timeout=45m, sized for the slowest chunk
# (cluster-core on cold-start CI). Override via TEST_ARGS.
#
# Spec: docs-internal/superpowers/specs/2026-05-16-e2e-sim-stability-design.md
# ---------------------------------------------------------------------------

SCAFFOLD    := e2e_test.go driver_install_suite_test.go cluster_helpers_test.go
CHUNK_FILES := $(sort $(filter-out $(SCAFFOLD),$(wildcard *_test.go)))
# Derive chunk name from filename: strip optional NN_ prefix, strip _test.go,
# convert remaining _ to -. e.g. 01_cluster_core_test.go -> cluster-core.
CHUNKS      := $(shell printf '%s\n' $(CHUNK_FILES) | sed -E 's/^[0-9]+_//; s/_test\.go$$//; s/_/-/g')

CHUNK_LOGDIR ?= chunk-logs

# Suite owner per chunk. Default = E2ESuite (resolved in recipe).
CHUNK_SUITE_cluster-core            := DriverInstallSuite
CHUNK_SUITE_cluster-upgrade-policy  := DriverInstallSuite

# .PHONY for chunk-* MUST come after CHUNKS := so the addprefix expands.
.PHONY: $(addprefix chunk-,$(CHUNKS))

all: smoke-advisory
	@mkdir -p $(CHUNK_LOGDIR)
	@FAILED=""; \
	for c in $(CHUNKS); do \
	  echo "==================== chunk: $$c ===================="; \
	  $(MAKE) chunk-cleanup; \
	  if ! $(MAKE) chunk-$$c; then FAILED="$$FAILED $$c"; fi; \
	done; \
	$(MAKE) chunk-report; \
	[ -z "$$FAILED" ]

# Per-chunk dispatch. File lookup: try <chunk>_test.go (unprefixed) first,
# fall back to NN_<chunk>_test.go (numerically-prefixed for ordering).
# Method names are grep'd from the resolved source file at recipe time and
# fed to gocheck via -check.f.
$(addprefix chunk-,$(CHUNKS)): chunk-%:
	@chunk_us=$$(echo "$*" | tr - _); \
	 src=$$(ls "$$chunk_us"_test.go [0-9]*_"$$chunk_us"_test.go 2>/dev/null | head -1); \
	 suite='$(or $(CHUNK_SUITE_$*),E2ESuite)'; \
	 log=$(CHUNK_LOGDIR)/$*.log; \
	 mkdir -p $(CHUNK_LOGDIR); \
	 if [ -z "$$src" ]; then echo "ERROR: no test file for chunk $*"; exit 2; fi; \
	 methods=$$(grep -oE 'func \(s \*'"$$suite"'\) Test[A-Za-z0-9_]+' "$$src" | awk '{print $$4}' | paste -sd '|' -); \
	 if [ -z "$$methods" ]; then echo "ERROR: no Test* methods on $$suite found in $$src"; exit 2; fi; \
	 echo "chunk=$* suite=$$suite file=$$src methods=$$methods"; \
	 go test -test.timeout=45m -v $(TEST_ARGS) -check.f "^$${suite}\\.($${methods})$$" > "$$log" 2>&1; \
	 rc=$$?; cat "$$log"; exit $$rc

# Inter-chunk cleanup (medium tier per design). Best-effort, never fails the loop.
chunk-cleanup:
	-kubectl delete deviceconfigs.amd.com -A --all --timeout=60s
	-kubectl delete remediationworkflowstatuses.amd.com -A --all --timeout=60s
	@stale_ds=$$(kubectl get ds -n kube-amd-gpu -o name 2>/dev/null | grep -E '^daemonset.apps/deviceconfig-' || true); \
	 if [ -n "$$stale_ds" ]; then \
	   echo "$$stale_ds" | xargs -r kubectl delete -n kube-amd-gpu --wait=true --timeout=60s || true; \
	 fi
	@stale_pods=$$(kubectl get pods -n kube-amd-gpu -o name 2>/dev/null | grep -E '^pod/(kmm-worker-|deviceconfig-)' || true); \
	 if [ -n "$$stale_pods" ]; then \
	   echo "$$stale_pods" | xargs -r kubectl delete -n kube-amd-gpu --grace-period=10 --timeout=60s || true; \
	 fi

smoke-advisory:
	-kubectl cluster-info
	-kubectl get ds -n node-feature-discovery
	-kubectl get pods -A | grep -E 'CrashLoopBackOff|Error' && \
	   echo 'WARN: unhealthy pods present, chunks may be slow' || true

chunk-report:
	@if [ -d $(CHUNK_LOGDIR) ] && ls $(CHUNK_LOGDIR)/*.log >/dev/null 2>&1; then \
	   python3 chunk_report.py $(CHUNK_LOGDIR) || true; \
	 else \
	   echo "(chunk-report: no logs in $(CHUNK_LOGDIR))"; \
	 fi

lint:
	@go fmt ./...
	@goimports -w ./
	@go vet ./...

dcm_e2e:
	go test -test.timeout=360m -check.f TestDCM.* -v -deviceConfigName test-deviceconfig -simEnable

agfhc:
	go test -test.timeout=360m -check.f TestTestRunner.* -v -framework "AGFHC"
