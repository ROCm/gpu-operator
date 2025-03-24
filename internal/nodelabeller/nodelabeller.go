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

package nodelabeller

import (
	"fmt"
	"strings"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	rocmDevicePluginRepo        = "rocm/k8s-device-plugin"
	rocmUbiNodeLabellerRepo     = "rocm/k8s-node-labeller"
	defaultNodeLabellerImage    = "rocm/k8s-device-plugin:labeller-latest"
	defaultUbiNodeLabellerImage = "rocm/k8s-node-labeller:rhubi-latest"
	defaultInitContainerImage   = "busybox:1.36"
)

//go:generate mockgen -source=nodelabeller.go -package=nodelabeller -destination=mock_nodelabeller.go NodeLabeller
type NodeLabeller interface {
	SetNodeLabellerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type nodeLabeller struct {
	scheme      *runtime.Scheme
	isOpenShift bool
}

func NewNodeLabeller(scheme *runtime.Scheme, isOpenshift bool) NodeLabeller {
	return &nodeLabeller{
		scheme:      scheme,
		isOpenShift: isOpenshift,
	}
}

func (nl *nodeLabeller) SetNodeLabellerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}
	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      "dev-volume",
			MountPath: "/dev",
		},
		{
			Name:      "sys-volume",
			MountPath: "/sys",
		},
	}

	initVolumeMounts := []v1.VolumeMount{
		{
			Name:      "sys-volume",
			MountPath: "/host-sys",
		},
		{
			Name:      "etc-volume",
			MountPath: "/host-etc",
		},
	}

	hostPathDirectory := v1.HostPathDirectory

	volumes := []v1.Volume{
		{
			Name: "dev-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/dev",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: "sys-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/sys",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: "etc-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/etc",
					Type: &hostPathDirectory,
				},
			},
		},
	}

	var initContainerCommand []string

	if devConfig.Spec.Driver.Blacklist != nil && *devConfig.Spec.Driver.Blacklist {
		// if users want to apply the blacklist, init container will add the amdgpu to the blacklist
		initContainerCommand = []string{"sh", "-c", "echo \"# added by gpu operator \nblacklist amdgpu\" > /host-etc/modprobe.d/blacklist-amdgpu.conf; while [ ! -d /host-sys/class/kfd ] || [ ! -d /host-sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"}
	} else {
		// if users disabled the KMM driver, or disabled the blacklist
		// init container will remove any hanging amdgpu blacklist entry from the list
		initContainerCommand = []string{"sh", "-c", "rm -f /host-etc/modprobe.d/blacklist-amdgpu.conf; while [ ! -d /host-sys/class/kfd ] || [ ! -d /host-sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"}
	}

	initContainerImage := defaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
	}
	initContainers := []v1.Container{
		{
			Name:            "driver-init",
			Image:           initContainerImage,
			Command:         initContainerCommand,
			SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
			VolumeMounts:    initVolumeMounts,
		},
	}

	imagePullSecrets := []v1.LocalObjectReference{}
	if devConfig.Spec.DevicePlugin.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *devConfig.Spec.DevicePlugin.ImageRegistrySecret)
	}
	matchLabels := map[string]string{"daemonset-name": devConfig.Name}
	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: matchLabels,
			},
			Spec: v1.PodSpec{
				InitContainers: initContainers,
				Containers: []v1.Container{
					{
						Args:    []string{"-c", "./k8s-node-labeller -vram -cu-count -simd-count -device-id -family -product-name -driver-version"},
						Command: []string{"sh"},
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
						Name:            "node-labeller-container",
						WorkingDir:      "/root",
						Image:           nl.getNodeLabellerImage(devConfig),
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts:    containerVolumeMounts,
					},
				},
				PriorityClassName:  "system-node-critical",
				NodeSelector:       devConfig.Spec.Selector,
				ServiceAccountName: "amd-gpu-operator-node-labeller",
				Volumes:            volumes,
				ImagePullSecrets:   imagePullSecrets,
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
	if devConfig.Spec.DevicePlugin.NodeLabellerImagePullPolicy != "" {
		ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(devConfig.Spec.DevicePlugin.NodeLabellerImagePullPolicy)
	}

	if len(devConfig.Spec.DevicePlugin.NodeLabellerTolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.DevicePlugin.NodeLabellerTolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}

	return controllerutil.SetControllerReference(devConfig, ds, nl.scheme)

}

func (nl *nodeLabeller) getNodeLabellerImage(devConfig *amdv1alpha1.DeviceConfig) string {
	if devConfig.Spec.DevicePlugin.NodeLabellerImage != "" {
		// if the node labeller image is clearly specified, directly use the user provided image
		return devConfig.Spec.DevicePlugin.NodeLabellerImage
	} else if version := getDevicePluginVersion(devConfig); version != "" {
		if nl.isOpenShift {
			return rocmUbiNodeLabellerRepo + ":" + version
		}
		return rocmDevicePluginRepo + ":labeller-" + version
	}
	if nl.isOpenShift {
		return defaultUbiNodeLabellerImage
	}
	return defaultNodeLabellerImage
}

func getDevicePluginVersion(devConfig *amdv1alpha1.DeviceConfig) string {
	if strings.Contains(devConfig.Spec.DevicePlugin.DevicePluginImage, rocmDevicePluginRepo) {
		imgInfo := strings.Split(devConfig.Spec.DevicePlugin.DevicePluginImage, rocmDevicePluginRepo)
		return strings.Replace(imgInfo[len(imgInfo)-1], ":", "", -1)
	}
	return ""
}
