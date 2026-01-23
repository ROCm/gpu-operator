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

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	wfv1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	conditionHWAssertion         = "AMDGPUHardwareAssertionHwa"
	conditionInternalError       = "AMDGPUDeviceInternalError"
	npdInbandRASConfigPath       = "./yamls/config/npd/node-problem-detector-config-inband.yaml"
	npdInbandRASErrorConfigPath  = "./yamls/config/npd/node-problem-detector-error-config-inband.yaml"
	npdInband2RASErrorConfigPath = "./yamls/config/npd/node-problem-detector-error-config-inband2.yaml"
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

func (s *E2ESuite) checkWorkflowExistence(c *C, nodeName string, shouldExist bool) bool {
	wfs, err := s.wfClient.ArgoprojV1alpha1().Workflows(s.ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Infof("Error listing workflows: %v", err)
		return false
	}
	exists := false
	for _, wf := range wfs.Items {
		if strings.Contains(wf.Name, nodeName) {
			exists = true
			break
		}
	}
	return exists == shouldExist
}

func (s *E2ESuite) isWorkflowSuspended(c *C, nodeName string) bool {
	wfs, err := s.wfClient.ArgoprojV1alpha1().Workflows(s.ns).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(wfs.Items) == 0 {
		logger.Infof("Error listing workflows: %v", err)
		return false
	}
	wf := wfs.Items[0]
	for _, wfItem := range wfs.Items {
		if strings.Contains(wfItem.Name, nodeName) {
			wf = wfItem
			break
		}
	}
	for _, nodeStatus := range wf.Status.Nodes {
		if nodeStatus.Type == "Suspend" && nodeStatus.Phase == "Running" {
			return true
		}
	}
	return false
}

func (s *E2ESuite) populateDeviceConfig(c *C) *v1alpha1.DeviceConfig {
	driverEnable := false
	remediationEnable := true
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.RemediationWorkflow.Enable = &remediationEnable
	devCfg.Spec.RemediationWorkflow.TesterImage = agfhcTestRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &remediationEnable
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.ImagePullPolicy = "Always"
	devCfg.Spec.MetricsExporter.Port = 5000
	devCfg.Spec.CommonConfig.UtilsContainer.Image = utilsContainerImage
	devCfg.Spec.CommonConfig.UtilsContainer.ImagePullPolicy = "Always"
	return devCfg
}

func (s *E2ESuite) addRemediationWorkflowStatusMetaData(ns, nodeName, nodeCondition string, metadataCount int, c *C) {
	// Create initial RemediationWorkflowStatus object if not present
	wfstatus, err := s.wfStatusClient.Get("default", ns)
	//if not found, create a new one
	if err != nil {
		logger.Infof("RemediationWorkflowStatus CR not found, creating a new one")
		wfstatus.Name = "default"
		wfstatus.Namespace = ns
		wfstatus, err = s.wfStatusClient.Create(wfstatus)
		assert.NoError(c, err, "Failed to create remediation workflow status")
		if err != nil {
			return
		}
	}
	wfMetaData := make([]v1alpha1.WorkflowMetadata, 0)
	for i := 0; i < metadataCount; i++ {
		data := v1alpha1.WorkflowMetadata{
			Name:      fmt.Sprintf("%s-%s", nodeName, nodeCondition),
			StartTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		}
		wfMetaData = append(wfMetaData, data)
	}
	ncmap := make(map[string][]v1alpha1.WorkflowMetadata)
	ncmap[nodeCondition] = wfMetaData
	wfstatus.Status = make(map[string]map[string][]v1alpha1.WorkflowMetadata)
	wfstatus.Status[nodeName] = ncmap
	_, err = s.wfStatusClient.Update(wfstatus)
	assert.NoError(c, err, "Failed to add metadata to remediation workflow status CR")
}

func (s *E2ESuite) untaintNode(nodeName string) {
	cmd := fmt.Sprintf("kubectl taint node %s amd-gpu-unhealthy:NoSchedule-", nodeName)
	utils.RunCommand(cmd)
}

