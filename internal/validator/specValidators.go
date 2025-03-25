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

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DriverSpec validation
func ValidateDriverSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	dSpec := devConfig.Spec.Driver

	if dSpec.Enable == nil || !*dSpec.Enable {
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
