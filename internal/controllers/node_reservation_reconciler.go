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
	"maps"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nodeReservationRequeueAfter = 20 * time.Second
	kmmModuleNameLabel          = "kmm.node.kubernetes.io/module.name"
	kmmWorkerComponentLabel     = "app.kubernetes.io/component"
	kmmWorkerComponent          = "worker"
)

type kmmNodeReservationReconciler interface {
	reconcileKMMNodeReservations(context.Context, *amdv1alpha1.DeviceConfig, *v1.NodeList) (bool, bool, error)
}

func (dcrh *deviceConfigReconcilerHelper) reconcileKMMNodeReservations(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, selected *v1.NodeList) (bool, bool, error) {
	if isVFIOConfig(devConfig) && devConfig.Spec.Driver.DriverType != utils.DriverTypeVFPassthrough {
		return false, false, nil
	}
	if selected == nil {
		selected = &v1.NodeList{}
	}
	selectedNames := make(map[string]struct{}, len(selected.Items))
	for _, node := range selected.Items {
		selectedNames[node.Name] = struct{}{}
	}
	needsRelease := hasUnselectedReservation(devConfig, selectedNames)
	hasRetirementTombstone := hasKMMRetirementPending(devConfig)
	if len(selected.Items) > 0 && !needsRelease && !hasRetirementTombstone && dcrh.shouldUseKMMOperatorLevel(devConfig) {
		return false, false, nil
	}

	module := &kmmv1beta1.Module{}
	moduleErr := dcrh.client.Get(ctx, types.NamespacedName{Namespace: devConfig.Namespace, Name: devConfig.Name}, module)
	moduleFound := moduleErr == nil
	moduleOwned := false
	moduleUnavailable := meta.IsNoMatchError(moduleErr)
	if moduleErr != nil && !k8serrors.IsNotFound(moduleErr) {
		if !moduleUnavailable {
			return false, false, fmt.Errorf("getting KMM Module for reservation release: %w", moduleErr)
		}
	}

	if moduleFound {
		owner := metav1.GetControllerOf(module)
		if owner != nil && owner.UID == devConfig.UID && owner.Kind == utils.KindDeviceConfig {
			moduleOwned = true
			if dcrh.shouldUseKMMOperatorLevel(devConfig) {
				selector := deviceConfigKMMSelector(devConfig)
				if !maps.Equal(module.Spec.Selector, selector) {
					before := module.DeepCopy()
					module.Spec.Selector = selector
					if err := dcrh.client.Patch(ctx, module, client.MergeFrom(before)); err != nil {
						return true, true, fmt.Errorf("updating KMM Module selector for DeviceConfig %s/%s: %w", devConfig.Namespace, devConfig.Name, err)
					}
				}
			}
		}
		moduleSelector := labels.Set(module.Spec.Selector).AsSelector()
		for _, node := range selected.Items {
			if moduleSelector.Matches(labels.Set(node.Labels)) {
				markOwnership := dcrh.markKMMReservationUnknown
				if moduleOwned {
					markOwnership = dcrh.markKMMReservationManaged
				}
				if err := markOwnership(ctx, devConfig, node.Name); err != nil {
					return true, true, err
				}
			}
		}
	}
	if !needsRelease && !hasRetirementTombstone {
		return false, false, nil
	}

	pods := &v1.PodList{}
	if err := dcrh.client.List(ctx, pods, client.InNamespace(devConfig.Namespace), client.MatchingLabels{
		kmmModuleNameLabel:      devConfig.Name,
		kmmWorkerComponentLabel: kmmWorkerComponent,
	}); err != nil {
		return needsRelease, true, fmt.Errorf("listing KMM worker pods for reservation release: %w", err)
	}

	pending := false
	requeue := false
	processedTombstones := make(map[string]struct{})
	for nodeName, reservation := range devConfig.Status.NodeReservations {
		if !reservation.KMMRetirementPending {
			continue
		}
		node := &v1.Node{}
		nodeErr := dcrh.client.Get(ctx, types.NamespacedName{Name: nodeName}, node)
		if nodeErr != nil && !k8serrors.IsNotFound(nodeErr) {
			return true, true, fmt.Errorf("getting node %s while reconciling retirement tombstone: %w", nodeName, nodeErr)
		}
		_, selectedNode := selectedNames[nodeName]
		retirementPending, retirementRequeue, handled, err := dcrh.reconcileVFIONodeRetirement(
			ctx, devConfig, nodeName, selectedNode, node, nodeErr, reservation, moduleFound, moduleOwned, moduleUnavailable,
			module, pods,
		)
		if err != nil {
			return true, true, err
		}
		if handled {
			processedTombstones[nodeName] = struct{}{}
			pending = pending || retirementPending
			requeue = requeue || retirementRequeue
		}
	}
	for nodeName := range devConfig.Status.NodeReservations {
		if _, ok := processedTombstones[nodeName]; ok {
			continue
		}
		reservation, _ := dcrh.nodeReservation(devConfig, nodeName)
		_, selectedNode := selectedNames[nodeName]
		if selectedNode {
			continue
		}

		node := &v1.Node{}
		nodeErr := dcrh.client.Get(ctx, types.NamespacedName{Name: nodeName}, node)
		if nodeErr != nil && !k8serrors.IsNotFound(nodeErr) {
			return true, true, fmt.Errorf("getting node %s while releasing KMM reservation: %w", nodeName, nodeErr)
		}
		if k8serrors.IsNotFound(nodeErr) && devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough {
			retirementPending, retirementRequeue, handled, err := dcrh.reconcileVFIONodeRetirement(
				ctx, devConfig, nodeName, false, node, nodeErr, reservation, moduleFound, moduleOwned, moduleUnavailable,
				module, pods,
			)
			if err != nil {
				return true, true, err
			}
			if handled {
				pending = pending || retirementPending
				requeue = requeue || retirementRequeue
				continue
			}
		}
		if moduleFound && nodeErr == nil && labels.Set(module.Spec.Selector).AsSelector().Matches(labels.Set(node.Labels)) {
			markOwnership := dcrh.markKMMReservationUnknown
			if moduleOwned {
				markOwnership = dcrh.markKMMReservationManaged
			}
			if err := markOwnership(ctx, devConfig, nodeName); err != nil {
				return true, true, err
			}
			pending = true
			requeue = true
			continue
		}
		neverKMMUnavailable := reservation.KMMOwnership == amdv1alpha1.KMMOwnershipUnknown && moduleUnavailable &&
			devConfig.Spec.Driver.Enable != nil && !*devConfig.Spec.Driver.Enable
		if moduleUnavailable && reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged && !neverKMMUnavailable {
			pending = true
			requeue = true
			continue
		}

		managed, err := dcrh.nodeHasKMMModuleState(ctx, devConfig, nodeName, pods)
		if meta.IsNoMatchError(err) {
			if reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged && !neverKMMUnavailable {
				pending = true
				requeue = true
				continue
			}
			err = nil
		}
		if err != nil {
			return true, true, err
		}
		if managed {
			if err := dcrh.markKMMReservationManaged(ctx, devConfig, nodeName); err != nil {
				return true, true, err
			}
			pending = true
			requeue = true
			continue
		}
		delete(devConfig.Status.NodeModuleStatus, nodeName)
		if err := dcrh.setNodeReservation(ctx, devConfig, nodeName, nil); err != nil {
			return true, true, err
		}
	}
	return pending, requeue, nil
}

