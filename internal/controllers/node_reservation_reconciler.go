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
	if len(selected.Items) > 0 && !needsRelease && dcrh.shouldUseKMMOperatorLevel(devConfig) {
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
	if !needsRelease {
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
	for nodeName := range devConfig.Status.NodeReservations {
		if _, ok := selectedNames[nodeName]; ok {
			continue
		}

		node := &v1.Node{}
		nodeErr := dcrh.client.Get(ctx, types.NamespacedName{Name: nodeName}, node)
		if nodeErr != nil && !k8serrors.IsNotFound(nodeErr) {
			return true, true, fmt.Errorf("getting node %s while releasing KMM reservation: %w", nodeName, nodeErr)
		}
		reservation, _ := dcrh.nodeReservation(devConfig, nodeName)
		if moduleFound && nodeErr != nil {
			if reservation.KMMOwnership == amdv1alpha1.KMMOwnershipNeverManaged {
				if err := dcrh.markKMMReservationUnknown(ctx, devConfig, nodeName); err != nil {
					return true, true, err
				}
			}
			pending = true
			requeue = true
			continue
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
