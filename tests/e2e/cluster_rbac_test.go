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
	"os"
	"time"

	"github.com/ROCm/gpu-operator/internal/metricsexporter"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/conditions"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func (s *E2ESuite) TestKubeRbacProxyClusterIP(c *C) {
	if !s.simEnable {
		skipTest(c, "Skipping for amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get("deviceconfig-kuberbac-clusterip", metav1.GetOptions{})
	assert.Errorf(c, err, "config deviceconfig-kuberbac-clusterip exists")

	logger.Info("create deviceconfig-kuberbac-clusterip")
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	devCfg := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-clusterip",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:  &enableExporter,
				SvcType: "ClusterIP",
				Port:    5000,
				Image:   exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)

	clusterIP, err := utils.GetClusterIP(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)
	assert.NoError(c, err, fmt.Sprintf("couldn't get cluster IP for metrics exporter service: %+v", err))

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err, fmt.Sprintf("failed to deploy resources from clusterrole_kuberbac.yaml: %+v", err))
	s.manageCurlJob(clusterIP, c)
	// delete
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, false)
	assert.NoError(c, err, fmt.Sprintf("failed to delete resources from clusterrole_kuberbac.yaml: %+v", err))
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestKubeRbacProxyNodePort(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get("deviceconfig-kuberbac-nodeport", metav1.GetOptions{})
	assert.Errorf(c, err, "config deviceconfig-kuberbac-nodeport exists")

	logger.Info("create deviceconfig-kuberbac-nodeport")
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	devCfg := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeNodePort, c)

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err, fmt.Sprintf("failed to deploy resources from clusterrole_kuberbac.yaml: %+v", err))

	// Run the token request repeatedly
	token := ""
	assert.Eventually(c, func() bool {
		token, err = utils.GenerateServiceAccountToken(s.clientSet, "default", "metrics-reader")
		if err != nil || len(token) == 0 {
			logger.Errorf("failed to generate token for default serviceaccount in metrics-client: %+v", err)
			return false
		}
		return true
	}, 1*time.Minute, 10*time.Second)
	assert.NoError(c, err, fmt.Sprintf("failed to generate token for default serviceaccount in metrics-client: %+v", err))

	nodeIPs, err := utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)
	assert.NoError(c, err, fmt.Sprintf("couldn't get node IPs for metrics exporter daemonset pods: %+v", err))

	// Test 1: Run the curl job repeatedly using nodeport
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), true, "", "", "")
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 3*time.Minute, 10*time.Second)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
	// Change th ports to give time for the old pods to be deleted and not affect the current test
	disableHttps := true
	devCfg.Spec.MetricsExporter.RbacConfig.DisableHttps = &disableHttps
	devCfg.Spec.MetricsExporter.Port = 6000
	devCfg.Spec.MetricsExporter.NodePort = 32000
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeNodePort, c)

	nodeIPs, err = utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)
	assert.NoError(c, err, fmt.Sprintf("couldn't get node IPs for metrics exporter daemonset pods: %+v", err))

	// Test 2: Run the curl job repeatedly using nodeport
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), false, "", "", "")
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 3*time.Minute, 10*time.Second)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, false)
	assert.NoError(c, err, fmt.Sprintf("failed to delete resources from clusterrole_kuberbac.yaml: %+v", err))
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestKubeRbacProxyNodePortCerts(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get("deviceconfig-kuberbac-nodeport", metav1.GetOptions{})
	assert.Errorf(c, err, "config deviceconfig-kuberbac-nodeport exists")

	// Create the cacert, cert and private key
	caCert, serverCert, serverKey, _, _, err := s.setupKubeRbacCerts(c, false)
	assert.NoErrorf(c, err, "failed to generate certs")

	secretName := "kube-tls-secret"
	err = utils.CreateTLSSecret(context.TODO(), s.clientSet, secretName, s.ns, serverCert, serverKey)
	assert.NoErrorf(c, err, fmt.Sprintf("failed to create secret %v", err))

	logger.Info("create deviceconfig-kuberbac-nodeport")
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	devCfg := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}
	devCfg.Spec.MetricsExporter.RbacConfig.Secret = &v1.LocalObjectReference{Name: secretName}
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeNodePort, c)

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err, fmt.Sprintf("failed to deploy resources from clusterrole_kuberbac.yaml: %+v", err))

	// Run the token request repeatedly
	token := ""
	assert.Eventually(c, func() bool {
		token, err = utils.GenerateServiceAccountToken(s.clientSet, "default", "metrics-reader")
		if err != nil || len(token) == 0 {
			logger.Errorf("failed to generate token for default serviceaccount in metrics-client: %+v", err)
			return false
		}
		return true
	}, 1*time.Minute, 10*time.Second)
	assert.NoError(c, err, fmt.Sprintf("failed to generate token for default serviceaccount in metrics-client: %+v", err))

	file, err := utils.CreateTempFile("cacert-*.crt", caCert)
	assert.NoError(c, err, fmt.Sprintf("failed to create cacert file: %v", err))

	// Get the nodeIPs of the nodes where the daemonset pods are deployed
	nodeIPs, err := utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)

	// Run the curl job repeatedly using nodeport
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), true, file.Name(), "", "")
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 2*time.Minute, 10*time.Second)
	err = utils.DeleteTempFile(file)
	assert.NoError(c, err, fmt.Sprintf("failed to delete cacert file: %v", err))

	// delete
	err = utils.DeleteTLSSecret(context.TODO(), s.clientSet, secretName, s.ns)
	assert.NoErrorf(c, err, fmt.Sprintf("failed to delete secret %v", err))
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, s.apiClientSet, false)
	assert.NoError(c, err, fmt.Sprintf("failed to delete resources from clusterrole_kuberbac.yaml: %+v", err))
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

