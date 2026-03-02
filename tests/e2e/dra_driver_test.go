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

package e2e

import (
	"context"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/conditions"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestDRADriverDaemonSetReadyAndCleanup verifies that the DRA driver daemonset
// is created and reaches a ready state when DRA is enabled, then verifies it
// is cleaned up when DRA is disabled.
func (s *E2ESuite) TestDRADriverDaemonSetReadyAndCleanup(c *C) {
	if !draDriverImageDefined {
		skipTest(c, "E2E_DRA_DRIVER_IMAGE is not defined, skipping DRA driver test")
	}

	driverEnable := false
	devCfg := &v1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.cfgName,
			Namespace: s.ns,
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				EnableDevicePlugin: ptr.To(false),
				DevicePluginImage:  devicePluginImage,
			},
			DRADriver: v1alpha1.DRADriverSpec{
				Enable: ptr.To(true),
				Image:  draDriverImage,
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}

	s.createDeviceConfig(devCfg, c)

	// Wait for DRA driver daemonset to become ready
	s.checkDRADriverStatus(devCfg, s.ns, c)

	// Verify the legacy device-plugin daemonset does NOT exist
	dpDS := utils.DevicePluginName(devCfg.Name)
	_, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), dpDS, metav1.GetOptions{})
	assert.Error(c, err, "device-plugin daemonset %s should not exist when DRA is enabled", dpDS)

	// --- Cleanup phase: disable DRA and verify daemonset is removed ---
	logger.Infof("Disabling DRA driver on DeviceConfig %s", devCfg.Name)
	devCfg.Spec.DRADriver.Enable = ptr.To(false)
	s.patchDRADriverEnablement(devCfg, c)

	// Verify DRA driver daemonset is deleted
	s.verifyDRADriverDeleted(devCfg, s.ns, c)
}

// TestDRAToDevicePluginMigration verifies that switching from DRA driver to
// legacy Device Plugin works correctly: DRA daemonset is removed and
// device-plugin daemonset is created.
func (s *E2ESuite) TestDRAToDevicePluginMigration(c *C) {
	if !draDriverImageDefined {
		skipTest(c, "E2E_DRA_DRIVER_IMAGE is not defined, skipping DRA migration test")
	}

	driverEnable := false
	devCfg := &v1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.cfgName,
			Namespace: s.ns,
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				EnableDevicePlugin: ptr.To(false),
				DevicePluginImage:  devicePluginImage,
				NodeLabellerImage:  nodeLabellerImage,
			},
			DRADriver: v1alpha1.DRADriverSpec{
				Enable: ptr.To(true),
				Image:  draDriverImage,
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}

	// Phase 1: Create with DRA enabled
	s.createDeviceConfig(devCfg, c)
	s.checkDRADriverStatus(devCfg, s.ns, c)

	dpDS := utils.DevicePluginName(devCfg.Name)
	_, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), dpDS, metav1.GetOptions{})
	assert.Error(c, err, "device-plugin daemonset should not exist when DRA is enabled")

	// Phase 2: Switch to Device Plugin - disable DRA, enable DevicePlugin
	logger.Infof("Migrating from DRA to DevicePlugin on DeviceConfig %s", devCfg.Name)
	devCfg.Spec.DRADriver.Enable = ptr.To(false)
	s.patchDRADriverEnablement(devCfg, c)

	devCfg.Spec.DevicePlugin.EnableDevicePlugin = ptr.To(true)
	s.patchDevicePluginEnablement(devCfg, c)

	// Verify DRA driver daemonset is deleted
	s.verifyDRADriverDeleted(devCfg, s.ns, c)

	// Verify device-plugin daemonset is created and running
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
}

// TestDRADriverDeviceClass verifies that the DeviceClass resource "gpu.amd.com"
// is created by the helm chart and has the correct CEL selector expression.
func (s *E2ESuite) TestDRADriverDeviceClass(c *C) {
	deviceClassName := "gpu.amd.com"

	dc, err := s.clientSet.ResourceV1beta1().DeviceClasses().Get(context.TODO(), deviceClassName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		c.Fatalf("DeviceClass %s not found - ensure helm chart was installed with draDriver.deviceClass.create=true", deviceClassName)
	}
	assert.NoError(c, err, "failed to get DeviceClass %s", deviceClassName)

	// Verify the DeviceClass has the correct CEL selector
	assert.True(c, len(dc.Spec.Selectors) > 0, "DeviceClass %s should have at least one selector", deviceClassName)
	assert.NotNil(c, dc.Spec.Selectors[0].CEL, "DeviceClass %s selector should have a CEL expression", deviceClassName)
	assert.Equal(c, "device.driver == 'gpu.amd.com'", dc.Spec.Selectors[0].CEL.Expression,
		"DeviceClass %s CEL expression mismatch", deviceClassName)

	logger.Infof("DeviceClass %s verified: selectors=%+v", deviceClassName, dc.Spec.Selectors)
}

// TestDRADriverAndDevicePluginMutualExclusion verifies that creating a
// DeviceConfig with both DRA driver and Device Plugin enabled produces a
// validation error condition on the DeviceConfig status.
func (s *E2ESuite) TestDRADriverAndDevicePluginMutualExclusion(c *C) {
	driverEnable := false
	devCfg := &v1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.cfgName,
			Namespace: s.ns,
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &driverEnable,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				EnableDevicePlugin: ptr.To(true),
				DevicePluginImage:  devicePluginImage,
			},
			DRADriver: v1alpha1.DRADriverSpec{
				Enable: ptr.To(true),
				Image:  "rocm/k8s-gpu-dra-driver:latest",
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}

	s.createDeviceConfig(devCfg, c)

	// Expect a ValidationError condition on the DeviceConfig
	assert.Eventually(c, func() bool {
		dc, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get DeviceConfig %s: %v", devCfg.Name, err)
			return false
		}
		for _, cond := range dc.Status.Conditions {
			if cond.Type == conditions.ConditionTypeError &&
				cond.Status == metav1.ConditionTrue &&
				cond.Reason == conditions.ValidationError &&
				strings.Contains(cond.Message, "DRADriver and DevicePlugin cannot be enabled at the same time") {
				logger.Infof("Got expected validation error: %s", cond.Message)
				return true
			}
		}
		logger.Infof("Waiting for validation error condition on DeviceConfig %s, current conditions: %+v",
			devCfg.Name, dc.Status.Conditions)
		return false
	}, 1*time.Minute, 5*time.Second)
}
