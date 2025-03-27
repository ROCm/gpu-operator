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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/ROCm/gpu-operator/internal/configmanager"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
)

func (s *E2ESuite) getDeviceConfigForDCM(c *C) *v1alpha1.DeviceConfig {
	dcmenable := true
	nodelabelenable := false
	driverEnable := false
	devCfg := &v1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.cfgName,
			Namespace: s.ns,
		},
		Spec: v1alpha1.DeviceConfigSpec{
			Driver: v1alpha1.DriverSpec{
				Enable:  &driverEnable,
				Image:   driverImageRepo,
				Version: s.defaultDriverVersion,
			},
			// SkipDrivers:    true,
			ConfigManager: v1alpha1.ConfigManagerSpec{
				Enable:          &dcmenable,
				Image:           dcmImage,
				ImagePullPolicy: "Always",
				ConfigManagerTolerations: []v1.Toleration{
					{
						Key:      "dcm",
						Operator: v1.TolerationOpEqual,
						Value:    "up",
						Effect:   v1.TaintEffectNoExecute,
					},
				},
			},
			DevicePlugin: v1alpha1.DevicePluginSpec{
				DevicePluginImage:  devicePluginImage,
				EnableNodeLabeller: &nodelabelenable,
			},
			Selector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
		},
	}
	return devCfg
}

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
				Port:     5001,
			},
			Selector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
		},
	}
	insecure := true
	devCfg.Spec.Driver.ImageRegistryTLS.Insecure = &insecure
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage
	if s.simEnable {
		devCfg.Spec.MetricsExporter.Image = exporterImage
	}
	if s.openshift {
		devCfg.Spec.Driver.Version = "6.1.1"
	}
	return devCfg
}

func (s *E2ESuite) createDeviceConfig(devCfg *v1alpha1.DeviceConfig, c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Create(devCfg)
	assert.NoError(c, err, "failed to create %v", s.cfgName)
}

func (s *E2ESuite) manageCurlJob(clusterIP string, c *C) {
	backoffLimit := int32(4)
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "amd-curl",
			Namespace: "metrics-reader",
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "amd-curl",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    "amd-curl",
							Image:   kubeRbacProxyCurlImage,
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`curl -v -s -k -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" https://%v:5000/metrics`, clusterIP),
							},
						},
					},
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}
	assert.Eventually(c, func() bool {
		if !s.deployCurlJob(job) {
			return false
		}
		defer s.deleteJob(job)

		for i := 0; i < 20; i++ {
			if s.checkJobStatus(job) {
				return true
			}
			time.Sleep(1 * time.Second)
		}

		return false
	}, 5*time.Minute, 25*time.Second)
}

func (s *E2ESuite) deployCurlJob(job *batchv1.Job) bool {
	_, err := s.clientSet.BatchV1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		logger.Errorf("failed to create job: %+v", err)
		return false
	}
	return true
}

func (s *E2ESuite) deleteJob(job *batchv1.Job) bool {
	propagationPolicy := metav1.DeletePropagationBackground

	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	}
	err := s.clientSet.BatchV1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, deleteOptions)
	if err != nil {
		logger.Errorf("failed to delete job: %+v", err)
		return false
	}
	return true
}

func (s *E2ESuite) checkJobStatus(job *batchv1.Job) bool {
	job, err := s.clientSet.BatchV1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("failed to get job %s: %v", job.Name, err)
		return false
	}

	if job.Status.Succeeded > 0 {
		jobLogs, err := utils.GetJobLogs(s.clientSet, job)
		if err != nil {
			logger.Errorf("failed to get job logs: %v", err)
			return false
		}
		for _, jlog := range jobLogs {
			if !strings.Contains(jlog, "gpu_id") {
				logger.Errorf("failed to fetch metrics, log: %s", jlog)
				return false
			}
		}
		logger.Infof("JobLogs: %+v", jobLogs)
		return true
	}

	return false
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

