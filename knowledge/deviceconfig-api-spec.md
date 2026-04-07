---
name: DeviceConfig API Specification
description: Complete DeviceConfig CRD structure and field semantics for AMD GPU Operator
type: reference
---

# DeviceConfig API Specification

## CRD Definition
**Group**: amd.com  
**Version**: v1alpha1  
**Kind**: DeviceConfig  
**Scope**: Namespaced  
**Short Name**: gpue  
**Status Subresource**: Yes

## API File Location
`api/v1alpha1/deviceconfig_types.go`

## Spec Structure Overview

```go
type DeviceConfigSpec struct {
    Driver              DriverSpec
    MetricsExporter     MetricsExporterSpec
    ConfigManager       ConfigManagerSpec
    DevicePlugin        DevicePluginSpec
    DRADriver           DRADriverSpec
    TestRunner          TestRunnerSpec
    CommonConfig        CommonConfigSpec
    Selector            map[string]string  // Node selector
    RemediationWorkflow RemediationWorkflowSpec
}
```

## Core Spec Fields

### Selector (map[string]string)
- **Purpose**: Determines which nodes this DeviceConfig manages
- **Common values**:
  - `feature.node.kubernetes.io/amd-gpu: "true"` (physical GPUs)
  - `feature.node.kubernetes.io/amd-vgpu: "true"` (virtual GPUs)
- **Note**: Each component can override with its own selector

### Driver Spec
```go
type DriverSpec struct {
    Enable                *bool                       // Default: true
    DriverType            string                      // container|vf-passthrough|pf-passthrough
    Version               string                      // ROCm version (e.g., "6.2.2", "30.20.1")
    Image                 string                      // Driver image repo (no tag)
    ImageRegistrySecret   *v1.LocalObjectReference    // Pull secret
    ImageRegistryTLS      RegistryTLS
    ImageSign             ImageSignSpec               // For secure boot
    ImageBuild            ImageBuildSpec
    Blacklist             *bool                       // Blacklist inbox driver
    UseSourceImage        *bool                       // OpenShift: build from source
    AMDGPUInstallerRepoURL string                     // Radeon repo URL
    UpgradePolicy         *DriverUpgradePolicySpec
    KernelModuleConfig    KernelModuleConfigSpec      // modprobe args
    VFIOConfig            VFIOConfigSpec              // For passthrough types
    Tolerations           []v1.Toleration
}
```

**Key points**:
- `Image`: Don't include tag - operator manages it automatically
- Format: `<distro>-<release>-<kernel>-<driver_version>`
- Example tag: `coreos-416.94-5.14.0-427.28.1.el9_4.x86_64-6.2.2`
- **Cannot change image repo after creation** - must delete/recreate DeviceConfig

### Driver Upgrade Policy
```go
type DriverUpgradePolicySpec struct {
    Enable                *bool                // Default: false
    MaxParallelUpgrades   int                  // 0=unlimited, default: 1
    MaxUnavailableNodes   intstr.IntOrString   // Default: "25%"
    NodeDrainPolicy       *DrainSpec
    PodDeletionPolicy     *PodDeletionSpec
    RebootRequired        *bool                // Default: true
}

type UpgradeState string // Enum tracking per-node upgrade state
// States: NotStarted, Started, InstallInProgress, InstallComplete, 
//         InProgress, Complete, Failed, TimedOut, CordonFailed, etc.
```

### Device Plugin Spec
```go
type DevicePluginSpec struct {
    EnableDevicePlugin           *bool                       // Default: true
    DevicePluginImage            string
    DevicePluginImagePullPolicy  string
    DevicePluginTolerations      []v1.Toleration
    DevicePluginArguments        map[string]string           // CLI flags
    NodeLabellerImage            string
    NodeLabellerImagePullPolicy  string
    NodeLabellerTolerations      []v1.Toleration
    NodeLabellerArguments        []string                    // CLI flags
    EnableNodeLabeller           *bool                       // Default: true
    ImageRegistrySecret          *v1.LocalObjectReference
    UpgradePolicy                *DaemonSetUpgradeSpec
    KubeletSocketPath            string                      // Default: /var/lib/kubelet/device-plugins
    HostNetwork                  *bool
}
```

