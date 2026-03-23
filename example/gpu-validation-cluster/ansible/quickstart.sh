#!/bin/bash
# Quick start script for GPU Validation Cluster Ansible deployment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=========================================="
echo "GPU Validation Cluster - Quick Start"
echo "=========================================="
echo ""

# Check if Ansible is installed
if ! command -v ansible &> /dev/null; then
    echo "[ERROR] Ansible is not installed."
    echo "[INFO] Install Ansible:"
    echo "  sudo apt update && sudo apt install ansible"
    echo "  or"
    echo "  pip install ansible"
    exit 1
fi

echo "[INFO] Ansible version: $(ansible --version | head -n1)"
echo ""

# Check if inventory exists
if [ ! -f "inventory.yml" ]; then
    echo "[ERROR] inventory.yml not found!"
    echo "[INFO] Please create and edit inventory.yml with your node details:"
    echo "  vi inventory.yml"
    echo ""
    echo "[INFO] Required configuration in inventory.yml:"
    echo "  - ansible_host: IP addresses of your nodes"
    echo "  - ansible_user: SSH username"
    echo "  - ansible_connection: local (for server node if running locally)"
    exit 1
fi

echo "[INFO] Inventory file found: inventory.yml"
echo ""

# Test connectivity
echo "[STEP 1] Testing SSH connectivity to nodes..."
if ansible all -m ping -o; then
    echo "[SUCCESS] All nodes are reachable"
else
    echo "[WARN] Some nodes are not reachable via SSH"
    echo ""
    read -p "Do you want to setup SSH keys? (y/n) " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "[INFO] Running SSH key setup..."
        ansible-playbook playbooks/setup-ssh-keys.yml --ask-pass --ask-become-pass
    else
        echo "[ERROR] Cannot proceed without SSH access to nodes"
        exit 1
    fi
fi

echo ""
echo "[STEP 2] Ready to deploy cluster"
echo ""
echo "The following will be performed:"
echo "  1. Build Docker image locally"
echo "  2. Install Docker and jq on all nodes (if needed)"
echo "  3. Copy image to all nodes"
echo "  4. Start server node"
echo "  5. Start agent nodes and join cluster"
echo ""
read -p "Proceed with cluster deployment? (y/n) " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Deployment cancelled."
    exit 0
fi

echo ""
echo "[STEP 3] Deploying cluster..."
ansible-playbook playbooks/setup-cluster.yml

echo ""
echo "=========================================="
echo "Deployment Complete!"
echo "=========================================="
echo ""
echo "Check cluster status:"
echo "  ansible-playbook playbooks/check-status.yml"
echo ""
echo "Teardown cluster:"
echo "  ansible-playbook playbooks/teardown-cluster.yml"
echo ""
