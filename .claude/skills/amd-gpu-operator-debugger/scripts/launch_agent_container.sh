#!/bin/bash
# Launch kubectl container in background for agent to use
# Agent wraps kubectl commands with: docker exec $CONTAINER kubectl ...

set -e

KUBECONFIG_PATH=${1:?Error: KUBECONFIG path required. Usage: $0 <kubeconfig-path> [namespace] [local|remote] [node]}
NS=${2:-kube-amd-gpu}
MODE=${3:-local}
TARGET_NODE=${4:-""}

# Normalize mode names
if [[ "$MODE" == "local-container" ]]; then
  MODE="local"
elif [[ "$MODE" == "remote-container" ]]; then
  MODE="remote"
fi

if [[ "$MODE" == "local" ]]; then
  # ========== LOCAL DOCKER CONTAINER (DETACHED) ==========
  CONTAINER_NAME="amd-gpu-debugger-$(date +%s)"

  echo "Launching kubectl container (detached)..."
  docker run -d --rm \
    --name "$CONTAINER_NAME" \
    -v "$KUBECONFIG_PATH:/root/.kube/config:ro" \
    -e KUBECONFIG=/root/.kube/config \
    -e NS="$NS" \
    bitnami/kubectl:latest \
    sleep 3600

  # Test kubectl works
  echo "Testing kubectl in container..."
  docker exec "$CONTAINER_NAME" kubectl cluster-info

  echo
  echo "=== Container Ready ==="
  echo "Container: $CONTAINER_NAME"
  echo "Namespace: $NS"
  echo
  echo "Wrap kubectl commands with:"
  echo "  docker exec $CONTAINER_NAME kubectl <args>"
  echo
  echo "Cleanup when done:"
  echo "  docker stop $CONTAINER_NAME"
  echo
  echo "$CONTAINER_NAME"  # Return container name

elif [[ "$MODE" == "remote" ]]; then
  # ========== REMOTE KUBERNETES POD ==========
  if [ -z "$TARGET_NODE" ]; then
    echo "Error: Remote mode requires node. Usage: $0 <kubeconfig> <ns> remote <node>"
    exit 1
  fi

  POD_NAME="amd-gpu-debugger-$TARGET_NODE"
  KUBECONFIG_HOST_PATH=$(realpath "$KUBECONFIG_PATH")

  kubectl --kubeconfig="$KUBECONFIG_PATH" delete pod $POD_NAME -n $NS --ignore-not-found

  cat <<EOF | kubectl --kubeconfig="$KUBECONFIG_PATH" apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: $POD_NAME
  namespace: $NS
spec:
  nodeName: $TARGET_NODE
  restartPolicy: Never
  containers:
  - name: kubectl
    image: bitnami/kubectl:latest
    command: ["sleep", "3600"]
    env:
    - name: KUBECONFIG
      value: /kubeconfig/config
    - name: NS
      value: $NS
    volumeMounts:
    - name: kubeconfig
      mountPath: /kubeconfig
      readOnly: true
  volumes:
  - name: kubeconfig
    hostPath:
      path: $KUBECONFIG_HOST_PATH
      type: File
EOF

  kubectl --kubeconfig="$KUBECONFIG_PATH" wait pod/$POD_NAME -n $NS --for=condition=Ready --timeout=60s

  # Test
  kubectl --kubeconfig="$KUBECONFIG_PATH" exec $POD_NAME -n $NS -- kubectl cluster-info

  echo
  echo "=== Remote Pod Ready ==="
  echo "Pod: $POD_NAME (on node $TARGET_NODE)"
  echo "Namespace: $NS"
  echo
  echo "Wrap kubectl commands with:"
  echo "  kubectl --kubeconfig=$KUBECONFIG_PATH exec $POD_NAME -n $NS -- kubectl <args>"
  echo
  echo "Cleanup:"
  echo "  kubectl --kubeconfig=$KUBECONFIG_PATH delete pod $POD_NAME -n $NS"
  echo
  echo "$POD_NAME"  # Return pod name

else
  echo "Error: Invalid mode. Use 'local' or 'remote'"
  exit 1
fi
