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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DeviceConfigSpec describes how the AMD GPU operator should enable AMD GPU device for customer's use.
type DeviceConfigSpec struct {
	// driver
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Driver",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:driver"}
	// +optional
	Driver DriverSpec `json:"driver,omitempty"`

	// metrics exporter
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="MetricsExporter",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:metricsExporter"}
	// +optional
	MetricsExporter MetricsExporterSpec `json:"metricsExporter,omitempty"`

	// config manager
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ConfigManager",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:configManager"}
	// +optional
	ConfigManager ConfigManagerSpec `json:"configManager,omitempty"`

	// device plugin
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePlugin",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePlugin"}
	// +optional
	DevicePlugin DevicePluginSpec `json:"devicePlugin,omitempty"`

	// test runner
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TestRunner",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:testRunner"}
	// +optional
	TestRunner TestRunnerSpec `json:"testRunner,omitempty"`

	// common config
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CommonConfig",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:commonConfig"}
	// +optional
	CommonConfig CommonConfigSpec `json:"commonConfig,omitempty"`

	// Selector describes on which nodes the GPU Operator should enable the GPU device.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:selector"}
	// +optional
	Selector map[string]string `json:"selector,omitempty"`
}

type RegistryTLS struct {
	// If true, check if the container image already exists using plain HTTP.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Insecure",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:insecure"}
	// +optional
	Insecure *bool `json:"insecure,omitempty"`
	// If true, skip any TLS server certificate validation
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="InsecureSkipTLSVerify",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:insecureSkipTLSVerify"}
	// +optional
	InsecureSkipTLSVerify *bool `json:"insecureSkipTLSVerify,omitempty"`
}

type DriverSpec struct {
	// enable driver install. default value is true.
	// disable is for skipping driver install/uninstall for dryrun or using in-tree amdgpu kernel module
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// blacklist amdgpu drivers on the host
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="BlacklistDrivers",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:blacklistDrivers"}
	Blacklist *bool `json:"blacklist,omitempty"`

	// radeon repo URL for fetching amdgpu installer if building driver image on the fly
	// installer URL is https://repo.radeon.com/amdgpu-install by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="AMDGPUInstallerRepoURL",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:amdgpuInstallerRepoURL"}
	// +optional
	AMDGPUInstallerRepoURL string `json:"amdgpuInstallerRepoURL,omitempty"`

	// version of the drivers source code, can be used as part of image of dockerfile source image
	// default value for different OS is: ubuntu: 6.1.3, coreOS: 6.2.2
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Version",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:version"}
	// +optional
	Version string `json:"version,omitempty"`

	// defines image that includes drivers and firmware blobs, don't include tag since it will be fully managed by operator
	// for vanilla k8s the default value is image-registry:5000/$MOD_NAMESPACE/amdgpu_kmod
	// for OpenShift the default value is image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amdgpu_kmod
	// image tag will be in the format of <linux distro>-<release version>-<kernel version>-<driver version>
	// example tag is coreos-416.94-5.14.0-427.28.1.el9_4.x86_64-6.2.2 and ubuntu-22.04-5.15.0-94-generic-6.1.3
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[$a-zA-Z0-9_]+(?:[._-][$a-zA-Z0-9_]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// driver image registry TLS setting for the container image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistryTLS",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistryTLS"}
	// +optional
	ImageRegistryTLS RegistryTLS `json:"imageRegistryTLS,omitempty"`

	// secrets used for pull/push images from/to private registry specified in driversImage
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// image signing config to sign the driver image when building driver image on the fly
	// image signing is required for installing driver on secure boot enabled system
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageSign",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageSign"}
	// +optional
	ImageSign ImageSignSpec `json:"imageSign,omitempty"`

	// policy to upgrade the drivers
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradePolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradePolicy"}
	// +optional
	UpgradePolicy *DriverUpgradePolicySpec `json:"upgradePolicy,omitempty"`
}

// UpgradeState captures the state of the upgrade process on a node
// +enum
type UpgradeState string

