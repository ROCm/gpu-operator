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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (s *E2ESuite) verifyNoMetricsExporter(devCfg *v1alpha1.DeviceConfig, c *C) {
	ns := devCfg.Namespace
	assert.Eventually(c, func() bool {
		if _, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), devCfg.Name+"-"+metricsexporter.ExporterName,
			metav1.GetOptions{}); err == nil {
			logger.Warnf("metrics exporter exists: %+v %v", devCfg, err)
			return false
		}

		if _, err := s.clientSet.CoreV1().Services(ns).Get(context.TODO(),
			devCfg.Name+"-"+metricsexporter.ExporterName, metav1.GetOptions{}); err == nil {
			logger.Warnf("metrics service exists")
			return false
		}

		return true
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyNodePortMetrics(c *C, devCfg *v1alpha1.DeviceConfig, fields []string, labels []string) {
	nodes, _ := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kmmmodule.MapToLabelSelector(devCfg.Spec.Selector),
	})

	if fields == nil {
		fields = []string{"gpu_package_power", "gpu_memory_temperature", "gpu_power_usage", "gpu_clock"}
	}

	if labels == nil {
		labels = []string{"card_model"}
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				assert.Eventually(c, func() bool {
					resp, err := http.Get(fmt.Sprintf("http://%v:32500/metrics", addr.Address))
					if err != nil {
						logger.Warnf("failed to get metrics from %v:32500/metrics, %v", addr.Address, err)
						return false
					}
					defer resp.Body.Close()
					p := expfmt.TextParser{}
					m, err := p.TextToMetricFamilies(resp.Body)
					assert.NoError(c, err, "failed to parse metrics data")
					for _, f := range fields {
						k, ok := m[f]
						if !ok || k == nil {
							return false
						}
						metricsLabels := map[string]string{}
						for _, km := range k.Metric {
							for _, lp := range km.GetLabel() {
								metricsLabels[*lp.Name] = *lp.Value
							}
						}
						logger.Infof("found field %v labels %v", f, metricsLabels)
						for _, l := range labels {
							_, ok := metricsLabels[l]
							assert.True(c, ok, fmt.Sprintf("missing label %v", l))
						}
					}
					return true
				}, time.Minute, 5*time.Second)
			}
		}
	}
}

func (s *E2ESuite) verifyClusterIPMetrics(c *C, devCfg *v1alpha1.DeviceConfig, fields []string, labels []string) {
	pods, err := s.clientSet.CoreV1().Pods(devCfg.Namespace).List(context.TODO(),
		metav1.ListOptions{LabelSelector: kmmmodule.MapToLabelSelector(
			map[string]string{"app.kubernetes.io/name": metricsexporter.ExporterName})})
	assert.NoError(c, err, "failed to list pods")

	if fields == nil {
		fields = []string{"gpu_package_power", "gpu_memory_temperature", "gpu_power_usage", "gpu_clock"}
	}

	if labels == nil {
		labels = []string{"card_model"}
	}

	for _, pod := range pods.Items {
		assert.Eventually(c, func() bool {
			cmd := fmt.Sprintf("curl -sS 127.0.0.1:%v/metrics",
				devCfg.Spec.MetricsExporter.Port)
			out, err := utils.ExecPodCmd(cmd, devCfg.Namespace, pod.Name, metricsexporter.ExporterName+"-container")
			if err != nil {
				logger.Warnf("%v err:%v", cmd, err)
				return false
			}
			logger.Infof("metrics %v", out)

			p := expfmt.TextParser{}
			m, err := p.TextToMetricFamilies(strings.NewReader(out))
			assert.NoError(c, err, "failed to parse metrics data")
			for _, f := range fields {
				k, ok := m[f]
				if !ok || k == nil {
					return false
				}
				metricsLabels := map[string]string{}
				for _, km := range k.Metric {
					for _, lp := range km.GetLabel() {
						metricsLabels[*lp.Name] = *lp.Value
					}
				}
				logger.Infof("found field %v labels %v", f, metricsLabels)
				for _, l := range labels {
					_, ok := metricsLabels[l]
					assert.True(c, ok, fmt.Sprintf("missing label %v", l))
				}
			}
			return true
		}, time.Minute, 5*time.Second)
	}
}

