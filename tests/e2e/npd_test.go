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
	"os"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	npdNamespace                                    = "kube-system"
	npdServiceAccountPath                           = "./yamls/config/npd/node-problem-detector-rbac.yaml"
	npdCustomPluginMonitorConfigPath                = "./yamls/config/npd/node-problem-detector-config.yaml"
	npdCustomPluginMonitorErrorConfigPath           = "./yamls/config/npd/node-problem-detector-error-config.yaml"
	npdMTLSCustomPluginMonitorConfigPath            = "./yamls/config/npd/node-problem-detector-config-mtls.yaml"
	npdMTLSCustomPluginMonitorErrorConfigPath       = "./yamls/config/npd/node-problem-detector-error-config-mtls.yaml"
	npdPrometheusCustomPluginMonitorConfigPath      = "yamls/config/npd/node-problem-detector-config-prom.yaml"
	npdPrometheusCustomPluginMonitorErrorConfigPath = "yamls/config/npd/node-problem-detector-error-config-prom.yaml"
	npdDaemonSetPath                                = "./yamls/config/npd/node-problem-detector.yaml"
	npdMTLSDaemonSetPath                            = "./yamls/config/npd/node-problem-detector-mtls.yaml"
	prometheusServiceMonitorPath                    = "./yamls/config/npd/prometheus-servicemonitor.yaml"
)

func kubectlCreateCmd(filePath string) {
	cmd := fmt.Sprintf("kubectl create -f %s", filePath)
	utils.RunCommand(cmd)
}

func kubectlDeleteCmd(filePath string) {
	cmd := fmt.Sprintf("kubectl delete -f %s", filePath)
	utils.RunCommand(cmd)
}

func setupNPD(saFilePath, configFilePath, daemonSetFilePath string) {
	kubectlCreateCmd(saFilePath)
	if configFilePath != "" {
		kubectlCreateCmd(configFilePath)
	}
	kubectlCreateCmd(daemonSetFilePath)
}

func tearDownNPD(saFilePath, configFilePath, daemonSetFilePath string) {
	kubectlDeleteCmd(daemonSetFilePath)
	if configFilePath != "" {
		kubectlDeleteCmd(configFilePath)
	}
	kubectlDeleteCmd(saFilePath)
}

func (s *E2ESuite) getPrometheusEndpointURL() (string, error) {
	// Get the Prometheus endpoint
	endpoints, err := s.clientSet.CoreV1().Endpoints("default").Get(context.Background(), "prometheus-operated", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get endpoints for service prometheus-operated: %v", err)
	}
	var endpointIP string
	var port int32
	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 && len(subset.Ports) > 0 {
			endpointIP = subset.Addresses[0].IP
			port = subset.Ports[0].Port
			break
		}
	}
	if endpointIP == "" || port == 0 {
		return "", fmt.Errorf("no valid endpoint found for service prometheus-operated")
	}

	return fmt.Sprintf("http://%s:%d", endpointIP, port), nil
}

func (s *E2ESuite) createNPDConfigWithPrometheusEndpoint(conf, prometheusEndpoint string) (string, error) {
	fileContent, err := os.ReadFile(conf)
	if err != nil {
		return "", err
	}
	fileContentString := string(fileContent)
	// Replace the placeholder with the actual Prometheus endpoint
	fileContentString = strings.ReplaceAll(fileContentString, "$$PROM_ENDPOINT$$", prometheusEndpoint)

	utils.RunCommand(fmt.Sprintf("echo '%s' | kubectl apply -f -", fileContentString))
	return fileContentString, nil
}

func deleteNPDConfig(conf string) {
	cmd := fmt.Sprintf("echo '%s' | kubectl delete -f -", conf)
	utils.RunCommand(cmd)
}

func (s *E2ESuite) restartNPDPods(c *C) {
	// Restart the NPD pods to apply the new config
	err := s.clientSet.CoreV1().Pods(npdNamespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=node-problem-detector",
	})
	assert.NoError(c, err, "unable to restart npd pods")
}