const (
	// No State.
	UpgradeStateEmpty UpgradeState = ""
	// Node upgrade pending
	UpgradeStateNotStarted UpgradeState = "Upgrade-Not-Started"
	// Node pre-upgrade ops
	UpgradeStateStarted UpgradeState = "Upgrade-Started"
	// Node install in progress
	UpgradeStateInstallInProgress UpgradeState = "Install-In-Progress"
	// Node install complete
	UpgradeStateInstallComplete UpgradeState = "Install-Complete"
	// Node upgrade in progress
	UpgradeStateInProgress UpgradeState = "Upgrade-In-Progress"
	// Node upgrade complete
	UpgradeStateComplete UpgradeState = "Upgrade-Complete"
	// Node upgrade failed
	UpgradeStateFailed UpgradeState = "Upgrade-Failed"
	// Node cordon failed
	UpgradeStateCordonFailed UpgradeState = "Cordon-Failed"
	// Node uncordon failed
	UpgradeStateUncordonFailed UpgradeState = "Uncordon-Failed"
	// Node drain failed
	UpgradeStateDrainFailed UpgradeState = "Drain-Failed"
	// Node reboot in progress
	UpgradeStateRebootInProgress UpgradeState = "Reboot-In-Progress"
	// Node reboot failed
	UpgradeStateRebootFailed UpgradeState = "Reboot-Failed"
)

type DriverUpgradePolicySpec struct {
	// enable upgrade policy, disabled by default
	// If disabled, user has to manually upgrade all the nodes.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +optional
	Enable *bool `json:"enable,omitempty"`
	// MaxParallelUpgrades indicates how many nodes can be upgraded in parallel
	// 0 means no limit, all nodes will be upgraded in parallel
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="MaxParallelUpgrades",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:maxParallelUpgrades"}
	// +optional
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=0
	MaxParallelUpgrades int `json:"maxParallelUpgrades,omitempty"`
	// MaxUnavailableNodes indicates maximum number of nodes that can be in a failed upgrade state beyond which upgrades will stop to keep cluster at a minimal healthy state
	// Value can be an integer (ex: 2) which would mean atmost 2 nodes can be in failed state after which new upgrades will not start. Or it can be a percentage string(ex: "50%") from which absolute number will be calculated and round up
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="MaxUnavailableNodes",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:maxUnavailableNodes"}
	// +optional
	// +kubebuilder:default:="25%"
	MaxUnavailableNodes intstr.IntOrString `json:"maxUnavailableNodes,omitempty"`
	// Node draining policy
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeDrainPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:nodeDrainPolicy"}
	// +optional
	NodeDrainPolicy *DrainSpec `json:"nodeDrainPolicy,omitempty"`
	// Pod Deletion policy. If both NodeDrainPolicy and PodDeletionPolicy config is available, NodeDrainPolicy(if enabled) will take precedence.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="PodDeletionPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:podDeletionPolicy"}
	// +optional
	PodDeletionPolicy *PodDeletionSpec `json:"podDeletionPolicy,omitempty"`
	// reboot between driver upgrades, enabled by default, if enabled spec.commonConfig.utilsContainer will be used to perform reboot on worker nodes
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="RebootRequired",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:rebootRequired"}
	// +optional
	// +kubebuilder:default:=true
	RebootRequired *bool `json:"rebootRequired,omitempty"`
}

