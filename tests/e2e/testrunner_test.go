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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/testrunner"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	defaultRecipe = "gst_single"
)

var (
	defaultTestRunningLabel = map[string]string{
		"testrunner.amd.com.gpu_health_check.gst_single": "running",
	}
)

func (s *E2ESuite) checkTestRunnerStatus(devCfg *v1alpha1.DeviceConfig, expectDSExist bool, c *C) {
	if expectDSExist {
		assert.Eventually(c, func() bool {
			_, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), devCfg.Name+"-"+testrunner.TestRunnerName, metav1.GetOptions{})
			if err != nil {
				logger.Errorf("cannot find expected test runner daemonset, err %+v", err)
				return false
			}
			return true
		}, 5*time.Minute, 10*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			trDS, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), devCfg.Name+"-"+testrunner.TestRunnerName, metav1.GetOptions{})
			if err == nil {
				logger.Errorf("found expected test runner daemonset but expect it doesn't exist %+v", trDS)
				return false
			}
			return true
		}, 5*time.Minute, 10*time.Second)
	}
}

func (s *E2ESuite) simulateOneGPUUnhealthyStatus(ns, nodeName string, c *C) {
	// inject the UE to one of the exporter pod
	labelMap := make(map[string]string)
	logger.Infof("Marking GPU unhealthy")
	err := utils.SetGPUHealthOnNode(s.clientSet, ns, "0", "unhealthy", nodeName)
	assert.NoError(c, err, fmt.Sprintf("failed to mark GPU 0 unhealthy. Error:%v", err))
	labelMap["metricsexporter.amd.com.gpu.0.state"] = "unhealthy"
	logger.Print("Verifying unhealthy label on the node(s)")
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labelMap).String(),
		})
		if err != nil || len(nodes.Items) == 0 {
			return false
		}
		logger.Printf("Got %d nodes with unhealthy label", len(nodes.Items))
		return true
	}, 90*time.Second, 10*time.Second, "expected gpu 0 to become unhealthy but got healthy")
}

func (s *E2ESuite) deleteTestRunnerPod(node string, devCfg *v1alpha1.DeviceConfig, c *C) {
	// delete the test runner pod during the test
	// check logs to make sure that the test will be restarted
	// and test runner was bale to detect the incomplete test run and restart it
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node &&
				strings.Contains(pod.Name, devCfg.Name+"-"+testrunner.TestRunnerName) {
				err = s.clientSet.CoreV1().Pods(devCfg.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					logger.Printf("failed to delete pod %+v err %+v", pod.Name, err)
					return false
				}
				return true
			}
		}
		logger.Printf("cannot find test runner pods")
		return false
	}, 30*time.Second, 2*time.Second, "expected to delete test runner pod on node %+v", node)
}

func (s *E2ESuite) createTestRunnerConfigmap(valid bool, devCfg *v1alpha1.DeviceConfig, nodeName, recipe string, stopOnFailure bool, iterations, timeoutInSeconds int, c *C) string {
	cmName := fmt.Sprintf("%v-%v-%v-%v-%v-%v", valid, devCfg.Name, strings.ReplaceAll(recipe, "_", "-"), iterations, stopOnFailure, timeoutInSeconds)
	if nodeName == "" {
		nodeName = "global"
	}
	var cm *v1.ConfigMap
	if !valid {
		cm = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      devCfg.Spec.TestRunner.Config.Name,
				Namespace: devCfg.Namespace,
			},
			Data: map[string]string{
				"config.json": `{
					"TestConfig": "invalid configs"
				}`,
			},
		}
	} else {
		config := fmt.Sprintf(`{
			"TestConfig": {
				"GPU_HEALTH_CHECK": {
					"TestLocationTrigger": {
						"%v": {
							"TestParameters": {
								"AUTO_UNHEALTHY_GPU_WATCH": {
									"TestCases": [
										{
											"Recipe": "%v",
											"Iterations": %v,
											"StopOnFailure": %v,
											"TimeoutSeconds": %v
										}
									]
								}
							}
						}
					}
				}
			}
		}`, nodeName, recipe, iterations, stopOnFailure, timeoutInSeconds)
		cm = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: devCfg.Namespace,
			},
			Data: map[string]string{
				"config.json": config,
			},
		}
	}
	assert.Eventually(c, func() bool {
		_, err := s.clientSet.CoreV1().ConfigMaps(devCfg.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				return true
			}
			logger.Printf("failed to create configmap for test runner err %+v", err)
			return false
		}
		return true
	}, 30*time.Second, 2*time.Second, "failed to create configmap for test runner")
	return cmName
}

