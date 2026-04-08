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

package checks

import (
	"context"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateDeviceConfig validates the DeviceConfig CR itself
func ValidateDeviceConfig(ctx context.Context, k8sClient client.Client, devConfig *gpuev1alpha1.DeviceConfig) gpuev1alpha1.ComponentValidationResult {
	result := gpuev1alpha1.ComponentValidationResult{
		Name:   "DeviceConfig",
		Status: "healthy",
		Checks: []gpuev1alpha1.ValidationCheck{},
	}

	// Check 1: DeviceConfig CR exists (already verified by caller)
	result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
		Type:    "ResourceExistence",
		Name:    "DeviceConfig CR exists",
		Passed:  true,
		Message: "DeviceConfig CR found",
	})

	// Check 2: Mutual exclusion - DevicePlugin and DRADriver cannot both be enabled
	dpEnabled := devConfig.Spec.DevicePlugin.EnableDevicePlugin != nil && *devConfig.Spec.DevicePlugin.EnableDevicePlugin
	draEnabled := devConfig.Spec.DRADriver.Enable != nil && *devConfig.Spec.DRADriver.Enable

	if dpEnabled && draEnabled {
		result.Status = "failed"
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "MutualExclusion",
			Name:    "DevicePlugin and DRADriver mutual exclusion",
			Passed:  false,
			Message: "Both DevicePlugin and DRADriver are enabled - they are mutually exclusive",
			Details: "Disable either spec.devicePlugin.enableDevicePlugin or spec.draDriver.enable",
		})
	} else {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "MutualExclusion",
			Name:    "DevicePlugin and DRADriver mutual exclusion",
			Passed:  true,
			Message: "Mutual exclusion satisfied",
		})
	}

	// Check 3: Spec validation (basic sanity)
	if devConfig.Spec.Selector == nil || len(devConfig.Spec.Selector) == 0 {
		result.Status = "warning"
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ConfigurationMatch",
			Name:    "Node selector configured",
			Passed:  false,
			Message: "No node selector specified - DeviceConfig won't target any nodes",
			Details: "Set spec.selector to target nodes with AMD GPUs",
		})
	} else {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ConfigurationMatch",
			Name:    "Node selector configured",
			Passed:  true,
			Message: "Node selector configured",
		})
	}

	return result
}
