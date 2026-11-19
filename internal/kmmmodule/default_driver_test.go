package kmmmodule

import (
	"context"
	"encoding/json"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	mock_client "github.com/ROCm/gpu-operator/internal/client"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNodeDriverDefaults(t *testing.T) {
	for _, requested := range []string{"", "31.60"} {
		t.Run("version="+requested, func(t *testing.T) {
			config := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "gpu"}}
			config.Spec.Driver.Version = requested
			key, _ := GetVersionLabelKV(config)
			nodes := &v1.NodeList{Items: []v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "noble"}, Status: v1.NodeStatus{NodeInfo: v1.NodeSystemInfo{OSImage: "Ubuntu 24.04.1 LTS"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "resolute"}, Status: v1.NodeStatus{NodeInfo: v1.NodeSystemInfo{OSImage: "Ubuntu 26.04.1 LTS"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "existing", Labels: map[string]string{key: "31.40.1"}}},
			}}
			got := map[string]string{}
			kubeClient := mock_client.NewMockClient(gomock.NewController(t))
			kubeClient.EXPECT().Patch(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
					data, err := patch.Data(obj)
					if err != nil {
						return err
					}
					var node v1.Node
					if err := json.Unmarshal(data, &node); err != nil {
						return err
					}
					got[obj.GetName()] = node.Labels[key]
					return nil
				}).Times(2)
			if err := NewKMMModule(kubeClient, nil, false).SetNodeVersionLabelAsDesired(context.Background(), config, nodes); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"noble": "6.3.3", "resolute": "31.50"} {
				if requested != "" {
					want = requested
				}
				if got[name] != want {
					t.Errorf("%s: driver label = %q, want %q", name, got[name], want)
				}
			}
			nodes.Items = []v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "unsupported"}, Status: v1.NodeStatus{NodeInfo: v1.NodeSystemInfo{OSImage: "Ubuntu 18.04 LTS"}}}}
			config.Spec.Driver.Version = ""
			if err := NewKMMModule(kubeClient, nil, false).SetNodeVersionLabelAsDesired(context.Background(), config, nodes); err == nil {
				t.Fatal("expected an error for an unsupported default driver")
			}
		})
	}
}