func (s *E2ESuite) clearRemediationWorkflowStatusMetaData(ns string, c *C) {
	wfstatus, err := s.wfStatusClient.Get("default", ns)
	if err != nil {
		logger.Infof("RemediationWorkflowStatus object is not found")
		return
	}
	wfstatus.Status = make(map[string]map[string][]v1alpha1.WorkflowMetadata)
	_, err = s.wfStatusClient.Update(wfstatus)
	assert.NoError(c, err, "Failed to clear metadata from remediation workflow status CR")
}

func (s *E2ESuite) TestAutoNodeRemediationWithoutPhysicalAction(c *C) {
	logger.Infof("Starting Auto Node Remediation Test without physical action")
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

	devCfg := s.populateDeviceConfig(c)

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

	logger.Infof("Verifying that node condition %s is added for the node %s", conditionHWAssertion, nodeName)
	s.verifyNodeCondition(c, conditionHWAssertion, corev1.ConditionFalse)

	// Trigger error condition by modifying NPD config
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdInbandRASConfigPath, npdInbandRASErrorConfigPath)

	s.verifyNodeCondition(c, conditionHWAssertion, corev1.ConditionTrue)

	// Verify remediation workflow is started and running
	logger.Infof("Verifying remediation workflow started on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowRunning), 5)

	time.Sleep(4 * time.Minute) // wait for workflow to progress
	logger.Infof("Reverting Node Problem Detector (NPD) thresholds to original configuration")
	s.updateConfigForNPD(c, npdInbandRASErrorConfigPath, npdInbandRASConfigPath)

	//verify workflow succeeded
	logger.Infof("Waiting for remediation workflow to complete on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowSucceeded), 70)

	logger.Infof("Verifying that node condition %s is false on the node %s", conditionHWAssertion, nodeName)
	s.verifyNodeCondition(c, conditionHWAssertion, corev1.ConditionFalse)
}

func (s *E2ESuite) TestAutoNodeRemediationWithPhysicalAction(c *C) {
	logger.Infof("Starting Auto Node Remediation Test with physical action")
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

	devCfg := s.populateDeviceConfig(c)

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

	logger.Infof("Verifying that node condition %s is added for the node %s", conditionInternalError, nodeName)
	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionFalse)

	// Trigger error condition by modifying NPD config
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdInbandRASConfigPath, npdInband2RASErrorConfigPath)

	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionTrue)

	// Verify remediation workflow started
	logger.Infof("Verifying remediation workflow started on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowRunning), 5)

	//verify workflow is suspended waiting for physical action
	logger.Infof("Verifying remediation workflow is suspended on the node %s", nodeName)
	assert.Eventually(c, func() bool {
		return s.isWorkflowSuspended(c, nodeName)
	}, 5*time.Minute, 10*time.Second, "Remediation workflow did not reach suspended state")

	// resume workflow by adding label to node
	err = utils.AddNodeLabel(s.clientSet, nodeName, "operator.amd.com/gpu-force-resume-workflow", "true")
	assert.NoError(c, err, "Failed to add label to resume workflow")

	logger.Infof("Reverting Node Problem Detector (NPD) thresholds to original configuration")
	s.updateConfigForNPD(c, npdInband2RASErrorConfigPath, npdInbandRASConfigPath)

	logger.Infof("Waiting for remediation workflow to complete on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowSucceeded), 70)

	logger.Infof("Verifying that node condition %s is false on the node %s", conditionInternalError, nodeName)
	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionFalse)
}

