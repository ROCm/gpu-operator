#!/bin/bash

set -e

echo "Starting GPU Validation Cluster teardown..."

# Remove k3s containers
echo "Removing k3s containers..."
docker ps -a | grep -E "(k3s-server|k3s-agent)" | awk '{print $1}' | xargs -r docker rm -f 2>/dev/null || true

# Remove k3s images
#echo "Removing k3s images..."
#docker images | grep "k3s" | awk '{print $1}' | xargs -r docker rmi -f 2>/dev/null || true

# Clean up Rancher directories
echo "Cleaning up Rancher directories..."
if [ -d "/etc/rancher" ]; then
    sudo rm -rf /etc/rancher
    echo "Removed /etc/rancher"
fi

if [ -d "/var/lib/rancher" ]; then
    sudo rm -rf /var/lib/rancher
    echo "Removed /var/lib/rancher"
fi

if [ -d "/var/lib/kubelet" ]; then
    sudo rm -rf /var/lib/kubelet
    echo "Removed /var/lib/kubelet"
fi

rm -f /var/log/k3s.log
echo "Removed /var/log/k3s.log"

# Clean up CNI directory
echo "Cleaning up CNI directory..."
if [ -d "/opt/cni/bin" ]; then
    sudo rm -rf /opt/cni/bin
    echo "Removed /opt/cni/bin"
fi

# Prune unused Docker resources
echo "Pruning Docker system..."
docker system prune -f --volumes 2>/dev/null || true

echo "Teardown completed successfully!"
