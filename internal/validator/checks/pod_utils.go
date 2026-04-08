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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodIssue represents a problem found with a pod
type PodIssue struct {
	PodName       string
	ContainerName string
	Reason        string
	Message       string
}

// checkPodsForImagePullErrors inspects pods for image pull and crash errors
func checkPodsForImagePullErrors(ctx context.Context, k8sClient client.Client, namespace string, labelSelector map[string]string) []PodIssue {
	issues := []PodIssue{}

	// List pods with the given label selector
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labelSelector),
	}

	if err := k8sClient.List(ctx, podList, listOpts...); err != nil {
		// If we can't list pods, return empty - the component check will catch this
		return issues
	}

	// Check each pod's container statuses
	for _, pod := range podList.Items {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			// Check for waiting state with errors
			if containerStatus.State.Waiting != nil {
				reason := containerStatus.State.Waiting.Reason
				// Check for common error states
				if reason == "ImagePullBackOff" || reason == "ErrImagePull" ||
					reason == "CrashLoopBackOff" || reason == "CreateContainerError" ||
					reason == "InvalidImageName" {
					issues = append(issues, PodIssue{
						PodName:       pod.Name,
						ContainerName: containerStatus.Name,
						Reason:        reason,
						Message:       containerStatus.State.Waiting.Message,
					})
				}
			}
		}

		// Also check init container statuses
		for _, containerStatus := range pod.Status.InitContainerStatuses {
			if containerStatus.State.Waiting != nil {
				reason := containerStatus.State.Waiting.Reason
				if reason == "ImagePullBackOff" || reason == "ErrImagePull" ||
					reason == "CrashLoopBackOff" || reason == "CreateContainerError" ||
					reason == "InvalidImageName" {
					issues = append(issues, PodIssue{
						PodName:       pod.Name,
						ContainerName: containerStatus.Name + " (init)",
						Reason:        reason,
						Message:       containerStatus.State.Waiting.Message,
					})
				}
			}
		}
	}

	return issues
}
