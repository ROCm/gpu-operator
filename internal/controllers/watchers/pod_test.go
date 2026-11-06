package watchers

import (
	"context"
	"testing"
	"time"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"github.com/go-logr/logr"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	workerMgr := workermgr.NewMockWorkerMgrAPI(ctrl)
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
	).Return(nil)
	kubeClient.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil)
	workerMgr.EXPECT().RemoveWorkReadyLabel(gomock.Any(), gomock.Any(), types.NamespacedName{Namespace: "test", Name: "dc"}, "node")

	h := &PodEventHandler{client: kubeClient, workerMgr: workerMgr}
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

	h.handleWorkerMgrPodEvt(context.Background(), logr.Discard(), pod, utils.UnloadVFIOAction, nil)
}