func (dcrh *deviceConfigReconcilerHelper) reconcileVFIONodeRetirement(
	ctx context.Context,
	devConfig *amdv1alpha1.DeviceConfig,
	nodeName string,
	selected bool,
	node *v1.Node,
	nodeErr error,
	reservation amdv1alpha1.NodeReservationStatus,
	moduleFound, moduleOwned, moduleUnavailable bool,
	module *kmmv1beta1.Module,
	pods *v1.PodList,
) (pending, requeue, handled bool, err error) {
	if devConfig.Spec.Driver.DriverType != utils.DriverTypeVFPassthrough ||
		(!reservation.KMMRetirementPending && !k8serrors.IsNotFound(nodeErr)) {
		return false, false, false, nil
	}
	if nodeErr != nil && !k8serrors.IsNotFound(nodeErr) {
		return true, true, true, fmt.Errorf("getting node %s while checking KMM retirement: %w", nodeName, nodeErr)
	}
	if nodeErr == nil {
		reservation.KMMNodeLabels = maps.Clone(node.Labels)
		reservation.KMMNodeLabelsCaptured = true
	}
	persist := func(status amdv1alpha1.NodeReservationStatus) error {
		return dcrh.setNodeReservation(ctx, devConfig, nodeName, &status)
	}
	holdUnknown := func() (bool, bool, bool, error) {
		status := reservation
		if status.KMMOwnership != amdv1alpha1.KMMOwnershipManaged {
			status.KMMOwnership = amdv1alpha1.KMMOwnershipUnknown
		}
		if err := persist(status); err != nil {
			return true, true, true, err
		}
		return true, true, true, nil
	}
	holdManaged := func() (bool, bool, bool, error) {
		status := reservation
		status.KMMOwnership = amdv1alpha1.KMMOwnershipManaged
		status.KMMRetirementPending = false
		if err := persist(status); err != nil {
			return true, true, true, err
		}
		return !selected, !selected, true, nil
	}
	if reservation.VFIOCleanupPending || reservation.VFIOBindingBootID != "" {
		return holdUnknown()
	}
	if _, err := dcrh.kmmPostProcessor.GetWorkerPod(ctx, devConfig, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}); err == nil {
		return holdUnknown()
	} else if !k8serrors.IsNotFound(err) {
		return true, true, true, fmt.Errorf("checking VFIO worker for node %s during KMM retirement: %w", nodeName, err)
	}
	if moduleUnavailable {
		return holdUnknown()
	}
	managed, stateErr := dcrh.nodeHasKMMModuleState(ctx, devConfig, nodeName, pods)
	if stateErr != nil {
		if meta.IsNoMatchError(stateErr) {
			return holdUnknown()
		}
		return true, true, true, fmt.Errorf("checking KMM state before retiring node %s: %w", nodeName, stateErr)
	}
	if managed {
		return holdManaged()
	}
	if moduleFound && !moduleOwned {
		return holdUnknown()
	}
	if moduleFound {
		selector := labels.Set(module.Spec.Selector).AsSelector()
		selectorKnown := nodeErr == nil || reservation.KMMNodeLabelsCaptured
		selectorMatches := nodeErr == nil && selector.Matches(labels.Set(node.Labels))
		if k8serrors.IsNotFound(nodeErr) && reservation.KMMNodeLabelsCaptured {
			selectorMatches = selector.Matches(labels.Set(reservation.KMMNodeLabels))
		}
		if !selectorKnown {
			return holdUnknown()
		}
		if selectorMatches {
			return holdManaged()
		}
	}
	if reservation.KMMRetirementPending && moduleStatusShowsKMM(devConfig.Status.NodeModuleStatus[nodeName]) {
		return holdManaged()
	}
	if k8serrors.IsNotFound(nodeErr) {
		reservation.KMMOwnership = amdv1alpha1.KMMOwnershipNeverManaged
		reservation.KMMRetirementPending = true
		delete(devConfig.Status.NodeModuleStatus, nodeName)
		if err := persist(reservation); err != nil {
			return true, true, true, err
		}
		return false, false, true, nil
	}
	if selected {
		reservation.KMMOwnership = amdv1alpha1.KMMOwnershipNeverManaged
		reservation.KMMRetirementPending = false
		if err := persist(reservation); err != nil {
			return true, true, true, err
		}
		return false, false, true, nil
	}
	delete(devConfig.Status.NodeModuleStatus, nodeName)
	if err := dcrh.setNodeReservation(ctx, devConfig, nodeName, nil); err != nil {
		return true, true, true, err
	}
	return false, false, true, nil
}

