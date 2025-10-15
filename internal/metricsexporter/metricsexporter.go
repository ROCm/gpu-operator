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
	"encoding/json"
	"fmt"
	"os"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

const (
	defaultMetricsExporterImage       = "docker.io/rocm/device-metrics-exporter:v1.4.0"
	defaultKubeRbacProxyImage         = "quay.io/brancz/kube-rbac-proxy:v0.18.1"
	defaultInitContainerImage         = "busybox:1.36"
	servicePort                 int32 = 5000
	nobodyUser                        = 65532
	ExporterName                      = "metrics-exporter"
	KubeRbacName                      = "kube-rbac-proxy"
	StaticAuthSecretName              = ExporterName + "-static-auth-config"
	defaultSAName                     = "amd-gpu-operator-metrics-exporter"
	kubeRbacSAName                    = "amd-gpu-operator-metrics-exporter-rbac-proxy"
	svcLabel                          = "app.kubernetes.io/service"
	defaultKubeleteDir                = "/var/lib/kubelet/pod-resources"
)

var serviceMonitorLabelPair = []string{"app", "amd-device-metrics-exporter"}
var metricsExporterLabelPair = []string{"app.kubernetes.io/name", ExporterName}

//go:generate mockgen -source=metricsexporter.go -package=metricsexporter -destination=mock_metricsexporter.go MetricsExporter
type MetricsExporter interface {
	SetMetricsExporterAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
	SetMetricsServiceAsDesired(svc *v1.Service, devConfig *amdv1alpha1.DeviceConfig) error
	SetStaticAuthSecretAsDesired(secret *v1.Secret, devConfig *amdv1alpha1.DeviceConfig) error
	SetServiceMonitorAsDesired(sm *monitoringv1.ServiceMonitor, devConfig *amdv1alpha1.DeviceConfig) error
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

	kubeletDir := defaultKubeleteDir
	if mSpec.PodResourceAPISocketPath != "" {
		kubeletDir = mSpec.PodResourceAPISocketPath
	}

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
					Path: kubeletDir,
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

	nodeSelector := map[string]string{}
	if mSpec.Selector != nil {
		for k, v := range mSpec.Selector {
			nodeSelector[k] = v
		}
	} else if devConfig.Spec.Selector != nil {
		for k, v := range devConfig.Spec.Selector {
			nodeSelector[k] = v
		}
	}

	// only use module ready label as node selector when KMM driver is enabled
	if utils.ShouldUseKMM(devConfig) {
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
				{
					Name:  "AMD_EXPORTER_RELAXED_FLAGS_PARSING",
					Value: "true",
				},
			},
			Name:            ExporterName + "-container",
			WorkingDir:      "/root",
			Image:           mxImage,
			SecurityContext: &v1.SecurityContext{Privileged: ptr.To(true)},
			VolumeMounts:    containerVolumeMounts,
		},
	}

	// Set resource limits if configured
	if mSpec.Resource != nil {
		containers[0].Resources = *mSpec.Resource
	} else {
		// Set default resource limits
		containers[0].Resources = v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("4G"),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("500m"),
				v1.ResourceMemory: resource.MustParse("512M"),
			},
		}
	}

	// set annotations for metrics exporter
	podAnnotations := map[string]string{}
	if mSpec.PodAnnotations != nil {
		podAnnotations = mSpec.PodAnnotations
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
		// Bind service port to localhost only, don't expose port in ContainerPort
		containers[0].Args = []string{"--bind=127.0.0.1"}
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

		// Add client CA config map mount for mTLS if specified
		if mSpec.RbacConfig.ClientCAConfigMap != nil {
			volumes = append(volumes, v1.Volume{
				Name: "client-ca",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: *mSpec.RbacConfig.ClientCAConfigMap,
					},
				},
			})
			volumeMounts = append(volumeMounts, v1.VolumeMount{
				Name:      "client-ca",
				MountPath: "/etc/kube-rbac-proxy/ca",
				ReadOnly:  true,
			})
			args = append(args, "--client-ca-file=/etc/kube-rbac-proxy/ca/ca.crt")
		}

		// Create and mount static authorization config if enabled
		if mSpec.RbacConfig.StaticAuthorization != nil && mSpec.RbacConfig.StaticAuthorization.Enable {

			// Mount the static auth config secret
			staticAuthSecretName := devConfig.Name + "-" + StaticAuthSecretName

			// Add volume and mount for static auth config
			volumes = append(volumes, v1.Volume{
				Name: "static-auth-config",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: staticAuthSecretName,
					},
				},
			})
			volumeMounts = append(volumeMounts, v1.VolumeMount{
				Name:      "static-auth-config",
				MountPath: "/etc/kube-rbac-proxy",
				ReadOnly:  true,
			})
			args = append(args, "--config-file=/etc/kube-rbac-proxy/config.yaml")
		}

		// Continue with existing TLS cert handling
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
			Ports: []v1.ContainerPort{
				{
					Name:          "exporter-port",
					Protocol:      v1.ProtocolTCP,
					ContainerPort: port,
				},
			},
		})

		// Provide elevated privilege only when rbac-proxy is enabled
		serviceaccount = kubeRbacSAName
	} else {
		containers[0].Env[1].Value = fmt.Sprintf("%v", port)
		containers[0].Ports = []v1.ContainerPort{
			{
				Name:          "exporter-port",
				Protocol:      v1.ProtocolTCP,
				ContainerPort: port,
			},
		}
	}

	gracePeriod := int64(1)
	initContainerImage := defaultInitContainerImage
	if devConfig.Spec.CommonConfig.InitContainerImage != "" {
		initContainerImage = devConfig.Spec.CommonConfig.InitContainerImage
	}

	initContainerCommand := "if [ \"$SIM_ENABLE\" = \"true\" ]; then exit 0; fi; while [ ! -d /host-sys/class/kfd ] || [ ! -d /host-sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"
	switch devConfig.Spec.Driver.DriverType {
	case utils.DriverTypeVFPassthrough:
		initContainerCommand = "if [ \"$SIM_ENABLE\" = \"true\" ]; then exit 0; fi; while [ ! -d /host-sys/module/gim/drivers/ ]; do echo \"gim driver is not loaded \"; sleep 2 ;done"
	case utils.DriverTypePFPassthrough:
		initContainerCommand = "true"
	}

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: matchLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      matchLabels,
				Annotations: podAnnotations,
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

	// Add app label for ServiceMonitor selection
	if svc.Labels == nil {
		svc.Labels = make(map[string]string)
	}
	svc.Labels[svcLabel] = devConfig.Name + "-" + ExporterName

	svc.Spec = v1.ServiceSpec{
		Selector: map[string]string{
			"daemonset-name":            devConfig.Name,
			metricsExporterLabelPair[0]: metricsExporterLabelPair[1],
		},
	}

	// set annotations for metrics exporter
	serviceAnnocations := map[string]string{}
	if mSpec.ServiceAnnotations != nil {
		serviceAnnocations = mSpec.ServiceAnnotations
	}
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}
	svc.Annotations = serviceAnnocations

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
				Name:       "exporter-port",
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
				Name:       "exporter-port",
				Protocol:   v1.ProtocolTCP,
				Port:       port,
				TargetPort: intstr.FromInt32(port),
			},
		}

	}

	return controllerutil.SetControllerReference(devConfig, svc, nl.scheme)
}

