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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"

	workflowv1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	RemediationTaintKey        = "amd-gpu-unhealthy"
	DefaultConfigMapSuffix     = "default-conditional-workflow-mappings"
	DefaultTemplate            = "default-template"
	DefaultTestRunnerImage     = "docker.io/rocm/test-runner:v1.4.1"
	TestRunnerServiceAccount   = "amd-gpu-operator-test-runner"
	AmdGpuRemediationRequired  = "amd-gpu-remediation-required"
	AmdGpuRemediationSucceeded = "amd-gpu-remediation-succeeded"
	AmdGpuRemediationFailed    = "amd-gpu-remediation-failed"
	DefaultUtilityImage        = "docker.io/rocm/gpu-operator-utils:latest"
	// DefaultRecoveryPolicyWindowSize - defines the time window size for recovery policy
	DefaultRecoveryPolicyWindowSize = "15m"
	// DefaultRecoveryPolicyMaxRunsPerWindow - defines the max allowed runs per window for recovery policy
	// If a specific node condition is hit more than this number of times within the window size, no new remediation workflows will be scheduled
	DefaultRecoveryPolicyMaxRunsPerWindow = 3
	DefaultTimeFormatLayout               = "2006-01-02 15:04:05 UTC"
	DefaultStatusCRCleanupWindowSize      = "72h"
	// Below is the label and value needed to be added to node to force resume a suspended workflow
	ForceResumeWorkflowLabelKey   = "operator.amd.com/gpu-force-resume-workflow"
	ForceResumeWorkflowLabelValue = "true"
	// Below is the label and value needed to be added to node to abort an ongoing workflow
	AbortWorkflowLabelKey           = "operator.amd.com/gpu-abort-workflow"
	AbortWorkflowLabelValue         = "true"
	RemediationFilesPath            = "/remediation"
	DefaultInitContainerImage       = "busybox:1.36"
	ArgoWorkflowControllerConfigMap = "workflow-controller-configmap"
)

type RecoveryPolicyConfig struct {
	MaxAllowedRunsPerWindow int    `json:"maxAllowedRunsPerWindow" yaml:"maxAllowedRunsPerWindow"`
	WindowSize              string `json:"windowSize" yaml:"windowSize"`
}

