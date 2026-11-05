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

package controllers

import (
	"testing"

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

func TestDeviceConfigReadiness(t *testing.T) {
	newNodes := func() *v1.NodeList {
		return &v1.NodeList{Items: []v1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
		}}
	}
	newDeviceConfig := func(driverType string) *amdv1alpha1.DeviceConfig {
		return &amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "ns"},
			Spec:       amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{DriverType: driverType}},
		}
	}

	t.Run("container mode is ready", func(t *testing.T) {
		ready, _, _ := deviceConfigReadiness(newDeviceConfig(utils.DriverTypeContainer), newNodes(), nil, true)
		if !ready {
			t.Fatal("container mode should not wait for KMM or VFIO")
		}
	})

	t.Run("vf passthrough waits for KMM", func(t *testing.T) {
		ready, reason, _ := deviceConfigReadiness(newDeviceConfig(utils.DriverTypeVFPassthrough), newNodes(), nil, true)
		if ready || reason != "KMMModulePending" {
			t.Fatalf("got ready=%v reason=%q, want not ready with KMMModulePending", ready, reason)
		}
	})

	t.Run("vf passthrough waits for every node", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		nodes := newNodes()
		module := &kmmv1beta1.Module{Status: kmmv1beta1.ModuleStatus{
			ModuleLoader: kmmv1beta1.DaemonSetStatus{AvailableNumber: 2},
		}}
		for i := range nodes.Items {
			nodes.Items[i].Labels = map[string]string{"kmm.node.kubernetes.io/ns.dc.ready": ""}
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true)
		if ready || reason != "VFIOBindingPending" {
			t.Fatalf("got ready=%v reason=%q, want not ready with VFIOBindingPending", ready, reason)
		}
	})

	t.Run("vf passthrough is ready only after KMM and VFIO labels", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		nodes := newNodes()
		module := &kmmv1beta1.Module{Status: kmmv1beta1.ModuleStatus{
			ModuleLoader: kmmv1beta1.DaemonSetStatus{AvailableNumber: 2},
		}}
		for i := range nodes.Items {
			nodes.Items[i].Labels = map[string]string{
				"kmm.node.kubernetes.io/ns.dc.ready":    "",
				"gpu.operator.amd.com/ns.dc.vfio.ready": "",
			}
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with OperatorReady", ready, reason)
		}
	})

	t.Run("pf passthrough requires VFIO labels", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypePFPassthrough)
		nodes := newNodes()
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, nil, false)
		if ready || reason != "VFIOBindingPending" {
			t.Fatalf("got ready=%v reason=%q, want not ready without VFIO labels", ready, reason)
		}
		for i := range nodes.Items {
			nodes.Items[i].Labels = map[string]string{"gpu.operator.amd.com/ns.dc.vfio.ready": ""}
		}
		ready, reason, _ = deviceConfigReadiness(devConfig, nodes, nil, false)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with VFIO labels", ready, reason)
		}
	})

	t.Run("vf passthrough can use an externally managed driver", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		nodes := newNodes()
		for i := range nodes.Items {
			nodes.Items[i].Labels = map[string]string{"gpu.operator.amd.com/ns.dc.vfio.ready": ""}
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, nil, false)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with externally managed driver", ready, reason)
		}
	})
}
