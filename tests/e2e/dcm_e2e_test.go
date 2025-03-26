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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/configmanager"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

var (
	dcmImage        string
	dcmImageDefined bool
)

func init() {
	dcmImage, dcmImageDefined = os.LookupEnv("E2E_DCM_IMAGE")
}

type GPUConfigProfiles struct {
	ProfilesList map[string]*GPUConfigProfile `json:"gpu-config-profiles,omitempty"`
}

type ProfileConfig struct {
	ComputePartition string `json:"computePartition,omitempty"`
	MemoryPartition  string `json:"memoryPartition,omitempty"`
	NumGPUsAssigned  uint32 `json:"numGPUsAssigned,omitempty"`
}

type SkippedGPUs struct {
	Id []uint32 `json:"ids,omitempty"`
}

type GPUConfigProfile struct {
	Filters  *SkippedGPUs     `json:"skippedGPUs,omitempty"`
	Profiles []*ProfileConfig `json:"profiles,omitempty"`
}

func (s *E2ESuite) addRemoveNodeLabels(nodeName string, selectedProfile string) {
	err := utils.AddNodeLabel(s.clientSet, nodeName, "dcm.amd.com/gpu-config-profile", selectedProfile)
	_ = utils.AddNodeLabel(s.clientSet, nodeName, "dcm.amd.com/apply-gpu-config-profile", "apply")
	if err != nil {
		logger.Infof("Error adding node lbels: %s\n", err.Error())
		return
	}
	time.Sleep(45 * time.Second)
	// Allow partition to happen
	err = utils.DeleteNodeLabel(s.clientSet, nodeName, "dcm.amd.com/gpu-config-profile")
	_ = utils.DeleteNodeLabel(s.clientSet, nodeName, "dcm.amd.com/apply-gpu-config-profile")
	if err != nil {
		logger.Infof("Error removing node lbels: %s\n", err.Error())
		return
	}
}

func (s *E2ESuite) verifyNoConfigManager(devCfg *v1alpha1.DeviceConfig, c *C) {
	ns := devCfg.Namespace
	assert.Eventually(c, func() bool {
		if _, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), devCfg.Name+"-"+configmanager.ConfigManagerName,
			metav1.GetOptions{}); err == nil {
			logger.Warnf("config manager exists: %+v %v", devCfg, err)
			return false
		}

		if _, err := s.clientSet.CoreV1().Services(ns).Get(context.TODO(),
			devCfg.Name+"-"+configmanager.ConfigManagerName, metav1.GetOptions{}); err == nil {
			logger.Warnf("config manager service exists")
			return false
		}

		return true
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) GetPodName(ns string) (string, error) {
	podList, err := s.clientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, "device-config-manager") {
			return pod.Name, nil
		}
	}
	return "", nil
}

func (s *E2ESuite) GetLatestEvents(ns string) ([]corev1.Event, error) {

	dsName := s.cfgName + "-device-config-manager"
	fieldSelector := fields.Set{
		"involvedObject.name": dsName,
	}.AsSelector().String()

	listOptions := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	eventListFromDs, err := s.clientSet.CoreV1().Events(ns).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("error getting events: %w", err)
	}

	podName, _ := s.GetPodName(s.ns)

	fieldSelector = fields.Set{
		"involvedObject.name": podName,
	}.AsSelector().String()

	listOptions = metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	eventListFromPods, err := s.clientSet.CoreV1().Events(ns).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("error getting events: %w", err)
	}

	var eventList []corev1.Event
	eventList = append(eventList, eventListFromDs.Items...)
	eventList = append(eventList, eventListFromPods.Items...)

	var filteredEvents []corev1.Event
	currentTime := time.Now()
	threshold := currentTime.Add(-2 * time.Minute)

	for _, event := range eventList {
		firstEventTime := event.FirstTimestamp.Time
		lastEventTime := event.LastTimestamp.Time
		if firstEventTime.After(threshold) || lastEventTime.After(threshold) {
			filteredEvents = append(filteredEvents, event)
		}
	}
	// Sort the events by time in case we have multiple events
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].LastTimestamp.Time.After(filteredEvents[j].LastTimestamp.Time)
	})

	return filteredEvents, nil
}

func (s *E2ESuite) getLogs() string {
	podName, _ := s.GetPodName(s.ns)
	rs, err := s.clientSet.CoreV1().Pods(s.ns).GetLogs(podName, &corev1.PodLogOptions{
		Container: "device-config-manager-container",
		Follow:    true,
	}).Stream(context.TODO())
	if err != nil {
		logger.Infof("Error getting pod logs: %s\n", err.Error())
		return ""
	}
	defer rs.Close()
	// Read the entire log stream at once
	// Use a bufio.Reader to read the stream line by line
	reader := bufio.NewReader(rs)
	done := make(chan bool)

	var logs string
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			logs += line
		}
	}()

	select {
	case <-done:
		// Break the loop when the end of the stream is reached
	case <-time.After(10 * time.Second):
		// Timeout after few seconds
		logger.Info("Collecting logs")
	}
	logger.Infof("Pod logs\n %v", logs)
	return logs
}

