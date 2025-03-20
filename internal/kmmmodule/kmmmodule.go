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

package kmmmodule

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/pointer"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	kubeletDevicePluginsVolumeName = "kubelet-device-plugins"
	kubeletDevicePluginsPath       = "/var/lib/kubelet/device-plugins"
	nodeVarLibFirmwarePath         = "/var/lib/firmware"
	gpuDriverModuleName            = "amdgpu"
	ttmModuleName                  = "amdttm"
	kclModuleName                  = "amdkcl"
	imageFirmwarePath              = "firmwareDir/updates"
	kmmNodeVersionLabelTemplate    = "kmm.node.kubernetes.io/version-module.%s.%s"
	// check the device plugin image tags here: https://hub.docker.com/r/rocm/k8s-device-plugin/tags
	defaultDevicePluginImage      = "rocm/k8s-device-plugin:latest"
	defaultUbiDevicePluginImage   = "rocm/k8s-device-plugin:rhubi-latest"
	defaultOcDriversImageTemplate = "image-registry.openshift-image-registry.svc:5000/$MOD_NAMESPACE/amdgpu_kmod"
	// start local registry image-registry:5000 in k8s
	defaultDriversImageTemplate = "image-registry:5000/$MOD_NAMESPACE/amdgpu_kmod"
	defaultOcDriversVersion     = "6.2.2"
	defaultInstallerRepoURL     = "https://repo.radeon.com"
	defaultInitContainerImage   = "busybox:1.36"
)

var (
	//go:embed dockerfiles/DockerfileTemplate.ubuntu
	dockerfileTemplateUbuntu string
	//go:embed dockerfiles/driversDockerfile.txt
	buildOcDockerfile string
	//go:embed devdockerfiles/devdockerfile.txt
	dockerfileDevTemplateUbuntu string
)

//go:generate mockgen -source=kmmmodule.go -package=kmmmodule -destination=mock_kmmmodule.go KMMModuleAPI
type KMMModuleAPI interface {
	SetNodeVersionLabelAsDesired(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error
	SetKMMModuleAsDesired(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error
	SetDevicePluginAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error
}

type kmmModule struct {
	client      client.Client
	scheme      *runtime.Scheme
	isOpenShift bool
}

func NewKMMModule(client client.Client, scheme *runtime.Scheme) KMMModuleAPI {
	return &kmmModule{
		client:      client,
		scheme:      scheme,
		isOpenShift: isOpenshift(),
	}
}

func isOpenshift() bool {
	if dc, err := discovery.NewDiscoveryClientForConfig(ctrl.GetConfigOrDie()); err == nil {
		if gplist, err := dc.ServerGroups(); err == nil {
			for _, gp := range gplist.Groups {
				if gp.Name == "route.openshift.io" {
					return true
				}
			}
		}
	}
	return false
}

func (km *kmmModule) SetNodeVersionLabelAsDesired(ctx context.Context, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	// for each selected node
	// put the KMM version label given by CR's driver version
	// KMM operator will watch on the version label and manage the kmod upgrade
	labelKey, labelVal := GetVersionLabelKV(devConfig)
	logger := log.FromContext(ctx)
	for _, node := range nodes.Items {
		if _, ok := node.Labels[labelKey]; ok {
			// version label was already put on the node object
			// our operator should only upload the version label for 0->1 installation
			// for 1->2 upgrade, we expect users to manually update the version label on Node resource to trigger ordered upgrade
			// so if thee label was already there, controller won't update it
			continue
		}
		if labelVal == "" {
			defaultVersion, err := utils.GetDefaultDriversVersion(node)
			if err != nil {
				logger.Error(err, fmt.Sprintf("failed to get default version for node %+v err %+v", node.GetName(), err))
			}
			labelVal = defaultVersion
		}
		patch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]string{
					labelKey: labelVal,
				},
			},
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return fmt.Errorf("failed to marshal node label patch: %+v", err)
		}
		rawPatch := client.RawPatch(types.StrategicMergePatchType, patchBytes)
		if err := km.client.Patch(ctx, &node, rawPatch); err != nil {
			return fmt.Errorf("failed to patch node label: %+v", err)
		}
	}
	return nil
}