// ConditionWorkflowMapping defines a single condition-to-workflow mapping.
// This is used when parsing the ConfigMap specified in the DeviceConfig.
type ConditionWorkflowMapping struct {
	NodeCondition            string                 `json:"nodeCondition" yaml:"nodeCondition"`
	WorkflowTemplate         string                 `json:"workflowTemplate" yaml:"workflowTemplate"`
	ValidationTests          ValidationTestsProfile `json:"validationTestsProfile" yaml:"validationTestsProfile"`
	PhysicalActionNeeded     bool                   `json:"physicalActionNeeded" yaml:"physicalActionNeeded"`
	NotifyRemediationMessage string                 `json:"notifyRemediationMessage" yaml:"notifyRemediationMessage"`
	NotifyTestFailureMessage string                 `json:"notifyTestFailureMessage" yaml:"notifyTestFailureMessage"`
	RecoveryPolicy           RecoveryPolicyConfig   `json:"recoveryPolicy" yaml:"recoveryPolicy"`
	SkipRebootStep           bool                   `json:"skipRebootStep" yaml:"skipRebootStep"`
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

	// Update max parallel workflows based on DeviceConfig
	if err := n.helper.updateMaxParallelWorkflows(ctx, devConfig); err != nil {
		logger.Error(err, "Failed to update max parallel workflows, continuing with remediation")
	}

	// Clear any older recovery attempts from the status CR
	if err := n.helper.dropOlderRecoveryAttemptsFromStatusCR(ctx, devConfig.Namespace); err != nil {
		logger.Error(err, "Failed to drop older recovery attempts from status CR")
		return res, err
	}

	if err := n.helper.syncInternalMapFromStatusCR(ctx, devConfig.Namespace); err != nil {
		logger.Error(err, "Failed to sync internal map from status CR")
		return res, err
	}
	logger.Info("Internal map synced from status CR successfully")

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
		createNewWorkflow := n.helper.handleExistingWorkflowsOnNode(ctx, devConfig, &node, mapping)
		if !createNewWorkflow {
			continue
		}
		canSchedule := n.helper.isWorkflowSchedulableOnNode(ctx, devConfig, &node, mapping)
		if !canSchedule {
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

		// Drop older recovery attempts from internal map based on the window size
		windowSize := n.helper.getWindowSize(&mapping.RecoveryPolicy)
		if err := n.helper.dropOlderRecoveryAttemptsInternal(node.Name, mapping.NodeCondition, windowSize); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to drop older recovery attempts for node %s and condition %s", node.Name, mapping.NodeCondition))
			return res, err
		}

		// Register the recovery attempt in internal map
		if err := n.helper.registerRecoveryAttempt(ctx, node.Name, mapping.NodeCondition, devConfig.Namespace, wf.Name); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to register recovery attempt for node %s", node.Name))
			return res, err
		}
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
	if deviceConfig.Spec.RemediationWorkflow.Config != nil {
		cfgMapName = deviceConfig.Spec.RemediationWorkflow.Config.Name
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
	getServiceAccountName(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) string
	isRemediationDisabled(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (bool, error)
	resumeSuspendedWorkflow(ctx context.Context, wfName, namespace string) error
	isDriverUpgradeInProgress(devCfg *amdv1alpha1.DeviceConfig, node *v1.Node) bool
	checkIfTaintExists(node *v1.Node, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string) bool
	getWorkflowList(ctx context.Context, namespace string) (*workflowv1alpha1.WorkflowList, error)
	getWorkflowTemplate(ctx context.Context, workflowTemplateName, namespace string) (*workflowv1alpha1.WorkflowTemplate, error)
	getConfigMap(ctx context.Context, configmapName string, namespace string) (*v1.ConfigMap, error)
	deleteConfigMap(ctx context.Context, name, namespace string) error
	createDefaultConfigMap(ctx context.Context, name, namespace string) (*v1.ConfigMap, error)
	createDefaultWorkflowTemplate(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*workflowv1alpha1.WorkflowTemplate, error)
	createDefaultObjects(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*v1.ConfigMap, error)
	populateWorkflow(ctx context.Context, wfTemplate *workflowv1alpha1.WorkflowTemplate, mapping *ConditionWorkflowMapping, nodeName string, devCfg *amdv1alpha1.DeviceConfig) *workflowv1alpha1.Workflow
	createWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error
	deleteWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error
	validateNodeConditions(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mappings map[string]ConditionWorkflowMapping) (ConditionWorkflowMapping, error)
	isWorkflowSchedulableOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping) bool
	handleExistingWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping) bool
	getWorkflowUtilityImage(devConfig *amdv1alpha1.DeviceConfig) v1.Container
	createRemediationWorkflowStatus(ctx context.Context, namespace string) (*amdv1alpha1.RemediationWorkflowStatus, error)
	getRemediationWorkflowStatus(ctx context.Context, namespace string) (*amdv1alpha1.RemediationWorkflowStatus, error)
	getRecentRecoveryCount(nodeName string, nodeCondition string) int
	dropOlderRecoveryAttemptsFromStatusCR(ctx context.Context, namespace string) error
	dropOlderRecoveryAttemptsInternal(nodeName string, nodeCondition string, windowSize string) error
	registerRecoveryAttempt(ctx context.Context, nodeName string, nodeCondition string, namespace string, wfName string) error
	registerRecoveryAttemptInternal(nodeName string, nodeCondition string, namespace string, startTime time.Time) error
	registerRecoveryAttemptToStatusCR(ctx context.Context, nodeName string, nodeCondition string, namespace string, wfName string, startTime time.Time) error
	getRecoveryTrackerKey(nodeName string, nodeCondition string) string
	getMaxAllowedRunsPerWindow(recoveryPolicy *RecoveryPolicyConfig) int
	getWindowSize(recoveryPolicy *RecoveryPolicyConfig) string
	isRecoveryPolicyViolated(ctx context.Context, nodeName string, mapping *ConditionWorkflowMapping) bool
	canResumeWorkflowOnNode(ctx context.Context, node *v1.Node, mapping *ConditionWorkflowMapping, stageName string) bool
	syncInternalMapFromStatusCR(ctx context.Context, namespace string) error
	isNodeLabelledForForceResume(ctx context.Context, node *v1.Node) bool
	removeForceResumeWorkflowLabelFromNode(ctx context.Context, node *v1.Node) error
	isNodeLabelledForAbortWorkflow(node *v1.Node) bool
	removeAbortWorkflowLabelFromNode(ctx context.Context, node *v1.Node) error
	abortWorkflow(ctx context.Context, workflow *workflowv1alpha1.Workflow) error
	attemptAbortWorkflowOnNode(ctx context.Context, node *v1.Node, wf *workflowv1alpha1.Workflow) (bool, error)
	attemptResumeWorkflowOnNode(ctx context.Context, node *v1.Node, mapping ConditionWorkflowMapping, wf *workflowv1alpha1.Workflow, stageName string)
	handleSuspendedWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping, wf *workflowv1alpha1.Workflow) bool
	getWorkflowTaskScriptSource(scriptFileName string) (string, error)
	updateMaxParallelWorkflows(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error
	getNodeLabelsFromCR(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) []string
	getNodeTaints(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string) []string
	applyTolerationsToWorkflow(wf *workflowv1alpha1.Workflow, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string)
}

type remediationMgrHelper struct {
	client               client.Client
	k8sInterface         kubernetes.Interface
	recoveryTracker      *sync.Map
	serviceAccountName   string
	maxParallelWorkflows int
}

// Initialize remediation manager helper interface
func newRemediationMgrHelperHandler(client client.Client, k8sInterface kubernetes.Interface) remediationMgrHelperAPI {
	return &remediationMgrHelper{
		client:          client,
		k8sInterface:    k8sInterface,
		recoveryTracker: new(sync.Map),
	}
}

