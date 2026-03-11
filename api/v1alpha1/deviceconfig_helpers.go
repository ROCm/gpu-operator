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

package v1alpha1

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

const (
	DriverTypeContainer     = "container"
	DriverTypeVFPassthrough = "vf-passthrough"
	DriverTypePFPassthrough = "pf-passthrough"
)

// UseDriversSpec reports whether the new spec.Drivers field is configured.
func (spec *DeviceConfigSpec) UseDriversSpec() bool {
	return spec.Drivers != nil
}

// GetAllDriversList returns pointers to all declared drivers regardless of their Enable field.
// Falls back to spec.Driver when spec.Drivers is not configured.
func (spec *DeviceConfigSpec) GetAllDriversList() []*DriverSpec {
	if spec.UseDriversSpec() {
		ptrs := make([]*DriverSpec, len(spec.Drivers.Items))
		for i := range spec.Drivers.Items {
			ptrs[i] = &spec.Drivers.Items[i]
		}
		return ptrs
	} else if spec.Driver != nil {
		return []*DriverSpec{spec.Driver}
	}
	return nil
}

// GetEnabledDriversList returns pointers to drivers whose Enable field is nil (default) or true.
// Returned pointers reference the same elements as GetAllDriversList.
func (spec *DeviceConfigSpec) GetEnabledDriversList() []*DriverSpec {
	all := spec.GetAllDriversList()
	enabled := make([]*DriverSpec, 0, len(all))
	for _, d := range all {
		if d.Enable == nil || *d.Enable {
			enabled = append(enabled, d)
		}
	}
	return enabled
}

// GetDriverUpgradePolicy returns the active upgrade policy from spec.Drivers or the legacy spec.Driver.
func (spec *DeviceConfigSpec) GetDriverUpgradePolicy() *DriverUpgradePolicySpec {
	if spec.UseDriversSpec() {
		return spec.Drivers.UpgradePolicy
	} else if spec.Driver != nil {
		return spec.Driver.UpgradePolicy
	}
	return nil
}

// GetDriverTolerations returns the driver tolerations from spec.Drivers or the legacy spec.Driver.
func (spec *DeviceConfigSpec) GetDriverTolerations() []v1.Toleration {
	if spec.UseDriversSpec() {
		return spec.Drivers.Tolerations
	} else if spec.Driver != nil {
		return spec.Driver.Tolerations
	}
	return nil
}

// GetGpuDriver returns the first enabled driver in priority order: container, vf-passthrough, pf-passthrough.
// Callers must ensure at least one driver is enabled (enforced by validation).
func (spec *DeviceConfigSpec) GetGpuDriver() *DriverSpec {
	driverMap := DriversToMap(spec.GetEnabledDriversList())
	for _, dt := range []string{DriverTypeContainer, DriverTypeVFPassthrough, DriverTypePFPassthrough} {
		if d, ok := driverMap[dt]; ok {
			return d
		}
	}
	return nil
}

// GetAllNodeModuleStatuses returns a map of node name to ModuleStatus pointers across both storage formats.
func (dc *DeviceConfig) GetAllNodeModuleStatuses() map[string][]*ModuleStatus {
	result := make(map[string][]*ModuleStatus)
	if dc.Spec.UseDriversSpec() {
		for nodeName, statuses := range dc.Status.NodeModulesStatus {
			ptrs := make([]*ModuleStatus, len(statuses))
			for i := range statuses {
				ptrs[i] = &statuses[i]
			}
			result[nodeName] = ptrs
		}
	} else {
		for nodeName, ms := range dc.Status.NodeModuleStatus {
			if ms.DriverType == "" {
				ms.DriverType = DriverTypeContainer
			}
			result[nodeName] = []*ModuleStatus{&ms}
		}
	}
	return result
}

// GetNodeModuleStatusList returns the ModuleStatus pointer list for the given node.
func (dc *DeviceConfig) GetNodeModuleStatusList(nodeName string) []*ModuleStatus {
	return dc.GetAllNodeModuleStatuses()[nodeName]
}

// IsAnyDriverEnabled returns true if any driver in the DeviceConfig is enabled.
func (dc *DeviceConfig) IsAnyDriverEnabled() bool {
	return dc != nil && len(dc.Spec.GetEnabledDriversList()) > 0
}

// IsDriverTypeEnabled returns true if an enabled driver of the given type exists.
func (dc *DeviceConfig) IsDriverTypeEnabled(driverType string) bool {
	if dc == nil || driverType == "" {
		return false
	}
	_, ok := DriversToMap(dc.Spec.GetEnabledDriversList())[driverType]
	return ok
}

// ShouldUseKMM returns true if KMM is needed for any driver in the DeviceConfig.
func (dc *DeviceConfig) ShouldUseKMM() bool {
	if dc == nil {
		return false
	}
	for _, d := range dc.Spec.GetEnabledDriversList() {
		if d.UsesKMM() {
			return true
		}
	}
	return false
}

// KMMModuleNameForDriver returns the KMM Module CR name for the given driver type.
// Returns dc.Name for the legacy spec.Driver API, or "<name>-<type>" for spec.Drivers.
func (dc *DeviceConfig) KMMModuleNameForDriver(driverType string) string {
	if !dc.Spec.UseDriversSpec() {
		return dc.Name
	}
	return dc.Name + "-" + driverType
}

// UsesKMM returns true if the driver requires KMM (i.e. not pf-passthrough).
func (d *DriverSpec) UsesKMM() bool {
	if d == nil {
		return false
	}
	return d.DriverType != DriverTypePFPassthrough
}

// DriversToMap returns a map of DriverType to *DriverSpec for the given slice.
func DriversToMap(drivers []*DriverSpec) map[string]*DriverSpec {
	m := make(map[string]*DriverSpec, len(drivers))
	for _, d := range drivers {
		m[d.DriverType] = d
	}
	return m
}

// NodeModulesStatusToMap converts a ModuleStatus pointer slice to a map keyed by DriverType.
func NodeModulesStatusToMap(statuses []*ModuleStatus) map[string]*ModuleStatus {
	m := make(map[string]*ModuleStatus, len(statuses))
	for _, s := range statuses {
		m[s.DriverType] = s
	}
	return m
}

// AllDriverTypes returns all known driver types.
func AllDriverTypes() []string {
	return []string{DriverTypeContainer, DriverTypeVFPassthrough, DriverTypePFPassthrough}
}

// SplitKMMCRName parses a KMM module CR name into devConfigName and driverType.
// For "<devConfig>-<driverType>" both parts are returned; for single-driver names
// (module name == devConfig name) driverType is "".
func SplitKMMCRName(moduleCRName string) (devConfigName, driverType string) {
	for _, dt := range AllDriverTypes() {
		if strings.HasSuffix(moduleCRName, "-"+dt) {
			return strings.TrimSuffix(moduleCRName, "-"+dt), dt
		}
	}
	return moduleCRName, ""
}