func (s *E2ESuite) checkKMMOperatorStatus(ns string, c *C, operatorName string) {
	if operatorName == "" {
		operatorName = "amd-gpu-operator-kmm-controller"
	}

	assert.Eventually(c, func() bool {
		deployment, err := s.clientSet.AppsV1().Deployments(ns).Get(context.TODO(), operatorName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get KMM operator deployment: %v", err)
			return false
		}

		logger.Infof("KMM operator deployment status: %+v", deployment.Status)
		return deployment.Status.Replicas > 0 &&
			deployment.Status.Replicas == deployment.Status.AvailableReplicas &&
			deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
			deployment.Status.Replicas == deployment.Status.ReadyReplicas
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyDevicePluginStatus(ns string, c *C, devCfg *v1alpha1.DeviceConfig) {
	assert.Eventually(c, func() bool {
		pods, err := s.clientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			logger.Errorf("  failed to get device-plugin %v", err)
			return false
		}
		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, utils.DevicePluginName(devCfg.Name)) {
				return true
			}
		}
		logger.Infof(" Device Plugin Not found for deviceconfig %v", devCfg.Name)
		return false
	}, 25*time.Minute, 5*time.Second)
}

func (s *E2ESuite) checkNodeLabellerStatus(ns string, c *C, devCfg *v1alpha1.DeviceConfig) {
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), utils.NodeLabellerName(devCfg.Name), metav1.GetOptions{})
		if err != nil {
			logger.Errorf("  failed to get node-labeller %v", err)
			return false
		}

		logger.Infof(" node-labeller: %s status %+v", ds.Name, ds.Status)
		return ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}, 45*time.Minute, 5*time.Second)
}

func (s *E2ESuite) checkMetricsExporterStatus(devCfg *v1alpha1.DeviceConfig, ns string, serviceType v1.ServiceType, c *C) {
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), devCfg.Name+"-"+metricsexporter.ExporterName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get metrics exporter devCfg: %+v %v", devCfg, err)
			return false
		}
		logger.Infof("metrics exporter %+v", ds.Status)
		svc, err := s.clientSet.CoreV1().Services(ns).Get(context.TODO(), devCfg.Name+"-"+metricsexporter.ExporterName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get metrics service %v", err)
			return false
		}
		logger.Infof("metrics service %+v", svc.Spec)

		ready := ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
			len(svc.Spec.Ports) > 0 && svc.Spec.Ports[0].TargetPort == intstr.FromInt32(devCfg.Spec.MetricsExporter.Port)
		if serviceType == v1.ServiceTypeNodePort {
			ready = ready && svc.Spec.Type == v1.ServiceTypeNodePort && svc.Spec.Ports[0].NodePort == devCfg.Spec.MetricsExporter.NodePort
		} else {
			ready = ready && svc.Spec.Type == v1.ServiceTypeClusterIP
		}

		return ready
	}, 45*time.Minute, 5*time.Second)
}