**DevicePluginArguments** supported keys:
- `resource_naming_strategy`: "single" | "mixed"

**NodeLabellerArguments** supported flags:
- Default enabled: vram, cu-count, simd-count, device-id, family, product-name, driver-version
- Optional: compute-memory-partition, compute-partitioning-supported, memory-partitioning-supported

### DRA Driver Spec
```go
type DRADriverSpec struct {
    Enable              *bool                       // Default: false
    Image               string                      // Default: rocm/k8s-gpu-dra-driver:latest
    ImagePullPolicy     string                      // Always|IfNotPresent|Never
    Tolerations         []v1.Toleration
    ImageRegistrySecret *v1.LocalObjectReference
    UpgradePolicy       *DaemonSetUpgradeSpec
    CmdLineArguments    map[string]string           // Pass flags to gpu-kubeletplugin
    Selector            map[string]string
}

func (d *DRADriverSpec) IsEnabled() bool // Helper method
```

**CRITICAL**: DRA and Device Plugin are mutually exclusive - operator validates this

**CmdLineArguments** examples:
- `cdi-root`: "/etc/cdi"
- `healthcheck-port`: "8080"
- `v`: "4" (verbosity)
- `logging-format`: "json"

### Config Manager Spec
```go
type ConfigManagerSpec struct {
    Enable                   *bool
    Image                    string
    ImagePullPolicy          string
    ImageRegistrySecret      *v1.LocalObjectReference
    Config                   *v1.LocalObjectReference  // ConfigMap with partition profiles
    Selector                 map[string]string
    UpgradePolicy            *DaemonSetUpgradeSpec
    ConfigManagerTolerations []v1.Toleration
}
```

**Config field**: References ConfigMap with DCM config.json
- When omitted/empty: operator mounts "default-dcm-config" and creates if missing
- When set: operator mounts specified ConfigMap (must exist)

### Metrics Exporter Spec
```go
type MetricsExporterSpec struct {
    Enable                   *bool
    Image                    string
    Prometheus               *PrometheusConfig           // ServiceMonitor config
    ImageRegistrySecret      *v1.LocalObjectReference
    ImagePullPolicy          string
    Tolerations              []v1.Toleration
    Port                     int32                       // Default: 5000
    SvcType                  ServiceType                 // ClusterIP|NodePort
    NodePort                 int32                       // 30000-32767
    Config                   MetricsConfig               // ConfigMap for custom metrics
    RbacConfig               KubeRbacConfig              // kube-rbac-proxy sidecar
    Selector                 map[string]string
    UpgradePolicy            *DaemonSetUpgradeSpec
    PodResourceAPISocketPath string                      // Default: /var/lib/kubelet/pod-resources
    Resource                 *v1.ResourceRequirements    // Pod resources
    PodAnnotations           map[string]string
    ServiceAnnotations       map[string]string
    HostNetwork              *bool
}

type PrometheusConfig struct {
    ServiceMonitor *ServiceMonitorConfig
}

type ServiceMonitorConfig struct {
    Enable            *bool
    Interval          string                              // e.g., "30s"
    AttachMetadata    *monitoringv1.AttachMetadata
    HonorLabels       *bool
    HonorTimestamps   *bool
    Labels            map[string]string
    Relabelings       []monitoringv1.RelabelConfig
    MetricRelabelings []monitoringv1.RelabelConfig
    Authorization     *monitoringv1.SafeAuthorization
    BearerTokenFile   string                              // Deprecated
    TLSConfig         *monitoringv1.TLSConfig
}
```

