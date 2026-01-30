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
	"fmt"
	"os"
	"os/exec"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

const (
	releaseName             = "amd-gpu-operator"
	defaultDeviceConfigName = "default"
	tmpValuesYamlPath       = "/tmp/values.yaml"
)

var (
	boolTrue      = true
	boolFalse     = false
	testLabelName = "test123"
)

func (s *E2ESuite) installHelmChart(c *C, expectErr bool, extraArgs []string) {
	helmChartPath, ok := os.LookupEnv("GPU_OPERATOR_CHART")
	if !ok {
		c.Fatalf("failed to get helm chart path from env GPU_OPERATOR_CHART")
	}
	args := []string{"install", releaseName, "-n", s.ns, helmChartPath}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	logger.Infof("Running command %+v", cmd.String())
	if err := cmd.Run(); err != nil && !expectErr {
		c.Fatalf("failed to install helm chart err %+v %+v", err, stderr.String())
	}
}

func (s *E2ESuite) uninstallHelmChart(c *C, expectErr bool, extraArgs []string) {
	args := []string{"delete", releaseName, "-n", s.ns}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	logger.Infof("Running command %+v", cmd.String())
	if err := cmd.Run(); err != nil && !expectErr {
		c.Fatalf("failed to uninstall helm chart err %+v %+v", err, stderr.String())
	}
}

func (s *E2ESuite) upgradeHelmChart(c *C, expectErr bool, extraArgs []string) {
	helmChartPath, ok := os.LookupEnv("GPU_OPERATOR_CHART")
	if !ok {
		c.Fatalf("failed to get helm chart path from env GPU_OPERATOR_CHART")
	}
	args := []string{"upgrade", releaseName, "-n", s.ns, helmChartPath}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	logger.Infof("Running command %+v", cmd.String())
	if err := cmd.Run(); err != nil && !expectErr {
		c.Fatalf("failed to upgrade helm chart err %+v %+v", err, stderr.String())
	}
}

func (s *E2ESuite) verifyDefaultDeviceConfig(c *C, testName string, expect bool,
	expectSpec *v1alpha1.DeviceConfigSpec,
	verifyFunc func(expect, actual *v1alpha1.DeviceConfigSpec) bool) {
	devCfgList, err := s.dClient.DeviceConfigs(s.ns).List(v1.ListOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		assert.NoError(c, err, fmt.Sprintf("test %v error listing DeviceConfig", testName))
	}
	if !expect && err != nil {
		// default CR was removed and even CRD was removed
		return
	}
	if !expect && err == nil && devCfgList != nil && len(devCfgList.Items) == 0 {
		// default CR was removed but CRD was not removed yet
		return
	}
	if expect && err == nil && devCfgList != nil {
		// make sure only one default CR exists
		assert.True(c, len(devCfgList.Items) == 1,
			"test %v expect only one default DeviceConfig but got %+v %+v",
			testName, len(devCfgList.Items), devCfgList.Items)
		// verify metadata
		assert.True(c, devCfgList.Items[0].Name == defaultDeviceConfigName,
			"test %v expect default DeviceConfig name to be %v but got %v",
			testName, defaultDeviceConfigName, devCfgList.Items[0].Name)
		assert.True(c, devCfgList.Items[0].Namespace == s.ns,
			"test %v expect default DeviceConfig namespace to be %v but got %v",
			testName, s.ns, devCfgList.Items[0].Namespace)
		// verify spec
		if expectSpec != nil && verifyFunc != nil {
			assert.True(c, verifyFunc(expectSpec, &devCfgList.Items[0].Spec),
				fmt.Sprintf("test %v expect %+v got %+v", testName, expectSpec, &devCfgList.Items[0].Spec))
		}
		return
	}
	c.Fatalf("test %v unexpected default CR, expect %+v list error %+v devCfgList %+v",
		testName, expect, err, devCfgList)
}

func (s *E2ESuite) verifySelector(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.Selector, actual.Selector)
}

func (s *E2ESuite) verifyDriver(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.Driver, actual.Driver)
}

func (s *E2ESuite) verifyCommonConfig(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.CommonConfig, actual.CommonConfig)
}

func (s *E2ESuite) verifyMetricsExporter(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.MetricsExporter, actual.MetricsExporter)
}

func (s *E2ESuite) verifyTestRunner(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.TestRunner, actual.TestRunner)
}

func (s *E2ESuite) verifyConfigManager(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.ConfigManager, actual.ConfigManager)
}

