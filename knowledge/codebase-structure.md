---
name: Codebase Structure and Organization
description: Directory layout, key files, and module organization of GPU Operator codebase
type: reference
---

# Codebase Structure and Organization

## Repository Root Structure

```
gpu-operator/
├── api/                    # CRD definitions
│   └── v1alpha1/          # DeviceConfig, RemediationWF types
├── cmd/                    # Main entrypoint
│   └── main.go            # Operator startup
├── internal/              # Core operator logic
│   ├── controllers/       # Reconciliation logic
│   ├── kmmmodule/        # KMM integration
│   ├── plugin/           # Device Plugin handler
│   ├── nodelabeller/     # Node Labeller handler
│   ├── metricsexporter/  # Metrics Exporter handler
│   ├── configmanager/    # DCM handler
│   ├── testrunner/       # Test Runner handler
│   ├── config/           # Operator configuration
│   ├── utils_container/  # Utils container logic
│   ├── validator/        # Validation logic
│   └── utils.go          # Shared utilities
├── config/                # Kubernetes manifests
│   ├── crd/              # CRD YAML
│   ├── manager/          # Deployment manifests
│   ├── rbac/             # RBAC rules
│   └── samples/          # Example DeviceConfigs
├── helm-charts-k8s/       # Helm chart for vanilla K8s
├── bundle/                # OLM bundle for OpenShift
├── tests/                 # Test suites
│   ├── e2e/              # End-to-end tests
│   └── pytests/          # Python test automation
├── docs/                  # Documentation
├── ci-internal/          # Internal CI/CD scripts
├── tools/                # Build and dev tools
├── vendor/               # Vendored dependencies
├── go.mod, go.sum        # Go module files
├── Makefile              # Build targets
├── Dockerfile            # Operator image
└── PROJECT               # Kubebuilder metadata
```

## Key Directories Detail

### api/v1alpha1/
**Purpose**: Kubernetes Custom Resource Definitions

- `deviceconfig_types.go` (1033 lines)
  - Main CRD: DeviceConfig, DeviceConfigSpec, DeviceConfigStatus
  - All component specs: Driver, DevicePlugin, DRADriver, ConfigManager, etc.
  - Status types: DeploymentStatus, ModuleStatus
  - Enum: UpgradeState
- `remediationwf_types.go`
  - RemediationWorkflow type (if separate from DeviceConfig)
- `groupversion_info.go`
  - API group registration (amd.com/v1alpha1)
- `zz_generated.deepcopy.go`
  - Auto-generated DeepCopy methods

### cmd/
**Purpose**: Application entrypoint

- `main.go` (179 lines)
  - Initializes scheme with all required types
  - Sets up controller manager
  - Registers DeviceConfigReconciler
  - Reads configuration from file (--config flag)
  - Handles KMM_WATCH_ENABLED env var
  - Creates handlers for all components:
    - kmmHandler (KMM or NoOp)
    - dpHandler (device plugin)
    - nlHandler (node labeller)
    - metricsHandler
    - testrunnerHandler
    - configmanagerHandler
    - workerMgr
  - Starts manager with health/ready checks

### internal/

#### internal/controllers/
**Main reconciliation logic**

- `device_config_reconciler.go` (~66KB)
  - DeviceConfigReconciler struct
  - Reconcile() method - main entry point
  - Orchestrates all component handlers
  - Handles driver installation, upgrades, removals
  - Manages DaemonSets for device plugin, metrics, etc.
  - Enforces mutual exclusion (Device Plugin vs DRA)
  
- `remediation_handler.go` (~75KB)
  - RemediationHandler interface and implementation
  - Watches node conditions
  - Creates/manages Argo Workflows
  - Handles workflow suspension/resumption
  - Applies taints/labels to nodes
  - Parses condition-to-workflow ConfigMap
  
- `upgrademgr.go` (~52KB)
  - WorkerMgr for coordinating driver upgrades
  - Tracks per-node upgrade state
  - Implements upgrade policies (max parallel, max unavailable)
  - Handles node drain and reboot coordination

- `watchers/`
  - Additional watchers for Node, Module, Workflow events

- `remediation/`
  - `configs/default-configmap.yaml` - Default AFID mappings
  - Remediation-specific utilities

#### internal/kmmmodule/
**KMM (Kernel Module Management) integration**

- Abstracts KMM operations
- Handles Module CR creation/updates
- Manages driver image builds
- Two implementations: real KMM and NoOp (when KMM_WATCH_ENABLED=false)

#### internal/plugin/
**Device Plugin handler**

- Creates/manages device plugin DaemonSet
- Creates/manages node labeller DaemonSet
- Handles ConfigMaps for device plugin config

#### internal/nodelabeller/
**Node Labeller handler**

- Manages node labeller component
- Configures which labels to apply

#### internal/metricsexporter/
**Metrics Exporter handler**

- Creates DaemonSet for metrics exporter
- Creates Service (ClusterIP or NodePort)
- Optional ServiceMonitor for Prometheus Operator
- Optional kube-rbac-proxy sidecar

#### internal/configmanager/
**Device Config Manager handler**