func (h *remediationMgrHelper) getServiceAccountName(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) string {
	if h.serviceAccountName == "" {
		sas := v1.ServiceAccountList{}
		if err := h.client.List(ctx, &sas, client.InNamespace(devConfig.Namespace)); err == nil {
			for _, sa := range sas.Items {
				if strings.HasSuffix(sa.Name, "controller-manager") {
					h.serviceAccountName = sa.Name
					break
				}
			}
		}
	}
	return h.serviceAccountName
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

func (h *remediationMgrHelper) checkIfTaintExists(node *v1.Node, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string) bool {
	taints := make([]v1.Taint, 0)
	if len(devConfig.Spec.RemediationWorkflow.NodeRemediationTaints) > 0 {
		taints = devConfig.Spec.RemediationWorkflow.NodeRemediationTaints
	} else {
		taints = append(taints, v1.Taint{
			Key:    RemediationTaintKey,
			Effect: v1.TaintEffectNoSchedule,
		})
	}
	for _, t := range node.Spec.Taints {
		for _, targetTaint := range taints {
			if t.Key == targetTaint.Key && t.Effect == targetTaint.Effect {
				return true
			}
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
	return cm, err
}

func (h *remediationMgrHelper) createDefaultConfigMap(ctx context.Context, name string, namespace string) (*v1.ConfigMap, error) {
	logger := log.FromContext(ctx)

	yamlBytes, err := os.ReadFile(filepath.Join(RemediationFilesPath, "configs/default-configmap.yaml"))
	if err != nil {
		logger.Error(err, "Failed to read default remediation workflows file")
		return nil, err
	}

	workflowYaml := string(yamlBytes)

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

	err = h.client.Create(ctx, defaultCfgMap)
	if err != nil {
		logger.Error(err, "Failed to create default remediation configmap")
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

func (h *remediationMgrHelper) getWorkflowTaskScriptSource(scriptFileName string) (string, error) {
	scriptPath := filepath.Join(RemediationFilesPath, "scripts", scriptFileName)
	yamlBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read script file %q: %w", scriptFileName, err)
	}
	return string(yamlBytes), nil
}

func (h *remediationMgrHelper) createDefaultWorkflowTemplate(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) (*workflowv1alpha1.WorkflowTemplate, error) {

	utilityContainer := h.getWorkflowUtilityImage(devConfig)
	utilityContainer.Command = []string{"sh"}

	rebootContainer := h.getWorkflowUtilityImage(devConfig)
	rebootContainer.Command = []string{"/nsenter", "--all", "--target=1", "--", "/sbin/reboot", "-f"}
	rebootContainer.SecurityContext = &v1.SecurityContext{Privileged: ptr.To(true)}

	notifySrc, err := h.getWorkflowTaskScriptSource("notify.sh")
	if err != nil {
		return nil, err
	}

	notifyTemplate := &workflowv1alpha1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-notify-template",
			Namespace: devConfig.Namespace,
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
						Source:    notifySrc,
						Container: utilityContainer,
					},
				},
			},
		},
	}

	if err := h.client.Create(ctx, notifyTemplate); err != nil {
		return nil, err
	}
	taintSrc, err := h.getWorkflowTaskScriptSource("taint.sh")
	if err != nil {
		return nil, err
	}
	drainSrc, err := h.getWorkflowTaskScriptSource("drain.sh")
	if err != nil {
		return nil, err
	}
	testSrc, err := h.getWorkflowTaskScriptSource("test.sh")
	if err != nil {
		return nil, err
	}
	waitSrc, err := h.getWorkflowTaskScriptSource("wait.sh")
	if err != nil {
		return nil, err
	}
	untaintSrc, err := h.getWorkflowTaskScriptSource("untaint.sh")
	if err != nil {
		return nil, err
	}
	applyLabelsSrc, err := h.getWorkflowTaskScriptSource("applylabels.sh")
	if err != nil {
		return nil, err
	}
	removeLabelsSrc, err := h.getWorkflowTaskScriptSource("removelabels.sh")
	if err != nil {
		return nil, err
	}

	template := &workflowv1alpha1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-template",
			Namespace: devConfig.Namespace,
		},
		Spec: workflowv1alpha1.WorkflowSpec{
			Entrypoint: "inbuilt",
			Templates: []workflowv1alpha1.Template{
				{
					Name: "inbuilt",
					Steps: []workflowv1alpha1.ParallelSteps{
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "autostart", Template: "suspend", When: "{{workflow.parameters.auto_start}} == 'false'"}}}, // If auto start is disabled, workflow will be created in suspended state and needs to be manually resumed by user
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "applylabels", Template: "applylabels"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "taint", Template: "taint"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "drain", Template: "drain"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifybeforesuspend",
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
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "reboot", Template: "reboot", ContinueOn: &workflowv1alpha1.ContinueOn{Failed: true}, When: "{{workflow.parameters.skipRebootStep}} == 'false'"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "test", Template: "test", ContinueOn: &workflowv1alpha1.ContinueOn{Failed: true}}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifygputestfailed",
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
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "failurecleanup", Template: "removelabels", When: "{{steps.test.exitCode}} != 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "failworkflow", Template: "failworkflow", When: "{{steps.test.exitCode}} != 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "wait", Template: "wait", When: "{{steps.test.exitCode}} == 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "untaint", Template: "untaint", When: "{{steps.test.exitCode}} == 0"}}},
						{Steps: []workflowv1alpha1.WorkflowStep{
							{
								Name:        "notifyworkflowsucceeded",
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
						{Steps: []workflowv1alpha1.WorkflowStep{{Name: "successcleanup", Template: "removelabels", When: "{{steps.test.exitCode}} == 0"}}},
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
						Source:    taintSrc,
						Container: utilityContainer,
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
						Source:    drainSrc,
						Container: utilityContainer,
					},
				},
				{
					Name:      "reboot",
					Container: &rebootContainer,
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
							{
								Name:  "initContainerImage",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.initContainerImage}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source:    testSrc,
						Container: utilityContainer,
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
						Source:    waitSrc,
						Container: utilityContainer,
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
						Source:    untaintSrc,
						Container: utilityContainer,
					},
				},
				{
					Name: "failworkflow",
					Script: &workflowv1alpha1.ScriptTemplate{
						Source:    `echo "Failing workflow" && exit 1`,
						Container: utilityContainer,
					},
				},
				{
					Name: "applylabels",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
							{
								Name:  "labels",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.labels}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source:    applyLabelsSrc,
						Container: utilityContainer,
					},
				},
				{
					Name: "removelabels",
					Inputs: workflowv1alpha1.Inputs{
						Parameters: []workflowv1alpha1.Parameter{
							{
								Name:  "node_name",
								Value: workflowv1alpha1.AnyStringPtr("{{workflow.parameters.node_name}}"),
							},
						},
					},
					Script: &workflowv1alpha1.ScriptTemplate{
						Source:    removeLabelsSrc,
						Container: utilityContainer,
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
	if devConfig.Spec.RemediationWorkflow.Config != nil {
		cfgMapName = devConfig.Spec.RemediationWorkflow.Config.Name
	} else {
		cfgMapName = devConfig.Name + "-" + DefaultConfigMapSuffix
	}
	// Create default configmap if required
	cm, err := h.getConfigMap(ctx, cfgMapName, devConfig.Namespace)
	if err != nil {
		if devConfig.Spec.RemediationWorkflow.Config == nil {
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
		if _, err = h.createDefaultWorkflowTemplate(ctx, devConfig); err != nil {
			logger.Error(err, "Failed to create default workflow template")
			return nil, err
		}
		logger.Info("Created default workflow template successfully")
	}

	// Create Default RemediationWorkflowStatus if required
	_, err = h.getRemediationWorkflowStatus(ctx, devConfig.Namespace)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to fetch RemediationWorkflowStatus %s", "default"))
		if _, err = h.createRemediationWorkflowStatus(ctx, devConfig.Namespace); err != nil {
			logger.Error(err, "Failed to create default remediation workflow status")
			return nil, err
		}
		logger.Info("Created default remediation workflow status successfully")
	}

	return cm, nil
}

func (h *remediationMgrHelper) updateMaxParallelWorkflows(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) error {
	logger := log.FromContext(ctx)
	// Set maximum parallel workflows that can run simultaneously
	if h.maxParallelWorkflows != devConfig.Spec.RemediationWorkflow.MaxParallelWorkflows {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			acm, err := h.getConfigMap(ctx, ArgoWorkflowControllerConfigMap, devConfig.Namespace)
			if err != nil {
				logger.Error(err, "Failed to fetch argo workflow controller configmap")
				return err
			}
			if acm.Data == nil {
				acm.Data = make(map[string]string)
			}
			// Update parallelism in Argo workflow controller configmap.
			// https://github.com/argoproj/argo-workflows/blob/main/config/config.go#L69
			acm.Data["parallelism"] = strconv.Itoa(devConfig.Spec.RemediationWorkflow.MaxParallelWorkflows)
			return h.client.Update(ctx, acm)
		})
		if err != nil {
			logger.Error(err, "Failed to update parallelism in argo workflow controller")
			return err
		}
		h.maxParallelWorkflows = devConfig.Spec.RemediationWorkflow.MaxParallelWorkflows
		logger.Info(fmt.Sprintf("Updated maximum parallel remediation workflows to %d", h.maxParallelWorkflows))
	}
	return nil
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
	wf.Spec.ServiceAccountName = h.getServiceAccountName(ctx, devConfig)
	ttlDuration, err := time.ParseDuration(devConfig.Spec.RemediationWorkflow.TtlForFailedWorkflows)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse TTL duration, using default of 24h")
		ttlDuration = 24 * time.Hour
	}
	ttlSeconds := int32(ttlDuration.Seconds())
	wf.Spec.TTLStrategy = &workflowv1alpha1.TTLStrategy{
		SecondsAfterCompletion: &ttlSeconds,
	}

	for i := range wf.Spec.Templates {
		if wf.Spec.Templates[i].NodeSelector == nil {
			wf.Spec.Templates[i].NodeSelector = map[string]string{}
		}
		wf.Spec.Templates[i].NodeSelector["kubernetes.io/hostname"] = nodeName
	}
	// apply tolerations based on node taints
	h.applyTolerationsToWorkflow(wf, devConfig, mapping.NodeCondition)

	testrunnerImage := DefaultTestRunnerImage

	if devConfig.Spec.RemediationWorkflow.TesterImage != "" {
		testrunnerImage = devConfig.Spec.RemediationWorkflow.TesterImage
	}

	initContainerImage := DefaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
	}

	nodeLabels := h.getNodeLabelsFromCR(ctx, devConfig)
	labelsJSONBytes, err := json.Marshal(nodeLabels)
	if err != nil {
		labelsJSONBytes = []byte("[]")
	}

	nodeTaints := h.getNodeTaints(ctx, devConfig, mapping.NodeCondition)
	taintsJSONBytes, err := json.Marshal(nodeTaints)
	if err != nil {
		taintsJSONBytes = []byte("[]")
	}

	drainPolicy := devConfig.Spec.RemediationWorkflow.NodeDrainPolicy
	if drainPolicy == nil {
		// Set default drain policy if not specified
		drainPolicy = &amdv1alpha1.DrainSpec{
			Force:              ptr.To(true),
			IgnoreDaemonSets:   ptr.To(true),
			TimeoutSeconds:     300,
			GracePeriodSeconds: -1,
			IgnoreNamespaces:   []string{"kube-system", "cert-manager", devConfig.Namespace},
		}
	}

	drainPolicyJSONBytes, err := json.Marshal(drainPolicy)
	if err != nil {
		drainPolicyJSONBytes = []byte("{}")
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
				Value: workflowv1alpha1.AnyStringPtr(testrunnerImage),
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
				Value: workflowv1alpha1.AnyStringPtr(mapping.NotifyRemediationMessage),
			},
			{
				Name:  "notifyErrorMessage",
				Value: workflowv1alpha1.AnyStringPtr(fmt.Sprintf("Remediation for node condition %s failed on node %s. %s", mapping.NodeCondition, nodeName, mapping.NotifyTestFailureMessage)),
			},
			{
				Name:  "notifySuccessMessage",
				Value: workflowv1alpha1.AnyStringPtr(fmt.Sprintf("Remediation for node condition %s completed successfully on node %s", mapping.NodeCondition, nodeName)),
			},
			{
				Name:  "initContainerImage",
				Value: workflowv1alpha1.AnyStringPtr(initContainerImage),
			},
			{
				Name:  "node_labels",
				Value: workflowv1alpha1.AnyStringPtr(string(labelsJSONBytes)),
			},
			{
				Name:  "node_taints",
				Value: workflowv1alpha1.AnyStringPtr(string(taintsJSONBytes)),
			},
			{
				Name:  "drain_policy",
				Value: workflowv1alpha1.AnyStringPtr(string(drainPolicyJSONBytes)),
			},
			{
				Name:  "skipRebootStep",
				Value: workflowv1alpha1.AnyStringPtr(mapping.SkipRebootStep),
			},
			{
				Name:  "auto_start",
				Value: workflowv1alpha1.AnyStringPtr(strconv.FormatBool(*devConfig.Spec.RemediationWorkflow.AutoStartWorkflow)),
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
	if hasTaint := h.checkIfTaintExists(node, devConfig, mapping.NodeCondition); hasTaint {
		logger.Info(fmt.Sprintf("Taint %s already present on node %s, skipping creation of workflow", taint.Key, node.Name))
		return false
	}

	// If driver install/upgrade is in progress, skip the node
	if driverUpgradeInProgress := h.isDriverUpgradeInProgress(devConfig, node); driverUpgradeInProgress {
		logger.Info(fmt.Sprintf("Driver Install/Upgrade is in progress, skipping creation of workflow on node %s", node.Name))
		return false
	}

	// if same node condition remediation workflow has crossed max threshold, skip the node
	if h.isRecoveryPolicyViolated(ctx, node.Name, &mapping) {
		logger.Info(fmt.Sprintf("Max remediation attempts reached for node %s on condition %s, skipping creation of workflow", node.Name, mapping.NodeCondition))
		return false
	}
	return true
}

