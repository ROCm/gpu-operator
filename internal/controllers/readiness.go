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

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/conditions"
)

func deviceConfigReadiness(devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList, module *kmmv1beta1.Module) (bool, string, string) {
	if devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough {
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
	return true, conditions.ReadyStatus, ""
}