func (s *E2ESuite) TestExporterDeployment(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	exporterEnable := false
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.MetricsExporter.Enable = &exporterEnable
	logger.Infof("create device-config %+v", devCfg.Spec.MetricsExporter)
	s.createDeviceConfig(devCfg, c)
	s.verifyNoMetricsExporter(devCfg, c)

	// enable Node port
	updConfig, err := s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")

	exporterEnable = true
	updConfig.Spec.MetricsExporter.Enable = &exporterEnable
	updConfig.Spec.MetricsExporter.NodePort = 32500
	updConfig.Spec.MetricsExporter.Port = 5000
	updConfig.Spec.MetricsExporter.SvcType = v1alpha1.ServiceTypeNodePort
	//updConfig.Spec.MetricsExporter.Image = "10.11.18.9:5000/amd/exporter:v1" //todo: use default image
	logger.Infof("update exporter-config %+v", updConfig.Spec.MetricsExporter)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeNodePort, c)
	s.verifyNodePortMetrics(c, devCfg, []string{}, []string{})

	// add configmap with custom labels/fields, verify metrics
	cmFields := []string{"gpu_package_power", "gpu_edge_temperature"}
	cmLabels := []string{"gpu_id", "pod", "container"}
	cfgData, err := json.Marshal(struct {
		GPUConfig struct {
			Fields []string
			Labels []string
		}
	}{
		GPUConfig: struct {
			Fields []string
			Labels []string
		}{
			Fields: cmFields,
			Labels: cmLabels,
		},
	})
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

	// enable Node port
	updConfig, err = s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")
	updConfig.Spec.MetricsExporter.Config = v1alpha1.MetricsConfig{Name: devCfg.Name}

	logger.Infof("update exporter-config %+v", updConfig.Spec.MetricsExporter)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeNodePort, c)
	s.verifyNodePortMetrics(c, devCfg, cmFields, cmLabels)

	// change service type, verify metrics
	updConfig, err = s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")
	updConfig.Spec.MetricsExporter.Port = 5000
	updConfig.Spec.MetricsExporter.SvcType = v1alpha1.ServiceTypeClusterIP
	logger.Infof("update exporter-config %+v", updConfig.Spec.MetricsExporter)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeClusterIP, c)
	s.verifyClusterIPMetrics(c, updConfig, cmFields, cmLabels)

	// change port, verify metrics
	updConfig, err = s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")
	updConfig.Spec.MetricsExporter.Port = 6000
	updConfig.Spec.MetricsExporter.SvcType = v1alpha1.ServiceTypeClusterIP
	logger.Infof("update exporter-config %+v", updConfig.Spec.MetricsExporter)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeClusterIP, c)
	s.verifyClusterIPMetrics(c, updConfig, cmFields, cmLabels)

	// include selector, verify pods
	updConfig, err = s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")

	nodes, _ := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kmmmodule.MapToLabelSelector(devCfg.Spec.Selector),
	})
	assert.True(c, len(nodes.Items) > 0, "no nodes with gpu", len(nodes.Items))
	logger.Infof("selecting node %v for exporter pod", nodes.Items[0].Name)
	updConfig.Spec.MetricsExporter.Selector = map[string]string{"kubernetes.io/hostname": nodes.Items[0].Name}
	logger.Infof("update exporter-config %+v", updConfig.Spec.MetricsExporter)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeClusterIP, c)
	pods, _ := s.clientSet.CoreV1().Pods(devCfg.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: kmmmodule.MapToLabelSelector(map[string]string{"app.kubernetes.io/name": metricsexporter.ExporterName}),
	})
	assert.True(c, len(pods.Items) == 1, "> 1 pods", len(pods.Items))
	assert.True(c, pods.Items[0].Spec.NodeName == nodes.Items[0].Name, fmt.Sprintf("mismatch in scheduled node got [%v], want [%v]",
		pods.Items[0].Spec.NodeName, nodes.Items[0].Name))
}