func (h *remediationMgrHelper) handleExistingWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping) bool {
	logger := log.FromContext(ctx)
	wfList, err := h.getWorkflowList(ctx, devConfig.Namespace)
	if err != nil {
		logger.Error(err, "Get workflow list failed")
		return false
	}

	// If a workflow is already running on that node, then skip the node but resume/delete workflow if needed
	for _, wf := range wfList.Items {
		if !strings.HasPrefix(wf.Name, fmt.Sprintf("%s-", node.Name)) {
			continue
		}
		if wf.Status.Phase == workflowv1alpha1.WorkflowSucceeded {
			if err := h.deleteWorkflow(ctx, &wf); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete workflow %s on node %v", wf.Name, node.Name))
				return false
			}
			logger.Info(fmt.Sprintf("Deleted completed workflow %s on node %v", wf.Name, node.Name))
		} else if wf.Status.Phase == workflowv1alpha1.WorkflowRunning {
			return h.handleSuspendedWorkflowsOnNode(ctx, devConfig, node, mapping, &wf)
		}
	}
	return true
}

func (h *remediationMgrHelper) handleSuspendedWorkflowsOnNode(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node, mapping ConditionWorkflowMapping, wf *workflowv1alpha1.Workflow) bool {
	logger := log.FromContext(ctx)
	if wf.Status.Phase != workflowv1alpha1.WorkflowRunning {
		return false
	}
	stages := wf.Status.Nodes
	for _, wfStage := range stages {
		if wfStage.Type == "Suspend" && wfStage.Phase == "Running" {
			logger.Info(fmt.Sprintf("Suspended workflow %s found on node %s", wf.Name, node.Name))
			// Check if the workflow can be aborted, and attempt abort
			// If aborted, return true so that new workflow can be created
			canAbort, err := h.attemptAbortWorkflowOnNode(ctx, node, wf)
			if canAbort && err == nil {
				return true
			}

			// Check if the workflow can be resumed, and attempt resume
			h.attemptResumeWorkflowOnNode(ctx, node, mapping, wf, wfStage.DisplayName)
			// irrespective of whether it was resumed or not, return false to avoid creating a new workflow
			return false
		}
	}
	return false
}

