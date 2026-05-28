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
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *DriverInstallSuite) TestBasicSkipDriverInstall(c *C) {
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	logger.Infof("create %v", s.cfgName)
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err := utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *DriverInstallSuite) TestDeployment(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
	}

	if !s.simEnable {
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

func (s *DriverInstallSuite) TestDeploymentWithPreInstalledKMMAndNFD(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}
	skipTest(c, "Skipping for non amd gpu testbed")
	var deployCommand, undeployCommand, deployWithoutNFDKMMCommand string
	var nfdInstallCommands, nfdUnInstallCommands []string
	var kmmInstallCommand, kmmUnInstallCommand string
	var standardNFDNamespace, standardNFDWorkerName, standardSelector string
	if s.openshift {
		standardNFDNamespace = "openshift-nfd"
		standardSelector = "feature.node.kubernetes.io/pci-1002.present"
		deployCommand = "OPENSHIFT=1 make -C ../../ helm-install"
		undeployCommand = "OPENSHIFT=1 make -C ../../ helm-uninstall"
		deployWithoutNFDKMMCommand = "OPENSHIFT=1 SKIP_NFD=1 SKIP_KMM=1 make -C ../../ helm-install"
		nfdInstallCommands = append(nfdInstallCommands, "oc create -f ./yamls/openshift/nfd-namespace.yaml")
		nfdInstallCommands = append(nfdInstallCommands, "oc create -f ./yamls/openshift/nfd-operatorgroup.yaml")
		nfdInstallCommands = append(nfdInstallCommands, "oc create -f ./yamls/openshift/nfd-sub.yaml")
		nfdInstallCommands = append(nfdInstallCommands, "oc apply -f ./yamls/openshift/nfd-instance.yaml")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "oc delete -f ./yamls/openshift/nfd-instance.yaml")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "oc delete subscription nfd -n openshift-nfd")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "oc delete -f ./yamls/openshift/nfd-operatorgroup.yaml")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "oc delete clusterserviceversion -n openshift-nfd %s")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "oc delete -f ./yamls/openshift/nfd-namespace.yaml")
		kmmInstallCommand = "oc apply -k https://github.com/rh-ecosystem-edge/kernel-module-management/config/default"
		kmmUnInstallCommand = "oc delete -k https://github.com/rh-ecosystem-edge/kernel-module-management/config/default"

	} else {
		standardSelector = "feature.node.kubernetes.io/amd-gpu"
		standardNFDNamespace = "node-feature-discovery"
		standardNFDWorkerName = "nfd-worker"
		deployCommand = "make -C ../../ helm-install"
		undeployCommand = "make -C ../../ helm-uninstall"
		deployWithoutNFDKMMCommand = "SKIP_NFD=1 SKIP_KMM=1 make -C ../../ helm-install"
		nfdInstallCommands = append(nfdInstallCommands, "kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.7.0/nfd-master.yaml.template")
		nfdInstallCommands = append(nfdInstallCommands, "kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.7.0/nfd-worker-daemonset.yaml.template")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "kubectl delete -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.7.0/nfd-worker-daemonset.yaml.template")
		nfdUnInstallCommands = append(nfdUnInstallCommands, "kubectl delete -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/v0.7.0/nfd-master.yaml.template")
		kmmInstallCommand = "kubectl apply -k https://github.com/kubernetes-sigs/kernel-module-management/config/default"
		kmmUnInstallCommand = "kubectl delete -k https://github.com/kubernetes-sigs/kernel-module-management/config/default"
	}

	logger.Infof("Un-Deploying the e2e deployment")
	// Delete the current Deployment
	utils.RunCommand(undeployCommand)
	logger.Infof("Waiting for cleanup after undeploy")
	if !s.openshift {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmDeployment(s.clientSet, s.ns, false); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmOCDeployment(s.clientSet, false); err != nil {
				logger.Infof("    %v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	}

	logger.Infof("Deploying standard NFD and KMM Operator")
	// Deploy standard NFD and KMM Operator
	for _, cmd := range nfdInstallCommands {
		utils.RunCommand(cmd)
	}
	utils.RunCommand(kmmInstallCommand)

	logger.Infof("Deploying GPU operator without NFD and KMM Operator")
	// Deploy GPU operator. Skip NFD and KMM
	utils.RunCommand(deployWithoutNFDKMMCommand)

	logger.Infof("Verify GPU operator deployment with standard NFD and KMM operator")
	if !s.openshift {
		assert.Eventually(c, func() bool {
			if err := utils.CheckDeploymentWithStandardKMMNFD(s.clientSet, true); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			if err := utils.CheckOCDeploymentWithStandardKMMNFD(s.clientSet, true); err != nil {
				logger.Infof("    %v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	}

	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Selector = map[string]string{standardSelector: "true"}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(standardNFDNamespace, c, standardNFDWorkerName)
	s.checkNodeLabellerStatus("kube-amd-gpu", c, devCfg)

	logger.Infof("Un-Deploying the current deployment")

	// Delete the current Deployment
	utils.RunCommand(undeployCommand)
	utils.RunCommand(kmmUnInstallCommand)
	nfdCurrentCSV := s.getNFDCurrentCSV()
	for _, cmd := range nfdUnInstallCommands {
		if strings.Contains(cmd, "clusterserviceversion") {
			utils.RunCommand(fmt.Sprintf(cmd, nfdCurrentCSV))
			continue
		}
		utils.RunCommand(cmd)
	}

	logger.Infof("m4")
	logger.Infof("Waiting for cleanup with standard KMM NFD deployment")
	if !s.openshift {
		assert.Eventually(c, func() bool {
			if err := utils.CheckDeploymentWithStandardKMMNFD(s.clientSet, false); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			if err := utils.CheckOCDeploymentWithStandardKMMNFD(s.clientSet, false); err != nil {
				logger.Infof("    %v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	}

	logger.Infof("Re-Deploying the e2e deployment")
	// Restore E2E Deployment
	utils.RunCommand(deployCommand)
	if !s.openshift {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmDeployment(s.clientSet, s.ns, true); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmOCDeployment(s.clientSet, true); err != nil {
				logger.Infof("    %v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	}
}

func (s *DriverInstallSuite) TestDeploymentOnNonAMDGPUCluster(c *C) {

	ctx := context.TODO()
	noamdWorkerList := utils.GetNonAMDGpuWorker(s.clientSet)
	noamdNodeMap := make(map[string]v1.Node)
	noamdNodeNames := make([]string, 0)
	for _, worker := range noamdWorkerList {
		noamdNodeMap[worker.Name] = worker
		noamdNodeNames = append(noamdNodeNames, worker.Name)
		break
	}
	logger.Infof("%v", noamdNodeNames)
	if len(noamdNodeNames) == 0 {
		skipTest(c, "Skipping no non amd gpu server in testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)

	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Selector = map[string]string{
		"kubernetes.io/hostname": noamdNodeNames[0],
	}

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

	assert.Eventually(c, func() bool {
		devCfg, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get deviceConfig %v", err)
			return false
		}
		logger.Infof("driver status %+v", devCfg.Status.Drivers)
		logger.Infof("device-plugin status %+v", devCfg.Status.DevicePlugin)

		return devCfg.Status.DevicePlugin.NodesMatchingSelectorNumber > 0 &&
			devCfg.Status.Drivers.NodesMatchingSelectorNumber == devCfg.Status.Drivers.AvailableNumber &&
			devCfg.Status.Drivers.DesiredNumber == devCfg.Status.Drivers.AvailableNumber &&
			devCfg.Status.DevicePlugin.NodesMatchingSelectorNumber == devCfg.Status.DevicePlugin.AvailableNumber &&
			devCfg.Status.DevicePlugin.DesiredNumber == devCfg.Status.DevicePlugin.AvailableNumber
	}, 25*time.Minute, 5*time.Second)

	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: func() string {
				s := []string{}
				for k, v := range devCfg.Spec.Selector {
					s = append(s, fmt.Sprintf("%v=%v", k, v))
				}
				return strings.Join(s, ",")
			}(),
		})
		if err != nil {
			logger.Errorf("failed to get nodes %v", err)
			return false
		}

		for _, node := range nodes.Items {
			_, ok := noamdNodeMap[node.Name]
			assert.True(c, ok, fmt.Sprintf("unexpected pod on %s", node.Name))
		}
		return true

	}, 5*time.Minute, 5*time.Second)

	// delete
	_, err = s.dClient.DeviceConfigs(s.ns).Delete(s.cfgName)
	assert.NoErrorf(c, err, "failed to delete %v", s.cfgName)

	assert.Eventually(c, func() bool {
		_, err := s.clientSet.AppsV1().DaemonSets(s.ns).
			Get(ctx, s.cfgName+"-node-labeller", metav1.GetOptions{})
		if err == nil {
			logger.Warnf("waiting to delete node-labeller ")
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second)

	assert.Eventually(c, func() bool {
		_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
		if err == nil {
			logger.Warnf("waiting to delete deviceConfig")
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second)
}

func (s *DriverInstallSuite) TestEnableBlacklist(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	logger.Infof("TestEnableBlacklist")

	devCfg := s.getDeviceConfig(c)
	blacklist := true
	devCfg.Spec.Driver.Blacklist = &blacklist

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err := utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *DriverInstallSuite) TestWorkloadRequestedGPUs(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	ctx := context.TODO()
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "gpu")
	if err != nil {
		logger.Errorf("error: %v", err)
	}
	var minGPU int = 10000
	for _, v := range ret {
		if v < minGPU {
			minGPU = v
		}
	}
	assert.Greater(c, minGPU, 0, "did not find any server with amd gpu")

	gpuLimitCount := minGPU
	gpuReqCount := minGPU

	res := &v1.ResourceRequirements{
		Limits: v1.ResourceList{
			amdGpuResourceLabel: resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			amdGpuResourceLabel: resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "gpu")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *DriverInstallSuite) TestWorkloadRequestedGPUsHomogeneousSingle(c *C) {
	s.skipDCMTestIfSIMRequiresGPU(c)
	if !dcmImageDefined {
		skipTest(c, "skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile2")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogenous default partitioning")
	} else {
		logger.Errorf("Failure test homogenous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)

	time.Sleep(60 * time.Second)

	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "gpu")
	if err != nil {
		logger.Errorf("error: %v", err)
	}
	var minGPU int = 10000
	for _, v := range ret {
		if v < minGPU {
			minGPU = v
		}
	}
	assert.Greater(c, minGPU, 0, "did not find any server with amd gpu")

	gpuLimitCount := minGPU
	gpuReqCount := minGPU

	res := &v1.ResourceRequirements{
		Limits: v1.ResourceList{
			amdGpuResourceLabel: resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			amdGpuResourceLabel: resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "gpu")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *DriverInstallSuite) TestWorkloadRequestedGPUsHomogeneousMixed(c *C) {
	s.skipDCMTestIfSIMRequiresGPU(c)
	if !dcmImageDefined {
		skipTest(c, "skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile2")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogeneous partitioning")
	} else {
		logger.Errorf("Failure test homogeneous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)
	time.Sleep(60 * time.Second)
	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{resourceNamingStrategy: namingStrategyMixed}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "cpx_nps4")
	if err != nil {
		logger.Errorf("error: %v", err)
	}
	var minGPU int = 10000
	for _, v := range ret {
		if v < minGPU {
			minGPU = v
		}
	}
	assert.Greater(c, minGPU, 0, "did not find any server with amd gpu")

	gpuLimitCount := minGPU
	gpuReqCount := minGPU

	res := &v1.ResourceRequirements{
		Limits: v1.ResourceList{
			"amd.com/cpx_nps4": resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			"amd.com/cpx_nps4": resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "cpx_nps4")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")

}

func (s *DriverInstallSuite) TestWorkloadRequestedGPUsHeterogeneousMixed(c *C) {
	s.skipDCMTestIfSIMRequiresGPU(c)
	if !dcmImageDefined {
		skipTest(c, "skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile1")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogeneous partitioning")
	} else {
		logger.Errorf("Failure test heterogenous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)
	time.Sleep(60 * time.Second)

	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{resourceNamingStrategy: namingStrategyMixed}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "cpx_nps1")
	if err != nil {
		logger.Errorf("error: %v", err)
	}
	var minGPU int = 10000
	for _, v := range ret {
		if v < minGPU {
			minGPU = v
		}
	}
	assert.Greater(c, minGPU, 0, "did not find any server with amd gpu")

	gpuLimitCount := minGPU
	gpuReqCount := minGPU

	res := &v1.ResourceRequirements{
		Limits: v1.ResourceList{
			"amd.com/cpx_nps1": resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			"amd.com/cpx_nps1": resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "cpx_nps1")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *DriverInstallSuite) TestNodeLabellerPartitionLabelsPresent(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	labelNames := []string{"compute-partitioning-supported", "memory-partitioning-supported", "compute-memory-partition"}
	devCfg.Spec.DevicePlugin.NodeLabellerArguments = labelNames
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodePartitionLabels(devCfg, labelNames, true, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err := utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *DriverInstallSuite) TestNodeLabellerPartitionLabelsAbsent(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	labelNames := []string{"compute-partitioning-supported", "memory-partitioning-supported", "compute-memory-partition"}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodePartitionLabels(devCfg, labelNames, false, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err := utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *DriverInstallSuite) TestDeployDefaultDriver(c *C) {
	if s.simEnable {
		skipTest(c, "Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// do not specify driver version
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *DriverInstallSuite) TestDifferentCRsForDifferentNodes(c *C) {
	var nodes []v1.Node
	if s.simEnable {
		nodes = utils.GetNonAMDGpuWorker(s.clientSet)
	} else {
		nodes = utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	}

	ctx := context.TODO()
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	// Deploying Different CR's for worker nodes using unique node selector with different Image Versions
	driverVersions := []string{"6.3.1", "6.3.3"}
	devCfgs := []*v1alpha1.DeviceConfig{}
	for i, nodeName := range nodeNames {
		cfgName := nodeName
		_, err := s.dClient.DeviceConfigs(s.ns).Get(cfgName, metav1.GetOptions{})
		assert.Errorf(c, err, fmt.Sprintf("config %v exists", cfgName))

		logger.Infof("create %v", cfgName)
		devCfg := s.getDeviceConfig(c)
		devCfg.Name = cfgName
		devCfg.Spec.Selector = map[string]string{
			"kubernetes.io/hostname": nodeName,
		}
		devCfg.Spec.Driver.Version = driverVersions[i%len(driverVersions)]
		s.createDeviceConfig(devCfg, c)
		devCfgs = append(devCfgs, devCfg)
		s.checkNFDWorkerStatus(s.ns, c, "")
		s.verifyDeviceConfigStatus(devCfg, c)
		s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
		s.verifyDevicePluginStatus(s.ns, c, devCfg)
		s.checkNodeLabellerStatus(s.ns, c, devCfg)
	}

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		// Deploying rocm pods for worker nodes
		err := utils.DeployRocmPodsByNodeNames(ctx, s.clientSet, nodeNames)
		assert.NoError(c, err, "failed to deploy rocm pods")
		s.verifyROCMPOD(true, c)

		for _, devCfg := range devCfgs {
			s.deleteDeviceConfig(devCfg, c)
		}

		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPodsByNodeNames(ctx, s.clientSet, nodeNames)
		assert.NoError(c, err, "failed to remove rocm pods")

		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		for _, devCfg := range devCfgs {
			s.deleteDeviceConfig(devCfg, c)
		}
	}
}

func (s *DriverInstallSuite) TestRemediationWorkflow(c *C) {
	// TODO: Fix this testcase and re-enable
	skipTest(c, "Skipping failing test case")

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	remediationEnable := true
	devCfg.Spec.RemediationWorkflow.Enable = &remediationEnable
	s.createDeviceConfig(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Patch the default template to avoid rebooting for kind cluster in CI run. Still tests triggering of workflow on basis of node condition and configmap
	if s.ciEnv {
		template, err := s.wfClient.ArgoprojV1alpha1().WorkflowTemplates(s.ns).Get(context.TODO(), "default-template", metav1.GetOptions{})
		assert.NoError(c, err)

		template.Spec.Templates[0].Steps = []wfv1.ParallelSteps{
			{Steps: []wfv1.WorkflowStep{{Name: "taint", Template: "taint"}}},
			{Steps: []wfv1.WorkflowStep{{Name: "suspend", Template: "suspend"}}},
			{Steps: []wfv1.WorkflowStep{{Name: "drain", Template: "drain"}}},
			{Steps: []wfv1.WorkflowStep{{Name: "wait", Template: "wait"}}},
			{Steps: []wfv1.WorkflowStep{{Name: "untaint", Template: "untaint"}}},
		}

		_, err = s.wfClient.ArgoprojV1alpha1().WorkflowTemplates(s.ns).Update(context.TODO(), template, metav1.UpdateOptions{})
		assert.NoError(c, err)
	}

	var nodes []v1.Node
	if s.simEnable {
		nodes = utils.GetNonAMDGpuWorker(s.clientSet)
	} else {
		nodes = utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	}

	if len(nodes) == 0 {
		c.Fatalf("No nodes found for remediation")
	}

	node := nodes[0]
	nodeName := node.Name

	defer func() {
		nodeObj, err := s.clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Failed to fetch node %s for untainting: %v", nodeName, err)
			return
		}

		var newTaints []v1.Taint
		for _, taint := range nodeObj.Spec.Taints {
			if taint.Key != "amd-gpu-unhealthy" {
				newTaints = append(newTaints, taint)
			}
		}
		nodeObj.Spec.Taints = newTaints

		_, err = s.clientSet.CoreV1().Nodes().Update(context.TODO(), nodeObj, metav1.UpdateOptions{})
		if err != nil {
			logger.Errorf("Failed to remove taint from node %s: %v", nodeName, err)
		} else {
			logger.Infof("Removed amd-gpu-unhealthy taint from node %s", nodeName)
		}
	}()

	// Patch node condition to True
	s.patchNodeCondition(c, nodeName, "AMDGPUUnhealthy", v1.ConditionTrue)
	logger.Info(fmt.Sprintf("Node condition AMDGPUUnhealthy hit on %+v", nodeName))

	// Wait for the workflow to be triggered
	logger.Info("Waiting for workflow to be triggered")
	time.Sleep(60 * time.Second)

	// Patch node condition to False (simulate remediation completed)
	s.patchNodeCondition(c, nodeName, "AMDGPUUnhealthy", v1.ConditionFalse)

	// Get and verify workflow
	wf := s.getWorkflowForNode(c, nodeName)
	s.verifyWorkflowSucceeded(c, wf)

	wf = s.getWorkflowForNode(c, nodeName)
	logger.Infof("Workflow for node %s: %+v", nodeName, wf)

	// Delete workflow
	s.deleteWorkflowForNode(c, wf)

}
