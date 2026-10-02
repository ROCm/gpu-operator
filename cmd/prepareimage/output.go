package main

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
)

func createOutput(config *Config) (string, error) {
	deviceConfig := deriveDeviceConfig(config)

	osName, err := deriveOSName(config.osPrettyName, deviceConfig)
	if err != nil {
		return "", fmt.Errorf("failed to derive OS name from OS pretty name %q: %w", config.osPrettyName, err)
	}

	buildCM := deriveBuildConfigMap(osName, deviceConfig)

	dockerfile, err := deriveDockerfile(config, buildCM, deviceConfig, scheme)
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
		imageName := deriveImageNameWithTag(config, osName)
		output = deriveScript(config.OutputFormat, imageName, dockerfile)
	default:
		return "", fmt.Errorf("unknown output format %q; valid values are: dockerfile, docker-script, buildah-script, podman-script", config.OutputFormat)
	}
	return output, nil
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
					PackageRepoURL: config.buildArgs.packageRepoURL,
					GPGKeyURL:      config.buildArgs.gpgKeyURL,
				},
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

func deriveDockerfile(config *Config, buildCM *corev1.ConfigMap, deviceConfig *gpuev1alpha1.DeviceConfig, scheme *runtime.Scheme) (string, error) {
	kmmModule := kmmmodule.NewKMMModule(
		nil,    // The client is not used by any of the kmmModule methods we call.
		scheme, // A correctly initialized scheme is required by SetBuildConfigMapAsDesired.
		false,  // Dockerfiles for all supported OSes can be derived without this input.
	)

	err := kmmModule.SetBuildConfigMapAsDesired(buildCM, deviceConfig)
	if err != nil {
		return "", fmt.Errorf("failed to set BuildConfigMap as desired: %v", err)
	}

	dockerfile, ok := buildCM.Data["dockerfile"]
	if !ok {
		return "", fmt.Errorf("failed to get Dockerfile from BuildConfigMap")
	}

	return setBuildArgDefaults(config, dockerfile), nil
}

func setBuildArgDefaults(config *Config, dockerfile string) string {
	buildArgReplacements := []string{}

	if config.buildArgs.kernelFullVersion != "" {
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG KERNEL_FULL_VERSION",
			fmt.Sprintf("ARG KERNEL_FULL_VERSION=%s", config.buildArgs.kernelFullVersion),
		)

		// OpenShift Dockerfile templates use KERNEL_VERSION instead of KERNEL_FULL_VERSION,
		// but the accepted values should be identical.
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG KERNEL_VERSION",
			fmt.Sprintf("ARG KERNEL_VERSION=%s", config.buildArgs.kernelFullVersion),
		)
	}
	if config.buildArgs.driversVersion != "" {
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG DRIVERS_VERSION",
			fmt.Sprintf("ARG DRIVERS_VERSION=%s", config.buildArgs.driversVersion),
		)
	}
	if config.buildArgs.repoURL != "" {
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG REPO_URL",
			fmt.Sprintf("ARG REPO_URL=%s", config.buildArgs.repoURL),
		)
	}
	if config.buildArgs.packageRepoURL != "" {
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG PACKAGE_REPO_URL",
			fmt.Sprintf("ARG PACKAGE_REPO_URL=%s", config.buildArgs.packageRepoURL),
		)
	}
	if config.buildArgs.gpgKeyURL != "" {
		buildArgReplacements = append(
			buildArgReplacements,
			"ARG GPG_KEY_URL",
			fmt.Sprintf("ARG GPG_KEY_URL=%s", config.buildArgs.gpgKeyURL),
		)
	}

	return strings.NewReplacer(buildArgReplacements...).Replace(dockerfile)
}

func deriveOSName(osPrettyName string, deviceConfig *gpuev1alpha1.DeviceConfig) (string, error) {
	node := corev1.Node{
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				OSImage: osPrettyName,
			},
		},
	}
	return kmmmodule.GetOSName(node, deviceConfig)
}

func deriveImageTag(osName, kernelVersion, driversVersion string) string {
	return fmt.Sprintf("%s-%s-%s", osName, kernelVersion, driversVersion)
}

func deriveImageNameWithTag(config *Config, osName string) string {
	tag := deriveImageTag(osName, config.buildArgs.kernelFullVersion, config.buildArgs.driversVersion)
	return fmt.Sprintf("%s:%s", config.imageName, tag)
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