### Test Runner Spec
```go
type TestRunnerSpec struct {
    Enable              *bool
    Image               string
    ImagePullPolicy     string
    Tolerations         []v1.Toleration
    ImageRegistrySecret *v1.LocalObjectReference
    Config              *v1.LocalObjectReference        // Test config ConfigMap
    Selector            map[string]string
    UpgradePolicy       *DaemonSetUpgradeSpec
    LogsLocation        LogsLocationConfig
}

type LogsLocationConfig struct {
    MountPath         string                            // Default: /var/log/amd-test-runner
    HostPath          string                            // Default: /var/log/amd-test-runner
    LogsExportSecrets []*v1.LocalObjectReference       // Cloud storage secrets
}
```

### Remediation Workflow Spec
```go
type RemediationWorkflowSpec struct {
    Enable                   *bool
    Config                   *v1.LocalObjectReference  // Condition-to-workflow mappings
    TtlForFailedWorkflows    string                    // Default: "24h", pattern: duration
    TesterImage              string
    MaxParallelWorkflows     int32                     // Default: 0 (unlimited)
    NodeRemediationTaints    []v1.Taint
    NodeRemediationLabels    map[string]string
    NodeDrainPolicy          *DrainSpec
    AutoStartWorkflow        *bool                     // Default: true
}

type DrainSpec struct {
    Force              *bool      // Default: false
    TimeoutSeconds     int        // Default: 300, 0=infinite
    GracePeriodSeconds int        // Default: -1 (use pod default)
    IgnoreDaemonSets   *bool      // Default: true
    IgnoreNamespaces   []string   // Namespaces to skip draining
}
```

**Config field**: Maps to ConfigMap defining AFID → workflow mappings
- Default: "default-conditional-workflow-mappings" (created by operator)
- Format: Each entry specifies nodeCondition, workflowTemplate, validationTestsProfile, etc.

### Common Config Spec
```go
type CommonConfigSpec struct {
    InitContainerImage   string                        // For operand pods
    ImageRegistrySecrets []v1.LocalObjectReference     // Global pull secrets
    UtilsContainer       UtilsContainerSpec
}

type UtilsContainerSpec struct {
    Image               string
    ImagePullPolicy     string
    ImageRegistrySecret *v1.LocalObjectReference
}
```

**Purpose**: Shared configuration across all components

## Status Structure

```go
type DeviceConfigStatus struct {
    DevicePlugin        DeploymentStatus
    Drivers             DeploymentStatus
    MetricsExporter     DeploymentStatus
    ConfigManager       DeploymentStatus
    RemediationWorkflow DeploymentStatus
    NodeModuleStatus    map[string]ModuleStatus      // Per-node driver status
    Conditions          []metav1.Condition
    ObservedGeneration  int64
}

type DeploymentStatus struct {
    NodesMatchingSelectorNumber int32
    DesiredNumber               int32
    AvailableNumber             int32
}

type ModuleStatus struct {
    ContainerImage     string
    KernelVersion      string
    LastTransitionTime string
    Status             UpgradeState
    UpgradeStartTime   string
    BootId             string
}
```

## Validation Rules

1. **Mutual Exclusion**: DevicePlugin and DRADriver cannot both be enabled
2. **Image patterns**: Validated via regex (see kubebuilder markers in types.go)
3. **Duration patterns**: For TTL fields (e.g., `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`)
4. **Path patterns**: Unix paths for socket/mount paths
5. **Port ranges**: NodePort must be 30000-32767

## Important Behaviors

1. **Default values**: Specified via kubebuilder markers (+kubebuilder:default=...)
2. **Image pull policy**: Defaults to "Always" if `:latest` tag, else "IfNotPresent"
3. **Selector inheritance**: Components can inherit from spec.selector or override
4. **ConfigMap defaults**: DCM and RemediationWorkflow use default ConfigMaps if not specified
5. **Image tag management**: Operator manages driver image tags - users provide repo only

## Upgrade Semantics

- Driver upgrades: Coordinated by WorkerMgr, state tracked in status.nodeModuleStatus
- DaemonSet upgrades: RollingUpdate (default) or OnDelete
- MaxParallelUpgrades: Controls concurrent node upgrades
- MaxUnavailableNodes: Stops upgrades if too many nodes in failed state
