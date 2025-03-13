/*
Copyright 2024.

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

package configmanager

import (
	"fmt"
	"os"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// TODO: determine where to host the config manager image and put the registry URL here
	defaultConfigManagerImage = "docker.io/rocm/device-config-manager:v1.3.0"
	ConfigManagerName         = "device-config-manager"
	defaultSAName             = "amd-gpu-operator-config-manager"
)

var configManagerLabelPair = []string{"app.kubernetes.io/name", ConfigManagerName}

//go:generate mockgen -source=configmanager.go -package=configmanager -destination=mock_configmanager.go ConfigManager
type ConfigManager interface {
	SetConfigManagerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type configManager struct {
	scheme *runtime.Scheme
}

func NewConfigManager(scheme *runtime.Scheme) ConfigManager {
	return &configManager{
		scheme: scheme,
	}
}

func (nl *configManager) SetConfigManagerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}
	trSpec := devConfig.Spec.ConfigManager
	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      "dev-volume",
			MountPath: "/dev",
		},
		{
			Name:      "sys-volume",
			MountPath: "/sys",
		},
		{
			Name:      "libmodules",
			MountPath: "/lib/modules",
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
			Name: "health",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/lib/",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: "libmodules",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/lib/modules/",
					Type: &hostPathDirectory,
				},
			},
		},
	}

	if trSpec.Config != nil {
		volumes = append(volumes, v1.Volume{
			Name: "config-manager-config-volume",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: *trSpec.Config,
				},
			},
		})

		containerVolumeMounts = append(containerVolumeMounts, v1.VolumeMount{
			Name:      "config-manager-config-volume",
			MountPath: "/etc/config-manager/",
		})
	}

	matchLabels := map[string]string{
		"daemonset-name":          devConfig.Name,
		configManagerLabelPair[0]: configManagerLabelPair[1], // in amdgpu namespace
	}
	var nodeSelector map[string]string

	if trSpec.Selector != nil {
		nodeSelector = trSpec.Selector
	} else {
		nodeSelector = devConfig.Spec.Selector
	}

	// only use module ready label as node selector when KMM driver is enabled
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		nodeSelector[labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name)] = ""
	}

	trImage := defaultConfigManagerImage
	if trSpec.Image != "" {
		trImage = trSpec.Image
	}

	containers := []v1.Container{
		{
			Env: []v1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name: "POD_NAMESPACE",
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "DS_NODE_NAME",
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
			},
			Name:            ConfigManagerName + "-container",
			Image:           trImage,
			SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
			VolumeMounts:    containerVolumeMounts,
		},
	}

	if trSpec.ImagePullPolicy != "" {
		containers[0].ImagePullPolicy = v1.PullPolicy(trSpec.ImagePullPolicy)
	}

	imagePullSecrets := []v1.LocalObjectReference{}
	if trSpec.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *trSpec.ImageRegistrySecret)
	}

	serviceaccount := defaultSAName
	gracePeriod := int64(1)
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
						Image:           "busybox:1.36",
						Command:         []string{"sh", "-c", "if [ \"$SIM_ENABLE\" = \"true\" ]; then exit 0; fi; while [ ! -d /host-sys/class/kfd ] || [ ! -d /host-sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"},
						SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "sys-volume",
								MountPath: "/host-sys",
							},
						},
						Env: []v1.EnvVar{
							{
								Name:  "SIM_ENABLE",
								Value: os.Getenv("SIM_ENABLE"),
							},
						},
					},
				},
				Containers:                    containers,
				PriorityClassName:             "system-node-critical",
				NodeSelector:                  nodeSelector,
				ServiceAccountName:            serviceaccount,
				Volumes:                       volumes,
				ImagePullSecrets:              imagePullSecrets,
				TerminationGracePeriodSeconds: &gracePeriod,
			},
		},
	}
	if devConfig.Spec.ConfigManager.UpgradePolicy != nil {
		up := devConfig.Spec.ConfigManager.UpgradePolicy
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

	if len(devConfig.Spec.ConfigManager.ConfigManagerTolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.ConfigManager.ConfigManagerTolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}
	return controllerutil.SetControllerReference(devConfig, ds, nl.scheme)
}
