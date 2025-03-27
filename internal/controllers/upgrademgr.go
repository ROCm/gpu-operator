/*
Copyright 2024.

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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"

	utils "github.com/ROCm/gpu-operator/internal"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/kubectl/pkg/drain"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultUtilsImage = "docker.io/rocm/gpu-operator-utils:latest"
	defaultSAName     = "amd-gpu-operator-utils-container"
)

type upgradeMgr struct {
	helper upgradeMgrHelperAPI
}

//go:generate mockgen -source=upgrademgr.go -package=controllers -destination=mock_upgrademgr.go UpgradeMgr
type upgradeMgrAPI interface {
	HandleUpgrade(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (ctrl.Result, error)
	HandleDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (ctrl.Result, error)
	GetNodeStatus(nodeName string) amdv1alpha1.UpgradeState
	GetNodeUpgradeStartTime(nodeName string) string
}

func newUpgradeMgrHandler(client client.Client, k8sConfig *rest.Config) upgradeMgrAPI {
	k8sIntf, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil
	}
	return &upgradeMgr{
		helper: newUpgradeMgrHelperHandler(client, k8sIntf),
	}
}

/*================================= Upgrade Manager APIs===================================*/

// HandleUpgrade handles the upgrade functionalities for device config
func (n *upgradeMgr) HandleUpgrade(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodeList *v1.NodeList) (ctrl.Result, error) {
	res := ctrl.Result{}

	var candidateNodes []v1.Node
	var upgradeDone, upgradeInProgress, upgradeFailedState int

	if deviceConfig.Spec.Driver.UpgradePolicy == nil ||
		(deviceConfig.Spec.Driver.UpgradePolicy.Enable != nil &&
			!*deviceConfig.Spec.Driver.UpgradePolicy.Enable) {
		// No upgrade policy enabled. Manual upgrade on all nodes
		return ctrl.Result{}, nil
	}

	initInternalNodeStates := func(deviceConfig *amdv1alpha1.DeviceConfig) {
		for nodeName, moduleStatus := range deviceConfig.Status.NodeModuleStatus {
			if moduleStatus.Status == amdv1alpha1.UpgradeStateStarted {
				if deviceConfig.Spec.Driver.UpgradePolicy.RebootRequired != nil && *deviceConfig.Spec.Driver.UpgradePolicy.RebootRequired {
					nodeObj, err := n.helper.getNode(ctx, nodeName)
					if err == nil {
						log.FromContext(ctx).Info("Reboot is required for driver upgrade, triggering node reboot")
						n.helper.handleNodeReboot(ctx, nodeObj, deviceConfig)
					}
				} else {
					log.FromContext(ctx).Info("Resetting Upgrade State to UpgradeStateEmpty")
					n.helper.setNodeStatus(ctx, nodeName, amdv1alpha1.UpgradeStateEmpty)
				}
			} else if moduleStatus.Status == amdv1alpha1.UpgradeStateRebootInProgress {
				// Operator restarted during upgrade operation. Schedule the reboot pod deletion
				log.FromContext(ctx).Info("Reboot is in progress, scheduling reboot pod deletion")
				n.helper.setNodeStatus(ctx, nodeName, moduleStatus.Status)
				go n.helper.deleteRebootPod(ctx, nodeName, deviceConfig, false, deviceConfig.Generation)
			} else {
				n.helper.setNodeStatus(ctx, nodeName, moduleStatus.Status)
			}
		}
	}

	if n.helper.isInit() {
		log.FromContext(ctx).Info("Operator coming up, initializing internal node states")
		initInternalNodeStates(deviceConfig)
	}

	if n.helper.specChanged(deviceConfig) {
		/* Reset internal states for nodes not in failed state or if in failed state but uncordoned */
		for i := 0; i < len(nodeList.Items); i++ {
			if n.helper.isNodeStateUpgradeFailed(ctx, &nodeList.Items[i], deviceConfig) {
				if !nodeList.Items[i].Spec.Unschedulable {
					n.helper.setNodeStatus(ctx, nodeList.Items[i].Name, amdv1alpha1.UpgradeStateEmpty)
				}
				continue
			} else {
				n.helper.setNodeStatus(ctx, nodeList.Items[i].Name, amdv1alpha1.UpgradeStateEmpty)
			}
		}
	}
	n.helper.setcurrentSpec(deviceConfig)

	for i := 0; i < len(nodeList.Items); i++ {

		// 1. Set init status for unprocessed nodes
		n.helper.handleInitStatus(ctx, &nodeList.Items[i])

		// 2. Handle failed nodes
		if n.helper.isNodeStateUpgradeFailed(ctx, &nodeList.Items[i], deviceConfig) {
			n.helper.clearUpgradeStartTime(nodeList.Items[i].Name)
			upgradeFailedState++
			continue
		}

		// 3. Handle Started Nodes
		if n.helper.isNodeStateUpgradeStarted(&nodeList.Items[i]) {
			upgradeInProgress++
			continue
		}

		// 4. Handle Completed nodes
		if n.helper.isNodeReady(ctx, &nodeList.Items[i], deviceConfig) {
			n.helper.clearUpgradeStartTime(nodeList.Items[i].Name)
			upgradeDone++
			continue
		}

		// 5. Handle New nodes
		if n.helper.isNodeNew(ctx, &nodeList.Items[i], deviceConfig) {
			// Driver will be unconditionally installed on new node
			continue
		}

		// 6. Handle Driver Install In Progres nodes
		if n.helper.isNodeStateInstallInProgress(ctx, &nodeList.Items[i], deviceConfig) {
			continue
		}

		// 7. Handle Driver Upgrade InProgress nodes
		if n.helper.isNodeStateUpgradeInProgress(ctx, &nodeList.Items[i], deviceConfig) {
			upgradeInProgress++
			continue
		}

		if !n.helper.isNodeReadyForUpgrade(ctx, &nodeList.Items[i]) {
			res = ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}
			continue
		}

		//This node is a candidate for selection
		candidateNodes = append(candidateNodes, nodeList.Items[i])
	}

	if len(candidateNodes) == 0 && (upgradeInProgress > 0) {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
	}
	// All nodes have correct drivers installed
	if upgradeDone == len(nodeList.Items) || len(candidateNodes) == 0 {
		return res, nil
	}

	maxParallelUpgrades, policyViolated := n.helper.isUpgradePolicyViolated(upgradeInProgress, upgradeFailedState, len(nodeList.Items), deviceConfig)

	if policyViolated {
		// Re-try after 20 seconds as the policy does not allow more parallel nodes
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
	}

	// Add nodes per policy
	for i := 0; i < (maxParallelUpgrades-upgradeInProgress) && i < len(candidateNodes); i++ {

		// Mark the state as progress
		n.helper.setNodeStatus(ctx, candidateNodes[i].Name, amdv1alpha1.UpgradeStateStarted)
		n.helper.setUpgradeStartTime(candidateNodes[i].Name)
		// Drain/Delete the pods and set the expected module version in module-config label of the ndoe
		go n.helper.handleNodeUpgrade(ctx, *deviceConfig, candidateNodes[i])

	}

	return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 20}, nil
}

