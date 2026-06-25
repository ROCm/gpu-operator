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

### Test Execution

#### `/run-tests`

**File**: `run-tests.md` | **Component**: Test Launcher

Run GPU Operator pytest test suites against a cluster using `k8_test_launcher.sh`.

**Use cases**:

- Execute full test suites against K8s/OpenShift clusters
- Run specific test modules or individual test cases
- Generate test reports (HTML/XML) for CI/CD integration
- Collect tech-support on test failures
- Test with specific driver versions or workloads

**Example**:

```bash
# Run all GPU operator tests
/run-tests gpu-operator --manifest tests/pytests/image-manifest/mi350_collab_images.yaml

# Run specific module
/run-tests gpu-operator --manifest <manifest> --module installation

# Run with debug mode
/run-tests gpu-operator --manifest <manifest> --module metrics --testcase values --debug
```

**Output**: Test reports in `logs/` directory (HTML, XML, test logs)

---

### Test Analysis & Triage

Component-focused analysis skills that perform deep correlation and root cause analysis:

#### `/analyze-dme`

**File**: `analyze-dme.md` | **Component**: Device Metrics Exporter

Deep analysis of Device Metrics Exporter issues by correlating AMD-SMI output, gpuagent connectivity, and exporter endpoint metrics.

**Use cases**:

- Diagnose missing metrics (temperature, activity, power, etc.)
- Correlate AMD-SMI vs exporter output to identify data flow breaks
- Check gpuagent socket connectivity
- Identify product bugs vs configuration issues vs test bugs
- Validate metrics against metrics-support.json

**Analysis Flow**: AMD-SMI → GPUAgent → Exporter → Prometheus Endpoint

**Example**:

```bash
# Analyze CI run
/analyze-dme --target-id 30883642

# Analyze local test
/analyze-dme --logs-path tests/pytests/logs/

# With verbose output
/analyze-dme --verbose --output dme-report.json
```

**Output**: Root cause categorization (PRODUCT_BUG | CONFIG_ISSUE | TEST_BUG | HEALTHY)

---

#### `/analyze-npd` *(Planned)*

**Component**: Node Problem Detector

Analyze NPD failures:
- ConfigMap validation
- Problem detection rules
- Kubernetes event reporting
- Metric endpoint health

---

#### `/analyze-anr` *(Planned)*

**Component**: Auto Node Remediation

Analyze ANR workflow failures:
- Argo Workflow execution
- Remediation action success/failure
- NHC integration
- Custom remediation templates

---

#### `/analyze-driver` *(Planned)*

**Component**: ROCm/AMDGPU Driver

Analyze driver failures:
- KMM worker issues
- Driver loading/unloading
- Version compatibility
- Firmware issues

---

#### `/triage`

**File**: `triage.md`

Categorize test failures as product bugs, infrastructure issues, or test code bugs.

**Use cases**:

- Triage all failures from a test run
- Categorize: PRODUCT_BUG | INFRASTRUCTURE_ISSUE | TEST_BUG
- Generate bug reports for product issues
- Identify test improvements needed

**Input**: Results from component analysis skills (`/analyze-dme`, etc.)

**Example**:

```bash
# Triage all failures from CI run
/triage --target-id 30883642

# Triage with component analysis
/triage --logs-path tests/pytests/logs/ --deep-analysis
```

**Output**: Categorized failures with root causes and next steps

---

### Release Engineering

#### `/gen-image-manifest`

**File**: `gen-image-manifest.md` | **Component**: Image Manifests

Generate or update image manifest YAML files with latest builds from assets-hq.pensando.io.

**Use cases**:

- Update existing manifest with latest RC builds for a branch
- Generate new manifest for a new release branch
- Query build servers for latest gpu-operator, device-metrics-exporter, device-config-manager, kernel-module-management builds

**Manifest files**: `tests/pytests/image-manifest/*.yaml`

**Example**:

```bash
# Update existing manifest
/gen-image-manifest tests/pytests/image-manifest/mi350_collab_images.yaml --branch collab-7.12

# Generate new manifest
/gen-image-manifest tests/pytests/image-manifest/v1.6.0_rc_images.yaml --branch v1.6.0
```

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

### Hardware / BMC

#### `/bmc-einj-enable`

**File**: `bmc-einj-enable/SKILL.md` | **Component**: BMC / RAS

Enable EINJ (Error Injection) on AMD GPU OAM slots via BMC Redfish API. Pre-requisite for `amdgpuras` RAS error injection testing.

**Use cases**:

- Enable EINJ on SMCI BMC before running `amdgpuras` RAS tests
- Discover which OAM slot(s) support EINJ
- Power cycle the host via Redfish to activate EINJ
- Verify EINJ state after reboot

**Supported BMC vendors**: SMCI (Supermicro) — other vendors may not expose the OEM endpoint.

**Example**:

```bash
/bmc-einj-enable <BMC_IP> <USERNAME> <PASSWORD>
```

---

#### `/ras-inject-test`

**File**: `ras-inject-test/SKILL.md` | **Component**: RAS / ECC Testing

Run end-to-end RAS error injection tests on AMD GPUs. Injects errors via `amdgpuras`, verifies ECC counters in `amd-smi` (ground truth) and device-metrics-exporter, collects AFID data, and generates a test report.

**Use cases**:

- Validate DME ECC metric accuracy against amd-smi after hardware error injection
- Test all injectable blocks (UMC, SDMA, GFX, MMHUB, PCIe, XGMI) across all GPUs
- Collect AFID data and correlate with `gpu_afid_errors` metric
- Generate structured test reports for Confluence upload

**Prerequisites**: EINJ enabled (`/bmc-einj-enable`), `amdgpuras` installed, DME running on host.

**Example**:

```bash
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD>
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> --release v1.5.2
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> --release v1.5.1 --blocks gfx,mmhub,pcie_bif
```

---

## Component-to-Skill Mapping

| GPU Operator Component          | Development Skill     | Analysis Skill     | Test Location                               |
|---------------------------------|-----------------------|--------------------|---------------------------------------------|
| Device Config Manager (DCM)     | `/pytest-dcm-dev`     | *(planned)*        | `k8/gpu-operator/test_config_manager.py`    |
| Device Metrics Exporter (DME)   | `/pytest-dme-dev`     | `/analyze-dme`     | `k8/gpu-operator/test_metrics_exporter.py`  |
| Node Problem Detector (NPD)     | `/pytest-npd-dev`     | *(planned)*        | `k8/gpu-operator/test_node_problem_detector.py` |
| Auto Node Remediation (ANR)     | `/pytest-anr-dev`     | *(planned)*        | `k8/gpu-operator/test_node_remediation.py`  |
| ROCm/AMDGPU Driver              | `/pytest-driver-dev`  | *(planned)*        | `k8/gpu-operator/test_driver_deviceplugin.py` |
| **Operator & Operand Upgrades** | `/pytest-upgrade-dev` | -                  | `k8/gpu-operator/upgrade/*.py`              |
| Generic / Multi-component       | `/pytest-dev`         | `/analyze-results` | Any test file                               |
| **Test Execution**              | `/run-tests`          | -                  | Runs `k8_test_launcher.sh`                  |
| **Image Manifests**             | `/gen-image-manifest` | -                  | `image-manifest/*.yaml`                     |
| **Failure Triage**              | -                     | `/triage`          | All failures                                |

## Workflow

### Development Workflow

The typical test development workflow:

1. **Planning** → `/test-plan-dev` from PRD
2. **Review** → Human approval of test plan
3. **Select Component Skill** → Choose based on component being tested:
   - DCM partition tests → `/pytest-dcm-dev`
   - Metrics exporter tests → `/pytest-dme-dev`
   - NPD integration tests → `/pytest-npd-dev`
   - ANR workflow tests → `/pytest-anr-dev`
   - Generic/multi-component → `/pytest-dev`
4. **Implementation** → Use selected skill to implement tests from approved plan
5. **Execution** → `/run-tests` to run tests against cluster
6. **Debugging** → Use same component skill with job log reference

**Example Development Workflow**:

```bash
# Step 1: Generate test plan
/test-plan-dev Generate test plan from PRD at .claude/knowledge/prds/2026/Q2/PRD-GPU-20260406-01.md

# Step 2: Human reviews and approves test plan

# Step 3: Implement tests for specific component
/pytest-dme-dev Implement metric validation tests from approved plan at test-plan.md

# Step 4: Debug failures if needed
/pytest-dme-dev Debug metric comparison failures in job log 30158283
```

---

### Analysis Workflow

When tests fail in CI, use component-focused analysis skills:

1. **Generic Analysis** → `/analyze-results --target-id <id>` 
   - Parses test reports, extracts failures
   - Identifies which components failed
   - Routes to component-specific analysis

2. **Component Deep-Dive** → Use focused skill based on failing component:
   - DME failures → `/analyze-dme --target-id <id>`
   - NPD failures → `/analyze-npd --target-id <id>` *(planned)*
   - ANR failures → `/analyze-anr --target-id <id>` *(planned)*
   - Driver failures → `/analyze-driver --target-id <id>` *(planned)*

3. **Triage** → `/triage --target-id <id>`
   - Uses component analysis results
   - Categorizes as: PRODUCT_BUG | INFRASTRUCTURE_ISSUE | TEST_BUG
   - Generates bug reports and next steps

**Example Analysis Workflow**:

```bash
# Step 1: Download CI logs and quick overview
/analyze-results 30883642

# Output shows: 21 DME failures, 12 NPD failures, 8 test-runner failures

# Step 2: Deep-dive into DME issues
/analyze-dme --target-id 30883642

# Output: ROOT CAUSE - GPUAgent socket not accessible (PRODUCT_BUG)

# Step 3: Triage all failures
/triage --target-id 30883642

# Output: Categorized failures with bug report templates
```

**Why Component-Focused Analysis?**

Generic `/analyze-results` finds "21 metrics tests failed" but doesn't know WHY.

Component-focused `/analyze-dme`:
- ✅ Checks AMD-SMI output (GPU layer)
- ✅ Checks gpuagent connectivity (collection layer)
- ✅ Checks exporter endpoint (exposition layer)
- ✅ Correlates with metrics-support.json (expected vs actual)
- ✅ Identifies exact root cause: "gpuagent socket not available"
- ✅ Categorizes: PRODUCT_BUG vs CONFIG_ISSUE vs TEST_BUG

**Each component skill is an expert** with deep domain knowledge of that subsystem!

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