func (km *kmmModule) SetBuildConfigMapAsDesired(buildCM *v1.ConfigMap, devConfig *amdv1alpha1.DeviceConfig) error {
	if buildCM.Data == nil {
		buildCM.Data = make(map[string]string)
	}
	if km.isOpenShift {
		buildCM.Data["dockerfile"] = buildOcDockerfile
	} else {
		dockerfile, err := resolveDockerfile(buildCM.Name, devConfig)
		if err != nil {
			return err
		}
		buildCM.Data["dockerfile"] = dockerfile
	}
	return controllerutil.SetControllerReference(devConfig, buildCM, km.scheme)
}

var driverLabels = map[string]string{
	"20.04": "focal",
	"22.04": "jammy",
	"24.04": "noble",
}

func resolveDockerfile(cmName string, devConfig *amdv1alpha1.DeviceConfig) (string, error) {
	splits := strings.SplitN(cmName, "-", 4)
	osDistro := splits[0]
	version := splits[1]
	var dockerfileTemplate string
	switch osDistro {
	case "ubuntu":
		dockerfileTemplate = dockerfileTemplateUbuntu
		driverLabel, present := driverLabels[version]
		if !present {
			return "", fmt.Errorf("invalid ubuntu version, expected to be one of %v", maps.Keys(driverLabels))
		}
		dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$DRIVER_LABEL", driverLabel, -1)

		// trigger to pull the internal ROCM dev build
		if internalArtifactoryURL, ok := os.LookupEnv("INTERNAL_ARTIFACTORY"); ok &&
			strings.Contains(strings.ToLower(devConfig.Spec.Driver.AMDGPUInstallerRepoURL), internalArtifactoryURL) {
			dockerfileTemplate = dockerfileDevTemplateUbuntu
			devBuildinfo := strings.Split(devConfig.Spec.Driver.AMDGPUInstallerRepoURL, " ")
			if len(devBuildinfo) < 4 {
				return "", fmt.Errorf("please provide internal build info, required 4 items: artifactory URL, installer deb file name, amdgpu build number and rocm build tag, got: %+v", devConfig.Spec.Driver.AMDGPUInstallerRepoURL)
			}
			devConfig.Spec.Driver.AMDGPUInstallerRepoURL = devBuildinfo[0]
			dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$DEV_DEB", devBuildinfo[1], -1)
			dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$AMDGPU_BUILD", devBuildinfo[2], -1)
			dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$ROCM_BUILD", devBuildinfo[3], -1)
		}
		// use an environment variable to ask CI infra to pull image from internal repository
		// in order to avoid docekrhub pull rate limit issue
		_, isCIEnvSet := os.LookupEnv("CI_ENV")
		internalUbuntuBaseImage, internalUbuntuBaseSet := os.LookupEnv("INTERNAL_UBUNTU_BASE")
		if isCIEnvSet && internalUbuntuBaseSet {
			dockerfileTemplate = strings.Replace(dockerfileTemplate, "ubuntu:$$VERSION", fmt.Sprintf("%v:$$VERSION", internalUbuntuBaseImage), -1)
		}
	case "coreos":
		dockerfileTemplate = buildOcDockerfile
	// FIX ME
	// add the RHEL back when it is fully supported
	/*case "rhel":
	dockerfileTemplate = dockerfileTemplateRHEL
	versionSplits := strings.Split(version, ".")
	dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$MAJOR_VERSION", versionSplits[0], -1)
	if devConfig.Spec.RedhatSubscriptionUsername == "" || devConfig.Spec.RedhatSubscriptionPassword == "" {
		return "", fmt.Errorf("Redhat subscription RedhatSubscriptionUsername and RedhatSubscriptionPassword required")
	}
	dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$REDHAT_SUBSCRIPTION_USERNAME", devConfig.Spec.RedhatSubscriptionUsername, -1)
	dockerfileTemplate = strings.Replace(dockerfileTemplate, "$$REDHAT_SUBSCRIPTION_PASSWORD", devConfig.Spec.RedhatSubscriptionPassword, -1)
	*/
	default:
		return "", fmt.Errorf("not supported OS: %s", osDistro)
	}
	resolvedDockerfile := strings.Replace(dockerfileTemplate, "$$VERSION", version, -1)
	return resolvedDockerfile, nil
}

