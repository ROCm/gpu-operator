# Cluster Validator - GPU Operator Feature PRD

**PRD ID**: PRD-GPU-OPERATOR-20260406-01  
**Author**: Claude  
**Date**: 2026-04-06  
**Status**: Draft

## 1. Feature Overview

The Cluster Validator is a Kubernetes-native diagnostic feature that validates whether the AMD GPU Operator is correctly deployed and configured. Triggered via DeviceConfig annotations, the validator runs as a Kubernetes Job that compares the actual deployed state of GPU Operator components against the desired state, identifying and reporting discrepancies directly in the DeviceConfig status.

**User Benefits**:
- On-demand validation via simple annotation (no CLI tool needed)
- Quickly diagnose GPU Operator deployment issues
- Validate configuration consistency across cluster nodes
- Independent validator releases without redeploying GPU Operator
- Results stored in DeviceConfig status for easy access
- Kubernetes-native workflow with Job logs for deep debugging

**Goals**:
- Validate all GPU Operator components (driver, device plugin/DRA, metrics exporter, DCM, test runner, remediation)
- Compare actual deployment state against DeviceConfig spec
- Report missing, misconfigured, or unhealthy components in status subresource
- Support both vanilla Kubernetes and OpenShift platforms
- Enable independent versioning and rapid fixes to validation logic

## 2. Technical Specification

### 2.1 CRD Changes

Add validation configuration and status fields to DeviceConfig CRD.

**Validation Configuration** (in spec):
- `image` - Validator container image (default: latest)
- `imagePullPolicy` - Pull policy for validator image
- `ttlSecondsAfterFinished` - How long to keep Job after completion for log inspection (default: 1800s / 30min)
- `activeDeadlineSeconds` - Max validation time before Job is killed (default: 600s / 10min)

**Validation Status** (in status):
- `requestedAt` - Annotation value that triggered validation
- `state` - InProgress, Completed, Failed
- `jobName` - Validation Job name (for log access)
- `startedAt`, `completedAt` - Timestamps
- `status` - healthy, degraded, warning, failed
- `components[]` - Per-component validation results with checks and issues
- `recommendations[]` - Actionable next steps

See complete type definitions in Appendix A.

### 2.2 Platform Support
- Vanilla Kubernetes: Yes
- OpenShift: Yes

### 2.3 Triggering Validation

Users trigger validation by adding/updating the `gpu.amd.com/validate` annotation:

```bash
# Trigger with timestamp (recommended)
kubectl annotate deviceconfig gpu-config \
  gpu.amd.com/validate="$(date -u +%s)" --overwrite

# Or any unique string
kubectl annotate deviceconfig gpu-config \
  gpu.amd.com/validate="request-1" --overwrite
```

The annotation value must change to trigger a new validation.

### 2.4 Architecture

**Workflow**:
1. User adds/updates annotation
2. Controller detects change, creates validation Job
3. Controller updates status.validation.state = "InProgress"
4. Validator Job runs checks
5. Validator updates DeviceConfig status with results
6. Controller monitors Job completion, emits Events
7. Job auto-deletes after TTL

**Key Design**: Validator updates status directly (not via controller) for simplicity and authority.

### 2.5 User Workflow Example

```bash
# 1. Trigger
kubectl annotate dc gpu-config gpu.amd.com/validate="$(date -u +%s)" --overwrite

# 2. Check status
kubectl get dc gpu-config -o jsonpath='{.status.validation.state}'

# 3. View results
kubectl describe dc gpu-config

# 4. Check logs if needed
kubectl logs job/$(kubectl get dc gpu-config -o jsonpath='{.status.validation.jobName}')
```

### 2.6 Validation Checks

**DeviceConfig Validation**:
- CR exists, spec valid
- No conflicting configurations (DevicePlugin + DRADriver both enabled)

**Per-Component** (Driver, DevicePlugin, DRADriver, MetricsExporter, ConfigManager, TestRunner, RemediationWorkflow):
1. **Resource Existence**: DaemonSet/Deployment, ConfigMaps, Secrets, Services exist
2. **Configuration Match**: Image, tolerations, node selectors, resources, args match spec
3. **Deployment Health**: Pod status, readiness, events, node coverage
4. **Node-Specific**: Driver status, node labels, GPU resources advertised, kernel modules loaded

**Cross-Component**:
- Mutual exclusion (only DevicePlugin OR DRADriver)
- Dependencies (NFD, KMM, Argo installed)
- Integration points functional

## 3. Controller Changes

Controller watches for annotation changes and manages validation Jobs.

**Responsibilities**:
1. Detect new validation requests (annotation changed)
2. Create Job with user configuration
3. Update status to "InProgress"
4. Monitor Job completion
5. Emit Events
6. Job auto-cleanup via TTL

**RBAC**: Controller needs `batch/jobs` create, get, list, watch, delete

## 4. Implementation Plan