func (s *E2ESuite) checkDeviceConfigManagerStatus(devCfg *v1alpha1.DeviceConfig, ns string, c *C) {
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), devCfg.Name+"-"+configmanager.ConfigManagerName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get config manager devCfg: %+v %v NAME %v", devCfg, err, devCfg.Name+"-"+configmanager.ConfigManagerName)
			return false
		}
		logger.Infof("config manager %+v", ds.Status)

		ready := ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
		return ready
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) patchMetricsExporterEnablement(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchMetricsExporterEnablement(devCfg)
	assert.NoError(c, err, "failed to update %v", s.cfgName)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchTestRunnerEnablement(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchTestRunnerEnablement(devCfg)
	assert.NoError(c, err, "failed to update %v", s.cfgName)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchTestRunnerConfigmap(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchTestRunnerConfigmap(devCfg)
	assert.NoError(c, err, "failed to update %v", s.cfgName)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchDriversVersion(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchDriversVersion(devCfg)
	assert.NoError(c, err, "failed to update %v", s.cfgName)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchDevicePluginImage(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchDevicePluginImage(devCfg)
	assert.NoError(c, err, "failed to update %v", devCfg.Name)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchNodeLabellerImage(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchNodeLabellerImage(devCfg)
	assert.NoError(c, err, "failed to update %v", devCfg.Name)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) patchMetricsExporterImage(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchMetricsExporterImage(devCfg)
	assert.NoError(c, err, "failed to update %v", devCfg.Name)
	logger.Info(fmt.Sprintf("updated device config %+v", result))
}

func (s *E2ESuite) isUpgradeInProgress(devCfg *v1alpha1.DeviceConfig) bool {
	// Define the blocked states that indicate an upgrade is in progress
	blockedStates := map[v1alpha1.UpgradeState]bool{
		v1alpha1.UpgradeStateNotStarted:        true,
		v1alpha1.UpgradeStateStarted:           true,
		v1alpha1.UpgradeStateInstallInProgress: true,
		v1alpha1.UpgradeStateInProgress:        true,
	}

	// Iterate over NodeModuleStatus in DeviceConfigStatus
	for nodeName, moduleStatus := range devCfg.Status.NodeModuleStatus {
		if blockedStates[moduleStatus.Status] {
			logger.Infof("Upgrade in progress for node %s with state %s", nodeName, moduleStatus.Status)
			return true
		}
	}

	logger.Infof("No nodes in blocked states. Upgrade not in progress.")
	return false
}

func (s *E2ESuite) verifyDeviceConfigStatus(devCfg *v1alpha1.DeviceConfig, c *C) {
	assert.Eventually(c, func() bool {
		devCfg, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
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
	}, 45*time.Minute, 5*time.Second)
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

func (s *E2ESuite) deleteDeviceConfig(devCfg *v1alpha1.DeviceConfig, c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Delete(devCfg.Name)
	assert.NoErrorf(c, err, "failed to delete %v", devCfg.Name)

	assert.Eventually(c, func() bool {
		_, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(context.TODO(), devCfg.Name+"-node-labeller", metav1.GetOptions{})
		if err == nil {
			logger.Warnf("waiting to delete node-labeller ")
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second)

	assert.Eventually(c, func() bool {
		_, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
		if err == nil {
			logger.Warnf("waiting to delete deviceConfig")
			return false
		}
		return true
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) TestBasicSkipDriverInstall(c *C) {
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	logger.Infof("create %v", s.cfgName)
	s.createDeviceConfig(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
}

func (s *E2ESuite) TestDeployment(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, c)
	}

	if !s.simEnable {
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

// TestDriverUpgradeByUpdatingCR
// test the driver upgrade by directly updating CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded

func (s *E2ESuite) TestDriverUpgradeByUpdatingCR(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, c)
	}
	s.verifyNodeDriverVersionLabel(devCfg, c)
	if !s.simEnable {
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
	}

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.2.2"
	s.patchDriversVersion(devCfg, c)
	// update the node resources version labels
	s.updateNodeDriverVersionLabel(devCfg, c)
	if !s.simEnable {
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

// TestDriverUpgradeByPushingNewCR
// test the driver upgrade by pushing new CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded
func (s *E2ESuite) TestDriverUpgradeByPushingNewCR(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, c)
		s.verifyNodeDriverVersionLabel(devCfg, c)
	}

	if !s.simEnable {
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		s.deleteDeviceConfig(devCfg, c)
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		s.deleteDeviceConfig(devCfg, c)
	}
	// upgrade by pushing new CR with new version
	devCfg.Spec.Driver.Version = "6.2.2"
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	if !s.simEnable {
		s.verifyNodeGPULabel(devCfg, c)
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
		s.deleteDeviceConfig(devCfg, c)
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		s.deleteDeviceConfig(devCfg, c)
	}
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
	if s.simEnable {
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
	s.checkNodeLabellerStatus("kube-amd-gpu", c, devCfg)

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

	ctx := context.TODO()
	noamdWorkerList := utils.GetNonAMDGpuWorker(s.clientSet)
	noamdNodeMap := make(map[string]v1.Node)
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

	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.Selector = map[string]string{
		"kubernetes.io/hostname": noamdNodeNames[0],
	}

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

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
	}, 25*time.Minute, 5*time.Second)

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
}

func (s *E2ESuite) TestEnableBlacklist(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	logger.Infof("TestEnableBlacklist")

	devCfg := s.getDeviceConfig(c)
	blacklist := true
	devCfg.Spec.Driver.Blacklist = &blacklist

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
}

func (s *E2ESuite) TestWorkloadRequestedGPUs(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}

	ctx := context.TODO()
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "gpu")
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
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "gpu")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *E2ESuite) TestWorkloadRequestedGPUsHomogeneousSingle(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile2")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogenous default partitioning")
	} else {
		logger.Errorf("Failure test homogenous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)

	time.Sleep(60 * time.Second)

	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "gpu")
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
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "gpu")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *E2ESuite) TestWorkloadRequestedGPUsHomogeneousMixed(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile2")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogeneous partitioning")
	} else {
		logger.Errorf("Failure test homogeneous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)
	time.Sleep(60 * time.Second)
	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{"resource_naming_strategy": "mixed"}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "cpx_nps4")
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
			"amd.com/cpx_nps4": resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			"amd.com/cpx_nps4": resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "cpx_nps4")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")

}

func (s *E2ESuite) TestWorkloadRequestedGPUsHeterogeneousMixed(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	if !dcmImageDefined {
		c.Skip("skip DCM test because E2E_DCM_IMAGE is not defined")
	}

	s.configMapHelper(c)

	logger.Infof("Add node label after pod comes up")
	time.Sleep(30 * time.Second)

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	for _, nodeName := range nodeNames {
		s.addRemoveNodeLabels(nodeName, "e2e_profile1")
	}

	logs := s.getLogs()
	if strings.Contains(logs, "Partition completed successfully") && (!strings.Contains(logs, "ERROR")) && (s.eventHelper("SuccessfullyPartitioned", "Normal")) {
		logger.Infof("Successfully tested homogeneous partitioning")
	} else {
		logger.Errorf("Failure test heterogenous partitioning")
	}
	devCfgDcm := s.getDeviceConfigForDCM(c)
	s.deleteDeviceConfig(devCfgDcm, c)
	time.Sleep(60 * time.Second)

	ctx := context.TODO()
	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	driverEnable := false
	devCfg.Spec.Driver.Enable = &driverEnable
	devCfg.Spec.DevicePlugin.DevicePluginArguments = map[string]string{"resource_naming_strategy": "mixed"}
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	ret, err := utils.GetAMDGPUCount(ctx, s.clientSet, "cpx_nps1")
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
			"amd.com/cpx_nps1": resource.MustParse(fmt.Sprintf("%d", gpuLimitCount)),
		},
		Requests: v1.ResourceList{
			"amd.com/cpx_nps1": resource.MustParse(fmt.Sprintf("%d", gpuReqCount)),
		},
	}

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, res)
	assert.NoError(c, err, "failed to deploy pods")
	err = utils.VerifyROCMPODResourceCount(ctx, s.clientSet, gpuReqCount, "cpx_nps1")
	assert.NoError(c, err, fmt.Sprintf("%v", err))

	// delete
	s.deleteDeviceConfig(devCfg, c)

	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
}

