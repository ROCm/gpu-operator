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

package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/cmd"
)

const (
	defaultOcDriversVersion    = "6.2.2"
	openShiftNodeLabel         = "node.openshift.io/os_id"
	NodeFeatureLabelAmdGpu     = "feature.node.kubernetes.io/amd-gpu"
	NodeFeatureLabelAmdVGpu    = "feature.node.kubernetes.io/amd-vgpu"
	ResourceNamingStrategyFlag = "resource_naming_strategy"
	SingleStrategy             = "single"
	MixedStrategy              = "mixed"
)

func GetDriverVersion(node v1.Node, deviceConfig amdv1alpha1.DeviceConfig) (string, error) {
	var driverVersion string
	var err error
	if deviceConfig.Spec.Driver.Version != "" {
		driverVersion = deviceConfig.Spec.Driver.Version
	} else {
		defaultDriverVersion, err := GetDefaultDriversVersion(node)
		if err == nil {
			driverVersion = defaultDriverVersion
		}
	}
	return driverVersion, err
}

func GetDefaultDriversVersion(node v1.Node) (string, error) {
	osImageStr := strings.ToLower(node.Status.NodeInfo.OSImage)
	for os, mapper := range defaultDriverversionsMappers {
		if strings.Contains(osImageStr, os) {
			return mapper(osImageStr)
		}
	}
	return "", fmt.Errorf("OS: %s not supported", osImageStr)
}

var defaultDriverversionsMappers = map[string]func(fullImageStr string) (string, error){
	"ubuntu": UbuntuDefaultDriverVersionsMapper,
	"rhel": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
	"redhat": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
	"red hat": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
}

func UbuntuDefaultDriverVersionsMapper(fullImageStr string) (string, error) {
	if strings.Contains(fullImageStr, "20.04") {
		return "6.1.3", nil // due to a known ROCM issue, 6.2 unload + load back may cause system reboot, let's use 6.1.3 as default
	}
	if strings.Contains(fullImageStr, "22.04") {
		return "6.1.3", nil // due to a known ROCM issue, 6.2 unload + load back may cause system reboot, let's use 6.1.3 as default
	}
	if strings.Contains(fullImageStr, "24.04") {
		return "6.1.3", nil // due to a known ROCM issue, 6.2 unload + load back may cause system reboot, let's use 6.1.3 as default
	}
	return "", fmt.Errorf("invalid ubuntu version, should be one of [20.04, 22.04]")
}

func HasNodeLabelKey(node v1.Node, labelKey string) bool {
	for k := range node.Labels {
		if k == labelKey {
			return true
		}
	}
	return false
}

func IsOpenShift(logger logr.Logger) bool {
	config, err := rest.InClusterConfig()
	if err != nil {
		cmd.FatalError(logger, err, "unable to get cluster config")
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		cmd.FatalError(logger, err, "unable to create cluster clientset")
	}
	// Check for OpenShift-specific labels on nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		cmd.FatalError(logger, err, "unable to list nodes")
	}

	isOpenShift := false
	for _, node := range nodes.Items {
		if _, exists := node.Labels[openShiftNodeLabel]; exists {
			isOpenShift = true
			break
		}
	}
	logger.Info(fmt.Sprintf("IsOpenShift: %+v", isOpenShift))
	return isOpenShift
}
