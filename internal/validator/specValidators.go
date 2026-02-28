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

package validator

import (
	"context"
	"fmt"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DriverSpec validation
func ValidateDriverSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	if devConfig.Spec.Driver.DriverType == "" {
		devConfig.Spec.Driver.DriverType = utils.DriverTypeContainer
	}
	dSpec := devConfig.Spec.Driver

	switch dSpec.DriverType {
	case utils.DriverTypeContainer,
		utils.DriverTypeVFPassthrough,
		utils.DriverTypePFPassthrough:
		// valid
	default:
		return fmt.Errorf("invalid driver type %v", dSpec.DriverType)
	}

	// if KMM is not triggered, no need to verify the rest of the config
	if !utils.ShouldUseKMM(devConfig) {
		return nil
	}

	if dSpec.ImageRegistrySecret != nil {
		if err := validateSecret(ctx, client, dSpec.ImageRegistrySecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageRegistrySecret: %v", err)
		}
	}

	if dSpec.ImageSign.KeySecret != nil {
		if err := validateSecret(ctx, client, dSpec.ImageSign.KeySecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageSign KeySecret: %v", err)
		}
	}

	if dSpec.ImageSign.CertSecret != nil {
		if err := validateSecret(ctx, client, dSpec.ImageSign.CertSecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageSign CertSecret: %v", err)
		}
	}

	return nil
}

// MetricsExporterSpec validation
func ValidateMetricsExporterSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	mSpec := devConfig.Spec.MetricsExporter

	if mSpec.Enable == nil || !*mSpec.Enable {
		return nil
	}

	if mSpec.ImageRegistrySecret != nil {
		if err := validateSecret(ctx, client, mSpec.ImageRegistrySecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageRegistrySecret: %v", err)
		}
	}

	if mSpec.Config.Name != "" {
		if err := validateConfigMap(ctx, client, mSpec.Config.Name, devConfig.Namespace); err != nil {
			return fmt.Errorf("ConfigMap: %v", err)
		}
	}

	// Validate ServiceMonitor CRD availability if ServiceMonitor is enabled
	if utils.IsPrometheusServiceMonitorEnable(devConfig) {
		if err := validateServiceMonitorCRD(ctx, client); err != nil {
			return fmt.Errorf("ServiceMonitor: %v", err)
		}
	}

	return nil
}

// DevicePluginSpec validation
func ValidateDevicePluginSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	dSpec := devConfig.Spec.DevicePlugin

	if dSpec.ImageRegistrySecret != nil {
		if err := validateSecret(ctx, client, dSpec.ImageRegistrySecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageRegistrySecret: %v", err)
		}
	}

	supportedFlagValues := map[string][]string{
		utils.ResourceNamingStrategyFlag: {utils.SingleStrategy, utils.MixedStrategy},
		utils.DriverTypeFlag:             {utils.DriverTypeContainer, utils.DriverTypeVFPassthrough, utils.DriverTypePFPassthrough},
	}

	devicePluginArguments := devConfig.Spec.DevicePlugin.DevicePluginArguments
	for key, val := range devicePluginArguments {
		validValues, validKey := supportedFlagValues[key]
		if !validKey {
			return fmt.Errorf("Invalid flag: %s", key)
		}
		validKeyValue := false

		for _, validVal := range validValues {
			if val == validVal {
				validKeyValue = true
				break
			}
		}

		if !validKeyValue {
			return fmt.Errorf("Invalid flag value: %s=%s. Supported values: %v", key, val, supportedFlagValues[key])
		}
	}

	return nil
}

func ValidateRemediationWorkflowSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	rSpec := devConfig.Spec.RemediationWorkflow

	if rSpec.Enable == nil || !*rSpec.Enable {
		return nil
	}

	if rSpec.Config != nil {
		if err := validateConfigMap(ctx, client, rSpec.Config.Name, devConfig.Namespace); err != nil {
			return fmt.Errorf("validating remediation workflow config map: %v", err)
		}
	}

	if rSpec.TtlForFailedWorkflows != "" {
		if _, err := time.ParseDuration(rSpec.TtlForFailedWorkflows); err != nil {
			return fmt.Errorf("parsing ttlForFailedWorkflows: %v", err)
		}
	}

	return nil
}
