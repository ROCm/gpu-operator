/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the \"License\");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an \"AS IS\" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"

	workflowv1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	RemediationTaintKey        = "amd-gpu-unhealthy"
	DefaultConfigMapSuffix     = "default-conditional-workflow-mappings"
	DefaultTemplate            = "default-template"
	TestRunnerImage            = "registry.test.pensando.io:5000/test-runner:agfhc-latest"
	TestRunnerServiceAccount   = "amd-gpu-operator-test-runner"
	AmdGpuRemediationRequired  = "amd-gpu-remediation-required"
	AmdGpuRemediationSucceeded = "amd-gpu-remediation-succeeded"
	AmdGpuRemediationFailed    = "amd-gpu-remediation-failed"
)

// ConditionWorkflowMapping defines a single condition-to-workflow mapping.
// This is used when parsing the ConfigMap specified in the DeviceConfig.
type ConditionWorkflowMapping struct {
	NodeCondition        string                 `json:"nodeCondition" yaml:"nodeCondition"`
	WorkflowTemplate     string                 `json:"workflowTemplate" yaml:"workflowTemplate"`
	ValidationTests      ValidationTestsProfile `json:"validationTestsProfile" yaml:"validationTestsProfile"`
	PhysicalActionNeeded string                 `json:"physicalActionNeeded" yaml:"physicalActionNeeded"`
	NotifyMessage        string                 `json:"notifyMessage" yaml:"notifyMessage"`
}

type ValidationTestsProfile struct {
	Framework      string `json:"framework" yaml:"framework"`
	Recipe         string `json:"recipe" yaml:"recipe"`
	Iterations     int    `json:"iterations" yaml:"iterations"`
	StopOnFailure  bool   `json:"stopOnFailure" yaml:"stopOnFailure"`
	TimeoutSeconds int    `json:"timeoutSeconds" yaml:"timeoutSeconds"`
}

type remediationMgr struct {
	helper remediationMgrHelperAPI
}

//go:generate mockgen -source=remediation_handler.go -package=controllers -destination=mock_remediation_handler.go remediationMgr
type remediationMgrAPI interface {
	HandleRemediation(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (ctrl.Result, error)
	HandleDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (ctrl.Result, error)
}

func newRemediationMgrHandler(client client.Client, k8sConfig *rest.Config) remediationMgrAPI {
	k8sIntf, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil
	}
	return &remediationMgr{
		helper: newRemediationMgrHelperHandler(client, k8sIntf),
	}
}

/*================================= Remediation Manager APIs===================================*/

// HandleRemediation handles the remediation functionalities for device config
func (n *remediationMgr) HandleRemediation(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (ctrl.Result, error) {
	res := ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}
	logger := log.FromContext(ctx)

	// Don't handle remediation if disabled
	remediationDisabled, err := n.helper.isRemediationDisabled(ctx, devConfig)

	if err != nil {
		return res, err
	}

	if remediationDisabled {
		return ctrl.Result{}, nil
	}

	var configMap *v1.ConfigMap
	if configMap, err = n.helper.createDefaultObjects(ctx, devConfig); err != nil {
		return res, err
	}

	var mappingsList []ConditionWorkflowMapping
	if err = yaml.Unmarshal([]byte(configMap.Data["workflow"]), &mappingsList); err != nil {
		return res, fmt.Errorf("failed to parse workflows from ConfigMap: %w", err)
	}

	mappings := make(map[string]ConditionWorkflowMapping)
	for _, m := range mappingsList {
		mappings[m.NodeCondition] = m
	}

	var errs error
	for _, node := range nodes.Items {
		// Validate node conditions
		mapping, err := n.helper.validateNodeConditions(ctx, devConfig, &node, mappings)
		if err != nil {
			logger.Info(fmt.Sprintf("Node conditions validations for node %s failed with error: %v", node.Name, err))
			continue
		}
		canSchedule := n.helper.isWorkflowSchedulableOnNode(ctx, devConfig, &node, mapping)
		if !canSchedule {
			continue
		}

		createNewWorkflow := n.helper.handleExistingWorkflowsOnNode(ctx, devConfig, &node)
		if !createNewWorkflow {
			continue
		}
		logger.Info(fmt.Sprintf("GPU Condition: %s observed and node: %s is unhealthy. Starting Remediation Workflow: %s", mapping.NodeCondition, node.Name, mapping.WorkflowTemplate))

		// Fetch WorkflowTemplate
		wfTemplate, err := n.helper.getWorkflowTemplate(ctx, mapping.WorkflowTemplate, devConfig.Namespace)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to start remediation workflow %s on node %s", mapping.WorkflowTemplate, node.Name))
			errs = errors.Join(errs, err)
			continue
		}

		// Populate Workflow Object
		wf := n.helper.populateWorkflow(ctx, wfTemplate, &mapping, node.Name, devConfig)

		// Create Workflow
		if err := n.helper.createWorkflow(ctx, wf); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to create remediation workflow %s on node %s", mapping.WorkflowTemplate, node.Name))
			errs = errors.Join(errs, err)
			continue
		}

		logger.Info(fmt.Sprintf("Remediation Workflow for the condition is created successfully on node %s using template %s", node.Name, mapping.WorkflowTemplate))
	}
	logger.Info("Requeue for any node conditions that may be present")
	return res, errs
}

