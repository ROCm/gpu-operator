# Auto Remediation of GPU nodes

The GPU Operator provides automatic remediation for GPU worker nodes that become unhealthy due to GPU-related issues. When such problems are detected, the operator triggers a workflow—a series of automated steps designed to restore the node to a healthy state. This functionality is powered by Argo Workflows, a lightweight and scalable open-source workflow engine for Kubernetes. Through the DeviceConfig Custom Resource, the GPU Operator offers extensive customization options for configuring remediation behavior.

## Auto-Remediation Workflow Overview

The following diagram illustrates the end-to-end flow of automatic remediation:

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                           GPU Worker Node                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────┐                                                 │
│  │ Device Metrics         │                                                 │
│  │ Exporter               │  Reports inband-RAS errors                      │
│  └───────────┬────────────┘                                                 │
│              │                                                              │
│              ▼                                                              │
│  ┌────────────────────────┐                                                 │
│  │ Node Problem           │  Queries for inband-RAS errors                  │
│  │ Detector (NPD)         │  and marks node condition as True               │
│  └───────────┬────────────┘                                                 │
│              │                                                              │
└──────────────┼──────────────────────────────────────────────────────────────┘
               │
               │ Node condition status update
               ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Controller Node                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────┐                                                 │
│  │ GPU Operator           │  Observes node error conditions                 │
│  │                        │                                                 │
│  └───────────┬────────────┘                                                 │
│              │                                                              │
│              ▼                                                              │
│  ┌────────────────────────┐                                                 │
│  │ Argo Workflow          │  Triggers remediation workflow                  │
│  │ Controller             │  for the affected node                          │
│  └────────────────────────┘                                                 │
│              │                                                              │
└──────────────┼──────────────────────────────────────────────────────────────┘
               │
               │ Executes remediation steps
               ▼
        Affected GPU Worker Node
