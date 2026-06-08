#!/bin/bash
set -e

MODE="${K3S_MODE:-server}"
K3S_TOKEN="${K3S_TOKEN:-}"
K3S_IP="${K3S_IP:-}"
K3S_URL="${K3S_URL:-https://${K3S_IP}:6443}"
IN_CLUSTER_REGISTRY_PORT="${IN_CLUSTER_REGISTRY_PORT:-5000}"
AGENT_REGISTRIES_CONFIG="${AGENT_REGISTRIES_CONFIG:-}"

move_preloaded_images() {
  echo "[INFO] Move preloaded images to /var/lib/rancher/k3s/agent/images/"
  mkdir -p /var/lib/rancher/k3s/agent/images/
  if [ -d "/images" ] && [ -n "$(ls -A /images 2>/dev/null)" ]; then
    mv /images/* /var/lib/rancher/k3s/agent/images/
    echo "[INFO] Moved preloaded images successfully"
  else
    echo "[INFO] No preloaded images found in /images/, skipping"
  fi
}

start_k3s_server() {
  echo "[INFO] Starting k3s server"
  k3s server \
    --embedded-registry \
    --disable=traefik \
    --disable=servicelb \
    --kubelet-arg=serialize-image-pulls=true \
    ${K3S_EXTRA_ARGS} >> /var/log/k3s.log 2>&1 &
  K3S_PID=$!
}

start_k3s_agent() {
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
    --with-node-id \
    --kubelet-arg=serialize-image-pulls=true \
    ${K3S_EXTRA_ARGS} >> /var/log/k3s.log 2>&1 &
  K3S_PID=$!
}

# Supervisor loop. Re-launches k3s ($MODE) whenever it exits non-zero,
# replacing the previous "fire-and-forget + sleep infinity" pattern.
#
# Why this exists: k3s frequently crashes on the FIRST start after an OS
# reboot due to transient host state -- iptables-nft chains wiped (kube-
# router netpol panics on missing KUBE-ROUTER-OUTPUT), missing cni0
# device (flannel can't add IP), stale containerd socket file from prior
# boot, etc. A second/third attempt typically succeeds because the first
# attempt re-creates the chains/ifaces before crashing. Before this loop,
# such crashes left the container looking "Up" (entrypoint + sleep) while
# k3s was dead, requiring an operator to docker rm/run the container.
#
# Safety: a rolling cap (MAX_RESTARTS in WINDOW_SECS) prevents a hot loop
# when k3s is fundamentally misconfigured. When the cap is hit the script
# exits non-zero so `docker --restart=unless-stopped` (already set by
# gpu-cluster.sh) recreates the container fresh, clearing any per-PID
# state inside the container. The CVF setup-cluster.yml health probe
# will then detect the new container on its next idempotent re-run.
supervise_k3s() {
  local mode="$1"
  local restarts=0
  local window_start
  window_start=$(date +%s)
  local MAX_RESTARTS="${K3S_MAX_RESTARTS:-10}"
  local WINDOW_SECS="${K3S_RESTART_WINDOW:-600}"   # 10 min
  local BACKOFF_MIN=2
  local BACKOFF_MAX=30
  local backoff="$BACKOFF_MIN"

  # Forward SIGTERM/SIGINT to k3s so `docker stop` is graceful (node
  # drain) instead of getting SIGKILLed after the 10s grace period.
  # shellcheck disable=SC2064
  trap 'echo "[INFO] supervisor: forwarding signal to k3s pid=$K3S_PID"; \
        [ -n "$K3S_PID" ] && kill -TERM "$K3S_PID" 2>/dev/null; \
        wait "$K3S_PID" 2>/dev/null; exit 0' TERM INT

  while true; do
    case "$mode" in
      server) start_k3s_server ;;
      agent)  start_k3s_agent  ;;
    esac

    # `wait $pid` blocks until that child exits and returns its exit code.
    # `set -e` would abort the script if k3s exits non-zero -- we want to
    # observe the rc and decide, so disable -e around the wait.
    set +e
    wait "$K3S_PID"
    local rc=$?
    set -e
    local now
    now=$(date +%s)

    # Reset the rolling restart window if k3s ran longer than WINDOW_SECS
    # before crashing -- it was effectively stable, so this crash is
    # unrelated to the post-boot flapping we are protecting against.
    if [ $(( now - window_start )) -gt "$WINDOW_SECS" ]; then
      restarts=0
      window_start=$now
      backoff="$BACKOFF_MIN"
    fi

    restarts=$(( restarts + 1 ))
    echo "[WARN] supervisor: k3s $mode exited rc=$rc (restart $restarts/$MAX_RESTARTS in ${WINDOW_SECS}s window)"

    if [ "$restarts" -ge "$MAX_RESTARTS" ]; then
      echo "[ERROR] supervisor: $MAX_RESTARTS restarts within ${WINDOW_SECS}s -- giving up so docker recreates the container"
      exit 1
    fi

    echo "[INFO] supervisor: sleeping ${backoff}s before restart"
    sleep "$backoff"
    # Exponential backoff capped at BACKOFF_MAX so we don't grow unbounded.
    backoff=$(( backoff * 2 ))
    [ "$backoff" -gt "$BACKOFF_MAX" ] && backoff="$BACKOFF_MAX"
  done
}

move_preloaded_images
echo "[INFO] Starting k3s in $MODE mode (supervised)"

case "$MODE" in
  server|agent) supervise_k3s "$MODE" ;;
  *)
    echo "[ERROR] Unknown mode: $MODE. Use 'server' or 'agent'"
    exit 1
    ;;
esac
