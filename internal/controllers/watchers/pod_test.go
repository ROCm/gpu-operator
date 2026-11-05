package watchers

import (
	"context"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
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
