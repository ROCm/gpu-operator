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

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServiceMonitorCRDName    = "servicemonitors.monitoring.coreos.com"
	ServiceMonitorCRDGroup   = "monitoring.coreos.com"
	ServiceMonitorCRDVersion = "v1"
)

func validateSecret(ctx context.Context, client client.Client, secretRef *v1.LocalObjectReference, namespace string) error {
	if secretRef == nil || secretRef.Name == "" {
		return fmt.Errorf("Secret reference is nil or empty")
	}

	secret := &v1.Secret{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretRef.Name}, secret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("Secret %s not found in namespace %s", secretRef.Name, namespace)
		}
		return fmt.Errorf("failed to get Secret %s: %v", secretRef.Name, err)
	}

	return nil
}

func validateConfigMap(ctx context.Context, client client.Client, mapRef string, namespace string) error {
	if mapRef == "" {
		return fmt.Errorf("No ConfigMap name provided for validation")
	}

	configMap := &v1.ConfigMap{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: mapRef}, configMap)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("ConfigMap %s not found in namespace %s", mapRef, namespace)
		}
		return fmt.Errorf("failed to get ConfigMap %s: %v", mapRef, err)
	}

	return nil
}

// validateServiceMonitorCRD checks if the ServiceMonitor CRD is available in the cluster
func validateServiceMonitorCRD(ctx context.Context, c client.Client) error {
	// Define the ServiceMonitor CRD we want to check
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := c.Get(ctx, client.ObjectKey{Name: ServiceMonitorCRDName}, crd)
	if err != nil {
		return fmt.Errorf("ServiceMonitor CRD is not available in the cluster. Please ensure the Prometheus Operator is installed: %v", err)
	}

	// Check if the CRD is in the correct group
	if crd.Spec.Group != ServiceMonitorCRDGroup {
		return fmt.Errorf("ServiceMonitor CRD group mismatch. Expected %s, got %s", ServiceMonitorCRDGroup, crd.Spec.Group)
	}

	found := false
	// Check if the expected version is served
	for _, version := range crd.Spec.Versions {
		if version.Name == ServiceMonitorCRDVersion && version.Served {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("ServiceMonitor CRD does not support version %s", ServiceMonitorCRDVersion)
	}
	return nil
}

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
	podList := &v1.PodList{}
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

// checkNodesForDriverVersion scans GPU nodes for driver version labels
func checkNodesForDriverVersion(ctx context.Context, k8sClient client.Client) map[string]string {
	nodeVersions := make(map[string]string)

	// List all nodes with AMD GPU label
	nodeList := &v1.NodeList{}
	listOpts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			"feature.node.kubernetes.io/amd-gpu": "true",
		}),
	}

	if err := k8sClient.List(ctx, nodeList, listOpts...); err != nil {
		return nodeVersions
	}

	// Check each node for driver version label
	for _, node := range nodeList.Items {
		// Check for node labeler driver version
		if version, ok := node.Labels["amd.com/gpu.driver-version"]; ok {
			nodeVersions[node.Name] = version
		}
	}

	return nodeVersions
}
