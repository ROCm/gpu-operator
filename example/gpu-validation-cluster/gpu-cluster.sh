#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
    echo "Usage: $0 <command> [args...]"
    echo ""
    echo "Commands:"
    echo "  build                          Build the Docker image"
    echo "  run <server|agent> [args...]   Run the node as server or agent"
    echo "  teardown                       Tear down the cluster and clean up"
    echo "  get-token                      Run on server node to print agent join token"
    echo "  status                         Show cluster validation framework status and recent runs"
    echo "  node-status                    Show validation status per node"
    echo "  help                           Show this help message"
    echo ""
    echo "Run arguments:"
    echo "  run server"
    echo "  run agent  <server-ip> <token>"
    echo ""
    echo "Environment variables:"
    echo "  IMAGE_NAME  (default: gpu-validation-cluster)        Docker image name"
    echo "  IMAGE_TAG   (default: latest)                    Docker image tag"
    echo "  BUILD_DIR   (default: \$SCRIPT_DIR/build)         Path to directory containing Dockerfile and entrypoint.sh"
    echo "  CONFIG_DIR  (default: \$SCRIPT_DIR/configs)        Path to directory containing config.json and other config files"
    echo "  CLEANUP_TEST_LOGS (default: false) Clean up cluster validation logs during teardown"
    echo ""
    echo "Examples:"
    echo "  # Build using default build directory"
    echo "  $0 build"
    echo ""
    echo "  # Build using a custom build directory"
    echo "  BUILD_DIR=/path/to/custom/build $0 build"
    echo ""
    echo "  # Run server node with custom config directory"
    echo "  CONFIG_DIR=/path/to/custom/configs $0 run server"
    echo ""
    echo "  # Run agent node with custom config directory to join a cluster"
    echo "  CONFIG_DIR=/path/to/custom/configs $0 run agent <server-ip> <token>"
    echo ""
    echo "  # Teardown with cluster validation logs cleanup enabled"
    echo "  CLEANUP_TEST_LOGS=true $0 teardown"
    echo ""
    echo "  # Show cluster validation framework status"
    echo "  $0 status"
    echo ""
    echo "  # Show per-node validation status"
    echo "  $0 node-status"
}

cmd_build() {
    local IMAGE_NAME="${IMAGE_NAME:-gpu-validation-cluster}"
    local IMAGE_TAG="${IMAGE_TAG:-latest}"
    local FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
    local BUILD_DIR="${BUILD_DIR:-$SCRIPT_DIR/build}"

    echo "[INFO] Building Docker image: $FULL_IMAGE"
    echo "[INFO] Build context: $BUILD_DIR"
    docker build -t "$FULL_IMAGE" "$BUILD_DIR"
    echo "[INFO] Build complete. Image: $FULL_IMAGE"
}

