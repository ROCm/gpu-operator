/*
Copyright 2022.

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

package plugin

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	kmmLabels "github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
)

const (
	// check the device plugin image tags here: https://hub.docker.com/r/rocm/k8s-device-plugin/tags
	defaultDevicePluginImage    = "rocm/k8s-device-plugin:latest"
	defaultUbiDevicePluginImage = "rocm/k8s-device-plugin:rhubi-latest"
	defaultInitContainerImage   = "busybox:1.36"

	// check the DRA driver image tags here: https://hub.docker.com/r/rocm/k8s-gpu-dra-driver/tags
	defaultDRADriverImage = "rocm/k8s-gpu-dra-driver:latest"
)

//go:generate mockgen -source=plugin.go -package=plugin -destination=mock_plugin.go DevicePluginAPI
type DevicePluginAPI interface {
	SetDevicePluginAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
	SetDRADriverAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type devicePlugin struct {
	client      client.Client
	scheme      *runtime.Scheme
	isOpenShift bool
}

func NewDevicePlugin(client client.Client, scheme *runtime.Scheme, isOpenShift bool) DevicePluginAPI {
	return &devicePlugin{
		client:      client,
		scheme:      scheme,
		isOpenShift: isOpenShift,
	}
}

func (dp *devicePlugin) SetDevicePluginAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	var devicePluginImage string

	if devConfig.Spec.DevicePlugin.DevicePluginImage == "" {
		if dp.isOpenShift {
			devicePluginImage = defaultUbiDevicePluginImage
		} else {
			devicePluginImage = defaultDevicePluginImage
		}
	} else {
		devicePluginImage = devConfig.Spec.DevicePlugin.DevicePluginImage
	}
	hostPathDirectory := v1.HostPathDirectory
	healthCreateHostDirectory := v1.HostPathDirectoryOrCreate

	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}

	// Use configurable kubelet device plugins path, default to standard path
	kubeletDevicePluginsDir := utils.KubeletDevicePluginsPath
	if devConfig.Spec.DevicePlugin.KubeletSocketPath != "" {
		kubeletDevicePluginsDir = devConfig.Spec.DevicePlugin.KubeletSocketPath
	}

	commandArgs := "./k8s-device-plugin -logtostderr=true -stderrthreshold=INFO -v=5 -pulse=30"

	devicePluginArguments := devConfig.Spec.DevicePlugin.DevicePluginArguments

	// Default resource_naming_strategy to "mixed" for PF and VF passthrough if not set
	if _, exists := devicePluginArguments["resource_naming_strategy"]; !exists {
		if devConfig.Spec.Driver.DriverType == utils.DriverTypePFPassthrough ||
			devConfig.Spec.Driver.DriverType == utils.DriverTypeVFPassthrough {
			if devicePluginArguments == nil {
				devicePluginArguments = make(map[string]string)
			}
			devicePluginArguments["resource_naming_strategy"] = "mixed"
		}
	}

	for key, val := range devicePluginArguments {
		commandArgs += " -" + key + "=" + val
	}

	command := []string{"sh", "-c", commandArgs}

	nodeSelector := map[string]string{}
	for key, val := range devConfig.Spec.Selector {
		nodeSelector[key] = val
	}
	if utils.ShouldUseKMM(devConfig) {
		nodeSelector[kmmLabels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name)] = ""
	}
	imagePullSecrets := []v1.LocalObjectReference{}
	if devConfig.Spec.DevicePlugin.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *devConfig.Spec.DevicePlugin.ImageRegistrySecret)
	}

	matchLabels := map[string]string{"daemonset-name": devConfig.Name}
	initContainerImage := defaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
	}

	initContainerCommand := "while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"
	switch devConfig.Spec.Driver.DriverType {
	case utils.DriverTypeVFPassthrough:
		initContainerCommand = "while [ ! -d /sys/module/gim/drivers/ ]; do echo \"gim driver is not loaded \"; sleep 2 ;done"
	case utils.DriverTypePFPassthrough:
		initContainerCommand = "true"
	}

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: matchLabels,
			},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{
					{
						Name:            "driver-init",
						Image:           initContainerImage,
						Command:         []string{"sh", "-c", initContainerCommand},
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "sys",
								MountPath: "/sys",
							},
						},
					},
				},
				Containers: []v1.Container{
					{

						Env: []v1.EnvVar{
							{
								Name: "DS_NODE_NAME",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: "spec.nodeName",
									},
								},
							},
						},
						Name:            "device-plugin",
						WorkingDir:      "/root",
						Command:         command,
						Image:           devicePluginImage,
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "kubelet-device-plugins",
								MountPath: utils.KubeletDevicePluginsPath,
							},
							{
								Name:      "sys",
								MountPath: "/sys",
							},
							{
								Name:      "health",
								MountPath: "/var/lib/amd-metrics-exporter",
							},
						},
					},
				},
				ImagePullSecrets:   imagePullSecrets,
				PriorityClassName:  "system-node-critical",
				NodeSelector:       nodeSelector,
				ServiceAccountName: "amd-gpu-operator-kmm-device-plugin",
				Volumes: []v1.Volume{
					{
						Name: "kubelet-device-plugins",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: kubeletDevicePluginsDir,
								Type: &hostPathDirectory,
							},
						},
					},
					{
						Name: "sys",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/sys",
								Type: &hostPathDirectory,
							},
						},
					},
					{
						Name: "health",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/var/lib/amd-metrics-exporter",
								Type: &healthCreateHostDirectory,
							},
						},
					},
				},
			},
		},
	}
	if devConfig.Spec.DevicePlugin.UpgradePolicy != nil {
		up := devConfig.Spec.DevicePlugin.UpgradePolicy
		upgradeStrategy := appsv1.RollingUpdateDaemonSetStrategyType
		if up.UpgradeStrategy == "OnDelete" {
			upgradeStrategy = appsv1.OnDeleteDaemonSetStrategyType
		}
		ds.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
			Type: upgradeStrategy,
		}
		if upgradeStrategy == appsv1.RollingUpdateDaemonSetStrategyType {
			ds.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateDaemonSet{
				MaxUnavailable: &intstr.IntOrString{IntVal: int32(up.MaxUnavailable)},
			}
		}
	}
	if devConfig.Spec.DevicePlugin.DevicePluginImagePullPolicy != "" {
		ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(devConfig.Spec.DevicePlugin.DevicePluginImagePullPolicy)
	}
	if len(devConfig.Spec.DevicePlugin.DevicePluginTolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.DevicePlugin.DevicePluginTolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}
	return controllerutil.SetControllerReference(devConfig, ds, dp.scheme)
}

func (dp *devicePlugin) SetDRADriverAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}

	image := devConfig.Spec.DRADriver.Image
	if image == "" {
		image = defaultDRADriverImage
	}

	// Prepare arguments
	var args []string
	if devConfig.Spec.DRADriver.CmdLineArguments != nil {
		for key, val := range devConfig.Spec.DRADriver.CmdLineArguments {
			args = append(args, fmt.Sprintf("-%s=%s", key, val))
		}
		sort.Strings(args)
	}

	nodeSelector := map[string]string{}
	if devConfig.Spec.DRADriver.Selector != nil {
		for key, val := range devConfig.Spec.DRADriver.Selector {
			nodeSelector[key] = val
		}
	} else if devConfig.Spec.Selector != nil {
		for key, val := range devConfig.Spec.Selector {
			nodeSelector[key] = val
		}
	}
	if utils.ShouldUseKMM(devConfig) {
		nodeSelector[kmmLabels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name)] = ""
	}

	imagePullSecrets := []v1.LocalObjectReference{}
	if devConfig.Spec.DRADriver.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *devConfig.Spec.DRADriver.ImageRegistrySecret)
	}

	matchLabels := map[string]string{"daemonset-name": devConfig.Name + utils.DRADriverNameSuffix}

	initContainerImage := defaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
	}

	initContainerCommand := "while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: matchLabels,
			},
			Spec: v1.PodSpec{
				InitContainers: []v1.Container{
					{
						Name:            "driver-init",
						Image:           initContainerImage,
						Command:         []string{"sh", "-c", initContainerCommand},
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "sys",
								MountPath: "/sys",
							},
						},
					},
				},
				Containers: []v1.Container{
					{
						Name:    "plugin",
						Image:   image,
						Command: []string{"gpu-kubeletplugin"},
						Args:    args,
						Env: []v1.EnvVar{
							{
								Name:  "CDI_ROOT",
								Value: "/var/run/cdi",
							},
							{
								Name:  "KUBELET_REGISTRAR_DIRECTORY_PATH",
								Value: "/var/lib/kubelet/plugins_registry",
							},
							{
								Name:  "KUBELET_PLUGINS_DIRECTORY_PATH",
								Value: "/var/lib/kubelet/plugins",
							},
							{
								Name: "NODE_NAME",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: "spec.nodeName",
									},
								},
							},
							{
								Name: "NAMESPACE",
								ValueFrom: &v1.EnvVarSource{
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: "metadata.namespace",
									},
								},
							},
						},
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "plugins-registry",
								MountPath: "/var/lib/kubelet/plugins_registry",
							},
							{
								Name:      "plugins",
								MountPath: "/var/lib/kubelet/plugins",
							},
							{
								Name:      "cdi",
								MountPath: "/var/run/cdi",
							},
							{
								Name:      "dev",
								MountPath: "/dev",
							},
							{
								Name:      "sys",
								MountPath: "/sys",
							},
						},
					},
				},
				ImagePullSecrets:   imagePullSecrets,
				PriorityClassName:  "system-node-critical",
				NodeSelector:       nodeSelector,
				ServiceAccountName: "amd-gpu-operator-dra-driver",
				Volumes: []v1.Volume{
					{
						Name: "plugins-registry",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/var/lib/kubelet/plugins_registry",
							},
						},
					},
					{
						Name: "plugins",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/var/lib/kubelet/plugins",
							},
						},
					},
					{
						Name: "cdi",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/var/run/cdi",
							},
						},
					},
					{
						Name: "dev",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/dev",
							},
						},
					},
					{
						Name: "sys",
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: "/sys",
							},
						},
					},
				},
			},
		},
	}
	if devConfig.Spec.DRADriver.UpgradePolicy != nil {
		up := devConfig.Spec.DRADriver.UpgradePolicy
		upgradeStrategy := appsv1.RollingUpdateDaemonSetStrategyType
		if up.UpgradeStrategy == "OnDelete" {
			upgradeStrategy = appsv1.OnDeleteDaemonSetStrategyType
		}
		ds.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
			Type: upgradeStrategy,
		}
		if upgradeStrategy == appsv1.RollingUpdateDaemonSetStrategyType {
			ds.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateDaemonSet{
				MaxUnavailable: &intstr.IntOrString{IntVal: int32(up.MaxUnavailable)},
			}
		}
	}
	if devConfig.Spec.DRADriver.ImagePullPolicy != "" {
		ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(devConfig.Spec.DRADriver.ImagePullPolicy)
	}
	if len(devConfig.Spec.DRADriver.Tolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.DRADriver.Tolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}
	return controllerutil.SetControllerReference(devConfig, ds, dp.scheme)
}