func (s *E2ESuite) scheduleWorkloadOnNodeWithMaxGPUs(c *C) string {
	ret, err := utils.GetAMDGPUCount(context.TODO(), s.clientSet)
	if err != nil {
		logger.Errorf("error: %v", err)
	}
	var maxPerNodeGPU int = 0
	var nodeWithMaxGPU string
	for nodeName, v := range ret {
		if v > maxPerNodeGPU {
			nodeWithMaxGPU = nodeName
			maxPerNodeGPU = v
		}
	}
	assert.Greater(c, maxPerNodeGPU, 0, "did not find any server with amd gpu")

	gpuLimitCount := maxPerNodeGPU
	gpuReqCount := maxPerNodeGPU

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
	err = utils.VerifyROCMPODResourceCount(context.TODO(), s.clientSet, gpuReqCount)
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	return nodeWithMaxGPU
}

func (s *E2ESuite) verifyRestartIncompleteTest(node string, devCfg *v1alpha1.DeviceConfig, c *C) {
	// new test runner pod will be brought up automatically by k8s
	// verify that its logs are saying it is restarting incomplete test
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node &&
				strings.Contains(pod.Name, devCfg.Name+"-"+testrunner.TestRunnerName) {
				req := s.clientSet.CoreV1().Pods(devCfg.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{Container: "test-runner-container"})
				podLogs, err := req.Stream(context.TODO())
				if err != nil {
					fmt.Printf("failed to get pod logs err %+v", err)
					return false
				}
				defer podLogs.Close()

				// Print the logs
				buf := new(bytes.Buffer)
				_, err = io.Copy(buf, podLogs)
				if err != nil {
					fmt.Printf("failed to get pod logs err %+v", err)
					return false
				}
				if strings.Contains(buf.String(), "incomplete test") {
					logger.Print("found test runner pod that has restarted the incomplete test")
					return true
				}
			}
		}
		logger.Printf("cannot find test runner pods restarting the incomplete test")
		return false
	}, 90*time.Second, 10*time.Second, "cannot find test runner pods restarting the incomplete test on node %+v", node)
}

func (s *E2ESuite) verifyFoundUnhealthyGPUWithWorkload(node string, devCfg *v1alpha1.DeviceConfig, c *C) {
	// new test runner pod will be brought up automatically by k8s
	// verify that its logs are saying unhealthy GPU status was detected and associated workload is also getting detected
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node &&
				strings.Contains(pod.Name, devCfg.Name+"-"+testrunner.TestRunnerName) {
				req := s.clientSet.CoreV1().Pods(devCfg.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{Container: "test-runner-container"})
				podLogs, err := req.Stream(context.TODO())
				if err != nil {
					fmt.Printf("failed to get pod logs err %+v", err)
					return false
				}
				defer podLogs.Close()

				// Print the logs
				buf := new(bytes.Buffer)
				_, err = io.Copy(buf, podLogs)
				if err != nil {
					fmt.Printf("failed to get pod logs err %+v", err)
					return false
				}
				if strings.Contains(buf.String(), "unhealthy but still associated with workload") {
					logger.Print("found test runner pod that has detected unhealthy GPU with associated workload")
					return true
				}
			}
		}
		logger.Printf("cannot find test runner pod that has detected unhealthy GPU with associated workload")
		return false
	}, 90*time.Second, 10*time.Second, "cannot find test runner pod that has detected unhealthy GPU with associated workload on node %+v", node)
}

