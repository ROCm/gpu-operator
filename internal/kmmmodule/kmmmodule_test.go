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

Licensed under the Apache License, Version 2.0 (the \"License\");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an \"AS IS\" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kmmmodule

import (
	"context"
	"fmt"

	//"gopkg.in/yaml.v3"
	"os"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var (
	testNodeList = &v1.NodeList{
		Items: []v1.Node{
			{
				TypeMeta: metav1.TypeMeta{
					Kind: "Node",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "unit-test-node",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{
						Architecture:            "amd64",
						ContainerRuntimeVersion: "containerd://1.7.19",
						KernelVersion:           "6.8.0-40-generic",
						KubeProxyVersion:        "v1.30.3",
						KubeletVersion:          "v1.30.3",
						OperatingSystem:         "linux",
						OSImage:                 "Ubuntu 22.04.3 LTS",
					},
				},
			},
		},
	}
)

var _ = Describe("setKMMModuleLoader", func() {
	It("KMM module creation - default input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}
		driverEnable := true
		// default input
		input := amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "testns",
				Name:      "testname",
			},
			Spec: amdv1alpha1.DeviceConfigSpec{
				Driver: amdv1alpha1.DriverSpec{
					Enable: &driverEnable,
				},
			},
		}

		expectedYAMLFile, err := os.ReadFile("testdata/module_loader_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())
		fmt.Printf("<%s>\n", expectedMod.Name)
		fmt.Printf("<%s>\n", expectedMod.Spec.ModuleLoader.Container.Modprobe.ModuleName)
		Expect(len(expectedMod.Spec.ModuleLoader.Container.KernelMappings)).To(Equal(1))

		expectedMod.Spec.ModuleLoader.Container.Version = "6.1.3"
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].ContainerImage = "image-registry:5000/$MOD_NAMESPACE/amdgpu_kmod:ubuntu-22.04-${KERNEL_FULL_VERSION}-6.1.3"
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.DockerfileConfigMap.Name = fmt.Sprintf("ubuntu-22.04-%v-%v", input.Name, input.Namespace)
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.BuildArgs[0].Value = "6.1.3"
		expectedMod.Spec.Selector = map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"}
		expectedMod.Spec.ModuleLoader.Container.Modprobe.Args = &kmmv1beta1.ModprobeArgs{Load: nil, Unload: nil}
		expectedMod.Spec.Tolerations = []v1.Toleration{
			{
				Key:      "amd-gpu-driver-upgrade",
				Value:    "true",
				Operator: v1.TolerationOpEqual,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}

		err = setKMMModuleLoader(context.TODO(), &mod, &input, false, testNodeList)

		Expect(err).To(BeNil())
		Expect(mod).To(Equal(expectedMod))
	})

	It("KMM module creation - user input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}
		driverEnable := true
		// user input
		input := amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "testns",
				Name:      "testname",
			},
			Spec: amdv1alpha1.DeviceConfigSpec{
				Driver: amdv1alpha1.DriverSpec{
					Enable:  &driverEnable,
					Image:   "some driver image",
					Version: "some driver version",
					ImageRegistrySecret: &v1.LocalObjectReference{
						Name: "image repo secret name",
					},
				},
				Selector: map[string]string{"some label": "some label value"},
			},
		}

		expectedYAMLFile, err := os.ReadFile("testdata/module_loader_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())
		fmt.Printf("<%s>\n", expectedMod.Name)
		fmt.Printf("<%s>\n", expectedMod.Spec.ModuleLoader.Container.Modprobe.ModuleName)
		Expect(len(expectedMod.Spec.ModuleLoader.Container.KernelMappings)).To(Equal(1))

		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].ContainerImage = "some driver image:ubuntu-22.04-${KERNEL_FULL_VERSION}-some driver version"
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.DockerfileConfigMap.Name = fmt.Sprintf("ubuntu-22.04-%v-%v", input.Name, input.Namespace)
		expectedMod.Spec.ModuleLoader.Container.KernelMappings[0].Build.BuildArgs[0].Value = "some driver version"
		expectedMod.Spec.ModuleLoader.Container.Modprobe.Args = &kmmv1beta1.ModprobeArgs{Load: nil, Unload: nil}
		expectedMod.Spec.ModuleLoader.Container.Version = "some driver version"
		expectedMod.Spec.Selector = map[string]string{"some label": "some label value"}
		expectedMod.Spec.ImageRepoSecret = &v1.LocalObjectReference{Name: "image repo secret name"}
		expectedMod.Spec.Tolerations = []v1.Toleration{
			{
				Key:      "amd-gpu-driver-upgrade",
				Value:    "true",
				Operator: v1.TolerationOpEqual,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}

		err = setKMMModuleLoader(context.TODO(), &mod, &input, false, testNodeList)

		Expect(err).To(BeNil())
		Expect(mod).To(Equal(expectedMod))
	})
})

var _ = Describe("setKMMDevicePlugin", func() {
	It("KMM module creation - default input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}

		input := amdv1alpha1.DeviceConfig{}

		expectedYAMLFile, err := os.ReadFile("testdata/device_plugin_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())
		expectedMod.Spec.DevicePlugin.Container.Command = []string{"sh"}
		expectedMod.Spec.DevicePlugin.Container.Args = []string{
			"-c",
			"while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 1 ;done; ./k8s-device-plugin -logtostderr=true -stderrthreshold=INFO -v=5",
		}
		expectedMod.Spec.DevicePlugin.Container.ImagePullPolicy = v1.PullAlways

		setKMMDevicePlugin(&mod, &input)

		Expect(mod).To(Equal(expectedMod))
	})

	It("KMM module creation - user input values", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "moduleNamespace",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Module",
				APIVersion: "kmm.sigs.x-k8s.io/v1beta1",
			},
		}

		input := amdv1alpha1.DeviceConfig{
			Spec: amdv1alpha1.DeviceConfigSpec{
				DevicePlugin: amdv1alpha1.DevicePluginSpec{
					DevicePluginImage: "some device plugin image",
				},
			},
		}

		expectedYAMLFile, err := os.ReadFile("testdata/device_plugin_test.yaml")
		Expect(err).To(BeNil())
		expectedMod := kmmv1beta1.Module{}
		expectedJSON, err := yaml.YAMLToJSON(expectedYAMLFile)
		Expect(err).To(BeNil())
		err = yaml.Unmarshal(expectedJSON, &expectedMod)
		Expect(err).To(BeNil())

		expectedMod.Spec.DevicePlugin.Container.Image = "some device plugin image"
		expectedMod.Spec.DevicePlugin.Container.ImagePullPolicy = v1.PullAlways
		expectedMod.Spec.DevicePlugin.Container.Command = []string{"sh"}
		expectedMod.Spec.DevicePlugin.Container.Args = []string{
			"-c",
			"while [ ! -d /sys/class/kfd ] || [ ! -d /sys/module/amdgpu/drivers/ ]; do echo \"amdgpu driver is not loaded \"; sleep 1 ;done; ./k8s-device-plugin -logtostderr=true -stderrthreshold=INFO -v=5",
		}

		setKMMDevicePlugin(&mod, &input)

		Expect(mod).To(Equal(expectedMod))
	})
})
