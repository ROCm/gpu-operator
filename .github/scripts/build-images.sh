#!/usr/bin/env bash
set -euo pipefail
source ~/.bashrc 2>/dev/null || true
git config --global --add safe.directory /gpu-operator

make generate manager manifests
make docker-build && make docker-save
make docker-build-utils && make docker-save-utils
make bundle-build && make bundle-save