func (s *E2ESuite) updateConfigForNPD(c *C, existingConfigPath, newConfigPath string) {
	// Update the NPD config
	kubectlDeleteCmd(existingConfigPath)
	kubectlCreateCmd(newConfigPath)
	// restart the NPD pods to apply the new error config
	s.restartNPDPods(c)
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

func setupPrometheusOperator() {
	// Deploy prometheus operator
	helmCmds := []string{
		"helm repo add prometheus-community https://prometheus-community.github.io/helm-charts",
		"helm repo update",
		"helm install prometheus-e2e prometheus-community/kube-prometheus-stack",
	}
	for _, cmd := range helmCmds {
		utils.RunCommand(cmd)
	}
}

func tearDownPrometheusOperator() {
	// Uninstall prometheus operator
	helmCmds := []string{
		"helm uninstall prometheus-e2e",
	}
	for _, cmd := range helmCmds {
		utils.RunCommand(cmd)
	}
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
	setupNPD(npdServiceAccountPath, npdCustomPluginMonitorConfigPath, npdDaemonSetPath)
	defer tearDownNPD(npdServiceAccountPath, npdCustomPluginMonitorConfigPath, npdDaemonSetPath)

	// Check if NPD is running on all GPU nodes
	logger.Infof("Verify if Node Problem Detector (NPD) is running on all GPU nodes")
	s.verifyNPDRunning(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)

	//update npd config to to trigger error in Node condition
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdCustomPluginMonitorConfigPath, npdCustomPluginMonitorErrorConfigPath)

	// Check if NPD has detected the error condition
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to true")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionTrue)

	// restore NPD config to original state
	logger.Infof("Restore Node Problem Detector (NPD) config to original state")
	s.updateConfigForNPD(c, npdCustomPluginMonitorErrorConfigPath, npdCustomPluginMonitorConfigPath)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)
}

func (s *E2ESuite) TestNPDWithTLSEnabledOnExporter(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	// setup required certs
	caCert, serverCert, serverKey, clientCert, clientKey, err := s.setupKubeRbacCerts(c, true)
	assert.NoError(c, err)

	// Secret for metrics exporter TLS
	secretName := "kube-tls-secret"
	err = utils.CreateTLSSecret(context.TODO(), s.clientSet, secretName, s.ns, serverCert, serverKey)
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeleteTLSSecret(context.TODO(), s.clientSet, secretName, s.ns); errDel != nil {
			logger.Errorf("failed to delete TLS secret %s: %+v", secretName, errDel)
		}
	}()

	// Client CA ConfigMap
	cmName := "client-ca-cm"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: s.ns},
		Data:       map[string]string{"ca.crt": string(caCert)},
	}
	_, err = s.clientSet.CoreV1().ConfigMaps(s.ns).Create(context.TODO(), cm, metav1.CreateOptions{})
	assert.NoError(c, err)
	defer func() {
		if errDel := s.clientSet.CoreV1().ConfigMaps(s.ns).Delete(context.TODO(), cmName, metav1.DeleteOptions{}); errDel != nil {
			logger.Errorf("failed to delete ConfigMap %s: %+v", cmName, errDel)
		}
	}()

	// ----Below set of secrets are for client/NPD----
	// client certificate and key for NPD
	clientSecretName := "npd-client-cert"
	err = utils.CreateTLSSecret(context.TODO(), s.clientSet, clientSecretName, npdNamespace, clientCert, clientKey)
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeleteTLSSecret(context.TODO(), s.clientSet, clientSecretName, npdNamespace); errDel != nil {
			logger.Errorf("failed to delete TLS secret %s: %+v", clientSecretName, errDel)
		}
	}()

	// create root-ca secret in npd namespace to validate exporter's certificates
	rootCaSecretName := "exporter-rootca"
	secretKeys := make(map[string]string)
	secretKeys["ca.crt"] = string(caCert)
	err = utils.CreateOpaqueSecret(context.Background(), s.clientSet, rootCaSecretName, npdNamespace, secretKeys)
	assert.NoError(c, err, fmt.Sprintf("root-ca-secret creation is expected to succeed. Failed with error %v", err))
	defer func() {
		utils.DeleteOpaqueSecret(context.Background(), s.clientSet, rootCaSecretName, npdNamespace)
	}()

	// Create device config with metrics exporter enabled and TLS settings
	_, err = s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("expected no config to be present. but config %v exists", s.cfgName))

	exporterEnable := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	enableDriver := false
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.MetricsExporter.Enable = &exporterEnable
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.ImagePullPolicy = "Always"
	devCfg.Spec.MetricsExporter.Port = 5001
	devCfg.Spec.MetricsExporter.NodePort = 31000
	devCfg.Spec.MetricsExporter.SvcType = v1alpha1.ServiceTypeNodePort
	devCfg.Spec.MetricsExporter.RbacConfig = v1alpha1.KubeRbacConfig{
		Enable:       &enableKubeRbacProxy,
		DisableHttps: &disableHTTPs,
	}
	devCfg.Spec.MetricsExporter.RbacConfig.Secret = &corev1.LocalObjectReference{Name: secretName}
	devCfg.Spec.MetricsExporter.RbacConfig.ClientCAConfigMap = &corev1.LocalObjectReference{Name: cmName}
	devCfg.Spec.Driver.Enable = &enableDriver

	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, corev1.ServiceTypeNodePort, c)

	// Create NPD daemonset with TLS options and required service account
	logger.Infof("Setting up Node Problem Detector (NPD)")
	setupNPD(npdServiceAccountPath, npdMTLSCustomPluginMonitorConfigPath, npdMTLSDaemonSetPath)
	defer tearDownNPD(npdServiceAccountPath, npdMTLSCustomPluginMonitorConfigPath, npdMTLSDaemonSetPath)

	// Check if NPD is running on all GPU nodes
	logger.Infof("Verify if Node Problem Detector (NPD) is running on all GPU nodes")
	s.verifyNPDRunning(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)

	//update npd config to to trigger error in Node condition
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	s.updateConfigForNPD(c, npdMTLSCustomPluginMonitorConfigPath, npdMTLSCustomPluginMonitorErrorConfigPath)

	// Check if NPD has detected the error condition
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to true")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionTrue)

	// restore NPD config to original state
	logger.Infof("Restore Node Problem Detector (NPD) config to original state")
	s.updateConfigForNPD(c, npdMTLSCustomPluginMonitorErrorConfigPath, npdMTLSCustomPluginMonitorConfigPath)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)
}