func (s *E2ESuite) verifyDevicePlugin(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.DevicePlugin, actual.DevicePlugin)
}

func (s *E2ESuite) verifyRemediationWorkflow(expect, actual *v1alpha1.DeviceConfigSpec) bool {
	return expect != nil && actual != nil &&
		reflect.DeepEqual(expect.RemediationWorkflow, actual.RemediationWorkflow)
}

func (s *E2ESuite) writeYAMLToFile(yamlContent string) error {
	os.Remove(tmpValuesYamlPath)
	file, err := os.Create(tmpValuesYamlPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(yamlContent)
	return err
}

func (s *E2ESuite) TestHelmInstallDefaultCR(c *C) {
	// basic test case
	// install + verify default CR was created
	// uninstall + verify default CR was removed
	s.installHelmChart(c, false, nil)
	// verify default CR was created
	s.verifyDefaultDeviceConfig(c, "TestHelmInstallDefaultCR - initial install", true, nil, nil)
	s.uninstallHelmChart(c, false, nil)
	// verify default CR was removed
	s.verifyDefaultDeviceConfig(c, "TestHelmInstallDefaultCR - uninstall", false, nil, nil)
}

func (s *E2ESuite) TestHelmUpgradeDefaultCR(c *C) {
	s.installHelmChart(c, false, []string{"--set", "crds.defaultCR.install=false"})
	// verify default CR was not created when disabled by --set
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - initial install", false, nil, nil)
	s.upgradeHelmChart(c, false, nil)
	// verify that by default helm upgrade won't deploy default CR
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - initial upgrade", false, nil, nil)
	s.upgradeHelmChart(c, false, []string{"--set", "crds.defaultCR.upgrade=true"})
	// helm upgrade with --set to turn on crds.defaultCR.upgrade will deploy default CR
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - upgrade to deploy default CR", true, nil, nil)
	s.uninstallHelmChart(c, false, nil)
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - 1st uninstall", false, nil, nil)

	s.installHelmChart(c, false, nil)
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - 2nd install", true, nil, nil)
	s.upgradeHelmChart(c, false, nil)
	// verify that default upgrade won't affect the existing default CR
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - 2nd upgrade", true, nil, nil)
	s.uninstallHelmChart(c, false, nil)
	s.verifyDefaultDeviceConfig(c, "TestHelmUpgradeDefaultCR - initial uninstall", false, nil, nil)
}

func (s *E2ESuite) TestHelmRenderDefaultCR(c *C) {
	testCases := []struct {
		description          string
		valuesYAML           string
		extraArgs            []string
		helmFunc             func(c *C, expectErr bool, extraArgs []string)
		expectHelmCommandErr bool
		expectDefaultCR      bool
		expectSpec           *v1alpha1.DeviceConfigSpec
		verifyFunc           func(expect, actual *v1alpha1.DeviceConfigSpec) bool
	}{
		{
			description: "invalid values.yaml",
			valuesYAML: `
<invalid format of yaml file>
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath},
			helmFunc:             s.installHelmChart,
			expectHelmCommandErr: true,
		},
		{
			description: "install with rendering spec.selector",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath},
			helmFunc:             s.installHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				Selector: map[string]string{
					"kubernetes.io/hostname":             "node123",
					"feature.node.kubernetes.io/amd-gpu": "true",
				},
			},
			verifyFunc: s.verifySelector,
		},
		{
			description: "upgrade with rendering spec.driver",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      driverType: container
      vfioConfig:
        deviceIDs:
          - 74a1
          - 740f
      kernelModuleConfig:
        loadArgs:
          - arg1=val1
          - arg2=val2
        unloadArgs:
          - unloadArg1=unloadVal1
          - unloadArg2=unloadVal2
        parameters:
          - parameter1=val1
          - parameter2=val2
          - parameter3=val3
      image: "test.io/username/repo"
      imageRegistrySecret:
        name: pull-secret
      imageRegistryTLS:
        # -- set to true to use plain HTTP for driver image repository
        insecure: true
        # -- set to true to skip TLS validation for driver image repository
        insecureSkipTLSVerify: true
      version: "6.3.3"
      imageSign:
        keySecret:
          name: privateKeySecret
        certSecret:
          name: publicKeySecret
      imageBuild:
        baseImageRegistry: quay.io
        sourceImageRepo: custom.io/rocm/amdgpu-driver
        baseImageRegistryTLS:
          insecure: true
          insecureSkipTLSVerify: false
      useSourceImage: true
      tolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
      upgradePolicy:
        # -- enable/disable automatic driver upgrade feature 
        enable: false
        # -- how many nodes can be upgraded in parallel
        maxParallelUpgrades: 5
        # -- maximum number of nodes that can be in a failed upgrade state beyond which upgrades will stop to keep cluster at a minimal healthy state
        maxUnavailableNodes: 50%
        # -- whether reboot each worker node or not during the driver upgrade
        rebootRequired: false
        nodeDrainPolicy:
          # -- whether force draining is allowed or not
          force: false
          # -- the length of time in seconds to wait before giving up drain, zero means infinite
          timeoutSeconds: 600
          # -- the time kubernetes waits for a pod to shut down gracefully after receiving a termination signal, zero means immediate, minus value means follow pod defined grace period
          gracePeriodSeconds: -2
          ignoreDaemonSets: true
        podDeletionPolicy:
          # -- whether force deletion is allowed or not
          force: false
          # -- the length of time in seconds to wait before giving up on pod deletion, zero means infinite
          timeoutSeconds: 600
          # -- the time kubernetes waits for a pod to shut down gracefully after receiving a termination signal, zero means immediate, minus value means follow pod defined grace period
          gracePeriodSeconds: -2
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				Driver: v1alpha1.DriverSpec{
					Enable:     &boolTrue,
					DriverType: utils.DriverTypeContainer,
					VFIOConfig: v1alpha1.VFIOConfigSpec{
						DeviceIDs: []string{"74a1", "740f"},
					},
					KernelModuleConfig: v1alpha1.KernelModuleConfigSpec{
						LoadArgs: []string{
							"arg1=val1",
							"arg2=val2",
						},
						UnloadArgs: []string{
							"unloadArg1=unloadVal1",
							"unloadArg2=unloadVal2",
						},
						Parameters: []string{
							"parameter1=val1",
							"parameter2=val2",
							"parameter3=val3",
						},
					},
					Blacklist: &boolTrue,
					Image:     "test.io/username/repo",
					ImageRegistrySecret: &corev1.LocalObjectReference{
						Name: "pull-secret",
					},
					ImageRegistryTLS: v1alpha1.RegistryTLS{
						Insecure:              &boolTrue,
						InsecureSkipTLSVerify: &boolTrue,
					},
					Version: "6.3.3",
					ImageSign: v1alpha1.ImageSignSpec{
						KeySecret: &corev1.LocalObjectReference{
							Name: "privateKeySecret",
						},
						CertSecret: &corev1.LocalObjectReference{
							Name: "publicKeySecret",
						},
					},
					UseSourceImage: &boolTrue,
					ImageBuild: v1alpha1.ImageBuildSpec{
						BaseImageRegistry: "quay.io",
						SourceImageRepo:   "custom.io/rocm/amdgpu-driver",
						BaseImageRegistryTLS: v1alpha1.RegistryTLS{
							Insecure:              &boolTrue,
							InsecureSkipTLSVerify: &boolFalse,
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					UpgradePolicy: &v1alpha1.DriverUpgradePolicySpec{
						Enable:              &boolFalse,
						MaxParallelUpgrades: 5,
						MaxUnavailableNodes: intstr.FromString("50%"),
						RebootRequired:      &boolFalse,
						NodeDrainPolicy: &v1alpha1.DrainSpec{
							Force:              &boolFalse,
							TimeoutSeconds:     600,
							GracePeriodSeconds: -2,
							IgnoreDaemonSets:   &boolTrue,
						},
						PodDeletionPolicy: &v1alpha1.PodDeletionSpec{
							Force:              &boolFalse,
							TimeoutSeconds:     600,
							GracePeriodSeconds: -2,
						},
					},
				},
			},
			verifyFunc: s.verifyDriver,
		},
		{
			description: "upgrade with rendering spec.commonConfig",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      # -- init container image
      initContainerImage: busybox:1.37
      utilsContainer:
        # -- gpu operator utility container image
        image: test.io/test/gpu-operator-utils:v1.3.0
        # -- utility container image pull policy
        imagePullPolicy: Always
        # -- utility container image pull secret, e.g. {"name": "mySecretName"}
        imageRegistrySecret:
          name: mySecretName
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				CommonConfig: v1alpha1.CommonConfigSpec{
					InitContainerImage: "busybox:1.37",
					UtilsContainer: v1alpha1.UtilsContainerSpec{
						Image:           "test.io/test/gpu-operator-utils:v1.3.0",
						ImagePullPolicy: "Always",
						ImageRegistrySecret: &corev1.LocalObjectReference{
							Name: "mySecretName",
						},
					},
				},
			},
			verifyFunc: s.verifyCommonConfig,
		},
		{
			description: "upgrade with rendering spec.devicePlugin",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      # -- init container image
      initContainerImage: busybox:1.37
    devicePlugin:
      # -- device plugin image
      devicePluginImage: test/k8s-device-plugin:latest
      # -- device plugin image pull policy
      devicePluginImagePullPolicy: Always
      # -- device plugin tolerations
      devicePluginTolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
        - key: "example-key2"
          operator: "Equal"
          value: "example-value2"
          effect: "NoExecute"
      devicePluginArguments:
        resource_naming_strategy: mixed
      # -- enable / disable node labeller
      enableNodeLabeller: false
      # -- node labeller image
      nodeLabellerImage: test/k8s-device-plugin:labeller-latest
      # -- node labeller image pull policy
      nodeLabellerImagePullPolicy: Always
      # -- node labeller tolerations
      nodeLabellerTolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
      # -- pass supported labels while starting node labeller daemonset, default ["vram", "cu-count", "simd-count", "device-id", "family", "product-name", "driver-version"], also support ["compute-memory-partition", "compute-partitioning-supported", "memory-partitioning-supported"]
      nodeLabellerArguments:
        - vram
        - cu-count
      # -- image pull secret for device plugin and node labeller, e.g. {"name": "mySecretName"}
      imageRegistrySecret:
        name: mySecretName
      upgradePolicy:
        # -- the type of daemonset upgrade, RollingUpdate or OnDelete
        upgradeStrategy: OnDelete
        # -- the maximum number of Pods that can be unavailable during the update process
        maxUnavailable: 5
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				DevicePlugin: v1alpha1.DevicePluginSpec{
					DevicePluginImage:           "test/k8s-device-plugin:latest",
					DevicePluginImagePullPolicy: "Always",
					DevicePluginTolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
						{
							Key:      "example-key2",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value2",
							Effect:   corev1.TaintEffectNoExecute,
						},
					},
					DevicePluginArguments: map[string]string{
						"resource_naming_strategy": "mixed",
					},
					EnableNodeLabeller:          &boolFalse,
					NodeLabellerImage:           "test/k8s-device-plugin:labeller-latest",
					NodeLabellerImagePullPolicy: "Always",
					NodeLabellerTolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					NodeLabellerArguments: []string{"vram", "cu-count"},
					ImageRegistrySecret: &corev1.LocalObjectReference{
						Name: "mySecretName",
					},
					UpgradePolicy: &v1alpha1.DaemonSetUpgradeSpec{
						UpgradeStrategy: "OnDelete",
						MaxUnavailable:  5,
					},
				},
			},
			verifyFunc: s.verifyDevicePlugin,
		},
		{
			description: "upgrade with rendering spec.testRunner",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      # -- init container image
      initContainerImage: busybox:1.37
    devicePlugin:
      # -- device plugin image
      devicePluginImage: test/k8s-device-plugin:latest
      # -- device plugin image pull policy
      devicePluginImagePullPolicy: Always
    testRunner:
      # -- enable / disable test runner
      enable: true
      image: test.io/test/test-runner:v1.3.0
      imagePullPolicy: "Always" 
      # -- test runner config map, e.g. {"name": "myConfigMap"}
      config:
        name: myConfigMap
      logsLocation:
        # -- test runner internal mounted directory to save test run logs
        mountPath: "/var/log/amd-test-runner123" 
        # -- host directory to save test run logs
        hostPath: "/var/log/amd-test-runner321"
        logsExportSecrets:
          - name: azure
          - name: gcp
          - name: s3
      upgradePolicy:
        upgradeStrategy: OnDelete
        maxUnavailable: 10
      tolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
      imageRegistrySecret:
        name: mySecret123
      selector:
        "testRun": "true"
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				TestRunner: v1alpha1.TestRunnerSpec{
					Enable:          &boolTrue,
					Image:           "test.io/test/test-runner:v1.3.0",
					ImagePullPolicy: "Always",
					Config: &corev1.LocalObjectReference{
						Name: "myConfigMap",
					},
					LogsLocation: v1alpha1.LogsLocationConfig{
						MountPath: "/var/log/amd-test-runner123",
						HostPath:  "/var/log/amd-test-runner321",
						LogsExportSecrets: []*corev1.LocalObjectReference{
							{
								Name: "azure",
							},
							{
								Name: "gcp",
							},
							{
								Name: "s3",
							},
						},
					},
					UpgradePolicy: &v1alpha1.DaemonSetUpgradeSpec{
						UpgradeStrategy: "OnDelete",
						MaxUnavailable:  10,
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					ImageRegistrySecret: &corev1.LocalObjectReference{
						Name: "mySecret123",
					},
					Selector: map[string]string{
						"testRun": "true",
					},
				},
			},
			verifyFunc: s.verifyTestRunner,
		},
		{
			description: "upgrade with rendering spec.configManager",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      # -- init container image
      initContainerImage: busybox:1.37
    devicePlugin:
      # -- device plugin image
      devicePluginImage: test/k8s-device-plugin:latest
      # -- device plugin image pull policy
      devicePluginImagePullPolicy: Always
    testRunner:
      enable: true
      image: test.io/test/test-runner:v1.3.0
      imagePullPolicy: "Always"
    configManager:
      enable: true
      image: test.io/test/device-config-manager:v1.3.0
      imagePullPolicy: "Always"
      imageRegistrySecret:
        name: mySecret456
      config:
        name: myConfigMap
      selector:
        "dcm": "true"
      upgradePolicy:
        upgradeStrategy: OnDelete
        maxUnavailable: 10
      configManagerTolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				ConfigManager: v1alpha1.ConfigManagerSpec{
					Enable:          &boolTrue,
					Image:           "test.io/test/device-config-manager:v1.3.0",
					ImagePullPolicy: "Always",
					ImageRegistrySecret: &corev1.LocalObjectReference{
						Name: "mySecret456",
					},
					Config: &corev1.LocalObjectReference{
						Name: "myConfigMap",
					},
					Selector: map[string]string{
						"dcm": "true",
					},
					UpgradePolicy: &v1alpha1.DaemonSetUpgradeSpec{
						UpgradeStrategy: "OnDelete",
						MaxUnavailable:  10,
					},
					ConfigManagerTolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			verifyFunc: s.verifyConfigManager,
		},
		{
			description: "upgrade with rendering spec.metricsExporter",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      # -- init container image
      initContainerImage: busybox:1.37
    devicePlugin:
      # -- device plugin image
      devicePluginImage: test/k8s-device-plugin:latest
      # -- device plugin image pull policy
      devicePluginImagePullPolicy: Always
    testRunner:
      enable: true
      image: test.io/test/test-runner:v1.3.0
      imagePullPolicy: "Always"
    configManager:
      enable: true
      image: test.io/test/device-config-manager:v1.3.0
    metricsExporter:
      enable: false
      serviceType: NodePort
      port: 5001
      nodePort: 32501
      image: test/device-metrics-exporter:v1.3.0
      imagePullPolicy: "Always"
      config:
        name: metricsConfig
      podResourceAPISocketPath: /var/lib/kubelet/pod-resources-custom
      resource:
        limits:
          cpu: "4"
          memory: "8G"
        requests:
          cpu: "1"
          memory: "1G"
      podAnnotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "5001"
      serviceAnnotations:
        service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      tolerations:
        - key: "example-key"
          operator: "Equal"
          value: "example-value"
          effect: "NoSchedule"
      imageRegistrySecret:
        name: mySecret123
      selector:
        "exporter": "true"
      upgradePolicy:
        upgradeStrategy: RollingUpdate
        maxUnavailable: 5
      rbacConfig:
        enable: true
        image: quay.io/brancz/kube-rbac-proxy:latest
        disableHttps: false
        secret:
          name: rbacProxySecret
        clientCAConfigMap:
          name: clientCA
        staticAuthorization:
          enable: true
          clientName: "test"
      prometheus:
        serviceMonitor:
          enable: false
          interval: 30s
          attachMetadata:
            node: true
          honorLabels: false
          honorTimestamps: true
          labels:
            source: exporter
          relabelings:
            - targetLabel: test1
              replacement: test123
              action: Replace
          metricRelabelings:
            - targetLabel: test2
              replacement: test123
              action: Replace
          authorization:
            type: Bearer
            credentials:
              name: test
              key: test123
          bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
          tlsConfig:
            keyFile: /etc/credential
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				MetricsExporter: v1alpha1.MetricsExporterSpec{
					Enable:          &boolFalse,
					SvcType:         v1alpha1.ServiceTypeNodePort,
					Port:            5001,
					NodePort:        32501,
					Image:           "test/device-metrics-exporter:v1.3.0",
					ImagePullPolicy: "Always",
					Config: v1alpha1.MetricsConfig{
						Name: "metricsConfig",
					},
					PodResourceAPISocketPath: "/var/lib/kubelet/pod-resources-custom",
					Resource: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("8G"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1G"),
						},
					},
					PodAnnotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   "5001",
					},
					ServiceAnnotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "example-key",
							Operator: corev1.TolerationOpEqual,
							Value:    "example-value",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					ImageRegistrySecret: &corev1.LocalObjectReference{
						Name: "mySecret123",
					},
					Selector: map[string]string{
						"exporter": "true",
					},
					UpgradePolicy: &v1alpha1.DaemonSetUpgradeSpec{
						UpgradeStrategy: "RollingUpdate",
						MaxUnavailable:  5,
					},
					RbacConfig: v1alpha1.KubeRbacConfig{
						Enable:       &boolTrue,
						Image:        "quay.io/brancz/kube-rbac-proxy:latest",
						DisableHttps: &boolFalse,
						Secret: &corev1.LocalObjectReference{
							Name: "rbacProxySecret",
						},
						ClientCAConfigMap: &corev1.LocalObjectReference{
							Name: "clientCA",
						},
						StaticAuthorization: &v1alpha1.StaticAuthConfig{
							Enable:     boolTrue,
							ClientName: "test",
						},
					},
					Prometheus: &v1alpha1.PrometheusConfig{
						ServiceMonitor: &v1alpha1.ServiceMonitorConfig{
							Enable:   &boolFalse,
							Interval: "30s",
							AttachMetadata: &monitoringv1.AttachMetadata{
								Node: &boolTrue,
							},
							HonorLabels:     &boolFalse,
							HonorTimestamps: &boolTrue,
							Labels: map[string]string{
								"source": "exporter",
							},
							Relabelings: []monitoringv1.RelabelConfig{
								{
									TargetLabel: "test1",
									Replacement: &testLabelName,
									Action:      "Replace",
								},
							},
							MetricRelabelings: []monitoringv1.RelabelConfig{
								{
									TargetLabel: "test2",
									Replacement: &testLabelName,
									Action:      "Replace",
								},
							},
							Authorization: &monitoringv1.SafeAuthorization{
								Type: "Bearer",
								Credentials: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "test",
									},
									Key: "test123",
								},
							},
							BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
							TLSConfig: &monitoringv1.TLSConfig{
								KeyFile: "/etc/credential",
							},
						},
					},
				},
			},
			verifyFunc: s.verifyMetricsExporter,
		},
		{
			description: "upgrade with rendering spec.remediationWorkflow",
			valuesYAML: `
deviceConfig:
  spec:
    selector:
      kubernetes.io/hostname: "node123"
      feature.node.kubernetes.io/amd-gpu: "true"
    driver:
      enable: true
      blacklist: true
      image: "test.io/username/repo"
    commonConfig:
      initContainerImage: busybox:1.37
    devicePlugin:
      devicePluginImage: test/k8s-device-plugin:latest
      devicePluginImagePullPolicy: Always
    remediationWorkflow:
      enable: true
      conditionalWorkflows:
        name: "conditional-workflows-configmap"
      ttlForFailedWorkflows: 36
      testerImage: "test.io/test/remediation-workflow-tester:v1.3.0"
`,
			extraArgs:            []string{"-f", tmpValuesYamlPath, "--set", "crds.defaultCR.upgrade=true"},
			helmFunc:             s.upgradeHelmChart,
			expectHelmCommandErr: false,
			expectDefaultCR:      true,
			expectSpec: &v1alpha1.DeviceConfigSpec{
				RemediationWorkflow: v1alpha1.RemediationWorkflowSpec{
					Enable: &boolTrue,
					ConditionalWorkflows: &corev1.LocalObjectReference{
						Name: "conditional-workflows-configmap",
					},
					TtlForFailedWorkflows: 36,
					TesterImage:           "test.io/test/remediation-workflow-tester:v1.3.0",
				},
			},
			verifyFunc: s.verifyRemediationWorkflow,
		},
	}

	for _, tc := range testCases {
		logger.Info(fmt.Sprintf("Running test case %+v", tc.description))
		assert.NoError(c, s.writeYAMLToFile(tc.valuesYAML),
			"failed to prepare yaml file for test case %+v", tc.description)
		tc.helmFunc(c, tc.expectHelmCommandErr, tc.extraArgs)
		if tc.expectHelmCommandErr {
			continue
		}
		s.verifyDefaultDeviceConfig(c, tc.description, tc.expectDefaultCR, tc.expectSpec, tc.verifyFunc)
	}
}
