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
	"fmt"
	"testing"

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
		ready, _, _ := deviceConfigReadiness(newDeviceConfig(utils.DriverTypeContainer), newNodes(), nil, true, nil)
		if !ready {
			t.Fatal("container mode should not wait for KMM or VFIO")
		}
	})

	t.Run("disabled passthrough does not wait for VFIO", func(t *testing.T) {
		disabled := false
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.Driver.Enable = &disabled
		ready, reason, _ := deviceConfigReadiness(devConfig, newNodes(), nil, true, nil)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with OperatorReady", ready, reason)
		}
	})

	t.Run("vf passthrough waits for KMM", func(t *testing.T) {
		ready, reason, _ := deviceConfigReadiness(newDeviceConfig(utils.DriverTypeVFPassthrough), newNodes(), nil, true, nil)
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
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, nil)
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
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, nil)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with OperatorReady", ready, reason)
		}
	})

	t.Run("pf passthrough requires VFIO labels", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypePFPassthrough)
		nodes := newNodes()
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, nil, false, nil)
		if ready || reason != "VFIOBindingPending" {
			t.Fatalf("got ready=%v reason=%q, want not ready without VFIO labels", ready, reason)
		}
		for i := range nodes.Items {
			nodes.Items[i].Labels = map[string]string{"gpu.operator.amd.com/ns.dc.vfio.ready": ""}
		}
		ready, reason, _ = deviceConfigReadiness(devConfig, nodes, nil, false, nil)
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
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, nil, false, nil)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want ready with externally managed driver", ready, reason)
		}
	})

	t.Run("vf passthrough waits for an available device plugin", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		ready, reason, message := deviceConfigReadiness(devConfig, nodes, module, true, nil)
		if ready || reason != "DevicePluginPending" || message == "" {
			t.Fatalf("got ready=%v reason=%q message=%q, want DevicePluginPending", ready, reason, message)
		}
	})

	t.Run("vf passthrough waits for an AMD allocatable resource", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		devicePlugin := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{NumberAvailable: 2, UpdatedNumberScheduled: 2}}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if ready || reason != "DevicePluginPending" {
			t.Fatalf("got ready=%v reason=%q, want DevicePluginPending", ready, reason)
		}
	})

	t.Run("vf passthrough is ready with an AMD resource", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
			}
		}
		devicePlugin := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{NumberAvailable: 2, UpdatedNumberScheduled: 2}}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want OperatorReady", ready, reason)
		}
	})

	t.Run("vf passthrough accepts partition-style AMD resources", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/cpx_nps4"): resource.MustParse("1"),
			}
		}
		devicePlugin := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{NumberAvailable: 2, UpdatedNumberScheduled: 2}}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want OperatorReady", ready, reason)
		}
	})

	t.Run("vf passthrough waits for current rolling daemonset generation", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
			}
		}
		devicePlugin := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Generation: 2},
			Status: appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				NumberAvailable:        2,
				UpdatedNumberScheduled: 1,
			},
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if ready || reason != "DevicePluginPending" {
			t.Fatalf("got ready=%v reason=%q, want DevicePluginPending", ready, reason)
		}
	})

	t.Run("vf passthrough permits a ready OnDelete daemonset", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
			}
		}
		devicePlugin := &appsv1.DaemonSet{
			Spec:   appsv1.DaemonSetSpec{UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType}},
			Status: appsv1.DaemonSetStatus{NumberAvailable: 2},
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if !ready || reason != "OperatorReady" {
			t.Fatalf("got ready=%v reason=%q, want OperatorReady", ready, reason)
		}
	})

	t.Run("vf passthrough waits for a terminating daemonset", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
			}
		}
		deletionTimestamp := metav1.Now()
		devicePlugin := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Generation: 1, DeletionTimestamp: &deletionTimestamp},
			Status: appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				NumberAvailable:        2,
				UpdatedNumberScheduled: 2,
			},
		}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if ready || reason != "DevicePluginPending" {
			t.Fatalf("got ready=%v reason=%q, want DevicePluginPending", ready, reason)
		}
	})

	t.Run("single strategy requires amd.com/gpu", func(t *testing.T) {
		devConfig := newDeviceConfig(utils.DriverTypeVFPassthrough)
		devConfig.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
		devConfig.Spec.DevicePlugin.DevicePluginArguments = map[string]string{
			utils.ResourceNamingStrategyFlag: utils.SingleStrategy,
		}
		nodes := newNodes()
		module := readyVFModule(devConfig, nodes)
		labelVFIO(nodes, devConfig)
		for i := range nodes.Items {
			nodes.Items[i].Status.Allocatable = v1.ResourceList{
				v1.ResourceName("amd.com/cpx_nps4"): resource.MustParse("1"),
			}
		}
		devicePlugin := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{NumberAvailable: 2}}
		ready, reason, _ := deviceConfigReadiness(devConfig, nodes, module, true, devicePlugin)
		if ready || reason != "DevicePluginPending" {
			t.Fatalf("got ready=%v reason=%q, want DevicePluginPending", ready, reason)
		}
	})
}

func readyVFModule(devConfig *amdv1alpha1.DeviceConfig, nodes *v1.NodeList) *kmmv1beta1.Module {
	module := &kmmv1beta1.Module{Status: kmmv1beta1.ModuleStatus{
		ModuleLoader: kmmv1beta1.DaemonSetStatus{AvailableNumber: int32(len(nodes.Items))},
	}}
	for i := range nodes.Items {
		nodes.Items[i].Labels = map[string]string{
			fmt.Sprintf(utils.KMMModuleReadyLabelTemplate, devConfig.Namespace, devConfig.Name): "",
		}
	}
	return module
}

func labelVFIO(nodes *v1.NodeList, devConfig *amdv1alpha1.DeviceConfig) {
	label := fmt.Sprintf(utils.VFIOMountReadyLabelTemplate, devConfig.Namespace, devConfig.Name)
	for i := range nodes.Items {
		if nodes.Items[i].Labels == nil {
			nodes.Items[i].Labels = map[string]string{}
		}
		nodes.Items[i].Labels[label] = ""
	}
}
