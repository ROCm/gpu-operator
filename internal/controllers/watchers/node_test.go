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

package watchers

import (
	"context"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workqueue "k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNodeWatcherLeavesVFWorkerStartToReconciliation(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	kubeClient.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
		list.(*amdv1alpha1.DeviceConfigList).Items = []amdv1alpha1.DeviceConfig{{ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "test"}}}
		return nil
	})
	h := NewNodeEventHandler(kubeClient, workermgr.NewMockWorkerMgrAPI(ctrl))
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}
	newNode := oldNode.DeepCopy()
	newNode.Labels = map[string]string{"gpu.operator.amd.com/test.dc.vfio.ready": ""}
	h.Update(context.Background(), event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode}, queue)
	if queue.Len() != 1 {
		t.Fatalf("node update queued %d reconciles, want 1", queue.Len())
	}
}

func TestNodePredicateUpdateOnAllocatableChange(t *testing.T) {
	oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}
	newNode := oldNode.DeepCopy()
	newNode.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
	}

	if !(NodePredicate{}).Update(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode}) {
		t.Fatal("allocatable resource change should trigger a node event")
	}
}

func TestNodeHandlerRequeuesOnAllocatableChange(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	kubeClient.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
		list.(*amdv1alpha1.DeviceConfigList).Items = []amdv1alpha1.DeviceConfig{{ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "ns"}}}
		return nil
	})
	handler := NewNodeEventHandler(kubeClient, nil)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()

	oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}
	newNode := oldNode.DeepCopy()
	newNode.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceName("amd.com/gpu"): resource.MustParse("1"),
	}
	handler.Update(context.Background(), event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode}, queue)
	if queue.Len() != 1 {
		t.Fatalf("queue length = %d, want 1", queue.Len())
	}
}

func TestNodeReadyTransitionQueuesReconcile(t *testing.T) {
	for _, bootID := range []string{"boot-1", "boot-2"} {
		t.Run(bootID, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			kubeClient := mock_client.NewMockClient(ctrl)
			kubeClient.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
				list.(*amdv1alpha1.DeviceConfigList).Items = []amdv1alpha1.DeviceConfig{{ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "ns"}}}
				return nil
			})
			handler := NewNodeEventHandler(kubeClient, nil)
			queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer queue.ShutDown()

			oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}, Status: corev1.NodeStatus{
				NodeInfo:   corev1.NodeSystemInfo{BootID: "boot-1"},
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
			}}
			newNode := oldNode.DeepCopy()
			newNode.Status.NodeInfo.BootID = bootID
			newNode.Status.Conditions[0].Status = corev1.ConditionTrue
			evt := event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode}
			if !(NodePredicate{}).Update(evt) {
				t.Fatal("Ready transition should pass the node predicate")
			}
			handler.Update(context.Background(), evt, queue)
			if queue.Len() != 1 {
				t.Fatalf("Ready transition queued %d reconciles, want 1", queue.Len())
			}
		})
	}
}