func (h *remediationMgrHelper) attemptAbortWorkflowOnNode(ctx context.Context, node *v1.Node, wf *workflowv1alpha1.Workflow) (bool, error) {
	logger := log.FromContext(ctx)
	canAbort := h.isNodeLabelledForAbortWorkflow(node)
	if canAbort {
		logger.Info(fmt.Sprintf("Found abort label on node %s. Attempting abort workflow %s", node.Name, wf.Name))
		if err := h.abortWorkflow(ctx, wf); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to abort workflow %s on node %s", wf.Name, node.Name))
			return true, fmt.Errorf("Failed to abort workflow %s on node %s", wf.Name, node.Name)
		}
		if err := h.removeAbortWorkflowLabelFromNode(ctx, node); err != nil {
			return true, err
		}
		logger.Info(fmt.Sprintf("Aborted and deleted workflow %s on node %s.", wf.Name, node.Name))
		return true, nil
	}
	return canAbort, nil
}

func (h *remediationMgrHelper) attemptResumeWorkflowOnNode(ctx context.Context, node *v1.Node, mapping ConditionWorkflowMapping, wf *workflowv1alpha1.Workflow, stageName string) {
	logger := log.FromContext(ctx)
	// Check if the workflow can be resumed
	canResume := h.canResumeWorkflowOnNode(ctx, node, &mapping, stageName)
	if canResume {
		logger.Info(fmt.Sprintf("Attempting to resume suspended workflow %q on node %q.", wf.Name, node.Name))
		if err := h.resumeSuspendedWorkflow(ctx, wf.Name, wf.Namespace); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to resume workflow %s", wf.Name))
			return
		}
		resume := h.isNodeLabelledForForceResume(ctx, node)
		if resume {
			// Remove the label after allowing resumption
			if err := h.removeForceResumeWorkflowLabelFromNode(ctx, node); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to remove force resume label from node %s", node.Name))
				return
			}
		}
		logger.Info(fmt.Sprintf("Resumed suspended workflow %q on node %q.", wf.Name, node.Name))
	}
}

