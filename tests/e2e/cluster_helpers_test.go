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

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	serviceMonitorCRDURL   = "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.81.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml"
	amdGpuResourceLabel    = "amd.com/gpu"
	resourceNamingStrategy = "resource_naming_strategy"
	namingStrategySingle   = "single"
	namingStrategyMixed    = "mixed"
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
				Image:    exporterImage,
				NodePort: 32501,
				Port:     5001,
			},
			Selector: map[string]string{"feature.node.kubernetes.io/amd-gpu": "true"},
			CommonConfig: v1alpha1.CommonConfigSpec{
				InitContainerImage: initContainerImage,
			},
		},
	}
	insecure := true
	devCfg.Spec.Driver.ImageRegistryTLS.Insecure = &insecure
	devCfg.Spec.DevicePlugin.DevicePluginImage = devicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = nodeLabellerImage
	if s.simEnable {
		devCfg.Spec.MetricsExporter.Image = exporterMockImage
	}
	if s.openshift {
		devCfg.Spec.Driver.Version = "6.1.1"
	}
	return devCfg
}

func (s *E2ESuite) createDeviceConfig(devCfg *v1alpha1.DeviceConfig, c *C) {
	logger.Infof("Creating DeviceConfig %+v", devCfg)
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
	}, 20*time.Minute, 5*time.Second)
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
	}, 20*time.Minute, 5*time.Second)
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
	}, 20*time.Minute, 5*time.Second)
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

func podSpecHasDCMConfigVolumeMount(spec *v1.PodSpec) bool {
	check := func(mounts []v1.VolumeMount) bool {
		for _, m := range mounts {
			if m.Name == configmanager.ConfigManagerConfigVolumeName && m.MountPath == configmanager.DefaultDCMConfigMountPath {
				return true
			}
		}
		return false
	}
	for i := range spec.Containers {
		if check(spec.Containers[i].VolumeMounts) {
			return true
		}
	}
	for i := range spec.InitContainers {
		if check(spec.InitContainers[i].VolumeMounts) {
			return true
		}
	}
	return false
}

// verifyDCMConfigMapVolumeRef asserts the DCM DaemonSet has a ConfigMap volume
// (configmanager.ConfigManagerConfigVolumeName) pointing at expectedConfigMapName, and that some
// workload or init container mounts that volume at configmanager.DefaultDCMConfigMountPath.
func (s *E2ESuite) verifyDCMConfigMapVolumeRef(devCfg *v1alpha1.DeviceConfig, ns string, expectedConfigMapName string, c *C) {
	dsName := devCfg.Name + "-" + configmanager.ConfigManagerName
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), dsName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("verifyDCMConfigMapVolumeRef: get DS %s: %v", dsName, err)
			return false
		}
		spec := &ds.Spec.Template.Spec
		var volOK bool
		for _, vol := range spec.Volumes {
			if vol.Name != configmanager.ConfigManagerConfigVolumeName || vol.ConfigMap == nil {
				continue
			}
			volOK = true
			if vol.ConfigMap.Name != expectedConfigMapName {
				logger.Errorf("verifyDCMConfigMapVolumeRef: want ConfigMap %q, got %q", expectedConfigMapName, vol.ConfigMap.Name)
				return false
			}
			break
		}
		if !volOK {
			logger.Errorf("verifyDCMConfigMapVolumeRef: volume %q not found or not a ConfigMap", configmanager.ConfigManagerConfigVolumeName)
			return false
		}
		if !podSpecHasDCMConfigVolumeMount(spec) {
			logger.Errorf("verifyDCMConfigMapVolumeRef: no container VolumeMount for volume %q at %q", configmanager.ConfigManagerConfigVolumeName, configmanager.DefaultDCMConfigMountPath)
			return false
		}
		return true
	}, 2*time.Minute, 3*time.Second)
}

func (s *E2ESuite) checkDRADriverStatus(devCfg *v1alpha1.DeviceConfig, ns string, c *C) {
	dsName := utils.DRADriverName(devCfg.Name)
	assert.Eventually(c, func() bool {
		ds, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), dsName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get DRA driver daemonset %s: %v", dsName, err)
			return false
		}
		logger.Infof("DRA driver %s status: desired=%d, ready=%d",
			dsName, ds.Status.DesiredNumberScheduled, ds.Status.NumberReady)

		return ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}, 20*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyDRADriverDeleted(devCfg *v1alpha1.DeviceConfig, ns string, c *C) {
	dsName := utils.DRADriverName(devCfg.Name)
	assert.Eventually(c, func() bool {
		_, err := s.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), dsName, metav1.GetOptions{})
		if err == nil {
			logger.Warnf("DRA driver daemonset %s still exists, waiting for deletion", dsName)
			return false
		}
		logger.Infof("DRA driver daemonset %s deleted", dsName)
		return true
	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) patchDRADriverEnablement(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchDRADriverEnablement(devCfg)
	assert.NoError(c, err, "failed to patch DRA driver enablement on %v", devCfg.Name)
	logger.Info(fmt.Sprintf("patched DRA driver enablement on device config %+v", result))
}

