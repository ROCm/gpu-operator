#!/usr/bin/env bash
source ~/.bashrc 2>/dev/null || true
set -euo pipefail

export GOPATH="${GOPATH:-/root/go}"
export PATH="${GOPATH}/bin:${PATH}"
git config --global --add safe.directory /gpu-operator

make generate manager manifests helm-k8s
make docker-build && make docker-save
make docker-build-utils && make docker-save-utils
make bundle-build && make bundle-save
