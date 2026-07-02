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

package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	"github.com/ROCm/gpu-operator/internal/cmd"
)

const (
	KindDeviceConfig        = "DeviceConfig"
	defaultOcDriversVersion = "6.2.2"
	openShiftNodeLabel      = "node.openshift.io/os_id"
	NodeFeatureLabelAmdGpu  = "feature.node.kubernetes.io/amd-gpu"
	NodeFeatureLabelAmdVGpu = "feature.node.kubernetes.io/amd-vgpu"
	// kubelet paths
	KubeletDevicePluginsPath = "/var/lib/kubelet/device-plugins"
	// device plugin
	ResourceNamingStrategyFlag = "resource_naming_strategy"
	SingleStrategy             = "single"
	MixedStrategy              = "mixed"
	// node labeller
	experimentalAMDPrefix             = "beta.amd.com"
	amdPrefix                         = "amd.com"
	computePartitioningSupportedLabel = "amd.com/compute-partitioning-supported"
	memoryPartitioningSupportedLabel  = "amd.com/memory-partitioning-supported"
	partitionTypeLabel                = "amd.com/compute-memory-partition"
	// kubevirt
	DriverTypeFlag          = "driver_type"
	DriverTypeContainer     = "container"
	DriverTypeVFPassthrough = "vf-passthrough"
	DriverTypePFPassthrough = "pf-passthrough"
	DefaultUtilsImage       = "docker.io/rocm/gpu-operator-utils:latest"
	// workerMgr related labels
	LoadVFIOAction              = "loadVFIO"
	UnloadVFIOAction            = "unloadVFIO"
	WorkerActionLabelKey        = "gpu.operator.amd.com/worker-action"
	VFIOMountReadyLabelTemplate = "gpu.operator.amd.com/%v.%v.vfio.ready"
	DriverTypeNodeLabelTemplate = "gpu.operator.amd.com/%v.%v.driver"
	KMMModuleReadyLabelTemplate = "kmm.node.kubernetes.io/%v.%v.ready"
	// Operand metadata
	MetricsExporterNameSuffix = "-metrics-exporter"
	TestRunnerNameSuffix      = "-test-runner"
	DevicePluginNameSuffix    = "-device-plugin"
	DRADriverNameSuffix       = "-dra-driver"
	NodeLabellerNameSuffix    = "-node-labeller"
)

var (
	// node labeller
	nodeLabellerKinds = []string{
		"firmware", "family", "driver-version",
		"driver-src-version", "device-id", "product-name",
		"vram", "simd-count", "cu-count",
	}
	allAMDComLabels     = []string{}
	allBetaAMDComLabels = []string{}
	// kubevirt
	DefaultVFDeviceIDs = []string{
		"7410", // MI210 VF
		"74b5", // MI300X VF
		"74b9", // MI325X VF
		"7461", // Radeon Pro V710 MxGPU
		"73ae", // Radeon Pro V620 MxGPU
	}
	DefaultPFDeviceIDs = []string{
		"74a5", // MI325X
		"74a2", // MI308X
		"74b6", // MI308X
		"74a8", // MI308X HF
		"74a0", // MI300A
		"74a1", // MI300X
		"74a9", // MI300X HF
		"74bd", // MI300X HF
		"740f", // MI210
		"7408", // MI250X
		"740c", // MI250/MI250X
	}
)

func init() {
	initLabelLists()
}

func initLabelLists() {
	// pre-generate all the available node labeller labels
	// these 2 lists will be used to clean up old labels on the node
	for _, name := range nodeLabellerKinds {
		allAMDComLabels = append(allAMDComLabels, createLabelPrefix(name, false))
		allBetaAMDComLabels = append(allBetaAMDComLabels, createLabelPrefix(name, true))
	}
	allAMDComLabels = append(allAMDComLabels,
		computePartitioningSupportedLabel,
		memoryPartitioningSupportedLabel,
		partitionTypeLabel,
	)
}

func createLabelPrefix(name string, experimental bool) string {
	var prefix string
	if experimental {
		prefix = experimentalAMDPrefix
	} else {
		prefix = amdPrefix
	}
	return fmt.Sprintf("%s/gpu.%s", prefix, name)
}