// HandleDelete handles the delete operations during upgrade process
func (n *upgradeMgr) HandleDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, nodeList *v1.NodeList) (res ctrl.Result, err error) {

	for i := 0; i < len(nodeList.Items); i++ {
		if err := n.helper.cordonOrUncordonNode(ctx, deviceConfig, &nodeList.Items[i], false); err != nil {
			log.FromContext(ctx).Error(err, fmt.Sprintf("Taint Removal failed for %v during deviceconfig delete:%v", &nodeList.Items[i].Name, err))
		}
		n.helper.deleteRebootPod(ctx, nodeList.Items[i].Name, deviceConfig, true, deviceConfig.Generation)
	}
	n.helper.clearNodeStatus()
	return
}

// GetNodeStatus returns the upgrade status of the node
func (n *upgradeMgr) GetNodeStatus(nodeName string) (status amdv1alpha1.UpgradeState) {
	return n.helper.getNodeStatus(nodeName)
}

// GetNodeStaGetNodeUpgradeStartTimetus returns the time when upgrade started on the node
func (n *upgradeMgr) GetNodeUpgradeStartTime(nodeName string) string {
	return n.helper.getUpgradeStartTime(nodeName)
}

/*=========================================== Upgrade Manager Helper APIs ==========================================*/

//go:generate mockgen -source=upgrademgr.go -package=controllers -destination=mock_upgrademgr.go upgradeMgrHelperAPI
type upgradeMgrHelperAPI interface {
	// Initialize node status
	handleInitStatus(ctx context.Context, node *v1.Node)

	// Handle node state transitions
	isNodeReady(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool
	isNodeNew(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool
	isNodeStateUpgradeStarted(node *v1.Node) bool
	isNodeStateInstallInProgress(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool
	isNodeStateUpgradeInProgress(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool
	isNodeReadyForUpgrade(ctx context.Context, node *v1.Node) bool
	isNodeStateUpgradeFailed(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool
	isUpgradePolicyViolated(upgradeInProgress int, upgradeFailedState int, totalNodes int, deviceConfig *amdv1alpha1.DeviceConfig) (int, bool)

	// Helper APIs for upgrade-in-progress nodes
	cordonOrUncordonNode(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node, add bool) error
	handleNodeUpgrade(ctx context.Context, deviceConfig amdv1alpha1.DeviceConfig, node v1.Node)
	isDeviceConfigValid(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig) bool
	getPodsToDrainOrDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) (newPods []v1.Pod, err error)
	deleteOrDrainPods(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) error
	updateModuleVersionOnNode(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) error
	handleNodeReboot(ctx context.Context, node *v1.Node, dc *amdv1alpha1.DeviceConfig)
	deleteRebootPod(ctx context.Context, nodeName string, dc *amdv1alpha1.DeviceConfig, force bool, genId int64)
	getRebootPod(nodeName string, dc *amdv1alpha1.DeviceConfig) *v1.Pod

	// getters and setters
	specChanged(deviceConfig *amdv1alpha1.DeviceConfig) bool
	setcurrentSpec(deviceConfig *amdv1alpha1.DeviceConfig)
	getNodeStatus(nodeName string) amdv1alpha1.UpgradeState
	getNode(ctx context.Context, nodeName string) (node *v1.Node, err error)
	setNodeStatus(ctx context.Context, nodeName string, status amdv1alpha1.UpgradeState)
	getUpgradeStartTime(nodeName string) string
	setUpgradeStartTime(nodeName string)
	clearUpgradeStartTime(nodeName string)
	checkUpgradeTimeExceeded(ctx context.Context, nodeName string, deviceConfig *amdv1alpha1.DeviceConfig) bool
	clearNodeStatus()
	isInit() bool
}

type upgradeMgrHelper struct {
	client               client.Client
	k8sInterface         kubernetes.Interface
	drainHelper          *drain.Helper
	nodeStatus           *sync.Map
	nodeUpgradeStartTime *sync.Map
	init                 bool
	currentSpec          driverSpec
}

type driverSpec struct {
	version string
	enable  bool
}

// Initialize upgrade manager helper interface
func newUpgradeMgrHelperHandler(client client.Client, k8sInterface kubernetes.Interface) upgradeMgrHelperAPI {
	return &upgradeMgrHelper{
		client:               client,
		k8sInterface:         k8sInterface,
		nodeStatus:           new(sync.Map),
		nodeUpgradeStartTime: new(sync.Map),
	}
}

// Get init status
func (h *upgradeMgrHelper) isInit() (status bool) {
	if !h.init {
		status = true
		h.init = true
	}
	return
}

// Handle the init state for every node.
func (h *upgradeMgrHelper) handleInitStatus(ctx context.Context, node *v1.Node) {

	if h.getNodeStatus(node.Name) == amdv1alpha1.UpgradeStateEmpty {
		log.FromContext(ctx).Info("Setting upgrade state to UpgradeNotStarted")
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateNotStarted)
	}
}

// Handle New nodes. New nodes are not subjected to upgrade policy
func (h *upgradeMgrHelper) isNodeNew(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool {

	// The following applies for a new node
	// 1. If the version-module label is not available on the node
	// 2. If the version-module label on the node is different from CR and previous driver install was still in progress

	if moduleName, ok := node.Labels[fmt.Sprintf("kmm.node.kubernetes.io/version-module.%s.%s", deviceConfig.Namespace, deviceConfig.Name)]; !ok {
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateInstallInProgress)
		return true

	} else {

		// Upgrade of driver version in CR when the driver install was going on on new node
		if nodeStatus, ok := deviceConfig.Status.NodeModuleStatus[node.Name]; ok {
			if moduleName != deviceConfig.Spec.Driver.Version && nodeStatus.Status == amdv1alpha1.UpgradeStateInstallInProgress {

				// Update expected module version on the node
				if err := h.updateModuleVersionOnNode(ctx, deviceConfig, node); err != nil {
					log.FromContext(ctx).Error(err, fmt.Sprintf("Node: %v State: amdv1alpha1.UpgradeStateInstallInProgress UpgradeFailed with Error: %v", node.Name, err))
					// Mark the state as failed
					h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateFailed)
				}

				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateInstallInProgress)
				return true
			}
		}
	}

	return false
}

// Handle Driver installation for ready nodes.
func (h *upgradeMgrHelper) isNodeReady(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool {

	// Move the node state to complete if the driver install is done
	if nodeStatus, ok := deviceConfig.Status.NodeModuleStatus[node.Name]; ok {
		// If driver install is done but CR version not specified, get default version
		driverVersion, _ := utils.GetDriverVersion(*node, *deviceConfig)
		if strings.HasSuffix(nodeStatus.ContainerImage, driverVersion) {

			currentState := h.getNodeStatus(node.Name)

			// Return if the node is already taken care
			if currentState == amdv1alpha1.UpgradeStateComplete || currentState == amdv1alpha1.UpgradeStateInstallComplete {
				return true
			}

			// Move to failure state if uncordon fails
			if err := h.cordonOrUncordonNode(ctx, deviceConfig, node, false); err != nil {
				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateUncordonFailed)
				return false
			}

			// Set InstallComplete/UpgradeComplete
			if currentState == amdv1alpha1.UpgradeStateInstallInProgress {
				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateInstallComplete)
			} else {
				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateComplete)
			}

			return true
		}
	}

	return false
}

