package watchers

import (
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDaemonsetStatusChangedOnAvailability(t *testing.T) {
	oldDS := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{NumberAvailable: 0}}
	newDS := oldDS.DeepCopy()
	newDS.Status.NumberAvailable = 1

	if !daemonsetStatusChanged(oldDS, newDS) {
		t.Fatal("availability change should trigger a DeviceConfig reconcile")
	}
}

func TestDevicePluginStatusReportsSelectedNodes(t *testing.T) {
	devConfig := &amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "ns"}}
	ds := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{
		DesiredNumberScheduled: 3,
		NumberAvailable:        2,
	}}

	if !(&DaemonsetEventHandler{}).handleDevicePluginStatus(ds, devConfig) {
		t.Fatal("device-plugin status handler returned false")
	}
	if got := devConfig.Status.DevicePlugin.NodesMatchingSelectorNumber; got != 3 {
		t.Fatalf("nodesMatchingSelectorNumber = %d, want 3", got)
	}
	if got := devConfig.Status.DevicePlugin.AvailableNumber; got != 2 {
		t.Fatalf("availableNumber = %d, want 2", got)
	}
}