func (s *E2ESuite) TestKubeRbacProxyClusterIP(c *C) {
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
				Image:   exporterImage,
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

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, true)
	assert.NoError(c, err, fmt.Sprintf("failed to deploy resources from clusterrole_kuberbac.yaml: %+v", err))
	s.manageCurlJob(clusterIP, c)
	// delete
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, false)
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
				Image:    exporterImage,
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

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, true)
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
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), true, "")
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
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), false, "")
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 3*time.Minute, 10*time.Second)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, false)
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
	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoErrorf(c, err, "failed to create caPrivateKey")

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"My CA"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:         true,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivateKey.PublicKey, caPrivateKey)
	assert.NoErrorf(c, err, "failed to create caCert")

	tlsPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoErrorf(c, err, "failed to create tlsPrivateKey")
	tlsTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: []string{"My TLS"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{},
	}

	nodeIPs, err := utils.GetNodeIPs(s.clientSet)
	assert.NoErrorf(c, err, "failed to get nodeIPs")
	for _, ip := range nodeIPs {
		tlsTemplate.IPAddresses = append(tlsTemplate.IPAddresses, net.ParseIP(ip))
	}

	tlsCertDER, err := x509.CreateCertificate(rand.Reader, &tlsTemplate, &caTemplate, &tlsPrivateKey.PublicKey, caPrivateKey)
	assert.NoErrorf(c, err, "failed to create tlsCert")
	combinedCertPEM := &bytes.Buffer{}
	caCertPEM := &bytes.Buffer{}
	err = pem.Encode(combinedCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: tlsCertDER})
	assert.NoErrorf(c, err, "failed to encode certificate %v", err)
	err = pem.Encode(combinedCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	assert.NoErrorf(c, err, "failed to encode ca certificate %v", err)
	err = pem.Encode(caCertPEM, &pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	assert.NoErrorf(c, err, "failed to encode ca certificate %v", err)
	tlsKeyPEM := &bytes.Buffer{}
	pkcs8PrivateKey, err := x509.MarshalPKCS8PrivateKey(tlsPrivateKey)
	assert.NoErrorf(c, err, "failed to encode private key in PKCS#8 format")
	err = pem.Encode(tlsKeyPEM, &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8PrivateKey})
	assert.NoErrorf(c, err, "failed to encode private key %v", err)

	secretName := "kube-tls-secret"
	err = utils.CreateTLSSecret(context.TODO(), s.clientSet, secretName, s.ns, combinedCertPEM.Bytes(), tlsKeyPEM.Bytes())
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
				Image:    exporterImage,
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

	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, true)
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

	file, err := utils.CreateTempFile("cacert-*.crt", caCertPEM.Bytes())
	assert.NoError(c, err, fmt.Sprintf("failed to create cacert file: %v", err))

	// Get the nodeIPs of the nodes where the daemonset pods are deployed
	nodeIPs, err = utils.GetNodeIPsForDaemonSet(s.clientSet, devCfg.Name+"-"+metricsexporter.ExporterName, s.ns)

	// Run the curl job repeatedly using nodeport
	assert.Eventually(c, func() bool {
		err = utils.CurlMetrics(nodeIPs, token, int(devCfg.Spec.MetricsExporter.NodePort), true, file.Name())
		if err != nil {
			logger.Errorf("Error: %v", err.Error())
			return false
		}

		return true
	}, 3*time.Minute, 10*time.Second)
	err = utils.DeleteTempFile(file)
	assert.NoError(c, err, fmt.Sprintf("failed to delete cacert file: %v", err))

	// delete
	err = utils.DeleteTLSSecret(context.TODO(), s.clientSet, secretName, s.ns)
	assert.NoErrorf(c, err, fmt.Sprintf("failed to delete secret %v", err))
	s.deleteDeviceConfig(devCfg, c)
	err = utils.DeployResourcesFromFile("clusterrole_kuberbac.yaml", s.clientSet, false)
	assert.NoError(c, err, fmt.Sprintf("failed to delete resources from clusterrole_kuberbac.yaml: %+v", err))
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestDeployDefaultDriver(c *C) {
	if s.simEnable {
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
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)

	err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
	assert.NoError(c, err, "failed to deploy pods")
	s.verifyROCMPOD(true, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)

	s.verifyROCMPOD(false, c)
	err = utils.DelRocmPods(context.TODO(), s.clientSet)
	assert.NoError(c, err, "failed to remove rocm pods")
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
	assert.NoError(c, err, "failed to reboot nodes")
}