// Handle Driver installation for reboot pending nodes (new).
func (h *upgradeMgrHelper) isNodeStateUpgradeStarted(node *v1.Node) bool {

	return h.getNodeStatus(node.Name) == amdv1alpha1.UpgradeStateStarted || h.getNodeStatus(node.Name) == amdv1alpha1.UpgradeStateRebootInProgress

}

// Handle Driver installation for inprogress nodes (new).
func (h *upgradeMgrHelper) isNodeStateInstallInProgress(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool {

	return h.getNodeStatus(node.Name) == amdv1alpha1.UpgradeStateInstallInProgress

}

// Check the in progress status for nodes that are being upgraded.
func (h *upgradeMgrHelper) isNodeStateUpgradeInProgress(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool {

	// CR update when gpu operator restarted.
	currentStatus := h.getNodeStatus(node.Name)
	if moduleName, ok := node.Labels[fmt.Sprintf("kmm.node.kubernetes.io/version-module.%s.%s", deviceConfig.Namespace, deviceConfig.Name)]; ok {
		if moduleName != deviceConfig.Spec.Driver.Version && currentStatus == amdv1alpha1.UpgradeStateInProgress {

			// Update expected module version on the node
			if err := h.updateModuleVersionOnNode(ctx, deviceConfig, node); err != nil {
				log.FromContext(ctx).Error(err, fmt.Sprintf("Node: %v State: %v UpgradeFailed with Error: %v", node.Name, currentStatus, err))
				// Mark the state as failed
				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateFailed)
			}
			return true
		}
	}

	return h.getNodeStatus(node.Name) == amdv1alpha1.UpgradeStateInProgress
}

// Check the Failure status for nodes that are being upgraded.
func (h *upgradeMgrHelper) isNodeStateUpgradeFailed(ctx context.Context, node *v1.Node, deviceConfig *amdv1alpha1.DeviceConfig) bool {

	var nodeUpgradeTimeout bool
	nodeStatus := h.getNodeStatus(node.Name)
	nodeUpgradeTimeout = h.checkUpgradeTimeExceeded(ctx, node.Name, deviceConfig)
	if nodeUpgradeTimeout {
		log.FromContext(ctx).Info(fmt.Sprintf("Node: %v, Upgrade Timeout exceeded", node.Name))
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateFailed)
		return true
	}
	return (nodeStatus == amdv1alpha1.UpgradeStateFailed ||
		nodeStatus == amdv1alpha1.UpgradeStateCordonFailed ||
		nodeStatus == amdv1alpha1.UpgradeStateUncordonFailed ||
		nodeStatus == amdv1alpha1.UpgradeStateDrainFailed)

}

