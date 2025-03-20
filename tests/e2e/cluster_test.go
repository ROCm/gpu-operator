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

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/internal/metricsexporter"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
)

func (s *E2ESuite) getDeviceConfig(c *C) *v1alpha1.DeviceConfig {
	metricsExporterEnable := true
	devCfg := &v1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.cfgName,
			Namespace: s.ns,
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Image:   driverImageRepo,
				Version: s.defaultDriverVersion,
			},
			//SkipDrivers:    true,
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &metricsExporterEnable,
				NodePort: 32501,
			},
			Selector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
		},
	}

	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1"
	}
	return devCfg
}

func (s *E2ESuite) createDeviceConfig(devCfg *v1alpha1.DeviceConfig, c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Create(devCfg)
	assert.NoError(c, err, "failed to create %v", s.cfgName)
}

func (s *E2ESuite) checkNFDWorkerStatus(ns string, c *C, workerName string) {
	if workerName == "" {
		workerName = utils.NFDWorkerName(s.openshift)
	}
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), workerName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("  failed to get node-feature-discovery %v", err)
			return false
		}
		logger.Infof("  node-feature-discovery-worker status %+v",
			ds.Status)
		return ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyDevicePluginStatus(ns string, c *C) {
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			logger.Errorf("  failed to get device-plugin %v", err)
			return false
		}
		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, utils.DevicePluginName(s.cfgName)) {
				return true
			}
		}
		logger.Infof(" Device Plugin Not found for deviceconfig %v", s.cfgName)
		return false
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) checkNodeLabellerStatus(ns string, c *C) {
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), utils.NodeLabellerName(s.cfgName), metav1.GetOptions{})
		if err != nil {
			logger.Errorf("  failed to get node-labeller %v", err)
			return false
		}

		logger.Infof(" node-labeller: %s status %+v", ds.Name, ds.Status)
		return ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) checkMetricsExporterStatus(devCfg *v1alpha1.DeviceConfig, ns string, c *C) {
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), s.cfgName+"-"+metricsexporter.ExporterName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get metrics exporter %v", err)
			return false
		}
		logger.Infof("metrics exporter %+v", ds.Status)
		svc, err := s.clientSet.CoreV1().Services(ns).Get(context.TODO(), s.cfgName+"-"+metricsexporter.ExporterName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get metrics service %v", err)
			return false
		}
		logger.Infof("metrics service %+v", svc.Spec)

		return ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
			svc.Spec.Type == corev1.ServiceTypeNodePort && len(svc.Spec.Ports) > 0 && svc.Spec.Ports[0].TargetPort == intstr.FromInt32(5000) &&
			svc.Spec.Ports[0].NodePort == devCfg.Spec.MetricsExporter.NodePort
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) patchDriversVersion(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchDriversVersion(devCfg)
	assert.NoError(c, err, "failed to update %v", s.cfgName)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) verifyDeviceConfigStatus(devCfg *v1alpha1.DeviceConfig, c *C) {
	assert.Eventually(c, func() bool {
		devCfg, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get deviceConfig %v", err)
			return false
		}
		logger.Infof(" driver status %+v",
			devCfg.Status.Drivers)
		logger.Infof(" device-plugin status %+v",
			devCfg.Status.DevicePlugin)

		return devCfg.Status.DevicePlugin.NodesMatchingSelectorNumber > 0 &&
			devCfg.Status.Drivers.NodesMatchingSelectorNumber == devCfg.Status.Drivers.AvailableNumber &&
			devCfg.Status.Drivers.DesiredNumber == devCfg.Status.Drivers.AvailableNumber &&
			devCfg.Status.DevicePlugin.NodesMatchingSelectorNumber == devCfg.Status.DevicePlugin.AvailableNumber &&
			devCfg.Status.DevicePlugin.DesiredNumber == devCfg.Status.DevicePlugin.AvailableNumber
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyNodeGPULabel(devCfg *v1alpha1.DeviceConfig, c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
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
			if !utils.CheckGpuLabel(node.Status.Capacity) {
				logger.Infof("gpu not found in %v, %v ", node.Name, node.Status.Capacity)
				return false
			}
		}
		for _, node := range nodes.Items {
			if !utils.CheckGpuLabel(node.Status.Allocatable) {
				logger.Infof("allocatable gpu not found in %v, %v ", node.Name, node.Status.Allocatable)
				return false
			}
		}
		return true

	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyNodeDriverVersionLabel(devCfg *v1alpha1.DeviceConfig, c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
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
		allMatched := true
		for _, node := range nodes.Items {
			versionLabelKey, versionLabelValue := kmmmodule.GetVersionLabelKV(devCfg)
			if ver, ok := node.Labels[versionLabelKey]; !ok {
				logger.Errorf("failed to find driver version label %+v on node %+v", versionLabelKey, node.Name)
				allMatched = false
			} else if ver != versionLabelValue {
				logger.Errorf("mismatched driver version label, node resource has %+v but expect %+v", ver, versionLabelValue)
				allMatched = false
			}
		}
		return allMatched
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) updateNodeDriverVersionLabel(devCfg *v1alpha1.DeviceConfig, c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
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

		success := true
		for _, node := range nodes.Items {
			versionLabelKey, versionLabelValue := kmmmodule.GetVersionLabelKV(devCfg)
			node.Labels[versionLabelKey] = versionLabelValue
			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{
						versionLabelKey: versionLabelValue,
					},
				},
			}
			patchBytes, _ := json.Marshal(patch)
			result, err := s.clientSet.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
			if err != nil {
				logger.Errorf("failed to patch node label %v", err)
				success = false
				continue
			}
			if ver, ok := result.Labels[versionLabelKey]; !ok {
				logger.Errorf("failed to find label %+v after patching node resource", versionLabelKey)
				success = false
			} else if ver != versionLabelValue {
				logger.Errorf("failed to match label %+v after patching node resource, got %+v expect %+v", versionLabelKey, ver, versionLabelValue)
				success = false
			}
		}
		return success

	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyROCMPOD(driverInstalled bool, c *C) {
	pods, err := utils.ListRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to deploy pods")
	for _, p := range pods {
		if driverInstalled {
			v, err := utils.GetRocmInfo(p)
			assert.NoError(c, err, "rocm-smi failed on", p, v)
			logger.Infof("rocm-smi %v  \n %v", p, v)
			v, err = utils.ListGpuDrivers(p)
			assert.NoError(c, err, "list drivers failed on", p, v)
			logger.Infof("gpudrivers %v \n%v ", p, v)
			v, err = utils.GetGpuDriverVersion(p)
			assert.NoError(c, err, "drivers version failed on", p, v)
			logger.Infof("gpudrivers %v \n%v ", p, v)
		} else {
			v, err := utils.GetRocmInfo(p)
			assert.Errorf(c, err, "rocm-smi available oni %v %v", p, v)
			logger.Infof("rocm-smi %v \n %v", p, v)
			v, err = utils.ListGpuDrivers(p)
			assert.Errorf(c, err, "drivers available on %v %v", p, v)
			logger.Infof("gpudrivers %v \n%v ", p, v)
			v, err = utils.GetGpuDriverVersion(p)
			assert.Errorf(c, err, "driver version available on %v %v", p, v)
			logger.Infof("driver version %v \n%v ", p, v)
		}
	}
}

func (s *E2ESuite) deleteDeviceConfig(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Delete(s.cfgName)
	assert.NoErrorf(c, err, "failed to delete %v", s.cfgName)

	assert.Eventually(c, func() bool {
		_, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), s.cfgName+"-node-labeller", metav1.GetOptions{})
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

func (s *E2ESuite) TestBasicSkipDriverInstall(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c)
}

func (s *E2ESuite) TestDeployment(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)

	// delete
	s.deleteDeviceConfig(c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

// TestDriverUpgradeByUpdatingCR
// test the driver upgrade by directly updating CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded
func (s *E2ESuite) TestDriverUpgradeByUpdatingCR(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)
	s.verifyNodeDriverVersionLabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
	logger.Infof("Test completed")

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.2"
	s.patchDriversVersion(devCfg, c)
	// update the node resources version labels
	s.updateNodeDriverVersionLabel(devCfg, c)
	s.verifyNodeDriverVersionLabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)

	// delete
	s.deleteDeviceConfig(c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

// TestDriverUpgradeByPsuhingNewCR
// test the driver upgrade by pushing new CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded
func (s *E2ESuite) TestDriverUpgradeByPsuhingNewCR(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)
	s.verifyNodeDriverVersionLabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)
	s.deleteDeviceConfig(c)
	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")

	// upgrade by pushing new CR with new version
	devCfg.Spec.Driver.Version = "6.2"
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)
	s.verifyNodeDriverVersionLabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)
	s.deleteDeviceConfig(c)
	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *E2ESuite) getNFDCurrentCSV() (currentCSV string) {
	command := "oc get subscription nfd -n openshift-nfd -oyaml | grep currentCSV"
	logger.Infof("  %v", command)
	cmd := exec.Command("bash", "-c", command)
	output, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		logger.Errorf("Command %v failed to start with error: %v", command, err)
		return
	}
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		m := scanner.Text()
		logger.Infof("    %v", m)
		if strings.Contains(m, "currentCSV") {
			csvSplits := strings.Split(m, ":")
			if len(csvSplits) > 1 {
				currentCSV = csvSplits[1]
			}
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		logger.Errorf("Coammand %v did not complete with error: %v", command, err)
	}
	return
}

