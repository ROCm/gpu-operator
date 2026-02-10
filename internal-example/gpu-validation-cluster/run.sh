#!/bin/bash
set -e

# Enable debug mode - uncomment to see all commands
# set -x

IMAGE_NAME="${IMAGE_NAME:-gpu-validation-k3s}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
# Global variable for server node internal IP
NODE_INTERNAL_IP=""
# Global variable for secret name of the OS base image
OS_BASE_IMAGE_SECRET_NAME=""

# Load configuration
CONFIG_FILE="$(dirname "$0")/configs/config.json"
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
CONTAINER_NAME="${2:-k3s-${MODE}}"

if [ "$MODE" != "server" ] && [ "$MODE" != "agent" ]; then
    echo "[ERROR] Invalid mode: $MODE. Use 'server' or 'agent'"
    echo "Usage: $0 <server|agent> [container-name] [k3s-ip] [k3s-token]"
    exit 1
fi

# Common docker run options
DOCKER_OPTS=(
    "--privileged"
    "--net=host"
    "--cgroupns=host"
    "--security-opt=systempaths=unconfined"
    "--name" "$CONTAINER_NAME"
    "-e" "K3S_MODE=$MODE"
    "-v" "/etc/rancher:/etc/rancher"
    "-v" "/var/lib/rancher:/var/lib/rancher"
    "-v" "/var/lib/kubelet:/var/lib/kubelet"
    "-v" "/var/log:/var/log"
    "-v" "/var/run:/var/run"
    "-v" "/opt/cni/bin:/opt/cni/bin:shared"
    "-v" "/lib/modules:/lib/modules"
    "-v" "/sys:/sys"
    "-v" "/dev:/dev"
    "-v" "$(dirname "$0")/configs:/configs:ro"
    "--rm"
)

if [ "$MODE" = "agent" ]; then
    K3S_IP="${3:-}"
    K3S_TOKEN="${4:-}"
    
    if [ -z "$K3S_IP" ] || [ -z "$K3S_TOKEN" ]; then
        echo "[ERROR] For agent mode, K3S_IP and K3S_TOKEN must be provided"
        echo "Usage: $0 agent [container-name] <k3s-ip> <k3s-token>"
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
            REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-utl\"]")
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
            local REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-utl\"]")
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
            local REGISTRY_URL=$(echo "$IMAGE_PULL_SECRETS" | jq -r ".[$i][\"registry-utl\"]")
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
    docker exec "$CONTAINER_NAME" cp /opt/cni/bin/* /var/lib/rancher/k3s/data/cni/
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
    echo "[INFO] Installing MPI Operator..."
    local MPI_OPERATOR_VERSION=$(read_config '.["cluster-validation-framework"]["mpi-operator"].version')
    docker exec "$CONTAINER_NAME" kubectl apply --server-side -f https://raw.githubusercontent.com/kubeflow/mpi-operator/$MPI_OPERATOR_VERSION/deploy/v2beta1/mpi-operator.yaml
    echo "[INFO] MPI Operator installation completed"
    echo "[INFO] Posting Validation Framework manifests..."
    docker exec "$CONTAINER_NAME" kubectl apply -f /configs/cluster-validation-config.yaml
    docker exec "$CONTAINER_NAME" kubectl apply -f /configs/cluster-validation-job.yaml
    echo "[INFO] Cluster Validation Framework installation completed"
}

echo "[INFO] Running: docker run ${DOCKER_OPTS[@]} $FULL_IMAGE"
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
echo "[INFO] Waiting for container to finish..."
wait $CONTAINER_PID
