# GPU Validation Cluster

A containerized, one-click deployment solution for validating AMD GPU and AINIC in a cluster.

## Overview

This project provides an automated, reproducible testing environment for GPU operator functionality. It deploys a complete Kubernetes cluster with AMD GPU and Network operators pre-configured, enabling rapid validation of operator features and performance.

## Features

- **Automated Deployment**: Single-command cluster initialization with all operators ready
- **GPU Operator**: Full AMD GPU device plugin with resource management and scheduling
- **Network Operator**: AMD network operator for advanced networking and performance optimization
- **Cluster Validation Framework**: Comprehensive automated tests for both GPU validation and RCCL tests.
- **Containerized**: Entire stack runs in containers for portability and consistency

## Quick Start

### Prerequisites

- Docker engine installed and daemon is running (validated on Docker 29.1.5 or newer)
- `jq` CLI for JSON parsing
- Ubuntu 22.04 or 24.04 host

### Deployment

1. **Build the container image**

   ```bash
   ./gpu-cluster.sh build
   ```

   After building, you have two options to make the image available on all nodes:

   - **Option A**: Save and port the image to other nodes:

     ```bash
     # On server node: save the image
     docker save gpu-validation-cluster:latest -o gpu-validation-cluster.tar

     # Transfer to worker nodes and load:
     scp gpu-validation-cluster.tar user@worker-node:/tmp/
     ssh user@worker-node "docker load -i /tmp/gpu-validation-cluster.tar"
     ```

   - **Option B**: Rebuild the image on each worker node:

     ```bash
     # Run on each worker node
     ./gpu-cluster.sh build
     ```

2. **Configure cluster validation framework**

   Before starting the cluster, edit `configs/config.json` to match your environment. Common configuration options:

   **Device Type Selection:**
   - For physical GPUs: `"gpu-type": "amd-gpu"`
   - For SR-IOV VF GPUs (in VMs): `"gpu-type": "amd-vgpu"`
   - For physical NICs: `"nic-type": "amd-nic"`
   - For virtual NICs (in VMs): `"nic-type": "amd-vnic"`

   **Resource Configuration:**

   ```json
   "cluster-validation-framework": {
     "node-selector-labels": [      // Node selector labels for candidate selection
       "feature.node.kubernetes.io/amd-gpu=true",  // GPU label selector
       "feature.node.kubernetes.io/amd-nic=true"   // NIC label selector
     ],
     "resources": {
       "worker-replicas": 2,        // Number of nodes to validate in parallel
       "gpu-per-worker": 8,         // Number of GPUs per node
       "pf-nic-per-worker": 0,      // Number of physical function NICs per node
       "vf-nic-per-worker": 8,      // Number of virtual function NICs per node
       "slots-per-worker": 8,       // MPI ranks per worker
       "node-validation-interval-mins": 10  // Minimum interval between validation runs on same node
     },
     "skip-tests": {
       "skip-gpu-validation": false,  // Set to true to skip GPU validation tests (RVS/AGFHC)
       "skip-rccl-test": false         // Set to true to skip MPI Job RCCL tests
     }
   }
   ```

   **Node Selector Labels:**
   The `node-selector-labels` array defines which nodes are eligible for cluster validation. Each label is combined with AND logic to select nodes.

   Common label combinations:
   - Physical GPUs + Physical NICs: `["feature.node.kubernetes.io/amd-gpu=true", "feature.node.kubernetes.io/amd-nic=true"]`
   - Virtual GPUs + Virtual NICs (in VMs): `["feature.node.kubernetes.io/amd-vgpu=true", "feature.node.kubernetes.io/amd-vnic=true"]`
   - Mixed configurations: Customize the array to match your environment

3. **Start the validation cluster**

   ```bash
   # Bring up control plane (run in background)
   ./gpu-cluster.sh run server &

   # Fetch control plane token to join the cluster
   ./gpu-cluster.sh get-token

   # On other nodes, bring up workers to join the cluster (run in background)
   ./gpu-cluster.sh run agent <server-ip> <token> &
   ```

4. **Verify cluster status**

   After bringing up the cluster, login to the server container to check cluster status:

   ```bash
   # Login to server container
   docker exec -it server bash

   # Check all nodes are ready
   kubectl get nodes

   # Check all pods are running
   kubectl get pods -A

   # Exit container
   exit

   # Check cluster validation framework status
   ./gpu-cluster.sh status

   # Check per-node validation results
   ./gpu-cluster.sh node-status
   ```

5. **Tear down the cluster**

   ```bash
   ./gpu-cluster.sh teardown
   ```

## Usage

