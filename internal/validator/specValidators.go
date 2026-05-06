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
	"strings"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
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

// DRADriverSpec validation
func ValidateDRADriverSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	draSpec := devConfig.Spec.DRADriver
	pluginSpec := devConfig.Spec.DevicePlugin

	// Check if both DRA driver and Device Plugin are enabled
	draEnabled := draSpec.IsEnabled()
	pluginEnabled := pluginSpec.IsEnabled()

	if draEnabled && pluginEnabled {
		return fmt.Errorf("DRADriver and DevicePlugin cannot be enabled at the same time")
	}

	if !draEnabled {
		return nil
	}

	if draSpec.ImageRegistrySecret != nil {
		if err := validateSecret(ctx, client, draSpec.ImageRegistrySecret, devConfig.Namespace); err != nil {
			return fmt.Errorf("ImageRegistrySecret: %v", err)
		}
	}

	return nil
}

// DevicePluginSpec validation
func ValidateDevicePluginSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	pluginSpec := devConfig.Spec.DevicePlugin
	draSpec := devConfig.Spec.DRADriver

	// Check if both DRA driver and Device Plugin are enabled
	draEnabled := draSpec.IsEnabled()
	pluginEnabled := pluginSpec.IsEnabled()

	if draEnabled && pluginEnabled {
		return fmt.Errorf("DRADriver and DevicePlugin cannot be enabled at the same time")
	}

	if !pluginEnabled {
		return nil
	}
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

func validateTaintEffect(effect v1.TaintEffect) error {
	if effect != v1.TaintEffectNoSchedule && effect != v1.TaintEffectPreferNoSchedule && effect != v1.TaintEffectNoExecute {
		return fmt.Errorf("unsupported taint effect %v", effect)
	}

	return nil
}

func checkTaintValidation(taint v1.Taint) error {
	if errs := validation.IsQualifiedName(taint.Key); len(errs) > 0 {
		return fmt.Errorf("invalid taint key: %s", strings.Join(errs, "; "))
	}
	if taint.Value != "" {
		if errs := validation.IsValidLabelValue(taint.Value); len(errs) > 0 {
			return fmt.Errorf("invalid taint value: %s", strings.Join(errs, "; "))
		}
	}
	if taint.Effect != "" {
		if err := validateTaintEffect(taint.Effect); err != nil {
			return err
		}
	}

	return nil
}

func ValidateRemediationWorkflowSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	rSpec := devConfig.Spec.RemediationWorkflow

	if rSpec.Enable == nil || !*rSpec.Enable {
		return nil
	}

	if (rSpec.Config == nil || rSpec.Config.Name == "") && rSpec.ConfigMapImage == "" {
		return fmt.Errorf("either spec.remediationWorkflow.config or spec.remediationWorkflow.configMapImage must be specified when remediation is enabled")
	}

	if rSpec.Config != nil && rSpec.Config.Name != "" {
		if err := validateConfigMap(ctx, client, rSpec.Config.Name, devConfig.Namespace); err != nil {
			return fmt.Errorf("validating remediation workflow config map: %v", err)
		}
	}

	for key, value := range rSpec.NodeRemediationLabels {
		if len(validation.IsQualifiedName(key)) > 0 {
			return fmt.Errorf("invalid label key: %s", key)
		}
		if len(validation.IsValidLabelValue(value)) > 0 {
			return fmt.Errorf("invalid label value: %s", value)
		}
	}

	for _, taint := range rSpec.NodeRemediationTaints {
		err := checkTaintValidation(taint)
		if err != nil {
			return err
		}
	}

	return nil
}

// CommonConfigSpec validation
func ValidateCommonConfigSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) error {
	commonConfig := devConfig.Spec.CommonConfig

	// Validate global ImageRegistrySecrets
	if len(commonConfig.ImageRegistrySecrets) > 0 {
		for i, secretRef := range commonConfig.ImageRegistrySecrets {
			if err := validateSecret(ctx, client, &secretRef, devConfig.Namespace); err != nil {
				return fmt.Errorf("ImageRegistrySecrets[%d]: %v", i, err)
			}
		}
	}

	return nil
}