// Check if node is ready to be upgraded
func (h *upgradeMgrHelper) isNodeReadyForUpgrade(ctx context.Context, node *v1.Node) bool {

	if node.Spec.Unschedulable {
		return false
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady && condition.Status != v1.ConditionTrue {
			return false
		}
	}
	return true
}

func (h *upgradeMgrHelper) isUpgradePolicyViolated(upgradeInProgress int, upgradeFailedState int, totalNodes int, deviceConfig *amdv1alpha1.DeviceConfig) (int, bool) {

	maxParallelUpdates := deviceConfig.Spec.Driver.UpgradePolicy.MaxParallelUpgrades
	maxUnavailableNodes, err := intstr.GetScaledValueFromIntOrPercent(&deviceConfig.Spec.Driver.UpgradePolicy.MaxUnavailableNodes, totalNodes, true)
	if err != nil {
		return maxParallelUpdates, true
	}

	return maxParallelUpdates, (upgradeInProgress >= maxParallelUpdates) || (upgradeFailedState >= maxUnavailableNodes)

}

func (h *upgradeMgrHelper) getUpgradeStartTime(nodeName string) string {
	if value, ok := h.nodeUpgradeStartTime.Load(nodeName); ok {
		return value.(string)
	}

	return ""
}

func (h *upgradeMgrHelper) setUpgradeStartTime(nodeName string) {
	currentTime := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	h.nodeUpgradeStartTime.Store(nodeName, currentTime)
}

func (h *upgradeMgrHelper) clearUpgradeStartTime(nodeName string) {
	h.nodeUpgradeStartTime.Store(nodeName, "")
}

func (h *upgradeMgrHelper) checkUpgradeTimeExceeded(ctx context.Context, nodeName string, deviceConfig *amdv1alpha1.DeviceConfig) bool {
	// Fetch upgrade time started from node module status to ensure handling timeouts across operator restarts
	for name, moduleStatus := range deviceConfig.Status.NodeModuleStatus {
		if name == nodeName {
			upgradeStartTime := h.getUpgradeStartTime(nodeName)
			// If empty, it means UpgradeStartTime was cleared internally since Upgrade is in Complete/Failed state. But if Upgrade is in Progress and map value of start time was cleared, it means Operator restarted during upgrade. In this case, we should check for Timeout using original StartTime from the device config status
			if upgradeStartTime == "" {
				if moduleStatus.Status == amdv1alpha1.UpgradeStateStarted || moduleStatus.Status == amdv1alpha1.UpgradeStateInstallInProgress || moduleStatus.Status == amdv1alpha1.UpgradeStateInProgress || moduleStatus.Status == amdv1alpha1.UpgradeStateRebootInProgress {
					upgradeStartTime = moduleStatus.UpgradeStartTime
				} else {
					return false
				}
			}

			upgradeTime, err := time.Parse("2006-01-02 15:04:05 UTC", upgradeStartTime)
			if err != nil {
				return false
			}

			// Check if Upgrade has been in progress for more than 2 hours
			if time.Since(upgradeTime) > (2 * time.Hour) {
				return true
			}
		}
	}
	return false
}