cmd_run() {
    IMAGE_NAME="${IMAGE_NAME:-gpu-validation-cluster}"
    IMAGE_TAG="${IMAGE_TAG:-latest}"
    FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
    CONFIG_DIR="${CONFIG_DIR:-$SCRIPT_DIR/configs}"
    
    # Check if image exists locally
    if ! docker image inspect "$FULL_IMAGE" &>/dev/null; then
        echo "[ERROR] Docker image not found locally: $FULL_IMAGE"
        echo "[INFO] You can either:"
        echo "[INFO]   - Build the image locally: $0 build"
        echo "[INFO]   - Load the image from another node: docker load -i <image-tar-file>"
        exit 1
    fi
    
    # Global variable for server node internal IP
    NODE_INTERNAL_IP=""
    # Global variable for secret name of the OS base image
    OS_BASE_IMAGE_SECRET_NAME=""

    # Load configuration
    CONFIG_FILE="$CONFIG_DIR/config.json"
    echo "[INFO] Loading configuration from $CONFIG_FILE"
    if [ ! -f "$CONFIG_FILE" ]; then
        echo "[ERROR] config.json not found at $CONFIG_FILE"
        exit 1
    fi

    # Parse config using jq (requires jq to be installed)
    read_config() {
        jq -r "$1" "$CONFIG_FILE"
    }

    MODE="${1:-server}"
    CONTAINER_NAME="${MODE}"

    if [ "$MODE" != "server" ] && [ "$MODE" != "agent" ]; then
        echo "[ERROR] Invalid mode: $MODE. Use 'server' or 'agent'"
        echo "Usage: $0 run server"
        echo "Usage: $0 run agent <server-ip> <token>"
        exit 1
    fi

    # Create isolated directories for script-owned state to avoid interfering with host
    local SCRIPT_STATE_DIR="/var/lib/gpu-validation-cluster"
    mkdir -p "$SCRIPT_STATE_DIR/rancher"
    mkdir -p "$SCRIPT_STATE_DIR/cni"
    mkdir -p "$SCRIPT_STATE_DIR/kubelet"
    mkdir -p "$SCRIPT_STATE_DIR/cni-bin"

    # Common docker run options
    DOCKER_OPTS=(
        "--privileged"
        "--net=host"
        "--cgroupns=host"
        "--security-opt=systempaths=unconfined"
        "--name" "$CONTAINER_NAME"
        "--restart" "unless-stopped"
        "-e" "K3S_MODE=$MODE"
        "-v" "$SCRIPT_STATE_DIR/rancher:/etc/rancher"
        "-v" "$SCRIPT_STATE_DIR/cni:/etc/cni"
        "-v" "$SCRIPT_STATE_DIR/rancher:/var/lib/rancher"
        "-v" "$SCRIPT_STATE_DIR/kubelet:/var/lib/kubelet"
        "-v" "$SCRIPT_STATE_DIR/cni-bin:/opt/cni/bin:shared"
        "-v" "/var/log:/var/log"
        "-v" "/var/run:/var/run"
        "-v" "/lib/modules:/lib/modules"
        "-v" "/sys:/sys"
        "-v" "/dev:/dev"
        "-v" "$CONFIG_DIR:/configs:ro"
    )

    # Add extra mounts from config.json
    EXTRA_MOUNTS=$(read_config '.global["extra-mounts"] // []')
    if [ -n "$EXTRA_MOUNTS" ] && [ "$EXTRA_MOUNTS" != "[]" ]; then
        MOUNT_COUNT=$(echo "$EXTRA_MOUNTS" | jq 'length')
        echo "[INFO] Adding $MOUNT_COUNT extra mount(s) from config.json"
        for ((i=0; i<MOUNT_COUNT; i++)); do
            HOST_PATH=$(echo "$EXTRA_MOUNTS" | jq -r ".[$i][\"hostPath\"]")
            MOUNT_PATH=$(echo "$EXTRA_MOUNTS" | jq -r ".[$i][\"mountPath\"]")
            if [ -n "$HOST_PATH" ] && [ "$HOST_PATH" != "null" ] && [ -n "$MOUNT_PATH" ] && [ "$MOUNT_PATH" != "null" ]; then
                echo "[INFO]   Mounting: $HOST_PATH -> $MOUNT_PATH"
                DOCKER_OPTS+=("-v" "$HOST_PATH:$MOUNT_PATH")
            fi
        done
    fi

    if [ "$MODE" = "agent" ]; then
        K3S_IP="${2:-}"
        K3S_TOKEN="${3:-}"

        if [ -z "$K3S_IP" ] || [ -z "$K3S_TOKEN" ]; then
            echo "[ERROR] For agent mode, K3S_IP and K3S_TOKEN must be provided"
            echo "Usage: $0 run agent <k3s-ip> <k3s-token>"
            exit 1
        fi

        IN_CLUSTER_REGISTRY_PORT=$(read_config '.["in-cluster-registry"].nodePort')

        # Build registries.yaml config for agent
        AGENT_REGISTRIES_CONFIG="
mirrors:
  \"${K3S_IP}:${IN_CLUSTER_REGISTRY_PORT}\":
    endpoint:
      - http://${K3S_IP}:${IN_CLUSTER_REGISTRY_PORT}
  \"*\":
configs:
  \"${K3S_IP}:${IN_CLUSTER_REGISTRY_PORT}\":
    tls:
      insecure_skip_verify: true"

        # Add auth sections for each image pull secret
        IMAGE_PULL_SECRETS=$(read_config '.global["image-pull-secrets"]')
        if [ -n "$IMAGE_PULL_SECRETS" ] && [ "$IMAGE_PULL_SECRETS" != "null" ]; then
            SECRET_COUNT=$(echo "$IMAGE_PULL_SECRETS" | jq 'length')
            for ((i=0; i<SECRET_COUNT; i++)); do
                REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-url\"]")
                USERNAME=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"username\"]")
                TOKEN=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"token\"]")

                if [ -n "$REGISTRY_URL" ] && [ -n "$USERNAME" ] && [ -n "$TOKEN" ]; then
                    AGENT_REGISTRIES_CONFIG="${AGENT_REGISTRIES_CONFIG}
  \"${REGISTRY_URL}\":
    auth:
      username: ${USERNAME}
      password: ${TOKEN}"
                fi
            done
        fi

        DOCKER_OPTS+=(
            "-e" "K3S_IP=$K3S_IP"
            "-e" "K3S_TOKEN=$K3S_TOKEN"
            "-e" "IN_CLUSTER_REGISTRY_PORT=$IN_CLUSTER_REGISTRY_PORT"
            "-e" "AGENT_REGISTRIES_CONFIG=$AGENT_REGISTRIES_CONFIG"
        )
        echo "[INFO] Starting k3s agent container: $CONTAINER_NAME"
        echo "[INFO]   Server IP: $K3S_IP"
    else
        echo "[INFO] Starting k3s server container: $CONTAINER_NAME"
    fi

    # Function to create docker-registry secrets for base images
    create_docker_registry_secrets() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        local AMD_GPU_NS=$(read_config '.["amd-gpu-operator"].namespace')
        local NETWORK_NS=$(read_config '.["network-operator"].namespace')
        local IMAGE_PULL_SECRETS=$(read_config '.global["image-pull-secrets"]')

        if [ -z "$IMAGE_PULL_SECRETS" ] || [ "$IMAGE_PULL_SECRETS" = "null" ]; then
            echo "[INFO] No image pull secrets configured"
            return
        fi

        local SECRET_COUNT=$(echo "$IMAGE_PULL_SECRETS" | jq 'length')
        for ((i=0; i<SECRET_COUNT; i++)); do
            local IS_BASE_IMAGE=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"isBaseImageSecret\"] // false")

            if [ "$IS_BASE_IMAGE" = true ]; then
                local REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-url\"]")
                local USERNAME=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"username\"]")
                local TOKEN=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"token\"]")
                local SECRET_NAME="base-image-secret"
                OS_BASE_IMAGE_SECRET_NAME=$SECRET_NAME

                if [ -n "$REGISTRY_URL" ] && [ -n "$USERNAME" ] && [ -n "$TOKEN" ]; then
                    # Create secret in AMD GPU namespace
                    echo "[INFO] Creating docker-registry secret: $SECRET_NAME in namespace: $AMD_GPU_NS"
                    echo "[INFO]   Registry: $REGISTRY_URL"

                    local KUBECTL_CMD="docker exec \"$CONTAINER_NAME\" kubectl create secret docker-registry \"$SECRET_NAME\" "
                    if [ "$REGISTRY_URL" != "docker.io" ]; then
                        KUBECTL_CMD="$KUBECTL_CMD--docker-server=\"$REGISTRY_URL\" "
                    fi
                    KUBECTL_CMD="$KUBECTL_CMD--docker-username=\"$USERNAME\" --docker-password=\"$TOKEN\" -n \"$AMD_GPU_NS\" --dry-run=client -o yaml"

                    eval "$KUBECTL_CMD" | docker exec -i "$CONTAINER_NAME" kubectl apply -f -

                    echo "[INFO] Secret $SECRET_NAME created successfully in $AMD_GPU_NS"

                    # Create secret in Network namespace
                    echo "[INFO] Creating docker-registry secret: $SECRET_NAME in namespace: $NETWORK_NS"

                    KUBECTL_CMD="docker exec \"$CONTAINER_NAME\" kubectl create secret docker-registry \"$SECRET_NAME\" "
                    if [ "$REGISTRY_URL" != "docker.io" ]; then
                        KUBECTL_CMD="$KUBECTL_CMD--docker-server=\"$REGISTRY_URL\" "
                    fi
                    KUBECTL_CMD="$KUBECTL_CMD--docker-username=\"$USERNAME\" --docker-password=\"$TOKEN\" -n \"$NETWORK_NS\" --dry-run=client -o yaml"

                    eval "$KUBECTL_CMD" | docker exec -i "$CONTAINER_NAME" kubectl apply -f -

                    echo "[INFO] Secret $SECRET_NAME created successfully in $NETWORK_NS"
                fi
                break
            fi
        done
    }

    # Function to setup in-cluster registry
    setup_in_cluster_registry() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        echo "[INFO] Setting up in-cluster registry..."
        local REGISTRY_IMAGE=$(read_config '.["in-cluster-registry"].image')
        local REGISTRY_NS=$(read_config '.["in-cluster-registry"].namespace')
        local REGISTRY_PORT=$(read_config '.["in-cluster-registry"].nodePort')

        echo "[INFO]   Image: $REGISTRY_IMAGE"
        echo "[INFO]   Namespace: $REGISTRY_NS"
        echo "[INFO]   NodePort: $REGISTRY_PORT"

        if docker exec "$CONTAINER_NAME" kubectl get deployment registry -n "$REGISTRY_NS" &>/dev/null; then
            echo "[INFO] in-cluster registry is already installed, skipping..."
            return
        fi

        docker exec "$CONTAINER_NAME" sh -c "kubectl get namespace \"$REGISTRY_NS\" >/dev/null 2>&1 || kubectl create namespace \"$REGISTRY_NS\""

        docker exec "$CONTAINER_NAME" sh -c "cat <<YAMEOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: registry-storage
  namespace: $REGISTRY_NS
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: local-path
  resources:
    requests:
      storage: 20Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry
  namespace: $REGISTRY_NS
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry
  template:
    metadata:
      labels:
        app: registry
    spec:
      restartPolicy: Always
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
      containers:
      - name: registry
        image: $REGISTRY_IMAGE
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 5000
        volumeMounts:
        - name: storage
          mountPath: /var/lib/registry
      volumes:
      - name: storage
        persistentVolumeClaim:
          claimName: registry-storage
