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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// StatusUpdateRetries is the number of times to retry updating status on conflict
	StatusUpdateRetries = 5
	// StatusUpdateBackoff is the initial backoff duration for retries
	StatusUpdateBackoff = 100 * time.Millisecond
)

// updateValidationStatus updates the validation status of a DeviceConfig with retry logic
func updateValidationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	deviceConfigName string,
	validationStatus *gpuev1alpha1.ValidationStatus,
) error {
	backoff := wait.Backoff{
		Steps:    StatusUpdateRetries,
		Duration: StatusUpdateBackoff,
		Factor:   2.0,
		Jitter:   0.1,
	}

	return wait.ExponentialBackoff(backoff, func() (bool, error) {
		// Fetch latest DeviceConfig
		devConfig := &gpuev1alpha1.DeviceConfig{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      deviceConfigName,
		}, devConfig)
		if err != nil {
			return false, fmt.Errorf("failed to fetch DeviceConfig: %w", err)
		}

		// Update validation status
		devConfig.Status.Validation = *validationStatus

		// Update status subresource
		err = k8sClient.Status().Update(ctx, devConfig)
		if err != nil {
			// Retry on conflict
			return false, nil
		}

		// Success
		return true, nil
	})
}