// HandleDelete handles the delete operations during remediation process
func (n *remediationMgr) HandleDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodeList *v1.NodeList) (res ctrl.Result, err error) {

	wfList, err := n.helper.getWorkflowList(ctx, deviceConfig.Namespace)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to list workflows during delete")
		return ctrl.Result{}, err
	}

	for _, wf := range wfList.Items {
		if err := n.helper.deleteWorkflow(ctx, &wf); err != nil {
			log.FromContext(ctx).Error(err, fmt.Sprintf("Failed to delete workflow %s", wf.Name))
		}
		log.FromContext(ctx).Info(fmt.Sprintf("Deleted workflow: %s", wf.Name))
	}

	var cfgMapName string
	if deviceConfig.Spec.RemediationWorkflow.ConditionalWorkflows != nil {
		cfgMapName = deviceConfig.Spec.RemediationWorkflow.ConditionalWorkflows.Name
	} else {
		cfgMapName = deviceConfig.Name + "-" + DefaultConfigMapSuffix
	}
	if err := n.helper.deleteConfigMap(ctx, cfgMapName, deviceConfig.Namespace); err == nil {
		log.FromContext(ctx).Info(fmt.Sprintf("Deleted ConfigMap: %s", cfgMapName))
	}

	return
}

/*=========================================== Remediation Manager Helper APIs ==========================================*/

//go:generate mockgen -source=remediation_handler.go -package=controllers -destination=mock_remediation_handler.go remediationMgrHelperAPI
type remediationMgrHelperAPI interface {
	isRemediationDisabled(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (bool, error)
	resumeSuspendedWorkflow(ctx context.Context, wfName, namespace string) error
	isDriverUpgradeInProgress(devCfg *amdv1alpha1.DeviceConfig, node *v1.Node) bool
	checkIfTaintExists(node *v1.Node, targetTaint v1.Taint) bool
	getWorkflowList(ctx context.Context, namespace string) (*workflowv1alpha1.WorkflowList, error)
	getWorkflowTemplate(ctx context.Context, workflowTemplateName, namespace string) (*workflowv1alpha1.WorkflowTemplate, error)
	getConfigMap(ctx context.Context, configmapName string, namespace string) (*v1.ConfigMap, error)
	deleteConfigMap(ctx context.Context, name, namespace string) error
	createDefaultConfigMap(ctx context.Context, name, namespace string) (*v1.ConfigMap, error)
	createDefaultWorkflowTemplate(ctx context.Context, namespace string) (*workflowv1alpha1.WorkflowTemplate, error)
	createDefaultObjects(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*v1.ConfigMap, error)
	populateWorkflow(ctx context.Context, wfTemplate *workflowv1alpha1.WorkflowTemplate, mapping *ConditionWorkflowMapping, nodeName string, devCfg *amdv1alpha1.DeviceConfig) *workflowv1alpha1.Workflow
	createWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error
	deleteWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error
	validateNodeConditions(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mappings map[string]ConditionWorkflowMapping) (ConditionWorkflowMapping, error)
	isWorkflowSchedulableOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping) bool
	handleExistingWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node) bool
}

type remediationMgrHelper struct {
	client       client.Client
	k8sInterface kubernetes.Interface
}

// Initialize remediation manager helper interface
func newRemediationMgrHelperHandler(client client.Client, k8sInterface kubernetes.Interface) remediationMgrHelperAPI {
	return &remediationMgrHelper{
		client:       client,
		k8sInterface: k8sInterface,
	}
}

