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
	"reflect"

	//"gopkg.in/yaml.v3"
	"os"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
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
			{
				Key:      "amd-dcm",
				Value:    "up",
				Operator: v1.TolerationOpEqual,
			},
			{
				Key:      "amd-gpu-unhealthy",
				Operator: v1.TolerationOpExists,
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
			{
				Key:      "amd-dcm",
				Value:    "up",
				Operator: v1.TolerationOpEqual,
			},
			{
				Key:      "amd-gpu-unhealthy",
				Operator: v1.TolerationOpExists,
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

var testGetKernelMappingsDeviceConfig = amdv1alpha1.DeviceConfig{
	ObjectMeta: metav1.ObjectMeta{
		Name: "test",
	},
	Spec: amdv1alpha1.DeviceConfigSpec{
		Driver: amdv1alpha1.DriverSpec{
			Version: "6.3",
			Image:   "test.repo/driverImage",
		},
	},
}

var testGetKernelMappingsTestCases = []struct {
	tcName              string
	nodeList            v1.NodeList
	expectKernelMapping []kmmv1beta1.KernelMapping
	expectError         bool
}{
	{
		tcName: "multiple valid homogeneous nodes",
		nodeList: v1.NodeList{
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
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "Ubuntu 22.04.3 LTS",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node2",
					},
					Spec: v1.NodeSpec{},
					Status: v1.NodeStatus{
						NodeInfo: v1.NodeSystemInfo{
							Architecture:            "amd64",
							ContainerRuntimeVersion: "containerd://1.7.19",
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "Ubuntu 22.04.3 LTS",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node3",
					},
					Spec: v1.NodeSpec{},
					Status: v1.NodeStatus{
						NodeInfo: v1.NodeSystemInfo{
							Architecture:            "amd64",
							ContainerRuntimeVersion: "containerd://1.7.19",
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "Ubuntu 22.04.3 LTS",
						},
					},
				},
			},
		},
		expectKernelMapping: []kmmv1beta1.KernelMapping{
			{
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: "ubuntu-22.04" + "-" + testGetKernelMappingsDeviceConfig.Name + "-" + testGetKernelMappingsDeviceConfig.Namespace,
					},
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
						},
						{
							Name:  "REPO_URL",
							Value: defaultInstallerRepoURL,
						},
					},
				},
				Literal:        "5.15.0-40-generic",
				ContainerImage: testGetKernelMappingsDeviceConfig.Spec.Driver.Image + ":ubuntu-22.04-${KERNEL_FULL_VERSION}-" + testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
			},
		},
		expectError: false,
	},
	{
		tcName: "multiple valid heterogeneous nodes",
		nodeList: v1.NodeList{
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
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "Ubuntu 22.04.3 LTS",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node2",
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
							OSImage:                 "Ubuntu 24.04.3 LTS",
						},
					},
				},
			},
		},
		expectKernelMapping: []kmmv1beta1.KernelMapping{
			{
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: "ubuntu-22.04" + "-" + testGetKernelMappingsDeviceConfig.Name + "-" + testGetKernelMappingsDeviceConfig.Namespace,
					},
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
						},
						{
							Name:  "REPO_URL",
							Value: defaultInstallerRepoURL,
						},
					},
				},
				Literal:        "5.15.0-40-generic",
				ContainerImage: testGetKernelMappingsDeviceConfig.Spec.Driver.Image + ":ubuntu-22.04-${KERNEL_FULL_VERSION}-" + testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
			},
			{
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: "ubuntu-24.04" + "-" + testGetKernelMappingsDeviceConfig.Name + "-" + testGetKernelMappingsDeviceConfig.Namespace,
					},
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
						},
						{
							Name:  "REPO_URL",
							Value: defaultInstallerRepoURL,
						},
					},
				},
				Literal:        "6.8.0-40-generic",
				ContainerImage: testGetKernelMappingsDeviceConfig.Spec.Driver.Image + ":ubuntu-24.04-${KERNEL_FULL_VERSION}-" + testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
			},
		},
		expectError: false,
	},
	{
		tcName: "multiple valid heterogeneous nodes + one unsupported node",
		nodeList: v1.NodeList{
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
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "Ubuntu 22.04.3 LTS",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node2",
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
							OSImage:                 "Ubuntu 24.04.3 LTS",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node3",
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
							OSImage:                 "unsupported os platform",
						},
					},
				},
			},
		},
		expectKernelMapping: []kmmv1beta1.KernelMapping{
			{
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: "ubuntu-22.04" + "-" + testGetKernelMappingsDeviceConfig.Name + "-" + testGetKernelMappingsDeviceConfig.Namespace,
					},
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
						},
						{
							Name:  "REPO_URL",
							Value: defaultInstallerRepoURL,
						},
					},
				},
				Literal:        "5.15.0-40-generic",
				ContainerImage: testGetKernelMappingsDeviceConfig.Spec.Driver.Image + ":ubuntu-22.04-${KERNEL_FULL_VERSION}-" + testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
			},
			{
				Build: &kmmv1beta1.Build{
					DockerfileConfigMap: &v1.LocalObjectReference{
						Name: "ubuntu-24.04" + "-" + testGetKernelMappingsDeviceConfig.Name + "-" + testGetKernelMappingsDeviceConfig.Namespace,
					},
					BuildArgs: []kmmv1beta1.BuildArg{
						{
							Name:  "DRIVERS_VERSION",
							Value: testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
						},
						{
							Name:  "REPO_URL",
							Value: defaultInstallerRepoURL,
						},
					},
				},
				Literal:        "6.8.0-40-generic",
				ContainerImage: testGetKernelMappingsDeviceConfig.Spec.Driver.Image + ":ubuntu-24.04-${KERNEL_FULL_VERSION}-" + testGetKernelMappingsDeviceConfig.Spec.Driver.Version,
			},
		},
		expectError: false,
	},
	{
		tcName: "multiple unsupported nodes",
		nodeList: v1.NodeList{
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
							KernelVersion:           "5.15.0-40-generic",
							KubeProxyVersion:        "v1.30.3",
							KubeletVersion:          "v1.30.3",
							OperatingSystem:         "linux",
							OSImage:                 "unsupported linux distro",
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "unit-test-node2",
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
							OSImage:                 "unsupported os platform",
						},
					},
				},
			},
		},
		expectKernelMapping: nil,
		expectError:         true,
	},
	{
		tcName: "empty node list",
		nodeList: v1.NodeList{
			Items: []v1.Node{},
		},
		expectKernelMapping: nil,
		expectError:         true,
	},
}

