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
	"fmt"
	"strings"

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/conditions"
)

func deviceConfigReadiness(devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList, module *kmmv1beta1.Module, kmmManaged bool, devicePlugin *appsv1.DaemonSet) (bool, string, string) {
	if devConfig.Spec.Driver.Enable != nil && !*devConfig.Spec.Driver.Enable {
		return true, conditions.ReadyStatus, ""
	}
	if devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough && kmmManaged {
		if module == nil || module.Status.ModuleLoader.AvailableNumber != int32(len(nodes.Items)) {
			return false, conditions.KMMModulePending, fmt.Sprintf("KMM module is not available on all %d selected nodes", len(nodes.Items))
		}
		for _, node := range nodes.Items {
			if _, ok := node.Labels[fmt.Sprintf(utils.KMMModuleReadyLabelTemplate, devConfig.Namespace, devConfig.Name)]; !ok {
				return false, conditions.KMMModulePending, fmt.Sprintf("KMM module is not ready on node %s", node.Name)
			}
		}
	}

	if devConfig.Spec.Driver.DriverType != utils.DriverTypeVFPassthrough &&
		devConfig.Spec.Driver.DriverType != utils.DriverTypePFPassthrough {
		return true, conditions.ReadyStatus, ""
	}

	vfioLabel := fmt.Sprintf(utils.VFIOMountReadyLabelTemplate, devConfig.Namespace, devConfig.Name)
	for _, node := range nodes.Items {
		if _, ok := node.Labels[vfioLabel]; !ok {
			return false, conditions.VFIOBindingPending, fmt.Sprintf("VFIO binding is not ready on node %s", node.Name)
		}
	}

	if devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough && devConfig.Spec.DevicePlugin.IsEnabled() {
		if devicePlugin == nil {
			return false, conditions.DevicePluginPending, fmt.Sprintf("device plugin is not available on all %d selected nodes", len(nodes.Items))
		}
		if devicePlugin.DeletionTimestamp != nil {
			return false, conditions.DevicePluginPending, "device plugin daemonset is terminating"
		}
		if devicePlugin.Status.ObservedGeneration < devicePlugin.Generation {
			return false, conditions.DevicePluginPending, "device plugin daemonset status has not observed its current generation"
		}
		if devicePlugin.Spec.UpdateStrategy.Type != appsv1.OnDeleteDaemonSetStrategyType &&
			devicePlugin.Status.UpdatedNumberScheduled < int32(len(nodes.Items)) {
			return false, conditions.DevicePluginPending, "device plugin daemonset is still updating on selected nodes"
		}
		if devicePlugin.Status.NumberAvailable < int32(len(nodes.Items)) {
			return false, conditions.DevicePluginPending, fmt.Sprintf("device plugin is not available on all %d selected nodes", len(nodes.Items))
		}
		for _, node := range nodes.Items {
			if !hasAMDAllocatableResource(devConfig, node) {
				return false, conditions.DevicePluginPending, fmt.Sprintf("device plugin has not advertised an AMD resource on node %s; check the device-plugin image and logs", node.Name)
			}
		}
		if devicePlugin.Spec.UpdateStrategy.Type == appsv1.OnDeleteDaemonSetStrategyType {
			return true, conditions.ReadyStatus, "device plugin is available on selected nodes; OnDelete Ready does not verify the desired image"
		}
	}
	return true, conditions.ReadyStatus, ""
}

func hasAMDAllocatableResource(devConfig *amdv1alpha1.DeviceConfig, node v1.Node) bool {
	if strategy, ok := devConfig.Spec.DevicePlugin.DevicePluginArguments[utils.ResourceNamingStrategyFlag]; ok && strategy == utils.SingleStrategy {
		quantity, ok := node.Status.Allocatable[v1.ResourceName("amd.com/gpu")]
		return ok && quantity.Sign() > 0
	}
	for resourceName, quantity := range node.Status.Allocatable {
		if strings.HasPrefix(string(resourceName), "amd.com/") && quantity.Sign() > 0 {
			return true
		}
	}
	return false
}
