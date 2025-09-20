# Auto Remediation of GPU nodes using Argo Workflows

The GPU Operator supports remediation of GPU worker nodes that have moved into an unhealthy state due to GPU problems by triggering a workflow (set of steps) which attempts to remediate the issue. To achieve this, the GPU Operator makes use of Argo Workflows and its workflow templates. Argo Workflows is a popular open-source workflow engine for Kubernetes. It is lightweight and scalable. The GPU Operator, as part of its helm installation, installs the following:

1) Argo workflow controller as a k8s deployment
2) Argo CRDs for defining workflow templates and workflos

GPU Operator installs Argo v3.6.5

The source yaml to install it is present here: https://github.com/argoproj/argo-workflows/releases/download/v3.6.5/install.yaml

It has been modified to fit the requirements of this feature. For example, the workflow server is not necessary, so it doesn't get deployed as part of the 
GPU Operator-packaged argo installation

## About Workflows and Workflow Templates

The workflow controller is responsible for running a workflow and managing its lifecycle. 

Argo workflows by default uses Kubernetes API Server(etcd) as its database. Once a workflow is triggered, the controller maintains the running state of the workflow and persists in the database. In case workflow controller restarts in between, we still have the state.  

A typical workflow refers a workflow template. A workflow template can either be used to define a specific work, or it can be used to orchestrate a workflow. Each task within a workflow is run inside a container.

Creating a `workflow-template` on the cluster will store the template with its steps in k8s apiserver (etcd) but not trigger any action. 
Creating a `workflow` which invokes a `workflow-template` will store the workflow in k8s apiserver(etcd) and also trigger the actual steps in the template. 
GPU Operator creates the `workflow` which invokes the `workflow-template` to trigger remediation 

## Configuration to be handled by the User

-> Toggling `RemediationWorkflow.Enable` to True. 

-> NPD daemonset is relied upon to verify that the issue is fixed during the workflow run. Hence, user needs to add this toleration to NPD daemonset so that it can continue to be scheduled during the workflow run:

  `amd-gpu-unhealthy:NoSchedule op=Exists`

GPU Operator will handle adding this toleration for in-house components like KMM, metrics-exporter which should stay running during the workflow run

-> If a workflow runs and fails, the node will remain in tainted state. If the user wants to go ahead and make the node schedulable again for workloads, the node should be untainted with:
  `kubectl taint node <node-name> amd-gpu-unhealthy:NoSchedule-`

## How Workflows are triggered

Node problem detector (NPD) can set the node conditions by listening to GPU health reported by device metrics exporter periodically. 
GPU-Operator keeps monitoring the node conditions periodically and creates appropriate workflow based on the node condition status moving to `True`. For example, the below node condition would mean node is in a bad state: 

```yaml
  - lastHeartbeatTime: "2025-08-04T08:56:04Z"
    lastTransitionTime: "2025-08-04T08:56:04Z"
    reason: "Temperature Threshold Exceeded"
    status: "True"
    type: AMDGPUUnhealthy
```

When the status of the node condition is `False`, it means that node condition is currently fine and in good state. 
These are the new fields introduced under the RemediationWorkflow field in the DeviceConfig CR:

```yaml
    type RemediationWorkflowSpec struct {
        // enable remediation workflows. disabled by default
        // enable if operator should automatically handle remediation of node incase of gpu issues
        //+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
        Enable *bool `json:"enable,omitempty"`

        // Name of the ConfigMap that holds condition-to-workflow mappings.
        //+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ConditionalWorkflows",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:conditionalWorkflows"}
        ConditionalWorkflows *v1.LocalObjectReference `json:"conditionalWorkflows,omitempty"`

        // Time to live for argo workflow object and its pods for a failed workflow in hours. By default, it is set to 24 hours
        //+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TtlForFailedWorkflows",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:ttlForFailedWorkflows"}
        // +kubebuilder:default:=24
        TtlForFailedWorkflows int `json:"ttlForFailedWorkflows,omitempty"`
    }
``` 
The mappings are present in the configmap referenced by the ConditionalWorkflows field. 
GPU-Operator will create the `default-conditional-workflow-mappings` configmap on the cluster with some default mappings. The user can modify them if required and can add more mappings as well. If the user wants to use this default configmap, then they may leave the `RemediationWorkflow.ConditionalWorkflows` field empty in the CR. The user can also come up with their own configmap and mention the name of the configmap under `RemediationWorkflow.ConditionalWorkflows` if they do not want to use the default `default-conditional-workflow-mappings` configmap.

Note: `default-conditional-workflow-mappings` will be created on the cluster by GPU-Operator 

```yaml
apiVersion: v1
kind: ConfigMap
data:
  workflow: |-
    - nodeCondition: "AMDGPUUnhealthy"
      workflowTemplate: "default-template"
      notifyMessage: "notification message for admin(if any) to take manual remediation action"
      validationTestsProfile:
        framework: "AGFHC"
        recipe: "all_lvl4"
        iterations: 1
        stopOnFailure: true
        timeoutSeconds: 4800
```

`NodeCondition` field refers to the node condition that the user wants the Operator to watch for and to trigger remediation workflow.