func (dcrh *deviceConfigReconcilerHelper) markKMMReservationManaged(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string) error {
	reservation, _ := dcrh.nodeReservation(devConfig, node)
	if reservation.KMMOwnership == amdv1alpha1.KMMOwnershipManaged {
		return nil
	}
	reservation.KMMOwnership = amdv1alpha1.KMMOwnershipManaged
	return dcrh.setNodeReservation(ctx, devConfig, node, &reservation)
}

func (dcrh *deviceConfigReconcilerHelper) markKMMReservationUnknown(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, node string) error {
	reservation, _ := dcrh.nodeReservation(devConfig, node)
	if reservation.KMMOwnership == amdv1alpha1.KMMOwnershipManaged || reservation.KMMOwnership == amdv1alpha1.KMMOwnershipUnknown {
		return nil
	}
	reservation.KMMOwnership = amdv1alpha1.KMMOwnershipUnknown
	return dcrh.setNodeReservation(ctx, devConfig, node, &reservation)
}

func hasUnselectedReservation(devConfig *amdv1alpha1.DeviceConfig, selected map[string]struct{}) bool {
	for node := range devConfig.Status.NodeReservations {
		if _, ok := selected[node]; !ok {
			return true
		}
	}
	return false
}

func hasKMMRetirementPending(devConfig *amdv1alpha1.DeviceConfig) bool {
	for _, reservation := range devConfig.Status.NodeReservations {
		if reservation.KMMRetirementPending {
			return true
		}
	}
	return false
}