type DrainSpec struct {
	// Force indicates if force draining is allowed
	// +optional
	// +kubebuilder:default:=false
	Force *bool `json:"force,omitempty"`
	// TimeoutSecond specifies the length of time in seconds to wait before giving up drain, zero means infinite
	// +optional
	// +kubebuilder:default:=300
	// +kubebuilder:validation:Minimum:=0
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type PodDeletionSpec struct {
	// Force indicates if force deletion is allowed
	// +optional
	// +kubebuilder:default:=false
	Force *bool `json:"force,omitempty"`
	// TimeoutSecond specifies the length of time in seconds to wait before giving up on pod deletion, zero means infinite
	// +optional
	// +kubebuilder:default:=300
	// +kubebuilder:validation:Minimum:=0
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type DevicePluginSpec struct {
	// device plugin image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePluginImage",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePluginImage"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	DevicePluginImage string `json:"devicePluginImage,omitempty"`

	// image pull policy for device plugin
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePluginImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:DevicePluginImagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	DevicePluginImagePullPolicy string `json:"devicePluginImagePullPolicy,omitempty"`

	// tolerations for the device plugin DaemonSet
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePluginTolerations",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePluginTolerations"}
	// +optional
	DevicePluginTolerations []v1.Toleration `json:"devicePluginTolerations,omitempty"`

	// device plugin arguments is used to pass supported flags and their values while starting device plugin daemonset
	// supported flag values: {"resource_naming_strategy": {"single", "mixed"}}
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePluginArguments",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePluginArguments"}
	// +optional
	DevicePluginArguments map[string]string `json:"devicePluginArguments,omitempty"`

	// node labeller image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeLabellerImage",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:nodeLabellerImage"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	NodeLabellerImage string `json:"nodeLabellerImage,omitempty"`

	// image pull policy for node labeller
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeLabellerImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:NodeLabellerImagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	NodeLabellerImagePullPolicy string `json:"nodeLabellerImagePullPolicy,omitempty"`

	// tolerations for the node labeller DaemonSet
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeLabellerTolerations",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:nodeLabellerTolerations"}
	// +optional
	NodeLabellerTolerations []v1.Toleration `json:"nodeLabellerTolerations,omitempty"`

	// node labeller image registry secret used to pull/push images
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// enable or disable the node labeller
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="EnableNodeLabeller",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enableNodeLabeller"}
	// +kubebuilder:default=true
	EnableNodeLabeller *bool `json:"enableNodeLabeller,omitempty"`

	// upgrade policy for device plugin and node labeller daemons
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradePolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradePolicy"}
	// +optional
	UpgradePolicy *DaemonSetUpgradeSpec `json:"upgradePolicy,omitempty"`
}

type DaemonSetUpgradeSpec struct {
	// UpgradeStrategy specifies the type of the DaemonSet update. Valid values are "RollingUpdate" (default) or "OnDelete".
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradeStrategy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradeStrategy"}
	// +kubebuilder:validation:Enum=RollingUpdate;OnDelete
	// +optional
	UpgradeStrategy string `json:"upgradeStrategy,omitempty"`

	// MaxUnavailable specifies the maximum number of Pods that can be unavailable during the update process. Applicable for RollingUpdate only. Default value is 1.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="MaxUnavailable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:maxUnavailable"}
	// +kubebuilder:default=1
	MaxUnavailable int32 `json:"maxUnavailable,omitempty"`
}

type ImageSignSpec struct {
	// ImageSignKeySecret the private key used to sign kernel modules within image
	// necessary for secure boot enabled system
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageSignKeySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageSignKeySecret"}
	// +optional
	KeySecret *v1.LocalObjectReference `json:"keySecret,omitempty"`

	// ImageSignCertSecret the public key used to sign kernel modules within image
	// necessary for secure boot enabled system
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageSignCertSecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageSignCertSecret"}
	// +optional
	CertSecret *v1.LocalObjectReference `json:"certSecret,omitempty"`
}

// ServiceType string describes ingress methods for a service
type ServiceType string

const (
	// ServiceTypeClusterIP to access inside the cluster
	ServiceTypeClusterIP ServiceType = "ClusterIP"

	// ServiceTypeNodePort to expose service to external
	ServiceTypeNodePort ServiceType = "NodePort"
)

type ConfigManagerSpec struct {
	// enable config manager, disabled by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// config manager image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// image pull policy for config manager
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// config manager image registry secret used to pull/push images
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// config map to customize the config for config manager, if not specified default config will be applied
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:configmap"}
	// +optional
	Config *v1.LocalObjectReference `json:"config,omitempty"`

	// Selector describes on which nodes to enable config manager
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:selector"}
	// +optional
	Selector map[string]string `json:"selector,omitempty"`

	// upgrade policy for config manager daemonset
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradePolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradePolicy"}
	// +optional
	UpgradePolicy *DaemonSetUpgradeSpec `json:"upgradePolicy,omitempty"`

	// tolerations for the device config manager DaemonSet
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ConfigManagerTolerations",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:configManagerTolerations"}
	// +optional
	ConfigManagerTolerations []v1.Toleration `json:"configManagerTolerations,omitempty"`
}