func (km *kmmModule) SetKMMModuleAsDesired(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) error {
	err := setKMMModuleLoader(ctx, mod, devConfig, km.isOpenShift, nodes)
	if err != nil {
		return fmt.Errorf("failed to set KMM Module: %v", err)
	}
	return controllerutil.SetControllerReference(devConfig, mod, km.scheme)
}

func (km *kmmModule) SetDevicePluginAsDesired(ds *appsv1.DaemonSet, devConfig *amdv1alpha1.DeviceConfig) error {
	var devicePluginImage string

	if devConfig.Spec.DevicePlugin.DevicePluginImage == "" {
		if km.isOpenShift {
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

	nodeSelector := map[string]string{}
	for key, val := range devConfig.Spec.Selector {
		nodeSelector[key] = val
	}
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		nodeSelector[labels.GetKernelModuleReadyNodeLabel(devConfig.Namespace, devConfig.Name)] = ""
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
						Command:         []string{"sh", "-c", "while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 2 ;done"},
						SecurityContext: &v1.SecurityContext{Privileged: pointer.Bool(true)},
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
						Command:         []string{"sh", "-c", "./k8s-device-plugin -logtostderr=true -stderrthreshold=INFO -v=5 -pulse=30"},
						Image:           devicePluginImage,
						SecurityContext: &v1.SecurityContext{Privileged: pointer.Bool(true)},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "kubelet-device-plugins",
								MountPath: "/var/lib/kubelet/device-plugins",
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
								Path: "/var/lib/kubelet/device-plugins",
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
	return controllerutil.SetControllerReference(devConfig, ds, km.scheme)
}

func setKMMModuleLoader(ctx context.Context, mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig, isOpenshift bool, nodes *v1.NodeList) error {
	kmlog := log.FromContext(ctx)
	kmlog.Info(fmt.Sprintf("isOpenshift %+v", isOpenshift))

	args := &kmmv1beta1.ModprobeArgs{}
	firmwarePath := imageFirmwarePath

	kernelMappings, driversVersion, err := getKernelMappings(devConfig, isOpenshift, nodes)
	if err != nil {
		return err
	}

	var modLoadingOrder []string
	var moduleName = gpuDriverModuleName
	if !isOpenshift {
		// specify this order fror k8s in order to make sure amdttm and amdkcl was properly cleaned up after deletion of CR
		// module will be loaded in this order: amdkcl, amdttm, amdgpu
		// module will be unloaded in this order: amdgpu, amdttm, amdkcl
		modLoadingOrder = []string{
			gpuDriverModuleName,
			ttmModuleName,
			kclModuleName,
		}
	}

	if devConfig.Spec.Driver.Enable == nil || !*devConfig.Spec.Driver.Enable {
		args = &kmmv1beta1.ModprobeArgs{
			Load:   []string{"-n"},
			Unload: []string{"-n"},
		}
		firmwarePath = ""
		modLoadingOrder = nil
		kmlog.Info("skip driver install/uninstall")
		moduleName = "dummy"
	}

	mod.Spec.ModuleLoader.Container = kmmv1beta1.ModuleLoaderContainerSpec{
		Modprobe: kmmv1beta1.ModprobeSpec{
			ModuleName:          moduleName,
			FirmwarePath:        firmwarePath,
			Args:                args,
			ModulesLoadingOrder: modLoadingOrder,
		},
		Version:        devConfig.Spec.Driver.Version,
		KernelMappings: kernelMappings,
	}
	if mod.Spec.ModuleLoader.Container.Version == "" {
		mod.Spec.ModuleLoader.Container.Version = driversVersion
	}
	mod.Spec.ModuleLoader.ServiceAccountName = "amd-gpu-operator-kmm-module-loader"
	mod.Spec.ImageRepoSecret = devConfig.Spec.Driver.ImageRegistrySecret
	mod.Spec.Selector = getNodeSelector(devConfig)
	//TODO Enable when kmm has this field
	/*
		mod.Spec.Tolerations = []v1.Toleration {
			{
				Key:      "amd-gpu-driver-upgrade",
				Value:    "true",
				Operator: v1.TolerationOpEqual,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}
	*/
	return nil
}

func getKernelMappings(devConfig *amdv1alpha1.DeviceConfig, isOpenshift bool, nodes *v1.NodeList) ([]kmmv1beta1.KernelMapping, string, error) {

	inTreeModuleToRemove := ""

	if nodes == nil || len(nodes.Items) == 0 {
		return nil, "", fmt.Errorf("No nodes found for the label selector %s", MapToLabelSelector(devConfig.Spec.Selector))
	}
	kernelMappings := []kmmv1beta1.KernelMapping{}
	kmSet := map[string]bool{}
	var driversVersion string
	for _, node := range nodes.Items {
		km, ver, err := getKM(devConfig, node, inTreeModuleToRemove, isOpenshift)
		if err != nil {
			return nil, driversVersion, fmt.Errorf("error constructing a kernel mapping for node: %s, err: %v", node.Name, err)
		}
		if kmSet[km.Literal] {
			continue
		}
		kernelMappings = append(kernelMappings, km)
		kmSet[km.Literal] = true
		driversVersion = ver
	}
	return kernelMappings, driversVersion, nil
}

func getKM(devConfig *amdv1alpha1.DeviceConfig, node v1.Node, inTreeModuleToRemove string, isOpenShift bool) (kmmv1beta1.KernelMapping, string, error) {
	driversVersion := devConfig.Spec.Driver.Version
	driversImage := devConfig.Spec.Driver.Image
	var err error
	osName, err := GetOSName(node, devConfig)
	if err != nil {
		return kmmv1beta1.KernelMapping{}, "", err
	}

	if isOpenShift {
		if driversVersion == "" {
			driversVersion = defaultOcDriversVersion
		}
		if driversImage == "" {
			driversImage = defaultOcDriversImageTemplate
		}
		driversImage = addNodeInfoSuffixToImageTag(driversImage, osName, driversVersion)
	} else {
		if driversVersion == "" {
			driversVersion, err = utils.GetDefaultDriversVersion(node)
			if err != nil {
				return kmmv1beta1.KernelMapping{}, "", err
			}
		}
		if driversImage == "" {
			driversImage = defaultDriversImageTemplate
		}
		driversImage = addNodeInfoSuffixToImageTag(driversImage, osName, driversVersion)
	}

	repoURL := defaultInstallerRepoURL
	if devConfig.Spec.Driver.AMDGPUInstallerRepoURL != "" {
		repoURL = devConfig.Spec.Driver.AMDGPUInstallerRepoURL
	}

	var registryTLS *kmmv1beta1.TLSOptions
	if (devConfig.Spec.Driver.ImageRegistryTLS.Insecure != nil && *devConfig.Spec.Driver.ImageRegistryTLS.Insecure) ||
		(devConfig.Spec.Driver.ImageRegistryTLS.InsecureSkipTLSVerify != nil && *devConfig.Spec.Driver.ImageRegistryTLS.InsecureSkipTLSVerify) {
		registryTLS = &kmmv1beta1.TLSOptions{}
		if devConfig.Spec.Driver.ImageRegistryTLS.Insecure != nil {
			registryTLS.Insecure = *devConfig.Spec.Driver.ImageRegistryTLS.Insecure
		}
		if devConfig.Spec.Driver.ImageRegistryTLS.InsecureSkipTLSVerify != nil {
			registryTLS.InsecureSkipTLSVerify = *devConfig.Spec.Driver.ImageRegistryTLS.InsecureSkipTLSVerify
		}
	}

	var kmmSign *kmmv1beta1.Sign
	if devConfig.Spec.Driver.ImageSign.KeySecret != nil &&
		devConfig.Spec.Driver.ImageSign.CertSecret != nil {
		kmmSign = &kmmv1beta1.Sign{
			KeySecret:   devConfig.Spec.Driver.ImageSign.KeySecret,
			CertSecret:  devConfig.Spec.Driver.ImageSign.CertSecret,
			FilesToSign: getKmodsToSign(isOpenShift, node.Status.NodeInfo.KernelVersion),
		}
		if registryTLS != nil {
			kmmSign.UnsignedImageRegistryTLS = *registryTLS
		}
	}

	kmmBuild := &kmmv1beta1.Build{
		DockerfileConfigMap: &v1.LocalObjectReference{
			Name: GetCMName(osName, devConfig),
		},
		BuildArgs: []kmmv1beta1.BuildArg{
			{
				Name:  "DRIVERS_VERSION",
				Value: driversVersion,
			},
			{
				Name:  "REPO_URL",
				Value: repoURL,
			},
		},
	}

	_, isCIEnvSet := os.LookupEnv("CI_ENV")
	if isCIEnvSet {
		kmmBuild.BaseImageRegistryTLS.Insecure = true
		kmmBuild.BaseImageRegistryTLS.InsecureSkipTLSVerify = true
	}

	return kmmv1beta1.KernelMapping{
		Literal:              node.Status.NodeInfo.KernelVersion,
		ContainerImage:       driversImage,
		InTreeModuleToRemove: inTreeModuleToRemove,
		Build:                kmmBuild,
		Sign:                 kmmSign,
		RegistryTLS:          registryTLS,
	}, driversVersion, nil
}

func addNodeInfoSuffixToImageTag(imgStr string, osName, driversVersion string) string {
	// KMM will render and fulfill the value of ${KERNEL_FULL_VERSION}
	tag := osName + "-${KERNEL_FULL_VERSION}-" + driversVersion
	// tag cannot be more than 128 chars
	if len(tag) > 128 {
		tag = tag[len(tag)-128:]
	}
	return imgStr + ":" + tag
}

func GetCMName(osName string, devCfg *amdv1alpha1.DeviceConfig) string {
	return osName + "-" + devCfg.Name + "-" + devCfg.Namespace
}

func GetOSName(node v1.Node, devCfg *amdv1alpha1.DeviceConfig) (string, error) {
	osImageStr := strings.ToLower(node.Status.NodeInfo.OSImage)

	// sort the key of cmNameMappers
	// make sure in the given OS string, coreos was checked before all other types of RHEL string
	keys := make([]string, 0, len(cmNameMappers))
	for key := range cmNameMappers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, os := range keys {
		if strings.Contains(osImageStr, os) {
			return cmNameMappers[os](osImageStr), nil
		}
	}

	return "", fmt.Errorf("OS: %s not supported. Should be one of %v", osImageStr, maps.Keys(cmNameMappers))
}

var cmNameMappers = map[string]func(fullImageStr string) string{
	"ubuntu":  ubuntuCMNameMapper,
	"coreos":  rhelCoreOSNameMapper,
	"rhel":    rhelCMNameMapper,
	"red hat": rhelCMNameMapper,
	"redhat":  rhelCMNameMapper,
}

func rhelCMNameMapper(osImageStr string) string {
	// Check if the input contains "Red Hat Enterprise Linux"
	// Use regex to find the release version
	re := regexp.MustCompile(`(\d+\.\d+)`)
	matches := re.FindStringSubmatch(osImageStr)
	if len(matches) > 1 {
		return fmt.Sprintf("%s-%s", "rhel", matches[1])
	}
	return "rhel-" + osImageStr
}

func rhelCoreOSNameMapper(osImageStr string) string {
	// Check if the input contains "Red Hat Enterprise Linux"
	// Use regex to find the release version
	re := regexp.MustCompile(`(\d+\.\d+)`)
	matches := re.FindStringSubmatch(osImageStr)
	if len(matches) > 1 {
		return fmt.Sprintf("%s-%s", "coreos", matches[1])
	}
	return "coreos-" + osImageStr
}

func ubuntuCMNameMapper(osImageStr string) string {
	splits := strings.Split(osImageStr, " ")
	os := splits[0]
	version := splits[1]
	versionSplits := strings.Split(version, ".")
	trimmedVersion := strings.Join(versionSplits[:2], ".")
	return fmt.Sprintf("%s-%s", os, trimmedVersion)
}

func GetK8SNodes(ls string) (*v1.NodeList, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	options := metav1.ListOptions{
		LabelSelector: ls,
	}
	return clientset.CoreV1().Nodes().List(context.TODO(), options)
}

func MapToLabelSelector(selector map[string]string) string {
	selectorSlice := make([]string, 0)
	for k, v := range selector {
		selectorSlice = append(selectorSlice, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(selectorSlice, ",")
}

func GetVersionLabelKV(devConfig *amdv1alpha1.DeviceConfig) (string, string) {
	return fmt.Sprintf(kmmNodeVersionLabelTemplate, devConfig.Namespace, devConfig.Name), devConfig.Spec.Driver.Version
}

func setKMMDevicePlugin(mod *kmmv1beta1.Module, devConfig *amdv1alpha1.DeviceConfig) {
	devicePluginImage := devConfig.Spec.DevicePlugin.DevicePluginImage
	if devicePluginImage == "" {
		devicePluginImage = defaultDevicePluginImage
	}
	hostPathDirectory := v1.HostPathDirectory
	mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{
		ServiceAccountName: "amd-gpu-operator-kmm-device-plugin",
		Container: kmmv1beta1.DevicePluginContainerSpec{
			Command:         []string{"sh"},
			Args:            []string{"-c", "while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 1 ;done; ./k8s-device-plugin -logtostderr=true -stderrthreshold=INFO -v=5"},
			Image:           devicePluginImage,
			ImagePullPolicy: v1.PullAlways,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "sys",
					MountPath: "/sys",
				},
			},
		},
		Volumes: []v1.Volume{
			{
				Name: "sys",
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: "/sys",
						Type: &hostPathDirectory,
					},
				},
			},
		},
	}
}

