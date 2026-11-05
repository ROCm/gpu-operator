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
	"context"
	"encoding/json"
	"fmt"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"github.com/ROCm/gpu-operator/internal/nodelabeller"
	testutils "github.com/ROCm/gpu-operator/internal/test"
	. "github.com/onsi/gomega"
	kmmLabels "github.com/rh-ecosystem-edge/kernel-module-management/pkg/labels"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Exercise the real handler and label mutations, including a second reconcile.
// A custom selector must not opt a node into NFD-based stale-label cleanup.
func TestNodeLabellerCleanup(t *testing.T) {
	const pool = "example.com/pool"
	custom := map[string]string{pool: "gpu"}
	gpu := map[string]string{pool: "gpu", utils.NodeFeatureLabelAmdGpu: "true"}
	vgpu := map[string]string{pool: "gpu", utils.NodeFeatureLabelAmdVGpu: "true"}
	other := func(namespace, name string, selector map[string]string) []amdv1alpha1.DeviceConfig {
		return []amdv1alpha1.DeviceConfig{{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: amdv1alpha1.DeviceConfigSpec{
			Selector: selector, DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(true)},
		}}}
	}
	disabledOther := other("test", "disabled", custom)
	disabledOther[0].Spec.DevicePlugin.EnableNodeLabeller = ptr.To(false)
	deletingOther := other("test", "deleting", custom)
	deletingOther[0].DeletionTimestamp = ptr.To(metav1.Now())
	for _, tc := range []struct {
		name                string
		selector            map[string]string
		nodeLabels          map[string]string
		driverEnabled       *bool
		labellerDisabled    bool
		wantRemoved         bool
		otherConfigs        []amdv1alpha1.DeviceConfig
		reconcileOtherFirst bool
		listConfigError     bool
		changeBeforeRead    string
		changeOnConflict    string
		configOnConflict    bool
		patchError          bool
	}{
		{name: "custom selector without NFD", selector: custom, driverEnabled: ptr.To(false)},
		{name: "custom selector with managed driver", selector: custom, driverEnabled: ptr.To(true)},
		{name: "custom selector with unset driver", selector: custom},
		{name: "custom selector with GPU discovery", selector: custom, nodeLabels: gpu},
		{name: "custom selector with vGPU discovery", selector: custom, nodeLabels: vgpu},
		{name: "empty selector", selector: map[string]string{}},
		{name: "nil selector"},
		{name: "GPU selector preserves discovered GPU", selector: gpu, nodeLabels: gpu},
		{name: "vGPU selector preserves discovered vGPU", selector: vgpu, nodeLabels: vgpu},
		{name: "GPU selector preserves vGPU", selector: gpu, nodeLabels: vgpu},
		{name: "vGPU selector preserves GPU", selector: vgpu, nodeLabels: gpu},
		{name: "GPU selector cleans stale node", selector: gpu, wantRemoved: true},
		{name: "vGPU selector cleans stale node", selector: vgpu, wantRemoved: true},
		{name: "stale node outside pool is preserved", selector: gpu, nodeLabels: map[string]string{pool: "other"}},
		{name: "managed driver ready protects node", selector: gpu, driverEnabled: ptr.To(true), nodeLabels: map[string]string{kmmLabels.GetKernelModuleReadyNodeLabel("test", "test"): ""}},
		{name: "managed device plugin ready protects node", selector: gpu, driverEnabled: ptr.To(true), nodeLabels: map[string]string{kmmLabels.GetDevicePluginNodeLabel("test", "test"): ""}},
		{name: "managed driver with no readiness cleans stale node", selector: gpu, driverEnabled: ptr.To(true), wantRemoved: true},
		{name: "inbox driver ignores leftover KMM readiness", selector: gpu, driverEnabled: ptr.To(false), nodeLabels: map[string]string{kmmLabels.GetKernelModuleReadyNodeLabel("test", "test"): ""}, wantRemoved: true},
		{name: "disabled labeller cleans custom-selected node", selector: custom, labellerDisabled: true, wantRemoved: true},
		{name: "disabled labeller cleans discovered node", selector: gpu, nodeLabels: gpu, labellerDisabled: true, wantRemoved: true},
		{name: "other custom config protects node before its first reconcile", selector: gpu, otherConfigs: other("test", "custom", custom)},
		{name: "two reconciled configs preserve custom node", selector: map[string]string{utils.NodeFeatureLabelAmdGpu: "true"}, otherConfigs: other("test", "custom", custom), reconcileOtherFirst: true},
		{name: "same config name in another namespace protects node", selector: gpu, otherConfigs: other("other", "test", custom), reconcileOtherFirst: true},
		{name: "other config with nonmatching selector allows stale cleanup", selector: gpu, otherConfigs: other("test", "other-pool", map[string]string{pool: "other"}), wantRemoved: true},
		{name: "other config with empty selector protects node", selector: gpu, otherConfigs: other("test", "all-nodes", map[string]string{})},
		{name: "other config with nil selector protects node", selector: gpu, otherConfigs: other("test", "all-nodes", nil)},
		{name: "config list failure preserves labels and returns error", selector: gpu, listConfigError: true},
		{name: "other disabled config retains responsibility for cleanup", selector: gpu, otherConfigs: disabledOther},
		{name: "other deleting config retains responsibility for cleanup", selector: gpu, otherConfigs: deletingOther},
		{name: "fresh GPU discovery prevents cleanup", selector: gpu, changeBeforeRead: utils.NodeFeatureLabelAmdGpu},
		{name: "fresh vGPU discovery prevents cleanup", selector: gpu, changeBeforeRead: utils.NodeFeatureLabelAmdVGpu},
		{name: "fresh ownership label prevents cleanup", selector: gpu, changeBeforeRead: "example.com/new-owner", otherConfigs: other("test", "new-owner", map[string]string{"example.com/new-owner": "true"})},
		{name: "conflict rechecks discovery", selector: gpu, changeOnConflict: utils.NodeFeatureLabelAmdGpu},
		{name: "conflict rechecks ownership", selector: gpu, changeOnConflict: "example.com/new-owner", otherConfigs: other("test", "new-owner", map[string]string{"example.com/new-owner": "true"})},
		{name: "conflict refreshes other config selector", selector: gpu, configOnConflict: true, otherConfigs: other("test", "new-owner", map[string]string{pool: "other"})},
		{name: "patch failure propagates", selector: gpu, patchError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			scheme, err := testutils.TestScheme()
			g.Expect(err).NotTo(HaveOccurred())
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node", ResourceVersion: "1", Labels: map[string]string{
				pool: "gpu", "amd.com/gpu.device-id": "75a3", "beta.amd.com/gpu.family": "AI", "beta.amd.com/gpu.family.AI": "1", "amd.com/gpu.probe": "keep",
			}}}
			for k, v := range tc.nodeLabels {
				node.Labels[k] = v
			}
			cfg := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"}, Spec: amdv1alpha1.DeviceConfigSpec{
				Selector:     tc.selector,
				Driver:       amdv1alpha1.DriverSpec{Enable: tc.driverEnabled},
				DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(!tc.labellerDisabled)},
			}}
			ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "test-node-labeller", Namespace: "test"}}
			cli := mock_client.NewMockClient(gomock.NewController(t))
			storedNode := node.DeepCopy()
			readChanged, conflictInjected := false, false
			storedDS := map[client.ObjectKey]*appsv1.DaemonSet{client.ObjectKeyFromObject(ds): ds.DeepCopy()}
			for _, other := range tc.otherConfigs {
				key := client.ObjectKey{Namespace: other.Namespace, Name: other.Name + utils.NodeLabellerNameSuffix}
				storedDS[key] = &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name}}
			}
			cli.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				switch obj := obj.(type) {
				case *v1.Node:
					if tc.changeBeforeRead != "" && !readChanged {
						storedNode.Labels[tc.changeBeforeRead] = "true"
						storedNode.ResourceVersion = "2"
						readChanged = true
					}
					storedNode.DeepCopyInto(obj)
				case *appsv1.DaemonSet:
					if storedDS[key] == nil {
						return apierrors.NewNotFound(schema.GroupResource{Resource: "daemonsets"}, ds.Name)
					}
					storedDS[key].DeepCopyInto(obj)
				default:
					t.Fatalf("unexpected Get %T", obj)
				}
				return nil
			}).AnyTimes()
			cli.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
				options := (&client.ListOptions{}).ApplyOptions(opts)
				if configs, ok := list.(*amdv1alpha1.DeviceConfigList); ok {
					if tc.listConfigError {
						return fmt.Errorf("config list unavailable")
					}
					configs.Items = append([]amdv1alpha1.DeviceConfig{*cfg.DeepCopy()}, tc.otherConfigs...)
					return nil
				}
				nodes := list.(*v1.NodeList)
				if options.LabelSelector.Matches(labels.Set(storedNode.Labels)) {
					nodes.Items = []v1.Node{*storedNode.DeepCopy()}
				}
				return nil
			}).AnyTimes()
			cli.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
				switch obj := obj.(type) {
				case *v1.Node:
					data, err := patch.Data(obj)
					g.Expect(err).NotTo(HaveOccurred())
					var changes map[string]interface{}
					g.Expect(json.Unmarshal(data, &changes)).To(Succeed())
					if tc.labellerDisabled {
						g.Expect(changes["metadata"]).NotTo(HaveKey("resourceVersion"))
					} else {
						g.Expect(changes["metadata"]).To(HaveKeyWithValue("resourceVersion", storedNode.ResourceVersion))
					}
					if tc.patchError {
						return fmt.Errorf("patch unavailable")
					}
					if !conflictInjected && (tc.changeOnConflict != "" || tc.configOnConflict) {
						conflictInjected = true
						storedNode.ResourceVersion = "2"
						if tc.changeOnConflict != "" {
							storedNode.Labels[tc.changeOnConflict] = "true"
						}
						if tc.configOnConflict {
							tc.otherConfigs[0].Spec.Selector = custom
						}
						return apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, node.Name, fmt.Errorf("concurrent change"))
					}
					storedNode = obj.DeepCopy()
				case *appsv1.DaemonSet:
					storedDS[client.ObjectKeyFromObject(obj)] = obj.DeepCopy()
				default:
					t.Fatalf("unexpected Patch %T", obj)
				}
				return nil
			}).AnyTimes()
			cli.EXPECT().Delete(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
				g.Expect(obj).To(BeAssignableToTypeOf(&appsv1.DaemonSet{}))
				delete(storedDS, client.ObjectKeyFromObject(obj))
				return nil
			}).AnyTimes()
			helper := &deviceConfigReconcilerHelper{client: cli, nlHandler: nodelabeller.NewNodeLabeller(scheme, false)}
			for range 2 {
				if tc.reconcileOtherFirst {
					for i := range tc.otherConfigs {
						g.Expect(helper.handleNodeLabeller(ctx, &tc.otherConfigs[i], &v1.NodeList{Items: []v1.Node{*node}})).To(Succeed())
					}
				}
				err := helper.handleNodeLabeller(ctx, cfg, &v1.NodeList{Items: []v1.Node{*node}})
				if tc.patchError {
					g.Expect(err).To(MatchError(ContainSubstring("patch unavailable")))
				} else if tc.listConfigError {
					g.Expect(err).To(MatchError(ContainSubstring("config list unavailable")))
				} else {
					g.Expect(err).NotTo(HaveOccurred())
				}
				actual := &v1.Node{}
				g.Expect(cli.Get(ctx, client.ObjectKeyFromObject(node), actual)).To(Succeed())
				for _, key := range []string{"amd.com/gpu.device-id", "beta.amd.com/gpu.family", "beta.amd.com/gpu.family.AI"} {
					if tc.wantRemoved {
						g.Expect(actual.Labels).NotTo(HaveKey(key))
					} else {
						g.Expect(actual.Labels).To(HaveKeyWithValue(key, node.Labels[key]))
					}
				}
				g.Expect(actual.Labels).To(HaveKeyWithValue("amd.com/gpu.probe", "keep"))
				for key, value := range tc.nodeLabels {
					g.Expect(actual.Labels).To(HaveKeyWithValue(key, value))
				}
			}
			err = cli.Get(ctx, client.ObjectKeyFromObject(ds), ds)
			if tc.labellerDisabled {
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveLen(len(tc.selector)))
				for key, value := range tc.selector {
					g.Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue(key, value))
				}
			}
		})
	}
}