func hasUnselectedKMMReservation(devConfig *amdv1alpha1.DeviceConfig, selected *v1.NodeList) bool {
	selectedNames := make(map[string]struct{})
	if selected != nil {
		for _, node := range selected.Items {
			selectedNames[node.Name] = struct{}{}
		}
	}
	if !devConfig.Status.NodeReservationsInitialized {
		for node := range devConfig.Status.NodeModuleStatus {
			if _, ok := selectedNames[node]; !ok {
				return true
			}
		}
		return false
	}
	for node, reservation := range devConfig.Status.NodeReservations {
		if _, ok := selectedNames[node]; !ok && reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged {
			return true
		}
	}
	return false
}

func (dcrh *deviceConfigReconcilerHelper) kmmOwnershipForNewReservation(devConfig *amdv1alpha1.DeviceConfig, node string) amdv1alpha1.KMMOwnership {
	if dcrh.shouldUseKMMOperatorLevel(devConfig) || moduleStatusShowsKMM(devConfig.Status.NodeModuleStatus[node]) {
		return amdv1alpha1.KMMOwnershipManaged
	}
	if utils.ShouldUseKMM(devConfig) && !dcrh.kmmWatchEnabled {
		return amdv1alpha1.KMMOwnershipUnknown
	}
	return amdv1alpha1.KMMOwnershipNeverManaged
}

func (dcrh *deviceConfigReconcilerHelper) kmmOwnershipForLegacyReservation(devConfig *amdv1alpha1.DeviceConfig, node string) amdv1alpha1.KMMOwnership {
	if dcrh.shouldUseKMMOperatorLevel(devConfig) || moduleStatusShowsKMM(devConfig.Status.NodeModuleStatus[node]) {
		return amdv1alpha1.KMMOwnershipManaged
	}
	return amdv1alpha1.KMMOwnershipUnknown
}

func moduleStatusShowsKMM(status amdv1alpha1.ModuleStatus) bool {
	return status.ContainerImage != "" || status.KernelVersion != "" || status.LastTransitionTime != ""
}

func deviceConfigKMMSelector(devConfig *amdv1alpha1.DeviceConfig) map[string]string {
	if devConfig.Spec.Selector != nil {
		return maps.Clone(devConfig.Spec.Selector)
	}
	return map[string]string{utils.NodeFeatureLabelAmdGpu: "true"}
}

func (dcrh *deviceConfigReconcilerHelper) nodeHasKMMModuleState(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodeName string, pods *v1.PodList) (bool, error) {
	nmc := &kmmv1beta1.NodeModulesConfig{}
	var apiUnavailable error
	if err := dcrh.client.Get(ctx, types.NamespacedName{Name: nodeName}, nmc); err == nil {
		for _, module := range nmc.Spec.Modules {
			if module.Namespace == devConfig.Namespace && module.Name == devConfig.Name {
				return true, nil
			}
		}
		for _, module := range nmc.Status.Modules {
			if module.Namespace == devConfig.Namespace && module.Name == devConfig.Name {
				return true, nil
			}
		}
		for _, suffix := range []string{"module-configured", "module-in-use"} {
			key := fmt.Sprintf("beta.kmm.node.kubernetes.io/%s.%s.%s", devConfig.Namespace, devConfig.Name, suffix)
			if _, ok := nmc.Labels[key]; ok {
				return true, nil
			}
		}
	} else if !k8serrors.IsNotFound(err) {
		if meta.IsNoMatchError(err) {
			apiUnavailable = err
		} else {
			return false, fmt.Errorf("getting NodeModulesConfig for node %s: %w", nodeName, err)
		}
	}

	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName == nodeName &&
			pods.Items[i].Labels[kmmModuleNameLabel] == devConfig.Name &&
			pods.Items[i].Labels[kmmWorkerComponentLabel] == kmmWorkerComponent {
			return true, nil
		}
	}
	return false, apiUnavailable
}