```

The Node Problem Detector (NPD) maintains a unique node condition for each error type, enabling users to configure different remediation actions tailored to specific error conditions.

> **Note:** The GPU Operator prevents multiple concurrent workflows on the same node. When a node is tainted and a workflow is already executing, no additional workflows will be triggered on that node until the current workflow completes.

## Pre-requisites

Automatic node remediation requires the following components to be enabled and running on the cluster:

1. **Device Metrics Exporter** - Reports unhealthy metrics and inband-RAS errors that are used to detect faulty GPUs.
2. **Node Problem Detector (NPD)** - An open-source Kubernetes component that runs on all nodes to identify node issues and report them to upstream controllers in the Kubernetes management stack. For more information about NPD configuration, see the [NPD documentation](../npd/node-problem-detector.md).

## Installation

The GPU Operator Helm installation includes the following Argo Workflows components:

1. Argo workflow controller (deployed as a Kubernetes deployment)
2. Argo CRDs for defining workflow templates and workflows

The GPU Operator installs Argo Workflows v3.6.5, using a [customized installation YAML](https://github.com/argoproj/argo-workflows/releases/download/v3.6.5/install.yaml) tailored for auto-remediation requirements. This customization excludes components not needed for remediation, such as the Argo workflow server. For more information about Argo Workflows concepts, refer to the [official documentation](https://argo-workflows.readthedocs.io/en/release-3.6/workflow-concepts/).

> **Note:** By default, auto-remediation components (workflow controller and CRDs) are installed during Helm deployment. To disable the installation of these components, use the following Helm flag:
>
> ```bash
> --set remediation.enabled=false
> ```

## Configuration and customization

### Device Config configuration

The DeviceConfig Custom Resource includes a `RemediationWorkflowSpec` section for configuring and customizing the auto-remediation feature:

```yaml
remediationWorkflow:
  # Enable auto node remediation feature for AMD GPU Operator. Disabled by default.
  # Set to true to activate automatic remediation workflows when GPU issues are detected.
  enable: true

  # ConfigMap containing mappings between node conditions and remediation workflows.
  # If not specified, the operator uses the default 'default-conditional-workflow-mappings' ConfigMap.
  # The ConfigMap defines which workflow template to execute for each specific error condition.
  config:
    name: configmapName

  # Time-to-live duration for retaining failed workflow objects and pods before cleanup.
  # Accepts duration strings like "5h", "24h", "30m", "1h30m". Default is 24 hours.
  # Retaining failed workflows allows for post-mortem analysis and troubleshooting.
  ttlForFailedWorkflows: 5h

  # Container image used for executing GPU validation tests during remediation workflows.
  # This image runs test suites to verify GPU health after remediation completes.
  # Default image supports only RVS tests. Contact AMD for AGFHC-enabled test runner.
  testerImage: docker.io/rocm/test-runner:v1.4.1

  # Maximum number of remediation workflows that can execute concurrently across the cluster.
  # Helps maintain minimum node availability by preventing excessive simultaneous remediations.
  # A value of 0 (default) means no limit is enforced. Excess workflows are queued as Pending.
  maxParallelWorkflows: 0

  # Custom taints to apply to nodes during the remediation process.
  # If not specified, the operator applies the default taint 'amd-gpu-unhealthy:NoSchedule'.
  # Taints prevent new workload scheduling on affected nodes during remediation.
  nodeRemediationTaints:
    - key:       # Taint key (e.g., 'amd-gpu-unhealthy')
      value:     # Taint value (e.g., specific error condition)
      effect:    # Taint effect (e.g., 'NoSchedule', 'NoExecute', 'PreferNoSchedule')

  # Custom labels to apply to nodes during automatic remediation workflows.
  # These labels persist throughout the remediation process and can be used for
  # monitoring, tracking, or applying custom policies.
  nodeRemediationLabels:
    label-one-key: label-one-val
    label-two-key: label-two-val

  # Configuration for pod eviction behavior when draining workloads from nodes.
  # Controls how pods are removed during remediation, including timeouts, grace periods,
  # and namespace exclusions to protect critical infrastructure.
  nodeDrainPolicy:
    # Enable forced draining of pods that do not respond to standard termination signals.
    # When true, pods that cannot be evicted gracefully will be forcibly removed.
    force: false

    # Maximum time in seconds to wait for the drain operation to complete.
    # A value of 0 means infinite timeout. Default is 300 seconds (5 minutes).
    timeoutSeconds: 300

    # Grace period in seconds for pods to shut down gracefully after termination signal.
    # Overrides each pod's terminationGracePeriodSeconds. Use -1 to respect pod settings.
    gracePeriodSeconds: 60

    # When true, DaemonSet-managed pods are excluded from the drain operation.
    # DaemonSets are designed to run on all nodes and will automatically reschedule.
    ignoreDaemonSets: true

    # List of namespaces to exclude from pod eviction during drain operation.
    # Pods in these namespaces remain on the node, allowing critical infrastructure
    # components to continue operating throughout the remediation process.
    ignoreNamespaces:
      - kube-system
      - cert-manager