### 4.1 File Checklist

**CRD**:
- [ ] `api/v1alpha1/deviceconfig_types.go` - Add ValidationSpec, ValidationStatus types
- [ ] `config/crd/bases/amd.com_deviceconfigs.yaml` - Regenerate

**Controller**:
- [ ] `internal/controller/deviceconfig_controller.go` - Job handling
- [ ] `internal/controller/deviceconfig_validation.go` - Validation logic

**Validator**:
- [ ] `cmd/validator/main.go`
- [ ] `internal/validator/validator.go`
- [ ] `internal/validator/checks/deviceconfig.go`
- [ ] `internal/validator/checks/driver.go`
- [ ] `internal/validator/checks/deviceplugin.go`
- [ ] `internal/validator/checks/dra.go`
- [ ] `internal/validator/checks/metrics.go`
- [ ] `internal/validator/checks/configmanager.go`
- [ ] `internal/validator/checks/testrunner.go`
- [ ] `internal/validator/checks/remediation.go`
- [ ] `internal/validator/checks/dependencies.go`
- [ ] `internal/validator/status.go`

**RBAC & Deploy**:
- [ ] `config/rbac/validator_role.yaml`
- [ ] `config/rbac/validator_role_binding.yaml`
- [ ] `config/rbac/validator_service_account.yaml`
- [ ] `Dockerfile.validator`
- [ ] `Makefile` - Build targets

**Tests**:
- [ ] `tests/e2e/validator_test.go`
- [ ] `tests/unit/validator/`

**Docs**:
- [ ] `docs/validator.md`

### 4.2 Implementation Phases

1. **CRD & Controller** (Week 1) - Types, annotation watching, Job creation
2. **Validator Core** (Week 2) - Binary, client, status updates
3. **Component Validation** (Week 3-4) - All component validators
4. **Advanced Checks** (Week 5) - Dependencies, recommendations
5. **Testing** (Week 6) - Unit, integration, E2E
6. **Docs & Release** (Week 7)

## 5. Testing Requirements

**E2E Scenarios**:
1. Healthy cluster
2. Missing component
3. Configuration drift (image mismatch)
4. Degraded component (CrashLoopBackOff)
5. Partial deployment (node selector mismatch)
6. Conflicting configuration
7. Dependency missing
8. Job timeout
9. Concurrent validations
10. Custom validator image
11. Job retention and auto-delete

## 6. Documentation Updates

**New**:
- `docs/validator.md` - User guide (annotation triggers, reading results, config, logs)
- `docs/validator-development.md` - Developer guide

**Updated**:
- `docs/troubleshooting-guide.md` - Validation first step
- `README.md` - Features list
- `docs/api-reference.md` - New fields

## 7. Future Enhancements

- Scheduled validation (CronJob)
- Auto-remediation
- Enhanced checks (performance, CVEs)
- Multi-cluster validation
- Web UI

## 8. Success Metrics

- 50% reduction in time-to-diagnose
- 80% support team adoption
- <5% false positive rate
- 2x independent validator releases vs operator
- 4.5/5 user satisfaction

## 9. Open Questions

1. Support standalone CLI mode?
2. Store validation history?
3. Validation severity levels?
4. CVE checking?
5. Custom plugins?
6. Default TTL (30min, 1hr, 24hr)?

## 10. Dependencies

- Kubernetes client-go
- controller-runtime
- ServiceAccount with RBAC
- Container registry

## 11. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| False positives | Extensive testing, conservative rules |
| Version skew | Compatibility matrix, graceful degradation |
| RBAC restrictions | Document permissions, troubleshooting |
| Hung validation | activeDeadlineSeconds timeout |
| Large cluster performance | Parallel checks, pagination, caching |
| Platform edge cases | Detection, conditional logic, testing |
| Status conflicts | Retry with backoff |
| Excessive retention | Reasonable default TTL, docs |



---

## Appendix A: Complete Type Definitions

See implementation section above for complete Go type definitions of ValidationSpec and ValidationStatus.

## Appendix B: Key Design Decisions

1. **Annotation-based triggering** - Kubernetes-native, no CLI needed
2. **Job-based execution** - Independent validator releases, isolated execution
3. **Direct status updates** - Validator owns validation results
4. **Configurable Job retention** - Balance debuggability with cluster cleanliness
5. **No validation history in status** - Use Kubernetes Events for history

## Appendix C: Comparison with Original Design

| Aspect | Original (CLI) | New (Job-based) |
|--------|----------------|------------------|
| Trigger | Manual CLI execution | Annotation on DeviceConfig |
| Results | stdout (table/JSON) | DeviceConfig status |
| Logs | N/A | Job pod logs (retained per TTL) |
| Versioning | Coupled with operator | Independent validator image |
| Automation | Difficult | Easy (CronJob, scripts) |
| Debugging | Limited | Full (logs, Job inspection) |