func (h *upgradeMgrHelper) getNodeStatus(nodeName string) amdv1alpha1.UpgradeState {

	if value, ok := h.nodeStatus.Load(nodeName); ok {
		return value.(amdv1alpha1.UpgradeState)
	}
	return amdv1alpha1.UpgradeStateEmpty
}

func (h *upgradeMgrHelper) setNodeStatus(ctx context.Context, nodeName string, status amdv1alpha1.UpgradeState) {
	if h.getNodeStatus(nodeName) != status {
		log.FromContext(ctx).Info(fmt.Sprintf("UpgradeStateTransition Node: %v from %v state to %v", nodeName, h.getNodeStatus(nodeName), status))
		h.nodeStatus.Store(nodeName, status)
	}
}

func (h *upgradeMgrHelper) getNode(ctx context.Context, nodeName string) (*v1.Node, error) {
	nodeObj := &v1.Node{}
	var err error
	if err = h.client.Get(ctx, client.ObjectKey{Name: nodeName}, nodeObj); err == nil {
		return nodeObj, nil
	}
	return nodeObj, err
}

func (h *upgradeMgrHelper) clearNodeStatus() {

	h.nodeStatus = new(sync.Map)
}

func (h *upgradeMgrHelper) specChanged(deviceConfig *amdv1alpha1.DeviceConfig) bool {
	if h.currentSpec.version != "" && h.currentSpec.version != deviceConfig.Spec.Driver.Version {
		return true
	}
	return false
}

func (h *upgradeMgrHelper) setcurrentSpec(deviceConfig *amdv1alpha1.DeviceConfig) {
	h.currentSpec.version = deviceConfig.Spec.Driver.Version
	h.currentSpec.enable = false
	if deviceConfig.Spec.Driver.Enable != nil {
		h.currentSpec.enable = *deviceConfig.Spec.Driver.Enable
	}
}

func (h *upgradeMgrHelper) deleteOrDrainPods(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) error {

	pods, err := h.getPodsToDrainOrDelete(ctx, deviceConfig, node)

	if err != nil {
		return err
	}

	if len(pods) > 0 {

		h.drainHelper = &drain.Helper{
			Ctx:                 ctx,
			Client:              h.k8sInterface,
			Out:                 os.Stdout,
			ErrOut:              os.Stdout,
			DisableEviction:     false,
			IgnoreAllDaemonSets: true,
			GracePeriodSeconds:  -1,
			DeleteEmptyDirData:  true,
		}

		dc := deviceConfig.Spec.Driver.UpgradePolicy
		if dc.NodeDrainPolicy != nil {
			h.drainHelper.Force = false
			if dc.NodeDrainPolicy.Force != nil && *dc.NodeDrainPolicy.Force {
				h.drainHelper.Force = *dc.NodeDrainPolicy.Force
			}
			h.drainHelper.Timeout = time.Duration(float64(dc.NodeDrainPolicy.TimeoutSeconds) * float64(time.Second))
		} else if dc.PodDeletionPolicy != nil {
			h.drainHelper.DisableEviction = true
			h.drainHelper.Force = false
			if dc.PodDeletionPolicy.Force != nil && *dc.PodDeletionPolicy.Force {
				h.drainHelper.Force = *dc.PodDeletionPolicy.Force
			}
			h.drainHelper.Timeout = time.Duration(float64(dc.PodDeletionPolicy.TimeoutSeconds) * float64(time.Second))
		}

		if err := h.drainHelper.DeleteOrEvictPods(pods); err != nil {
			return err
		}
	}
	return nil
}

func (h *upgradeMgrHelper) getPodsToDrainOrDelete(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) (newPods []v1.Pod, err error) {

	options := metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": node.Name}).String(),
	}
	pods, err := h.k8sInterface.CoreV1().Pods(metav1.NamespaceAll).List(ctx, options)

	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, fmt.Sprintf("%v-%v", deviceConfig.Name, "metrics-exporter")) {
			newPods = append(newPods, pod)
			continue
		}
		for _, container := range pod.Spec.Containers {
			if _, ok := container.Resources.Requests["amd.com/gpu"]; ok {
				newPods = append(newPods, pod)
				break
			}
		}
	}
	return
}

