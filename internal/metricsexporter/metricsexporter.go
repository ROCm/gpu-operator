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

package metricsexporter

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
	defaultMetricsExporterImage       = "docker.io/rocm/device-metrics-exporter:v1.2.0"
	defaultKubeRbacProxyImage         = "quay.io/brancz/kube-rbac-proxy:v0.18.1"
	defaultInitContainerImage         = "busybox:1.36"
	servicePort                 int32 = 5000
	nobodyUser                        = 65532
	ExporterName                      = "metrics-exporter"
	KubeRbacName                      = "kube-rbac-proxy"
	defaultSAName                     = "amd-gpu-operator-metrics-exporter"
	kubeRbacSAName                    = "amd-gpu-operator-metrics-exporter-rbac-proxy"
)

var metricsExporterLabelPair = []string{"app.kubernetes.io/name", ExporterName}

//go:generate mockgen -source=metricsexporter.go -package=metricsexporter -destination=mock_metricsexporter.go MetricsExporter
type MetricsExporter interface {
	SetMetricsExporterAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
	SetMetricsServiceAsDesired(svc *v1.Service, devConfig *amdv1alpha1.DeviceConfig) error
}

type metricsExporter struct {
	scheme *runtime.Scheme
}

func NewMetricsExporter(scheme *runtime.Scheme) MetricsExporter {
	return &metricsExporter{
		scheme: scheme,
	}
}

func (nl *metricsExporter) SetMetricsExporterAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	if ds == nil {
		return fmt.Errorf("daemon set is not initialized, zero pointer")
	}
	mSpec := devConfig.Spec.MetricsExporter
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
			Name:      "pod-resources",
			MountPath: "/var/lib/kubelet/pod-resources",
		},
		{
			Name:      "health",
			MountPath: "/var/lib/amd-metrics-exporter",
		},
		{
			Name:      "slurm",
			MountPath: "/var/run/exporter",
		},
	}

	hostPathDirectory := v1.HostPathDirectory
	healthCreateHostDirectory := v1.HostPathDirectoryOrCreate

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
			Name: "pod-resources",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/lib/kubelet/pod-resources",
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
		{
			Name: "slurm",
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/run/exporter",
					Type: &healthCreateHostDirectory,
				},
			},
		},
	}

	if mSpec.Config.Name != "" {
		volumes = append(volumes, v1.Volume{
			Name: "metrics-config-volume",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: mSpec.Config.Name,
					},
				},
			},
		})

		containerVolumeMounts = append(containerVolumeMounts, v1.VolumeMount{
			Name:      "metrics-config-volume",
			MountPath: "/etc/metrics/",
		})
	}

	matchLabels := map[string]string{
		"daemonset-name":            devConfig.Name,
		metricsExporterLabelPair[0]: metricsExporterLabelPair[1], // in amdgpu namespace
	}
	var nodeSelector map[string]string

	if mSpec.Selector != nil {
		nodeSelector = mSpec.Selector
	} else {
		nodeSelector = devConfig.Spec.Selector
	}

	// only use module ready label as node selector when KMM driver is enabled
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		nodeSelector[labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name)] = ""
	}

	mxImage := defaultMetricsExporterImage
	if mSpec.Image != "" {
		mxImage = mSpec.Image
	}

	port := servicePort
	if mSpec.Port > 0 {
		port = mSpec.Port
	}

	containers := []v1.Container{
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
				{
					Name: "METRICS_EXPORTER_PORT",
				},
			},
			Name:            ExporterName + "-container",
			WorkingDir:      "/root",
			Image:           mxImage,
			SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
			VolumeMounts:    containerVolumeMounts,
		},
	}

	if mSpec.ImagePullPolicy != "" {
		containers[0].ImagePullPolicy = v1.PullPolicy(mSpec.ImagePullPolicy)
	}

	imagePullSecrets := []v1.LocalObjectReference{}
	if mSpec.ImageRegistrySecret != nil {
		imagePullSecrets = append(imagePullSecrets, *mSpec.ImageRegistrySecret)
	}

	serviceaccount := defaultSAName

	if mSpec.RbacConfig.Enable != nil && *mSpec.RbacConfig.Enable {
		internalPort := servicePort
		if internalPort == port {
			internalPort = port - 1
		}
		// Bind service port to localhost only
		containers[0].Args = []string{"--bind=127.0.0.1:" + fmt.Sprintf("%v", int32(internalPort))}
		containers[0].Env[1].Value = fmt.Sprintf("%v", internalPort)

		kubeImage := defaultKubeRbacProxyImage
		if mSpec.RbacConfig.Image != "" {
			kubeImage = mSpec.RbacConfig.Image
		}

		args := []string{
			"--upstream=http://127.0.0.1:" + fmt.Sprintf("%v", int32(internalPort)),
			"--logtostderr=true",
			"--v=10",
		}

		volumeMounts := []v1.VolumeMount{}
		if mSpec.RbacConfig.DisableHttps != nil && *mSpec.RbacConfig.DisableHttps {
			args = append(args, "--insecure-listen-address=0.0.0.0:"+fmt.Sprintf("%v", int32(port)))
		} else {
			args = append(args, "--secure-listen-address=0.0.0.0:"+fmt.Sprintf("%v", int32(port)))

			// Load the tls-certs if provided
			if mSpec.RbacConfig.Secret != nil {
				volumes = append(volumes, v1.Volume{
					Name: "tls-certs",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName: mSpec.RbacConfig.Secret.Name,
						},
					},
				})

				volumeMounts = append(volumeMounts, v1.VolumeMount{
					Name:      "tls-certs",
					MountPath: "/etc/tls",
					ReadOnly:  true,
				})

				args = append(args, "--tls-cert-file=/etc/tls/tls.crt")
				args = append(args, "--tls-private-key-file=/etc/tls/tls.key")
			}
		}

		containers = append(containers, v1.Container{
			Name:  KubeRbacName + "-container",
			Image: kubeImage,
			SecurityContext: &v1.SecurityContext{
				RunAsUser:                ptr.To(int64(nobodyUser)),
				AllowPrivilegeEscalation: ptr.To(false),
			},
			Args:         args,
			VolumeMounts: volumeMounts,
		})

		// Provide elevated privilege only when rbac-proxy is enabled
		serviceaccount = kubeRbacSAName
	} else {
		containers[0].Env[1].Value = fmt.Sprintf("%v", port)
	}

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
	if devConfig.Spec.MetricsExporter.UpgradePolicy != nil {
		up := devConfig.Spec.MetricsExporter.UpgradePolicy
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
	if len(devConfig.Spec.MetricsExporter.Tolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = devConfig.Spec.MetricsExporter.Tolerations
	} else {
		ds.Spec.Template.Spec.Tolerations = nil
	}
	return controllerutil.SetControllerReference(devConfig, ds, nl.scheme)

}