func (s *E2ESuite) TestDeploymentWithPreInstalledKMMAndNFD(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}
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

	logger.Infof("Deploying GPU opertor without NFD and KMM Operator")
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
	s.checkNodeLabellerStatus("kube-amd-gpu", c)

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

func (s *E2ESuite) TestDeploymentOnNonAMDGPUCluster(c *C) {

	if !s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}

	ctx := context.TODO()
	noamdWorkerList := utils.GetNonAMDGpuWorker(s.clientSet)
	noamdNodeMap := make(map[string]*corev1.Node)
	noamdNodeNames := make([]string, 0)
	for _, worker := range noamdWorkerList {
		noamdNodeMap[worker.Name] = worker
		noamdNodeNames = append(noamdNodeNames, worker.Name)
		break
	}
	logger.Infof("%v", noamdNodeNames)
	if len(noamdNodeNames) == 0 {
		c.Skip("Skipping no non amd gpu server in testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)

	userInfo, err := user.Current()
	assert.NoErrorf(c, err, "failed to get user%v")
	logger.Infof("user: %v", userInfo)
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Selector = map[string]string{
		"kubernetes.io/hostname": noamdNodeNames[0],
	}

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)

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
	}, 5*time.Minute, 5*time.Second)

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

	err = utils.DeployRocmPodsByNodeNames(ctx, s.clientSet, noamdNodeNames)
	assert.NoError(c, err, "failed to deploy rocm pods")

	pods := utils.ListRocmPodsByNodeNames(ctx, noamdNodeNames)
	for _, p := range pods {
		v, err := utils.GetRocmInfo(p)
		assert.NoError(c, err, "failed to get rocm", p, v)
		logger.Infof("rocm-smi %v: %v", p, v)
		v, err = utils.ListGpuDrivers(p)
		assert.NoError(c, err, "failed to list drivers", p, v)
		logger.Infof("gpudrivers %v \n%v ", p, v)
		v, err = utils.GetGpuDriverVersion(p)
		assert.NoError(c, err, "failed to list driver version", p, v)
		logger.Infof("gpudrivers %v: %v ", p, v)
	}

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

	err = utils.DelRocmPodsByNodeNames(ctx, s.clientSet, noamdNodeNames)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *E2ESuite) TestEnableBlacklist(c *C) {
	logger.Infof("TestEnableBlacklist")

	devCfg := s.getDeviceConfig(c)
	blacklist := true
	devCfg.Spec.Driver.Blacklist = &blacklist

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
}

func (s *E2ESuite) TestWorkloadRequestedGPUs(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}

	ctx := context.TODO()
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet)
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
			"amd.com/gpu": resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			"amd.com/gpu": resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount)
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *E2ESuite) TestDeployDefaultDriver(c *C) {
	if s.noamdgpu {
		c.Skip("Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// do not specify driver version
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)

	// delete
	s.deleteDeviceConfig(c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}