func (h *remediationMgrHelper) isRemediationDisabled(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (bool, error) {

	logger := log.FromContext(ctx)
	if devConfig.Spec.RemediationWorkflow.Enable == nil || !*devConfig.Spec.RemediationWorkflow.Enable {
		return true, nil
	}

	podList := &v1.PodList{}
	if err := h.client.List(ctx, podList, client.InNamespace(devConfig.Namespace)); err != nil {
		logger.Error(err, "failed to list pods")
		return false, err
	}

	found := false
	for _, pod := range podList.Items {
		if strings.HasPrefix(pod.Name, "amd-gpu-operator-workflow-controller") {
			found = true
			break
		}
	}

	if !found {
		logger.Info("Workflow controller pod not found. Please check if it was disabled during bringup, skipping remediation")
		return true, nil
	}
	return false, nil
}

func (h *remediationMgrHelper) resumeSuspendedWorkflow(ctx context.Context, wfName, namespace string) error {

	logger := log.FromContext(ctx)
	var wf workflowv1alpha1.Workflow
	if err := h.client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: namespace}, &wf); err != nil {
		return fmt.Errorf("could not fetch workflow: %w", err)
	}

	modified := false
	stages := wf.Status.Nodes
	for wfStageID, wfStage := range stages {
		if wfStage.Type == "Suspend" && wfStage.Phase == "Running" {
			logger.Info(fmt.Sprintf("Workflow %s is suspended. Resuming...", wfName))

			wfStage.Phase = workflowv1alpha1.NodeSucceeded
			wfStage.FinishedAt = metav1.Time{Time: time.Now().UTC()}
			stages[wfStageID] = wfStage
			modified = true
		}
	}
	if !modified {
		logger.Info(fmt.Sprintf("Workflow %q is not in suspended state", wfName))
		return nil
	}

	if err := h.client.Update(ctx, &wf); err != nil {
		return fmt.Errorf("failed to patch suspended node status: %w", err)
	}

	logger.Info(fmt.Sprintf("Workflow %s resumed successfully", wfName))
	return nil
}

func (h *remediationMgrHelper) isDriverUpgradeInProgress(devCfg *amdv1alpha1.DeviceConfig, node *v1.Node) bool {
	// Define the blocked states that indicate an upgrade is in progress
	blockedStates := map[amdv1alpha1.UpgradeState]bool{
		amdv1alpha1.UpgradeStateNotStarted:        true,
		amdv1alpha1.UpgradeStateStarted:           true,
		amdv1alpha1.UpgradeStateInstallInProgress: true,
		amdv1alpha1.UpgradeStateInProgress:        true,
		amdv1alpha1.UpgradeStateRebootInProgress:  true,
	}

	for nodeName, moduleStatus := range devCfg.Status.NodeModuleStatus {
		if nodeName == node.Name {
			if blockedStates[moduleStatus.Status] {
				return true
			}
		}
	}

	return false
}

func (h *remediationMgrHelper) checkIfTaintExists(node *v1.Node, targetTaint v1.Taint) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == targetTaint.Key && t.Effect == targetTaint.Effect {
			return true
		}
	}
	return false
}

func (h *remediationMgrHelper) getWorkflowList(ctx context.Context, namespace string) (*workflowv1alpha1.WorkflowList, error) {
	wfList := &workflowv1alpha1.WorkflowList{}
	if err := h.client.List(ctx, wfList, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}
	return wfList, nil
}

func (h *remediationMgrHelper) getConfigMap(ctx context.Context, configmapName string, namespace string) (*v1.ConfigMap, error) {
	cm := &v1.ConfigMap{}
	err := h.client.Get(ctx, client.ObjectKey{
		Name:      configmapName,
		Namespace: namespace,
	}, cm)
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func (h *remediationMgrHelper) createDefaultConfigMap(ctx context.Context, name string, namespace string) (*v1.ConfigMap, error) {

	workflowYaml := `- nodeCondition: "AMDGPUUnhealthy"
  workflowTemplate: "default-template"
  validationTestsProfile:
    framework: "AGFHC"
    recipe: "all_lvl4"
    iterations: 1
    stopOnFailure: true
    timeoutSeconds: 4800`

	defaultCfgMap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"workflow": workflowYaml,
		},
	}

	err := h.client.Create(ctx, defaultCfgMap)
	if err != nil {
		return nil, err
	}
	return defaultCfgMap, nil
}

func (h *remediationMgrHelper) deleteConfigMap(ctx context.Context, name, namespace string) error {

	cm := &v1.ConfigMap{}
	cm.Name = name
	cm.Namespace = namespace
	return h.client.Delete(ctx, cm)
}