func (s *E2ESuite) patchDevicePluginEnablement(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchDevicePluginEnablement(devCfg)
	assert.NoError(c, err, "failed to patch device plugin enablement on %v", devCfg.Name)
	logger.Info(fmt.Sprintf("patched device plugin enablement on device config %+v", result))
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

func (s *E2ESuite) patchUpgradePolicy(devCfg *v1alpha1.DeviceConfig, c *C) {
	result, err := s.dClient.DeviceConfigs(s.ns).PatchUpgradePolicy(devCfg)
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

func (s *E2ESuite) patchNodeCondition(c *C, nodeName, condType string, status v1.ConditionStatus) {
	patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"%s","status":"%s","reason":"e2e-test","message":"set by e2e test"}]}}`, condType, status)
	_, err := s.clientSet.CoreV1().Nodes().Patch(context.TODO(), nodeName, types.MergePatchType, []byte(patch), metav1.PatchOptions{}, "status")
	c.Assert(err, IsNil, Commentf("failed to patch condition %s=%s for node %s", condType, status, nodeName))
}

func (s *E2ESuite) getWorkflowForNode(c *C, nodeName string) *wfv1.Workflow {
	wfList, err := s.wfClient.ArgoprojV1alpha1().Workflows(s.ns).List(context.TODO(), metav1.ListOptions{})
	c.Assert(err, IsNil)

	for _, wf := range wfList.Items {
		if strings.Contains(wf.Name, nodeName) {
			return &wf
		}
	}
	c.Fatalf("workflow for node %s not found", nodeName)
	return nil
}

func (s *E2ESuite) verifyWorkflowSucceeded(c *C, wf *wfv1.Workflow) {
	assert.Eventually(c, func() bool {
		updated, err := s.wfClient.ArgoprojV1alpha1().Workflows(wf.Namespace).Get(context.TODO(), wf.Name, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("failed to get workflow %s: %v", wf.Name, err)
			return false
		}
		logger.Infof("workflow %s current phase: %s", wf.Name, updated.Status.Phase)
		return updated.Status.Phase == wfv1.WorkflowSucceeded
	}, 15*time.Minute, 10*time.Second)
}

func (s *E2ESuite) deleteWorkflowForNode(c *C, wf *wfv1.Workflow) {
	err := s.wfClient.ArgoprojV1alpha1().Workflows(wf.Namespace).Delete(context.TODO(), wf.Name, metav1.DeleteOptions{})
	c.Assert(err, IsNil, Commentf("failed to delete workflow %s", wf.Name))
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

func (s *E2ESuite) verifyNodeModuleStatus(devCfg *v1alpha1.DeviceConfig, expectedNodeStatus v1alpha1.UpgradeState, c *C) {
	assert.Eventually(c, func() bool {
		devCfg, err := s.dClient.DeviceConfigs(s.ns).Get(devCfg.Name, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Failed to get deviceConfig %v", err)
			return false
		}

		// Check if all NodeModuleStatus entries are in UpgradeStateComplete
		for nodeName, moduleStatus := range devCfg.Status.NodeModuleStatus {
			if moduleStatus.Status != expectedNodeStatus {
				logger.Infof("Upgrade not complete for node %s with state %s", nodeName, moduleStatus.Status)
				return false
			}
			if !strings.HasSuffix(moduleStatus.ContainerImage, devCfg.Spec.Driver.Version) {
				logger.Infof("Upgrade not complete for node %s with state container image %s while DeviceConfig specified version %s",
					nodeName, moduleStatus.ContainerImage, devCfg.Spec.Driver.Version)
				return false
			}
		}

		logger.Infof("All nodes are in expected state %s", expectedNodeStatus)
		return true

	}, 30*time.Minute, 5*time.Second)
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
	}, 20*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyNodeGPULabel(devCfg *v1alpha1.DeviceConfig, label string, c *C) {
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
			if !utils.CheckGpuLabel(node.Status.Capacity, label) {
				logger.Infof("gpu not found in %v, %v ", node.Name, node.Status.Capacity)
				return false
			}
		}
		for _, node := range nodes.Items {
			if !utils.CheckGpuLabel(node.Status.Allocatable, label) {
				logger.Infof("allocatable gpu not found in %v, %v ", node.Name, node.Status.Allocatable)
				return false
			}
		}
		return true

	}, 5*time.Minute, 5*time.Second)
}

func (s *E2ESuite) verifyNodePartitionLabels(devCfg *v1alpha1.DeviceConfig, labelNames []string, labelsPresent bool, c *C) {
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
			for _, label := range labelNames {
				_, exists := node.Labels[label]
				if labelsPresent && !exists {
					// label should be present, but it is not present on the node
					logger.Infof("label %s not found on node %s", label, node.Name)
					return false
				}
				if !labelsPresent && exists {
					// label should not be present, but it is present on the node
					logger.Infof("label %s still present on node %s", label, node.Name)
					return false
				}
			}
		}
		if labelsPresent {
			logger.Infof("labels %v are present as expected", labelNames)
		} else {
			logger.Infof("labels %v are absent as expected", labelNames)
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
			if versionLabelValue == "" {
				versionLabelValue = s.defaultDriverVersion
			}
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
	logger.Infof("rocm pods %v", pods)
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

// TestDriverUpgradeByUpdatingCR
// test the driver upgrade by directly updating CR
// 1. install the driver
// 2. make sure the worker node was labeled with correct driver version
// 3. update the CR to the new driver version
// 4. update the worker node label to the new driver version
// 5. make sure the new version driver was loaded

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

// setupKubeRbacCerts generates a CA, server TLS cert/key (with SANs), and optionally a client cert/key.
func (s *E2ESuite) setupKubeRbacCerts(c *C, includeClient bool) (
	caCertPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM []byte,
	err error,
) {
	// Generate CA
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	caTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"My CA"}},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return
	}
	caBuf := &bytes.Buffer{}
	err = pem.Encode(caBuf, &pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err != nil {
		return
	}

	// Generate Server cert/key with SANs
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	nodeIPs, err := utils.GetNodeIPs(s.clientSet)
	assert.NoError(c, err, "failed to get node IPs for SANs")
	ips := make([]net.IP, 0, len(nodeIPs))
	for _, ip := range nodeIPs {
		ips = append(ips, net.ParseIP(ip))
	}
	serverTmpl := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: []string{"My TLS"}, CommonName: "metrics-server"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTmpl, &caTmpl, &serverKey.PublicKey, caKey)
	if err != nil {
		return
	}
	serverBuf := &bytes.Buffer{}
	err = pem.Encode(serverBuf, &pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	if err != nil {
		return
	}
	err = pem.Encode(serverBuf, &pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err != nil {
		return
	}
	keyBuf := &bytes.Buffer{}
	privBytes, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return
	}
	err = pem.Encode(keyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err != nil {
		return
	}

	caCertPEM = caBuf.Bytes()
	serverCertPEM = serverBuf.Bytes()
	serverKeyPEM = keyBuf.Bytes()

	if includeClient {
		// Generate Client cert/key
		var clientKey *rsa.PrivateKey
		var clientDER, privCliBytes []byte
		clientKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return
		}
		clientTmpl := x509.Certificate{
			SerialNumber: big.NewInt(3),
			Subject:      pkix.Name{Organization: []string{"My Client"}, CommonName: "metrics-reader"},
			NotBefore:    time.Now(),
			NotAfter:     time.Now().Add(365 * 24 * time.Hour),
			KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		clientDER, err = x509.CreateCertificate(rand.Reader, &clientTmpl, &caTmpl, &clientKey.PublicKey, caKey)
		if err != nil {
			return
		}
		cliCertBuf := &bytes.Buffer{}
		err = pem.Encode(cliCertBuf, &pem.Block{Type: "CERTIFICATE", Bytes: clientDER})
		if err != nil {
			return
		}
		cliKeyBuf := &bytes.Buffer{}
		privCliBytes, err = x509.MarshalPKCS8PrivateKey(clientKey)
		if err != nil {
			return
		}
		err = pem.Encode(cliKeyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: privCliBytes})
		if err != nil {
			return
		}
		clientCertPEM = cliCertBuf.Bytes()
		clientKeyPEM = cliKeyBuf.Bytes()
	}
	return
}
