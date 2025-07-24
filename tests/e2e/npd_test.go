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
	"time"

	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	npdNamespace = "kube-system"

	npdServiceAccountPath                 = "./yamls/config/npd/node-problem-detector-rbac.yaml"
	npdCustomPluginMonitorConfigPath      = "./yamls/config/npd/node-problem-detector-config.yaml"
	npdDaemonSetPath                      = "./yamls/config/npd/node-problem-detector.yaml"
	npdCustomPluginMonitorErrorConfigPath = "./yamls/config/npd/node-problem-detector-error-config.yaml"
)

func kubectlCreateCmd(filePath string) {
	cmd := fmt.Sprintf("kubectl create -f %s", filePath)
	utils.RunCommand(cmd)
}

func kubectlDeleteCmd(filePath string) {
	cmd := fmt.Sprintf("kubectl delete -f %s", filePath)
	utils.RunCommand(cmd)
}

func setupNPD() {
	kubectlCreateCmd(npdServiceAccountPath)
	kubectlCreateCmd(npdCustomPluginMonitorConfigPath)
	kubectlCreateCmd(npdDaemonSetPath)
}

func tearDownNPD() {
	kubectlDeleteCmd(npdDaemonSetPath)
	kubectlDeleteCmd(npdCustomPluginMonitorConfigPath)
	kubectlDeleteCmd(npdServiceAccountPath)
}

func (s *E2ESuite) setErrorConfigForNPD(c *C) {
	// Update the NPD config to generate an error condition
	kubectlDeleteCmd(npdCustomPluginMonitorConfigPath)
	kubectlCreateCmd(npdCustomPluginMonitorErrorConfigPath)
	// restart the NPD pods to apply the new error config
	err := s.clientSet.CoreV1().Pods(npdNamespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=node-problem-detector",
	})
	assert.NoError(c, err, "unable to restart npd pods")
}

func (s *E2ESuite) restoreOriginalConfigForNPD(c *C) {
	// Revert the NPD config to the original state
	kubectlDeleteCmd(npdCustomPluginMonitorErrorConfigPath)
	kubectlCreateCmd(npdCustomPluginMonitorConfigPath)
	// restart the NPD pods to apply the original config
	err := s.clientSet.CoreV1().Pods(npdNamespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=node-problem-detector",
	})
	assert.NoError(c, err, "unable to restart npd pods")
}

func (s *E2ESuite) verifyNPDRunning(c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
			LabelSelector: "feature.node.kubernetes.io/amd-gpu=true",
		})
		if err != nil {
			return false
		}
		pods, err := s.clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=node-problem-detector",
		})
		if err != nil {
			return false
		}
		if len(pods.Items) != len(nodes.Items) {
			return false
		}
		podsRunning := make([]bool, len(pods.Items))
		for idx, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				podsRunning[idx] = true
			}
		}
		for _, running := range podsRunning {
			if !running {
				return false
			}
		}
		return true
	}, 2*time.Minute, 10*time.Second, "NPD daemonset failed to start")
}

func (s *E2ESuite) verifyNodeCondition(c *C, conditionType corev1.NodeConditionType, expectedStatus corev1.ConditionStatus) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
			LabelSelector: "feature.node.kubernetes.io/amd-gpu=true",
		})
		if err != nil {
			return false
		}
		detectedConditions := make([]bool, len(nodes.Items))
		for idx, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == conditionType && condition.Status == expectedStatus {
					detectedConditions[idx] = true
					break
				}
			}
		}
		for _, detected := range detectedConditions {
			if !detected {
				return false
			}
		}
		return true
	}, 2*time.Minute, 10*time.Second, "Node condition %v is not set to %v for nodes", conditionType, expectedStatus)
}

func (s *E2ESuite) TestNodeProblemDetector(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("expected no config to be present. but config %v exists", s.cfgName))

	exporterEnable := true
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.MetricsExporter.Enable = &exporterEnable
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.ImagePullPolicy = "Always"
	devCfg.Spec.MetricsExporter.Port = 5000

	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, corev1.ServiceTypeClusterIP, c)

	// Create NPD daemonset and required service account
	logger.Infof("Setting up Node Problem Detector (NPD)")
	setupNPD()
	defer tearDownNPD()

	// Check if NPD is running on all GPU nodes
	logger.Infof("Verify if Node Problem Detector (NPD) is running on all GPU nodes")
	s.verifyNPDRunning(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)

	//update npd config to to trigger error in Node condition
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.setErrorConfigForNPD(c)

	// Check if NPD has detected the error condition
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to true")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionTrue)

	// restore NPD config to original state
	logger.Infof("Restore Node Problem Detector (NPD) config to original state")
	s.restoreOriginalConfigForNPD(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)
}
