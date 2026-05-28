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
	"fmt"
	"os/exec"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (s *DriverInstallSuite) TestDriverUpgradeByUpdatingCR(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
	}
	s.verifyNodeDriverVersionLabel(devCfg, c)
	if !s.simEnable {
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
	}

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.3.2"
	s.patchDriversVersion(devCfg, c)
	// update the node resources version labels
	s.updateNodeDriverVersionLabel(devCfg, c)
	if !s.simEnable {
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

// TestDriverUpgradeByPushingNewCR
// test the driver upgrade by pushing new CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded
func (s *DriverInstallSuite) TestDriverUpgradeByPushingNewCR(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
		s.verifyNodeDriverVersionLabel(devCfg, c)
	}

	if !s.simEnable {
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		s.deleteDeviceConfig(devCfg, c)
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		s.deleteDeviceConfig(devCfg, c)
	}
	// upgrade by pushing new CR with new version
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		s.deleteDeviceConfig(devCfg, c)
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		s.deleteDeviceConfig(devCfg, c)
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradePolicyDefaults(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.3.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradeTwoNodes(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.3.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradeWithDrainPolicy(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	force := true
	drainPolicy := v1alpha1.DrainSpec{
		Force:          &force,
		TimeoutSeconds: 300,
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
		NodeDrainPolicy:     &drainPolicy,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.3.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradeWithPodDeletionPolicy(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	force := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	podDeletionPolicy := v1alpha1.PodDeletionSpec{
		Force:          &force,
		TimeoutSeconds: 300,
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
		PodDeletionPolicy:   &podDeletionPolicy,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.3.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradeBackToDefaultVersion(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = ""
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelUpgradeFromDefaultVersion(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.3.2"
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxParallelChangeDuringUpgrade(c *C) {
	// TODO: Fix this testcase and re-enable
	skipTest(c, "Skipping failing test case")

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 1,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// update
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.3.2"
	s.patchDriversVersion(devCfg, c)
	// update upgradePolicy maxParallel
	upgradePolicy = v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	s.patchUpgradePolicy(devCfg, c)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestMaxUnavailableChangeDuringUpgrade(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 1,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateInstallComplete, c)

	// update
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.3.2"
	s.patchDriversVersion(devCfg, c)

	// update upgradePolicy maxUnavailable
	upgradePolicy = v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 1,
		MaxUnavailableNodes: intstr.FromString("50%"),
	}
	s.patchUpgradePolicy(devCfg, c)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	if !s.simEnable {
		s.verifyNodeDriverVersionLabel(devCfg, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestRebootRequiredChangeDuringUpgrade(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := true

	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 1,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// update
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.3.2"
	s.patchDriversVersion(devCfg, c)

	// update upgradePolicy rebootRequired
	rebootRequired = false
	upgradePolicy = v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 1,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	s.patchUpgradePolicy(devCfg, c)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyNodeModuleStatus(devCfg, v1alpha1.UpgradeStateComplete, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeDriverVersionLabel(devCfg, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")

}

func (s *DriverInstallSuite) TestDevicePluginNodeLabellerDaemonSetUpgrade(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage
	upgradePolicy := v1alpha1.DaemonSetUpgradeSpec{
		UpgradeStrategy: "RollingUpdate",
		MaxUnavailable:  1,
	}
	devCfg.Spec.DevicePlugin.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

	// upgrade
	// update the CR's device plugin with image
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage2
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage2
	s.patchDevicePluginImage(devCfg, c)
	s.patchNodeLabellerImage(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

	// delete
	s.deleteDeviceConfig(devCfg, c)

}

func (s *DriverInstallSuite) TestMetricsExporterDaemonSetUpgrade(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	upgradePolicy := v1alpha1.DaemonSetUpgradeSpec{
		UpgradeStrategy: "RollingUpdate",
		MaxUnavailable:  2,
	}
	devCfg.Spec.MetricsExporter.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)

	// upgrade
	// update the CR's device plugin with image
	devCfg.Spec.MetricsExporter.Image = exporterMockImage2
	s.patchMetricsExporterImage(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)

}

func (s *DriverInstallSuite) TestKMMOperatorUpgrade(c *C) {
	if s.openshift || !s.simEnable {
		skipTest(c, "Skipping for openshift testbed/non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkKMMOperatorStatus(s.ns, c, "")

	// Upgrade KMM using the new helm chart
	logger.Infof("Upgrading KMM operator to new version")
	chartPath := "./yamls/charts/gpu-operator-helm-k8s-v1.0.0.tgz"
	upgradeCmd := exec.Command("helm", "upgrade", "amd-gpu-operator", chartPath, "-n", s.ns)
	output, err := upgradeCmd.CombinedOutput()
	logger.Infof("Helm upgrade output: %s", string(output))
	assert.NoError(c, err, "Helm upgrade failed")

	// Verify the status of NFD and KMM after upgrade
	logger.Infof("Checking NFD worker status post-upgrade")
	s.checkNFDWorkerStatus(s.ns, c, "")
	logger.Infof("Checking KMM operator status post-upgrade")
	s.checkKMMOperatorStatus(s.ns, c, "")

	// Rollback to the previous version
	logger.Infof("Rolling back KMM operator to the previous version")
	rollbackCmd := exec.Command("helm", "rollback", "amd-gpu-operator", "1", "-n", s.ns)
	rollbackOutput, rollbackErr := rollbackCmd.CombinedOutput()
	logger.Infof("Helm rollback output: %s", string(rollbackOutput))
	assert.NoError(c, rollbackErr, "Helm rollback failed")

	// Verify the status again after rollback
	logger.Infof("Checking NFD worker status post-rollback")
	s.checkNFDWorkerStatus(s.ns, c, "")
	logger.Infof("Checking KMM operator status post-rollback")
	s.checkKMMOperatorStatus(s.ns, c, "")

	logger.Infof("Deleting device configuration")
	s.deleteDeviceConfig(devCfg, c)
}

func (s *DriverInstallSuite) TestPreUpgradeHookFailure(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxUnavailableNodes: intstr.FromString("100%"),
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Initiate Driver Version Upgrade
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.3.2"
	}

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)

	// Check if the upgrade is in progress
	assert.Eventually(c, func() bool {
		updatedCfg, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get deviceConfig %v", err)
			return false
		}
		return s.isUpgradeInProgress(updatedCfg)
	}, 10*time.Minute, 5*time.Second, "Upgrade did not enter in-progress state as expected")

	chartPath := "./yamls/charts/gpu-operator-helm-k8s-v1.0.0.tgz"
	upgradeCmd := exec.Command("helm", "upgrade", "amd-gpu-operator", chartPath, "-n", s.ns)
	expectedError := "Error: UPGRADE FAILED: pre-upgrade hooks failed: 1 error occurred:\n\t* job pre-upgrade-check failed: BackoffLimitExceeded"

	output, err := upgradeCmd.CombinedOutput()
	logger.Infof("Helm upgrade output: %s", string(output))
	if assert.Error(c, err, "Helm upgrade should fail during upgrade-in-progress state") {
		// Check that the error message contains the expected substring
		assert.Contains(c, string(output), expectedError, "Upgrade failed, but the error message is not as expected")
		logger.Infof("Upgrade failed as expected with the correct error: %s", expectedError)
	} else {
		logger.Errorf("Unexpected error during helm upgrade: %v", err)
	}

	if s.openshift {
		devCfg.Spec.Driver.Version = "30.20.1"
	} else {
		devCfg.Spec.Driver.Version = "30.20.1"
	}
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Clean Up DeviceConfig
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}
