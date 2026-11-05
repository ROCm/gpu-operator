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
	"strings"
	"testing"

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

func TestVFIOWorkerBoundsVFDetectionAndFiltersAMDDevices(t *testing.T) {
	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "test-namespace"},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				DriverType: utils.DriverTypeVFPassthrough,
				VFIOConfig: amdv1alpha1.VFIOConfigSpec{DeviceIDs: []string{"74b5"}},
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
	if pod.Spec.ServiceAccountName != "amd-gpu-operator-utils-container" {
		t.Fatalf("worker pod service account is %q, want amd-gpu-operator-utils-container", pod.Spec.ServiceAccountName)
	}
	if !strings.Contains(pod.Spec.Containers[0].Command[2], "failed to bind GPU VF device") {
		t.Fatal("bind command does not fail when vfio-pci binding is not effective")
	}

	unloadPod := (&workerMgr{}).getPodDef(devConfig, "test-node", utils.UnloadVFIOAction)
	if !strings.Contains(unloadPod.Spec.Containers[0].Command[2], "failed to unbind GPU device") || !strings.Contains(unloadPod.Spec.Containers[0].Command[2], "remains bound") {
		t.Fatal("unbind command does not fail when vfio-pci unbinding is not effective")
	}
}
