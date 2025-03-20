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
	"fmt"
	"strings"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

const (
	defaultOcDriversVersion = "6.2.2"
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
