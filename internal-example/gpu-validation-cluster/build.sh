#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-gpu-validation-k3s}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"

echo "[INFO] Building Docker image: $FULL_IMAGE"
docker build -t "$FULL_IMAGE" "$SCRIPT_DIR"
echo "[INFO] Build complete. Image: $FULL_IMAGE"
