/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/conditions"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isDisabledPF(devConfig *amdv1alpha1.DeviceConfig) bool {
	return devConfig.Spec.Driver.DriverType == utils.DriverTypePFPassthrough &&
		devConfig.Spec.Driver.Enable != nil && !*devConfig.Spec.Driver.Enable
}

func isVFIOConfig(devConfig *amdv1alpha1.DeviceConfig) bool {
	return devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough ||
		devConfig.Spec.Driver.DriverType == utils.DriverTypePFPassthrough
}

func (dcrh *deviceConfigReconcilerHelper) ensureVFIOWorkers(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	if !isVFIOConfig(devConfig) {
		return nil
	}
	if nodes == nil {
		nodes = &v1.NodeList{}
	}
	enabled := devConfig.Spec.Driver.Enable == nil || *devConfig.Spec.Driver.Enable
	deleting := !devConfig.DeletionTimestamp.IsZero()
	selected := make(map[string]struct{}, len(nodes.Items))
	for _, node := range nodes.Items {
		selected[node.Name] = struct{}{}
	}
	reservationNames := nodeReservationNames(devConfig)

	cleanupNodes, err := dcrh.vfioCleanupNodes(ctx, devConfig, nodes)
	if err != nil {
		return err
	}
	cleanupSet := make(map[string]struct{}, len(cleanupNodes.Items))
	for _, node := range cleanupNodes.Items {
		cleanupSet[node.Name] = struct{}{}
		if err := dcrh.checkNodeReservationOwner(ctx, devConfig, node.Name); err != nil {
			return err
		}
		if err := dcrh.ensureVFIOCleanup(ctx, devConfig, &v1.NodeList{Items: []v1.Node{node}}); err != nil {
			return err
		}
	}
	if err := dcrh.releaseUnneededStaleReservations(ctx, devConfig, selected, cleanupSet); err != nil {
		return err
	}

	if !enabled || deleting {
		if !deleting {
			complete, err := dcrh.vfioCleanupComplete(ctx, devConfig, cleanupNodes)
			if err != nil || !complete {
				return err
			}
			knownNodes := make(map[string]struct{}, len(nodes.Items)+len(cleanupNodes.Items)+len(reservationNames))
			for _, node := range nodes.Items {
				knownNodes[node.Name] = struct{}{}
			}
			for _, node := range cleanupNodes.Items {
				knownNodes[node.Name] = struct{}{}
			}
			for _, node := range reservationNames {
				knownNodes[node] = struct{}{}
			}
			knownNodeNames := make([]string, 0, len(knownNodes))
			for node := range knownNodes {
				knownNodeNames = append(knownNodeNames, node)
			}
			sort.Strings(knownNodeNames)
			for _, node := range knownNodeNames {
				if err := dcrh.setNodeVFIOReady(ctx, devConfig, node, false, true); err != nil {
					return err
				}
			}
			return nil
		}
		return nil
	}

	kmmManaged := devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough && dcrh.shouldUseKMMOperatorLevel(devConfig)
	kmmReady := true
	if kmmManaged {
		module, err := dcrh.getDeviceConfigOwnedKMMModule(ctx, devConfig)
		if err != nil {
			return err
		}
		if module == nil || module.Status.ModuleLoader.AvailableNumber != int32(len(nodes.Items)) {
			kmmReady = false
		}
	}
	vfioLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	for _, node := range nodes.Items {
		if _, ok := cleanupSet[node.Name]; ok {
			continue
		}
		if reservation, ok := dcrh.nodeReservation(devConfig, node.Name); ok && reservation.VFIOCleanupPending {
			continue
		}
		bootID := node.Status.NodeInfo.BootID
		reservation, _ := dcrh.nodeReservation(devConfig, node.Name)
		if _, ready := node.Labels[vfioLabel]; ready && (bootID == "" || reservation.VFIOBindingBootID != bootID) {
			if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, false, false); err != nil {
				return err
			}
			delete(node.Labels, vfioLabel)
		}
		if bootID == "" {
			continue
		}
		if kmmManaged {
			if !kmmReady {
				continue
			}
			if _, ok := node.Labels[fmt.Sprintf(utils.KMMModuleReadyLabelTemplate, devConfig.Namespace, devConfig.Name)]; !ok {
				continue
			}
		}
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if err == nil {
			if pod.Labels[utils.WorkerActionLabelKey] == utils.LoadVFIOAction && pod.Status.Phase == v1.PodSucceeded {
				if pod.Annotations[workermgr.VFIOWorkerBootIDAnnotation] == "" || pod.Annotations[workermgr.VFIOWorkerBootIDAnnotation] != bootID {
					if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, false, false); err != nil {
						return err
					}
					if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
						return err
					}
					continue
				}
				if reservation.VFIOBindingBootID != bootID {
					if err := dcrh.setVFIOBindingBootID(ctx, devConfig, node.Name, bootID); err != nil {
						return err
					}
				}
				if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, true, false); err != nil {
					return err
				}
				if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
					return err
				}
			} else if isInterruptedVFIOLoad(pod) &&
				pod.Annotations[workermgr.VFIOWorkerBootIDAnnotation] != "" &&
				pod.Annotations[workermgr.VFIOWorkerBootIDAnnotation] != bootID && nodeIsReady(&node) {
				if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, false, false); err != nil {
					return err
				}
				if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
					return err
				}
			} else if kmmManaged && vfDetectorTimedOutBeforeGate(pod) {
				if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
					return err
				}
			}
			continue
		}
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to fetch VFIO worker pod on node %s: %w", node.Name, err)
		}
		if _, ok := node.Labels[vfioLabel]; ok {
			continue
		}
		if err := dcrh.setVFIOBindingBootID(ctx, devConfig, node.Name, bootID); err != nil {
			return err
		}
		if err := dcrh.kmmPostProcessor.Work(ctx, devConfig, &node); err != nil {
			return fmt.Errorf("failed to create VFIO worker pod on node %s: %w", node.Name, err)
		}
	}
	return nil
}

