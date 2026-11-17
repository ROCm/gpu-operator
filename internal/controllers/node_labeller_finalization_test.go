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
	"fmt"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNodeLabellerFinalizationPreservesOtherConfig(t *testing.T) {
	for _, otherNamespace := range []string{"test", "other"} {
		t.Run(otherNamespace, func(t *testing.T) {
			ctx := context.Background()
			makeConfig := func(name, pool string) *amdv1alpha1.DeviceConfig {
				return &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", Finalizers: []string{deviceConfigFinalizer}}, Spec: amdv1alpha1.DeviceConfigSpec{
					Selector:     map[string]string{"example.com/pool": pool},
					Driver:       amdv1alpha1.DriverSpec{Enable: ptr.To(false)},
					DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(true)},
				}}
			}
			configA, configB := makeConfig("a", "a"), makeConfig("b", "b")
			configB.Namespace = otherNamespace
			nodeA := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"example.com/pool": "a", "amd.com/gpu.device-id": "a-device", "amd.com/gpu.probe": "keep"}}}
			nodeB := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b", Labels: map[string]string{"example.com/pool": "b", "amd.com/gpu.device-id": "b-device", "beta.amd.com/gpu.family": "AI", "beta.amd.com/gpu.family.AI": "1", "amd.com/gpu.probe": "keep"}}}
			gm := gomock.NewController(t)
			cli := mock_client.NewMockClient(gm)
			configs := map[client.ObjectKey]*amdv1alpha1.DeviceConfig{client.ObjectKeyFromObject(configA): configA.DeepCopy(), client.ObjectKeyFromObject(configB): configB.DeepCopy()}
			nodes := map[string]*v1.Node{nodeA.Name: nodeA.DeepCopy(), nodeB.Name: nodeB.DeepCopy()}
			cli.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				switch obj := obj.(type) {
				case *v1.Node:
					nodes[key.Name].DeepCopyInto(obj)
					return nil
				case *amdv1alpha1.DeviceConfig:
					if configs[key] != nil {
						configs[key].DeepCopyInto(obj)
						return nil
					}
				case *appsv1.DaemonSet:
					// All original operands are already absent; only Node cleanup remains.
				}
				return apierrors.NewNotFound(schema.GroupResource{Resource: "fixture"}, key.Name)
			}).AnyTimes()
			cli.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
				options := (&client.ListOptions{}).ApplyOptions(opts)
				switch list := list.(type) {
				case *v1.NodeList:
					for _, node := range nodes {
						if options.LabelSelector == nil || options.LabelSelector.Matches(labels.Set(node.Labels)) {
							list.Items = append(list.Items, *node.DeepCopy())
						}
					}
				case *amdv1alpha1.DeviceConfigList:
					for _, config := range configs {
						list.Items = append(list.Items, *config.DeepCopy())
					}
				}
				return nil
			}).AnyTimes()
			cli.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
				config := obj.(*amdv1alpha1.DeviceConfig)
				configs[client.ObjectKeyFromObject(config)] = config.DeepCopy()
				return nil
			})
			cli.EXPECT().Delete(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
				config := obj.(*amdv1alpha1.DeviceConfig)
				stored := configs[client.ObjectKeyFromObject(config)].DeepCopy()
				stored.DeletionTimestamp = ptr.To(metav1.Now())
				configs[client.ObjectKeyFromObject(stored)] = stored
				return nil
			})
			cli.EXPECT().Patch(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				switch obj := obj.(type) {
				case *v1.Node:
					nodes[obj.Name] = obj.DeepCopy()
				case *amdv1alpha1.DeviceConfig:
					configs[client.ObjectKeyFromObject(obj)] = obj.DeepCopy()
				default:
					t.Fatalf("unexpected patch %T", obj)
				}
				return nil
			}).AnyTimes()
			assigned := map[string]string{"node-a": "test/a", "node-b": otherNamespace + "/b"}
			upgrade := NewMockupgradeMgrAPI(gm)
			remediation := NewMockremediationMgrAPI(gm)
			upgrade.EXPECT().HandleDelete(gomock.Any(), gomock.Any(), gomock.Any()).Return(ctrl.Result{}, nil)
			remediation.EXPECT().HandleDelete(gomock.Any(), gomock.Any(), gomock.Any()).Return(ctrl.Result{}, nil)
			helper := &deviceConfigReconcilerHelper{client: cli, kmmWatchEnabled: false, nodeAssignments: assigned, upgradeMgrHandler: upgrade, remediationMgrHandler: remediation}
			reconciler := &DeviceConfigReconciler{Client: cli, helper: helper}
			reconciler.once.Do(func() {}) // The assignments above represent previously reconciled A and B.
			latestA := &amdv1alpha1.DeviceConfig{}
			if err := cli.Get(ctx, client.ObjectKeyFromObject(configA), latestA); err != nil {
				t.Fatal(err)
			}
			latestA.Spec.Selector["example.com/pool"] = "b"
			if err := cli.Update(ctx, latestA); err != nil {
				t.Fatal(err)
			}
			selected := &v1.NodeList{}
			if err := cli.List(ctx, selected, client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(latestA.Spec.Selector)}); err != nil {
				t.Fatal(err)
			}
			if len(selected.Items) != 1 || selected.Items[0].Name != nodeB.Name {
				t.Fatalf("current selector should select B only: %+v", selected.Items)
			}
			if err := helper.validateNodeAssignments("test/a", selected); err == nil {
				t.Fatal("overlap must be rejected on a normal reconcile")
			} else {
				t.Logf("normal validation rejects the selector update: %v", err)
			}
			if err := cli.Delete(ctx, latestA); err != nil {
				t.Fatal(err)
			}
			if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "a"}}); err != nil {
				t.Fatal(err)
			}
			actualB := &v1.Node{}
			if err := cli.Get(ctx, client.ObjectKeyFromObject(nodeB), actualB); err != nil {
				t.Fatal(err)
			}
			if actualB.Labels["amd.com/gpu.device-id"] != "b-device" || actualB.Labels["beta.amd.com/gpu.family"] != "AI" || actualB.Labels["beta.amd.com/gpu.family.AI"] != "1" {
				t.Fatalf("deleting A removed B's labeller labels: %v", actualB.Labels)
			}
			t.Logf("B's labels preserved: %v", actualB.Labels)
		})
	}
}