```

**Enable** - Controls whether automatic node remediation is enabled. Set this field to `true` to activate the auto-remediation feature in the cluster.

**Config** - References a ConfigMap that contains mappings between node conditions and their corresponding remediation workflows. The GPU Operator automatically creates a `default-conditional-workflow-mappings` ConfigMap with predefined mappings. Users can either modify this default ConfigMap or create their own custom ConfigMap. If left empty, the default ConfigMap will be used automatically. More about the ConfigMap in [below section](auto-remediation.md#remediation-workflow-configmap).

> **Note:** The `default-conditional-workflow-mappings` ConfigMap is created automatically by the GPU Operator.

**TtlForFailedWorkflows** - Defines the time-to-live (TTL) duration for retaining failed workflow objects and their associated pods before automatic cleanup. This field accepts a duration string in standard formats (e.g., "24h", "30m", "1h30m"). Retaining failed workflows allows for post-mortem analysis and troubleshooting. Once the specified duration expires, the workflow resources are automatically garbage collected by the system. The default retention period is 24 hours.

**TesterImage** - Specifies the container image for executing GPU validation tests during remediation workflows. This image must align with `Spec.TestRunner.Image` specifications and runs test suites to verify GPU health after remediation completion. If unspecified, the default image is `docker.io/rocm/test-runner:v1.4.1`.

> **Note:** The default image supports only RVS test execution. For AGFHC test framework support within workflows, contact your AMD representative to obtain access to the AGFHC-enabled test runner image.

**MaxParallelWorkflows** - Limits the maximum number of remediation workflows that can execute concurrently across the cluster. This setting helps maintain minimum node availability by preventing excessive simultaneous remediation operations. A value of zero (default) means no limit is enforced.

When the number of triggered workflows exceeds this limit, additional workflows are queued by the Argo workflow controller in a **Pending** state. Queued workflows remain pending until an active workflow completes, freeing a slot within the configured parallelism limit.

**NodeRemediationLabels** - Defines custom labels to be applied to nodes during automatic remediation workflows. These labels persist throughout the remediation process and can be used for monitoring, tracking, or applying custom policies.

**NodeRemediationTaints** - Specifies custom taints to be applied to nodes during the remediation process. If no taints are specified, the Operator applies the default taint `amd-gpu-unhealthy:NoSchedule` to prevent workload scheduling on the affected node.

**NodeDrainPolicy** - Configures the pod eviction behavior when draining workloads from nodes during the remediation process. This policy controls how pods are removed, including timeout settings, grace periods, and namespace exclusions. See the [Node Drain Policy Configuration](#node-drain-policy-configuration) section below for detailed field descriptions.

**Spec.CommonConfig.UtilsContainer** - Remediation workflow uses a utility image for executing the steps. Specify the utility image in `Spec.CommonConfig.UtilsContainer` section of Device Config. If the UtilsContainer section is not specified, default image used is `docker.io/rocm/gpu-operator-utils:latest`

#### Node Drain Policy Configuration

The `NodeDrainPolicy` field accepts a `DrainSpec` object with the following configurable parameters:

**Force** - Enables forced draining of pods that do not respond to standard termination signals. When set to `true`, pods that cannot be evicted gracefully will be forcibly removed. Default value is `false`.

**TimeoutSeconds** - Specifies the maximum time in seconds to wait for the drain operation to complete before giving up. A value of zero means infinite timeout, allowing the drain operation to continue indefinitely. Default value is `300` seconds (5 minutes).

**GracePeriodSeconds** - Defines the grace period in seconds that Kubernetes allows for a pod to shut down gracefully after receiving a termination signal. This value overrides the pod's configured `terminationGracePeriodSeconds`. A value of `-1` uses each pod's own grace period setting. Default value is `-1`.

**IgnoreDaemonSets** - When set to `true`, DaemonSet-managed pods are excluded from the drain operation. This is typically desired since DaemonSets are designed to run on all nodes and will automatically reschedule on the same node. Default value is `true`.

**IgnoreNamespaces** - Defines a list of namespaces to exclude from pod eviction during the drain operation. Pods running in these namespaces will remain on the node, allowing critical infrastructure components to continue operating throughout the remediation process. By default, the following namespaces are excluded: `kube-system`, `cert-manager`, and the GPU Operator's namespace.

### Other Configuration options

**NPD Configuration** - NPD configuration is explained in more detail [in this section](../npd/node-problem-detector.md). The Node Problem Detector (NPD) DaemonSet must continue running during workflow execution to verify issue resolution. Add the following toleration to the NPD DaemonSet:

  `amd-gpu-unhealthy:NoSchedule op=Exists`

The GPU Operator automatically applies this toleration to internal components such as KMM and metrics-exporter, ensuring they continue running during workflow execution.

**Failed Workflow Handling** - If a remediation workflow fails, the affected node remains in a tainted state. To manually restore the node to a schedulable state for workloads, remove the taint using the following command:

  ```bash
  kubectl taint node <node-name> amd-gpu-unhealthy:NoSchedule-
  ```

## Remediation Workflow ConfigMap

The AMD GPU Operator automatically generates a default ConfigMap (`default-conditional-workflow-mappings`) derived from the latest AMD Service Action Guide. This ConfigMap establishes mappings between unique error codes (AFID) and their associated remediation workflows. Each mapping entry defines the error type, the workflow template to invoke for remediation, and workflow-specific parameters. The default ConfigMap is available in the [GPU Operator repository](https://github.com/ROCm/gpu-operator/blob/main/internal/controllers/remediation/configs/default-configmap.yaml) and includes all node conditions managed by the Operator by default.

### Example Error Mapping Section

The following example demonstrates a complete error mapping configuration:

```yaml
- nodeCondition: AMDGPUXgmi
  workflowTemplate: default-template
  validationTestsProfile:
    framework: AGFHC
    recipe: all_lvl4
    iterations: 1
    stopOnFailure: true
    timeoutSeconds: 4800
  physicalActionNeeded: true
  notifyRemediationMessage: Remove GPU tray from node.Confirm that all four screws on all eight OAMs are torqued as described in OAM Removal and Installation guideRe-install the GPU tray into node.
  notifyTestFailureMessage: 'Remove the failing UBB assembly and return to AMD, along with the relevant failure details: at a minimum this should be the RF event that indicated the original fail, and if that RF event includes an additional data URI, the CPER and/or the decoded JSON from the CPER as pointed by the additional data.Install a new or known-good UBB assembly to the GPU tray.'
  recoveryPolicy:
    maxAllowedRunsPerWindow: 3
    windowSize: 15m