func RemoveOldNodeLabels(node *v1.Node) bool {
	updated := false
	if node == nil {
		return false
	}
	// for the amd.com node labels
	// directly remove the old labels
	for _, label := range allAMDComLabels {
		if _, ok := node.Labels[label]; ok {
			delete(node.Labels, label)
			updated = true
		}
	}
	// for the beta.amd.com node labels
	// if it exists, both original label and counter label need to be removed, e.g.
	// beta.amd.com/gpu.family: AI
	// beta.amd.com/gpu.family.AI: "1"
	for _, label := range allBetaAMDComLabels {
		if val, ok := node.Labels[label]; ok {
			delete(node.Labels, label)
			counterLabel := fmt.Sprintf("%s.%s", label, val)
			delete(node.Labels, counterLabel)
			updated = true
		}
	}
	return updated
}

func GetDriverVersion(node v1.Node, deviceConfig amdv1alpha1.DeviceConfig) (string, error) {
	var driverVersion string
	var err error
	if deviceConfig.Spec.Driver.Version != "" {
		driverVersion = deviceConfig.Spec.Driver.Version
	} else {
		defaultDriverVersion, err := GetDefaultDriversVersion(node)
		if err == nil {
			driverVersion = defaultDriverVersion
		}
	}
	return driverVersion, err
}

func GetDefaultDriversVersion(node v1.Node) (string, error) {
	osImageStr := strings.ToLower(node.Status.NodeInfo.OSImage)
	for os, mapper := range defaultDriverversionsMappers {
		if strings.Contains(osImageStr, os) {
			return mapper(osImageStr)
		}
	}
	return "", fmt.Errorf("OS: %s not supported", osImageStr)
}

var defaultDriverversionsMappers = map[string]func(fullImageStr string) (string, error){
	"ubuntu": UbuntuDefaultDriverVersionsMapper,
	"rhel": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
	"redhat": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
	"red hat": func(f string) (string, error) {
		return defaultOcDriversVersion, nil
	},
	"sles": SLESDefaultDriverVersionsMapper,
	"suse": SLESDefaultDriverVersionsMapper,
}

func UbuntuDefaultDriverVersionsMapper(fullImageStr string) (string, error) {
	if strings.Contains(fullImageStr, "20.04") {
		return "6.1.3", nil // due to a known ROCM issue, 6.2 unload + load back may cause system reboot, let's use 6.1.3 as default
	}

	if strings.Contains(fullImageStr, "22.04") {
		re := regexp.MustCompile(`22\.04(?:\.(\d+))?`)
		match := re.FindStringSubmatch(fullImageStr)

		patch := 0
		if len(match) > 1 && match[1] != "" {
			p, err := strconv.Atoi(match[1])
			if err != nil {
				return "", fmt.Errorf("failed to parse patch version from %s: %v", fullImageStr, err)
			}
			patch = p
		}
		if patch >= 5 {
			// For 22.04.5 and above, we use "6.3.3" as default
			return "6.3.3", nil
		}
		// For 22.04.0 to 22.04.4, we use "6.1.3" as default
		// This is due to a known ROCM issue where unloading and loading back the driver may cause system reboot.
		return "6.1.3", nil
	}

	if strings.Contains(fullImageStr, "24.04") {
		return "6.3.3", nil // due to a known ROCM issue, 6.2 unload + load back may cause system reboot, let's use 6.3.3 as default
	}
	return "", fmt.Errorf("unsupported Ubuntu version: %s. Supported versions include 20.04, 22.04 and 24.04", fullImageStr)
}

var slesSPRegexp = regexp.MustCompile(`(\d+)\s*-?\s*sp(\d+)`)
var slesMajorMinorRegexp = regexp.MustCompile(`(\d+)\.(\d+)`)

var slesDefaultDriverVersions = map[string]string{
	"15.7": "31.30",
	"16.0": "31.30",
}

var (
	slesRegistryHTTPClient  = &http.Client{Timeout: 10 * time.Second}
	slesRegistryAuthURL     = "https://scc.suse.com/api/registry/authorize?service=SUSE+Linux+Docker+Registry&scope=repository:third-party/amd/amdgpu-driver:pull"
	slesRegistryManifestFmt = "https://registry.suse.com/v2/third-party/amd/amdgpu-driver/manifests/sles-%s-%s"
)

// slesVersionCache caches registry check results for 5 minutes to avoid redundant calls.
type slesVersionCacheEntry struct {
	err       error
	expiresAt time.Time
}

