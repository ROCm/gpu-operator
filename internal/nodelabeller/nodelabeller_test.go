package nodelabeller

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

func TestOpenShiftNodeLabellerDoesNotManageBlacklist(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := amdv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	devConfig := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dc", Namespace: "ns"},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{
				Enable:     ptr.To(true),
				Blacklist:  ptr.To(true),
				DriverType: utils.DriverTypeVFPassthrough,
			},
		},
	}
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-labeller", Namespace: "ns"}}

	if err := NewNodeLabeller(scheme, true).SetNodeLabellerAsDesired(ds, devConfig); err != nil {
		t.Fatal(err)
	}

	init := ds.Spec.Template.Spec.InitContainers[0]
	command := strings.Join(init.Command, " ")
	if strings.Contains(command, "/host-etc/modprobe.d") || strings.Contains(command, "blacklist amdgpu") {
		t.Fatalf("OpenShift node-labeller mutates the host blacklist: %q", command)
	}
	if !strings.Contains(command, "/host-sys/module/gim/drivers/") {
		t.Fatalf("OpenShift node-labeller no longer waits for GIM: %q", command)
	}
	for _, volume := range ds.Spec.Template.Spec.Volumes {
		if volume.Name == "etc-volume" {
			t.Fatal("OpenShift node-labeller should not mount host /etc")
		}
	}
	for _, mount := range init.VolumeMounts {
		if mount.Name == "etc-volume" || mount.MountPath == "/host-etc" {
			t.Fatal("OpenShift node-labeller should not mount host /etc")
		}
	}
	if got := ds.Spec.Template.Spec.ServiceAccountName; got != "amd-gpu-operator-node-labeller" {
		t.Fatalf("service account = %q, want amd-gpu-operator-node-labeller", got)
	}
}