// TestKubeRbacProxyNodePortMTLS exercises mTLS auth with User binding
func (s *E2ESuite) TestKubeRbacProxyNodePortMTLS(c *C) {
	// RBAC
	err := utils.DeployResourcesFromFile("clusterrole_mtls.yaml", s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeployResourcesFromFile("clusterrole_mtls.yaml", s.clientSet, s.apiClientSet, false); errDel != nil {
			logger.Errorf("failed to delete resources from clusterrole_mtls.yaml: %+v", errDel)
		}
	}()

	// Certs
	caCert, serverCert, serverKey, clientCert, clientKey, err := s.setupKubeRbacCerts(c, true)
	assert.NoError(c, err)

	// Secret
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
	err = utils.CreateConfigMap(context.TODO(), s.clientSet, s.ns, cmName, map[string]string{"ca.crt": string(caCert)})
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeleteConfigMap(context.TODO(), s.clientSet, cmName, s.ns); errDel != nil {
			logger.Errorf("failed to delete ConfigMap %s: %+v", cmName, errDel)
		}
	}()

	// Temp files
	caFile, err := utils.CreateTempFile("cacert-*.crt", caCert)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(caFile)
	certFile, err := utils.CreateTempFile("client-*.crt", clientCert)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(certFile)
	keyFile, err := utils.CreateTempFile("client-*.key", clientKey)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(keyFile)
	serverCertFile, err := utils.CreateTempFile("server-*.crt", serverCert)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(serverCertFile)
	serverKeyFile, err := utils.CreateTempFile("server-*.key", serverKey)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(serverKeyFile)

	// DeviceConfig
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	devCfg := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}
	devCfg.Spec.MetricsExporter.RbacConfig.Secret = &v1.LocalObjectReference{Name: secretName}
	devCfg.Spec.MetricsExporter.RbacConfig.ClientCAConfigMap = &v1.LocalObjectReference{Name: cmName}
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeNodePort, c)

	// Curl mTLS
	nodeIPs, err := utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)
	assert.NoError(c, err)
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, "", int(devCfg.Spec.MetricsExporter.NodePort), true, caFile.Name(), certFile.Name(), keyFile.Name())
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 2*time.Minute, 10*time.Second)
}

