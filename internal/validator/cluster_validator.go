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

package validator

import (
	"context"
	"fmt"
	"time"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/validator/checks"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterValidator orchestrates all validation checks and updates DeviceConfig status
type ClusterValidator struct {
	client           client.Client
	namespace        string
	deviceConfigName string
}

// NewClusterValidator creates a new ClusterValidator instance
func NewClusterValidator(client client.Client, namespace, deviceConfigName string) *ClusterValidator {
	return &ClusterValidator{
		client:           client,
		namespace:        namespace,
		deviceConfigName: deviceConfigName,
	}
}

// Validate runs all validation checks and updates DeviceConfig status
func (v *ClusterValidator) Validate(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("validator")
	logger.Info("Starting validation")

	// Get DeviceConfig
	devConfig := &gpuev1alpha1.DeviceConfig{}
	err := v.client.Get(ctx, types.NamespacedName{
		Namespace: v.namespace,
		Name:      v.deviceConfigName,
	}, devConfig)
	if err != nil {
		return fmt.Errorf("failed to get DeviceConfig: %w", err)
	}

	// Initialize validation status
	validationStatus := &gpuev1alpha1.ValidationStatus{
		RequestedAt:     devConfig.Annotations["gpu.amd.com/validate"],
		State:           "InProgress",
		StartedAt:       &metav1.Time{Time: time.Now()},
		Components:      []gpuev1alpha1.ComponentValidationResult{},
		Recommendations: []string{},
	}

	// Update status to InProgress
	if err := updateValidationStatus(ctx, v.client, v.namespace, v.deviceConfigName, validationStatus); err != nil {
		logger.Error(err, "failed to update validation status to InProgress")
		// Continue anyway - status update is not critical for running checks
	}

	// Run all component validations
	componentResults := []gpuev1alpha1.ComponentValidationResult{}

	// 1. Validate DeviceConfig itself
	logger.Info("Validating DeviceConfig")
	dcResult := checks.ValidateDeviceConfig(ctx, v.client, devConfig)
	componentResults = append(componentResults, dcResult)

	// 2. Validate Dependencies (NFD, KMM, Argo)
	logger.Info("Validating dependencies")
	depsResult := checks.ValidateDependencies(ctx, v.client, devConfig)
	componentResults = append(componentResults, depsResult)

	// 3. Validate Driver
	if devConfig.Spec.Driver.Enable == nil || *devConfig.Spec.Driver.Enable {
		logger.Info("Validating driver")
		driverResult := checks.ValidateDriver(ctx, v.client, devConfig)
		componentResults = append(componentResults, driverResult)
	}

	// 4. Validate Node Labeller (if enabled)
	if devConfig.Spec.DevicePlugin.EnableNodeLabeller != nil && *devConfig.Spec.DevicePlugin.EnableNodeLabeller {
		logger.Info("Validating node labeller")
		nlResult := checks.ValidateNodeLabeller(ctx, v.client, devConfig)
		componentResults = append(componentResults, nlResult)
	}

	// 5. Validate Device Plugin (if enabled)
	if devConfig.Spec.DevicePlugin.EnableDevicePlugin != nil && *devConfig.Spec.DevicePlugin.EnableDevicePlugin {
		logger.Info("Validating device plugin")
		dpResult := checks.ValidateDevicePlugin(ctx, v.client, devConfig)
		componentResults = append(componentResults, dpResult)
	}

	// 6. Validate DRA Driver (if enabled)
	if devConfig.Spec.DRADriver.Enable != nil && *devConfig.Spec.DRADriver.Enable {
		logger.Info("Validating DRA driver")
		draResult := checks.ValidateDRADriver(ctx, v.client, devConfig)
		componentResults = append(componentResults, draResult)
	}

	// 7. Validate Metrics Exporter (if enabled)
	if devConfig.Spec.MetricsExporter.Enable != nil && *devConfig.Spec.MetricsExporter.Enable {
		logger.Info("Validating metrics exporter")
		metricsResult := checks.ValidateMetricsExporter(ctx, v.client, devConfig)
		componentResults = append(componentResults, metricsResult)
	}

	// 8. Validate Config Manager (if enabled)
	if devConfig.Spec.ConfigManager.Enable != nil && *devConfig.Spec.ConfigManager.Enable {
		logger.Info("Validating config manager")
		cmResult := checks.ValidateConfigManager(ctx, v.client, devConfig)
		componentResults = append(componentResults, cmResult)
	}

	// 9. Validate Test Runner (if enabled)
	if devConfig.Spec.TestRunner.Enable != nil && *devConfig.Spec.TestRunner.Enable {
		logger.Info("Validating test runner")
		trResult := checks.ValidateTestRunner(ctx, v.client, devConfig)
		componentResults = append(componentResults, trResult)
	}

	// 10. Validate Remediation Workflow (if enabled)
	if devConfig.Spec.RemediationWorkflow.Enable != nil && *devConfig.Spec.RemediationWorkflow.Enable {
		logger.Info("Validating remediation workflow")
		remResult := checks.ValidateRemediationWorkflow(ctx, v.client, devConfig)
		componentResults = append(componentResults, remResult)
	}

	// Calculate overall status
	overallStatus := calculateOverallStatus(componentResults)

	// Generate recommendations
	recommendations := generateRecommendations(componentResults)

	// Update validation status with results
	validationStatus.State = "Completed"
	validationStatus.CompletedAt = &metav1.Time{Time: time.Now()}
	validationStatus.OverallStatus = overallStatus
	validationStatus.Components = componentResults
	validationStatus.Recommendations = recommendations

	// Update final status
	if err := updateValidationStatus(ctx, v.client, v.namespace, v.deviceConfigName, validationStatus); err != nil {
		return fmt.Errorf("failed to update final validation status: %w", err)
	}

	logger.Info("Validation completed", "overallStatus", overallStatus)
	return nil
}

// calculateOverallStatus determines the overall health status based on component results
func calculateOverallStatus(components []gpuev1alpha1.ComponentValidationResult) string {
	hasFailed := false
	hasDegraded := false
	hasWarning := false

	for _, comp := range components {
		switch comp.Status {
		case "failed":
			hasFailed = true
		case "degraded":
			hasDegraded = true
		case "warning":
			hasWarning = true
		}
	}

	if hasFailed {
		return "failed"
	}
	if hasDegraded {
		return "degraded"
	}
	if hasWarning {
		return "warning"
	}
	return "healthy"
}

// generateRecommendations creates actionable recommendations based on validation results
func generateRecommendations(components []gpuev1alpha1.ComponentValidationResult) []string {
	recommendations := []string{}

	for _, comp := range components {
		if comp.Status == "healthy" {
			continue
		}

		// Collect failed checks
		failedChecks := []string{}
		for _, check := range comp.Checks {
			if !check.Passed {
				failedChecks = append(failedChecks, check.Name)
			}
		}

		if len(failedChecks) > 0 {
			recommendations = append(recommendations,
				fmt.Sprintf("Component %s has issues: review logs and check configuration", comp.Name))
		}
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "All components are healthy")
	}

	return recommendations
}