func (h *remediationMgrHelper) createDefaultWorkflowTemplate(ctx context.Context, namespace string) (*workflowv1alpha1.WorkflowTemplate, error) {

	notifyTemplate := &workflowv1alpha1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-notify-template",
			Namespace: namespace,
		},
		Spec: workflowv1alpha1.WorkflowSpec{
			Entrypoint: "notify",
			Templates: []workflowv1alpha1.Template{
				{
					Name: "notify",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name: "nodeName",
							},
							{
								Name: "notifyMessage",
							},
							{
								Name: "eventName",
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
NODE_NAME="{{inputs.parameters.nodeName}}"
NOTIFY_MESSAGE="{{inputs.parameters.notifyMessage}}"
EVENT_NAME="{{inputs.parameters.eventName}}"

kubectl create -f - <<EOF
apiVersion: v1
kind: Event
metadata:
  namespace: {{workflow.namespace}}
  generateName: ${EVENT_NAME}-
  labels:
    app.kubernetes.io/part-of: amd-gpu-operator
firstTimestamp: $(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
involvedObject:
  apiVersion: v1
  kind: Node
  name: ${NODE_NAME}
  namespace: {{workflow.namespace}}
message: ${NOTIFY_MESSAGE}
reason: AMDGPUUnhealthy
reportingComponent: amd-gpu-node-remediation-workflow
reportingInstance: amd-gpu-node-remediation-workflow
source:
  component: {{workflow.name}}
  host: ${NODE_NAME}
type: Warning
EOF
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"bash"},
						},
					},
				},
			},
		},
	}

	if err := h.client.Create(ctx, notifyTemplate); err != nil {
		return nil, err
	}

	template := &workflowv1alpha1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-template",
			Namespace: namespace,
		},
		Spec: workflowv1alpha1.WorkflowSpec{
			Entrypoint: "inbuilt",
			Templates: []workflowv1alpha1.Template{
				{
					Name: "inbuilt",
					Steps: []workflowv1alpha1.ParallelSteps{
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "taint", Template: "taint"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "drain", Template: "drain"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifyBeforeSuspend",
								TemplateRef: &workflowv1alpha1.TemplateRef{Name: "event-notify-template", Template: "notify"},
								Arguments: workflowv1alpha1.Arguments{
									Parameters: []workflowv1alpha1.Parameter{
										{Name: "nodeName", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}")},
										{Name: "notifyMessage", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.notifyMessage}}")},
										{Name: "eventName", Value: workflowv1alpha1.AnyStringPtr(AmdGpuRemediationRequired)},
									},
								},
							},
						},
						},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "suspend", Template: "suspend"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "reboot", Template: "reboot", ContinueOn: &workflowv1alpha1.ContinueOn{Failed: true}}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "test", Template: "test", ContinueOn: &workflowv1alpha1.ContinueOn{Failed: true}}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifyGpuTestFailed",
								TemplateRef: &workflowv1alpha1.TemplateRef{Name: "event-notify-template", Template: "notify"},
								Arguments: workflowv1alpha1.Arguments{
									Parameters: []workflowv1alpha1.Parameter{
										{Name: "nodeName", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}")},
										{Name: "notifyMessage", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.notifyErrorMessage}}")},
										{Name: "eventName", Value: workflowv1alpha1.AnyStringPtr(AmdGpuRemediationFailed)},
									},
								},
								When: "{{steps.test.exitCode}} != 0",
							},
						},
						},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "failWorkflow", Template: "failWorkflow", When: "{{steps.test.exitCode}} != 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "wait", Template: "wait", When: "{{steps.test.exitCode}} == 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "untaint", Template: "untaint", When: "{{steps.test.exitCode}} == 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifyWorkflowSucceeded",
								TemplateRef: &workflowv1alpha1.TemplateRef{Name: "event-notify-template", Template: "notify"},
								Arguments: workflowv1alpha1.Arguments{
									Parameters: []workflowv1alpha1.Parameter{
										{Name: "nodeName", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}")},
										{Name: "notifyMessage", Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.notifySuccessMessage}}")},
										{Name: "eventName", Value: workflowv1alpha1.AnyStringPtr(AmdGpuRemediationSucceeded)},
									},
								},
								When: "{{steps.test.exitCode}} == 0",
							},
						},
						},
					},
				},
				{
					Name: "taint",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_condition",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_condition}}"),
							},
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Tainting node $NODE_NAME"
kubectl taint node "$NODE_NAME" amd-gpu-unhealthy="{{inputs.parameters.node_condition}}":NoSchedule --overwrite
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
				{
					Name:    "suspend",
					Suspend: &workflowv1alpha1.SuspendTemplate{},
				},
				{
					Name: "drain",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
echo "Fetching node name..."
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Identified node: $NODE_NAME"
echo "Finding pods on node $NODE_NAME with volume mount path starting with /dev/dri..."
PODS=$(kubectl get pods --all-namespaces -o json | jq -r '
  .items[] |
    select(.spec.nodeName == "'"$NODE_NAME"'") |
    select(
      (
        [.spec.volumes[]? | select(.hostPath?.path != null and (.hostPath.path | startswith("/dev/dri")))]
        | length > 0
      ) or (
        [.spec.containers[]? | select(.resources.requests["amd.com/gpu"] != null)]
        | length > 0
      )
    ) |
    "\(.metadata.namespace) \(.metadata.name)"
')
if [ -z "$PODS" ]; then
  echo "No pods with /dev/dri mounts found on node $NODE_NAME."
else
  echo "Evicting pods:"
  echo "$PODS"
  echo "$PODS" | while read -r ns name; do
    echo "Deleting pod $name in namespace $ns"
    kubectl delete pod "$name" -n "$ns" --grace-period=0 --force || true
  done
fi
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
				{
					Name: "reboot",
					Container: &v1.Container{
						Image:           "docker.io/rocm/gpu-operator-utils:latest",
						Command:         []string{"/nsenter", "--all", "--target=1", "--", "/sbin/reboot", "-f"},
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
					},
					PodSpecPatch: `
hostPID: true
hostNetwork: true
containers:
- name: main
  stdin: true
  tty: true
`,
				},
				{
					Name: "test",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
							{
								Name:  "framework",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.framework}}"),
							},
							{
								Name:  "recipe",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.recipe}}"),
							},
							{
								Name:  "iterations",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.iterations}}"),
							},
							{
								Name:  "stopOnFailure",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.stopOnFailure}}"),
							},
							{
								Name:  "timeoutSeconds",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.timeoutSeconds}}"),
							},
							{
								Name:  "testRunnerImage",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.testRunnerImage}}"),
							},
							{
								Name:  "testRunnerServiceAccount",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.testRunnerServiceAccount}}"),
							},
							{
								Name:  "namespace",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.namespace}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