// SetServiceMonitorAsDesired configures the ServiceMonitor resource for Prometheus integration
// Ignoring staticcheck linter for this function. SA1019: we intentionally use BearerTokenFile, a deprecated field for compatibility
//
//nolint:staticcheck
func (nl *metricsExporter) SetServiceMonitorAsDesired(sm *monitoringv1.ServiceMonitor, devConfig *amdv1alpha1.DeviceConfig) error {
	if sm == nil {
		return fmt.Errorf("ServiceMonitor is not initialized, zero pointer")
	}

	// Skip configuration if Prometheus ServiceMonitor is not enabled
	if !utils.IsPrometheusServiceMonitorEnable(devConfig) {
		return nil
	}

	// Configure app label selector for the service
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			svcLabel: devConfig.Name + "-" + ExporterName,
		},
	}

	port := servicePort
	if devConfig.Spec.MetricsExporter.Port > 0 {
		port = devConfig.Spec.MetricsExporter.Port
	}

	// Set up the endpoint
	endpoints := []monitoringv1.Endpoint{
		{
			Port:                 "exporter-port",
			TargetPort:           &intstr.IntOrString{Type: intstr.Int, IntVal: port},
			RelabelConfigs:       []monitoringv1.RelabelConfig{},
			MetricRelabelConfigs: []monitoringv1.RelabelConfig{},
		},
	}

	// Apply custom interval if specified
	if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Interval != "" {
		endpoints[0].Interval = monitoringv1.Duration(devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Interval)
	}

	// Apply honorLabels if specified
	if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.HonorLabels != nil {
		endpoints[0].HonorLabels = *devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.HonorLabels
	} else {
		endpoints[0].HonorLabels = false
	}

	// Apply honorTimestamps if specified
	if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.HonorTimestamps != nil {
		endpoints[0].HonorTimestamps = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.HonorTimestamps
	}

	// Apply relabelings if specified
	if len(devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Relabelings) > 0 {
		endpoints[0].RelabelConfigs = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Relabelings
	}

	// Apply metricRelabelings if specified
	if len(devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.MetricRelabelings) > 0 {
		endpoints[0].MetricRelabelConfigs = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.MetricRelabelings
	}

	// Default scheme to http
	endpoints[0].Scheme = "http"

	// Use HTTPS when RBAC is enabled and HTTPS is not explicitly disabled
	if devConfig.Spec.MetricsExporter.RbacConfig.Enable != nil &&
		*devConfig.Spec.MetricsExporter.RbacConfig.Enable {
		// If DisableHttps is nil or false, use HTTPS
		if devConfig.Spec.MetricsExporter.RbacConfig.DisableHttps == nil ||
			!*devConfig.Spec.MetricsExporter.RbacConfig.DisableHttps {
			endpoints[0].Scheme = "https"
		}
		// Set TLS config for HTTPS
		if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.TLSConfig != nil {
			endpoints[0].TLSConfig = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.TLSConfig
		}

		// Set bearer token file for RBAC proxy
		if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.BearerTokenFile != "" {
			endpoints[0].BearerTokenFile = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.BearerTokenFile
		}

		// Set Authorization if specified
		if devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Authorization != nil {
			endpoints[0].Authorization = devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Authorization
		}
	}

	// Configure ServiceMonitor
	sm.Spec = monitoringv1.ServiceMonitorSpec{
		Selector:          labelSelector,
		Endpoints:         endpoints,
		NamespaceSelector: monitoringv1.NamespaceSelector{MatchNames: []string{devConfig.Namespace}},
		AttachMetadata:    devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.AttachMetadata,
	}

	// Set custom labels
	sm.Labels = map[string]string{
		serviceMonitorLabelPair[0]: serviceMonitorLabelPair[1],
	}

	if len(devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Labels) > 0 {
		// Use custom labels from the CRD
		for k, v := range devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Labels {
			sm.Labels[k] = v
		}
	}

	return controllerutil.SetControllerReference(devConfig, sm, nl.scheme)
}

// SetStaticAuthSecretAsDesired creates a secret containing the kube-rbac-proxy static authorization config
func (nl *metricsExporter) SetStaticAuthSecretAsDesired(secret *v1.Secret, devConfig *amdv1alpha1.DeviceConfig) error {
	if secret == nil {
		return fmt.Errorf("secret is not initialized, zero pointer")
	}

	mSpec := devConfig.Spec.MetricsExporter
	if mSpec.RbacConfig.StaticAuthorization == nil || !mSpec.RbacConfig.StaticAuthorization.Enable {
		return nil
	}

	staticAuthConfig := map[string]interface{}{
		"authorization": map[string]interface{}{
			"static": []map[string]interface{}{
				{
					"path":            "/metrics",
					"resourceRequest": false,
					"user": map[string]string{
						"name": mSpec.RbacConfig.StaticAuthorization.ClientName,
					},
					"verb": "get",
				},
			},
		},
	}

	staticAuthConfigJSON, err := json.Marshal(staticAuthConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal static auth config: %v", err)
	}

	secret.StringData = map[string]string{
		"config.yaml": string(staticAuthConfigJSON),
	}

	return controllerutil.SetControllerReference(devConfig, secret, nl.scheme)
}
