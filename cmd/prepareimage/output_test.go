package main

import (
	"strings"
	"testing"

	"github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"

	utils "github.com/ROCm/gpu-operator/internal"
)

const (
	ubuntuPrettyName   = "Ubuntu 24.04.5 LTS"
	ubuntuKernel       = "6.8.0-139-generic"
	coreOSPrettyName   = "Red Hat Enterprise Linux CoreOS 9.6.20250121-0 (Plow)"
	coreOSKernel       = "5.14.0-570.12.1.el9_6.x86_64"
	driverToolkitImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sourceImageRepo    = "docker.io/rocm/amdgpu-driver"
	slesPrettyName     = "SUSE Linux Enterprise Server 15 SP7"
	slesKernel         = "6.4.0-150700.53.25-default"
	driversVersion     = "31.40.1"
	imageName          = "registry.example.com/project/amdgpu"
)

func TestCreateOutput(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		wantErrSubstring string
		// wantLines must each match a complete line of the output.
		wantLines        []string
		wantSubstrings   []string
		unwantSubstrings []string
	}{
		{
			name: "ubuntu container dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantLines: []string{
				"FROM docker.io/ubuntu:24.04 as builder",
				"FROM docker.io/ubuntu:24.04",
				"ARG KERNEL_FULL_VERSION=" + ubuntuKernel,
				"ARG DRIVERS_VERSION=" + driversVersion,
				"ARG REPO_URL=https://repo.radeon.com",
				// Optional build args without a value must remain unset, so the template falls back to REPO_URL.
				"ARG PACKAGE_REPO_URL",
				"ARG GPG_KEY_URL",
			},
			wantSubstrings: []string{
				"/ubuntu noble main",
			},
			unwantSubstrings: []string{
				"#!/bin/sh",
				"$$",
				"ARG PACKAGE_REPO_URL=",
				"ARG GPG_KEY_URL=",
			},
		},
		{
			name: "ubuntu container dockerfile with custom repo, package repo, and GPG key URLs",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "Ubuntu 22.04.4 LTS",
				buildArgs: BuildArgs{
					kernelFullVersion: "5.15.0-122-generic",
					driversVersion:    "30.20.1",
					repoURL:           "https://mirror.example.com",
					packageRepoURL:    "https://mirror.example.com/amdgpu/30.20.1/ubuntu jammy main",
					gpgKeyURL:         "https://mirror.example.com/rocm/rocm.gpg.key",
				},
			},
			wantLines: []string{
				"FROM docker.io/ubuntu:22.04 as builder",
				"FROM docker.io/ubuntu:22.04",
				"ARG KERNEL_FULL_VERSION=5.15.0-122-generic",
				"ARG DRIVERS_VERSION=30.20.1",
				"ARG REPO_URL=https://mirror.example.com",
				"ARG PACKAGE_REPO_URL=https://mirror.example.com/amdgpu/30.20.1/ubuntu jammy main",
				"ARG GPG_KEY_URL=https://mirror.example.com/rocm/rocm.gpg.key",
			},
			wantSubstrings: []string{
				"/ubuntu jammy main",
			},
			unwantSubstrings: []string{
				"$$",
			},
		},
		{
			name: "ubuntu vf-passthrough dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeVFPassthrough,
				osPrettyName: ubuntuPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    "8.1.0.K",
				},
			},
			wantLines: []string{
				"FROM docker.io/ubuntu:24.04 AS builder",
				"FROM docker.io/ubuntu:24.04",
				"ARG KERNEL_FULL_VERSION=" + ubuntuKernel,
				"ARG DRIVERS_VERSION=8.1.0.K",
				"ARG REPO_URL=https://repo.radeon.com",
			},
			wantSubstrings: []string{
				"gim-dkms_${DRIVERS_VERSION}_all.deb",
			},
			unwantSubstrings: []string{
				"$$",
				"amdgpu-dkms",
			},
		},
		{
			name: "coreos container dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: coreOSPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion:  coreOSKernel,
					driversVersion:     driversVersion,
					packageRepoURL:     "https://mirror.example.com/amdgpu/31.40.1/el/9.6/main/x86_64/",
					gpgKeyURL:          "https://mirror.example.com/rocm/rocm.gpg.key",
					driverToolkitImage: driverToolkitImage,
				},
			},
			wantLines: []string{
				"ARG DTK_AUTO=" + driverToolkitImage,
				"FROM ${DTK_AUTO} as builder",
				"ARG KERNEL_VERSION=" + coreOSKernel,
				"ARG DRIVERS_VERSION=" + driversVersion,
				"ARG REPO_URL=https://repo.radeon.com",
				"ARG PACKAGE_REPO_URL=https://mirror.example.com/amdgpu/31.40.1/el/9.6/main/x86_64/",
				"ARG GPG_KEY_URL=https://mirror.example.com/rocm/rocm.gpg.key",
			},
			wantSubstrings: []string{
				"dnf install amdgpu-dkms -y",
			},
			unwantSubstrings: []string{
				"$$",
				"ARG DTK_AUTO\n",
				"ARG KERNEL_VERSION\n",
				"gim-dkms",
			},
		},
		{
			name: "coreos vf-passthrough dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeVFPassthrough,
				osPrettyName: coreOSPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion:  coreOSKernel,
					driversVersion:     "8.1.0.K",
					driverToolkitImage: driverToolkitImage,
				},
			},
			wantLines: []string{
				"ARG DTK_AUTO=" + driverToolkitImage,
				"FROM ${DTK_AUTO} as builder",
				"ARG KERNEL_VERSION=" + coreOSKernel,
				"ARG DRIVERS_VERSION=8.1.0.K",
				"ARG REPO_URL=https://repo.radeon.com",
			},
			wantSubstrings: []string{
				"gim-dkms-${DRIVERS_VERSION}-0.noarch.rpm",
			},
			unwantSubstrings: []string{
				"$$",
				"ARG DTK_AUTO\n",
				"ARG KERNEL_VERSION\n",
				"amdgpu-dkms",
			},
		},
		{
			name: "coreos source image dockerfile",
			config: &Config{
				OutputFormat:   OutputFormatDockerfile,
				driverType:     utils.DriverTypeContainer,
				osPrettyName:   coreOSPrettyName,
				useSourceImage: true,
				buildArgs: BuildArgs{
					kernelFullVersion:  coreOSKernel,
					driversVersion:     driversVersion,
					driverToolkitImage: driverToolkitImage,
				},
			},
			wantLines: []string{
				"ARG DTK_AUTO=" + driverToolkitImage,
				"ARG RHEL_VERSION=9.6",
				"ARG SOURCE_IMAGE_REPO=" + sourceImageRepo,
				"ARG DRIVERS_VERSION=" + driversVersion,
				"ARG KERNEL_VERSION=" + coreOSKernel,
				"FROM ${SOURCE_IMAGE_REPO}:coreos-${RHEL_VERSION}-${DRIVERS_VERSION} as sources",
				"FROM ${DTK_AUTO} as builder",
			},
			wantSubstrings: []string{
				"RUN make modules",
			},
			unwantSubstrings: []string{
				"$$",
				"ARG DTK_AUTO\n",
				"ARG RHEL_VERSION\n",
				"ARG KERNEL_VERSION\n",
				"amdgpu-dkms",
			},
		},
		{
			name: "coreos source image dockerfile without --source-image-repo",
			config: &Config{
				OutputFormat:   OutputFormatDockerfile,
				driverType:     utils.DriverTypeContainer,
				osPrettyName:   coreOSPrettyName,
				useSourceImage: true,
				buildArgs: BuildArgs{
					kernelFullVersion:  coreOSKernel,
					driversVersion:     driversVersion,
					driverToolkitImage: driverToolkitImage,
					sourceImageRepo:    sourceImageRepo,
				},
			},
			wantLines: []string{
				"ARG SOURCE_IMAGE_REPO=" + sourceImageRepo,
				"FROM ${SOURCE_IMAGE_REPO}:coreos-${RHEL_VERSION}-${DRIVERS_VERSION} as sources",
			},
			unwantSubstrings: []string{
				"ARG SOURCE_IMAGE_REPO\n",
				"amdgpu-dkms",
			},
		},
		{
			name: "coreos source image dockerfile with --source-image-repo",
			config: &Config{
				OutputFormat:   OutputFormatDockerfile,
				driverType:     utils.DriverTypeContainer,
				osPrettyName:   coreOSPrettyName,
				useSourceImage: true,
				buildArgs: BuildArgs{
					kernelFullVersion:  coreOSKernel,
					driversVersion:     driversVersion,
					driverToolkitImage: driverToolkitImage,
					sourceImageRepo:    "registry.example.com/mirror/amdgpu-driver",
				},
			},
			wantLines: []string{
				"ARG SOURCE_IMAGE_REPO=registry.example.com/mirror/amdgpu-driver",
				"FROM ${SOURCE_IMAGE_REPO}:coreos-${RHEL_VERSION}-${DRIVERS_VERSION} as sources",
			},
			unwantSubstrings: []string{
				sourceImageRepo,
				"amdgpu-dkms",
			},
		},
		{
			name: "sles dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: slesPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: slesKernel,
					driversVersion:    "30.10.1",
				},
			},
			wantLines: []string{
				"ARG SUSE_PREBUILT_DRIVER_IMG=registry.suse.com/third-party/amd/amdgpu-driver:sles-15.7-6.4.0-150700.51-default-30.10.1",
				"FROM ${SUSE_PREBUILT_DRIVER_IMG} AS driver-source",
				"FROM registry.suse.com/bci/bci-micro:15.7",
				"ARG KERNEL_FULL_VERSION=" + slesKernel,
			},
			unwantSubstrings: []string{
				"$$",
				"ARG KERNEL_FULL_VERSION\n",
				"ARG SUSE_PREBUILT_DRIVER_IMG\n",
			},
		},
		{
			name: "sles 16 dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "SUSE Linux Enterprise Server 16.0",
				buildArgs: BuildArgs{
					kernelFullVersion: "6.12.0-160000.6-default",
					driversVersion:    "30.10.1",
				},
			},
			wantLines: []string{
				"ARG SUSE_PREBUILT_DRIVER_IMG=registry.suse.com/third-party/amd/amdgpu-driver:sles-16.0-6.12.0-160000.5-default-30.10.1",
				"FROM registry.suse.com/bci/bci-micro:16.0",
				"ARG KERNEL_FULL_VERSION=6.12.0-160000.6-default",
			},
			unwantSubstrings: []string{
				"$$",
			},
		},
		{
			name: "docker-script",
			config: &Config{
				OutputFormat: OutputFormatDockerScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantLines: []string{
				"#!/bin/sh",
				"docker buildx build -t " + imageName + ":ubuntu-24.04-6.8.0-139-generic-31.40.1 - <<'EOF'",
				"FROM docker.io/ubuntu:24.04 as builder",
				"ARG KERNEL_FULL_VERSION=" + ubuntuKernel,
				"ARG DRIVERS_VERSION=" + driversVersion,
				"EOF",
			},
		},
		{
			name: "buildah-script",
			config: &Config{
				OutputFormat: OutputFormatBuildahScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantLines: []string{
				"#!/bin/sh",
				"buildah bud -t " + imageName + ":ubuntu-24.04-6.8.0-139-generic-31.40.1 - <<'EOF'",
				"ARG KERNEL_FULL_VERSION=" + ubuntuKernel,
				"EOF",
			},
		},
		{
			name: "podman-script",
			config: &Config{
				OutputFormat: OutputFormatPodmanScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantLines: []string{
				"#!/bin/sh",
				"podman buildx build -t " + imageName + ":ubuntu-24.04-6.8.0-139-generic-31.40.1 - <<'EOF'",
				"ARG KERNEL_FULL_VERSION=" + ubuntuKernel,
				"EOF",
			},
		},
		{
			name: "docker-script includes driver type in image tag",
			config: &Config{
				OutputFormat: OutputFormatDockerScript,
				driverType:   utils.DriverTypeVFPassthrough,
				osPrettyName: ubuntuPrettyName,
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    "8.1.0.K",
				},
			},
			wantLines: []string{
				"docker buildx build -t " + imageName + ":ubuntu-24.04-6.8.0-139-generic-vf-passthrough-8.1.0.K - <<'EOF'",
			},
		},
		{
			name: "docker-script for sles",
			config: &Config{
				OutputFormat: OutputFormatDockerScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: slesPrettyName,
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: slesKernel,
					driversVersion:    "30.10.1",
				},
			},
			wantLines: []string{
				"docker buildx build -t " + imageName + ":sles-15.7-6.4.0-150700.53.25-default-30.10.1 - <<'EOF'",
				"ARG KERNEL_FULL_VERSION=" + slesKernel,
			},
		},
		{
			name: "script without image name",
			config: &Config{
				OutputFormat: OutputFormatDockerScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantErrSubstring: "image name is required",
		},
		{
			name: "coreos without driver toolkit image",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: coreOSPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: coreOSKernel,
					driversVersion:    driversVersion,
				},
			},
			wantErrSubstring: "driver toolkit image is required",
		},
		{
			name: "unknown output format",
			config: &Config{
				OutputFormat: "foo",
				driverType:   utils.DriverTypeContainer,
				osPrettyName: ubuntuPrettyName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantErrSubstring: "unknown output format",
		},
		{
			name: "unsupported ubuntu version",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "Ubuntu 18.04.6 LTS",
				buildArgs: BuildArgs{
					kernelFullVersion: "4.15.0-213-generic",
					driversVersion:    driversVersion,
				},
			},
			wantErrSubstring: "invalid ubuntu version",
		},
		{
			name: "rhel dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "Red Hat Enterprise Linux 9.4 (Plow)",
				buildArgs: BuildArgs{
					kernelFullVersion:  "5.14.0-427.13.1.el9_4.x86_64",
					driversVersion:     driversVersion,
					driverToolkitImage: driverToolkitImage,
				},
			},
			wantLines: []string{
				"ARG DTK_AUTO=" + driverToolkitImage,
				"FROM ${DTK_AUTO} as builder",
				"ARG KERNEL_VERSION=5.14.0-427.13.1.el9_4.x86_64",
				"ARG DRIVERS_VERSION=" + driversVersion,
				"ARG REPO_URL=https://repo.radeon.com",
			},
			wantSubstrings: []string{
				"dnf install amdgpu-dkms -y",
			},
			unwantSubstrings: []string{
				"$$",
				"ARG DTK_AUTO\n",
				"ARG KERNEL_VERSION\n",
			},
		},
		{
			name: "unknown OS",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "foo",
				imageName:    imageName,
				buildArgs: BuildArgs{
					kernelFullVersion: ubuntuKernel,
					driversVersion:    driversVersion,
				},
			},
			wantErrSubstring: "not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := createOutput(tt.config, scheme)

			if tt.wantErrSubstring != "" {
				if err == nil {
					t.Fatalf("createOutput returned nil error; got output:\n%s", output)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstring) {
					t.Errorf("error %q does not contain %q", err, tt.wantErrSubstring)
				}
				if output != "" {
					t.Errorf("expected empty output on error, got %q", output)
				}
				return
			}

			if err != nil {
				t.Fatalf("createOutput returned unexpected error: %v", err)
			}
			assertOutput(t, output, tt.wantLines, tt.wantSubstrings, tt.unwantSubstrings)
		})
	}
}

