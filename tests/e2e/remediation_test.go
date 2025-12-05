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

	wfv1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	remediationNodeCondition    = "AMDGPUHardwareAssertionHwa"
	npdInbandRASConfigPath      = "./yamls/config/npd/node-problem-detector-config-inband.yaml"
	npdInbandRASErrorConfigPath = "./yamls/config/npd/node-problem-detector-error-config-inband.yaml"
)

func (s *E2ESuite) verifyRemediationWorkflowStatus(c *C, nodeName, status string, waitTime int) {
	assert.Eventually(c, func() bool {
		wfs, err := s.wfClient.ArgoprojV1alpha1().Workflows(s.ns).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			logger.Infof("Error listing workflows: %v", err)
			return false
		}
		for _, wf := range wfs.Items {
			if strings.Contains(wf.Name, nodeName) && status == string(wf.Status.Phase) {
				return true
			}
		}
		return false
	}, time.Duration(waitTime)*time.Minute, 10*time.Second, "Remediation workflow did not reach expected status")
}

func (s *E2ESuite) TestAutoNodeRemediationWithoutPhysicalAction(c *C) {
	logger.Infof("Starting Auto Node Remediation Test")
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	nodes, err := s.clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: "feature.node.kubernetes.io/amd-gpu=true",
	})
	assert.NoError(c, err, "Failed to list nodes with AMD GPU label")
	if len(nodes.Items) == 0 {
		c.Fatalf("No nodes found with AMD GPU label")
	}
	nodeName := nodes.Items[0].Name

	_, err = s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("expected no config to be present. but config %v exists", s.cfgName))

	driverEnable := false
	remediationEnable := true
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.RemediationWorkflow.Enable = &remediationEnable
	devCfg.Spec.MetricsExporter.Enable = &remediationEnable
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.ImagePullPolicy = "Always"
	devCfg.Spec.MetricsExporter.Port = 5000
	devCfg.Spec.CommonConfig.UtilsContainer.Image = utilsContainerImage
	devCfg.Spec.CommonConfig.UtilsContainer.ImagePullPolicy = "Always"

	logger.Infof("Creating DeviceConfig with remediation enabled and driver disabled")
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, corev1.ServiceTypeClusterIP, c)

	// Wait for cluster to be up
	logger.Infof("Waiting for device config to be applied")
	time.Sleep(5 * time.Second)

	// Setup NPD
	logger.Infof("Setting up Node Problem Detector (NPD)")
	setupNPD(npdServiceAccountPath, npdInbandRASConfigPath, npdDaemonSetPath)
	defer tearDownNPD(npdServiceAccountPath, npdInbandRASConfigPath, npdDaemonSetPath)

	logger.Infof("Verify if Node Problem Detector (NPD) is running on all GPU nodes")
	s.verifyNPDRunning(c)

	logger.Infof("Verifying that node condition %s is added for the node %s", remediationNodeCondition, nodeName)
	s.verifyNodeCondition(c, remediationNodeCondition, corev1.ConditionTrue)

	// Trigger error condition by modifying NPD config
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdInbandRASConfigPath, npdInbandRASErrorConfigPath)

	s.verifyNodeCondition(c, remediationNodeCondition, corev1.ConditionTrue)

	// Verify remediation workflow started and completed
	logger.Infof("Verifying remediation workflow started on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowRunning), 5)

	time.Sleep(4 * time.Minute) // wait for workflow to progress
	logger.Infof("Reverting Node Problem Detector (NPD) thresholds to original configuration")
	s.updateConfigForNPD(c, npdInbandRASErrorConfigPath, npdInbandRASConfigPath)

	logger.Infof("Waiting for remediation workflow to complete on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowSucceeded), 70)

	logger.Infof("Verifying that node condition %s is false on the node %s", remediationNodeCondition, nodeName)
	s.verifyNodeCondition(c, remediationNodeCondition, corev1.ConditionFalse)
}
