# GPU Operator Skills

Project-specific Claude Code skills for GPU Operator development and testing.

## Available Skills by Operand

### Testing & Development (by Component)

#### `/pytest-dev`

**File**: `pytest-dev.md` | **Component**: Generic

Generic pytest development skill for AMD GPU testing infrastructure.

**Use cases**:

- Implement pytest testcases from approved test plans
- Write new test functions following project patterns
- Debug failing tests from CI job logs
- Understand test infrastructure and fixture relationships
- Navigate the test codebase
- Ensure cross-platform compatibility (K8s + OpenShift)

**Example**:

```bash
/pytest-dev Implement test scenarios from approved test plan at path/to/test-plan.md
/pytest-dev Debug the failures in job log 30158283
```

---

#### `/pytest-dcm-dev`

**File**: `pytest-dcm-dev.md` | **Component**: Device Config Manager

Device Config Manager (DCM) GPU partition testing.

**Use cases**:

- DCM partition testing (SPX, DPX, QPX, CPX with NPS1/NPS2/NPS4)
- Label verification across multiple nodes
- Driver reload timing validation
- Cleanup patterns for failed tests
- Partition profile file validation

**Test files**:

- `tests/pytests/k8/gpu-operator/test_config_manager.py`
- `tests/pytests/k8/gpu-operator/test_multi_deviceconfig.py`

**Example**:

```bash
/pytest-dcm-dev Add partition tests for SPX mode
/pytest-dcm-dev Debug label verification failure
```

---

#### `/pytest-dme-dev`

**File**: `pytest-dme-dev.md` | **Component**: Device Metrics Exporter

Device Metrics Exporter (DME) testing - Prometheus metrics collection and validation.

**Use cases**:

- Metrics exporter deployment testing (Helm and standalone)
- Validate metric values against AMD-SMI ground truth
- Troubleshoot metric endpoint connectivity (ClusterIP vs NodePort)
- Debug metric collection and comparison logic
- Prometheus metric format and labeling validation

**Test files**:

- `tests/pytests/k8/gpu-operator/test_metrics_exporter.py`
- `tests/pytests/k8/exporter/test_metrics_exporter.py`
- `tests/pytests/k8/gpu-operator/test_metrics_values.py`

**Example**:

```bash
/pytest-dme-dev Implement metric validation tests from test plan
/pytest-dme-dev Debug metric endpoint connectivity issue
```

---

#### `/pytest-npd-dev`

**File**: `pytest-npd-dev.md` | **Component**: Node Problem Detector

Node Problem Detector (NPD) integration testing with amdgpuhealth plugin.

**Use cases**:

- NPD amdgpuhealth plugin testing
- ConfigMap/DaemonSet volume mounting
- Metric endpoint validation
- NPD condition monitoring
- Platform coverage (K8s + OpenShift)

**Test files**:

- `tests/pytests/k8/gpu-operator/test_node_problem_detector.py`
- `tests/pytests/k8/exporter/test_node_problem_detector.py`

**Example**:

```bash
/pytest-npd-dev Implement NPD condition monitoring tests
/pytest-npd-dev Debug ConfigMap mounting issue
```

---

#### `/pytest-anr-dev`

**File**: `pytest-anr-dev.md` | **Component**: Auto Node Remediation

Auto Node Remediation (ANR) testing - Argo Workflow-based node remediation.

**Use cases**:

- ANR deployment and workflow testing
- Validate remediation workflows (driver reload, node reboot, drain)
- Troubleshoot Argo Workflow execution
- Debug NHC (Node Health Check) integration
- Custom ConfigMap remediation templates
- K8s only (not supported on OpenShift)

**Test files**:

- `tests/pytests/k8/gpu-operator/test_node_remediation.py`
- `tests/pytests/k8/gpu-operator/test_anr_deployment.py`

**Example**:

```bash
/pytest-anr-dev Implement workflow execution tests from test plan
/pytest-anr-dev Debug workflow stuck in Running phase
```

**Note**: ANR is K8s-only, not supported on OpenShift

---

#### `/pytest-driver-dev`

**File**: `pytest-driver-dev.md` | **Component**: ROCm/AMDGPU Driver

ROCm/AMDGPU driver version management and testing.

**Use cases**:

- Driver deployment testing (deviceconfig vs inbox)
- Driver version upgrades and downgrades
- Validate driver blacklist configuration
- Test driver-deviceplugin integration
- Debug KMM (Kernel Module Management) workflows
- Manage driver spec files for different ROCm versions

