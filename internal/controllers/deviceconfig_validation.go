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
	"context"
	"fmt"
	"time"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ValidationAnnotation is the annotation key that triggers validation
	ValidationAnnotation = "gpu.amd.com/validate"

	// DefaultValidatorImage is the default validator container image
	DefaultValidatorImage = "rocm/gpu-operator-validator:latest"
)

// createValidationJob creates a Kubernetes Job to run validation
func createValidationJob(
	ctx context.Context,
	k8sClient client.Client,
	devConfig *gpuev1alpha1.DeviceConfig,
) (*batchv1.Job, error) {
	logger := log.FromContext(ctx).WithName("validation")

	// Get configuration from spec
	validatorImage := devConfig.Spec.Validation.Image
	if validatorImage == "" {
		validatorImage = DefaultValidatorImage
	}

	imagePullPolicy := devConfig.Spec.Validation.ImagePullPolicy
	if imagePullPolicy == "" {
		imagePullPolicy = corev1.PullIfNotPresent
	}

	ttlSecondsAfterFinished := devConfig.Spec.Validation.TTLSecondsAfterFinished
	if ttlSecondsAfterFinished == nil {
		ttlSecondsAfterFinished = ptr.To(int32(1800)) // 30 minutes
	}

	activeDeadlineSeconds := devConfig.Spec.Validation.ActiveDeadlineSeconds
	if activeDeadlineSeconds == nil {
		activeDeadlineSeconds = ptr.To(int64(600)) // 10 minutes
	}

	// Generate Job name with timestamp to ensure uniqueness
	jobName := fmt.Sprintf("%s-validator-%d", devConfig.Name, time.Now().Unix())

	// Create Job spec
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: devConfig.Namespace,
			Labels: map[string]string{
				"app":          "gpu-operator-validator",
				"deviceconfig": devConfig.Name,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ttlSecondsAfterFinished,
			ActiveDeadlineSeconds:   activeDeadlineSeconds,
			BackoffLimit:            ptr.To(int32(0)), // No retries
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "gpu-operator-validator",
						"deviceconfig": devConfig.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "gpu-operator-validator",
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "validator",
							Image:           validatorImage,
							ImagePullPolicy: imagePullPolicy,
							Args: []string{
								fmt.Sprintf("--namespace=%s", devConfig.Namespace),
								fmt.Sprintf("--deviceconfig-name=%s", devConfig.Name),
							},
						},
					},
					Tolerations: devConfig.Spec.Validation.Tolerations,
				},
			},
		},
	}

	// Add ImagePullSecrets if specified
	if devConfig.Spec.Validation.ImageRegistrySecret != nil {
		job.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			*devConfig.Spec.Validation.ImageRegistrySecret,
		}
	}

	// Set DeviceConfig as owner
	if err := controllerutil.SetControllerReference(devConfig, job, k8sClient.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create Job
	if err := k8sClient.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create validation Job: %w", err)
	}

	logger.Info("Created validation Job", "job", jobName)
	return job, nil
}

// handleValidationAnnotation checks if validation was requested and creates Job if needed
// This is a method of deviceConfigReconcilerHelper to match the interface
func (dcrh *deviceConfigReconcilerHelper) handleValidationAnnotation(
	ctx context.Context,
	devConfig *gpuev1alpha1.DeviceConfig,
) error {
	logger := log.FromContext(ctx).WithName("validation")

	// Check if validation annotation exists
	annotationValue, exists := devConfig.Annotations[ValidationAnnotation]
	if !exists {
		return nil // No validation requested
	}

	// Check if validation is already in progress or was recently completed
	currentValidation := devConfig.Status.Validation
	if currentValidation.RequestedAt == annotationValue {
		// This validation request was already processed
		if currentValidation.State == "InProgress" || currentValidation.State == "Completed" {
			logger.V(1).Info("Validation already processed", "requestedAt", annotationValue)
			return nil
		}
	}

	// Create new validation Job
	logger.Info("Creating validation Job for new request", "requestedAt", annotationValue)
	job, err := createValidationJob(ctx, dcrh.client, devConfig)
	if err != nil {
		return fmt.Errorf("failed to create validation Job: %w", err)
	}

	// Update status with Job information
	devConfig.Status.Validation = gpuev1alpha1.ValidationStatus{
		RequestedAt: annotationValue,
		State:       "InProgress",
		JobName:     job.Name,
		StartedAt:   &metav1.Time{Time: time.Now()},
	}

	// Update status subresource
	if err := dcrh.client.Status().Update(ctx, devConfig); err != nil {
		logger.Error(err, "Failed to update validation status")
		// Don't fail - Job is already created
	}

	return nil
}