---
apiVersion: v1
kind: Service
metadata:
  name: registry
  namespace: $REGISTRY_NS
spec:
  type: NodePort
  ports:
  - port: 5000
    targetPort: 5000
    nodePort: $REGISTRY_PORT
  selector:
    app: registry
YAMEOF
"

        echo "[INFO] Waiting for registry to be ready..."
        docker exec "$CONTAINER_NAME" kubectl rollout status deployment/registry -n "$REGISTRY_NS" --timeout=300s
        docker exec "$CONTAINER_NAME" kubectl wait --for=condition=ready pod -l app=registry -n "$REGISTRY_NS" --timeout=300s
        echo "[INFO] in-cluster registry setup completed"
    }

    # Function to install cert-manager
    install_cert_manager() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        local CERT_MANAGER_VERSION=$(read_config '.["cert-manager"].version')
        local CERT_MANAGER_REPO=$(read_config '.["cert-manager"].repo')
        local CERT_MANAGER_NS=$(read_config '.["cert-manager"].namespace')

        echo "[INFO] Installing cert-manager via Helm..."
        echo "[INFO]   Version: $CERT_MANAGER_VERSION"

        if docker exec "$CONTAINER_NAME" helm list -n "$CERT_MANAGER_NS" | grep -q cert-manager; then
            echo "[INFO] cert-manager is already installed, skipping..."
            return
        fi

        docker exec "$CONTAINER_NAME" helm install \
            cert-manager "$CERT_MANAGER_REPO" \
            --version "$CERT_MANAGER_VERSION" \
            --namespace "$CERT_MANAGER_NS" \
            --create-namespace \
            --set crds.enabled=true

        echo "[INFO] cert-manager installation completed"
    }

    # Function to install AMD GPU operator
    install_amd_gpu_operator() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        local AMD_GPU_VERSION=$(read_config '.["amd-gpu-operator"].version')
        local AMD_GPU_REPO=$(read_config '.["amd-gpu-operator"].repo')
        local AMD_GPU_CHART=$(read_config '.["amd-gpu-operator"].chart')
        local AMD_GPU_NS=$(read_config '.["amd-gpu-operator"].namespace')

        echo "[INFO] Installing AMD GPU operator via Helm..."
        echo "[INFO]   Version: $AMD_GPU_VERSION"

        if docker exec "$CONTAINER_NAME" helm list -n "$AMD_GPU_NS" | grep -q amd-gpu-operator; then
            echo "[INFO] AMD GPU operator is already installed, skipping..."
            return
        fi

        docker exec "$CONTAINER_NAME" helm repo add rocm "$AMD_GPU_REPO"
        docker exec "$CONTAINER_NAME" helm repo update

        docker exec "$CONTAINER_NAME" helm install amd-gpu-operator "$AMD_GPU_CHART" \
            --namespace "$AMD_GPU_NS" \
            --create-namespace \
            --version="$AMD_GPU_VERSION"

        echo "[INFO] AMD GPU operator installation completed"
    }

    # Function to install network operator
    install_network_operator() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        local NETWORK_VERSION=$(read_config '.["network-operator"].version')
        local NETWORK_REPO=$(read_config '.["network-operator"].repo')
        local NETWORK_CHART=$(read_config '.["network-operator"].chart')
        local NETWORK_NS=$(read_config '.["network-operator"].namespace')

        echo "[INFO] Installing network operator via Helm..."
        echo "[INFO]   Version: $NETWORK_VERSION"

        if docker exec "$CONTAINER_NAME" helm list -n "$NETWORK_NS" | grep -q amd-network-operator; then
            echo "[INFO] Network operator is already installed, skipping..."
            return
        fi

        docker exec "$CONTAINER_NAME" helm repo add rocm-network "$NETWORK_REPO"
        docker exec "$CONTAINER_NAME" helm repo update

        docker exec "$CONTAINER_NAME" helm install amd-network-operator "$NETWORK_CHART" \
            --namespace "$NETWORK_NS" \
            --create-namespace \
            --set kmm.enabled=false \
            --set node-feature-discovery.enabled=false \
            --version="$NETWORK_VERSION"

        echo "[INFO] Network operator installation completed"
    }

    # Function to configure registries.yaml
    configure_server_registries() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        echo "[INFO] Fetching node internal IP..."

        # Wait for at least one node to be available
        local MAX_RETRIES=30
        local RETRY_COUNT=0

        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            local ADDRESSES=$(docker exec "$CONTAINER_NAME" kubectl get node -o=jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)

            # Try to find IPv4 address first
            NODE_INTERNAL_IP=$(echo "$ADDRESSES" | grep -oE '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' | head -1)

            # Fall back to IPv6 if no IPv4 found
            if [ -z "$NODE_INTERNAL_IP" ]; then
                NODE_INTERNAL_IP=$(echo "$ADDRESSES" | grep -oE '([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}' | head -1)
            fi

            if [ -n "$NODE_INTERNAL_IP" ]; then
                break
            fi

            echo "[INFO] Waiting for nodes to be ready... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 2
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done

        if [ -z "$NODE_INTERNAL_IP" ]; then
            echo "[ERROR] Failed to fetch node internal IP after $MAX_RETRIES retries"
            exit 1
        fi
        echo "[INFO] Node internal IP: $NODE_INTERNAL_IP"

        local REGISTRY_PORT=$(read_config '.["in-cluster-registry"].nodePort')

        echo "[INFO] Configuring registries.yaml..."

        # Build the configs section with in-cluster registry
        local CONFIGS_SECTION="configs:
  \"${NODE_INTERNAL_IP}:${REGISTRY_PORT}\":
    tls:
      insecure_skip_verify: true"

        # Add auth sections for each image pull secret
        local IMAGE_PULL_SECRETS=$(read_config '.global["image-pull-secrets"]')
        if [ -n "$IMAGE_PULL_SECRETS" ] && [ "$IMAGE_PULL_SECRETS" != "null" ]; then
            local SECRET_COUNT=$(echo "$IMAGE_PULL_SECRETS" | jq 'length')
            for ((i=0; i<SECRET_COUNT; i++)); do
                local REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-url\"]")
                local USERNAME=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"username\"]")
                local TOKEN=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"token\"]")

                if [ -n "$REGISTRY_URL" ] && [ -n "$USERNAME" ] && [ -n "$TOKEN" ]; then
                    CONFIGS_SECTION="${CONFIGS_SECTION}
  \"${REGISTRY_URL}\":
    auth:
      username: ${USERNAME}
      password: ${TOKEN}"
                fi
            done
        fi

        docker exec "$CONTAINER_NAME" sh -c "cat > /etc/rancher/k3s/registries.yaml <<'REGEOF'