func getNodeSelector(devConfig *amdv1alpha1.DeviceConfig) map[string]string {
	if devConfig.Spec.Selector != nil {
		return devConfig.Spec.Selector
	}

	ns := make(map[string]string, 0)
	ns["feature.node.kubernetes.io/amd-gpu"] = "true"
	return ns
}

func getKmodsToSign(isOpenShift bool, kernelVersion string) []string {
	if isOpenShift {
		return []string{
			"/opt/lib/modules/" + kernelVersion + "/extra/amdgpu.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amdkcl.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amdxcp.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amd-sched.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amdttm.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amddrm_buddy.ko",
			"/opt/lib/modules/" + kernelVersion + "/extra/amddrm_ttm_helper.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/gpu/drm/drm_exec.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/gpu/drm/drm_suballoc_helper.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/gpu/drm/display/drm_display_helper.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/acpi/video.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/platform/x86/wmi.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/i2c/algos/i2c-algo-bit.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/media/cec/core/cec.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/gpu/drm/drm_kms_helper.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/video/fbdev/core/syscopyarea.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/video/fbdev/core/sysfillrect.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/video/fbdev/core/sysimgblt.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/video/fbdev/core/fb_sys_fops.ko",
			"/opt/lib/modules/" + kernelVersion + "/kernel/drivers/gpu/drm/drm.ko",
		}
	}
	return []string{
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amdkcl.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amdttm.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amdgpu.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amdxcp.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amd-sched.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amddrm_buddy.ko",
		"/opt/lib/modules/" + kernelVersion + "/updates/dkms/amddrm_ttm_helper.ko",
	}
}