// TestKubeRbacProxyNodePortMTLSWithStaticAuth verifies static-auth mapping
func (s *E2ESuite) TestKubeRbacProxyNodePortMTLSWithStaticAuth(c *C) {
	// No RBAC setup necessary
	// Certs
	caPEM, srvPEM, srvKeyPEM, cliPEM, cliKeyPEM, err := s.setupKubeRbacCerts(c, true)
	assert.NoError(c, err)

	// Secret & CA
	secretName := "kube-tls-secret"
	err = utils.CreateTLSSecret(context.TODO(), s.clientSet, secretName, s.ns, srvPEM, srvKeyPEM)
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeleteTLSSecret(context.TODO(), s.clientSet, secretName, s.ns); errDel != nil {
			logger.Errorf("failed to delete TLS secret %s: %+v", secretName, errDel)
		}
	}()

	cmName := "client-ca-cm"
	err = utils.CreateConfigMap(context.TODO(), s.clientSet, s.ns, cmName, map[string]string{"ca.crt": string(caPEM)})
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeleteConfigMap(context.TODO(), s.clientSet, cmName, s.ns); errDel != nil {
			logger.Errorf("failed to delete ConfigMap %s: %+v", cmName, errDel)
		}
	}()

	// Files
	caFile, err := utils.CreateTempFile("cacert-*.crt", caPEM)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(caFile)
	certFile, err := utils.CreateTempFile("client-*.crt", cliPEM)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(certFile)
	keyFile, err := utils.CreateTempFile("client-*.key", cliKeyPEM)
	assert.NoError(c, err)
	defer func(file *os.File) {
		if file != nil {
			if errDel := utils.DeleteTempFile(file); errDel != nil {
				logger.Errorf("failed to delete temp file %s: %+v", file.Name(), errDel)
			}
		}
	}(keyFile)

	// DeviceConfig w/static-auth
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	devCfg := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}
	devCfg.Spec.MetricsExporter.RbacConfig.Secret = &v1.LocalObjectReference{Name: secretName}
	devCfg.Spec.MetricsExporter.RbacConfig.ClientCAConfigMap = &v1.LocalObjectReference{Name: cmName}
	devCfg.Spec.MetricsExporter.RbacConfig.StaticAuthorization = &v1alpha1.StaticAuthConfig{Enable: true, ClientName: "metrics-reader"}
	s.createDeviceConfig(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeNodePort, c)

	// Curl mTLS+static-auth
	nodeIPs, err := utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)
	assert.NoError(c, err)
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, "", int(devCfg.Spec.MetricsExporter.NodePort), true, caFile.Name(), certFile.Name(), keyFile.Name())
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 2*time.Minute, 10*time.Second)
}

// TestServiceMonitorCreation verifies ServiceMonitor CR creation and fields
func (s *E2ESuite) TestServiceMonitorCreation(c *C) {
	// Ensure CRD installed
	err := utils.DeployResourcesFromFile(serviceMonitorCRDURL, s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err)
	defer func() {
		if errDel := utils.DeployResourcesFromFile(serviceMonitorCRDURL, s.clientSet, s.apiClientSet, false); errDel != nil {
			logger.Errorf("failed to delete resources from %s: %+v", serviceMonitorCRDURL, errDel)
		}
	}()

	// Build DeviceConfig with ServiceMonitor enabled
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	dc := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
				Prometheus: &v1alpha1.PrometheusConfig{
					ServiceMonitor: &v1alpha1.ServiceMonitorConfig{
						Enable:   ptr.To(true),
						Interval: "30s",
						Labels:   map[string]string{"custom": "label"},
					},
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}

	// Create and wait
	s.createDeviceConfig(dc, c)

	smName := dc.Name + "-" + metricsexporter.ExporterName
	var sm *monitoringv1.ServiceMonitor
	assert.Eventually(c, func() bool {
		var getErr error
		sm, getErr = s.monClient.MonitoringV1().ServiceMonitors(s.ns).Get(context.TODO(), smName, metav1.GetOptions{})
		return getErr == nil
	}, 1*time.Minute, 5*time.Second)

	// Validate metadata labels
	c.Assert(sm.Labels["custom"], Equals, "label")
	c.Assert(sm.Labels["app"], Equals, "amd-device-metrics-exporter")

	// Validate selector matches underlying Service
	svc, err := s.clientSet.CoreV1().Services(s.ns).Get(context.TODO(), smName, metav1.GetOptions{})
	assert.NoError(c, err)
	for k, v := range sm.Spec.Selector.MatchLabels {
		c.Assert(svc.Labels[k], Equals, v)
	}

	// Validate scrape endpoint interval
	c.Assert(sm.Spec.Endpoints[0].Interval, Equals, monitoringv1.Duration("30s"))
}