**Test files**:

- `tests/pytests/k8/gpu-operator/test_driver_deviceplugin.py`

**Example**:

```bash
/pytest-driver-dev Debug KMM worker failure
/pytest-driver-dev Update driver spec for new ROCm 6.4
```

---

#### `/pytest-upgrade-dev`

**File**: `pytest-upgrade-dev.md` | **Component**: GPU Operator & Operand Upgrades

GPU Operator and operand upgrade testing from released versions to RC builds.

**Use cases**:

- GPU operator helm upgrade testing (base → RC)
- Operand upgrade workflows (RollingUpdate/OnDelete)
- Multi-version upgrade matrix testing
- Upgrade hook and CRD patching validation
- K8s only (Helm-based, not OpenShift OLM)

**Test files**:

- `tests/pytests/k8/gpu-operator/upgrade/*.py`

**Job config**: `tests/jobs/upgrade/.job.yml`

**Example**:

```bash
/pytest-upgrade-dev Implement upgrade from v1.5.0 to RC
/pytest-upgrade-dev Debug helm upgrade hook failure
```

**Note**: K8s-only, not applicable to OpenShift OLM

---

### Planning

#### `/test-plan-dev`

**File**: `test-plan-dev.md`

Generates comprehensive test plans from Product Requirement Documents (PRDs).

**Use cases**:

- Analyze PRDs and extract testable requirements
- Create structured test plans with scenario mapping
- Define test coverage matrix and priorities
- Identify test data and environment requirements
- Generate reviewable test plan documents

**Workflow**: Use this FIRST before implementing tests

**Example**:

```bash
/test-plan-dev Generate test plan from PRD at prds/2026/Q2/PRD-GPU-20260406-01.md
```

---

## Component-to-Skill Mapping

| GPU Operator Component          | Skill                 | Test Location                                      |
|---------------------------------|-----------------------|----------------------------------------------------|
| Device Config Manager (DCM)     | `/pytest-dcm-dev`     | `k8/gpu-operator/test_config_manager.py`           |
| Device Metrics Exporter (DME)   | `/pytest-dme-dev`     | `k8/gpu-operator/test_metrics_exporter.py`         |
| Node Problem Detector (NPD)     | `/pytest-npd-dev`     | `k8/gpu-operator/test_node_problem_detector.py`    |
| Auto Node Remediation (ANR)     | `/pytest-anr-dev`     | `k8/gpu-operator/test_node_remediation.py`         |
| ROCm/AMDGPU Driver              | `/pytest-driver-dev`  | `k8/gpu-operator/test_driver_deviceplugin.py`      |
| **Operator & Operand Upgrades** | `/pytest-upgrade-dev` | `k8/gpu-operator/upgrade/*.py`                     |
| Generic / Multi-component       | `/pytest-dev`         | Any test file                                      |

## Workflow

The typical development workflow:

1. **Planning** → `/test-plan-dev` from PRD
2. **Review** → Human approval of test plan
3. **Select Component Skill** → Choose based on component being tested:
   - DCM partition tests → `/pytest-dcm-dev`
   - Metrics exporter tests → `/pytest-dme-dev`
   - NPD integration tests → `/pytest-npd-dev`
   - ANR workflow tests → `/pytest-anr-dev`
   - Generic/multi-component → `/pytest-dev`
4. **Implementation** → Use selected skill to implement tests from approved plan
5. **Debugging** → Use same component skill with job log reference

**Example Full Workflow**:

```bash

# Step 1: Generate test plan

/test-plan-dev Generate test plan from PRD at .claude/knowledge/prds/2026/Q2/PRD-GPU-20260406-01.md

# Step 2: Human reviews and approves test plan

# Step 3: Implement tests for specific component

/pytest-dme-dev Implement metric validation tests from approved plan at test-plan.md

# Step 4: Debug failures if needed

/pytest-dme-dev Debug metric comparison failures in job log 30158283
```

## Creating New Skills

To add a new skill:

1. Create a new `.md` file with frontmatter:

```markdown
---
name: skill-name
description: Brief description
---

# Skill Name

Instructions for Claude...
```

1. Add to this README
2. Commit to make available project-wide

## Invoking Skills

Skills are invoked with the `/` prefix:

```bash
/skill-name [arguments]
```

Claude Code will load the skill and execute according to its instructions.