type MetricsExporterSpec struct {
	// enable metrics exporter, disabled by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// metrics exporter image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// metrics exporter image registry secret used to pull/push images
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// image pull policy for metrics exporter
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// tolerations for metrics exporter
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tolerations",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:tolerations"}
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// Port is the internal port used for in-cluster and node access to pull metrics from the metrics-exporter (default 5000).
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Port",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:port"}
	// +kubebuilder:default=5000
	Port int32 `json:"port,omitempty"`

	// ServiceType service type for metrics, clusterIP/NodePort, clusterIP by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ServiceType",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:serviceType"}
	// +kubebuilder:validation:Enum=ClusterIP;NodePort
	// +kubebuilder:default=ClusterIP
	SvcType ServiceType `json:"serviceType,omitempty"`

	// NodePort is the external port for pulling metrics from outside the cluster, in the range 30000-32767 (assigned automatically by default)
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodePort",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:nodePort"}
	// +optional
	// +kubebuilder:validation:Minimum=30000
	// +kubebuilder:validation:Maximum=32767
	NodePort int32 `json:"nodePort,omitempty"`

	// optional configuration for metrics
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:config"}
	// +optional
	Config MetricsConfig `json:"config,omitempty"`

	// optional kube-rbac-proxy config to provide rbac services
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="RbacConfig",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:rbacConfig"}
	// +optional
	RbacConfig KubeRbacConfig `json:"rbacConfig,omitempty"`

	// Selector describes on which nodes to enable metrics exporter
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:selector"}
	// +optional
	Selector map[string]string `json:"selector,omitempty"`

	// upgrade policy for metrics exporter daemons
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradePolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradePolicy"}
	// +optional
	UpgradePolicy *DaemonSetUpgradeSpec `json:"upgradePolicy,omitempty"`
}

// KubeRbacConfig contains configs for kube-rbac-proxy sidecar
type KubeRbacConfig struct {
	// enable kube-rbac-proxy, disabled by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// kube-rbac-proxy image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// disable https protecting the proxy endpoint
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DisableHttps",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:disableHttps"}
	// +optional
	DisableHttps *bool `json:"disableHttps,omitempty"`

	// certificate secret to mount in kube-rbac container for TLS, self signed certificates will be generated by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:secret"}
	// +optional
	Secret *v1.LocalObjectReference `json:"secret,omitempty"`
}

// MetricsConfig contains list of metrics to collect/report
type MetricsConfig struct {
	// Name of the configMap that defines the list of metrics
	// default list:[]
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:name"}
	// +optional
	Name string `json:"name,omitempty"`
}

type TestRunnerSpec struct {
	// enable test runner, disabled by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enable"}
	// +optional
	Enable *bool `json:"enable,omitempty"`

	// test runner image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// image pull policy for test runner
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// tolerations for test runner
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tolerations",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:tolerations"}
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// test runner image registry secret used to pull/push images
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// config map to customize the config for test runner, if not specified default test config will be aplied
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:configmap"}
	// +optional
	Config *v1.LocalObjectReference `json:"config,omitempty"`

	// Selector describes on which nodes to enable test runner
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:selector"}
	// +optional
	Selector map[string]string `json:"selector,omitempty"`

	// upgrade policy for test runner daemonset
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UpgradePolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:upgradePolicy"}
	// +optional
	UpgradePolicy *DaemonSetUpgradeSpec `json:"upgradePolicy,omitempty"`

	// captures logs location and export config for test runner logs
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogsLocation",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:logsLocation"}
	// +optional
	LogsLocation LogsLocationConfig `json:"logsLocation,omitempty"`
}

// LogsLocationConfig contains mount and export config for test runner logs
type LogsLocationConfig struct {
	// volume mount destination within test runner container
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="MountPath",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:mountPath"}
	// +kubebuilder:default="/var/log/amd-test-runner"
	// +optional
	MountPath string `json:"mountPath,omitempty"`

	// host path to store test runner internal status db in order to persist test running status
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="HostPath",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:hostPath"}
	// +kubebuilder:default="/var/log/amd-test-runner"
	// +optional
	HostPath string `json:"hostPath,omitempty"`

	// LogsExportSecrets is a list of secrets that contain connectivity info to multiple cloud providers
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogsExportSecrets",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:logsExportSecrets"}
	// +optional
	LogsExportSecrets []*v1.LocalObjectReference `json:"logsExportSecrets,omitempty"`
}