// TestServiceMonitorCRDFlow tests failure if CRD missing and success after install
func (s *E2ESuite) TestServiceMonitorCRDFlow(c *C) {
	// Remove CRD if present
	err := utils.DeployResourcesFromFile(serviceMonitorCRDURL, s.clientSet, s.apiClientSet, false)
	assert.NoError(c, err)

	// Build DeviceConfig
	enableDriver := false
	enableExporter := true
	enableKubeRbacProxy := true
	disableHTTPs := false
	dc := &v1alpha1.DeviceConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "amd.com/v1alpha1",
			Kind:       "DeviceConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devcfg-kuberbac-nodeport",
			Namespace: "kube-amd-gpu",
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable: &enableDriver,
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage: devicePluginImage,
				NodeLabellerImage: nodeLabellerImage,
			},
			MetricsExporter: v1alpha1.MetricsExporterSpec{
				Enable:   &enableExporter,
				SvcType:  "NodePort",
				Port:     5000,
				NodePort: 31000,
				Image:    exporterMockImage,
				RbacConfig: v1alpha1.KubeRbacConfig{
					Enable:       &enableKubeRbacProxy,
					DisableHttps: &disableHTTPs,
				},
				Prometheus: &v1alpha1.PrometheusConfig{
					ServiceMonitor: &v1alpha1.ServiceMonitorConfig{Enable: ptr.To(true)},
				},
			},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
			Selector: map[string]string{
				"feature.node.kubernetes.io/amd-gpu": "true",
			},
		},
	}

	// Create and expect validation error
	s.createDeviceConfig(dc, c)
	assert.Eventually(c, func() bool {
		d2, getErr := s.dClient.DeviceConfigs(s.ns).Get(dc.Name, metav1.GetOptions{})
		if getErr != nil {
			return false
		}
		for _, cond := range d2.Status.Conditions {
			if cond.Type == conditions.ConditionTypeError &&
				cond.Status == metav1.ConditionTrue &&
				cond.Reason == conditions.ValidationError {
				return true
			}
		}
		return false
	}, 1*time.Minute, 5*time.Second)

	// Install CRD
	err = utils.DeployResourcesFromFile(serviceMonitorCRDURL, s.clientSet, s.apiClientSet, true)
	assert.NoError(c, err)
	defer func() {
		errDel := utils.DeployResourcesFromFile(serviceMonitorCRDURL, s.clientSet, s.apiClientSet, false)
		if errDel != nil {
			logger.Errorf("failed to delete resources from %s: %+v", serviceMonitorCRDURL, errDel)
		}
	}()

	// Re-create DeviceConfig
	s.deleteDeviceConfig(dc, c)
	s.createDeviceConfig(dc, c)

	// Now ServiceMonitor should be created
	smName := dc.Name + "-" + metricsexporter.ExporterName
	assert.Eventually(c, func() bool {
		_, getErr := s.monClient.MonitoringV1().ServiceMonitors(s.ns).Get(context.TODO(), smName, metav1.GetOptions{})
		return getErr == nil
	}, 1*time.Minute, 5*time.Second)
}
