/*
Copyright 2022.

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

package plugin

import (
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var scheme *runtime.Scheme

func TestPlugin(t *testing.T) {
	RegisterFailHandler(Fail)

	var err error
	scheme, err = test.TestScheme()
	Expect(err).NotTo(HaveOccurred())

	RunSpecs(t, "Plugin Suite")
}

var _ = Describe("SetDevicePluginAsDesired", func() {
	var dp *devicePlugin

	BeforeEach(func() {
		dp = &devicePlugin{
			client:      nil,
			scheme:      scheme,
			isOpenShift: false,
		}
	})

	It("should use default kubelet device plugins path when not specified", func() {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-device-plugin",
				Namespace: "test-namespace",
			},
		}

		devConfig := &amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Spec: amdv1alpha1.DeviceConfigSpec{
				DevicePlugin: amdv1alpha1.DevicePluginSpec{},
			},
		}

		err := dp.SetDevicePluginAsDesired(ds, devConfig)
		Expect(err).To(BeNil())

		// Verify the default path is used
		expectedPath := utils.KubeletDevicePluginsPath

		// Check volume mount
		var foundVolumeMount bool
		for _, vm := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vm.Name == "kubelet-device-plugins" {
				Expect(vm.MountPath).To(Equal(expectedPath))
				foundVolumeMount = true
				break
			}
		}
		Expect(foundVolumeMount).To(BeTrue(), "kubelet-device-plugins volume mount not found")

		// Check volume
		var foundVolume bool
		for _, vol := range ds.Spec.Template.Spec.Volumes {
			if vol.Name == "kubelet-device-plugins" {
				Expect(vol.HostPath.Path).To(Equal(expectedPath))
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue(), "kubelet-device-plugins volume not found")
	})

	It("should use custom kubelet socket path when specified", func() {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-device-plugin",
				Namespace: "test-namespace",
			},
		}

		customPath := "/var/snap/microk8s/common/var/lib/kubelet/device-plugins"
		devConfig := &amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Spec: amdv1alpha1.DeviceConfigSpec{
				DevicePlugin: amdv1alpha1.DevicePluginSpec{
					KubeletSocketPath: customPath,
				},
			},
		}

		err := dp.SetDevicePluginAsDesired(ds, devConfig)
		Expect(err).To(BeNil())

		// Check volume mount uses the default path (container mount path should always be the standard path
		// because the kubelet device plugin API hardcodes the path inside the container)
		var foundVolumeMount bool
		for _, vm := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vm.Name == "kubelet-device-plugins" {
				Expect(vm.MountPath).To(Equal(utils.KubeletDevicePluginsPath))
				foundVolumeMount = true
				break
			}
		}
		Expect(foundVolumeMount).To(BeTrue(), "kubelet-device-plugins volume mount not found")

		// Check volume uses custom path
		var foundVolume bool
		for _, vol := range ds.Spec.Template.Spec.Volumes {
			if vol.Name == "kubelet-device-plugins" {
				Expect(vol.HostPath.Path).To(Equal(customPath))
				foundVolume = true
				break
			}
		}
		Expect(foundVolume).To(BeTrue(), "kubelet-device-plugins volume not found")
	})

	It("should return error when daemonset is nil", func() {
		devConfig := &amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
		}

		err := dp.SetDevicePluginAsDesired(nil, devConfig)
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("daemon set is not initialized"))
	})
})