mirrors:
  \"${NODE_INTERNAL_IP}:${REGISTRY_PORT}\":
    endpoint:
      - http://${NODE_INTERNAL_IP}:${REGISTRY_PORT}
  \"*\":
${CONFIGS_SECTION}
REGEOF
"
        echo "[INFO] registries.yaml configured successfully, need to restart k3s server to apply changes"
        echo "[INFO] Killing k3s process to apply registry changes..."
        docker exec "$CONTAINER_NAME" sh -c "pkill -9 k3s || true"

        echo "[INFO] Restarting k3s service..."
        sleep 3
        docker exec -d "$CONTAINER_NAME" /usr/local/bin/k3s server --embedded-registry --disable=traefik --disable=servicelb

        echo "[INFO] Waiting for k3s to be ready..."
        sleep 10
        docker exec "$CONTAINER_NAME" sh -c "until kubectl get nodes &>/dev/null; do sleep 1; done"
        echo "[INFO] k3s restarted successfully"

        RETRY_COUNT=0
        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" kubectl api-resources &>/dev/null; then
                echo "[INFO] Cluster is ready"
                break
            fi
            echo "[INFO] Waiting for cluster API to be ready... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 2
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done

        if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
            echo "[ERROR] Cluster failed to become ready after $MAX_RETRIES retries"
            exit 1
        fi
    }

    # install driver if needed
    install_driver() {
        if [ "$MODE" != "server" ]; then
            return
        fi

        local AMD_GPU_NS=$(read_config '.["amd-gpu-operator"].namespace')
        local AMDGPU_DRIVER_VERSION=$(read_config '.["amd-gpu-operator"]["driver-version"]')
        local REGISTRY_PORT=$(read_config '.["in-cluster-registry"].nodePort')

        echo "[INFO] Installing AMD GPU driver..."
        echo "[INFO]   Version: $AMDGPU_DRIVER_VERSION"

        # Wait for DeviceConfig to be available
        local MAX_RETRIES=30
        local RETRY_COUNT=0

        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" kubectl get deviceconfig default -n "$AMD_GPU_NS" &>/dev/null; then
                break
            fi
            echo "[INFO] Waiting for DeviceConfig to be available... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 2
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done

        if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
            echo "[ERROR] DeviceConfig not found after $MAX_RETRIES retries"
            exit 1
        fi

        # Create docker-registry secrets for base images before patching DeviceConfig
        create_docker_registry_secrets

        # Patch the DeviceConfig with driver settings
        local INSTALL_AMDGPU_DRIVER=$(read_config '.["amd-gpu-operator"]["install-amdgpu-driver"]')
        local GPU_NODE_SELECTOR=$(read_config '.["amd-gpu-operator"]["node-selector"]')

        # Build imageRegistrySecret section if secret was created
        local IMAGE_REGISTRY_SECRET_SECTION=""
        if [ -n "$OS_BASE_IMAGE_SECRET_NAME" ]; then
            IMAGE_REGISTRY_SECRET_SECTION="
    imageRegistrySecret:
      name: $OS_BASE_IMAGE_SECRET_NAME"
        fi

        docker exec "$CONTAINER_NAME" sh -c "cat <<DEVICEEOF | kubectl apply -f -
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: default
  namespace: $AMD_GPU_NS
spec:
  driver:
    enable: $INSTALL_AMDGPU_DRIVER
    version: \"$AMDGPU_DRIVER_VERSION\"
    image: $NODE_INTERNAL_IP:$REGISTRY_PORT/amdgpu
    imageRegistryTLS:
      insecure: true
      insecureSkipTLSVerify: true${IMAGE_REGISTRY_SECRET_SECTION}
  selector:
    $(echo "$GPU_NODE_SELECTOR" | jq -c .)
DEVICEEOF
"
        echo "[INFO] AMD GPU driver configuration applied successfully"

        local INSTALL_IONIC_DRIVER=$(read_config '.["network-operator"]["install-ionic-driver"]')
        local NETWORK_NS=$(read_config '.["network-operator"].namespace')
        local REGISTRY_PORT=$(read_config '.["in-cluster-registry"].nodePort')
        local IONIC_VERSION=$(read_config '.["network-operator"]["driver-version"]')
        local NIC_NODE_SELECTOR=$(read_config '.["network-operator"]["node-selector"]')

        # Build imageRegistrySecret section if secret was created
        local NETWORK_IMAGE_REGISTRY_SECRET_SECTION=""
        if [ -n "$OS_BASE_IMAGE_SECRET_NAME" ]; then
            NETWORK_IMAGE_REGISTRY_SECRET_SECTION="
    imageRegistrySecret:
      name: $OS_BASE_IMAGE_SECRET_NAME"
        fi

        docker exec "$CONTAINER_NAME" sh -c "cat <<EOF | kubectl apply -f -
apiVersion: amd.com/v1alpha1
kind: NetworkConfig
metadata:
  name: default
  namespace: $NETWORK_NS
spec:
  driver:
    enable: $INSTALL_IONIC_DRIVER
    blacklist: true
    image: $NODE_INTERNAL_IP:$REGISTRY_PORT/amdnetwork
    imageRegistryTLS:
      insecure: true
      insecureSkipTLSVerify: true$NETWORK_IMAGE_REGISTRY_SECRET_SECTION
    version: \"$IONIC_VERSION\"
  devicePlugin:
    enableNodeLabeller: true
  metricsExporter:
    enable: true
    port: 5007
  selector:
    $(echo "$NIC_NODE_SELECTOR" | jq -c .)
EOF
"
    }

    # Function to configure CNI folder for all nodes
    configure_cni_folder() {
        echo "[INFO] Configuring CNI for node..."
        # wait for k3s CNI configuration to be available
        local MAX_RETRIES=30
        local RETRY_COUNT=0
        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" sh -c '[ "$(ls -A /var/lib/rancher/k3s/agent/etc/cni/net.d)" ]'; then
                break
            fi
            echo "[INFO] Waiting for k3s CNI configuration to be available... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 3
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done
        if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
            echo "[ERROR] k3s CNI configuration not found after $MAX_RETRIES retries"
            exit 1
        fi
        docker exec "$CONTAINER_NAME" sh -c 'mkdir -p /etc/cni && cp -r /var/lib/rancher/k3s/agent/etc/cni/net.d /etc/cni/net.d'
        echo "[INFO] CNI configuration completed"
    }

    prepare_multus_artifacts() {
        echo "[INFO] Preparing Multus CNI artifacts..."
        # wait for CNI binaries to be available
        # it could take time to pull the multus image from GitHub container registry
        local MAX_RETRIES=70
        local RETRY_COUNT=0
        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" sh -c '[ "$(ls -A /opt/cni/bin)" ]'; then
                break
            fi
            echo "[INFO] Waiting for CNI binaries to be available... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 3
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done
        if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
            echo "[ERROR] CNI binaries not found after $MAX_RETRIES retries"
            exit 1
        fi

        local CP_MAX_RETRIES=30
        local CP_RETRY_COUNT=0
        while [ $CP_RETRY_COUNT -lt $CP_MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" sh -c 'cp /opt/cni/bin/* /var/lib/rancher/k3s/data/cni/'; then
                echo "[INFO] CNI binaries copied successfully"
                break
            fi
            echo "[WARN] Failed to copy CNI binaries, retrying... ($((CP_RETRY_COUNT + 1))/$CP_MAX_RETRIES)"
            sleep 3
            CP_RETRY_COUNT=$((CP_RETRY_COUNT + 1))
        done
        if [ $CP_RETRY_COUNT -ge $CP_MAX_RETRIES ]; then
            echo "[ERROR] Failed to copy CNI binaries after $CP_MAX_RETRIES retries"
            exit 1
        fi

        # wait for 00-multus.conflist to be available
        RETRY_COUNT=0
        while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
            if docker exec "$CONTAINER_NAME" sh -c '[ "$(ls -A /etc/cni/net.d/00-multus.conflist)" ]'; then
                break
            fi
            echo "[INFO] Waiting for Multus CNI configuration to be available... ($((RETRY_COUNT + 1))/$MAX_RETRIES)"
            sleep 2
            RETRY_COUNT=$((RETRY_COUNT + 1))
        done
        if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
            echo "[ERROR] Multus CNI configuration not found after $MAX_RETRIES retries"
            exit 1
        fi
        docker exec "$CONTAINER_NAME" cp /etc/cni/net.d/00-multus.conflist /etc/cni/net.d/00-multus.conf
    }

    install_cluster_validation_framework() {
        local INSTALL_CVF=$(read_config '.["cluster-validation-framework"]["install-cvf"]')
        if [ "$MODE" != "server" ] || [ "$INSTALL_CVF" != "true" ]; then
            return
        fi

        echo "[INFO] Installing Cluster Validation Framework..."

        # Read configuration values with defaults
        local CRONJOB_SCHEDULE=$(read_config '.["cluster-validation-framework"].cronjob.schedule // "*/10 * * * *"')
        local WORKER_REPLICAS=$(read_config '.["cluster-validation-framework"].resources["worker-replicas"] // 2')
        local LAUNCHER_REPLICAS=$(read_config '.["cluster-validation-framework"].resources["launcher-replicas"] // 1')
        local SLOTS_PER_WORKER=$(read_config '.["cluster-validation-framework"].resources["slots-per-worker"] // 8')
        local GPU_PER_WORKER=$(read_config '.["cluster-validation-framework"].resources["gpu-per-worker"] // 8')
        local PF_NIC_PER_WORKER=$(read_config '.["cluster-validation-framework"].resources["pf-nic-per-worker"] // 0')
        local VF_NIC_PER_WORKER=$(read_config '.["cluster-validation-framework"].resources["vf-nic-per-worker"] // 8')
        local NODE_VALIDATION_INTERVAL=$(read_config '.["cluster-validation-framework"].resources["node-validation-interval-mins"] // 10')
        local SKIP_GPU_VALIDATION=$(read_config '.["cluster-validation-framework"]["skip-tests"]["skip-gpu-validation"] // false')
        local SKIP_RCCL_TEST=$(read_config '.["cluster-validation-framework"]["skip-tests"]["skip-rccl-test"] // false')

        # Read node selector labels with default values
        local NODE_SELECTOR_LABELS=$(read_config '.["cluster-validation-framework"]["node-selector-labels"] // ["feature.node.kubernetes.io/amd-gpu=true", "feature.node.kubernetes.io/amd-nic=true"]')
        # Convert JSON array to YAML list format with 4 spaces indentation (to match the placeholder indentation)
        local NODE_SELECTOR_LABELS_YAML=$(echo "$NODE_SELECTOR_LABELS" | jq -r '.[] | "    - " + .')

        echo "[INFO]   CronJob Schedule: $CRONJOB_SCHEDULE"
        echo "[INFO]   Node Selector Labels: $(echo "$NODE_SELECTOR_LABELS" | jq -r 'join(", ")')"
        echo "[INFO]   Resources - Workers: $WORKER_REPLICAS, GPUs/Worker: $GPU_PER_WORKER"
        echo "[INFO]   Skip GPU Validation: $SKIP_GPU_VALIDATION, Skip RCCL Test: $SKIP_RCCL_TEST"

        # Install MPI Operator
        echo "[INFO] Installing MPI Operator..."
        local MPI_OPERATOR_VERSION=$(read_config '.["cluster-validation-framework"]["mpi-operator"].version')
        docker exec "$CONTAINER_NAME" kubectl apply --server-side --force-conflicts -f https://raw.githubusercontent.com/kubeflow/mpi-operator/$MPI_OPERATOR_VERSION/deploy/v2beta1/mpi-operator.yaml
        echo "[INFO] MPI Operator installation completed"

        # Apply cluster-validation-config.yaml with substitutions
        echo "[INFO] Applying Cluster Validation ConfigMap..."
        # Do substitutions using a bash while-loop to properly handle multiline content
        docker exec "$CONTAINER_NAME" cat /configs/cluster-validation-config.yaml | \
            while IFS= read -r line; do
                line="${line//__WORKER_REPLICAS__/$WORKER_REPLICAS}"
                line="${line//__LAUNCHER_REPLICAS__/$LAUNCHER_REPLICAS}"
                line="${line//__SLOTS_PER_WORKER__/$SLOTS_PER_WORKER}"
                line="${line//__GPU_PER_WORKER__/$GPU_PER_WORKER}"
                line="${line//__PF_NIC_PER_WORKER__/$PF_NIC_PER_WORKER}"
                line="${line//__VF_NIC_PER_WORKER__/$VF_NIC_PER_WORKER}"
                line="${line//__NODE_VALIDATION_INTERVAL_MINS__/$NODE_VALIDATION_INTERVAL}"
                line="${line//__SKIP_GPU_VALIDATION__/$SKIP_GPU_VALIDATION}"
                line="${line//__SKIP_RCCL_TEST__/$SKIP_RCCL_TEST}"
                # Handle multiline NODE_SELECTOR_LABELS replacement
                if [[ "$line" == *"__NODE_SELECTOR_LABELS__"* ]]; then
                    echo "$NODE_SELECTOR_LABELS_YAML"
                else
                    echo "$line"
                fi
            done | docker exec -i "$CONTAINER_NAME" kubectl apply -f -

        # Apply cluster-validation-job.yaml with substitutions
        echo "[INFO] Applying Cluster Validation CronJob..."
        docker exec "$CONTAINER_NAME" sh -c "cat /configs/cluster-validation-job.yaml | \
            sed 's|__CRONJOB_SCHEDULE__|'\"${CRONJOB_SCHEDULE}\"'|g' | \
            kubectl apply -f -"

        echo "[INFO] Cluster Validation Framework installation completed"
    }

    # Print sanitized command without exposing sensitive information
    if [ "$MODE" = "agent" ]; then
        echo "[INFO] Starting k3s agent container with masked credentials..."
        echo "[INFO]   Container: $CONTAINER_NAME"
        echo "[INFO]   Server IP: $K3S_IP"
        echo "[INFO]   Token: [MASKED]"
        echo "[INFO]   Registry Config: [MASKED]"
    else
        echo "[INFO] Starting k3s server container: $CONTAINER_NAME"
    fi

    # Check if container already exists
    if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        echo "[WARN] Container '$CONTAINER_NAME' already exists. Removing it first..."
        docker rm -f "$CONTAINER_NAME"
    fi

    docker run "${DOCKER_OPTS[@]}" "$FULL_IMAGE" &
    CONTAINER_PID=$!

    echo "[INFO] Waiting for k3s to be ready..."
    sleep 10

    configure_cni_folder

    if [ "$MODE" = "server" ]; then
        configure_server_registries

        setup_in_cluster_registry

        install_cert_manager

        install_amd_gpu_operator

        install_network_operator
    fi

    prepare_multus_artifacts

    if [ "$MODE" = "server" ]; then
        install_driver

        install_cluster_validation_framework
    fi

    echo "[INFO] Node Bringup completed successfully"
    echo "[INFO] Container is running with restart policy 'unless-stopped'"
    echo "[INFO] Container will automatically restart after system reboots or Docker daemon restarts"
    echo "[INFO] You can:"
    echo "[INFO]   - Login to container: docker exec -it $CONTAINER_NAME bash"
    echo "[INFO]   - Check container logs: docker logs -f $CONTAINER_NAME"
    echo "[INFO]   - Check status: $0 status"
    echo "[INFO]   - View node status: $0 node-status"
    echo "[INFO]"
    echo "[INFO] Keeping script running... Press Ctrl+C to exit (container will continue running)"
    wait $CONTAINER_PID
}

