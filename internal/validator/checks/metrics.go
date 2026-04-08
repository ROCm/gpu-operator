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
	"fmt"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateMetricsExporter validates the metrics exporter component
func ValidateMetricsExporter(ctx context.Context, k8sClient client.Client, devConfig *gpuev1alpha1.DeviceConfig) gpuev1alpha1.ComponentValidationResult {
	result := gpuev1alpha1.ComponentValidationResult{
		Name:   "MetricsExporter",
		Status: "healthy",
		Checks: []gpuev1alpha1.ValidationCheck{},
	}

	// Check 1: DaemonSet exists
	dsName := fmt.Sprintf("%s-metrics-exporter-daemonset", devConfig.Name)
	ds := &appsv1.DaemonSet{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      dsName,
	}, ds)
	if err != nil {
		result.Status = "failed"
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ResourceExistence",
			Name:    "DaemonSet exists",
			Passed:  false,
			Message: "Metrics Exporter DaemonSet not found",
		})
		return result
	}

	result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
		Type:    "ResourceExistence",
		Name:    "DaemonSet exists",
		Passed:  true,
		Message: "Metrics Exporter DaemonSet found",
	})

	// Check 2: Service exists
	svcName := fmt.Sprintf("%s-metrics-exporter-service", devConfig.Name)
	svc := &corev1.Service{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Namespace: devConfig.Namespace,
		Name:      svcName,
	}, svc)
	if err != nil {
		result.Status = "warning"
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ResourceExistence",
			Name:    "Service exists",
			Passed:  false,
			Message: "Metrics Exporter Service not found",
		})
	} else {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ResourceExistence",
			Name:    "Service exists",
			Passed:  true,
			Message: "Metrics Exporter Service found",
		})
	}

	// Check 3: Configuration matches spec - Image
	if devConfig.Spec.MetricsExporter.Image != "" {
		if len(ds.Spec.Template.Spec.Containers) > 0 {
			actualImage := ds.Spec.Template.Spec.Containers[0].Image
			expectedImage := devConfig.Spec.MetricsExporter.Image

			if actualImage != expectedImage {
				result.Status = "degraded"
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:          "ConfigurationMatch",
					Name:          "Metrics Exporter image matches spec",
					Passed:        false,
					Message:       "Image drift detected",
					ExpectedValue: expectedImage,
					ActualValue:   actualImage,
					Details:       "Update DeviceConfig spec or restart rollout to sync configuration",
				})
			} else {
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:    "ConfigurationMatch",
					Name:    "Metrics Exporter image matches spec",
					Passed:  true,
					Message: fmt.Sprintf("Image matches: %s", expectedImage),
				})
			}
		}
	}

	// Check 4: ImagePullPolicy matches spec
	if devConfig.Spec.MetricsExporter.ImagePullPolicy != "" {
		if len(ds.Spec.Template.Spec.Containers) > 0 {
			actualPolicy := string(ds.Spec.Template.Spec.Containers[0].ImagePullPolicy)
			expectedPolicy := devConfig.Spec.MetricsExporter.ImagePullPolicy

			if actualPolicy != expectedPolicy {
				result.Status = "warning"
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:          "ConfigurationMatch",
					Name:          "ImagePullPolicy matches spec",
					Passed:        false,
					Message:       "ImagePullPolicy drift detected",
					ExpectedValue: expectedPolicy,
					ActualValue:   actualPolicy,
				})
			} else {
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:    "ConfigurationMatch",
					Name:    "ImagePullPolicy matches spec",
					Passed:  true,
					Message: fmt.Sprintf("ImagePullPolicy matches: %s", expectedPolicy),
				})
			}
		}
	}

	// Check 5: Pod-level issues (image pull errors, crashes)
	labelSelector := map[string]string{
		"app": fmt.Sprintf("%s-metrics-exporter", devConfig.Name),
	}
	podIssues := checkPodsForImagePullErrors(ctx, k8sClient, devConfig.Namespace, labelSelector)
	if len(podIssues) > 0 {
		result.Status = "failed"
		for _, issue := range podIssues {
			result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
				Type:    "PodHealth",
				Name:    fmt.Sprintf("Pod %s container health", issue.PodName),
				Passed:  false,
				Message: fmt.Sprintf("Container %s: %s", issue.ContainerName, issue.Reason),
				Details: issue.Message,
			})
		}
	}

	return result
}