func TestSetBuildArgDefaults(t *testing.T) {
	dockerfile := strings.Join([]string{
		"ARG REPO_URL",
		"ARG PACKAGE_REPO_URL",
		"FROM base",
		"ARG KERNEL_FULL_VERSION",
		"ARG KERNEL_VERSION",
		"ARG KERNEL_VERSION_SUFFIX",
		"ARG UNSET",
		"ARG UNSET_WITH_DEFAULT=template-default",
		"ARG SOURCE_IMAGE_REPO=docker.io/rocm/amdgpu-driver",
		"ARG DOLLAR",
		"",
	}, "\n")

	output := setBuildArgDefaults(
		[]v1beta1.BuildArg{
			{Name: "REPO_URL", Value: "https://repo.example.com"},
			{Name: "PACKAGE_REPO_URL", Value: "https://pkg.example.com"},
			{Name: "KERNEL_FULL_VERSION", Value: "6.8.0-139-generic"},
			{Name: "KERNEL_VERSION", Value: "6.8.0-139-generic"},
			{Name: "UNSET", Value: ""},
			{Name: "UNSET_WITH_DEFAULT", Value: ""},
			{Name: "SOURCE_IMAGE_REPO", Value: sourceImageRepo},
			{Name: "DOLLAR", Value: "${FOO}$1"},
		},
		dockerfile,
	)

	want := strings.Join([]string{
		"ARG REPO_URL=https://repo.example.com",
		"ARG PACKAGE_REPO_URL=https://pkg.example.com",
		"FROM base",
		"ARG KERNEL_FULL_VERSION=6.8.0-139-generic",
		"ARG KERNEL_VERSION=6.8.0-139-generic",
		"ARG KERNEL_VERSION_SUFFIX",
		"ARG UNSET",
		"ARG UNSET_WITH_DEFAULT=template-default",
		"ARG SOURCE_IMAGE_REPO=docker.io/rocm/amdgpu-driver",
		"ARG DOLLAR=${FOO}$1",
		"",
	}, "\n")
	if output != want {
		t.Errorf("setBuildArgDefaults output mismatch\ngot:\n%s\nwant:\n%s", output, want)
	}
}

func assertOutput(t *testing.T, output string, wantLines, wantSubstrings, unwantSubstrings []string) {
	t.Helper()

	lines := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		lines[strings.TrimSpace(line)] = true
	}
	for _, want := range wantLines {
		if !lines[want] {
			t.Errorf("output missing line %q; got:\n%s", want, output)
		}
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got:\n%s", want, output)
		}
	}
	for _, unwant := range unwantSubstrings {
		if strings.Contains(output, unwant) {
			t.Errorf("output unexpectedly contains %q; got:\n%s", unwant, output)
		}
	}
}
