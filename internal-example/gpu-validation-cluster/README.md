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

- Docker installed and running
- `jq` (for JSON configuration parsing)

### Deployment

1. **Build the container image**
   ```bash
   ./build.sh
   ```

2. **Start the validation cluster**
   ```bash
   # bring up control plane
   ./run.sh server
   
   # fetch control plane token to join the cluster
   ./get_token.sh

   # on other nodes, bring up worker to join the cluster
   ./run.sh agent k3s-agent <token>
   ```

The cluster will automatically initialize with all operators deployed and configured.

## Directory Structure

```
gpu-validation-cluster/
├── README.md            # Project documentation
├── build.sh             # Build unified gpu-validation-cluster image
├── run.sh               # Launch validation cluster
├── entrypoint.sh        # Container entrypoint script
├── Dockerfile           # Dockerfile to build the image
├── teardown.sh          # Cluster shutdown and cleanup
└── configs/             # Configuration files
```

## Configuration

Customize operator behavior by editing files in the `configs/` directory:

- `config.json`: Main configuration settings
- `cluster-validation-config.yaml`: cluster validation framework config
- `cluster-validation-job.yaml`: cluster validation framework cronjob definition

## FAQ

1. What if I hit the DockerHub rate limit to pull images from public repository ? 

Users could configure a DockerHub account secrets in `configs/config.json` so that the system will globally use a registered acocunt to pull images from DockerHub, for example:

```json
  "global": {
    "image-pull-secrets": [
      {
        "registry-utl": "docker.io",
        "username": "my username",
        "token": "my password / access token",
        "isBaseImageSecret": true
      }
    ]
  }
```