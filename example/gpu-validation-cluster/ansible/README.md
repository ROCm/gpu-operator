# GPU Validation Cluster - Ansible Automation

This directory contains Ansible playbooks for automating the deployment and management of the GPU Validation Cluster across multiple nodes.

## Prerequisites

### On Control Node (where you run Ansible)

1. **Ansible installed** (version 2.9+)

   ```bash
   sudo apt update
   sudo apt install ansible
   # or
   pip install ansible
   ```

2. **SSH access to all nodes** (will be configured by playbooks if needed)

3. **Python 3** installed

### On Target Nodes

- Ubuntu 22.04 or 24.04
- Network connectivity between nodes
- Sufficient resources (CPU, RAM, disk space)

## Directory Structure

```plaintext
ansible/
├── ansible.cfg                 # Ansible configuration
├── inventory.yml               # Cluster inventory (edit this!)
├── quickstart.sh               # Interactive quick start script
├── playbooks/
│   ├── setup-ssh-keys.yml      # Configure passwordless SSH
│   ├── setup-cluster.yml       # Main cluster setup
│   ├── add-agent-nodes.yml     # Add new nodes to existing cluster
│   ├── remove-agent-nodes.yml  # Remove nodes from existing cluster
│   ├── teardown-cluster.yml    # Teardown entire cluster
│   └── check-status.yml        # Check cluster status
├── group_vars/                 # Group variables (optional)
└── README.md
```

## Quick Start

### Step 1: Configure Inventory

Edit `inventory.yml` and update with your actual node IPs and settings:

```yaml
server_nodes:
  hosts:
    server-node:
      ansible_connection: local   # If running playbook on server node
      ansible_host: 192.168.1.10  # Your server node IP
      ansible_user: vm            # Your SSH user

agent_nodes:
  hosts:
    agent-node-1:
      ansible_host: 192.168.1.11  # Your agent node IPs
      ansible_user: vm
    agent-node-2:
      ansible_host: 192.168.1.12
      ansible_user: vm
```

**Important notes:**

- Set `ansible_connection: local` on server-node if running the playbook from the server node itself
- Set `ansible_host` on server-node to the actual IP (needed for agent nodes to connect)
- Update `ansible_user` to match your SSH username on all nodes

### Step 2: Setup Passwordless SSH (if needed)

If you don't already have SSH keys configured:

```bash
cd ansible
ansible-playbook playbooks/setup-ssh-keys.yml --ask-pass --ask-become-pass
```

This will:

- Generate an SSH key pair (if one doesn't exist)
- Copy your public key to all nodes
- Verify passwordless access

**Note:** You'll be prompted for your SSH password and sudo password once.

### Step 3: Deploy the Cluster

Run the main setup playbook:

```bash
ansible-playbook playbooks/setup-cluster.yml
```

This will:

1. Build the Docker image locally
2. Install Docker and jq on all nodes (if not present)
3. Copy the image to all nodes and load it
4. Start the server node and retrieve the join token
5. Start agent nodes and join them to the cluster
6. Verify cluster setup

**Estimated time:** 10-20 minutes depending on network speed and number of nodes.

### Step 4: Verify Cluster Status

Check the cluster status:

```bash
ansible-playbook playbooks/check-status.yml
```

### Step 5: Adding New Agent Nodes (Optional)

If you need to add more agent nodes to an existing cluster:

```bash
# 1. Add new nodes to inventory.yml
vi inventory.yml
# Add entries like:
#   agent-node-4:
#     ansible_host: 192.168.1.14
#     ansible_user: vm

# 2. Run the add-agent-nodes playbook (target only new nodes)
ansible-playbook playbooks/add-agent-nodes.yml --limit agent-node-4

# 3. Verify new nodes joined
ansible-playbook playbooks/check-status.yml
```

**Important**: Use `--limit` to target only the new nodes you want to add. The playbook automatically detects and skips nodes already in the cluster.

### Step 6: Removing Agent Nodes (Optional)

If you need to remove agent nodes from an existing cluster:

```bash
# Run the remove-agent-nodes playbook (target nodes to remove)
ansible-playbook playbooks/remove-agent-nodes.yml --limit agent-node-2

# Or remove multiple nodes at once
ansible-playbook playbooks/remove-agent-nodes.yml --limit "agent-node-2,agent-node-3"

# Verify nodes were removed
ansible-playbook playbooks/check-status.yml
```

**Important**: This will drain workloads, delete the node from Kubernetes, stop the agent container, and clean up cluster state.

## Playbook Details

### setup-ssh-keys.yml

**Purpose:** Configure passwordless SSH access to all cluster nodes.

**Usage:**

```bash
ansible-playbook playbooks/setup-ssh-keys.yml --ask-pass --ask-become-pass
```

**What it does:**

- Generates SSH key pair on control node
- Distributes public key to all cluster nodes
- Verifies SSH connectivity

### setup-cluster.yml

**Purpose:** Complete cluster deployment from scratch.

**Usage:**

```bash
ansible-playbook playbooks/setup-cluster.yml
```

**What it does:**

1. **Build Phase (localhost):**
   - Builds Docker image using local Dockerfile
   - Exports image to tar file

2. **Prerequisites Phase (all nodes):**
   - Checks for Docker installation
   - Installs Docker if not present
   - Checks for jq installation
   - Installs jq if not present
   - Adds user to docker group

3. **Distribution Phase (all nodes):**
   - Copies Docker image tar to all nodes
   - Loads image into local Docker registry
   - Copies scripts and configs

4. **Server Phase (server node):**
   - Starts k3s server container
   - Retrieves cluster join token
   - Waits for server to be ready

5. **Agent Phase (agent nodes):**
   - Starts k3s agent containers one by one
   - Joins agents to server
   - Verifies connection

6. **Verification Phase:**
   - Lists all cluster nodes
   - Displays cluster info
   - Shows summary

**Options:**

```bash
# Verbose output
ansible-playbook playbooks/setup-cluster.yml -v

# Limit to specific nodes
ansible-playbook playbooks/setup-cluster.yml --limit server_nodes

# Dry run (check mode)
ansible-playbook playbooks/setup-cluster.yml --check
```

### add-agent-nodes.yml

**Purpose:** Add new agent nodes to an existing running cluster.

**Usage:**

```bash
# Add new nodes to inventory.yml first, then:
ansible-playbook playbooks/add-agent-nodes.yml --limit new-node-name

# Or add multiple nodes at once:
ansible-playbook playbooks/add-agent-nodes.yml --limit "agent-node-4,agent-node-5"
```

**What it does:**

1. **Validation Phase:**
   - Verifies server container is running
   - Retrieves k3s join token from existing server
   - Gets list of existing cluster nodes

2. **Prerequisites Phase (new nodes):**
   - Installs Docker if not present
   - Installs jq if not present
   - Verifies installations

3. **Distribution Phase (new nodes):**
   - Checks if image already exists (skips if present)
   - Copies Docker image from localhost
   - Loads image into local Docker

4. **File Copy Phase (new nodes):**
   - Copies gpu-cluster.sh script
   - Copies configs directory
   - Verifies config.json exists

5. **Join Phase (new nodes):**
   - Starts agent containers one at a time (serial: 1)
   - Joins nodes to existing cluster
   - Verifies container status

6. **Verification Phase:**
   - Lists all cluster nodes (including new ones)
   - Displays cluster summary

**Important Notes:**

- Does NOT restart or affect existing server/agent nodes
- Does NOT rebuild the Docker image
- Use `--limit` to target only the new nodes
- Existing image tar must exist at `/tmp/gpu-validation-cluster-latest.tar`
- Automatically detects and skips nodes already in the cluster (by IP address)

### remove-agent-nodes.yml

**Purpose:** Remove specific agent nodes from an existing running cluster.

**Usage:**

```bash
# Remove single node
ansible-playbook playbooks/remove-agent-nodes.yml --limit agent-node-2

# Remove multiple nodes
ansible-playbook playbooks/remove-agent-nodes.yml --limit "agent-node-2,agent-node-3"
```

**What it does:**

1. **Validation Phase:**
   - Verifies server container is running
   - Gets current list of cluster nodes

2. **Drain and Delete Phase (per node):**
   - Finds Kubernetes node name by matching IP address
   - Drains node (moves workloads to other nodes gracefully)
   - Deletes node from Kubernetes cluster
   - Processes nodes serially (one at a time)

3. **Container Cleanup Phase (target nodes):**
   - Stops agent container
   - Removes agent container

4. **State Cleanup Phase (target nodes):**
   - Runs teardown script to clean up cluster state
   - Verifies containers are removed

5. **Verification Phase:**
   - Lists remaining cluster nodes
   - Displays updated node count
   - Shows summary

**Important Notes:**

- Uses `kubectl drain` to gracefully move workloads before removal
- Finds nodes by IP address (works even if K8s node name differs from inventory name)
- Does NOT affect server or other agent nodes
- Always use `--limit` to target specific nodes to remove
- Removed nodes can be re-added later using add-agent-nodes.yml

### teardown-cluster.yml

**Purpose:** Completely remove cluster from all nodes.

**Usage:**

```bash
ansible-playbook playbooks/teardown-cluster.yml
```

**What it does:**

- Runs teardown script on all nodes
- Removes containers
- Cleans up cluster state
- Verifies cleanup

### check-status.yml

**Purpose:** Check current cluster status.

**Usage:**

```bash
ansible-playbook playbooks/check-status.yml
```

**What it does:**

- Shows Docker container status on all nodes
- Displays Kubernetes nodes
- Lists all pods
- Shows cluster validation framework status

## Advanced Usage

### Running Against Specific Nodes

```bash
# Only setup server
ansible-playbook playbooks/setup-cluster.yml --limit server_nodes

# Only setup specific agent
ansible-playbook playbooks/setup-cluster.yml --limit agent-node-1

# Setup server and one agent
ansible-playbook playbooks/setup-cluster.yml --limit "server_nodes,agent-node-1"
```

### Using Different Inventory

```bash
ansible-playbook -i custom-inventory.yml playbooks/setup-cluster.yml
```

### Overriding Variables

```bash
# Use different image tag
ansible-playbook playbooks/setup-cluster.yml -e "gpu_cluster_image_tag=v1.0.0"

# Use different user
ansible-playbook playbooks/setup-cluster.yml -e "ansible_user=myuser"
```

### Debug Mode

```bash
# Verbose output
ansible-playbook playbooks/setup-cluster.yml -v

# Very verbose (connection debugging)
ansible-playbook playbooks/setup-cluster.yml -vvv

# Show diff for changes
ansible-playbook playbooks/setup-cluster.yml --diff
```

## Customization

### Adding More Variables

Create a file `group_vars/all.yml`:

```yaml
---
# Custom settings for all nodes
docker_version: "20.10.*"
custom_registry: "registry.example.com"
```

### Node-Specific Variables

Create a file `host_vars/server-node.yml`:

```yaml
---
# Custom settings for server node
ansible_user: root
special_config: true
```

## Troubleshooting

### SSH Connection Issues

```bash
# Test SSH connectivity
ansible all -m ping

# Test with password
ansible all -m ping --ask-pass

# Test with specific user
ansible all -m ping -u ubuntu
```

### Docker Issues

```bash
# Check Docker on all nodes
ansible cluster_nodes -m command -a "docker ps" -b

# Restart Docker service
ansible cluster_nodes -m systemd -a "name=docker state=restarted" -b
```

### Container Issues

```bash
# Check container logs on server
ssh user@server-node "docker logs server"

# Check container logs on agent
ssh user@agent-node "docker logs agent"

# Or via Ansible
ansible server_nodes -m shell -a "docker logs server --tail 50"
```

### Playbook Failures

```bash
# Start from specific task
ansible-playbook playbooks/setup-cluster.yml --start-at-task="Start server container"

# Skip specific tags (if tags are defined)
ansible-playbook playbooks/setup-cluster.yml --skip-tags docker_install
```

## Maintenance

### Updating the Cluster

To update with a new image:

```bash
# 1. Teardown existing cluster
ansible-playbook playbooks/teardown-cluster.yml

# 2. Setup with new image
ansible-playbook playbooks/setup-cluster.yml
```

### Adding New Agent Nodes

1. Add new nodes to `inventory.yml`
2. Run setup for new nodes only:

   ```bash
   ansible-playbook playbooks/setup-cluster.yml --limit new-agent-node
   ```

## Security Considerations

- SSH keys are stored in `~/.ssh/` by default
- Cluster tokens are stored in Ansible facts (temporary)
- Consider using Ansible Vault for sensitive data:

  ```bash
  ansible-vault create group_vars/all.yml
  ansible-playbook playbooks/setup-cluster.yml --ask-vault-pass
  ```

## Additional Resources

- [Ansible Documentation](https://docs.ansible.com/)
- [GPU Validation Cluster Main README](../README.md)
- [k3s Documentation](https://docs.k3s.io/)

## Support

For issues or questions:

1. Check the troubleshooting section above
2. Review playbook logs with `-v` or `-vvv`
3. Check container logs on affected nodes
4. Open an issue in the project repository