func isInterruptedVFIOLoad(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodUnknown {
		return false
	}
	if pod.Status.Reason == "Terminated" || pod.Status.Reason == "NodeLost" {
		return true
	}
	for _, statuses := range [][]v1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, status := range statuses {
			if status.State.Waiting != nil && status.State.Waiting.Reason == "ContainerStatusUnknown" {
				return true
			}
			if status.State.Terminated != nil && (status.State.Terminated.Reason == "Terminated" || status.State.Terminated.Reason == "ContainerStatusUnknown") {
				return true
			}
			if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "ContainerStatusUnknown" {
				return true
			}
		}
	}
	return false
}

func nodeIsReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func (dcrh *deviceConfigReconcilerHelper) nodeReservation(devConfig *amdv1alpha1.DeviceConfig, node string) (amdv1alpha1.NodeReservationStatus, bool) {
	if devConfig.Status.NodeReservationsInitialized {
		status, ok := devConfig.Status.NodeReservations[node]
		return status, ok
	}
	if _, ok := devConfig.Status.NodeModuleStatus[node]; ok {
		return amdv1alpha1.NodeReservationStatus{}, true
	}
	return amdv1alpha1.NodeReservationStatus{}, false
}

func (dcrh *deviceConfigReconcilerHelper) setNodeReservation(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string, status *amdv1alpha1.NodeReservationStatus) error {
	reservations := make(map[string]amdv1alpha1.NodeReservationStatus)
	if devConfig.Status.NodeReservationsInitialized {
		for name, existing := range devConfig.Status.NodeReservations {
			reservations[name] = existing
		}
	} else {
		for name := range devConfig.Status.NodeModuleStatus {
			reservations[name] = amdv1alpha1.NodeReservationStatus{
				KMMOwnership: dcrh.kmmOwnershipForLegacyReservation(devConfig, name),
			}
		}
	}
	if status == nil {
		delete(reservations, node)
	} else {
		reservations[node] = *status
	}
	if devConfig.Status.NodeReservationsInitialized && maps.Equal(devConfig.Status.NodeReservations, reservations) {
		return nil
	}
	devConfig.Status.NodeReservations = reservations
	devConfig.Status.NodeReservationsInitialized = true
	if err := dcrh.updateDeviceConfigStatus(ctx, devConfig); err != nil {
		return fmt.Errorf("persisting VFIO reservation for node %s: %w", node, err)
	}
	dcrh.updateNodeAssignments(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name}.String(), nodeListForReservations(reservations), false)
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) markVFIOCleanupPending(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string) error {
	status, _ := dcrh.nodeReservation(devConfig, node)
	if status.VFIOCleanupPending {
		return nil
	}
	status.VFIOCleanupPending = true
	return dcrh.setNodeReservation(ctx, devConfig, node, &status)
}

func (dcrh *deviceConfigReconcilerHelper) clearVFIOCleanupPending(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string) error {
	status, ok := dcrh.nodeReservation(devConfig, node)
	if !ok || !status.VFIOCleanupPending {
		return nil
	}
	status.VFIOCleanupPending = false
	return dcrh.setNodeReservation(ctx, devConfig, node, &status)
}

func (dcrh *deviceConfigReconcilerHelper) setVFIOBindingBootID(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node, bootID string) error {
	status, _ := dcrh.nodeReservation(devConfig, node)
	if status.VFIOBindingBootID == bootID {
		return nil
	}
	status.VFIOBindingBootID = bootID
	return dcrh.setNodeReservation(ctx, devConfig, node, &status)
}

