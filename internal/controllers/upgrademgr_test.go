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
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	resourcev1beta2 "k8s.io/api/resource/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

	var selectWith func(clientset *fake.Clientset) ([]string, error)

	servingClaims := func(version string) *metav1.APIResourceList {
		return &metav1.APIResourceList{
			GroupVersion: "resource.k8s.io/" + version,
			APIResources: []metav1.APIResource{{Name: "deviceclasses"}, {Name: "resourceclaims", Namespaced: true}},
		}
	}

	selectFrom := func(servedDRAVersions []string, objects ...runtime.Object) ([]string, error) {
		clientset := fake.NewSimpleClientset(objects...)
		for _, version := range servedDRAVersions {
			clientset.Resources = append(clientset.Resources, servingClaims(version))
		}
		return selectWith(clientset)
	}

	selectWith = func(clientset *fake.Clientset) ([]string, error) {
		helper := &upgradeMgrHelper{k8sInterface: clientset}
		selected, err := helper.getPodsToDrainOrDelete(
			context.Background(),
			&amdv1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: "test-deviceconfig"}},
			&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
		)
		return names(selected), err
	}

	selectedFrom := func(pods ...*v1.Pod) []string {
		objects := make([]runtime.Object, 0, len(pods))
		for _, pod := range pods {
			objects = append(objects, pod)
		}
		selected, err := selectFrom(nil, objects...)
		Expect(err).ToNot(HaveOccurred())
		return selected
	}

	// podWithClaim returns a pod whose claim "gpu" resolves to claimName, either
	// directly in the spec or, as for a ResourceClaimTemplate, through the pod status.
	podWithClaim := func(name, claimName string, fromTemplate bool) *v1.Pod {
		pod := podRequesting(name, nil)
		if !fromTemplate {
			pod.Spec.ResourceClaims = []v1.PodResourceClaim{{Name: "gpu", ResourceClaimName: &claimName}}
			return pod
		}
		template := "gpu-template"
		pod.Spec.ResourceClaims = []v1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: &template}}
		pod.Status.ResourceClaimStatuses = []v1.PodResourceClaimStatus{{Name: "gpu", ResourceClaimName: &claimName}}
		return pod
	}

	claimAllocatedBy := func(name, driver string) *resourcev1.ResourceClaim {
		return &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{
				Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{
					{Request: "gpu", Driver: driver, Pool: nodeName, Device: "gpu-0"},
				}},
			}},
		}
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

	It("selects pods holding a ResourceClaim allocated by the AMD GPU DRA driver", func() {
		selected, err := selectFrom([]string{"v1", "v1beta2", "v1beta1"},
			podWithClaim("dra-direct", "direct-claim", false), claimAllocatedBy("direct-claim", "gpu.amd.com"),
			podWithClaim("dra-template", "template-claim", true), claimAllocatedBy("template-claim", "gpu.amd.com"),
			podWithClaim("other-driver", "nic-claim", true), claimAllocatedBy("nic-claim", "rdma.example.com"),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(ConsistOf("dra-direct", "dra-template"))
	})

	It("skips a template claim that has not been generated yet", func() {
		pod := podWithClaim("unresolved", "unused", true)
		pod.Status.ResourceClaimStatuses = nil
		selected, err := selectFrom([]string{"v1"}, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(BeEmpty())
	})

	It("reads ResourceClaims through the highest DRA API version the cluster serves", func() {
		claim := &resourcev1beta2.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "beta-claim", Namespace: "default"},
			Status: resourcev1beta2.ResourceClaimStatus{Allocation: &resourcev1beta2.AllocationResult{
				Devices: resourcev1beta2.DeviceAllocationResult{Results: []resourcev1beta2.DeviceRequestAllocationResult{
					{Request: "gpu", Driver: "gpu.amd.com", Pool: nodeName, Device: "gpu-0"},
				}},
			}},
		}
		clientset := fake.NewSimpleClientset(podWithClaim("dra-beta", "beta-claim", false), claim)
		clientset.Resources = []*metav1.APIResourceList{
			{GroupVersion: "resource.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "deviceclasses"}}},
			servingClaims("v1beta2"),
			servingClaims("v1beta1"),
		}
		selected, err := selectWith(clientset)
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(ConsistOf("dra-beta"))
	})

	It("reads ResourceClaims through v1beta1 when the cluster serves only v1beta1", func() {
		claim := &resourcev1beta1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "old-claim", Namespace: "default"},
			Status: resourcev1beta1.ResourceClaimStatus{Allocation: &resourcev1beta1.AllocationResult{
				Devices: resourcev1beta1.DeviceAllocationResult{Results: []resourcev1beta1.DeviceRequestAllocationResult{
					{Request: "gpu", Driver: "gpu.amd.com", Pool: nodeName, Device: "gpu-0"},
				}},
			}},
		}
		selected, err := selectFrom([]string{"v1beta1"}, podWithClaim("dra-old", "old-claim", false), claim)
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(ConsistOf("dra-old"))
	})

	It("selects a pod whose DRA extended resource request got a scheduler-created claim", func() {
		pod := podRequesting("dra-extended", oneGPU("deviceclass.resource.kubernetes.io/gpu.amd.com"))
		pod.Status.ExtendedResourceClaimStatus = &v1.PodExtendedResourceClaimStatus{ResourceClaimName: "extended-claim"}
		selected, err := selectFrom([]string{"v1"}, pod, claimAllocatedBy("extended-claim", "gpu.amd.com"))
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(ConsistOf("dra-extended"))
	})

	It("keeps the cause when discovery of the DRA API fails", func() {
		clientset := fake.NewSimpleClientset(podWithClaim("dra-direct", "direct-claim", false))
		clientset.PrependReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "resource.k8s.io"}, "", nil)
		})
		_, err := selectWith(clientset)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
	})

	It("skips a claim that is already deleted", func() {
		selected, err := selectFrom([]string{"v1"}, podWithClaim("dra-missing", "missing-claim", false))
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(BeEmpty())
	})

	It("fails rather than skipping a pod whose claim cannot be read", func() {
		clientset := fake.NewSimpleClientset(podWithClaim("dra-forbidden", "forbidden-claim", false))
		clientset.Resources = []*metav1.APIResourceList{servingClaims("v1")}
		clientset.PrependReactor("get", "resourceclaims", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "resource.k8s.io", Resource: "resourceclaims"}, "forbidden-claim", nil)
		})
		_, err := selectWith(clientset)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
	})
})
