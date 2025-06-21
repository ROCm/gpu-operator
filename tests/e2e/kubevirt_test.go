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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
)

const (
	amdGpuVFResourceLabel = "amd.com/gpu_vf"
	amdGpuPFResourceLabel = "amd.com/gpu_pf"
)

// verifyVFIOReadyLabel verifies the VFIO ready label on nodes
func (s *E2ESuite) verifyVFIOReadyLabel(devCfg *v1alpha1.DeviceConfig, expectLabel bool, c *C) {
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

		workerMgr := workermgr.NewWorkerMgr(nil, nil)
		vfioReadyLabel := workerMgr.GetWorkReadyLabel(types.NamespacedName{
			Namespace: devCfg.Namespace,
			Name:      devCfg.Name,
		})
		for _, node := range nodes.Items {
			if expectLabel {
				if _, ok := node.Labels[vfioReadyLabel]; !ok {
					logger.Errorf("cannot find vfio ready label on node %v", node.Name)
					return false
				}
			} else {
				if _, ok := node.Labels[vfioReadyLabel]; ok {
					logger.Errorf("vfio ready label still exists on node %v", node.Name)
					return false
				}
			}
		}
		return true
	}, 5*time.Minute, 3*time.Second)
}

// getNodesForDeviceConfig returns nodes that match the device config selector
func (s *E2ESuite) getNodesForDeviceConfig(devCfg *v1alpha1.DeviceConfig) (*v1.NodeList, error) {
	return s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: func() string {
			s := []string{}
			for k, v := range devCfg.Spec.Selector {
				s = append(s, fmt.Sprintf("%v=%v", k, v))
			}
			return strings.Join(s, ",")
		}(),
	})
}

// verifyNodeLabels verifies that specific node labels exist with expected values
func (s *E2ESuite) verifyNodeLabels(devCfg *v1alpha1.DeviceConfig, expectedLabels []string, expectedValues map[string]string, c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.getNodesForDeviceConfig(devCfg)
		if err != nil {
			logger.Errorf("failed to get nodes %v", err)
			return false
		}

		for _, node := range nodes.Items {
			for _, label := range expectedLabels {
				value, exists := node.Labels[label]
				if !exists {
					logger.Errorf("expected label %s not found on node %s", label, node.Name)
					return false
				}

				// Check specific expected values
				if expectedValue, hasExpectedValue := expectedValues[label]; hasExpectedValue {
					if value != expectedValue {
						logger.Errorf("label %s has value %s, expected %s on node %s", label, value, expectedValue, node.Name)
						return false
					}
				}

				// For device-id, just verify it exists and is not empty
				if label == "amd.com/gpu.device-id" && value == "" {
					logger.Errorf("label %s is empty on node %s", label, node.Name)
					return false
				}

				logger.Infof("Found label %s=%s on node %s", label, value, node.Name)
			}
		}
		return true
	}, 5*time.Minute, 3*time.Second)
}

// verifyVFPassthroughNodeLabels verifies VF passthrough specific node labels
func (s *E2ESuite) verifyVFPassthroughNodeLabels(devCfg *v1alpha1.DeviceConfig, driverVersion string, c *C) {
	expectedLabels := []string{
		"amd.com/gpu.mode",
		"amd.com/gpu.device-id",
		"amd.com/gpu.driver-version",
	}
	expectedValues := map[string]string{
		"amd.com/gpu.mode":           "vf-passthrough",
		"amd.com/gpu.driver-version": driverVersion,
	}
	s.verifyNodeLabels(devCfg, expectedLabels, expectedValues, c)
}

// verifyPFPassthroughNodeLabels verifies PF passthrough specific node labels
func (s *E2ESuite) verifyPFPassthroughNodeLabels(devCfg *v1alpha1.DeviceConfig, c *C) {
	expectedLabels := []string{
		"amd.com/gpu.mode",
		"amd.com/gpu.device-id",
	}
	expectedValues := map[string]string{
		"amd.com/gpu.mode": "pf-passthrough",
	}
	s.verifyNodeLabels(devCfg, expectedLabels, expectedValues, c)
}