func (s *E2ESuite) verifyTestResultEvts(node, recipe string, devCfg *v1alpha1.DeviceConfig, perEvtVerifyFunc func(v1.Event), c *C) {
	// verify that the test run event got generated
	logger.Print("Verifying test result event(s)")
	testEventLabel := map[string]string{
		"testrunner.amd.com/category": "gpu_health_check",
		"testrunner.amd.com/trigger":  "auto_unhealthy_gpu_watch",
		"testrunner.amd.com/recipe":   recipe,
		"testrunner.amd.com/hostname": node,
	}
	assert.Eventually(c, func() bool {
		evts, err := s.clientSet.CoreV1().Events(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(testEventLabel).String(),
		})
		if err != nil || len(evts.Items) == 0 {
			return false
		}
		logger.Printf("Got %d events with test events label: %+v", len(evts.Items), evts.Items)
		for _, evt := range evts.Items {
			// make sure that the event messages are json parsable
			assert.True(c, utils.IsJSONParsable(evt.Message), "event message is not json parsable %+v", evt)
			if perEvtVerifyFunc != nil {
				perEvtVerifyFunc(evt)
			}
		}
		return true
	}, 720*time.Second, 2*time.Second, "expected test run result event but got nothing")
}

func (s *E2ESuite) verifyTestRunningLabel(expect bool, testRunningLabel map[string]string, c *C) string {
	hostName := ""
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(testRunningLabel).String(),
		})
		if err != nil {
			logger.Printf("failed to list nodes err %+v", err)
			return false
		}
		if expect {
			if len(nodes.Items) == 0 {
				return false
			}
			hostName = nodes.Items[0].Name
			logger.Printf("Got %d nodes with test running label", len(nodes.Items))
			return true
		} else {
			if len(nodes.Items) != 0 {
				return false
			}
			logger.Printf("Got %d nodes with test running label", len(nodes.Items))
			return true
		}
	}, 90*time.Second, 2*time.Second, "expected test running label %+v exist: %+v", testRunningLabel, expect)
	return hostName
}

func (s *E2ESuite) cleanupTestRunnerEvts(devCfg *v1alpha1.DeviceConfig, c *C) {
	// cleanup
	// need to remove the existing test runner event
	// so that other test runner test cases won't be affected
	logger.Print("Clean up test runner events")
	assert.Eventually(c, func() bool {
		evts, err := s.clientSet.CoreV1().Events(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			logger.Printf("failed to list events err %+v", err)
			return false
		}
		for _, evt := range evts.Items {
			if strings.Contains(evt.Name, "amd-test-runner") {
				err = s.clientSet.CoreV1().Events(devCfg.Namespace).Delete(context.TODO(), evt.Name, metav1.DeleteOptions{})
				if err != nil {
					logger.Printf("failed to delete event %+v err %+v", evt.Name, err)
					return false
				}
			}
		}
		return true
	}, 60*time.Second, 15*time.Second, "expected test runner events to be cleaned up")
}

func (s *E2ESuite) TestTestRunnerEnablement(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// test runner shouldn't be brought up when it is disabled
	enableTestRunner := false
	enableExporter := false
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.TestRunner.Image = testRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkTestRunnerStatus(devCfg, false, c)
	// if we only enable test runner but didn't enable exporter, test runner daemonset shouldn't be brought up
	enableTestRunner = true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	s.patchTestRunnerEnablement(devCfg, c)
	s.checkTestRunnerStatus(devCfg, false, c)
	// enable both metrics exporter and test runner will bring up test runner daemonset
	enableTestRunner = true
	enableExporter = true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	s.patchTestRunnerEnablement(devCfg, c)
	s.patchMetricsExporterEnablement(devCfg, c)
	s.checkTestRunnerStatus(devCfg, true, c)
}