var _ = Describe("getKernelMappings", func() {
	It("test getKernelMappings", func() {
		logger := logr.New(nil)
		for _, tc := range testGetKernelMappingsTestCases {
			fmt.Printf("testing %v\n", tc.tcName)
			km, driverVersion, err := getKernelMappings(logger, &testGetKernelMappingsDeviceConfig, false, &tc.nodeList)
			Expect(err != nil).To(Equal(tc.expectError))
			if !reflect.DeepEqual(km, tc.expectKernelMapping) {
				fmt.Printf("expect kernel mapping %+v \nbut got %+v\n", tc.expectKernelMapping, km)
			}
			Expect(reflect.DeepEqual(km, tc.expectKernelMapping)).To(BeTrue())
			if !tc.expectError {
				Expect(driverVersion).To(Equal(testGetKernelMappingsDeviceConfig.Spec.Driver.Version))
			} else {
				Expect(km).To(BeNil())
				Expect(driverVersion).To(BeEmpty())
			}
		}
	})

	It("test parseRHELVersion", func() {
		testCases := []struct {
			labels   map[string]string
			input    string
			expected string
		}{
			{
				// legacy format
				input:    "Red Hat Enterprise Linux CoreOS 418.94.202410090804-0 (Pecan)",
				expected: "9.4",
			},
			{
				// legacy format
				input:    "Red Hat Enterprise Linux CoreOS 419.96.202410090804-0 (Plow)",
				expected: "9.6",
			},
			{
				input:    "Red Hat Enterprise Linux CoreOS 9.6.20250916-0 (Plow)",
				expected: "9.6",
			},
			{
				labels:   map[string]string{"feature.node.kubernetes.io/system-os_release.VERSION_ID": "9.6"},
				input:    "Red Hat Enterprise Linux CoreOS 9.6.20250916-0 (Plow)",
				expected: "9.6",
			},
			{
				input:    "Red Hat Enterprise Linux CoreOS 10.0.20260101-0 (Future)",
				expected: "10.0",
			},
			{
				labels:   map[string]string{"feature.node.kubernetes.io/system-os_release.VERSION_ID": "10.0"},
				input:    "Red Hat Enterprise Linux CoreOS 10.0.20250916-0 (Future)",
				expected: "10.0",
			},
			{
				input:    "Red Hat Enterprise Linux CoreOS 10.1.20260101-0 (Future)",
				expected: "10.1",
			},
			{
				input:    "Red Hat Enterprise Linux CoreOS 10.10.20260101-0 (Future)",
				expected: "10.10",
			},
			{
				input:    "Red Hat Enterprise Linux CoreOS 11.0.20260101-0 (Future)",
				expected: "11.0",
			},
		}

		for _, tc := range testCases {
			result := parseRHELVersion(tc.labels, tc.input)
			Expect(result).To(Equal(tc.expected))
		}
	})
})

var _ = Describe("SetDevicePluginAsDesired", func() {
	var km *kmmModule

	BeforeEach(func() {
		km = &kmmModule{
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

		err := km.SetDevicePluginAsDesired(ds, devConfig)
		Expect(err).To(BeNil())

		// Verify the default path is used
		expectedPath := "/var/lib/kubelet/device-plugins"

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

		err := km.SetDevicePluginAsDesired(ds, devConfig)
		Expect(err).To(BeNil())

		// Check volume mount uses custom path
		var foundVolumeMount bool
		for _, vm := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vm.Name == "kubelet-device-plugins" {
				Expect(vm.MountPath).To(Equal(customPath))
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

		err := km.SetDevicePluginAsDesired(nil, devConfig)
		Expect(err).NotTo(BeNil())
		Expect(err.Error()).To(ContainSubstring("daemon set is not initialized"))
	})
})