func (s *E2ESuite) TestVFPassthroughDeployment(c *C) {
	// only run this test case when all the worker node has AMD GPU model supported by GIM driver for VF Passthrough
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)

	enableDriver := true
	enableNodeLabeller := true
	driverVersion := "8.1.0.K"
	devCfg.Spec.Driver.Enable = &enableDriver
	devCfg.Spec.Driver.DriverType = utils.DriverTypeVFPassthrough
	devCfg.Spec.Driver.Version = driverVersion
	devCfg.Spec.DevicePlugin.EnableNodeLabeller = &enableNodeLabeller
	devCfg.Spec.DevicePlugin.DevicePluginImage = kubeVirtHostDevicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = kubeVirtHostNodeLabellerImage

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuVFResourceLabel, c)
	s.verifyVFIOReadyLabel(devCfg, true, c)

	// Test Node Labeller - verify VF passthrough labels
	logger.Infof("Testing VF Node Labeller labels")
	s.verifyVFPassthroughNodeLabels(devCfg, driverVersion, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, false, c)
}

func (s *E2ESuite) TestVFPassthroughSingleStrategy(c *C) {
	// Test VF passthrough with single resource naming strategy
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v with single strategy", s.cfgName)
	devCfg := s.getDeviceConfig(c)

	enableDriver := true
	enableNodeLabeller := true
	driverVersion := "8.1.0.K"
	devCfg.Spec.Driver.Enable = &enableDriver
	devCfg.Spec.Driver.DriverType = utils.DriverTypeVFPassthrough
	devCfg.Spec.Driver.Version = driverVersion
	devCfg.Spec.DevicePlugin.EnableNodeLabeller = &enableNodeLabeller
	devCfg.Spec.DevicePlugin.DevicePluginImage = kubeVirtHostDevicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = kubeVirtHostNodeLabellerImage

	// Set single resource naming strategy
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{
		resourceNamingStrategy: namingStrategySingle,
	}

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
	s.verifyVFIOReadyLabel(devCfg, true, c)

	// Verify node labels are still correct
	s.verifyVFPassthroughNodeLabels(devCfg, driverVersion, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, false, c)
}

func (s *E2ESuite) TestPFPassthroughDeployment(c *C) {
	// only run this test case when all the worker node has AMD GPU model supported by GIM driver for PF Passthrough
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)

	enableDriver := true
	enableNodeLabeller := true
	devCfg.Spec.Driver.Enable = &enableDriver
	devCfg.Spec.Driver.DriverType = utils.DriverTypePFPassthrough
	devCfg.Spec.DevicePlugin.EnableNodeLabeller = &enableNodeLabeller
	devCfg.Spec.DevicePlugin.DevicePluginImage = kubeVirtHostDevicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = kubeVirtHostNodeLabellerImage

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuPFResourceLabel, c)
	s.verifyVFIOReadyLabel(devCfg, true, c)

	// Test Node Labeller - verify PF passthrough labels
	logger.Infof("Testing PF Node Labeller labels")
	s.verifyPFPassthroughNodeLabels(devCfg, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, false, c)
}

func (s *E2ESuite) TestPFPassthroughSingleStrategy(c *C) {
	// Test PF passthrough with single resource naming strategy
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v with single strategy", s.cfgName)
	devCfg := s.getDeviceConfig(c)

	enableDriver := true
	enableNodeLabeller := true
	devCfg.Spec.Driver.Enable = &enableDriver
	devCfg.Spec.Driver.DriverType = utils.DriverTypePFPassthrough
	devCfg.Spec.DevicePlugin.EnableNodeLabeller = &enableNodeLabeller
	devCfg.Spec.DevicePlugin.DevicePluginImage = kubeVirtHostDevicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = kubeVirtHostNodeLabellerImage
	// Set single resource naming strategy
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{
		resourceNamingStrategy: namingStrategySingle,
	}

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, amdGpuResourceLabel, c)
	s.verifyVFIOReadyLabel(devCfg, true, c)

	// Verify node labels are still correct
	s.verifyPFPassthroughNodeLabels(devCfg, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, false, c)
}