func (s *E2ESuite) TestAutoNodeRemediationAbortWorkflow(c *C) {
	logger.Infof("Starting Auto Node Remediation abort workflow test")
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

	devCfg := s.populateDeviceConfig(c)

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

	logger.Infof("Verifying that node condition %s is added for the node %s", conditionInternalError, nodeName)
	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionFalse)

	// Trigger error condition by modifying NPD config
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdInbandRASConfigPath, npdInband2RASErrorConfigPath)

	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionTrue)

	// Verify remediation workflow started
	logger.Infof("Verifying remediation workflow started on the node %s", nodeName)
	s.verifyRemediationWorkflowStatus(c, nodeName, string(wfv1alpha1.WorkflowRunning), 5)

	//verify workflow is suspended waiting for physical action
	logger.Infof("Verifying remediation workflow is suspended on the node %s", nodeName)
	assert.Eventually(c, func() bool {
		return s.isWorkflowSuspended(c, nodeName)
	}, 5*time.Minute, 10*time.Second, "Remediation workflow did not reach suspended state")

	// abort workflow by adding label to node
	err = utils.AddNodeLabel(s.clientSet, nodeName, "operator.amd.com/gpu-abort-workflow", "true")
	assert.NoError(c, err, "Failed to add label to abort workflow")

	logger.Infof("Reverting Node Problem Detector (NPD) thresholds to original configuration")
	s.updateConfigForNPD(c, npdInband2RASErrorConfigPath, npdInbandRASConfigPath)

	//verify workflow is aborted and deleted
	logger.Infof("Verifying remediation workflow is aborted and deleted on the node %s", nodeName)
	assert.Eventually(c, func() bool {
		return s.checkWorkflowExistence(c, nodeName, false)
	}, 1*time.Minute, 10*time.Second, "Remediation workflow was not aborted and deleted")
	s.untaintNode(nodeName)
}

func (s *E2ESuite) TestAutoNodeRemediationRecoveryPolicy(c *C) {
	logger.Infof("Starting Auto Node Remediation recovery policy test")
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

	devCfg := s.populateDeviceConfig(c)

	//Clear previous state before starting the test
	logger.Infof("Clean-up RemediationWorkflowStatus CR before the test")
	s.clearRemediationWorkflowStatusMetaData(devCfg.Namespace, c)

	// Pre-populate RemediationWorkflowStatus with max retries
	logger.Infof("Pre-populate RemediationWorkflowStatus with max retries for node %s and condition %s", nodeName, conditionInternalError)
	s.addRemediationWorkflowStatusMetaData(devCfg.Namespace, nodeName, conditionInternalError, 4, c)

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

	logger.Infof("Verifying that node condition %s is added for the node %s", conditionInternalError, nodeName)
	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionFalse)

	// Trigger error condition by modifying NPD config
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdInbandRASConfigPath, npdInband2RASErrorConfigPath)

	s.verifyNodeCondition(c, conditionInternalError, corev1.ConditionTrue)

	// Verify remediation workflow is not started due to max retries reached
	logger.Infof("Verifying remediation workflow is not started on the node %s due to max retries reached", nodeName)
	assert.Eventually(c, func() bool {
		return s.checkWorkflowExistence(c, nodeName, false)
	}, 2*time.Minute, 10*time.Second, "Remediation workflow was started despite max retries reached")

	// Clear RemediationWorkflowStatus metadata
	logger.Infof("Clearing RemediationWorkflowStatus metadata for node %s and condition %s", nodeName, conditionInternalError)
	s.clearRemediationWorkflowStatusMetaData(devCfg.Namespace, c)

	// Verify remediation workflow is started and running now
	logger.Infof("Verifying remediation workflow is started on the node %s after clearing metadata", nodeName)
	assert.Eventually(c, func() bool {
		return s.checkWorkflowExistence(c, nodeName, true)
	}, 2*time.Minute, 10*time.Second, "Remediation workflow was started despite max retries reached")

	//verify workflow is suspended waiting for physical action
	logger.Infof("Verifying remediation workflow is suspended on the node %s", nodeName)
	assert.Eventually(c, func() bool {
		return s.isWorkflowSuspended(c, nodeName)
	}, 3*time.Minute, 10*time.Second, "Remediation workflow did not reach suspended state")

	// abort workflow by adding label to node
	logger.Infof("Aborting the suspended workflow")
	err = utils.AddNodeLabel(s.clientSet, nodeName, "operator.amd.com/gpu-abort-workflow", "true")
	assert.NoError(c, err, "Failed to add label to abort workflow")

	logger.Infof("Reverting Node Problem Detector (NPD) thresholds to original configuration")
	s.updateConfigForNPD(c, npdInband2RASErrorConfigPath, npdInbandRASConfigPath)

	//verify workflow is aborted and deleted
	logger.Infof("Verifying remediation workflow is aborted and deleted on the node %s", nodeName)
	assert.Eventually(c, func() bool {
		return s.checkWorkflowExistence(c, nodeName, false)
	}, 1*time.Minute, 10*time.Second, "Remediation workflow was not aborted and deleted")
	s.untaintNode(nodeName)
}