func (s *E2ESuite) createConfigMap() GPUConfigProfiles {
	skippedGPUs := &SkippedGPUs{
		Id: []uint32{},
	}

	skippedGPUs2 := &SkippedGPUs{
		Id: []uint32{2, 3},
	}

	profiles_set1 := []*ProfileConfig{
		{
			ComputePartition: "SPX",
			MemoryPartition:  "NPS1",
		},
	}

	profiles_set2 := []*ProfileConfig{
		{
			ComputePartition: "CPX",
			MemoryPartition:  "NPS1",
			NumGPUsAssigned:  1,
		},
		{
			ComputePartition: "DPX",
			MemoryPartition:  "NPS1",
			NumGPUsAssigned:  4,
		},
		{
			ComputePartition: "SPX",
			MemoryPartition:  "NPS1",
			NumGPUsAssigned:  1,
		},
	}

	profiles_set3 := []*ProfileConfig{
		{
			ComputePartition: "InvalidName",
			MemoryPartition:  "NPS1",
			NumGPUsAssigned:  1,
		},
	}

	profiles_set4 := []*ProfileConfig{
		{
			ComputePartition: "SPX",
			MemoryPartition:  "InvalidName",
			NumGPUsAssigned:  1,
		},
	}

	profiles_set5 := []*ProfileConfig{
		{
			ComputePartition: "SPX",
			MemoryPartition:  "NPS1",
			NumGPUsAssigned:  100,
		},
	}

	profiles_set6 := []*ProfileConfig{
		{
			ComputePartition: "CPX",
			MemoryPartition:  "NPS4",
			NumGPUsAssigned:  1,
		},
	}

	profileslist := GPUConfigProfiles{
		ProfilesList: map[string]*GPUConfigProfile{
			"default": {
				Filters:  skippedGPUs,
				Profiles: profiles_set1,
			},
			"e2e_profile1": {
				Filters:  skippedGPUs2,
				Profiles: profiles_set2,
			},
			"e2e_profile2": {
				Filters:  skippedGPUs,
				Profiles: profiles_set6,
			},
			"inval_prof1": {
				Filters:  skippedGPUs,
				Profiles: profiles_set3,
			},
			"inval_prof2": {
				Filters:  skippedGPUs,
				Profiles: profiles_set4,
			},
			"inval_prof3": {
				Filters:  skippedGPUs,
				Profiles: profiles_set5,
			},
		},
	}

	return profileslist
}

func (s *E2ESuite) configMapHelper(c *C) {
	logger.Infof("###BEGIN TESTCASE###\n")
	// check to see existing deviceconfig DS pods
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	// fetch the CR
	devCfg := s.getDeviceConfigForDCM(c)
	logger.Infof("create device-config %+v", devCfg.Spec.ConfigManager)
	s.createDeviceConfig(devCfg, c)

	s.checkDeviceConfigManagerStatus(devCfg, s.ns, c)
	logger.Infof("SUCCESSFULLY DEPLOYED DCM DAEMONSET")

	profileslist := s.createConfigMap()

	cfgData, err := json.Marshal(profileslist)
	assert.NoError(c, err, "failed to marshal config data")

	mcfgMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      devCfg.Name,
			Namespace: devCfg.Namespace,
		},
		Data: map[string]string{
			"config.json": string(cfgData),
		},
	}

	_, err = s.clientSet.CoreV1().ConfigMaps(devCfg.Namespace).Create(context.TODO(), mcfgMap, metav1.CreateOptions{})
	assert.NoError(c, err, "failed to create configmap %v", mcfgMap.Data)

	logger.Infof("Configmap created successfully.\n")
	time.Sleep(5 * time.Second)
	updConfig, err := s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")
	updConfig.Spec.ConfigManager.Config = &corev1.LocalObjectReference{Name: devCfg.Name}

	logger.Infof("update configmanager-config %+v", updConfig.Spec.ConfigManager)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkDeviceConfigManagerStatus(updConfig, s.ns, c)
}

func (s *E2ESuite) getWorkerNode(c *C) string {
	devCfg := s.getDeviceConfigForDCM(c)
	nodes, _ := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s,node-role.kubernetes.io/worker", kmmmodule.MapToLabelSelector(devCfg.Spec.Selector)),
	})
	if s.simEnable {
		nodes, _ = s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	}
	assert.True(c, len(nodes.Items) > 0, "no nodes with gpu", len(nodes.Items))
	nodeName := nodes.Items[0].Name
	return string(nodeName)
}

func (s *E2ESuite) eventHelper(reason string, event_type string) bool {
	events, _ := s.GetLatestEvents(s.ns)
	for _, event := range events {
		if (event.Reason == reason) && (event.Type == event_type) {
			logger.Infof("====================================\n")
			logger.Infof("Namespace: %s\n", event.InvolvedObject.Namespace)
			logger.Infof("Object Name: %s\n", event.InvolvedObject.Name)
			logger.Infof("Object Kind: %s\n", event.InvolvedObject.Kind)
			logger.Infof("Event Type: %s\n", event.Type)
			logger.Infof("Event Reason: %s\n", event.Reason)
			logger.Infof("Event Message: %s\n", event.Message)
			logger.Infof("First Timestamp: %s\n", event.FirstTimestamp.Time)
			logger.Infof("Last Timestamp: %s\n", event.LastTimestamp.Time)
			logger.Infof("Count: %d\n", event.Count)
			logger.Infof("====================================\n")
			return true
		}
	}
	return false
}