// Skipping or failing one node must not stop cleanup of later nodes. In
// particular, return nil inside RetryOnConflict exits only that node's callback.
func TestNodeLabellerCleanupContinuesAfterSkippedOrFailedNode(t *testing.T) {
	for _, firstOutcome := range []string{"protected", "eligibility error", "patch error", "conflict then protected"} {
		t.Run(firstOutcome, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			nodes := &v1.NodeList{}
			stored := map[string]*v1.Node{}
			for _, name := range []string{"first", "second"} {
				node := v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, ResourceVersion: "1", Labels: map[string]string{"amd.com/gpu.device-id": "75a3", "amd.com/gpu.probe": "keep"}}}
				nodes.Items = append(nodes.Items, node)
				stored[name] = node.DeepCopy()
			}
			cli := mock_client.NewMockClient(gomock.NewController(t))
			cli.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				stored[key.Name].DeepCopyInto(obj.(*v1.Node))
				return nil
			}).AnyTimes()
			patched := []string{}
			cli.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
				if obj.GetName() == "first" {
					if firstOutcome == "conflict then protected" {
						stored["first"].Labels[utils.NodeFeatureLabelAmdGpu] = "true"
						stored["first"].ResourceVersion = "2"
						return apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, "first", fmt.Errorf("discovery updated"))
					}
					g.Expect(firstOutcome).To(Equal("patch error"))
					return fmt.Errorf("patch unavailable")
				}
				stored[obj.GetName()] = obj.(*v1.Node).DeepCopy()
				patched = append(patched, obj.GetName())
				return nil
			}).AnyTimes()
			checked := []string{}
			helper := &deviceConfigReconcilerHelper{client: cli}
			err := helper.updateNodeLabelsIf(ctx, &amdv1alpha1.DeviceConfig{}, nodes, false, func(node *v1.Node) (bool, error) {
				checked = append(checked, node.Name)
				if node.Name == "first" {
					switch firstOutcome {
					case "protected":
						return false, nil
					case "eligibility error":
						return false, fmt.Errorf("ownership unavailable")
					case "conflict then protected":
						return node.Labels[utils.NodeFeatureLabelAmdGpu] != "true", nil
					}
				}
				return true, nil
			})
			if firstOutcome == "eligibility error" || firstOutcome == "patch error" {
				g.Expect(err).To(MatchError(ContainSubstring("unavailable")))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			wantChecked := []string{"first", "second"}
			if firstOutcome == "conflict then protected" {
				wantChecked = []string{"first", "first", "second"}
			}
			g.Expect(checked).To(Equal(wantChecked))
			g.Expect(patched).To(Equal([]string{"second"}))
			g.Expect(stored["first"].Labels).To(HaveKeyWithValue("amd.com/gpu.device-id", "75a3"))
			g.Expect(stored["second"].Labels).NotTo(HaveKey("amd.com/gpu.device-id"))
			for _, node := range stored {
				g.Expect(node.Labels).To(HaveKeyWithValue("amd.com/gpu.probe", "keep"))
			}
		})
	}
}
