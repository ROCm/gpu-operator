/*
Copyright 2026.

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

package controllers

import (
	"context"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("getPodsToDrainOrDelete", func() {
	const nodeName = "worker-0"

	podRequesting := func(name string, requests v1.ResourceList) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: v1.PodSpec{
				NodeName:   nodeName,
				Containers: []v1.Container{{Name: "main", Resources: v1.ResourceRequirements{Requests: requests}}},
			},
		}
	}

	oneGPU := func(name string) v1.ResourceList {
		return v1.ResourceList{v1.ResourceName(name): resource.MustParse("1")}
	}

	names := func(pods []v1.Pod) []string {
		var out []string
		for _, pod := range pods {
			out = append(out, pod.Name)
		}
		return out
	}

	selectedFrom := func(pods ...*v1.Pod) []string {
		objects := make([]runtime.Object, 0, len(pods))
		for _, pod := range pods {
			objects = append(objects, pod)
		}
		helper := &upgradeMgrHelper{k8sInterface: fake.NewSimpleClientset(objects...)}
		selected, err := helper.getPodsToDrainOrDelete(
			context.Background(),
			&amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "test-deviceconfig"}},
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
		)
		Expect(err).ToNot(HaveOccurred())
		return names(selected)
	}

	It("selects every amd.com resource, whatever the naming strategy produced", func() {
		Expect(selectedFrom(
			podRequesting("single", oneGPU("amd.com/gpu")),
			podRequesting("mixed", oneGPU("amd.com/cpx_nps4")),
			podRequesting("product", oneGPU("amd.com/gpu-mi300x")),
			podRequesting("product-partitioned", oneGPU("amd.com/gpu-mi300x-cpx-nps4")),
			podRequesting("product-alternatives", oneGPU("amd.com/gpu-mi250x_mi250")),
			podRequesting("memory-mode-absent-from-the-old-list", oneGPU("amd.com/spx_nps2")),
		)).To(ConsistOf("single", "mixed", "product", "product-partitioned", "product-alternatives", "memory-mode-absent-from-the-old-list"))
	})

	It("leaves pods holding no AMD GPU", func() {
		Expect(selectedFrom(
			podRequesting("cpu-only", v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}),
			podRequesting("other-vendor", oneGPU("nvidia.com/gpu")),
		)).To(BeEmpty())
	})
})