func (s *E2ESuite) TestHealthCheckFeature(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	exporterEnable := true
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("expected no config to be present. but config %v exists", s.cfgName))

	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.MetricsExporter.Image = exporterImage
	devCfg.Spec.MetricsExporter.Enable = &exporterEnable
	devCfg.Spec.MetricsExporter.NodePort = 32500
	devCfg.Spec.MetricsExporter.Port = 5000
	devCfg.Spec.MetricsExporter.SvcType = v1alpha1.ServiceTypeNodePort
	devCfg.Spec.DevicePlugin = v1alpha1.DevicePluginSpec{
		DevicePluginImage:           devicePluginImage,
		DevicePluginImagePullPolicy: "Always",
	}
	logger.Infof("create device-config %+v", devCfg)
	s.createDeviceConfig(devCfg, c)

	s.checkMetricsExporterStatus(devCfg, s.ns, corev1.ServiceTypeNodePort, c)
	s.verifyNodePortMetrics(c, devCfg, []string{}, []string{})

	// add configmap with custom labels/fields, verify metrics
	cmFields := []string{"gpu_package_power", "gpu_edge_temperature", "gpu_health"}
	cmLabels := []string{"gpu_id", "pod", "container"}
	cfgData, err := json.Marshal(struct {
		GPUConfig struct {
			Fields []string
			Labels []string
		}
	}{
		GPUConfig: struct {
			Fields []string
			Labels []string
		}{
			Fields: cmFields,
			Labels: cmLabels,
		},
	})
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

	// enable Node port
	updConfig, err := s.dClient.DeviceConfigs(devCfg.Namespace).Get(devCfg.Name, metav1.GetOptions{})
	assert.NoError(c, err, "failed to read deviceconfig")
	updConfig.Spec.MetricsExporter.Config = v1alpha1.MetricsConfig{Name: devCfg.Name}

	logger.Infof("update exporter-config %+v", updConfig)
	_, err = s.dClient.DeviceConfigs(s.ns).Update(updConfig)
	assert.NoError(c, err, "failed to update %v", updConfig.Name)
	logger.Infof("Verifying exporter status and metrics")
	s.checkMetricsExporterStatus(updConfig, s.ns, corev1.ServiceTypeNodePort, c)
	s.verifyNodePortMetrics(c, devCfg, cmFields, cmLabels)
	s.verifyNodeGPULabel(devCfg, c)

	labelMap := make(map[string]string)
	labelMap["metricsexporter.amd.com.gpu.0.state"] = "healthy"
	logger.Print("Verify healthy label on node(s)")
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labelMap).String(),
		})
		if err != nil || len(nodes.Items) == 0 {
			return false
		}
		logger.Printf("Got %d nodes with healthy label", len(nodes.Items))
		return true
	}, 2*time.Minute, 10*time.Second, "expected gpu 0 to be healthy but got unhealthy")

	logger.Infof("Marking GPU unhealthy")
	err = utils.SetGPUHealthOnNode(s.clientSet, devCfg.Namespace, "0", "unhealthy", "")
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
	}, 2*time.Minute, 10*time.Second, "expected gpu 0 to become unhealthy but got healthy")

	logger.Infof("Creating ROCM Pod on node with Unhealthy GPU. Expect it to be in Pending state")
	var rocmLabel = map[string]string{
		"e2e": "true",
	}
	err = utils.CreateDaemonset(s.clientSet, "default", "e2e-rocm", initContainerImage, rocmLabel, nil)
	assert.NoError(c, err, "failed to create ROCM pod")
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(rocmLabel).String(),
		})
		if err != nil {
			logger.Printf("Error occured when trying to get the pod. Error: %v", err)
			return false
		}
		if pods == nil || len(pods.Items) == 0 {
			logger.Print("No ROCM Pods found")
			return false
		}
		if pods.Items[0].Status.Phase == "Pending" {
			return true
		}
		return false
	}, 2*time.Minute, 10*time.Second, "Expected ROCM Pod to be in Pending State")
	logger.Print("Verified ROCM Pod is in pending state")

	logger.Print("Clear GPU error and verify ROCM Pod goes to Running state")
	logger.Infof("Marking GPU healthy")
	err = utils.SetGPUHealthOnNode(s.clientSet, devCfg.Namespace, "0", "healthy", "")
	assert.NoError(c, err, fmt.Sprintf("failed to mark GPU 0 healthy. Error:%v", err))
	labelMap["metricsexporter.amd.com.gpu.0.state"] = "healthy"
	logger.Print("Verifying healthy label on the node(s)")
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labelMap).String(),
		})
		if err != nil || len(nodes.Items) == 0 {
			return false
		}
		logger.Printf("Got %d nodes with healthy label", len(nodes.Items))
		return true
	}, 90*time.Second, 10*time.Second, "expected gpu 0 to become healthy but got unhealthy")
	logger.Print("Verifying ROCM Pod moved to Running state")
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(rocmLabel).String(),
		})
		if err != nil {
			logger.Printf("Error occured when trying to get the pod. Error: %v", err)
			return false
		}
		if pods == nil || len(pods.Items) == 0 {
			logger.Print("No ROCM Pods found")
			return false
		}
		if pods.Items[0].Status.Phase == "Running" {
			return true
		}
		return false
	}, 2*time.Minute, 10*time.Second, "Expected ROCM Pod to be in Running State")
	logger.Print("Verified ROCM Pod is in Running state")

	logger.Infof("Marking GPU unhealthy")
	err = utils.SetGPUHealthOnNode(s.clientSet, devCfg.Namespace, "0", "unhealthy", "")
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
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(rocmLabel).String(),
		})
		if err != nil {
			logger.Printf("Error occured when trying to get the pod. Error: %v", err)
			return false
		}
		if pods == nil || len(pods.Items) == 0 {
			logger.Print("No ROCM Pods found")
			return false
		}
		if pods.Items[0].Status.Phase == "Running" {
			return true
		}
		return false
	}, 2*time.Minute, 10*time.Second, "Expected ROCM Pod to be in Running State")
	logger.Print("Verified ROCM Pod is in Running state")
}