func (s *E2ESuite) TestDifferentCRsForDifferentNodes(c *C) {
	var nodes []v1.Node
	if s.simEnable {
		nodes = utils.GetNonAMDGpuWorker(s.clientSet)
	} else {
		nodes = utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	}

	ctx := context.TODO()
	nodeNames := make([]string, 0)
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	// Deploying Different CR's for worker nodes using unique node selector with different Image Versions
	driverVersions := []string{"6.1.3", "6.2.2"}
	devCfgs := []*v1alpha1.DeviceConfig{}
	for i, nodeName := range nodeNames {
		cfgName := nodeName
		_, err := s.dClient.DeviceConfigs(s.ns).Get(cfgName, metav1.GetOptions{})
		assert.Errorf(c, err, fmt.Sprintf("config %v exists", cfgName))

		logger.Infof("create %v", cfgName)
		devCfg := s.getDeviceConfig(c)
		devCfg.Name = cfgName
		devCfg.Spec.Selector = map[string]string{
			"kubernetes.io/hostname": nodeName,
		}
		devCfg.Spec.Driver.Version = driverVersions[i%len(driverVersions)]
		s.createDeviceConfig(devCfg, c)
		devCfgs = append(devCfgs, devCfg)
		s.checkNFDWorkerStatus(s.ns, c, "")
		s.verifyDeviceConfigStatus(devCfg, c)
		s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
		s.verifyDevicePluginStatus(s.ns, c, devCfg)
		s.checkNodeLabellerStatus(s.ns, c, devCfg)
	}

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		// Deploying rocm pods for worker nodes
		err := utils.DeployRocmPodsByNodeNames(ctx, s.clientSet, nodeNames)
		assert.NoError(c, err, "failed to deploy rocm pods")
		s.verifyROCMPOD(true, c)

		for _, devCfg := range devCfgs {
			s.deleteDeviceConfig(devCfg, c)
		}

		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPodsByNodeNames(ctx, s.clientSet, nodeNames)
		assert.NoError(c, err, "failed to remove rocm pods")

		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	} else {
		for _, devCfg := range devCfgs {
			s.deleteDeviceConfig(devCfg, c)
		}
	}
}

