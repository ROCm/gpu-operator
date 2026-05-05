.DEFAULT: all
.PHONY: all verify lint

TEST_ARGS :=
KUBECONFIG :=
OPERATOR_CHART ?= oci://registry-1.docker.io/rocm/gpu-operator-charts
OPERATOR_TAG   ?= v1.4.1
OPERATOR_NS    ?= kube-amd-gpu

ifdef KUBECONFIG
	TEST_ARGS += -kubeconfig=$(KUBECONFIG)
endif

# all: full install + verify + teardown (TestOp000–TestOp900)
all:
	go test -failfast \
	  -operatorchart $(OPERATOR_CHART) \
	  -operatortag $(OPERATOR_TAG) \
	  -namespace $(OPERATOR_NS) \
	  -test.timeout=60m \
	  -v $(TEST_ARGS)

# verify: verify DME on a PRE-DEPLOYED GPU Operator cluster (no install/teardown)
verify:
	go test -failfast -existing \
	  -namespace $(OPERATOR_NS) \
	  -check.f 'TestOp010|TestOp020|TestOp030|TestOp040|TestOp050|TestOp060|TestOp065|TestOp070' \
	  -test.timeout=30m \
	  -v $(TEST_ARGS)

lint:
	@go fmt ./...
	@go vet ./...