cmd_teardown() {
    echo "Starting GPU Validation Cluster teardown..."

    local IMAGE_NAME="${IMAGE_NAME:-gpu-validation-cluster}"
    local IMAGE_TAG="${IMAGE_TAG:-latest}"
    local FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
    local CLEANUP_TEST_LOGS="${CLEANUP_TEST_LOGS:-false}"

    # Stop and remove named containers (server/agent)
    echo "Stopping and removing server/agent containers..."
    for CONTAINER in server agent; do
        if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
            echo "  Stopping container: $CONTAINER"
            docker stop "$CONTAINER" 2>/dev/null || true
            echo "  Removing container: $CONTAINER"
            docker rm "$CONTAINER" 2>/dev/null || true
        fi
    done

    # Remove any other containers using the gpu-validation-cluster image
    echo "Removing any other containers from image: $FULL_IMAGE"
    docker ps -a --filter "ancestor=$FULL_IMAGE" --format '{{.ID}}' | xargs -r docker rm -f 2>/dev/null || true

    # Clean up script-owned state directory (includes rancher, cni, kubelet, and cni-bin)
    local SCRIPT_STATE_DIR="/var/lib/gpu-validation-cluster"
    echo "Cleaning up script-owned state directory: $SCRIPT_STATE_DIR"
    if [ -d "$SCRIPT_STATE_DIR" ]; then
        sudo umount -R -f "$SCRIPT_STATE_DIR" 2>/dev/null || true
        sudo rm -rf "$SCRIPT_STATE_DIR"
        echo "Removed $SCRIPT_STATE_DIR"
    fi

    rm -f /var/log/k3s.log
    echo "Removed /var/log/k3s.log"

    # Clean up cluster validation logs if enabled
    if [ "$CLEANUP_TEST_LOGS" = "true" ]; then
        echo "Cleaning up cluster validation logs..."
        if [ -d "/var/log/cluster-validation" ]; then
            sudo umount -R -f /var/log/cluster-validation 2>/dev/null || true
            sudo rm -rf /var/log/cluster-validation
            echo "Removed /var/log/cluster-validation"
        fi
    fi

    # Prune unused Docker resources
    echo "Pruning unused Docker resources..."
    docker system prune -f --volumes 2>/dev/null || true

    echo "Teardown completed successfully!"
}

