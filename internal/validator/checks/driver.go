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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// checkNodesForDriverVersion scans GPU nodes for driver version labels
func checkNodesForDriverVersion(ctx context.Context, k8sClient client.Client) map[string]string {
	nodeVersions := make(map[string]string)

	// List all nodes with AMD GPU label
	nodeList := &corev1.NodeList{}
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

// ValidateDriver validates the driver installation component
func ValidateDriver(ctx context.Context, k8sClient client.Client, devConfig *gpuev1alpha1.DeviceConfig) gpuev1alpha1.ComponentValidationResult {
	result := gpuev1alpha1.ComponentValidationResult{
		Name:   "Driver",
		Status: "healthy",
		Checks: []gpuev1alpha1.ValidationCheck{},
	}

	// Check 1: Driver module status in DeviceConfig status
	if len(devConfig.Status.NodeModuleStatus) == 0 {
		result.Status = "warning"
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "DeploymentHealth",
			Name:    "Driver module status",
			Passed:  false,
			Message: "No node module status reported",
			Details: "Driver may not be installed on any nodes yet",
		})
	} else {
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "DeploymentHealth",
			Name:    "Driver module status",
			Passed:  true,
			Message: fmt.Sprintf("Driver installed on %d nodes", len(devConfig.Status.NodeModuleStatus)),
		})
	}

	// Check 2: Build ConfigMaps exist (for KMM-based driver installation)
	// Build ConfigMap name format: <osName>-<deviceconfig-name>-dockerfile-cm
	// Note: This is a simplified check - actual implementation would need to determine OS names
	if devConfig.Spec.Driver.DriverType == "container" {
		cmName := fmt.Sprintf("%s-dockerfile-cm", devConfig.Name)
		cm := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: devConfig.Namespace,
			Name:      cmName,
		}, cm)
		if err != nil {
			result.Status = "warning"
			result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
				Type:    "ResourceExistence",
				Name:    "Build ConfigMap exists",
				Passed:  false,
				Message: "Build ConfigMap not found",
				Details: fmt.Sprintf("Expected ConfigMap %s/%s not found - driver build may fail", devConfig.Namespace, cmName),
			})
		} else {
			result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
				Type:    "ResourceExistence",
				Name:    "Build ConfigMap exists",
				Passed:  true,
				Message: "Build ConfigMap found",
			})
		}
	}

	// Check 3: Driver configuration matches spec and inbox driver detection
	if devConfig.Spec.Driver.Version == "" {
		// Check if there's an inbox driver by scanning node labels
		nodeVersions := checkNodesForDriverVersion(ctx, k8sClient)

		if len(nodeVersions) > 0 {
			// Found inbox driver on nodes
			result.Status = "warning"

			// Collect unique versions
			versionMap := make(map[string]int)
			for _, version := range nodeVersions {
				versionMap[version]++
			}

			versionSummary := ""
			for version, count := range versionMap {
				if versionSummary != "" {
					versionSummary += ", "
				}
				versionSummary += fmt.Sprintf("%s (%d nodes)", version, count)
			}

			result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
				Type:    "InboxDriverDetection",
				Name:    "Inbox driver detected",
				Passed:  true,
				Message: fmt.Sprintf("Inbox driver versions found: %s", versionSummary),
				Details: fmt.Sprintf("Node labeler detected inbox driver on %d nodes. Consider setting spec.driver.version explicitly for consistency and to enable operator-managed driver installation", len(nodeVersions)),
			})
		} else {
			// No inbox driver found and no version specified
			result.Status = "warning"
			result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
				Type:    "ConfigurationMatch",
				Name:    "Driver version specified",
				Passed:  false,
				Message: "No driver version specified and no inbox driver detected",
				Details: "Set spec.driver.version to ensure consistent driver deployment",
			})
		}
	} else {
		// Driver version is specified - verify it matches what's deployed on nodes
		result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
			Type:    "ConfigurationMatch",
			Name:    "Driver version specified",
			Passed:  true,
			Message: fmt.Sprintf("Driver version: %s", devConfig.Spec.Driver.Version),
		})

		// Check KMM labels on nodes to verify driver version matches spec
		kmmLabelKey := fmt.Sprintf("kmm.node.kubernetes.io/version-module.%s.%s", devConfig.Namespace, devConfig.Name)
		expectedVersion := devConfig.Spec.Driver.Version

		// List all GPU nodes
		nodeList := &corev1.NodeList{}
		listOpts := []client.ListOption{
			client.MatchingLabels(map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			}),
		}

		if err := k8sClient.List(ctx, nodeList, listOpts...); err == nil && len(nodeList.Items) > 0 {
			mismatchedNodes := []string{}
			missingLabelNodes := []string{}
			matchedCount := 0

			for _, node := range nodeList.Items {
				if actualVersion, ok := node.Labels[kmmLabelKey]; ok {
					if actualVersion != expectedVersion {
						mismatchedNodes = append(mismatchedNodes, fmt.Sprintf("%s (has %s)", node.Name, actualVersion))
					} else {
						matchedCount++
					}
				} else {
					missingLabelNodes = append(missingLabelNodes, node.Name)
				}
			}

			// Report mismatches
			if len(mismatchedNodes) > 0 {
				result.Status = "degraded"
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:          "DriverVersionMatch",
					Name:          "Driver version matches spec on nodes",
					Passed:        false,
					Message:       fmt.Sprintf("Driver version mismatch on %d nodes", len(mismatchedNodes)),
					ExpectedValue: expectedVersion,
					ActualValue:   fmt.Sprintf("Mismatched nodes: %v", mismatchedNodes),
					Details:       "KMM-managed driver version on nodes does not match spec.driver.version. Driver update may be in progress or failed.",
				})
			} else if len(missingLabelNodes) > 0 && devConfig.Spec.Driver.DriverType == "container" {
				// Only warn about missing KMM labels if using container driver type
				result.Status = "warning"
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:    "DriverVersionMatch",
					Name:    "KMM driver labels present on nodes",
					Passed:  false,
					Message: fmt.Sprintf("KMM driver label missing on %d nodes", len(missingLabelNodes)),
					Details: fmt.Sprintf("Nodes without label: %v. Driver may not be installed yet or installation in progress.", missingLabelNodes),
				})
			} else if matchedCount > 0 {
				result.Checks = append(result.Checks, gpuev1alpha1.ValidationCheck{
					Type:    "DriverVersionMatch",
					Name:    "Driver version matches spec on nodes",
					Passed:  true,
					Message: fmt.Sprintf("Driver version %s confirmed on %d nodes", expectedVersion, matchedCount),
				})
			}
		}
	}

	return result
}