var (
	slesVersionCache    = map[string]slesVersionCacheEntry{}
	slesVersionCacheMu  sync.RWMutex
	slesVersionCacheTTL = 5 * time.Minute
)

// slesCodestream extracts the codestream key (e.g. "15.7", "16.0") from an OS image string.
func slesCodestream(osImage string) string {
	lower := strings.ToLower(osImage)
	if match := slesSPRegexp.FindStringSubmatch(lower); len(match) >= 3 {
		return fmt.Sprintf("%s.%s", match[1], match[2])
	}
	if match := slesMajorMinorRegexp.FindStringSubmatch(lower); len(match) >= 3 {
		return fmt.Sprintf("%s.%s", match[1], match[2])
	}
	return ""
}

// ValidateSLESDriverVersion checks driverVersion availability on registry.suse.com for the given SLES osImage.
func ValidateSLESDriverVersion(ctx context.Context, osImage, driverVersion string) error {
	lower := strings.ToLower(osImage)
	if !strings.Contains(lower, "suse") && !strings.Contains(lower, "sles") {
		return nil
	}
	cs := slesCodestream(osImage)
	if cs == "" {
		return fmt.Errorf("could not determine SLES codestream from OS image %q", osImage)
	}

	cacheKey := cs + "@" + driverVersion
	slesVersionCacheMu.RLock()
	e, hit := slesVersionCache[cacheKey]
	slesVersionCacheMu.RUnlock()
	if hit && time.Now().Before(e.expiresAt) {
		logr.FromContextOrDiscard(ctx).Info("SLES driver version cache hit", "codestream", cs, "version", driverVersion)
		return e.err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, slesRegistryAuthURL, nil)
	if err != nil {
		return err
	}
	resp, err := slesRegistryHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach SUSE registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SCC auth request failed with status %d", resp.StatusCode)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("could not parse SCC token: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodHead, fmt.Sprintf(slesRegistryManifestFmt, cs, driverVersion), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp2, err := slesRegistryHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach SUSE registry: %w", err)
	}
	defer resp2.Body.Close()

	var result error
	switch resp2.StatusCode {
	case http.StatusOK:
		result = nil
	case http.StatusNotFound:
		result = fmt.Errorf("driver version %q is not available for SLES %s on registry.suse.com", driverVersion, cs)
	default:
		result = fmt.Errorf("unexpected status %d from SUSE registry for SLES %s version %q", resp2.StatusCode, cs, driverVersion)
	}
	slesVersionCacheMu.Lock()
	slesVersionCache[cacheKey] = slesVersionCacheEntry{err: result, expiresAt: time.Now().Add(slesVersionCacheTTL)}
	slesVersionCacheMu.Unlock()
	logr.FromContextOrDiscard(ctx).Info("SLES driver version cache refreshed", "codestream", cs, "version", driverVersion, "available", result == nil)
	return result
}

func SLESDefaultDriverVersionsMapper(fullImageStr string) (string, error) {
	lower := strings.ToLower(fullImageStr)
	var csVersion string

	// SP-style notation used by SLES 15 (e.g. "15 SP7" or "15-sp7")
	if match := slesSPRegexp.FindStringSubmatch(lower); len(match) >= 3 {
		csVersion = fmt.Sprintf("%s.%s", match[1], match[2])
	} else if match := slesMajorMinorRegexp.FindStringSubmatch(lower); len(match) >= 3 {
		// major.minor notation used by SLES 16+ (e.g. "16.0")
		csVersion = fmt.Sprintf("%s.%s", match[1], match[2])
	}

	if csVersion != "" {
		if v, ok := slesDefaultDriverVersions[csVersion]; ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("unsupported SLES version %q", fullImageStr)
}

func HasNodeLabelKey(node v1.Node, labelKey string) bool {
	for k := range node.Labels {
		if k == labelKey {
			return true
		}
	}
	return false
}

func IsOpenShift(logger logr.Logger) bool {
	config, err := rest.InClusterConfig()
	if err != nil {
		cmd.FatalError(logger, err, "unable to get cluster config")
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		cmd.FatalError(logger, err, "unable to create cluster clientset")
	}
	// Check for OpenShift-specific labels on nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		cmd.FatalError(logger, err, "unable to list nodes")
	}

	isOpenShift := false
	for _, node := range nodes.Items {
		if _, exists := node.Labels[openShiftNodeLabel]; exists {
			isOpenShift = true
			break
		}
	}
	logger.Info(fmt.Sprintf("IsOpenShift: %+v", isOpenShift))
	return isOpenShift
}