cmd_get_token() {
    local CONTAINER_NAME="server"
    if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        echo "[ERROR] Container '$CONTAINER_NAME' not found"
        return 1
    fi
    docker exec "$CONTAINER_NAME" sh -c 'cat /var/lib/rancher/k3s/server/agent-token' || {
        echo "[ERROR] Failed to read agent token"
        return 1
    }
}

cmd_status() {
    local CONTAINER_NAME="server"
    
    if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        echo "[ERROR] Container '$CONTAINER_NAME' not found"
        echo "[INFO] Make sure to start the server with: $0 run server"
        return 1
    fi

    echo "[INFO] Fetching Cluster Validation Framework status..."
    echo ""

    # Get the namespace from config or use default
    local CONFIG_DIR="${CONFIG_DIR:-$SCRIPT_DIR/configs}"
    local CONFIG_FILE="$CONFIG_DIR/config.json"
    
    if [ ! -f "$CONFIG_FILE" ]; then
        echo "[ERROR] config.json not found at $CONFIG_FILE"
        return 1
    fi

    local CVF_NAMESPACE=$(docker exec "$CONTAINER_NAME" sh -c "jq -r '.\"cluster-validation-framework\".namespace // \"default\"' /configs/config.json" 2>/dev/null || echo "default")
    
    # Check if CVF is installed
    if ! docker exec "$CONTAINER_NAME" kubectl get ns "$CVF_NAMESPACE" &>/dev/null; then
        echo "[ERROR] Cluster Validation Framework namespace '$CVF_NAMESPACE' not found"
        echo "[INFO] Make sure CVF is installed by setting 'install-cvf': true in config.json"
        return 1
    fi

    # Get CronJob information
    echo "=== Cluster Validation Framework CronJob Status ==="
    docker exec "$CONTAINER_NAME" kubectl get cronjob -n "$CVF_NAMESPACE" -o wide 2>/dev/null || {
        echo "[WARN] No CronJobs found in namespace $CVF_NAMESPACE"
    }
    echo ""

    # Get recent pod runs with detailed information
    echo "=== Recent Pod Runs ==="
    echo ""
    
    # First check if there are any pods
    local POD_COUNT=$(docker exec "$CONTAINER_NAME" kubectl get pods -n "$CVF_NAMESPACE" --no-headers 2>/dev/null | wc -l)
    
    if [ "$POD_COUNT" -eq 0 ]; then
        echo "[INFO] No pods found in namespace $CVF_NAMESPACE"
        return 0
    fi

    # Display all pods in a simple table format
    docker exec "$CONTAINER_NAME" kubectl get pods -n "$CVF_NAMESPACE" -o wide 2>/dev/null || {
        echo "[ERROR] Failed to fetch pod information"
        return 1
    }
    
    echo ""
    echo "=== Recent Pod Details (Last 5) ==="
    echo ""

    # Show details for each recent pod
    local POD_NAMES=$(docker exec "$CONTAINER_NAME" kubectl get pods -n "$CVF_NAMESPACE" -o json 2>/dev/null | \
        jq -r '.items | map(select(.metadata.name | startswith("cluster-validation-cron-job-"))) | sort_by(.metadata.creationTimestamp) | reverse | .[0:5] | .[].metadata.name' 2>/dev/null)

    if [ -z "$POD_NAMES" ]; then
        echo "[INFO] Could not fetch pod details"
        return 0
    fi

    while IFS= read -r POD_NAME; do
        if [ -n "$POD_NAME" ]; then
            echo "Pod: $POD_NAME"
            
            # Use kubectl describe for simpler, more reliable output
            local POD_INFO=$(docker exec "$CONTAINER_NAME" kubectl describe pod "$POD_NAME" -n "$CVF_NAMESPACE" 2>/dev/null)
            
            if [ -z "$POD_INFO" ]; then
                echo "  (pod information unavailable)"
            else
                # Extract key information using grep
                local CREATED=$(docker exec "$CONTAINER_NAME" kubectl get pod "$POD_NAME" -n "$CVF_NAMESPACE" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null)
                local PHASE=$(docker exec "$CONTAINER_NAME" kubectl get pod "$POD_NAME" -n "$CVF_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)
                local NODE=$(docker exec "$CONTAINER_NAME" kubectl get pod "$POD_NAME" -n "$CVF_NAMESPACE" -o jsonpath='{.spec.nodeName}' 2>/dev/null)
                local STATE=$(docker exec "$CONTAINER_NAME" kubectl get pod "$POD_NAME" -n "$CVF_NAMESPACE" -o jsonpath='{.status.containerStatuses[0].state}' 2>/dev/null | jq -r 'keys[0]' 2>/dev/null)
                
                [ -n "$CREATED" ] && echo "  Test Executed at: $CREATED"
                [ -n "$PHASE" ] && echo "  Phase: $PHASE"
                [ -n "$NODE" ] && echo "  Node: $NODE" || echo "  Node: Not Assigned"
                [ -n "$STATE" ] && echo "  Container State: $STATE"
            fi
            echo ""
        fi
    done <<< "$POD_NAMES"

    echo "[INFO] To download RVS / AGFHC GPU Validation test logs:"
    echo "[INFO] Login to corresponding GPU worker node"
    echo "[INFO] Get logs from /var/log/cluster-validation/"

    echo "[INFO] To download RCCL test logs:"
    echo "[INFO] Login to the node where CronJob pod was executed"
    echo "[INFO] Get logs from /var/log/cluster-validation/"
    echo ""
}

cmd_node_status() {
    local CONTAINER_NAME="server"
    
    if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
        echo "[ERROR] Container '$CONTAINER_NAME' not found"
        echo "[INFO] Make sure to start the server with: $0 run server"
        return 1
    fi

    echo "[INFO] Fetching per-node validation status..."
    echo ""

    # Get list of all nodes
    local NODES=$(docker exec "$CONTAINER_NAME" kubectl get nodes -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)

    if [ -z "$NODES" ]; then
        echo "[ERROR] No nodes found in the cluster"
        return 1
    fi

    echo "=== Per-Node Validation Status ==="
    echo ""
    echo "Format: NODE_NAME | LAST_RUN_TIMESTAMP | STATUS | POD_NAME"
    echo "---"
    echo ""

    local node_found=false

    for NODE in $NODES; do
        # Get node annotation for last run timestamp
        local LAST_RUN=$(docker exec "$CONTAINER_NAME" kubectl get node "$NODE" -o jsonpath='{.metadata.annotations.amd\.com/cluster-validation-last-run-timestamp}' 2>/dev/null)
        
        # Get node labels for validation status
        local HAS_PASSED=$(docker exec "$CONTAINER_NAME" kubectl get node "$NODE" -o jsonpath='{.metadata.labels.amd\.com/cluster-validation-status}' 2>/dev/null)
        
        # Get the pod that ran on this node (most recent)
        local POD_NAME=$(docker exec "$CONTAINER_NAME" kubectl get pods -n "cluster-validation" --field-selector=spec.nodeName="$NODE" -o json 2>/dev/null | \
            jq -r '.items | sort_by(.metadata.creationTimestamp) | reverse | .[0].metadata.name // "N/A"' 2>/dev/null)

        # Only show nodes that have validation annotations or have run pods
        if [ -n "$LAST_RUN" ] || [ "$POD_NAME" != "N/A" ]; then
            node_found=true
            
            # Determine validation status based on label
            local DISPLAY_STATUS="Pending"
            if [ "$HAS_PASSED" = "passed" ]; then
                DISPLAY_STATUS="Passed"
            elif [ "$HAS_PASSED" = "failed" ]; then
                DISPLAY_STATUS="Failed"
            fi
            
            # Format output
            local DISPLAY_TIMESTAMP="${LAST_RUN:-N/A}"
            local DISPLAY_POD="${POD_NAME:-N/A}"
            
            printf "%-30s | %-30s | %-10s | %s\n" "$NODE" "$DISPLAY_TIMESTAMP" "$DISPLAY_STATUS" "$DISPLAY_POD"
        fi
    done

    if [ "$node_found" = false ]; then
        echo "[INFO] No validation data found on any nodes"
        echo "[INFO] Nodes may not have run validation tests yet"
        return 0
    fi

    echo ""
    echo "=== Detailed Node Information ==="
    echo ""

    for NODE in $NODES; do
        local LAST_RUN=$(docker exec "$CONTAINER_NAME" kubectl get node "$NODE" -o jsonpath='{.metadata.annotations.amd\.com/cluster-validation-last-run-timestamp}' 2>/dev/null)
        
        # Only show nodes with validation data
        if [ -n "$LAST_RUN" ]; then
            echo "Node: $NODE"
            
            local HAS_PASSED=$(docker exec "$CONTAINER_NAME" kubectl get node "$NODE" -o jsonpath='{.metadata.labels.amd\.com/cluster-validation-status}' 2>/dev/null)

            # Determine validation status based on label
            local DISPLAY_STATUS="Pending"
            if [ "$HAS_PASSED" = "passed" ]; then
                DISPLAY_STATUS="Passed ✓"
            elif [ "$HAS_PASSED" = "failed" ]; then
                DISPLAY_STATUS="Failed ✗"
            fi
            
            echo "  Last Run: ${LAST_RUN:-N/A}"
            echo "  Status: ${DISPLAY_STATUS}"
            echo ""
        fi
    done

    echo "[INFO] Legend:"
    echo "  Passed   - All validation tests on the node passed (label: amd.com/cluster-validation-status=passed)"
    echo "  Failed   - One or more validation tests on the node failed (label: amd.com/cluster-validation-status=failed)"
    echo "  Pending  - Validation tests are running or not yet executed (label not set)"
    echo ""
    echo "[INFO] To view detailed test logs on a node:"
    echo "[INFO]   Login to the node and check /var/log/cluster-validation/"
    echo ""
}

# --- Main dispatch ---
COMMAND="${1:-help}"
shift 2>/dev/null || true

case "$COMMAND" in
    build)
        cmd_build "$@"
        ;;
    run)
        cmd_run "$@"
        ;;
    teardown)
        cmd_teardown "$@"
        ;;
    get-token)
        cmd_get_token "$@"
        ;;
    status)
        cmd_status "$@"
        ;;
    node-status)
        cmd_node_status "$@"
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo "[ERROR] Unknown command: $COMMAND"
        usage
        exit 1
        ;;
esac