func (s *E2ESuite) TestTestRunnerAutoUnhealthyGPUWatchTrigger(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// test runner should be brought up
	// when both exporter and test runner are enabled
	enableTestRunner := true
	enableExporter := true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.TestRunner.Image = testRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.checkTestRunnerStatus(devCfg, true, c)

	s.cleanupTestRunnerEvts(devCfg, c)
	s.simulateOneGPUUnhealthyStatus(devCfg.Namespace, "", c)
	logger.Print("Verifying test running label on the node(s)")
	hostName := s.verifyTestRunningLabel(true, defaultTestRunningLabel, c)

	// delete the test runner pod during the test
	// check logs to make sure that the test will be restarted
	// and test runner was bale to detect the incomplete test run and restart it
	s.deleteTestRunnerPod(hostName, devCfg, c)
	// new test runner pod will be brought up automatically by k8s
	// verify that its logs are saying it is restarting incomplete test
	s.verifyRestartIncompleteTest(hostName, devCfg, c)

	// verify that the test run event got generated
	s.verifyTestResultEvts(hostName, defaultRecipe, devCfg, nil, c)

	// verify that the test running label gets removed after the test completed
	logger.Print("Verifying that the test running label gets removed after the test completed")
	s.verifyTestRunningLabel(false, defaultTestRunningLabel, c)

	// cleanup
	// need to remove the existing test runner event
	// so that other test runner test cases won't be affected
	s.cleanupTestRunnerEvts(devCfg, c)
}

func (s *E2ESuite) TestTestRunnerNodeSpecificConfig(c *C) {
	// create a config map with node name specified config
	// verify that the test runner is using node specific config
	// not the global default config
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// test runner should be brought up
	// when both exporter and test runner are enabled
	enableTestRunner := true
	enableExporter := true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.TestRunner.Image = testRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.checkTestRunnerStatus(devCfg, true, c)

	s.cleanupTestRunnerEvts(devCfg, c)
	s.simulateOneGPUUnhealthyStatus(devCfg.Namespace, "", c)
	logger.Print("Verifying test running label on the node(s)")
	hostName := s.verifyTestRunningLabel(true, defaultTestRunningLabel, c)

	testRecipe := "babel"
	cmName := s.createTestRunnerConfigmap(true, devCfg, hostName, testRecipe, false, 1, 600, c)
	devCfg.Spec.TestRunner.Config = &v1.LocalObjectReference{
		Name: cmName,
	}
	s.patchTestRunnerConfigmap(devCfg, c)
	// verify that the test run event got generated
	s.verifyTestResultEvts(hostName, testRecipe, devCfg, nil, c)

	// try to recover GPU health, then convert it to unhealthy again
	// to verify the node name specific config works on other test recipe
	err = utils.SetGPUHealthOnNode(s.clientSet, devCfg.Namespace, "0", "healthy", hostName)
	assert.NoError(c, err, fmt.Sprintf("failed to mark GPU 0 healthy. Error:%v", err))
	time.Sleep(90 * time.Second) // give enough time for test runner to recognize the GPU becomes healthy
	testRecipe = "gst_single"
	cmName = s.createTestRunnerConfigmap(true, devCfg, hostName, testRecipe, false, 1, 600, c)
	devCfg.Spec.TestRunner.Config = &v1.LocalObjectReference{
		Name: cmName,
	}
	s.patchTestRunnerConfigmap(devCfg, c)
	err = utils.SetGPUHealthOnNode(s.clientSet, devCfg.Namespace, "0", "unhealthy", hostName)
	assert.NoError(c, err, fmt.Sprintf("failed to mark GPU 0 unhealthy. Error:%v", err))
	// verify that the test run event got generated
	s.verifyTestResultEvts(hostName, testRecipe, devCfg, nil, c)

	// verify that the test running label gets removed after the test completed
	logger.Print("Verifying that the test running label gets removed after the test completed")
	s.verifyTestRunningLabel(false, defaultTestRunningLabel, c)

	// cleanup
	// need to remove the existing test runner event
	// so that other test runner test cases won't be affected
	s.cleanupTestRunnerEvts(devCfg, c)
}

