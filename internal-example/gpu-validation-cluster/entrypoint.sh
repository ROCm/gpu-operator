#!/bin/bash
set -e

MODE="${K3S_MODE:-server}"
K3S_TOKEN="${K3S_TOKEN:-}"
K3S_IP="${K3S_IP:-}"
K3S_URL="${K3S_URL:-https://${K3S_IP}:6443}"
IN_CLUSTER_REGISTRY_PORT="${IN_CLUSTER_REGISTRY_PORT:-5000}"
AGENT_REGISTRIES_CONFIG="${AGENT_REGISTRIES_CONFIG:-}"

echo "[INFO] Move preloaded images to /var/lib/rancher/k3s/agent/images/"
mkdir -p /var/lib/rancher/k3s/agent/images/
mv /images/* /var/lib/rancher/k3s/agent/images/

echo "[INFO] Starting k3s in $MODE mode"

case "$MODE" in
  server)
    echo "[INFO] Starting k3s server"
    k3s server \
      --embedded-registry \
      --disable=traefik \
      --disable=servicelb \
      ${K3S_EXTRA_ARGS} > /var/log/k3s.log 2>&1 &
    ;;
  agent)
    if [ -z "$K3S_IP" ] || [ -z "$K3S_TOKEN" ]; then
      echo "[ERROR] K3S_IP and K3S_TOKEN must be set for agent mode"
      exit 1
    fi
    echo "[INFO] Starting k3s agent, joining server at $K3S_URL"
    
    # Create registries.yaml for in-cluster registry
    # do this for agent before starting the k3s agent process
    # at this point we already know the URL to in cluster registry service
    mkdir -p /etc/rancher/k3s
    cat > /etc/rancher/k3s/registries.yaml <<EOF
${AGENT_REGISTRIES_CONFIG}
EOF
    echo "[INFO] Created registries.yaml for registry at ${K3S_IP}:${IN_CLUSTER_REGISTRY_PORT}"
    
    k3s agent \
      --server="$K3S_URL" \
      --token="$K3S_TOKEN" \
      ${K3S_EXTRA_ARGS} > /var/log/k3s.log 2>&1 &
    ;;
  *)
    echo "[ERROR] Unknown mode: $MODE. Use 'server' or 'agent'"
    exit 1
    ;;
esac

# Keep container running
sleep infinity