// UtilsContainerSpec contains parameters to configure operator's utils
type UtilsContainerSpec struct {
	// Image is the image of utils container
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:image"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	Image string `json:"image,omitempty"`

	// image pull policy for utils container
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImagePullPolicy",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imagePullPolicy"}
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// secret used for pull utils container image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`
}

// CommonConfigSpec contains the common config across operator and operands
type CommonConfigSpec struct {
	// InitContainerImage is being used for the operands pods, i.e. metrics exporter, test runner, device plugin and node labeller
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="InitContainerImage",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:initContainerImage"}
	// +optional
	InitContainerImage string `json:"initContainerImage,omitempty"`

	// UtilsContainer contains parameters to configure operator's utils container
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="UtilsContainer",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:utilsContainer"}
	// +optional
	UtilsContainer UtilsContainerSpec `json:"utilsContainer,omitempty"`
}

// DeploymentStatus contains the status for a daemonset deployed during
// reconciliation loop
type DeploymentStatus struct {
	// number of nodes that are targeted by the DeviceConfig selector
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="NodesMatchingSelectorNumber",xDescriptors="urn:alm:descriptor:com.amd.deviceconfigs:nodesMatchingSelectorNumber"
	NodesMatchingSelectorNumber int32 `json:"nodesMatchingSelectorNumber,omitempty"`
	// number of the pods that should be deployed for daemonset
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="DesiredNumber",xDescriptors="urn:alm:descriptor:com.amd.deviceconfigs:desiredNumber"
	DesiredNumber int32 `json:"desiredNumber,omitempty"`
	// number of the actually deployed and running pods
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="AvailableNumber",xDescriptors="urn:alm:descriptor:com.amd.deviceconfigs:availableNumber"
	AvailableNumber int32 `json:"availableNumber,omitempty"`
}

// ModuleStatus contains the status of driver module installed by operator on the node
type ModuleStatus struct {
	ContainerImage     string       `json:"containerImage,omitempty"`
	KernelVersion      string       `json:"kernelVersion,omitempty"`
	LastTransitionTime string       `json:"lastTransitionTime,omitempty"`
	Status             UpgradeState `json:"status,omitempty"`
	UpgradeStartTime   string       `json:"upgradeStartTime,omitempty"`
}

// DeviceConfigStatus defines the observed state of Module.
type DeviceConfigStatus struct {
	// DevicePlugin contains the status of the Device Plugin deployment
	DevicePlugin DeploymentStatus `json:"devicePlugin,omitempty"`
	// Driver contains the status of the Drivers deployment
	Drivers DeploymentStatus `json:"driver,omitempty"`
	// MetricsExporter contains the status of the MetricsExporter deployment
	MetricsExporter DeploymentStatus `json:"metricsExporter,omitempty"`
	// ConfigManager contains the status of the ConfigManager deployment
	ConfigManager DeploymentStatus `json:"configManager,omitempty"`
	// NodeModuleStatus contains per node status of driver module installation
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="NodeModuleStatus",xDescriptors="urn:alm:descriptor:com.amd.deviceconfigs:nodeModuleStatus"
	NodeModuleStatus map[string]ModuleStatus `json:"nodeModuleStatus,omitempty"`
	// Conditions list the current status of the DeviceConfig object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the latest spec generation successfully processed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Namespaced,shortName=gpue
//+kubebuilder:subresource:status

// DeviceConfig describes how to enable AMD GPU device
// +operator-sdk:csv:customresourcedefinitions:displayName="DeviceConfig",resources={{Module,v1beta1,modules.kmm.sigs.x-k8s.io},{Daemonset,v1,apps}, {services,v1,core}}
type DeviceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeviceConfigSpec   `json:"spec,omitempty"`
	Status DeviceConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DeviceConfigList contains a list of DeviceConfigs
type DeviceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeviceConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeviceConfig{}, &DeviceConfigList{})
}
