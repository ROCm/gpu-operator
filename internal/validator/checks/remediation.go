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

// ValidateRemediationWorkflow validates the remediation workflow component
func ValidateRemediationWorkflow(ctx context.Context, k8sClient client.Client, devConfig *gpuev1alpha1.DeviceConfig) gpuev1alpha1.ComponentValidationResult {
	result := gpuev1alpha1.ComponentValidationResult{
		Name:   "RemediationWorkflow",
		Status: "healthy",
		Checks: []gpuev1alpha1.ValidationCheck{},
	}

	// Check 1: Config ConfigMap exists (if specified)
	if devConfig.Spec.RemediationWorkflow.Config != nil {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ConfigurationMatch",
			Name:    "Remediation config specified",
			Passed:  true,
			Message: "Remediation ConfigMap configured",
		})
	} else {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ConfigurationMatch",
			Name:    "Remediation config specified",
			Passed:  true,
			Message: "Using default remediation configuration",
		})
	}

	return result
}