NODE_NAME="{{inputs.parameters.node_name}}"
JOB_NAME="test-runner-manual-trigger-${NODE_NAME}"
CM_NAME="manual-config-map-${NODE_NAME}"
FRAMEWORK="{{inputs.parameters.framework}}"
RECIPE="{{inputs.parameters.recipe}}"
ITERATIONS="{{inputs.parameters.iterations}}"
STOPONFAILURE="{{inputs.parameters.stopOnFailure}}"
TIMEOUTSECONDS="{{inputs.parameters.timeoutSeconds}}"
TESTRUNNERIMAGE="{{inputs.parameters.testRunnerImage}}"
TESTRUNNERSA="{{inputs.parameters.testRunnerServiceAccount}}"
NAMESPACE="{{inputs.parameters.namespace}}"

if [ -z "$FRAMEWORK" ] || [ -z "$RECIPE" ] || [ -z "$ITERATIONS" ] || [ -z "$STOPONFAILURE" ] || [ -z "$TIMEOUTSECONDS" ]; then
  echo "Validation profile incomplete, skipping configmap and job creation. Please enter framework, recipe, iterations, stopOnFailure, timeoutSeconds as per testrunner requirements"
  exit 0
fi

echo "Creating test runner Job $JOB_NAME and ConfigMap $CM_NAME..."

cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${CM_NAME}
  namespace: ${NAMESPACE}
data:
  config.json: |
    {
      "TestConfig": {
        "GPU_HEALTH_CHECK": {
          "TestLocationTrigger": {
            "${NODE_NAME}": {
              "TestParameters": {
                "MANUAL": {
                  "TestCases": [
                    {
                      "Framework": "${FRAMEWORK}",
                      "Recipe": "${RECIPE}",
                      "Iterations": "${ITERATIONS}",
                      "StopOnFailure": "${STOPONFAILURE}",
                      "TimeoutSeconds": "${TIMEOUTSECONDS}"
                    }
                  ]
                }
              }
            }
          }
        }
      }
    }
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ${JOB_NAME}
  namespace: ${NAMESPACE}