func (dcrh *deviceConfigReconcilerHelper) setNodeVFIOReady(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodeName string, ready, cleanupDriverLabel bool) error {
	node := &v1.Node{}
	if err := dcrh.client.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	before := node.DeepCopy()
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	changed := false
	if ready {
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		if _, ok := node.Labels[readyLabel]; !ok {
			node.Labels[readyLabel] = ""
			changed = true
		}
	} else if _, ok := node.Labels[readyLabel]; ok {
		delete(node.Labels, readyLabel)
		changed = true
	}
	if cleanupDriverLabel {
		if _, ok := node.Labels[utils.GetDriverTypeNodeLabel(devConfig)]; ok {
			delete(node.Labels, utils.GetDriverTypeNodeLabel(devConfig))
			changed = true
		}
	}
	if changed {
		if err := dcrh.client.Patch(ctx, node, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("updating VFIO labels on node %s: %w", nodeName, err)
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) checkNodeReservationOwner(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string) error {
	owners := dcrh.nodeAssignments[node]
	if len(owners) > 1 {
		return dcrh.blockVFIOCleanup(ctx, devConfig, node, fmt.Errorf("cannot unbind node %s with conflicting DeviceConfig reservations", node))
	}
	name := types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name}.String()
	for owner := range owners {
		if owner != name {
			return dcrh.blockVFIOCleanup(ctx, devConfig, node, fmt.Errorf("cannot unbind node %s assigned to DeviceConfig %s", node, owner))
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) blockVFIOCleanup(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string, conflict error) error {
	markerErr := dcrh.markVFIOCleanupPending(ctx, devConfig, node)
	var statusErr error
	if dcrh.conditionUpdater != nil {
		dcrh.conditionUpdater.SetReadyCondition(devConfig, metav1.ConditionFalse, conditions.VFIOCleanupPending, conflict.Error())
		statusErr = dcrh.updateDeviceConfigStatus(ctx, devConfig)
	}
	return errors.Join(conflict, markerErr, statusErr)
}

func (dcrh *deviceConfigReconcilerHelper) releaseUnneededStaleReservations(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, selected, cleanup map[string]struct{}) error {
	if !devConfig.Status.NodeReservationsInitialized {
		return nil
	}
	for node := range devConfig.Status.NodeReservations {
		if _, ok := selected[node]; ok {
			continue
		}
		if _, ok := cleanup[node]; ok {
			if reservation, exists := dcrh.nodeReservation(devConfig, node); exists && reservation.VFIOCleanupPending {
				continue
			}
			_, err := dcrh.kmmPostProcessor.GetWorkerPod(ctx, devConfig, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: node}})
			if err == nil || !k8serrors.IsNotFound(err) {
				continue
			}
		}
		if reservation, exists := dcrh.nodeReservation(devConfig, node); exists &&
			reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged {
			continue
		}
		if err := dcrh.setNodeReservation(ctx, devConfig, node, nil); err != nil {
			return err
		}
		delete(devConfig.Status.NodeModuleStatus, node)
	}
	return nil
}

func vfDetectorTimedOutBeforeGate(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodFailed || pod.Labels[utils.WorkerActionLabelKey] != utils.LoadVFIOAction ||
		pod.Annotations[workermgr.VFDetectorRetryMarker] != "" {
		return false
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Terminated != nil && status.State.Terminated.ExitCode == 124 {
			return true
		}
	}
	return false
}

func (dcrh *deviceConfigReconcilerHelper) hasVFIOCleanup(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, selected *v1.NodeList) (bool, error) {
	nodes, err := dcrh.vfioCleanupNodes(ctx, devConfig, selected)
	if err != nil {
		return false, err
	}
	return len(nodes.Items) > 0, nil
}

func (dcrh *deviceConfigReconcilerHelper) vfioCleanupNodes(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, selected *v1.NodeList) (*v1.NodeList, error) {
	enabled := devConfig.Spec.Driver.Enable == nil || *devConfig.Spec.Driver.Enable
	deleting := !devConfig.DeletionTimestamp.IsZero()
	selectedNodes := make(map[string]struct{})
	if selected != nil {
		for _, node := range selected.Items {
			selectedNodes[node.Name] = struct{}{}
		}
	}
	names := make(map[string]struct{})
	if devConfig.Status.NodeReservationsInitialized {
		for node, reservation := range devConfig.Status.NodeReservations {
			_, selected := selectedNodes[node]
			if reservation.VFIOCleanupPending || reservation.VFIOBindingBootID != "" && (!enabled || deleting || !selected) {
				names[node] = struct{}{}
			}
		}
	}
	pods := &v1.PodList{}
	if err := dcrh.client.List(ctx, pods, client.InNamespace(devConfig.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list VFIO workers: %w", err)
	}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner != nil && owner.UID == devConfig.UID && owner.Kind == utils.KindDeviceConfig &&
			pod.Name == fmt.Sprintf("worker-%s-%s", devConfig.Name, pod.Spec.NodeName) {
			if pod.Labels[utils.WorkerActionLabelKey] == utils.UnloadVFIOAction || !enabled || deleting {
				names[pod.Spec.NodeName] = struct{}{}
			} else if _, selected := selectedNodes[pod.Spec.NodeName]; !selected {
				names[pod.Spec.NodeName] = struct{}{}
			}
		}
	}
	allNodes := &v1.NodeList{}
	if err := dcrh.client.List(ctx, allNodes); err != nil {
		return nil, fmt.Errorf("failed to list nodes for VFIO cleanup: %w", err)
	}
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	driverLabel := utils.GetDriverTypeNodeLabel(devConfig)
	allNodesByName := make(map[string]v1.Node, len(allNodes.Items))
	for _, node := range allNodes.Items {
		allNodesByName[node.Name] = node
		_, selected := selectedNodes[node.Name]
		if utils.HasNodeLabelKey(node, readyLabel) &&
			(!enabled || deleting || !selected || node.Labels[driverLabel] != string(devConfig.Spec.Driver.DriverType)) {
			names[node.Name] = struct{}{}
		}
	}
	result := &v1.NodeList{}
	for name := range names {
		if node, ok := allNodesByName[name]; ok {
			result.Items = append(result.Items, node)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result, nil
}

func (dcrh *deviceConfigReconcilerHelper) getOwnedWorkerPod(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node *v1.Node) (*v1.Pod, error) {
	pod, err := dcrh.kmmPostProcessor.GetWorkerPod(ctx, devConfig, node)
	if err != nil {
		return nil, err
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.UID != devConfig.UID || owner.Kind != utils.KindDeviceConfig {
		return nil, fmt.Errorf("worker pod %s is not owned by DeviceConfig %s/%s", pod.Name, devConfig.Namespace, devConfig.Name)
	}
	return pod, nil
}

func (dcrh *deviceConfigReconcilerHelper) deleteWorkerPod(ctx context.Context, pod *v1.Pod) error {
	if pod.DeletionTimestamp != nil {
		return nil
	}
	if err := dcrh.client.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: &workermgr.WorkerPodGracePeriod}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete worker pod %s: %w", pod.Name, err)
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) ensureVFIOCleanup(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	for _, node := range nodes.Items {
		if err := dcrh.markVFIOCleanupPending(ctx, devConfig, node.Name); err != nil {
			return err
		}
		if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, false, false); err != nil {
			return err
		}
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if k8serrors.IsNotFound(err) {
			if err := dcrh.kmmPostProcessor.Cleanup(ctx, devConfig, &node); err != nil {
				return fmt.Errorf("failed to create VFIO cleanup worker on node %s: %w", node.Name, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if pod.Labels[utils.WorkerActionLabelKey] == utils.LoadVFIOAction {
			if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
				return err
			}
			continue
		}
		if pod.Labels[utils.WorkerActionLabelKey] != utils.UnloadVFIOAction {
			return fmt.Errorf("worker pod %s has unknown action", pod.Name)
		}
		if pod.Status.Phase == v1.PodFailed || pod.Status.Phase == v1.PodUnknown {
			if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
				return err
			}
			continue
		}
		if pod.Status.Phase != v1.PodSucceeded {
			continue
		}
		if err := dcrh.setNodeVFIOReady(ctx, devConfig, node.Name, false, true); err != nil {
			return err
		}
		if err := dcrh.clearVFIOCleanupPending(ctx, devConfig, node.Name); err != nil {
			return err
		}
		if err := dcrh.setVFIOBindingBootID(ctx, devConfig, node.Name, ""); err != nil {
			return err
		}
		if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
			return err
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) vfioCleanupComplete(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (bool, error) {
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	for _, node := range nodes.Items {
		if reservation, ok := dcrh.nodeReservation(devConfig, node.Name); ok && reservation.VFIOCleanupPending {
			return false, nil
		}
		if reservation, ok := dcrh.nodeReservation(devConfig, node.Name); ok && reservation.VFIOBindingBootID != "" {
			return false, nil
		}
		current := &v1.Node{}
		if err := dcrh.client.Get(ctx, types.NamespacedName{Name: node.Name}, current); err == nil {
			if _, ok := current.Labels[readyLabel]; ok {
				return false, nil
			}
		} else if !k8serrors.IsNotFound(err) {
			return false, err
		}
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if pod.Labels[utils.WorkerActionLabelKey] != utils.UnloadVFIOAction || pod.Status.Phase != v1.PodSucceeded {
			return false, nil
		}
	}
	return true, nil
}

func (dcrh *deviceConfigReconcilerHelper) deleteVFIOCleanupMarkers(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	for _, node := range nodes.Items {
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
			return err
		}
	}
	return nil
}
