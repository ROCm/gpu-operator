package watchers

import (
	"context"
	"testing"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"github.com/go-logr/logr"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestGetWorkerDeviceConfigRejectsStaleOwner(t *testing.T) {
	devConfig := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test", Name: "dc", UID: types.UID("current"),
	}}
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	kubeClient.EXPECT().Get(gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, gomock.Any()).Do(
		func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
			obj.(*amdv1alpha1.DeviceConfig).ObjectMeta = *devConfig.ObjectMeta.DeepCopy()
		},
	).Return(nil)
	h := &PodEventHandler{client: kubeClient}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "worker-dc-node",
			Labels:    map[string]string{utils.WorkerActionLabelKey: utils.LoadVFIOAction},
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       utils.KindDeviceConfig,
				Name:       "dc",
				UID:        types.UID("stale"),
				Controller: func() *bool { value := true; return &value }(),
			}},
		},
		Spec: v1.PodSpec{NodeName: "node"},
	}

	if _, _, ok := h.getWorkerDeviceConfig(context.Background(), pod, utils.LoadVFIOAction); ok {
		t.Fatal("stale worker owner was accepted")
	}
}

func TestWorkerUnloadCompletesDuringDeviceConfigDeletion(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	devConfig := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{
		Namespace:         "test",
		Name:              "dc",
		UID:               types.UID("current"),
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
	}}
	kubeClient.EXPECT().Get(gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, gomock.Any()).Do(
		func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
			obj.(*amdv1alpha1.DeviceConfig).ObjectMeta = *devConfig.ObjectMeta.DeepCopy()
		},
	).Return(nil).Times(1)

	h := &PodEventHandler{client: kubeClient}
	controller := true
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "worker-dc-node",
			Labels:    map[string]string{utils.WorkerActionLabelKey: utils.UnloadVFIOAction},
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       utils.KindDeviceConfig,
				Name:       "dc",
				UID:        types.UID("current"),
				Controller: &controller,
			}},
		},
		Spec:   v1.PodSpec{NodeName: "node"},
		Status: v1.PodStatus{Phase: v1.PodSucceeded},
	}

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()
	h.handleWorkerMgrPodEvt(context.Background(), logr.Discard(), pod, utils.UnloadVFIOAction, q)
	if q.Len() != 1 {
		t.Fatalf("successful unload queued %d reconciles, want 1", q.Len())
	}
}

func TestSuccessfulLoadIsConsumedByReconciliation(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	enabled := true
	dc := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test", Name: "dc", UID: types.UID("current"),
	}, Spec: amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{
		DriverType: utils.DriverTypeVFPassthrough, Enable: &enabled,
	}}}
	kubeClient.EXPECT().Get(gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, gomock.Any()).Do(
		func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
			*obj.(*amdv1alpha1.DeviceConfig) = *dc.DeepCopy()
		},
	).Return(nil)
	controller := true
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test", Name: "worker-dc-node",
			Labels:          map[string]string{utils.WorkerActionLabelKey: utils.LoadVFIOAction},
			OwnerReferences: []metav1.OwnerReference{{Kind: utils.KindDeviceConfig, Name: dc.Name, UID: dc.UID, Controller: &controller}},
		},
		Spec: v1.PodSpec{NodeName: "node"}, Status: v1.PodStatus{Phase: v1.PodSucceeded},
	}
	h := &PodEventHandler{client: kubeClient}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()
	h.handleWorkerMgrPodEvt(context.Background(), logr.Discard(), pod, utils.LoadVFIOAction, q)
	if q.Len() != 1 {
		t.Fatalf("successful load queued %d reconciles, want 1", q.Len())
	}
}

func TestDeletedFailedUnloadQueuesReconciliation(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	dc := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test", Name: "dc", UID: types.UID("current"),
	}, Spec: amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{DriverType: utils.DriverTypeVFPassthrough}}}
	kubeClient.EXPECT().Get(gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, gomock.Any()).Do(
		func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) {
			*obj.(*amdv1alpha1.DeviceConfig) = *dc.DeepCopy()
		},
	).Return(nil)
	controller := true
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test", Name: "worker-dc-node",
		Labels:          map[string]string{utils.WorkerActionLabelKey: utils.UnloadVFIOAction},
		OwnerReferences: []metav1.OwnerReference{{Kind: utils.KindDeviceConfig, Name: dc.Name, UID: dc.UID, Controller: &controller}},
	}, Spec: v1.PodSpec{NodeName: "node"}, Status: v1.PodStatus{Phase: v1.PodFailed}}
	h := &PodEventHandler{client: kubeClient}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()
	h.Delete(context.Background(), event.TypedDeleteEvent[client.Object]{Object: pod}, q)
	if q.Len() != 1 {
		t.Fatalf("failed unload deletion queued %d reconciles, want 1", q.Len())
	}
}

func TestPFCleanupMarkerIsRetainedAndDeletionQueuesReconcile(t *testing.T) {
	ctrl := gomock.NewController(t)
	kubeClient := mock_client.NewMockClient(ctrl)
	disabled := false
	dc := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "dc", UID: types.UID("current")},
		Spec: amdv1alpha1.DeviceConfigSpec{Driver: amdv1alpha1.DriverSpec{
			Enable: &disabled, DriverType: utils.DriverTypePFPassthrough,
		}},
	}
	kubeClient.EXPECT().Get(gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			*obj.(*amdv1alpha1.DeviceConfig) = *dc.DeepCopy()
			return nil
		},
	).Times(2)
	controller := true
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "worker-dc-node",
			Labels:          map[string]string{utils.WorkerActionLabelKey: utils.UnloadVFIOAction},
			OwnerReferences: []metav1.OwnerReference{{Kind: utils.KindDeviceConfig, Name: dc.Name, UID: dc.UID, Controller: &controller}}},
		Spec:   v1.PodSpec{NodeName: "node"},
		Status: v1.PodStatus{Phase: v1.PodSucceeded},
	}
	h := &PodEventHandler{client: kubeClient}
	qUpdate := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer qUpdate.ShutDown()
	h.handleWorkerMgrPodEvt(context.Background(), logr.Discard(), pod, utils.UnloadVFIOAction, qUpdate)
	if qUpdate.Len() != 1 {
		t.Fatalf("successful marker update queued %d reconciles, want 1", qUpdate.Len())
	}
	qDelete := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer qDelete.ShutDown()
	h.Delete(context.Background(), event.DeleteEvent{Object: pod}, qDelete)
	if qDelete.Len() != 1 {
		t.Fatalf("successful marker deletion queued %d reconciles, want 1", qDelete.Len())
	}
}
