#!/usr/bin/env bash
set -euo pipefail
source ~/.bashrc 2>/dev/null || true
git config --global --add safe.directory /gpu-operator

make generate manifests helm-k8s