func (s *E2ESuite) TestDCMConfigMapCreation(c *C) {
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	if s.eventHelper("SuccessfulCreate", "Normal") {
		logger.Infof("###DCM deployed successfully with a config map###\n")
	} else {
		logger.Infof("###DCM deployment unsuccessful with a config map###\n")
	}
}

func (s *E2ESuite) TestDCMConfigMapPartitionHomogenous(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "default")

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogenous default partitioning")
	} else {
		logger.Errorf("Failure test homogenous partitioning")
	}
}

func (s *E2ESuite) TestDCMConfigMapPartitionHeterogenous(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "e2e_profile1")

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested heterogenous partitioning")
	} else {
		logger.Errorf("Failure test heterogenous partitioning")
	}
}

func (s *E2ESuite) TestDCMPartitionNPS4(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "e2e_profile2")
	time.Sleep(30 * time.Second)

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested NPS4 partitioning")
	} else {
		logger.Errorf("Failure test NPS4 partitioning")
	}
}

func (s *E2ESuite) TestDCMInvalidComputeType(c *C) {
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "inval_prof1")

	logs := s.getLogs()
	if strings.Contains(logs, "Invalid partition types") && (s.eventHelper("InvalidComputeType", "Warning")) {
		logger.Infof("Successfully tested invalid compute type profile")
	} else {
		logger.Errorf("Failure testing invalid compute type")
	}
}

func (s *E2ESuite) TestDCMInvalidMemoryType(c *C) {
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "inval_prof2")

	logs := s.getLogs()
	if strings.Contains(logs, "Invalid partition types") && (s.eventHelper("InvalidMemoryType", "Warning")) {
		logger.Infof("Successfully tested invalid memory type profile")
	} else {
		logger.Errorf("Failure testing invalid memory type")
	}
}

func (s *E2ESuite) TestDCMInvalidGPUFilter(c *C) {
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	s.configMapHelper(c)
	// Trigger partition using labels
	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	logger.Infof("NODE NAME %v", nodeName)

	s.addRemoveNodeLabels(nodeName, "inval_prof3")

	logs := s.getLogs()
	if strings.Contains(logs, "exceeding the total number") && strings.Contains(logs, "ERROR") && (s.eventHelper("InvalidProfileInfo", "Warning")) {
		logger.Infof("Successfully tested invalid GPU filter profile")
	} else {
		logger.Errorf("Failure testing invalid GPU filter profile")
	}
}

func (s *E2ESuite) TestDCMDefaultPartition(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}
	logger.Infof("###BEGIN TESTCASE###\n")
	// check to see existing deviceconfig DS pods
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	// fetch the CR
	devCfg := s.getDeviceConfigForDCM(c)
	logger.Infof("create device-config %+v", devCfg.Spec.ConfigManager)
	s.createDeviceConfig(devCfg, c)

	s.checkDeviceConfigManagerStatus(devCfg, s.ns, c)
	logger.Infof("SUCCESSFULLY DEPLOYED DCM DAEMONSET")
	time.Sleep(30 * time.Second)

	nodeName := s.getWorkerNode(c)
	err = utils.AddNodeLabel(s.clientSet, nodeName, "dcm.amd.com/apply-gpu-config-profile", "apply")
	if err != nil {
		logger.Infof("Error adding node lbels: %s\n", err.Error())
		return
	}
	time.Sleep(15 * time.Second)
	// Allow partition to happen
	err = utils.DeleteNodeLabel(s.clientSet, nodeName, "dcm.amd.com/apply-gpu-config-profile")
	if err != nil {
		logger.Infof("Error removing node lbels: %s\n", err.Error())
		return
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested default partitioning")
	} else {
		logger.Errorf("Failure testing default partitioning")
	}
}

func (s *E2ESuite) TestConfigManagerDeploymentOnly(c *C) {
	// Run on SIM and Non SIM Setups
	configManagerEnable := false
	logger.Infof("###BEGIN TESTCASE 1###\n")
	// check to see existing deviceconfig DS pods
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	// fetch the CR
	devCfg := s.getDeviceConfigForDCM(c)
	devCfg.Spec.ConfigManager.Enable = &configManagerEnable
	logger.Infof("create device-config %+v", devCfg.Spec.ConfigManager)
	s.createDeviceConfig(devCfg, c)
	s.verifyNoConfigManager(devCfg, c)

	updConfig, err := s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")

	configManagerEnable = true
	updConfig.Spec.ConfigManager.Enable = &configManagerEnable

	logger.Infof("update dcm-config %+v", updConfig.Spec.ConfigManager)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)

	s.checkDeviceConfigManagerStatus(updConfig, s.ns, c)
	logger.Infof("SUCCESSFULLY DEPLOYED DCM DAEMONSET")

	logger.Infof("###END TESTCASE###\n")

}