func (s *E2ESuite) TestTestRunnerMultipleIterations(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// test runner should be brought up
	// when both exporter and test runner are enabled
	enableTestRunner := true
	enableExporter := true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.TestRunner.Image = testRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.Driver.Version = "6.3.2"
	// configure test runner to run 3 iterations of gst_single
	iterations := 3
	cmName := s.createTestRunnerConfigmap(true, devCfg, "", "gst_single", false, iterations, 600, c)
	devCfg.Spec.TestRunner.Config = &v1.LocalObjectReference{
		Name: cmName,
	}
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.checkTestRunnerStatus(devCfg, true, c)

	s.cleanupTestRunnerEvts(devCfg, c)
	s.simulateOneGPUUnhealthyStatus(devCfg.Namespace, "", c)
	logger.Print("Verifying test running label on the node(s)")
	hostName := s.verifyTestRunningLabel(true, defaultTestRunningLabel, c)

	// delete the test runner pod during the test
	// check logs to make sure that the test will be restarted
	// and test runner was bale to detect the incomplete test run and restart it
	s.deleteTestRunnerPod(hostName, devCfg, c)
	// new test runner pod will be brought up automatically by k8s
	// verify that its logs are saying it is restarting incomplete test
	s.verifyRestartIncompleteTest(hostName, devCfg, c)

	// verify that the test run event got generated
	verifyIterationsSummary := func(evt v1.Event) {
		for iter := 1; iter <= iterations; iter++ {
			assert.True(c,
				strings.Contains(evt.Message, fmt.Sprintf(`"number":%v`, iter)),
				"event is expected to have message for all %+v iterations but got %+v",
				iterations, evt)
		}
	}
	s.verifyTestResultEvts(hostName, defaultRecipe, devCfg, verifyIterationsSummary, c)

	// verify that the test running label gets removed after the test completed
	logger.Print("Verifying that the test running label gets removed after the test completed")
	s.verifyTestRunningLabel(false, defaultTestRunningLabel, c)

	// cleanup
	// need to remove the existing test runner event
	// so that other test runner test cases won't be affected
	s.cleanupTestRunnerEvts(devCfg, c)
}

func (s *E2ESuite) TestTestRunnerAssociatedWorkloadOnUnhealthyGPU(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	// test runner should be brought up
	// when both exporter and test runner are enabled
	enableTestRunner := true
	enableExporter := true
	devCfg.Spec.TestRunner.Enable = &enableTestRunner
	devCfg.Spec.TestRunner.Image = testRunnerImage
	devCfg.Spec.MetricsExporter.Enable = &enableExporter
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.Driver.Version = "6.3.2"
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.checkTestRunnerStatus(devCfg, true, c)

	s.cleanupTestRunnerEvts(devCfg, c)
	// schedule sample workload pods on nodes with maximum GPUs
	nodeName := s.scheduleWorkloadOnNodeWithMaxGPUs(c)
	defer func() {
		assert.NoError(c, utils.DelRocmPods(context.TODO(), s.clientSet), "failed to delete workload pods")
	}()
	// for a given node with workload scheduled
	// simulate the UE unhealthy status
	// make sure test runner detected unhealthy status but won't trigger test due to existing workload
	s.simulateOneGPUUnhealthyStatus(devCfg.Namespace, nodeName, c)
	time.Sleep(time.Minute) // wait for 1 minute in case any test run was triggered unexpectedly
	s.verifyFoundUnhealthyGPUWithWorkload(nodeName, devCfg, c)
	s.verifyTestRunningLabel(false, defaultTestRunningLabel, c)
	s.cleanupTestRunnerEvts(devCfg, c)
}
