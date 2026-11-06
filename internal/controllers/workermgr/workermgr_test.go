/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

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

package workermgr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVFIOWorkerQuotesConfiguredDeviceIDsForBindingAndDetection(t *testing.T) {
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "test-namespace"},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				DriverType: utils.DriverTypeVFPassthrough,
				VFIOConfig: amdv1alpha1.VFIOConfigSpec{DeviceIDs: []string{"75b3; echo injected"}},
			},
		},
	}

	pod := (&workerMgr{}).getPodDef(devConfig, "test-node", utils.LoadVFIOAction)
	bindCommand := pod.Spec.Containers[0].Command[2]
	detectCommand := pod.Spec.InitContainers[0].Command[2]

	if !strings.Contains(bindCommand, "'75b3; echo injected'") {
		t.Fatalf("bind command does not contain configured VF ID: %s", bindCommand)
	}
	if !strings.Contains(detectCommand, "-- '75b3; echo injected'") {
		t.Fatalf("detect command does not contain configured VF ID: %s", detectCommand)
	}
}

func TestVFIOWorkerDetectsConfiguredIDsUsingLspciSelectors(t *testing.T) {
	tests := []struct {
		name      string
		deviceIDs []string
		match     string
		want      []string
	}{
		{
			name:      "lowercase",
			deviceIDs: []string{"74b5"},
			match:     "1002:74b5",
			want:      []string{"1002:74b5"},
		},
		{
			name:      "uppercase",
			deviceIDs: []string{"74B5"},
			match:     "1002:74B5",
			want:      []string{"1002:74B5"},
		},
		{
			name:      "prefixed",
			deviceIDs: []string{"0x74b5"},
			match:     "1002:0x74b5",
			want:      []string{"1002:0x74b5"},
		},
		{
			name:      "second configured ID matches",
			deviceIDs: []string{"74B5", "0x74b5"},
			match:     "1002:0x74b5",
			want:      []string{"1002:74B5", "1002:0x74b5"},
		},
		{
			name:      "shell metacharacters remain one argument",
			deviceIDs: []string{"75b3; echo injected"},
			match:     "1002:75b3; echo injected",
			want:      []string{"1002:75b3; echo injected"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			devConfig := &amdv1alpha1.DeviceConfig{
				Spec: amdv1alpha1.DeviceConfigSpec{
					Driver: amdv1alpha1.DriverSpec{
						DriverType: utils.DriverTypeVFPassthrough,
						VFIOConfig: amdv1alpha1.VFIOConfigSpec{DeviceIDs: tt.deviceIDs},
					},
				},
			}
			pod := (&workerMgr{}).getPodDef(devConfig, "test-node", utils.LoadVFIOAction)
			detectCommand := pod.Spec.InitContainers[0].Command[2]
			if !strings.Contains(detectCommand, "timeout 10m") || !strings.Contains(detectCommand, "lspci -nn -d \"1002:$product\"") {
				t.Fatalf("detect command is not bounded to AMD devices: %s", detectCommand)
			}
			if pod.Spec.RestartPolicy != v1.RestartPolicyNever {
				t.Fatalf("worker pod restart policy is %q, want Never", pod.Spec.RestartPolicy)
			}
			if pod.Spec.ServiceAccountName != utilsContainerSA {
				t.Fatalf("worker pod service account is %q, want %s", pod.Spec.ServiceAccountName, utilsContainerSA)
			}

			got := runVFDetection(t, detectCommand, tt.match)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("detector lspci selectors = %v, want %v; command: %s", got, tt.want, detectCommand)
			}
		})
	}
}

func TestVFIOWorkerQuotesCompleteSelectorInEmbeddedScripts(t *testing.T) {
	want := "lspci -nn -d \"1002:${PRODUCT_CODE}\""
	for name, script := range map[string]string{
		"bind":   vfioBindScript,
		"unbind": vfioUnbindScript,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(script, want) {
				t.Fatalf("script does not quote complete PCI selector: %s", script)
			}
			if name == "bind" && !strings.Contains(script, "if [ -e \"/sys/bus/pci/devices/$VFIO_DEVICE/driver/unbind\" ]; then") {
				t.Fatalf("bind script does not use a valid unbind guard: %s", script)
			}
			if strings.Contains(script, "if [-e") {
				t.Fatalf("script contains malformed unbind guard: %s", script)
			}
			if output, err := exec.Command("bash", "-n", "-c", script).CombinedOutput(); err != nil {
				t.Fatalf("script has invalid bash syntax: %v: %s", err, output)
			}
		})
	}
}

func TestVFIOWorkerUsesConfiguredIDsInBindAndUnbindCommands(t *testing.T) {
	devConfig := &amdv1alpha1.DeviceConfig{
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				DriverType: utils.DriverTypeVFPassthrough,
				VFIOConfig: amdv1alpha1.VFIOConfigSpec{DeviceIDs: []string{"74B5", "0x74b5"}},
			},
		},
	}

	bindPod := (&workerMgr{}).getPodDef(devConfig, "test-node", utils.LoadVFIOAction)
	unbindPod := (&workerMgr{}).getPodDef(devConfig, "test-node", utils.UnloadVFIOAction)
	if !strings.Contains(bindPod.Spec.Containers[0].Command[2], "failed to bind GPU VF device") {
		t.Fatal("bind command does not fail when vfio-pci binding is not effective")
	}
	if !strings.Contains(unbindPod.Spec.Containers[0].Command[2], "failed to unbind GPU device") || !strings.Contains(unbindPod.Spec.Containers[0].Command[2], "remains bound") {
		t.Fatal("unbind command does not fail when vfio-pci unbinding is not effective")
	}
	for name, command := range map[string]string{
		"bind":   bindPod.Spec.Containers[0].Command[2],
		"unbind": unbindPod.Spec.Containers[0].Command[2],
	} {
		t.Run(name, func(t *testing.T) {
			for _, deviceID := range []string{"'74B5'", "'0x74b5'"} {
				if !strings.Contains(command, deviceID) {
					t.Fatalf("command does not contain configured ID %s: %s", deviceID, command)
				}
			}
			if output, err := exec.Command("bash", "-n", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("command has invalid bash syntax: %v: %s", err, output)
			}
		})
	}
}

func runVFDetection(t *testing.T, detectCommand, match string) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	lspciPath := filepath.Join(tempDir, "lspci")
	sleepPath := filepath.Join(tempDir, "sleep")
	argsPath := filepath.Join(tempDir, "lspci.args")
	lspciScript := "#!/bin/sh\n" +
		"set -eu\n" +
		"if [ \"${2:-}\" = \"-d\" ]; then\n" +
		"  printf '%s\\n' \"${3:-}\" >> \"$LSPCI_ARGS\"\n" +
		"fi\n" +
		"if [ \"${3:-}\" = \"$LSPCI_MATCH\" ]; then\n" +
		"  printf '0000:01:00.0 Processing accelerators [1002:%s]\\n' \"${3#1002:}\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(lspciPath, []byte(lspciScript), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sleepPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(ctx, "sh", "-c", detectCommand)
	command.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"LSPCI_ARGS="+argsPath,
		"LSPCI_MATCH="+match,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("detector command failed: %v: %s", err, output)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