`WorkflowTemplate` will use the default-template in most cases which is discussed below. If user wants to use his own workflow template for a certain node condition, he can create the template in the cluster and mention the name of the template in this field but the recommended way is to let Operator handle it through the default-template.

`notifyMessage` contains remediation instructions for the admin in case the node problem requires manual action. Workflow will trigger a Kubernetes event with the content of **notifyMessage** to alert the admin.

`validationTestsProfile` field refers to the AGFHC/RVS test-profile to be run by the workflow to verify that the problem is fixed. The test-profile will be passed onto testrunner for it to be run.

```yaml
  validationTestsProfile:
    framework: "AGFHC"
    recipe: "all_lvl4"
    iterations: 1
    stopOnFailure: true
    timeoutSeconds: 4800`
 ```

If a user would like to run a testsuite as part of the workflow, these fields under `validationTestsProfile` are mandatory and they correspond to the fields of the same in the [Test Runner Documentation](../test/manual-test.md)

`physicalActionNeeded` field refers to the physical action the user has to take for certain conditions that will not be fixed by a reboot. The action will be mentioned for each of those conditions in the `default-conditional-workflow-mappings`. For conditions where reboot fixes the issue, this field will be left empty. 

This integration works on the basis that NPD applies different node conditions for different critical errors. 

Note: Operator ensures that when a node is tainted and a workflow is already running, we don’t trigger any new workflows on the node.

## Enable auto remediation

To enable this feature, the user needs to toggle `RemediationWorkflow.Enable` to true in the Device Config CR. It is disabled by default.
The most common CR users will be using will be of this form which will use the `default-conditional-workflow-mappings` for ConditionalWorkflows field unless the user wants to create their own configmap.

```yaml
  remediationWorkflow:
    enable: true
```

## Default Workflow Template

Note: `default-template` will be created on the cluster by GPU-Operator 


`default-template` will perform the following steps: 

1. Taint the node with `key = "AMD_GPU_Unhealthy”, op = equal, value = node_condition, effect = noSchedule `

2. Drain workloads/pods that are using AMD GPUs 

3. Notify admin/user if manual intervention is required

4. Suspend workflow

5. Reboot the node 

6. Run AGFHC/RVS tests to verify the GPUs are healthy post reboot. 

7. Verify that the node condition has become False 

8. Un-taint the node and this will make the GPUs available for scheduling again. 

For each step in the workflow template, a pod is spun up that performs the task.
For the case when user wants to create his own template, the argo CRDs are present on the cluster and the user can create any workflow template and refer it in the config-map.

Most steps in the default-template are self-explanatory. However, there are some details to be known about Step 2, 3 and 6 

## Workflow Step 2: Check if physical intervention is required 

As per AMD service action guide, many problems require user to intervene physically (checking wiring, screws, retorquing, etc.). The workflow, as per this, will raise a k8s event to suggest the physical action required to the user in such cases before suspending the workflow in step3. If a physical action is needed for a certain node condition, it will be present in the `physicalActionNeeded` field in the configmap mapping corresponding to that node condition. 

The benefit of having this step is that admin can see which node is waiting for physical intervention. Once he fixes it physically, he can simply resume the workflow for validation using the label mentioned in Workflow Step3. 

## Workflow Step 3: Suspend/Resume the Workflow

The GPU-Operator determines whether to resume the workflow after it has been paused in Step 2. This pause provides an opportunity for users to perform necessary manual actions. There are two primary scenarios where user intervention may be required:

1. **Excessive Node Remediation:**  
	Users can define a `RecoveryPolicy` in the `ConditionalWorkflowMappings` ConfigMap, specifying the maximum number of recovery attempts allowed within a given time window. If a node exceeds this limit, the workflow remains paused.
2. **Physical Action Required:**
	If a physical action is specified for a workflow in the `ConditionalWorkflowMappings` ConfigMap, the node will pause at this step, allowing the user to perform the required action. The user is also notified via an event.

If neither of these conditions apply, the workflow will automatically resume from this step.

### Resuming a paused workflow
Whenever the user is satisfied that the workflow can be resumed, they can add the label `operator.amd.com/gpu-force-resume-workflow=true` to the relevant node. The operator will detect this label and resume the workflow.

To abort the workflow, label the node with `operator.amd.com/gpu-abort-workflow=true`. The node will remain in a tainted state for manual intervention. If remediation is no longer desired, this label provides the option to delete the workflow while the node is paused.

## Workflow Step 6: Run AGFHC/RVS tests
 
-> The user will mention the test-profile to pass to test runner to run in the configmap for each condition under `validationTestsProfile`

-> The workflow step will ensure that a k8s job is created which spins up a test runner container which picks up that test-profile to run as part of this step.  

-> The test results will be checked by the workflow step and will ensure that the workflow moves ahead only if the tests pass. If the tests fail, the workflow will fail.

#### **Notes**
During helm installation of GPU Operator, by default, installation of remediation components like workflow controller and crds is enabled. If the admin does not require this auto remediation feature and would like to disable the installation of these components, they can simply pass this flag during the helm installation:

  `--set remediation.enabled=false`