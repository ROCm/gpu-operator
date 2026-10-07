package main

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
)

func createOutput(config *Config, scheme *runtime.Scheme) (string, error) {
	deviceConfig := deriveDeviceConfig(config)
	node := deriveNode(config.osPrettyName, config.buildArgs.kernelFullVersion)

	osName, err := kmmmodule.GetOSName(node, deviceConfig)
	if err != nil {
		return "", fmt.Errorf("failed to derive OS name from OS pretty name %q: %w", config.osPrettyName, err)
	}

	if isOpenShift(osName) && config.buildArgs.driverToolkitImage == "" {
		return "", fmt.Errorf("driver toolkit image is required for OpenShift")
	}

	dockerfile, err := deriveDockerfile(
		osName,
		config.buildArgs.driverToolkitImage,
		node,
		deviceConfig,
		scheme,
	)
	if err != nil {
		return "", fmt.Errorf("failed to derive Dockerfile: %w", err)
	}

	var output string
	switch config.OutputFormat {
	case OutputFormatDockerfile:
		output = dockerfile
	case OutputFormatDockerScript, OutputFormatBuildahScript, OutputFormatPodmanScript:
		if config.imageName == "" {
			return "", fmt.Errorf("image name is required for docker-script, buildah-script, and podman-script output formats")
		}
		imageName := deriveImageNameWithTag(osName, config, deviceConfig)
		output = deriveScript(config.OutputFormat, imageName, dockerfile)
	default:
		return "", fmt.Errorf("unknown output format %q; valid values are: dockerfile, docker-script, buildah-script, podman-script", config.OutputFormat)
	}
	return output, nil
}

func deriveImageNameWithTag(osName string, config *Config, deviceConfig *gpuev1alpha1.DeviceConfig) string {
	imageNameWithoutKernelVersion := kmmmodule.AddNodeInfoSuffixToImageTag(config.imageName, osName, config.buildArgs.driversVersion, deviceConfig)
	return strings.Replace(imageNameWithoutKernelVersion, "${KERNEL_FULL_VERSION}", config.buildArgs.kernelFullVersion, 1)
}

func deriveDeviceConfig(config *Config) *gpuev1alpha1.DeviceConfig {
	return &gpuev1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "build",
		},
		Spec: gpuev1alpha1.DeviceConfigSpec{
			Driver: gpuev1alpha1.DriverSpec{
				DriverType:             config.driverType,
				Version:                config.buildArgs.driversVersion,
				AMDGPUInstallerRepoURL: config.buildArgs.repoURL,
				ImageBuild: gpuev1alpha1.ImageBuildSpec{
					PackageRepoURL:  config.buildArgs.packageRepoURL,
					GPGKeyURL:       config.buildArgs.gpgKeyURL,
					SourceImageRepo: config.buildArgs.sourceImageRepo,
				},
				UseSourceImage: ptr.To(config.useSourceImage),
			},
		},
	}
}

func deriveNode(osPrettyName, kernelFullVersion string) corev1.Node {
	return corev1.Node{
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				OSImage:       osPrettyName,
				KernelVersion: kernelFullVersion,
			},
		},
	}
}

func deriveBuildConfigMap(osName string, deviceConfig *gpuev1alpha1.DeviceConfig) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: kmmmodule.GetCMName(osName, deviceConfig),
		},
	}
}

func isOpenShift(osName string) bool {
	return strings.Contains(osName, "rhel") || strings.Contains(osName, "coreos")
}

func deriveDockerfile(osName string, driverToolkitImage string, node corev1.Node, deviceConfig *gpuev1alpha1.DeviceConfig, scheme *runtime.Scheme) (string, error) {
	buildCM := deriveBuildConfigMap(osName, deviceConfig)

	kmmModule := kmmmodule.NewKMMModule(
		nil,                 // The client is not used by any of the kmmModule methods we call.
		scheme,              // A correctly initialized scheme is required by SetBuildConfigMapAsDesired.
		isOpenShift(osName), // OpenShift-specific logic is required to derive the Dockerfilefor RHEL and CoreOS.
	)

	err := kmmModule.SetBuildConfigMapAsDesired(buildCM, deviceConfig)
	if err != nil {
		return "", fmt.Errorf("failed to set BuildConfigMap as desired: %v", err)
	}

	dockerfile, ok := buildCM.Data["dockerfile"]
	if !ok {
		return "", fmt.Errorf("failed to get Dockerfile from BuildConfigMap")
	}

	// The GetKM methods returns a drivers version because its caller uses the version to create a
	// module loader container. We do not create a module loader container, so we ignore the drivers
	// version.
	km, _, err := kmmmodule.GetKM(
		deviceConfig,
		node,
		"",
		isOpenShift(osName),
	)
	if err != nil {
		return "", fmt.Errorf("failed to get KernelMapping: %v", err)
	}

	km.Build.BuildArgs = append(
		// The BuildArgs do not include the kernel full version, so we need to add it.
		km.Build.BuildArgs,
		v1beta1.BuildArg{
			Name:  "KERNEL_FULL_VERSION",
			Value: km.Literal,
		},
		v1beta1.BuildArg{
			Name:  "KERNEL_VERSION",
			Value: km.Literal,
		},
	)

	// The DTK_AUTO build arg is only used for OpenShift.
	if isOpenShift(osName) {
		km.Build.BuildArgs = append(km.Build.BuildArgs, v1beta1.BuildArg{
			Name:  "DTK_AUTO",
			Value: driverToolkitImage,
		})
	}

	return setBuildArgDefaults(km.Build.BuildArgs, dockerfile), nil
}

func setBuildArgDefaults(buildArgs []v1beta1.BuildArg, dockerfile string) string {
	for _, arg := range buildArgs {
		if arg.Value == "" {
			continue
		}
		argLine := regexp.MustCompile(`(?m)^ARG ` + regexp.QuoteMeta(arg.Name) + `(=.*)?$`)
		dockerfile = argLine.ReplaceAllLiteralString(dockerfile, fmt.Sprintf("ARG %s=%s", arg.Name, arg.Value))
	}
	return dockerfile
}

func deriveScript(outputFormat OutputFormat, imageName string, dockerfile string) string {
	switch outputFormat {
	case OutputFormatDockerScript:
		return fmt.Sprintf(`#!/bin/sh
docker buildx build -t %s - <<'EOF'
%s
EOF
`, imageName, dockerfile)
	case OutputFormatBuildahScript:
		return fmt.Sprintf(`#!/bin/sh
buildah bud -t %s - <<'EOF'
%s
EOF
`, imageName, dockerfile)
	case OutputFormatPodmanScript:
		return fmt.Sprintf(`#!/bin/sh
podman buildx build -t %s - <<'EOF'
%s
EOF
`, imageName, dockerfile)
	default:
		return ""
	}
}
