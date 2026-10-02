package main

import (
	"strings"
	"testing"

	utils "github.com/ROCm/gpu-operator/internal"
)

func TestCreateOutput(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		wantErrSubstring string
		wantSubstrings   []string
		unwantSubstrings []string
	}{
		{
			name: "dockerfile",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "Ubuntu 24.04.5 LTS",
				imageName:    "registry.example.com/project/amdgpu",
				buildArgs: BuildArgs{
					kernelFullVersion: "6.8.0-139-generic",
					driversVersion:    "31.40.1",
				},
			},
			wantSubstrings: []string{
				"FROM docker.io/ubuntu:24.04",
				"ARG KERNEL_FULL_VERSION=6.8.0-139-generic",
				"ARG DRIVERS_VERSION=31.40.1",
			},
			unwantSubstrings: []string{
				"#!/bin/sh",
			},
		},
		{
			name: "docker-script",
			config: &Config{
				OutputFormat: OutputFormatDockerScript,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "Ubuntu 24.04.5 LTS",
				imageName:    "registry.example.com/project/amdgpu",
				buildArgs: BuildArgs{
					kernelFullVersion: "6.8.0-139-generic",
					driversVersion:    "31.40.1",
				},
			},
			wantSubstrings: []string{
				"#!/bin/sh",
				"docker buildx build -t registry.example.com/project/amdgpu:ubuntu-24.04-6.8.0-139-generic-31.40.1 - <<'EOF'",
				"FROM docker.io/ubuntu:24.04",
				"ARG KERNEL_FULL_VERSION=6.8.0-139-generic",
				"ARG DRIVERS_VERSION=31.40.1",
				"EOF",
			},
		},
		{
			name: "unknown OS",
			config: &Config{
				OutputFormat: OutputFormatDockerfile,
				driverType:   utils.DriverTypeContainer,
				osPrettyName: "foo",
				imageName:    "registry.example.com/project/amdgpu",
				buildArgs: BuildArgs{
					kernelFullVersion: "6.8.0-139-generic",
					driversVersion:    "31.40.1",
				},
			},
			wantErrSubstring: "not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := createOutput(tt.config)

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
			for _, want := range tt.wantSubstrings {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q; got:\n%s", want, output)
				}
			}
			for _, unwant := range tt.unwantSubstrings {
				if strings.Contains(output, unwant) {
					t.Errorf("output unexpectedly contains %q; got:\n%s", unwant, output)
				}
			}
		})
	}
}