// IsPrometheusServiceMonitorEnable checks if the Prometheus ServiceMonitor is enabled in the DeviceConfig
func IsPrometheusServiceMonitorEnable(devConfig *amdv1alpha1.DeviceConfig) bool {
	if devConfig.Spec.MetricsExporter.Prometheus != nil &&
		devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor != nil &&
		devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Enable != nil &&
		*devConfig.Spec.MetricsExporter.Prometheus.ServiceMonitor.Enable {
		return true
	}
	return false
}

func GetDriverTypeTag(devCfg *amdv1alpha1.DeviceConfig) string {
	driverTypeTag := ""
	switch devCfg.Spec.Driver.DriverType {
	case DriverTypeVFPassthrough,
		DriverTypePFPassthrough:
		driverTypeTag = "-" + devCfg.Spec.Driver.DriverType
	case DriverTypeContainer:
		// when the driver type is container
		// don't add any driver type inside the driver image tag
		// in order to make sure driver image tag is backward compatible
		// so that the driver image tag built before KubeVirt integration could still apply
	}
	return driverTypeTag
}

func generateRegexPattern(template string) string {
	// Escape dots
	pattern := strings.Replace(template, ".", `\.`, -1)
	// Replace %v with .+ to match any valid characters
	pattern = strings.Replace(pattern, "%v", "([^.]+)", 1)
	pattern = strings.Replace(pattern, "%v", "(.+)", 1)
	// Add start and end anchors
	pattern = "^" + pattern + "$"
	return pattern
}

func HasNodeLabelTemplateMatch(nodeLabels map[string]string, template string) (bool, string, string, string) {
	pattern := generateRegexPattern(template)
	re := regexp.MustCompile(pattern)
	// Check each label key against the pattern
	for key := range nodeLabels {
		matches := re.FindStringSubmatch(key)
		if len(matches) >= 3 {
			return true, key, matches[1], matches[2]
		}
	}
	return false, "", "", ""
}

func GetDriverTypeNodeLabel(devConfig *v1alpha1.DeviceConfig) string {
	return fmt.Sprintf(DriverTypeNodeLabelTemplate, devConfig.Namespace, devConfig.Name)
}

func UpdateDriverTypeNodeLabel(ctx context.Context, cli client.Client, devConfig *v1alpha1.DeviceConfig, nodes *v1.NodeList, cleanup bool) error {
	if devConfig == nil {
		return fmt.Errorf("received nil DeviceConfig")
	}
	if nodes == nil {
		return fmt.Errorf("received nil node list")
	}
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		var err error
		for _, node := range nodes.Items {
			var nodeCopy *v1.Node
			if cleanup {
				if _, ok := node.Labels[GetDriverTypeNodeLabel(devConfig)]; !ok {
					// no need to clean up driver type node label if it is non-existing
					continue
				}
				nodeCopy = node.DeepCopy()
				delete(node.Labels, GetDriverTypeNodeLabel(devConfig))
			} else {
				if val, ok := node.Labels[GetDriverTypeNodeLabel(devConfig)]; ok && val == devConfig.Spec.Driver.DriverType {
					// no need to patch the driver type node label if it is existing
					continue
				}
				nodeCopy = node.DeepCopy()
				if node.Labels == nil {
					node.Labels = map[string]string{}
				}
				node.Labels[GetDriverTypeNodeLabel(devConfig)] = devConfig.Spec.Driver.DriverType
			}
			if patchErr := cli.Patch(ctx, &node, client.MergeFrom(nodeCopy)); patchErr != nil {
				err = errors.Join(err, patchErr)
			}
		}
		return err
	}
	return nil
}

// ShouldUseKMM return true if KMM needs to be triggered otherwise return false
func ShouldUseKMM(devConfig *v1alpha1.DeviceConfig) bool {
	if devConfig == nil {
		return false
	}
	if devConfig.Spec.Driver.Enable != nil && *devConfig.Spec.Driver.Enable {
		switch devConfig.Spec.Driver.DriverType {
		case DriverTypePFPassthrough:
			// for pf-passthrough there is no need to install driver via KMM
			return false
		}
		// for container or vf-passthrough driver KMM is needed to install driver
		return true
	}
	return false
}