func (h *upgradeMgrHelper) handleNodeUpgrade(ctx context.Context, deviceConfig amdv1alpha1.DeviceConfig, node v1.Node) {

	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Node: %v Upgrade begin", node.Name))

	// Nothing more to do if the label is already set. Node might have rebooted
	nodeObj := &v1.Node{}
	if err := h.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err == nil {
		if version, ok := nodeObj.Labels[fmt.Sprintf("kmm.node.kubernetes.io/version-module.%s.%s", deviceConfig.Namespace, deviceConfig.Name)]; ok {
			if version == deviceConfig.Spec.Driver.Version {
				log.FromContext(ctx).Info("Setting Upgrade State to Upgrade-In-Progress since label is already set")
				h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateInProgress)
				return
			}
		}
	}

	// Cordon the node to prevent scheduling of new nodes
	cordonErr := h.cordonOrUncordonNode(ctx, &deviceConfig, &node, true)
	if deviceConfigValid := h.isDeviceConfigValid(context.TODO(), &deviceConfig); deviceConfigValid {
		if cordonErr != nil {
			logger.Error(cordonErr, fmt.Sprintf("Node: %v State: %v UpgradeFailed with Error: %v", node.Name, h.getNodeStatus(node.Name), cordonErr))
			// Cordoning failed. Mark the state as failed
			h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateCordonFailed)
			return
		}
		// Proceed if the device config is valid and cordoning is successful
	} else {
		// Device config changed when cordoning was going on. Dont proceed
		return
	}

	// Drain the pods that are using amdgpu
	drainErr := h.deleteOrDrainPods(ctx, &deviceConfig, &node)
	if deviceConfigValid := h.isDeviceConfigValid(context.TODO(), &deviceConfig); deviceConfigValid {
		if drainErr != nil {
			logger.Error(drainErr, fmt.Sprintf("Node: %v State: %v UpgradeFailed with Error: %v", node.Name, h.getNodeStatus(node.Name), drainErr))
			// Pod Draining failed. Mark the state as failed
			h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateDrainFailed)
			return
		}
		// Proceed if the device config is valid and cordoning is successful
	} else {
		// Device config changed when draining was going on. Dont proceed
		return
	}

	// Reboot the node if required
	if deviceConfig.Spec.Driver.UpgradePolicy.RebootRequired != nil && *deviceConfig.Spec.Driver.UpgradePolicy.RebootRequired {
		h.handleNodeReboot(ctx, &node, &deviceConfig)
	} else {
		// Update expected module version on the node
		if err := h.updateModuleVersionOnNode(ctx, &deviceConfig, &node); err != nil {
			logger.Error(err, fmt.Sprintf("Node: %v State: %v UpgradeFailed with Error: %v", node.Name, h.getNodeStatus(node.Name), err))
			// Mark the state as failed
			h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateFailed)
			return
		}
		log.FromContext(ctx).Info("Reboot required is false, setting Upgrade state to Upgrade-In-Progress")
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateInProgress)
	}
}

func (h *upgradeMgrHelper) isDeviceConfigValid(ctx context.Context, dc *amdv1alpha1.DeviceConfig) bool {

	devConfig := amdv1alpha1.DeviceConfig{}

	if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name}, &devConfig); err != nil {
		return false
	}

	if dc.Spec.Driver.Version != devConfig.Spec.Driver.Version {
		return false
	}

	return true
}

func (h *upgradeMgrHelper) cordonOrUncordonNode(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node, cordon bool) error {

	logger := log.FromContext(ctx)

	nodeObj := &v1.Node{}
	if err := h.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err != nil {
		return err
	}

	if err := h.addOrRemoveTaintToNode(ctx, deviceConfig, node, cordon); err != nil {
		logger.Error(err, fmt.Sprintf("Failed to Taint(%v) %+v", cordon, node.Name))
		return err
	}

	if cordon {
		logger.Info(fmt.Sprintf("cordoned node %+v", node.Name))
	} else {
		logger.Info(fmt.Sprintf("uncordoned node %+v", node.Name))
	}

	return nil
}

func (h *upgradeMgrHelper) addOrRemoveTaintToNode(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node, taint bool) error {

	logger := log.FromContext(ctx)
	upgradeTaint := v1.Taint{
		Key:    "amd-gpu-driver-upgrade",
		Value:  "true",
		Effect: v1.TaintEffectNoSchedule,
	}

	checkIfTaintsAlreadyExists := func(node *v1.Node, upgradeTaint v1.Taint) bool {
		for _, t := range node.Spec.Taints {
			if upgradeTaint.Key == t.Key && upgradeTaint.Effect == t.Effect {
				return true
			}
		}
		return false
	}

	removeTaint := func(node *v1.Node, upgradeTaint v1.Taint) (ts []v1.Taint) {
		for _, t := range node.Spec.Taints {
			if upgradeTaint.Key == t.Key && upgradeTaint.Effect == t.Effect {
				continue
			}
			ts = append(ts, t)
		}
		return
	}

	if retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		nodeObj := &v1.Node{}
		if err := h.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err != nil {
			return err
		}

		nodeObjCopy := nodeObj.DeepCopy()
		exists := checkIfTaintsAlreadyExists(node, upgradeTaint)
		logger.Info(fmt.Sprintf("node: %v taint: %v exist:%v", node.Name, taint, exists))
		if taint {
			if exists {
				return nil
			}
			nodeObj.Spec.Taints = append(node.Spec.Taints, upgradeTaint)
		} else {
			if !exists {
				return nil
			}
			nodeObj.Spec.Taints = removeTaint(node, upgradeTaint)
		}

		return h.client.Patch(ctx, nodeObj, client.MergeFrom(nodeObjCopy))

	}); retryErr != nil {

		logger.Error(retryErr, fmt.Sprintf("failed to change taints on node %+v", node.Name))
		return retryErr

	}
	return nil
}

