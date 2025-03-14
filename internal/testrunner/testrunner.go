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

package testrunner

import (
	"fmt"
	"os"
	"path/filepath"

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
	// TODO: determine where to host the test runner image and put the registry URL here
	defaultTestRunnerImage       = "docker.io/rocm/test-runner:v1.2.0-beta.0"
	defaultInitContainerImage    = "busybox:1.36"
	TestRunnerName               = "test-runner"
	defaultSAName                = "amd-gpu-operator-test-runner"
	defaultTestRunnerDirHostPath = "/var/log/amd-test-runner"
	defaultTestRunnerMountPath   = "/var/log/amd-test-runner"
	LogDirEnv                    = "LOG_MOUNT_DIR"
)

var testRunnerLabelPair = []string{"app.kubernetes.io/name", TestRunnerName}

//go:generate mockgen -source=testrunner.go -package=testrunner -destination=mock_testrunner.go TestRunner
type TestRunner interface {
	SetTestRunnerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type testRunner struct {
	scheme *runtime.Scheme
}

func NewTestRunner(scheme *runtime.Scheme) TestRunner {
	return &testRunner{
		scheme: scheme,
	}
}

func (nl *testRunner) SetTestRunnerAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}
	trSpec := devConfig.Spec.TestRunner

	logsHostPath := defaultTestRunnerDirHostPath
	if trSpec.LogsLocation.HostPath != "" {
		logsHostPath = trSpec.LogsLocation.HostPath
	}

	logsMountPath := defaultTestRunnerMountPath
	if trSpec.LogsLocation.MountPath != "" {
		logsMountPath = trSpec.LogsLocation.MountPath
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
		{
			Name:      "test-runner-volume",
			MountPath: logsMountPath,
		},
		{
			Name:      "health",
			MountPath: "/var/lib/amd-metrics-exporter/",
		},
	}

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

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
			Name: "test-runner-volume",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: logsHostPath,
					Type: &hostPathDirectoryOrCreate,
				},
			},
		},
		{
			Name: "health",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/lib/amd-metrics-exporter/",
					Type: &hostPathDirectory,
				},
			},
		},
	}

	if trSpec.Config != nil {
		volumes = append(volumes, v1.Volume{
			Name: "test-runner-config-volume",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: *trSpec.Config,
				},
			},
		})

		containerVolumeMounts = append(containerVolumeMounts, v1.VolumeMount{
			Name:      "test-runner-config-volume",
			MountPath: "/etc/test-runner/",
		})
	}

	if len(trSpec.LogsLocation.LogsExportSecrets) > 0 {
		for _, secret := range trSpec.LogsLocation.LogsExportSecrets {
			volumes = append(volumes, v1.Volume{
				Name: secret.Name,
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: secret.Name,
					},
				},
			})
			containerVolumeMounts = append(containerVolumeMounts, v1.VolumeMount{
				Name:      secret.Name,
				MountPath: filepath.Join("/etc", "logs-export-secrets", secret.Name),
			})
		}
	}

	matchLabels := map[string]string{
		"daemonset-name":       devConfig.Name,
		testRunnerLabelPair[0]: testRunnerLabelPair[1], // in amdgpu namespace
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

	trImage := defaultTestRunnerImage
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
					Name: "NODE_NAME",
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
				{
					Name:  LogDirEnv,
					Value: logsMountPath,
				},
				{
					Name:  "TEST_TRIGGER",
					Value: "AUTO_UNHEALTHY_GPU_WATCH",
				},
			},
			Name:            TestRunnerName + "-container",
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
	initContainerImage := defaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
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
	if devConfig.Spec.TestRunner.UpgradePolicy != nil {
		up := devConfig.Spec.TestRunner.UpgradePolicy
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
	if len(devConfig.Spec.TestRunner.Tolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.TestRunner.Tolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}
	return controllerutil.SetControllerReference(devConfig, ds, nl.scheme)
}