func (s *E2ESuite) TestNPDWithPrometheus(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("expected no config to be present. but config %v exists", s.cfgName))

	exporterEnable := true
	driverEnable := false
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.MetricsExporter.Enable = &exporterEnable
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.ImagePullPolicy = "Always"
	devCfg.Spec.MetricsExporter.Port = 5000
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.MetricsExporter.Prometheus = &v1alpha1.PrometheusConfig{
		ServiceMonitor: &v1alpha1.ServiceMonitorConfig{
			Enable:          &exporterEnable,
			Interval:        "15s",
			HonorLabels:     &exporterEnable,
			HonorTimestamps: &exporterEnable,
		},
	}

	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, corev1.ServiceTypeClusterIP, c)

	logger.Infof("Setting up Prometheus Operator")
	setupPrometheusOperator()
	defer tearDownPrometheusOperator()

	logger.Infof("-----Waiting for 1 minute for Prometheus operator to come up-----")
	time.Sleep(1 * time.Minute)

	// Create Prometheus ServiceMonitor for NPD
	logger.Infof("Creating Prometheus ServiceMonitor to scrape Exporter metrics")
	kubectlCreateCmd(prometheusServiceMonitorPath)
	defer kubectlDeleteCmd(prometheusServiceMonitorPath)

	logger.Infof("-----Waiting for 1 minute for Prometheus to scrape metrics-----")
	time.Sleep(1 * time.Minute)

	logger.Infof("Fetching Prometheus endpoint URL")
	prometheusEndpoint, err := s.getPrometheusEndpointURL()
	if err != nil {
		logger.Errorf("Failed to get Prometheus endpoint URL: %v", err)
		return
	}

	logger.Infof("Creating NPD config with Prometheus endpoint %s", prometheusEndpoint)
	npdConfig, err := s.createNPDConfigWithPrometheusEndpoint(npdPrometheusCustomPluginMonitorConfigPath, prometheusEndpoint)
	if err != nil {
		logger.Errorf("Failed to create NPD config with Prometheus endpoint: %v", err)
		return
	}
	defer deleteNPDConfig(npdConfig)

	// Create NPD daemonset and required service account
	logger.Infof("Setting up Node Problem Detector (NPD)")
	setupNPD(npdServiceAccountPath, "", npdDaemonSetPath)
	defer tearDownNPD(npdServiceAccountPath, "", npdDaemonSetPath)

	// Check if NPD is running on all GPU nodes
	logger.Infof("Verify if Node Problem Detector (NPD) is running on all GPU nodes")
	s.verifyNPDRunning(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)

	//update npd config to to trigger error in Node condition
	logger.Infof("Edit Node Problem Detector (NPD) thresholds to simulate error condition")
	deleteNPDConfig(npdConfig)
	npdErrConfig, err := s.createNPDConfigWithPrometheusEndpoint(npdPrometheusCustomPluginMonitorErrorConfigPath, prometheusEndpoint)
	if err != nil {
		logger.Errorf("Failed to update NPD config:  Error %v", err)
		return
	}
	s.restartNPDPods(c)

	// Check if NPD has detected the error condition
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to true")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionTrue)

	// restore NPD config to original state
	logger.Infof("Restore Node Problem Detector (NPD) config to original state")
	deleteNPDConfig(npdErrConfig)
	_, err = s.createNPDConfigWithPrometheusEndpoint(npdPrometheusCustomPluginMonitorConfigPath, prometheusEndpoint)
	if err != nil {
		logger.Errorf("Failed to update NPD config:  Error %v", err)
		return
	}
	s.restartNPDPods(c)

	// Check node condition AMDGPUUnhealthy is set to false
	logger.Infof("Verify if Node condition AMDGPUUnhealthy is set to false")
	s.verifyNodeCondition(c, "AMDGPUUnhealthy", corev1.ConditionFalse)
}
