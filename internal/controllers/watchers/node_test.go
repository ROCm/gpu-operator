package watchers

import (
	"context"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workqueue "k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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