func (h *remediationMgrHelper) getWorkflowUtilityImage(devConfig *amdv1alpha1.DeviceConfig) v1.Container {
	output := v1.Container{}
	if devConfig.Spec.CommonConfig.UtilsContainer.Image != "" {
		output.Image = devConfig.Spec.CommonConfig.UtilsContainer.Image
	} else {
		output.Image = DefaultUtilityImage
	}
	if devConfig.Spec.CommonConfig.UtilsContainer.ImagePullPolicy != "" {
		output.ImagePullPolicy = v1.PullPolicy(devConfig.Spec.CommonConfig.UtilsContainer.ImagePullPolicy)
	}

	return output
}

func (h *remediationMgrHelper) getRecentRecoveryCount(nodeName string, nodeCondition string) int {
	// get the length of the slice of attempts for the given node and condition
	key := h.getRecoveryTrackerKey(nodeName, nodeCondition)

	attempts, ok := h.recoveryTracker.Load(key)
	if !ok {
		return 0
	}
	if attemptsSlice, ok := attempts.([]time.Time); ok {
		// Return the length of the slice as the count of recent recovery attempts
		return len(attemptsSlice)
	}
	return 0
}

func (h *remediationMgrHelper) dropOlderRecoveryAttemptsInternal(nodeName string, nodeCondition string, windowSize string) error {
	key := h.getRecoveryTrackerKey(nodeName, nodeCondition)

	attempts, _ := h.recoveryTracker.LoadOrStore(key, []time.Time{})
	if attemptsSlice, ok := attempts.([]time.Time); ok {
		windowSizeDuration, err := time.ParseDuration(windowSize)
		if err != nil {
			return fmt.Errorf("failed to parse window size %s: %w", windowSize, err)
		}

		// Filter out attempts older than the window size
		cutoffTime := time.Now().UTC().Add(-time.Duration(windowSizeDuration))
		startIndex := len(attemptsSlice)
		for i, attempt := range attemptsSlice {
			if attempt.After(cutoffTime) {
				startIndex = i
				break
			}
		}
		filtered := attemptsSlice[startIndex:]
		h.recoveryTracker.Store(key, filtered)
	} else {
		return fmt.Errorf("failed to cast recovery tracker value to []time.Time")
	}

	return nil
}

func (h *remediationMgrHelper) dropOlderRecoveryAttemptsFromStatusCR(ctx context.Context, namespace string) error {
	windowSize := DefaultStatusCRCleanupWindowSize
	wfStatus, err := h.getRemediationWorkflowStatus(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to get remediation workflow status: %w", err)
	}

	if wfStatus.Status == nil {
		return nil // Nothing to drop
	}

	wfStatusCopy := wfStatus.DeepCopy()
	windowSizeDuration, err := time.ParseDuration(windowSize)
	if err != nil {
		return fmt.Errorf("failed to parse window size %s: %w", windowSize, err)
	}

	cutoffTime := time.Now().UTC().Add(-time.Duration(windowSizeDuration))

	for nodeName, conditions := range wfStatus.Status {
		for nodeCondition, attempts := range conditions {
			filtered := []amdv1alpha1.WorkflowMetadata{}
			for _, attempt := range attempts {
				attemptTime, err := time.Parse(DefaultTimeFormatLayout, attempt.StartTime)
				if err != nil {
					return fmt.Errorf("failed to parse attempt start time %s: %w", attempt.StartTime, err)
				}
				if attemptTime.After(cutoffTime) {
					filtered = append(filtered, attempt)
				}
			}
			// Update the status for the node and condition with the filtered attempts
			if len(filtered) > 0 {
				wfStatus.Status[nodeName][nodeCondition] = filtered
			} else {
				// If no attempts are left, remove the condition from the node
				delete(wfStatus.Status[nodeName], nodeCondition)
			}
		}
		// If no conditions are left for the node, remove the node from the status
		if len(wfStatus.Status[nodeName]) == 0 {
			delete(wfStatus.Status, nodeName)
		}
	}

	if err := h.client.Status().Patch(ctx, wfStatus, client.MergeFrom(wfStatusCopy)); err != nil {
		return fmt.Errorf("failed to patch remediation workflow status: %w", err)
	}

	return nil
}

func (h *remediationMgrHelper) registerRecoveryAttempt(ctx context.Context, nodeName string, nodeCondition string, namespace string, wfName string) error {
	startTime := time.Now().UTC()

	// Register the recovery attempt in internal map
	if err := h.registerRecoveryAttemptInternal(nodeName, nodeCondition, namespace, startTime); err != nil {
		return fmt.Errorf("failed to register recovery attempt: %w", err)
	}

	// Register the recovery attempt in the status CR
	if err := h.registerRecoveryAttemptToStatusCR(ctx, nodeName, nodeCondition, namespace, wfName, startTime); err != nil {
		return fmt.Errorf("failed to register recovery attempt in status CR: %w", err)
	}

	return nil
}