func (h *upgradeMgrHelper) updateModuleVersionOnNode(ctx context.Context, deviceConfig *amdv1alpha1.DeviceConfig, node *v1.Node) error {

	logger := log.FromContext(ctx)

	if retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		nodeObj := &v1.Node{}
		if err := h.client.Get(ctx, client.ObjectKey{Name: node.Name}, nodeObj); err != nil {
			return err
		}
		nodeObjCopy := nodeObj.DeepCopy()
		driverVersion, err := utils.GetDriverVersion(*node, *deviceConfig)
		if err == nil {
			nodeObj.Labels[fmt.Sprintf("kmm.node.kubernetes.io/version-module.%s.%s", deviceConfig.Namespace, deviceConfig.Name)] = driverVersion
		}
		return h.client.Patch(ctx, nodeObj, client.MergeFrom(nodeObjCopy))

	}); retryErr != nil {

		logger.Error(retryErr, fmt.Sprintf("failed to update module version on node %+v", node.Name))
		return retryErr

	}
	return nil
}

func (h *upgradeMgrHelper) handleNodeReboot(ctx context.Context, node *v1.Node, dc *amdv1alpha1.DeviceConfig) {
	logger := log.FromContext(ctx)
	rebootPod := h.getRebootPod(node.Name, dc)
	// Delete the existing pod if present
	pod := &v1.Pod{}
	if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: rebootPod.Name}, pod); err == nil {
		if err := h.client.Delete(ctx, pod); err != nil {
			logger.Error(err, fmt.Sprintf("Node: %v State: %v RebootPod Delete failed with Error: %v", node.Name, h.getNodeStatus(node.Name), err))
			h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateRebootFailed)
			return
		}
	}

	// Update expected module version on the node
	if err := h.updateModuleVersionOnNode(ctx, dc, node); err != nil {
		logger.Error(err, fmt.Sprintf("Node: %v State: %v UpgradeFailed with Error: %v", node.Name, h.getNodeStatus(node.Name), err))
		// Mark the state as failed
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateFailed)
		return
	}

	waitForDriverUpgrade := func() {
		for i := uint(0); i < 360; _, i = <-time.NewTicker(10*time.Second).C, i+1 {
			nmcObj := &kmmv1beta1.NodeModulesConfig{}
			if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: node.Name}, nmcObj); err == nil {
				for _, status := range nmcObj.Status.Modules {
					if strings.HasSuffix(status.Config.ContainerImage, dc.Spec.Driver.Version) {
						return
					}
				}
			}
		}
	}

	// Wait for the driver upgrade to complete
	waitForDriverUpgrade()

	if err := h.client.Create(ctx, rebootPod); err != nil {
		logger.Error(err, fmt.Sprintf("Node: %v State: %v RebootPod Create failed with Error: %v", node.Name, h.getNodeStatus(node.Name), err))
		// Mark the state as failed
		h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateRebootFailed)
		return
	}

	waitForRebootPod := func() {
		for i := uint(0); i < 300; _, i = <-time.NewTicker(2*time.Second).C, i+1 {
			if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: rebootPod.Name}, pod); err == nil {
				// Check if the node has moved to NotReady state
				nodeObj := &v1.Node{}
				if err := h.client.Get(ctx, types.NamespacedName{Name: node.Name}, nodeObj); err == nil {
					nodeNotReady := false
					for _, condition := range nodeObj.Status.Conditions {
						if condition.Type == v1.NodeReady && condition.Status != v1.ConditionTrue {
							nodeNotReady = true
							break
						}
					}

					// If node is NotReady, proceed; otherwise, wait for the next tick
					if nodeNotReady {
						logger.Info(fmt.Sprintf("Node: %v has moved to NotReady", node.Name))
						return
					} else {
						logger.Info(fmt.Sprintf("Node: %v is still in Ready state. Waiting for NotReady.", node.Name))
						continue
					}
				}
			}
		}
	}

	// Wait for the rebootPod to get spawned
	waitForRebootPod()

	h.setNodeStatus(ctx, node.Name, amdv1alpha1.UpgradeStateRebootInProgress)
	h.deleteRebootPod(ctx, node.Name, dc, false, dc.Generation)

}

