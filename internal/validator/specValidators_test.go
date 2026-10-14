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

package validator

import (
	"context"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Validator Suite")
}

var _ = Describe("ValidateDevicePluginSpec", func() {
	enabled := true

	// No ImageRegistrySecret, so the client is never reached.
	validate := func(args map[string]string) error {
		return ValidateDevicePluginSpec(context.Background(), nil, &amdv1alpha1.DeviceConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "test-namespace"},
			Spec: amdv1alpha1.DeviceConfigSpec{
				DevicePlugin: amdv1alpha1.DevicePluginSpec{
					EnableDevicePlugin:    &enabled,
					DevicePluginArguments: args,
				},
			},
		})
	}

	DescribeTable("accepts every naming strategy the device plugin implements",
		func(strategy string) {
			Expect(validate(map[string]string{utils.ResourceNamingStrategyFlag: strategy})).To(Succeed())
		},
		Entry("single", utils.SingleStrategy),
		Entry("mixed", utils.MixedStrategy),
		Entry("product", utils.ProductStrategy),
	)

	It("rejects a strategy the device plugin would exit on", func() {
		err := validate(map[string]string{utils.ResourceNamingStrategyFlag: "partition"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Invalid flag value"))
	})

	It("rejects an unknown flag", func() {
		Expect(validate(map[string]string{"resource_naming": utils.SingleStrategy})).ToNot(Succeed())
	})
})