func (h *remediationMgrHelper) registerRecoveryAttemptToStatusCR(ctx context.Context, nodeName string, nodeCondition string, namespace string, wfName string, startTime time.Time) error {
	wfStatus, err := h.getRemediationWorkflowStatus(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to get remediation workflow status: %w", err)
	}

	wfStatusCopy := wfStatus.DeepCopy()

	if wfStatus.Status == nil {
		wfStatus.Status = make(map[string]map[string][]amdv1alpha1.WorkflowMetadata)
	}
	if wfStatus.Status[nodeName] == nil {
		wfStatus.Status[nodeName] = make(map[string][]amdv1alpha1.WorkflowMetadata)
	}
	if wfStatus.Status[nodeName][nodeCondition] == nil {
		wfStatus.Status[nodeName][nodeCondition] = []amdv1alpha1.WorkflowMetadata{}
	}

	// Create a new WorkflowMetadata entry
	metadata := amdv1alpha1.WorkflowMetadata{
		Name:      wfName,
		StartTime: startTime.Format(DefaultTimeFormatLayout),
	}

	// Append the new metadata entry to the status
	wfStatus.Status[nodeName][nodeCondition] = append(wfStatus.Status[nodeName][nodeCondition], metadata)

	// Patch the wfStatus with the new entry
	if err := h.client.Status().Patch(ctx, wfStatus, client.MergeFrom(wfStatusCopy)); err != nil {
		return fmt.Errorf("failed to patch remediation workflow status: %w", err)
	}
	return nil
}

func (h *remediationMgrHelper) registerRecoveryAttemptInternal(nodeName string, nodeCondition string, namespace string, startTime time.Time) error {
	key := h.getRecoveryTrackerKey(nodeName, nodeCondition)

	attempts, _ := h.recoveryTracker.LoadOrStore(key, []time.Time{})
	if attemptsSlice, ok := attempts.([]time.Time); ok {
		attemptsSlice = append(attemptsSlice, startTime)
		h.recoveryTracker.Store(key, attemptsSlice)
	} else {
		return fmt.Errorf("failed to cast recovery tracker value to []time.Time")
	}

	return nil
}

func (h *remediationMgrHelper) getRecoveryTrackerKey(nodeName string, nodeCondition string) string {
	return fmt.Sprintf("%s-%s", nodeName, nodeCondition)
}

func (h *remediationMgrHelper) getMaxAllowedRunsPerWindow(recoveryPolicy *RecoveryPolicyConfig) int {
	if recoveryPolicy == nil || recoveryPolicy.MaxAllowedRunsPerWindow == 0 {
		return DefaultRecoveryPolicyMaxRunsPerWindow
	}
	return recoveryPolicy.MaxAllowedRunsPerWindow
}

func (h *remediationMgrHelper) getWindowSize(recoveryPolicy *RecoveryPolicyConfig) string {
	if recoveryPolicy == nil || recoveryPolicy.WindowSize == "" {
		return DefaultRecoveryPolicyWindowSize
	}
	return recoveryPolicy.WindowSize
}

func (h *remediationMgrHelper) createRemediationWorkflowStatus(ctx context.Context, namespace string) (*amdv1alpha1.RemediationWorkflowStatus, error) {
	wfstatus := &amdv1alpha1.RemediationWorkflowStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace,
		},
		Status: make(map[string]map[string][]amdv1alpha1.WorkflowMetadata),
	}

	if err := h.client.Create(ctx, wfstatus); err != nil {
		return nil, fmt.Errorf("failed to create remediation workflow status: %w", err)
	}
	return wfstatus, nil
}

func (h *remediationMgrHelper) getRemediationWorkflowStatus(ctx context.Context, namespace string) (*amdv1alpha1.RemediationWorkflowStatus, error) {
	wfstatus := &amdv1alpha1.RemediationWorkflowStatus{}
	err := h.client.Get(ctx, client.ObjectKey{Name: "default", Namespace: namespace}, wfstatus)
	if err != nil {
		return nil, fmt.Errorf("failed to get remediation workflow status: %w", err)
	}
	return wfstatus, nil
}

func (h *remediationMgrHelper) syncInternalMapFromStatusCR(ctx context.Context, namespace string) error {
	wfStatus, err := h.getRemediationWorkflowStatus(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to get remediation workflow status: %w", err)
	}

	h.recoveryTracker = new(sync.Map)

	for nodeName, conditions := range wfStatus.Status {
		for nodeCondition, attempts := range conditions {
			key := h.getRecoveryTrackerKey(nodeName, nodeCondition)

			attemptTimes := make([]time.Time, len(attempts))
			for i, attempt := range attempts {
				attemptTime, err := time.Parse(DefaultTimeFormatLayout, attempt.StartTime)
				if err != nil {
					return fmt.Errorf("failed to parse attempt start time %s: %w", attempt.StartTime, err)
				}
				attemptTimes[i] = attemptTime
			}
			h.recoveryTracker.Store(key, attemptTimes)
		}
	}

	return nil
}

func (h *remediationMgrHelper) isRecoveryPolicyViolated(ctx context.Context, nodeName string, mapping *ConditionWorkflowMapping) bool {
	logger := log.FromContext(ctx)

	maxAllowedRuns := h.getMaxAllowedRunsPerWindow(&mapping.RecoveryPolicy)
	recentRecoveryCount := h.getRecentRecoveryCount(nodeName, mapping.NodeCondition)

	logger.Info(fmt.Sprintf("Recent recovery count for node %s and condition %s: %d", nodeName, mapping.NodeCondition, recentRecoveryCount))
	logger.Info(fmt.Sprintf("Max allowed runs per window for node %s and condition %s: %d", nodeName, mapping.NodeCondition, maxAllowedRuns))
	return recentRecoveryCount > maxAllowedRuns
}

func (h *remediationMgrHelper) isNodeLabelledForForceResume(ctx context.Context, nodeObj *v1.Node) bool {
	if labelValue, exists := nodeObj.Labels[ForceResumeWorkflowLabelKey]; exists && labelValue == ForceResumeWorkflowLabelValue {
		return true
	}
	return false
}

