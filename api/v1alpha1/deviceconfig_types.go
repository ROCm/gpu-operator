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

	// device plugin
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePlugin",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePlugin"}
	// +optional
	DevicePlugin DevicePluginSpec `json:"devicePlugin,omitempty"`

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
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
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
}

type DevicePluginSpec struct {
	// device plugin image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DevicePluginImage",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:devicePluginImage"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	DevicePluginImage string `json:"devicePluginImage,omitempty"`

	// node labeller image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeLabellerImage",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:nodeLabellerImage"}
	// +optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(?:[._-][a-z0-9]+)*(:[0-9]+)?)(/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[a-z0-9._-]+)?(?:@[a-zA-Z0-9]+:[a-f0-9]+)?$`
	NodeLabellerImage string `json:"nodeLabellerImage,omitempty"`

	// node labeller image registry secret used to pull/push images
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageRegistrySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageRegistrySecret"}
	// +optional
	ImageRegistrySecret *v1.LocalObjectReference `json:"imageRegistrySecret,omitempty"`

	// enable or disable the node labeller
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="EnableNodeLabeller",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:enableNodeLabeller"}
	// +kubebuilder:default=true
	EnableNodeLabeller *bool `json:"enableNodeLabeller,omitempty"`
}

type ImageSignSpec struct {
	// ImageSignKeySecret the private key used to sign kernel modules within image
	// necessary for secire boot enabled system
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ImageSignKeySecret",xDescriptors={"urn:alm:descriptor:com.amd.deviceconfigs:imageSignKeySecret"}
	// +optional
	KeySecret *v1.LocalObjectReference `json:"keySecret,omitempty"`

	// ImageSignCertSecret the public key used to sign kernel modules within image
	// necessary for secire boot enabled system
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
	ContainerImage     string `json:"containerImage,omitempty"`
	KernelVersion      string `json:"kernelVersion,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// DeviceConfigStatus defines the observed state of Module.
type DeviceConfigStatus struct {
	// DevicePlugin contains the status of the Device Plugin deployment
	DevicePlugin DeploymentStatus `json:"devicePlugin,omitempty"`
	// Driver contains the status of the Drivers deployment
	Drivers DeploymentStatus `json:"driver,omitempty"`
	// MetricsExporter contains the status of the MetricsExporter deployment
	MetricsExporter DeploymentStatus `json:"metricsExporter,omitempty"`
	// NodeModuleStatus contains per node status of driver module installation
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="NodeModuleStatus",xDescriptors="urn:alm:descriptor:com.amd.deviceconfigs:nodeModuleStatus"
	NodeModuleStatus map[string]ModuleStatus `json:"nodeModuleStatus,omitempty"`
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