func (h *upgradeMgrHelper) deleteRebootPod(ctx context.Context, nodeName string, dc *amdv1alpha1.DeviceConfig, force bool, genId int64) {

	logger := log.FromContext(ctx)
	rebootPod := h.getRebootPod(nodeName, dc)
	fetchedDeviceConfig := &amdv1alpha1.DeviceConfig{}
	pod := &v1.Pod{}
	if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: rebootPod.Name}, pod); err != nil {
		return
	}

	if !force {
		// Wait (max 1 hour) until reboot is done
		for i := uint(0); i < 360; _, i = <-time.NewTicker(10*time.Second).C, i+1 {
			if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name}, fetchedDeviceConfig); err != nil {
				logger.Error(err, "Failed to fetch DeviceConfig from API server")
				return
			}
			// Get the current node status
			node := &v1.Node{}
			if err := h.client.Get(ctx, types.NamespacedName{Name: nodeName}, node); err == nil {
				// Check if the node has come back to ready state
				nodeReady := false

				for _, condition := range node.Status.Conditions {
					if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
						nodeReady = true
						break
					}
				}
				// If the node is ready, delete the reboot pod
				if nodeReady {
					logger.Info(fmt.Sprintf("Node: %v is Ready. Attempting to delete reboot pod", nodeName))
					if err := h.client.Delete(ctx, rebootPod); err != nil {
						logger.Error(err, fmt.Sprintf("Node: %v State: %v RebootPod Delete failed with Error: %v", nodeName, h.getNodeStatus(nodeName), err))
					}
					if fetchedDeviceConfig.Generation == genId {
						logger.Info("Setting to In-Progress after deleting reboot pod")
						h.setNodeStatus(ctx, nodeName, amdv1alpha1.UpgradeStateInProgress)
					}
					return
				} else {
					logger.Info(fmt.Sprintf("Node: %v State: %v Node is not ready yet, continuing to wait", nodeName, h.getNodeStatus(nodeName)))
				}
			} else {
				logger.Info(fmt.Sprintf("Node: %v State: %v Failed to get node status", nodeName, h.getNodeStatus(nodeName)))
			}

			logger.Info(fmt.Sprintf("Node: %v State: %v Waiting for node to become Ready", nodeName, h.getNodeStatus(nodeName)))
		}
	}

	if err := h.client.Delete(ctx, rebootPod); err != nil {
		logger.Error(err, fmt.Sprintf("Node: %v State: %v RebootPod Delete failed with Error: %v", nodeName, h.getNodeStatus(nodeName), err))
	}
	if err := h.client.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name}, fetchedDeviceConfig); err != nil {
		logger.Error(err, "Failed to fetch DeviceConfig from API server")
		return
	}
	if fetchedDeviceConfig.Generation == genId {
		logger.Info("Setting to In-Progress after deleting reboot pod eventually")
		h.setNodeStatus(ctx, nodeName, amdv1alpha1.UpgradeStateInProgress)
	}
}

func (h *upgradeMgrHelper) getRebootPod(nodeName string, dc *amdv1alpha1.DeviceConfig) *v1.Pod {
	nodeSelector := map[string]string{}
	nodeSelector["kubernetes.io/hostname"] = nodeName
	utilsImage := defaultUtilsImage
	serviceaccount := defaultSAName
	if dc.Spec.CommonConfig.UtilsContainer.Image != "" {
		utilsImage = dc.Spec.CommonConfig.UtilsContainer.Image
	}
	imagePullSecrets := []v1.LocalObjectReference{}
	if dc.Spec.CommonConfig.UtilsContainer.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *dc.Spec.CommonConfig.UtilsContainer.ImageRegistrySecret)
	}
	rebootPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("amd-gpu-operator-%v-reboot-worker", nodeName),
			Namespace: dc.Namespace,
		},
		Spec: v1.PodSpec{
			ServiceAccountName: serviceaccount,
			HostPID:            true,
			HostNetwork:        true,
			RestartPolicy:      v1.RestartPolicyNever,
			NodeSelector:       nodeSelector,
			ImagePullSecrets:   imagePullSecrets,
			Containers: []v1.Container{
				{
					Name:            "reboot-container",
					Image:           utilsImage,
					Command:         []string{"/nsenter", "--all", "--target=1", "--", "sudo", "reboot"},
					Stdin:           true,
					TTY:             true,
					SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
				},
			},
			Tolerations: []v1.Toleration{
				{
					Key:      "amd-gpu-driver-upgrade",
					Value:    "true",
					Operator: v1.TolerationOpEqual,
					Effect:   v1.TaintEffectNoSchedule,
				},
			},
		},
	}

	if dc.Spec.CommonConfig.UtilsContainer.ImagePullPolicy != "" {
		rebootPod.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(dc.Spec.CommonConfig.UtilsContainer.ImagePullPolicy)
	}

	return rebootPod
}