spec:
  ttlSecondsAfterFinished: 120
  backoffLimit: 0
  template:
    spec:
      serviceAccountName: "${TESTRUNNERSA}"
      nodeSelector:
        kubernetes.io/hostname: ${NODE_NAME}
      tolerations:
      - key: "amd-gpu-unhealthy"
        operator: "Exists"
        effect: "NoSchedule"
      restartPolicy: Never
      volumes:
        - name: kfd
          hostPath:
            path: /dev/kfd
            type: CharDevice
        - name: dri
          hostPath:
            path: /dev/dri
            type: Directory
        - name: config-volume
          configMap:
            name: ${CM_NAME}
        - hostPath:
            path: /var/log/amd-test-runner
            type: DirectoryOrCreate
          name: test-runner-volume
      containers:
        - name: amd-test-runner
          image: "${TESTRUNNERIMAGE}"
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          volumeMounts:
            - mountPath: /dev/dri
              name: dri
            - mountPath: /dev/kfd
              name: kfd
            - mountPath: /var/log/amd-test-runner
              name: test-runner-volume
            - mountPath: /etc/test-runner/
              name: config-volume
          env:
            - name: LOG_MOUNT_DIR # Use LOG_MOUNT_DIR environment variable to ask test runner to save logs in mounted directory
              value: /var/log/amd-test-runner
            - name: TEST_TRIGGER
              value: "MANUAL"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
EOF

echo "Waiting for Job $JOB_NAME to complete..."

while true; do
  job_status=$(kubectl get job "$JOB_NAME" -n "$NAMESPACE" -o jsonpath='{.status.conditions[0].type}' 2>/dev/null || true)
  if [ "$job_status" = "Complete" ]; then
    echo "Test runner job completed successfully."
	kubectl logs -n $NAMESPACE job/$JOB_NAME
    echo "Detailed run report can be found at /var/log/amd-test-runner"
    exit 0
  elif [ "$job_status" = "Failed" ]; then
    echo "Test runner job failed."
    kubectl logs -n $NAMESPACE job/$JOB_NAME
    echo "Detailed run report can be found at /var/log/amd-test-runner"
    exit 1
  else
    echo "Test runner job is still running. Waiting..."
    sleep 60
  fi
done
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
				{
					Name: "wait",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_condition",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_condition}}"),
							},
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Waiting for {{inputs.parameters.node_condition}} condition to be False on node $NODE_NAME for 2 consecutive minutes (timeout: 15 minutes)"
STABLE_COUNT=0
TOTAL_WAIT=0
while [ "$TOTAL_WAIT" -lt 15 ]; do
  STATUS=$(kubectl get node "$NODE_NAME" -o jsonpath="{.status.conditions[?(@.type=='{{inputs.parameters.node_condition}}')].status}")
  echo "[$(date)] {{inputs.parameters.node_condition}} status: $STATUS"
  if [ "$STATUS" = "False" ]; then
    STABLE_COUNT=$((STABLE_COUNT + 1))
    echo "Condition is stable (False) for $STABLE_COUNT minute(s)"
    if [ "$STABLE_COUNT" -ge 2 ]; then
      echo "Condition has been False for 2 consecutive checks (~2 minutes). Proceeding..."
      exit 0
    fi
  else
    STABLE_COUNT=0
    echo "Condition is not stable (status: $STATUS)."
  fi
  sleep 60
  TOTAL_WAIT=$((TOTAL_WAIT + 1))
done
echo "{{inputs.parameters.node_condition}} did not remain False for 2 consecutive minutes within 15 minutes. Exiting with failure."
exit 1
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
				{
					Name: "untaint",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Untainting node $NODE_NAME"
kubectl taint node "$NODE_NAME" amd-gpu-unhealthy:NoSchedule-
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
				{
					Name: "failWorkflow",
					Script: &workflowv1alpha1.ScriptTemplate{
						Source: `
echo "Failing workflow"
exit 1
`,
						Container: v1.Container{
							Image:   "bitnami/kubectl:1.29.0",
							Command: []string{"sh"},
						},
					},
				},
			},
		},
	}

	if err := h.client.Create(ctx, template); err != nil {
		return nil, err
	}

	return template, nil
}

