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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=validator.go -package=validator -destination=mock_validator.go ValidatorAPI
type ValidatorAPI interface {
	ValidateDeviceConfigAll(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) []string
	ValidateDeviceConfigSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig, specs []string) []string
}

type validator struct {
	specValidationFuncs map[string]func(context.Context, client.Client, *amdv1alpha1.DeviceConfig) error
}

func NewValidator() ValidatorAPI {
	// Map of spec names to their respective validation functions
	specValidationFuncs := map[string]func(context.Context, client.Client, *amdv1alpha1.DeviceConfig) error{
		"driver":          ValidateDriverSpec,
		"metricsExporter": ValidateMetricsExporterSpec,
		"devicePlugin":    ValidateDevicePluginSpec,
	}
	vInst := &validator{
		specValidationFuncs: specValidationFuncs,
	}
	return vInst
}

// Validate entire spec
func (v *validator) ValidateDeviceConfigAll(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig) []string {
	var failedValidations []string

	for spec, validate := range v.specValidationFuncs {
		err := validate(ctx, client, devConfig)
		if err != nil {
			failedValidations = append(failedValidations, fmt.Sprintf("%s %v", spec, err.Error()))
		}
	}

	return failedValidations
}

// Validate only the sections that have changed
func (v *validator) ValidateDeviceConfigSpec(ctx context.Context, client client.Client, devConfig *amdv1alpha1.DeviceConfig, specs []string) []string {
	var failedValidations []string

	for _, spec := range specs {
		if validate, ok := v.specValidationFuncs[spec]; ok {
			err := validate(ctx, client, devConfig)
			if err != nil {
				failedValidations = append(failedValidations, err.Error())
			}
		} else {
			failedValidations = append(failedValidations, fmt.Sprintf("No spec validator found for spec: %s", spec))
		}
	}

	return failedValidations
}