func (nl *metricsExporter) SetMetricsServiceAsDesired(svc *v1.Service, devConfig *amdv1alpha1.DeviceConfig) error {
	mSpec := devConfig.Spec.MetricsExporter
	if svc == nil {
		return fmt.Errorf("service  is not initialized, zero pointer")
	}

	svc.Spec = v1.ServiceSpec{
		Selector: map[string]string{
			"daemonset-name":            devConfig.Name,
			metricsExporterLabelPair[0]: metricsExporterLabelPair[1],
		},
	}

	port := servicePort
	if mSpec.Port > 0 {
		port = mSpec.Port
	}

	trafficPolicyLocal := v1.ServiceInternalTrafficPolicyLocal
	svc.Spec.InternalTrafficPolicy = &trafficPolicyLocal

	switch mSpec.SvcType {
	case amdv1alpha1.ServiceTypeNodePort:
		svc.Spec.Type = v1.ServiceTypeNodePort
		svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyLocal
		svc.Spec.Ports = []v1.ServicePort{
			{
				Protocol:   v1.ProtocolTCP,
				Port:       port,
				TargetPort: intstr.FromInt32(port),
				NodePort:   mSpec.NodePort,
			},
		}
	default:
		svc.Spec.Type = v1.ServiceTypeClusterIP
		svc.Spec.Ports = []v1.ServicePort{
			{
				Protocol:   v1.ProtocolTCP,
				Port:       port,
				TargetPort: intstr.FromInt32(port),
			},
		}

	}

	return controllerutil.SetControllerReference(devConfig, svc, nl.scheme)
}
