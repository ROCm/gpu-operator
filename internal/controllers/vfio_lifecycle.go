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
	"fmt"
	"sort"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
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

func (dcrh *deviceConfigReconcilerHelper) ensureVFIOWorkers(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	if isDisabledPF(devConfig) {
		cleanupNodes, err := dcrh.pfCleanupNodes(ctx, devConfig, nodes)
		if err != nil {
			return err
		}
		if err := dcrh.ensurePFCleanup(ctx, devConfig, cleanupNodes); err != nil {
			return err
		}
		complete, err := dcrh.pfCleanupComplete(ctx, devConfig, cleanupNodes)
		if err != nil || !complete {
			return err
		}
		return utils.UpdateDriverTypeNodeLabel(ctx, dcrh.client, devConfig, cleanupNodes, true)
	}
	if devConfig.Spec.Driver.Enable != nil && !*devConfig.Spec.Driver.Enable {
		return nil
	}
	if devConfig.Spec.Driver.DriverType != utils.DriverTypeVFPassthrough && devConfig.Spec.Driver.DriverType != utils.DriverTypePFPassthrough {
		return nil
	}
	kmmManaged := devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough && dcrh.shouldUseKMMOperatorLevel(devConfig)
	if kmmManaged {
		module, err := dcrh.getDeviceConfigOwnedKMMModule(ctx, devConfig)
		if err != nil {
			return err
		}
		if module == nil || module.Status.ModuleLoader.AvailableNumber != int32(len(nodes.Items)) {
			return nil
		}
	}
	vfioLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	for _, node := range nodes.Items {
		if kmmManaged {
			if _, ok := node.Labels[fmt.Sprintf(utils.KMMModuleReadyLabelTemplate, devConfig.Namespace, devConfig.Name)]; !ok {
				continue
			}
		}
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if err == nil {
			if devConfig.Spec.Driver.DriverType == utils.DriverTypePFPassthrough && pod.Labels[utils.WorkerActionLabelKey] == utils.UnloadVFIOAction {
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
		if err := dcrh.kmmPostProcessor.Work(ctx, devConfig, &node); err != nil {
			return fmt.Errorf("failed to create VFIO worker pod on node %s: %w", node.Name, err)
		}
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

func (dcrh *deviceConfigReconcilerHelper) pfCleanupNodes(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, selected *v1.NodeList) (*v1.NodeList, error) {
	// shortcut: a former node with no worker or operator label cannot be found; persist assignments if those signals can disappear before cleanup.
	names := make(map[string]bool, len(selected.Items))
	if len(devConfig.Spec.Selector) > 0 {
		for _, node := range selected.Items {
			names[node.Name] = true
		}
	}
	pods := &v1.PodList{}
	if err := dcrh.client.List(ctx, pods, client.InNamespace(devConfig.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list PF workers: %w", err)
	}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner != nil && owner.UID == devConfig.UID && owner.Kind == utils.KindDeviceConfig &&
			pod.Name == fmt.Sprintf("worker-%s-%s", devConfig.Name, pod.Spec.NodeName) {
			names[pod.Spec.NodeName] = true
		}
	}
	allNodes := &v1.NodeList{}
	if err := dcrh.client.List(ctx, allNodes); err != nil {
		return nil, fmt.Errorf("failed to list nodes for PF cleanup: %w", err)
	}
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	driverLabel := utils.GetDriverTypeNodeLabel(devConfig)
	result := &v1.NodeList{}
	for _, node := range allNodes.Items {
		if names[node.Name] || utils.HasNodeLabelKey(node, readyLabel) || node.Labels[driverLabel] == utils.DriverTypePFPassthrough {
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

func (dcrh *deviceConfigReconcilerHelper) ensurePFCleanup(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	for _, node := range nodes.Items {
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if k8serrors.IsNotFound(err) {
			if err := dcrh.kmmPostProcessor.Cleanup(ctx, devConfig, &node); err != nil {
				return fmt.Errorf("failed to create PF cleanup worker on node %s: %w", node.Name, err)
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
		if pod.Status.Phase != v1.PodSucceeded {
			continue
		}
		if _, ok := node.Labels[readyLabel]; ok {
			node = *node.DeepCopy()
			before := node.DeepCopy()
			delete(node.Labels, readyLabel)
			if err := dcrh.client.Patch(ctx, &node, client.MergeFrom(before)); err != nil {
				return fmt.Errorf("failed to remove VFIO-ready label from node %s: %w", node.Name, err)
			}
		}
	}
	return nil
}

func (dcrh *deviceConfigReconcilerHelper) pfCleanupComplete(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) (bool, error) {
	readyLabel := dcrh.kmmPostProcessor.GetWorkReadyLabel(types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name})
	for _, node := range nodes.Items {
		if _, ok := node.Labels[readyLabel]; ok {
			return false, nil
		}
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if k8serrors.IsNotFound(err) {
			return false, nil
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

func (dcrh *deviceConfigReconcilerHelper) deletePFCleanupMarkers(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	for _, node := range nodes.Items {
		pod, err := dcrh.getOwnedWorkerPod(ctx, devConfig, &node)
		if err != nil {
			return err
		}
		if err := dcrh.deleteWorkerPod(ctx, pod); err != nil {
			return err
		}
	}
	return nil
}