func (s *E2ESuite) TestMaxParallelUpgradePolicyDefaults(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:         &enable,
		RebootRequired: &rebootRequired,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.2.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestMaxParallelUpgradeTwoNodes(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.2.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestMaxParallelUpgradeWithDrainPolicy(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	force := true
	drainPolicy := v1alpha1.DrainSpec{
		Force:          &force,
		TimeoutSeconds: 300,
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		NodeDrainPolicy:     &drainPolicy,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.2.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestMaxParallelUpgradeWithPodDeletionPolicy(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	force := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	podDeletionPolicy := v1alpha1.PodDeletionSpec{
		Force:          &force,
		TimeoutSeconds: 300,
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
		PodDeletionPolicy:   &podDeletionPolicy,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.2.2"
	}
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestMaxParallelUpgradeBackToDefaultVersion(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = "6.2.2"
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = ""
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestMaxParallelUpgradeFromDefaultVersion(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:              &enable,
		RebootRequired:      &rebootRequired,
		MaxParallelUpgrades: 2,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	devCfg.Spec.Driver.Version = ""
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// upgrade
	// update the CR's driver version config
	devCfg.Spec.Driver.Version = "6.2.2"
	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
		s.verifyNodeDriverVersionLabel(devCfg, c)
		err = utils.DeployRocmPods(context.TODO(), s.clientSet, nil)
		assert.NoError(c, err, "failed to deploy pods")
		s.verifyROCMPOD(true, c)
	}

	// delete
	s.deleteDeviceConfig(devCfg, c)

	// Verify rocm pod deployment only for real amd gpu setup
	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}

func (s *E2ESuite) TestDevicePluginNodeLabellerDaemonSetUpgrade(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage
	upgradePolicy := v1alpha1.DaemonSetUpgradeSpec{
		UpgradeStrategy: "RollingUpdate",
		MaxUnavailable:  1,
	}
	devCfg.Spec.DevicePlugin.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

	// upgrade
	// update the CR's device plugin with image
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage2
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage2
	s.patchDevicePluginImage(devCfg, c)
	s.patchNodeLabellerImage(devCfg, c)
	s.verifyDevicePluginStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNodeLabellerStatus(s.ns, c, devCfg)

	// delete
	s.deleteDeviceConfig(devCfg, c)

}

func (s *E2ESuite) TestMetricsExporterDaemonSetUpgrade(c *C) {
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	upgradePolicy := v1alpha1.DaemonSetUpgradeSpec{
		UpgradeStrategy: "RollingUpdate",
		MaxUnavailable:  2,
	}
	devCfg.Spec.MetricsExporter.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)

	// upgrade
	// update the CR's device plugin with image
	devCfg.Spec.MetricsExporter.Image = exporterImage2
	s.patchMetricsExporterImage(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)

}

func (s *E2ESuite) TestKMMOperatorUpgrade(c *C) {
	if s.openshift {
		c.Skip("Skipping for openshift testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	s.createDeviceConfig(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkKMMOperatorStatus(s.ns, c, "")

	// Upgrade KMM using the new helm chart
	logger.Infof("Upgrading KMM operator to new version")
	chartPath := "./yamls/charts/gpu-operator-helm-k8s-v1.0.0.tgz"
	upgradeCmd := exec.Command("helm", "upgrade", "amd-gpu-operator", chartPath, "-n", s.ns)
	output, err := upgradeCmd.CombinedOutput()
	logger.Infof("Helm upgrade output: %s", string(output))
	assert.NoError(c, err, "Helm upgrade failed")

	// Verify the status of NFD and KMM after upgrade
	logger.Infof("Checking NFD worker status post-upgrade")
	s.checkNFDWorkerStatus(s.ns, c, "")
	logger.Infof("Checking KMM operator status post-upgrade")
	s.checkKMMOperatorStatus(s.ns, c, "")

	// Rollback to the previous version
	logger.Infof("Rolling back KMM operator to the previous version")
	rollbackCmd := exec.Command("helm", "rollback", "amd-gpu-operator", "1", "-n", s.ns)
	rollbackOutput, rollbackErr := rollbackCmd.CombinedOutput()
	logger.Infof("Helm rollback output: %s", string(rollbackOutput))
	assert.NoError(c, rollbackErr, "Helm rollback failed")

	// Verify the status again after rollback
	logger.Infof("Checking NFD worker status post-rollback")
	s.checkNFDWorkerStatus(s.ns, c, "")
	logger.Infof("Checking KMM operator status post-rollback")
	s.checkKMMOperatorStatus(s.ns, c, "")

	logger.Infof("Deleting device configuration")
	s.deleteDeviceConfig(devCfg, c)
}

func (s *E2ESuite) TestPreUpgradeHookFailure(c *C) {
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)
	enable := true
	rebootRequired := false
	if !s.simEnable {
		rebootRequired = true
	}
	upgradePolicy := v1alpha1.DriverUpgradePolicySpec{
		Enable:         &enable,
		RebootRequired: &rebootRequired,
	}
	devCfg.Spec.Driver.UpgradePolicy = &upgradePolicy
	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Initiate Driver Version Upgrade
	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.1b"
	} else {
		devCfg.Spec.Driver.Version = "6.2.2"
	}

	nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
	s.patchDriversVersion(devCfg, c)

	// Check if the upgrade is in progress
	assert.Eventually(c, func() bool {
		updatedCfg, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get deviceConfig %v", err)
			return false
		}
		return s.isUpgradeInProgress(updatedCfg)
	}, 10*time.Minute, 5*time.Second, "Upgrade did not enter in-progress state as expected")

	chartPath := "./yamls/charts/gpu-operator-helm-k8s-v1.0.0.tgz"
	upgradeCmd := exec.Command("helm", "upgrade", "amd-gpu-operator", chartPath, "-n", s.ns)
	expectedError := "Error: UPGRADE FAILED: pre-upgrade hooks failed: 1 error occurred:\n\t* job pre-upgrade-check failed: BackoffLimitExceeded"

	output, err := upgradeCmd.CombinedOutput()
	logger.Infof("Helm upgrade output: %s", string(output))
	if assert.Error(c, err, "Helm upgrade should fail during upgrade-in-progress state") {
		// Check that the error message contains the expected substring
		assert.Contains(c, string(output), expectedError, "Upgrade failed, but the error message is not as expected")
		logger.Infof("Upgrade failed as expected with the correct error: %s", expectedError)
	} else {
		logger.Errorf("Unexpected error during helm upgrade: %v", err)
	}

	if s.openshift {
		devCfg.Spec.Driver.Version = "el9-6.1.0"
	} else {
		devCfg.Spec.Driver.Version = "6.1.0"
	}
	s.patchDriversVersion(devCfg, c)
	s.verifyDeviceConfigStatus(devCfg, c)

	// Clean Up DeviceConfig
	s.deleteDeviceConfig(devCfg, c)

	if !s.simEnable {
		s.verifyROCMPOD(false, c)
		err = utils.DelRocmPods(context.TODO(), s.clientSet)
		assert.NoError(c, err, "failed to remove rocm pods")
		err = utils.RebootNodesWithWait(context.TODO(), s.clientSet, nodes)
		assert.NoError(c, err, "failed to reboot nodes")
	}
}