// A conflicting label update must recheck ownership before retrying cleanup.
func TestNodeLabellerFinalizationRechecksOwnershipOnConflict(t *testing.T) {
	ctx := context.Background()
	cfg := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "test", Finalizers: []string{deviceConfigFinalizer}}, Spec: amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{Enable: ptr.To(false)}}}
	other := amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "other"}, Spec: amdv1alpha1.DeviceConfigSpec{Selector: map[string]string{"example.com/owner": "b"}}}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node", ResourceVersion: "1", Labels: map[string]string{"amd.com/gpu.device-id": "75a3"}}}
	cli := mock_client.NewMockClient(gomock.NewController(t))
	cli.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
		if actual, ok := obj.(*v1.Node); ok {
			node.DeepCopyInto(actual)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{Resource: "fixture"}, key.Name)
	}).AnyTimes()
	cli.EXPECT().List(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
		if (&client.ListOptions{}).ApplyOptions(opts).Namespace != "" {
			t.Fatal("ownership must be checked across namespaces")
		}
		list.(*amdv1alpha1.DeviceConfigList).Items = []amdv1alpha1.DeviceConfig{*cfg.DeepCopy(), other}
		return nil
	}).AnyTimes()
	nodePatches, finalizerRemoved := 0, false
	cli.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
		if _, ok := obj.(*v1.Node); ok {
			nodePatches++
			// Another config acquires the Node between its Get and Patch.
			node.Labels["example.com/owner"] = "b"
			node.ResourceVersion = "2"
			return apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, node.Name, fmt.Errorf("ownership changed"))
		}
		if len(obj.GetFinalizers()) != 0 {
			t.Fatal("successful finalization must remove the finalizer")
		}
		finalizerRemoved = true
		return nil
	}).AnyTimes()
	helper := &deviceConfigReconcilerHelper{client: cli, kmmWatchEnabled: false}
	if err := helper.finalizeDeviceConfig(ctx, cfg, &v1.NodeList{Items: []v1.Node{*node.DeepCopy()}}); err != nil {
		t.Fatal(err)
	}
	if nodePatches != 1 || !finalizerRemoved {
		t.Fatalf("node patches=%d finalizer removed=%v", nodePatches, finalizerRemoved)
	}
	if node.Labels["amd.com/gpu.device-id"] != "75a3" {
		t.Fatal("cleanup must preserve the new owner's GPU labels")
	}
}