func (h *remediationMgrHelper) createDefaultObjects(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*v1.ConfigMap, error) {

	logger := log.FromContext(ctx)
	var cfgMapName string
	if devConfig.Spec.RemediationWorkflow.ConditionalWorkflows != nil {
		cfgMapName = devConfig.Spec.RemediationWorkflow.ConditionalWorkflows.Name
	} else {
		cfgMapName = devConfig.Name + "-" + DefaultConfigMapSuffix
	}

	// Create default configmap if required
	cm, err := h.getConfigMap(ctx, cfgMapName, devConfig.Namespace)
	if err != nil {
		if devConfig.Spec.RemediationWorkflow.ConditionalWorkflows == nil {
			cm, err = h.createDefaultConfigMap(ctx, cfgMapName, devConfig.Namespace)
			if err != nil {
				logger.Error(err, "Failed to create default configmap")
				return nil, err
			}
			logger.Info("Created default configmap successfully")
		} else {
			logger.Error(err, fmt.Sprintf("Configmap: %s not found", cfgMapName))
			return nil, err
		}
	}

	// Create Default WorkflowTemplate if required
	_, err = h.getWorkflowTemplate(ctx, DefaultTemplate, devConfig.Namespace)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to fetch WorkflowTemplate %s", DefaultTemplate))
		if _, err = h.createDefaultWorkflowTemplate(ctx, devConfig.Namespace); err != nil {
			logger.Error(err, "Failed to create default workflow template")
			return nil, err
		}
		logger.Info("Created default workflow template successfully")
	}

	return cm, nil
}

func (h *remediationMgrHelper) populateWorkflow(ctx context.Context, wfTemplate *workflowv1alpha1.WorkflowTemplate, mapping *ConditionWorkflowMapping, nodeName string, devConfig *amdv1alpha1.DeviceConfig) *workflowv1alpha1.Workflow {
	wf := &workflowv1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-", nodeName, mapping.WorkflowTemplate),
			Namespace:    devConfig.Namespace,
		},
		Spec: *wfTemplate.Spec.DeepCopy(),
	}

	wf.Spec.Entrypoint = wfTemplate.Spec.Entrypoint
	wf.Spec.ServiceAccountName = "amd-gpu-operator-gpu-operator-charts-controller-manager"
	ttlHours := devConfig.Spec.RemediationWorkflow.TtlForFailedWorkflows
	ttlSeconds := int32(ttlHours * 3600)
	wf.Spec.TTLStrategy = &workflowv1alpha1.TTLStrategy{
		SecondsAfterCompletion: &ttlSeconds,
	}

	for i := range wf.Spec.Templates {
		if wf.Spec.Templates[i].NodeSelector == nil {
			wf.Spec.Templates[i].NodeSelector = map[string]string{}
		}
		wf.Spec.Templates[i].NodeSelector["kubernetes.io/hostname"] = nodeName

		toleration := v1.Toleration{
			Key:      RemediationTaintKey,
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		}

		if wf.Spec.Templates[i].Tolerations == nil {
			wf.Spec.Templates[i].Tolerations = []v1.Toleration{}
		}
		wf.Spec.Templates[i].Tolerations = append(wf.Spec.Templates[i].Tolerations, toleration)
	}

	// Pass the args required to be used in the template
	wf.Spec.Arguments = workflowv1alpha1.Arguments{
		Parameters: []workflowv1alpha1.Parameter{
			{
				Name:  "node_condition",
				Value: workflowv1alpha1.AnyStringPtr(mapping.NodeCondition),
			},
			{
				Name:  "node_name",
				Value: workflowv1alpha1.AnyStringPtr(nodeName),
			},
			{
				Name:  "framework",
				Value: workflowv1alpha1.AnyStringPtr(mapping.ValidationTests.Framework),
			},
			{
				Name:  "recipe",
				Value: workflowv1alpha1.AnyStringPtr(mapping.ValidationTests.Recipe),
			},
			{
				Name:  "iterations",
				Value: workflowv1alpha1.AnyStringPtr(mapping.ValidationTests.Iterations),
			},
			{
				Name:  "stopOnFailure",
				Value: workflowv1alpha1.AnyStringPtr(mapping.ValidationTests.StopOnFailure),
			},
			{
				Name:  "timeoutSeconds",
				Value: workflowv1alpha1.AnyStringPtr(mapping.ValidationTests.TimeoutSeconds),
			},
			{
				Name:  "testRunnerImage",
				Value: workflowv1alpha1.AnyStringPtr(TestRunnerImage),
			},
			{
				Name:  "testRunnerServiceAccount",
				Value: workflowv1alpha1.AnyStringPtr(TestRunnerServiceAccount),
			},
			{
				Name:  "namespace",
				Value: workflowv1alpha1.AnyStringPtr(devConfig.Namespace),
			},
			{
				Name:  "notifyMessage",
				Value: workflowv1alpha1.AnyStringPtr(mapping.NotifyMessage),
			},
			{
				Name:  "notifyErrorMessage",
				Value: workflowv1alpha1.AnyStringPtr(fmt.Sprintf("Remediation for node condition %s failed on node %s", mapping.NodeCondition, nodeName)),
			},
			{
				Name:  "notifySuccessMessage",
				Value: workflowv1alpha1.AnyStringPtr(fmt.Sprintf("Remediation for node condition %s completed successfully on node %s", mapping.NodeCondition, nodeName)),
			},
		},
	}

	return wf

}