- Creates DCM DaemonSet
- Manages partition configuration ConfigMap

#### internal/testrunner/
**Test Runner handler**

- Creates test runner DaemonSet
- Manages test configuration ConfigMap

#### internal/config/
**Operator configuration parsing**

- Reads config file passed via --config flag
- Parses manager options

#### internal/utils.go
**Shared utilities** (12KB)

- `IsOpenShift()` - Detect OpenShift vs vanilla K8s
- Image parsing utilities
- Label/annotation helpers
- Common constants

#### internal/utils_container/
**Utils container logic**

- Used for reboot operations during upgrades
- Used in remediation workflows

#### internal/validator/
**Validation logic**

- Validates DeviceConfig before reconciliation
- Enforces mutual exclusion rules
- Validates image formats, paths, etc.

### config/
**Kubernetes manifests and Kustomize configs**

- `crd/` - Generated CRD YAML
- `manager/` - Operator deployment YAML
- `rbac/` - ClusterRole, ClusterRoleBinding, ServiceAccount
- `samples/` - Example DeviceConfig CRs
- `prometheus/` - ServiceMonitor for operator metrics
- `certmanager/` - Webhook certificates (if webhooks enabled)

### helm-charts-k8s/
**Helm chart for vanilla Kubernetes**

Structure:
```
gpu-operator-charts/
├── Chart.yaml
├── values.yaml              # Default values
├── templates/
│   ├── operator.yaml        # Operator deployment
│   ├── deviceconfig.yaml    # Default DeviceConfig
│   ├── crds/                # CRDs
│   ├── remediation/         # Argo Workflows components
│   ├── kmm/                 # KMM operator (if bundled)
│   └── nfd/                 # NFD operator (if bundled)
└── README.md
```

### bundle/
**OpenShift OLM (Operator Lifecycle Manager) bundle**

- `manifests/` - ClusterServiceVersion (CSV), CRDs
- `metadata/` - Bundle annotations
- Used for OpenShift OperatorHub installation

### tests/

#### tests/e2e/
**End-to-end tests**

- Kubernetes cluster required
- Tests full operator workflow
- Located in `tests/e2e/`

#### tests/pytests/
**Python-based test automation**

- Integration tests
- Test scripts for various scenarios
- Helpers for data collection

### docs/
**User-facing documentation**

Key documentation files:
- `overview.md` - Operator overview
- `drivers/installation.md` - Driver setup
- `device_plugin/device-plugin.md` - Device Plugin docs
- `dra/dra-driver.md` - DRA driver docs
- `dcm/device-config-manager.md` - DCM docs
- `metrics/exporter.md` - Metrics exporter docs
- `autoremediation/auto-remediation.md` - Auto-remediation docs
- `test/test-runner-overview.md` - Test runner docs
- `installation/kubernetes-helm.md` - Helm install guide
- `installation/openshift-olm.md` - OpenShift install guide

### ci-internal/
**Internal CI/CD automation**

- Build scripts
- Test launchers
- Image management
- Deployment automation

### tools/
**Development and build tools**

- `build/` - Build utilities
- `techsupport_dump.sh` - Diagnostic data collection

## Key Files

### Makefile
**Build and development tasks**

Common targets:
- `make build` - Build operator binary
- `make docker-build` - Build operator image
- `make install` - Install CRDs
- `make deploy` - Deploy operator
- `make test` - Run tests
- `make generate` - Generate code (DeepCopy, etc.)
- `make manifests` - Generate CRD manifests

### go.mod
**Go module dependencies**

Key dependencies:
- controller-runtime
- client-go
- KMM API (github.com/rh-ecosystem-edge/kernel-module-management)
- Argo Workflows API (github.com/argoproj/argo-workflows/v4)
- Prometheus Operator API

### Dockerfile
**Operator container image**

Multi-stage build:
1. Builder stage (Go compilation)
2. Final stage (minimal runtime image)

### PROJECT
**Kubebuilder project metadata**

- Domain: sigs.x-k8s.io
- Repo: github.com/ROCm/gpu-operator
- Layout: go.kubebuilder.io/v3
- Resources: DeviceConfig

## Code Generation

Generated files (DO NOT EDIT):
- `api/v1alpha1/zz_generated.deepcopy.go` - DeepCopy methods
- `config/crd/` - CRD YAML manifests

Regenerate with:
```bash
make generate  # Code generation
make manifests # CRD manifest generation
```

## Import Paths

Package paths follow Go module structure:
```go
import (
    gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
    "github.com/ROCm/gpu-operator/internal/controllers"
    "github.com/ROCm/gpu-operator/internal/kmmmodule"
    // etc.
)
```

## Testing Strategy

1. **Unit tests**: `*_test.go` files throughout codebase
2. **Integration tests**: `internal/test/` and `tests/pytests/`
3. **E2E tests**: `tests/e2e/`
4. **Mocks**: `mock_*.go` files for testing reconciler components

## Build Artifacts

Generated during build:
- `bin/` - Compiled binaries
- `bundle/` - OLM bundle (for OpenShift)
- `testbin/` - Test binaries
- Container images pushed to registry