func (h *remediationMgrHelper) removeForceResumeWorkflowLabelFromNode(ctx context.Context, nodeObj *v1.Node) error {
	logger := log.FromContext(ctx)

	if labelValue, exists := nodeObj.Labels[ForceResumeWorkflowLabelKey]; exists {
		if labelValue == ForceResumeWorkflowLabelValue {
			original := nodeObj.DeepCopy()
			delete(nodeObj.Labels, ForceResumeWorkflowLabelKey)

			if err := h.client.Patch(ctx, nodeObj, client.MergeFrom(original)); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to remove label %q from node %s using Patch", ForceResumeWorkflowLabelKey, nodeObj.Name))
				return err
			}
			logger.Info(fmt.Sprintf("Successfully removed label %q from node %s", ForceResumeWorkflowLabelKey, nodeObj.Name))
		}
	}
	return nil
}

func (h *remediationMgrHelper) canResumeWorkflowOnNode(ctx context.Context, node *v1.Node, mapping *ConditionWorkflowMapping, stageName string) bool {
	logger := log.FromContext(ctx)

	// Check if the recovery policy is violated, if so, do not allow resumption
	recoveryPolicyViolated := h.isRecoveryPolicyViolated(ctx, node.Name, mapping)
	if recoveryPolicyViolated {
		logger.Info(fmt.Sprintf("Recovery policy is violated for node %s with condition %s, not allowing workflow resumption", node.Name, mapping.NodeCondition))
		return false
	}

	// if no physical action is needed, allow resumption of workflow
	if !mapping.PhysicalActionNeeded && stageName != "autostart" {
		return true
	}

	// in case physical action is needed, check if the node is labelled for force resume
	resume := h.isNodeLabelledForForceResume(ctx, node)
	if !resume {
		logger.Info(fmt.Sprintf("Node %s is not labelled for force resume, not allowing workflow resumption", node.Name))
	}
	return resume
}

func (h *remediationMgrHelper) isNodeLabelledForAbortWorkflow(node *v1.Node) bool {
	if labelValue, exists := node.Labels[AbortWorkflowLabelKey]; exists && labelValue == AbortWorkflowLabelValue {
		return true
	}
	return false
}

func (h *remediationMgrHelper) removeAbortWorkflowLabelFromNode(ctx context.Context, nodeObj *v1.Node) error {
	logger := log.FromContext(ctx)
	if labelValue, exists := nodeObj.Labels[AbortWorkflowLabelKey]; exists && labelValue == AbortWorkflowLabelValue {
		original := nodeObj.DeepCopy()
		delete(nodeObj.Labels, AbortWorkflowLabelKey)
		if err := h.client.Patch(ctx, nodeObj, client.MergeFrom(original)); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to remove label %q on node %s", AbortWorkflowLabelKey, nodeObj.Name))
			return err
		}
		logger.Info(fmt.Sprintf("Successfully removed label %q from node %s", AbortWorkflowLabelKey, nodeObj.Name))
	}
	return nil
}

func (h *remediationMgrHelper) abortWorkflow(ctx context.Context, wf *workflowv1alpha1.Workflow) error {
	logger := log.FromContext(ctx)

	// Delete the workflow
	if err := h.client.Delete(ctx, wf); err != nil {
		return fmt.Errorf("failed to delete workflow %s: %w", wf.Name, err)
	}

	logger.Info(fmt.Sprintf("Workflow %s aborted successfully", wf.Name))
	return nil
}

func (h *remediationMgrHelper) getNodeLabelsFromCR(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) []string {
	nodeLabels := make([]string, 0)
	for key, value := range devConfig.Spec.RemediationWorkflow.NodeRemediationLabels {
		nodeLabels = append(nodeLabels, fmt.Sprintf("%s=%s", key, value))
	}
	return nodeLabels
}

func (h *remediationMgrHelper) getNodeTaintsFromCR(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig) []string {
	taints := make([]string, 0)
	for _, taint := range devConfig.Spec.RemediationWorkflow.NodeRemediationTaints {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
	}
	return taints
}

// getNodeTaints returns the list of taints to be applied to the node during remediation.
// If no user configured taints are found in the DeviceConfig CR, it returns a default taint.
func (h *remediationMgrHelper) getNodeTaints(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string) []string {
	taints := h.getNodeTaintsFromCR(ctx, devConfig)
	if len(taints) == 0 {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", RemediationTaintKey, nodeCondition, v1.TaintEffectNoSchedule))
	}
	return taints
}

func (h *remediationMgrHelper) applyTolerationsToWorkflow(wf *workflowv1alpha1.Workflow, devConfig *amdv1alpha1.DeviceConfig, nodeCondition string) {
	taints := make([]v1.Taint, 0)
	if len(devConfig.Spec.RemediationWorkflow.NodeRemediationTaints) > 0 {
		taints = devConfig.Spec.RemediationWorkflow.NodeRemediationTaints
	} else {
		taints = append(taints, v1.Taint{
			Key:    RemediationTaintKey,
			Value:  nodeCondition,
			Effect: v1.TaintEffectNoSchedule,
		})
	}
	for i := range wf.Spec.Templates {
		if wf.Spec.Templates[i].Tolerations == nil {
			wf.Spec.Templates[i].Tolerations = []v1.Toleration{}
		}
		for _, taint := range taints {
			wf.Spec.Templates[i].Tolerations = append(wf.Spec.Templates[i].Tolerations, v1.Toleration{
				Key:      taint.Key,
				Operator: v1.TolerationOpExists,
				Effect:   taint.Effect,
			})
		}
	}
}
