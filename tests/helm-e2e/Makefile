ifneq ("$(wildcard ../../dev.env)","")
    include ../../dev.env
endif

export GPU_OPERATOR_CHART

.DEFAULT: all
.PHONY: all lint
all:
	go test -test.timeout=360m -v
lint:
	@go fmt ./...
	@goimports -w ./
	@go vet ./...