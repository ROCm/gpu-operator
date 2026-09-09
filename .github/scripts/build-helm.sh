#!/usr/bin/env bash
source ~/.bashrc 2>/dev/null || true
set -euo pipefail

export GOPATH="${GOPATH:-/root/go}"
export PATH="${GOPATH}/bin:${PATH}"
git config --global --add safe.directory /gpu-operator

make generate manifests helm-k8s
