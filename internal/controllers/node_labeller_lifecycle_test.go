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
	"github.com/ROCm/gpu-operator/internal/controllers/watchers"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNodeLabellerFinalizationWithoutKMM(t *testing.T) {
	for _, failPatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("patchFailure=%v", failPatch), func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			cfg := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "test", Finalizers: []string{deviceConfigFinalizer}}, Spec: amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{Enable: ptr.To(false)}}}
			versionKey, _ := kmmmodule.GetVersionLabelKV(cfg)
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node", ResourceVersion: "1", Labels: map[string]string{"amd.com/gpu.device-id": "75a3", "amd.com/gpu.probe": "keep", versionKey: "old"}}}
			cli := mock_client.NewMockClient(gomock.NewController(t))
			dsDeleted := false
			cli.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				switch obj := obj.(type) {
				case *v1.Node:
					node.DeepCopyInto(obj)
					return nil
				case *kmmv1beta1.Module:
					t.Fatal("KMM must not be accessed when watching is disabled")
				case *appsv1.DaemonSet:
					if key.Name == "custom-node-labeller" && !dsDeleted {
						obj.Name = key.Name
						obj.Namespace = key.Namespace
						return nil
					}
				}
				return apierrors.NewNotFound(schema.GroupResource{Resource: "fixture"}, key.Name)
			}).AnyTimes()
			cli.EXPECT().Delete(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
				g.Expect(obj.GetName()).To(Equal("custom-node-labeller"))
				dsDeleted = true
				return nil
			})
			finalizerPatched := false
			cli.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
				if actual, ok := obj.(*v1.Node); ok {
					if failPatch {
						return fmt.Errorf("node patch unavailable")
					}
					data, err := patch.Data(obj)
					g.Expect(err).NotTo(HaveOccurred())
					var changes map[string]interface{}
					g.Expect(json.Unmarshal(data, &changes)).To(Succeed())
					g.Expect(changes["metadata"]).NotTo(HaveKey("resourceVersion"))
					node = actual.DeepCopy()
					return nil
				}
				g.Expect(node.Labels).NotTo(HaveKey("amd.com/gpu.device-id"))
				finalizerPatched = true
				return nil
			}).AnyTimes()
			helper := &deviceConfigReconcilerHelper{client: cli, kmmWatchEnabled: false}
			err := helper.finalizeDeviceConfig(ctx, cfg, &v1.NodeList{Items: []v1.Node{*node.DeepCopy()}})
			g.Expect(dsDeleted).To(BeTrue())
			g.Expect(node.Labels).To(HaveKeyWithValue("amd.com/gpu.probe", "keep"))
			if failPatch {
				g.Expect(err).To(MatchError(ContainSubstring("node patch unavailable")))
				g.Expect(cfg.Finalizers).To(ContainElement(deviceConfigFinalizer))
				g.Expect(finalizerPatched).To(BeFalse())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(finalizerPatched).To(BeTrue())
				g.Expect(cfg.Finalizers).NotTo(ContainElement(deviceConfigFinalizer))
				g.Expect(node.Labels).NotTo(HaveKey(versionKey))
			}
		})
	}
}

func TestDeviceConfigCleanupWatch(t *testing.T) {
	g := NewWithT(t)
	before := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "test"}, Spec: amdv1alpha1.DeviceConfigSpec{Selector: map[string]string{"pool": "old"}}}
	after := before.DeepCopy()
	after.Spec.Selector = map[string]string{"pool": "new"}
	pred := watchers.DeviceConfigSelectorChangedPredicate{}
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after})).To(BeTrue())
	unchanged := before.DeepCopy()
	unchanged.Status.ObservedGeneration = 2
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: unchanged})).To(BeFalse())
	unchanged.Spec.DevicePlugin.NodeLabellerImage = "another-image"
	g.Expect(pred.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: unchanged})).To(BeFalse())
	g.Expect(pred.Create(event.CreateEvent{Object: before})).To(BeTrue())
	g.Expect(pred.Delete(event.DeleteEvent{Object: before})).To(BeTrue())
	g.Expect(pred.Generic(event.GenericEvent{Object: before})).To(BeFalse())
	cli := mock_client.NewMockClient(gomock.NewController(t))
	cli.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
		g.Expect((&client.ListOptions{}).ApplyOptions(opts).Namespace).To(BeEmpty())
		configs := list.(*amdv1alpha1.DeviceConfigList)
		configs.Items = []amdv1alpha1.DeviceConfig{*before,
			{ObjectMeta: metav1.ObjectMeta{Name: "gpu", Namespace: "test"}, Spec: amdv1alpha1.DeviceConfigSpec{Selector: map[string]string{utils.NodeFeatureLabelAmdGpu: "true"}, DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(true)}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "other"}, Spec: amdv1alpha1.DeviceConfigSpec{Selector: map[string]string{utils.NodeFeatureLabelAmdVGpu: "true"}, DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(true)}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "disabled", Namespace: "test"}, Spec: amdv1alpha1.DeviceConfigSpec{Selector: map[string]string{utils.NodeFeatureLabelAmdGpu: "true"}, DevicePlugin: amdv1alpha1.DevicePluginSpec{EnableNodeLabeller: ptr.To(false)}}},
		}
		return nil
	}).AnyTimes()
	r := &DeviceConfigReconciler{Client: cli}
	want := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "test", Name: "gpu"}}, {NamespacedName: types.NamespacedName{Namespace: "other", Name: "custom"}}}
	// Both the old/new update objects and a deleted config enqueue the dependents.
	for _, obj := range []client.Object{before, after} {
		g.Expect(r.findDeviceConfigsForLabelCleanup(context.Background(), obj)).To(ConsistOf(want))
	}
}