```text
Usage: ./gpu-cluster.sh <command> [args...]

Commands:
  build                          Build the Docker image
  run <server|agent> [args...]   Run the node as server or agent
  teardown                       Tear down the cluster and clean up
  get-token                      Run on server node to print agent join token
  status                         Show cluster validation framework status and recent runs
  node-status                    Show validation status per node
  help                           Show this help message

Run arguments:
  run server
  run agent  <server-ip> <token>
```

## Environment Variables

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `IMAGE_NAME` | `gpu-validation-cluster` | Docker image name |
| `IMAGE_TAG` | `latest` | Docker image tag |
| `BUILD_DIR` | `$SCRIPT_DIR/build` | Path to directory containing Dockerfile and entrypoint.sh |
| `CONFIG_DIR` | `$SCRIPT_DIR/configs` | Path to directory containing config.json and other config files |
| `CLEANUP_TEST_LOGS` | `false` | Clean up cluster validation test logs in `/var/log/cluster-validation` during teardown |

### Examples

```bash
# Build using a custom build directory
BUILD_DIR=/path/to/custom/build ./gpu-cluster.sh build

# Run server node with custom config directory
CONFIG_DIR=/path/to/custom/configs ./gpu-cluster.sh run server

# Run agent node with custom config directory to join a cluster
CONFIG_DIR=/path/to/custom/configs ./gpu-cluster.sh run agent <server-ip> <token>

# Teardown with cluster validation logs cleanup enabled
CLEANUP_TEST_LOGS=true ./gpu-cluster.sh teardown

# Show cluster validation framework CronJob status and recent pod runs
./gpu-cluster.sh status

# Show per-node validation test status (last run time and result)
./gpu-cluster.sh node-status
```

## Directory Structure

```text
gpu-validation-cluster/
├── README.md            # Project documentation
├── gpu-cluster.sh       # Unified script for build, run, teardown, and get-token
├── build/               # Build context
│   ├── Dockerfile       # Dockerfile to build the image
│   └── entrypoint.sh    # Container entrypoint script
└── configs/             # Configuration files
    ├── config.json                      # Main configuration settings
    ├── cluster-validation-config.yaml   # Cluster validation framework config
    └── cluster-validation-job.yaml      # Cluster validation framework cronjob
```

## Configuration

Customize operator behavior by editing files in the `configs/` directory:

- `config.json`: Main configuration settings
- `cluster-validation-config.yaml`: cluster validation framework config
- `cluster-validation-job.yaml`: cluster validation framework cronjob definition

## Cleanup Behavior

By default, the teardown command preserves cluster validation logs in `/var/log/cluster-validation` for troubleshooting and analysis. To remove these logs during teardown, set the `CLEANUP_TEST_LOGS` environment variable to `true`:

```bash
CLEANUP_TEST_LOGS=true ./gpu-cluster.sh teardown
```

## Monitoring Validation Tests

### Cluster-Wide Status

To view the overall cluster validation framework status including CronJob configuration and recent pod runs:

```bash
./gpu-cluster.sh status
```

This command displays:

- **CronJob Status**: Configuration and schedule of validation CronJobs
- **Recent Pod Runs**: Last 5 pod executions with timestamps, phases, and assigned nodes
- **Pod Details**: Detailed information about recent validation test pods

### Per-Node Validation Status

To view validation test results broken down by individual node:

```bash
./gpu-cluster.sh node-status
```

This command displays:

- **Node Summary Table**: Shows each node with its last run timestamp and validation result (Passed/Failed/Pending)
- **Detailed Node Information**: Per-node breakdown including:
  - Last run timestamp (from node annotation)
  - Validation result status
  - Most recent pod name that executed on the node

**Result Status Legend:**

- `Passed`: All validation tests on the node passed
  - Label: `amd.com/cluster-validation-status=passed`
- `Failed`: One or more validation tests on the node failed
  - Label: `amd.com/cluster-validation-status=failed`
- `Pending`: Validation tests are running or have not yet executed
  - Label: not set (no label present)

### Understanding the Output

The per-node view uses Kubernetes node labels and annotations to track validation test execution:

- **Annotation `amd.com/cluster-validation-last-run-timestamp`**: Timestamp of the last validation test execution on this node
- **Label `amd.com/cluster-validation-status`**: Current validation result status:
  - Set to `passed` if all tests passed
  - Set to `failed` if any tests failed
  - Not set (empty) if tests are pending or have not yet run

## FAQ

1. What if I hit the DockerHub rate limit to pull images from public repository?

Users could configure a DockerHub account secrets in `configs/config.json` so that the system will globally use a registered account to pull images from DockerHub, for example:

```json
  "global": {
    "image-pull-secrets": [
      {
        "registry-url": "docker.io",
        "username": "my username",
        "token": "my password / access token",
        "isBaseImageSecret": true
      }
    ]
  }
```