```

### ConfigMap Field Descriptions

**nodeCondition** - Specifies a unique description for an error code (AFID). This value must match the corresponding node condition defined in the Node Problem Detector (NPD) configuration.

**workflowTemplate** - Defines the Argo Workflows template to execute for this specific error condition. The `default-template` is used by default and provides comprehensive remediation steps (detailed below). While users can create and reference custom Argo workflow templates in the cluster, it is recommended to use the operator-managed `default-template` for consistency and maintainability.

**validationTestsProfile** - Specifies the test framework and test suite to execute for validating GPU health after remediation. Supported frameworks include AGFHC and RVS. All fields under `validationTestsProfile` are mandatory and correspond to the parameters documented in the [Test Runner Documentation](../test/manual-test.md).

**physicalActionNeeded** - Indicates whether manual physical intervention is required on the node (e.g., RMA of faulty GPU, hardware inspection, etc.). Specific actions are detailed in the `notifyRemediationMessage` field for each error condition. For issues resolved by a reboot, this field is set to `false`.

**notifyRemediationMessage** - Provides detailed instructions for physical or manual actions when `physicalActionNeeded` is `true`. This message guides administrators through the required remediation steps to resolve the fault.

**notifyTestFailureMessage** - Contains instructions to be displayed when validation tests fail after remediation attempts. This message typically includes escalation procedures and diagnostic information requirements.

**recoveryPolicy** - Defines limits on remediation attempts to prevent excessive recovery cycles. Includes `maxAllowedRunsPerWindow` (maximum retry attempts) and `windowSize` (time window for counting attempts). When exceeded, the workflow pauses for manual intervention.

**skipRebootStep** - Controls whether the node reboot step is executed during the remediation workflow. The default workflow template includes an automatic reboot step to reinitialize GPU hardware after performing the recommended remediation actions. Set this field to `true` to skip the reboot step when the node has already been rebooted manually as part of the remediation process or when a reboot is not desired for the specific error condition. Default value is `false`.

## Default Workflow Template

> **Note:** The `default-template` is automatically created on the cluster by the GPU Operator.

The `default-template` workflow performs the following remediation steps:

1. **Label Node** - Applies custom labels to the node as specified in the `NodeRemediationLabels` field of the DeviceConfig Custom Resource. If no labels are configured, this step is skipped and the workflow proceeds to the next step.

2. **Taint Node** - Apply taint with `key = "AMD_GPU_Unhealthy", op = equal, value = node_condition, effect = noSchedule` to prevent new workload scheduling.

3. **Drain Workloads** - Evict all pods utilizing AMD GPUs from the affected node.

4. **Notify Administrator** - Send notification if manual intervention is required for the detected issue.

5. **Suspend Workflow** - Pause workflow execution pending manual intervention or automatic resumption based on configured policies.

6. **Reboot Node** - Perform node reboot to clear transient errors and reinitialize GPU hardware.

7. **Validate GPUs** - Execute AGFHC/RVS validation tests to confirm GPU health after reboot.

8. **Verify Condition** - Confirm that the triggering node condition has been resolved (status changed to False).

9. **Remove Taint** - Remove the node taint to restore GPU availability for workload scheduling.

10. **Remove Labels** - Removes all custom labels that were applied to the node in Step 1, restoring the node to its original label state.

Each workflow step is executed as a separate Kubernetes pod. For advanced use cases, users can create custom workflow templates using the Argo CRDs available on the cluster and reference them in the ConfigMap.

While most workflow steps are self-explanatory, Steps 4, 5, and 7 require additional clarification.

### Workflow Step 4: Physical Intervention Check

According to the AMD service action guide, certain GPU issues require physical intervention (e.g., checking wiring, securing screws, retorquing connections). When such conditions are detected, the workflow generates a Kubernetes event to notify the administrator of the required physical action before suspending at this step. The specific physical action for each node condition is defined in the `physicalActionNeeded` field within the corresponding ConfigMap mapping.

This step enables administrators to identify nodes awaiting physical intervention. After completing the necessary physical repairs, administrators can resume the workflow for validation using the label described in Workflow Step 4.

### Workflow Step 5: Workflow Suspension and Resumption

The GPU Operator determines whether to automatically resume the workflow after it pauses in Step 4. This pause accommodates scenarios requiring manual intervention. The workflow may remain suspended in two primary cases:

1. **Excessive Remediation Attempts:**
   When a `RecoveryPolicy` is configured in the `ConditionalWorkflowMappings` ConfigMap, it defines the maximum remediation attempts allowed within a specified time window. Nodes exceeding this threshold will have their workflows paused indefinitely until manual resumption.
2. **Physical Action Required:**
   When a physical action is specified for a workflow in the `ConditionalWorkflowMappings` ConfigMap, the workflow pauses at this step, allowing administrators to perform the required maintenance. A notification event is generated to alert the user.

If neither condition applies, the workflow automatically resumes without manual intervention.

#### Resuming a Paused Workflow

To resume a suspended workflow, apply the label `operator.amd.com/gpu-force-resume-workflow=true` to the affected node. The operator detects this label and resumes workflow execution.

To abort the workflow entirely, apply the label `operator.amd.com/gpu-abort-workflow=true` to the node. This keeps the node in a tainted state for manual remediation. This option is useful when automatic remediation is no longer desired and the workflow should be deleted while paused.

### Workflow Step 7: GPU Validation Testing

This step executes comprehensive GPU health validation tests using the test runner:

- **Test Profile Configuration:** The test profile for each node condition is specified in the `validationTestsProfile` field within the ConfigMap.

- **Test Execution:** The workflow creates a Kubernetes Job that launches a test runner container. This container retrieves and executes the specified test profile.

- **Result Verification:** The workflow evaluates test results and only proceeds if all tests pass successfully. If any test fails, the entire workflow terminates with a failure status.