func (h *remediationMgrHelper) createWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error {
	if err := h.client.Create(ctx, workflow); err != nil {
		return err
	}
	return nil
}

func (h *remediationMgrHelper) deleteWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error {
	if err := h.client.Delete(ctx, workflow); err != nil {
		return err
	}
	return nil
}

func (h *remediationMgrHelper) getWorkflowTemplate(ctx context.Context, workflowTemplateName, namespace string) (*workflowv1alpha1.WorkflowTemplate, error) {
	wfTemplate := &workflowv1alpha1.WorkflowTemplate{}
	err := h.client.Get(ctx, client.ObjectKey{
		Name:      workflowTemplateName,
		Namespace: namespace,
	}, wfTemplate)
	if err != nil {
		return nil, err
	}
	return wfTemplate, nil
}

func (h *remediationMgrHelper) validateNodeConditions(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mappings map[string]ConditionWorkflowMapping) (ConditionWorkflowMapping, error) {
	// Check if any node condition of interest is set to True
	conditionMet := false
	exists := false
	var mapping ConditionWorkflowMapping
	logger := log.FromContext(ctx)
	for _, cond := range node.Status.Conditions {
		if cond.Status != v1.ConditionTrue {
			continue
		}
		mapping, exists = mappings[string(cond.Type)]
		if !exists {
			continue
		}
		logger.Info(fmt.Sprintf("Matching condition %s found on node %s", mapping.NodeCondition, node.Name))
		conditionMet = true
		break
	}
	if !conditionMet {
		return mapping, fmt.Errorf("No matching condition found on node %s for condition %s", node.Name, mapping.NodeCondition)
	}

	return mapping, nil
}

func (h *remediationMgrHelper) isWorkflowSchedulableOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping) bool {
	logger := log.FromContext(ctx)
	taint := v1.Taint{
		Key:    RemediationTaintKey,
		Value:  mapping.NodeCondition,
		Effect: v1.TaintEffectNoSchedule,
	}

	// If taint already exists, skip the node
	if hasTaint := h.checkIfTaintExists(node, taint); hasTaint {
		logger.Info(fmt.Sprintf("Taint %s already present on node %s, skipping creation of workflow", taint.Key, node.Name))
		return false
	}

	// If driver install/upgrade is in progress, skip the node
	if driverUpgradeInProgress := h.isDriverUpgradeInProgress(devConfig, node); driverUpgradeInProgress {
		logger.Info(fmt.Sprintf("Driver Install/Upgrade is in progress, skipping creation of workflow on node %s", node.Name))
		return false
	}
	return true
}

func (h *remediationMgrHelper) handleExistingWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node) bool {
	logger := log.FromContext(ctx)
	wfList, err := h.getWorkflowList(ctx, devConfig.Namespace)
	if err != nil {
		logger.Error(err, "Get workflow list failed")
		return false
	}

	// If a workflow is already running on that node, then skip the node but resume/delete workflow if needed
	for _, wf := range wfList.Items {
		if strings.HasPrefix(wf.Name, fmt.Sprintf("%s-", node.Name)) {
			if wf.Status.Phase == workflowv1alpha1.WorkflowSucceeded {
				if err := h.deleteWorkflow(ctx, &wf); err != nil {
					logger.Error(err, fmt.Sprintf("Failed to delete workflow %s on node %v", wf.Name, node.Name))
					return false
				}
				logger.Info(fmt.Sprintf("Deleted completed workflow %s on node %v", wf.Name, node.Name))
			} else if wf.Status.Phase == workflowv1alpha1.WorkflowRunning {
				stages := wf.Status.Nodes
				for _, wfStage := range stages {
					if wfStage.Type == "Suspend" && wfStage.Phase == "Running" {
						logger.Info(fmt.Sprintf("Found suspended workflow %s on node %s. Attempting resume.", wf.Name, node.Name))
						if err := h.resumeSuspendedWorkflow(ctx, wf.Name, wf.Namespace); err != nil {
							logger.Error(err, fmt.Sprintf("Failed to resume workflow %s on node %s", wf.Name, node.Name))
						} else {
							logger.Info(fmt.Sprintf("successfully resumed workflow %s on node %v", wf.Name, node.Name))
						}
						return false
					}
				}
				logger.Info(fmt.Sprintf("Workflow: %s already running on the node: %s, skipping creation of workflow", wf.Name, node.Name))
				return false
			}
			break
		}
	}
	return true
}
