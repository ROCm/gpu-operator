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

// GPU Operator e2e tests — full lifecycle and DME verification.
//
// Test sequence:
//
//	Op000  InstallCertManager       — install cert-manager (operator prerequisite)
//	Op001  InstallGPUOperator       — install AMD GPU Operator via Helm
//	Op010  VerifyNodeLabeller       — NFD labels nodes; Node Labeller DaemonSet ready
//	Op020  VerifyDevicePlugin       — Device Plugin DaemonSet ready; amd.com/gpu allocatable
//	Op030  VerifyKMMDriver          — KMM controller pods Running (skipped for DKMS nodes)
//	Op040  VerifyDMEDaemonSet       — DME DaemonSet ready; key metrics present
//	Op050  VerifyGPUHealth          — all GPUs report gpu_health=1
//	Op060  VerifyCoreMetrics        — VRAM/PCIe/ECC/clock/power/temperature metrics present
//	Op065  VerifyPartitionedGPUMetrics — partition labels and per-partition metrics (auto-skips)
//	Op070  ScheduleGPUWorkload      — rocminfo/amd-smi pod completes successfully (GPU scheduling verified)
//	Op900  TearDownOperator         — uninstall GPU Operator and cert-manager

package gpuope2e

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// certManagerReleaseName is the helm release name for cert-manager.
	certManagerReleaseName = "cert-manager"

	// certManagerChart is the public cert-manager helm chart repo URL.
	certManagerChart = "https://charts.jetstack.io"

	// dmeMetricsPort is the port DME listens on inside its pod.
	dmeMetricsPort = 5000

	// gpuWorkloadPodName is the name of the test GPU workload pod.
	gpuWorkloadPodName = "op-e2e-gpu-workload"

	// operatorPollTimeout is the timeout for waiting on operator-managed resources.
	operatorPollTimeout = 15 * time.Minute
)

// ---- Op000–Op001: install -----------------------------------------------

// TestOp000InstallCertManager installs cert-manager, a prerequisite for the GPU Operator.
// Skipped when running in existing-deploy mode or when cert-manager is already present.
func (s *E2ESuite) TestOp000InstallCertManager(c *C) {
	if s.existingDeploy {
		c.Skip("skipping install: existing deploy mode")
	}
	ctx := context.Background()

	if s.k8sclient.NamespaceExists(ctx, "cert-manager") {
		log.Print("Op000: cert-manager namespace already exists — skipping install")
		return
	}

	log.Print("Op000: adding cert-manager helm repository")
	assert.NoError(c, s.helmClient.AddRepository("jetstack", certManagerChart))

	log.Print("Op000: installing cert-manager")
	_, err := s.helmClient.InstallChartWithTimeout(
		ctx,
		certManagerReleaseName,
		"jetstack/cert-manager",
		"",
		[]string{"installCRDs=true"},
		operatorPollTimeout,
	)
	assert.NoError(c, err)
	log.Print("Op000: cert-manager installed")
}

// TestOp001InstallGPUOperator installs the AMD GPU Operator via Helm using
// -operatorchart and -operatortag. Pass extra overrides with -helmset.
//
// Example overrides for local images:
//
//	-helmset controllerManager.manager.imagePullPolicy=Never
//	-helmset kmm.controller.manager.imagePullPolicy=Never
//	-helmset kmm.webhookServer.webhookServer.imagePullPolicy=IfNotPresent
//	-helmset 'deviceConfig.spec.metricsExporter.image=rocm/device-metrics-exporter:v1.5.0'
func (s *E2ESuite) TestOp001InstallGPUOperator(c *C) {
	if s.existingDeploy {
		c.Skip("skipping install: existing deploy mode")
	}
	ctx := context.Background()

	log.Printf("Op001: installing GPU Operator from chart %q version %s helmset=[%s]",
		s.operatorChart, s.operatorTag, helmSetJoin(s.helmSet))

	params := append([]string{
		fmt.Sprintf("devicePlugin.image.tag=%s", s.operatorTag),
		fmt.Sprintf("nodeLabellerImage.tag=%s", s.operatorTag),
	}, s.helmSet...)

	_, err := s.helmClient.InstallChartWithTimeout(
		ctx,
		operatorReleaseName,
		s.operatorChart,
		s.operatorTag,
		params,
		operatorPollTimeout,
	)
	assert.NoError(c, err)
	log.Print("Op001: GPU Operator installed")
}

// ---- Op010–Op030: infrastructure ----------------------------------------

// TestOp010VerifyNodeLabeller waits for NFD to label GPU nodes and verifies
// the Node Labeller DaemonSet is ready.
func (s *E2ESuite) TestOp010VerifyNodeLabeller(c *C) {
	ctx := context.Background()

	log.Print("Op010: waiting for NFD to label node feature.node.kubernetes.io/amd-gpu=true")
	err := s.k8sclient.WaitForNodeLabel(ctx, "feature.node.kubernetes.io/amd-gpu", "true", operatorPollTimeout)
	assert.NoError(c, err, "NFD did not label node with feature.node.kubernetes.io/amd-gpu=true")

	log.Print("Op010: waiting for node-labeller DaemonSet")
	err = s.k8sclient.WaitForDaemonSetReady(ctx, s.ns, "default-node-labeller", operatorPollTimeout)
	assert.NoError(c, err, "node-labeller DaemonSet did not become ready")

	nodes, err := s.k8sclient.GetNodesByLabel(ctx, map[string]string{
		"feature.node.kubernetes.io/amd-gpu": "true",
	})
	assert.NoError(c, err)
	assert.True(c, len(nodes) > 0, "expected at least one node with feature.node.kubernetes.io/amd-gpu=true")
	log.Printf("Op010: %d node(s) have amd-gpu label", len(nodes))
	for _, n := range nodes {
		for k, v := range n.Labels {
			if strings.HasPrefix(k, "amd.com/gpu.") {
				log.Printf("Op010: node %s label %s=%s", n.Name, k, v)
			}
		}
	}
}

// TestOp020VerifyDevicePlugin verifies the Device Plugin DaemonSet is ready
// and amd.com/gpu resources are allocatable on GPU nodes.
func (s *E2ESuite) TestOp020VerifyDevicePlugin(c *C) {
	ctx := context.Background()

	log.Print("Op020: waiting for device-plugin DaemonSet")
	err := s.k8sclient.WaitForDaemonSetReady(ctx, s.ns, "default-device-plugin", operatorPollTimeout)
	assert.NoError(c, err, "device-plugin DaemonSet did not become ready")

	gpuNodes, err := s.k8sclient.GetNodesByLabel(ctx, map[string]string{
		"feature.node.kubernetes.io/amd-gpu": "true",
	})
	assert.NoError(c, err)
	assert.True(c, len(gpuNodes) > 0, "no GPU nodes found")

	// The device-plugin may take a moment after its pod is Running to register
	// with kubelet and update node allocatable. Poll until GPU count > 0.
	for _, node := range gpuNodes {
		pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		var gpuCount int64
		for {
			gpuCount, err = s.k8sclient.GetNodeAllocatableGPUs(pollCtx, node.Name)
			if err != nil || gpuCount > 0 {
				break
			}
			log.Printf("Op020: node %s allocatable amd.com/gpu = 0, waiting for device-plugin registration…", node.Name)
			select {
			case <-pollCtx.Done():
			case <-time.After(5 * time.Second):
				continue
			}
			break
		}
		cancel()
		assert.NoError(c, err)
		assert.True(c, gpuCount > 0,
			"node %s: expected amd.com/gpu > 0 in allocatable resources, got %d", node.Name, gpuCount)
		log.Printf("Op020: node %s allocatable amd.com/gpu = %d", node.Name, gpuCount)
	}
}

// TestOp030VerifyKMMDriver verifies KMM controller pods are Running.
// Automatically skipped on clusters using a pre-installed DKMS driver.
func (s *E2ESuite) TestOp030VerifyKMMDriver(c *C) {
	ctx := context.Background()

	pods, err := s.k8sclient.GetPodsByLabel(ctx, s.ns, map[string]string{
		"app.kubernetes.io/name": "kmm",
		"control-plane":          "controller",
	})
	if err != nil || len(pods) == 0 {
		log.Print("Op030: no KMM controller pods found — assuming pre-installed DKMS driver; skipping")
		c.Skip("KMM controller not found — pre-installed driver assumed")
		return
	}

	log.Printf("Op030: found %d KMM controller pod(s); phase=%s", len(pods), pods[0].Status.Phase)
	for _, pod := range pods {
		// Skip terminal pods from old ReplicaSet revisions (Succeeded/Failed are expected
		// for completed init or evicted pods from prior rollouts).
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			log.Printf("Op030: skipping terminal pod %s (phase=%s)", pod.Name, pod.Status.Phase)
			continue
		}
		assert.Equal(c, corev1.PodRunning, pod.Status.Phase,
			"KMM pod %s is not Running (phase: %s)", pod.Name, pod.Status.Phase)
	}
}

// ---- Op040–Op065: DME verification --------------------------------------

// TestOp040VerifyDMEDaemonSet verifies the DME DaemonSet is ready and key
// metric families are present in the metrics endpoint.
func (s *E2ESuite) TestOp040VerifyDMEDaemonSet(c *C) {
	ctx := context.Background()

	log.Print("Op040: waiting for metrics-exporter DaemonSet")
	err := s.k8sclient.WaitForDaemonSetReady(ctx, s.ns, "default-metrics-exporter", operatorPollTimeout)
	assert.NoError(c, err, "metrics-exporter DaemonSet did not become ready")

	pods, err := s.k8sclient.GetPodsByLabel(ctx, s.ns, map[string]string{
		"app.kubernetes.io/name": "metrics-exporter",
	})
	assert.NoError(c, err)
	assert.True(c, len(pods) > 0, "no metrics-exporter pods found")

	var dmePod *corev1.Pod
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			dmePod = &pods[i]
			break
		}
	}
	if dmePod == nil {
		assert.Fail(c, "no Running metrics-exporter pod found (pods may be in CrashLoopBackOff)")
		return
	}
	log.Printf("Op040: using DME pod %s (phase=%s)", dmePod.Name, dmePod.Status.Phase)

	var fields []string
	assert.Eventually(c, func() bool {
		_, f, err := s.k8sclient.GetMetricsCmdFromPod(ctx, s.restConfig, dmePod)
		if err != nil {
			log.Printf("Op040: waiting for DME metrics endpoint: %v", err)
			return false
		}
		fields = f
		return len(fields) > 0
	}, 90*time.Second, 5*time.Second, "DME metrics endpoint did not become ready within 90s")

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}
	for _, m := range []string{"gpu_nodes_total", "gpu_health", "gpu_total_vram", "gpu_used_vram"} {
		assert.True(c, fieldSet[m], "required metric %q not found in DME output", m)
	}
	hasPower := fieldSet["gpu_package_power"] || fieldSet["gpu_average_package_power"]
	assert.True(c, hasPower, "no power metric found (expected gpu_package_power or gpu_average_package_power)")
	hasTemp := fieldSet["gpu_junction_temperature"] || fieldSet["gpu_edge_temperature"]
	assert.True(c, hasTemp, "no temperature metric found (expected gpu_junction_temperature or gpu_edge_temperature)")
	log.Printf("Op040: DME returned %d metric families; all required metrics present", len(fields))
}

// TestOp050VerifyGPUHealth asserts all GPUs reported by DME have gpu_health=1.
func (s *E2ESuite) TestOp050VerifyGPUHealth(c *C) {
	ctx := context.Background()

	pods, err := s.k8sclient.GetPodsByLabel(ctx, s.ns, map[string]string{
		"app.kubernetes.io/name": "metrics-exporter",
	})
	assert.NoError(c, err)
	assert.True(c, len(pods) > 0, "no metrics-exporter pods found")

	var dmePod *corev1.Pod
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			dmePod = &pods[i]
			break
		}
	}
	if dmePod == nil {
		assert.Fail(c, "no Running metrics-exporter pod found")
		return
	}

	var output string
	assert.Eventually(c, func() bool {
		var err error
		output, err = s.k8sclient.ExecCmdOnPod(ctx, s.restConfig, dmePod, "",
			fmt.Sprintf("curl -s localhost:%d/metrics", dmeMetricsPort))
		if err != nil {
			log.Printf("Op050: waiting for DME metrics: %v", err)
			return false
		}
		return strings.Contains(output, "gpu_health")
	}, 90*time.Second, 5*time.Second, "DME metrics endpoint did not return gpu_health within 90s")

	healthy, unhealthy := 0, 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "gpu_health{") {
			if strings.HasSuffix(strings.TrimSpace(line), " 1") {
				healthy++
			} else {
				unhealthy++
				log.Printf("Op050: unhealthy GPU: %s", line)
			}
		}
	}
	log.Printf("Op050: gpu_health — healthy=%d unhealthy=%d", healthy, unhealthy)
	assert.True(c, healthy > 0, "no healthy GPUs found in DME metrics")
	assert.Equal(c, 0, unhealthy, "found %d unhealthy GPU(s)", unhealthy)
}

// TestOp060VerifyCoreMetrics checks VRAM, PCIe, ECC, clock, power and temperature
// metric categories in the DME output.
func (s *E2ESuite) TestOp060VerifyCoreMetrics(c *C) {
	ctx := context.Background()

	pods, err := s.k8sclient.GetPodsByLabel(ctx, s.ns, map[string]string{
		"app.kubernetes.io/name": "metrics-exporter",
	})
	assert.NoError(c, err)
	assert.True(c, len(pods) > 0)

	var dmePod *corev1.Pod
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			dmePod = &pods[i]
			break
		}
	}
	if dmePod == nil {
		assert.Fail(c, "no Running metrics-exporter pod found")
		return
	}

	var fields []string
	assert.Eventually(c, func() bool {
		_, f, err := s.k8sclient.GetMetricsCmdFromPod(ctx, s.restConfig, dmePod)
		if err != nil {
			log.Printf("Op060: waiting for DME metrics: %v", err)
			return false
		}
		fields = f
		return len(fields) > 0
	}, 90*time.Second, 5*time.Second, "DME metrics endpoint did not respond within 90s")

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	categories := map[string][]string{
		"vram":  {"gpu_total_vram", "gpu_used_vram", "gpu_free_vram"},
		"pcie":  {"pcie_speed", "pcie_max_speed"},
		"ecc":   {"gpu_ecc_correct_total", "gpu_ecc_uncorrect_total"},
		"clock": {"gpu_clock"},
	}
	for category, metrics := range categories {
		for _, m := range metrics {
			assert.True(c, fieldSet[m], "category %q: metric %q not found", category, m)
		}
		log.Printf("Op060: category %q OK", category)
	}
	hasPower := fieldSet["gpu_package_power"] || fieldSet["gpu_average_package_power"]
	assert.True(c, hasPower, "power: no power metric (expected gpu_package_power or gpu_average_package_power)")
	log.Printf("Op060: category %q OK", "power")
	hasTemp := fieldSet["gpu_junction_temperature"] || fieldSet["gpu_edge_temperature"]
	assert.True(c, hasTemp, "temperature: no temperature metric (expected gpu_junction_temperature or gpu_edge_temperature)")
	log.Printf("Op060: category %q OK", "temperature")
	log.Printf("Op060: %d total metric families verified", len(fields))
}

// TestOp065VerifyPartitionedGPUMetrics validates partition labels and per-partition
// metrics when partitioned GPUs are detected (SPX/DPX/TPX/QPX/CPX).
// Automatically skipped on non-partitioned clusters (e.g. Radeon cards).
func (s *E2ESuite) TestOp065VerifyPartitionedGPUMetrics(c *C) {
	ctx := context.Background()

	pods, err := s.k8sclient.GetPodsByLabel(ctx, s.ns, map[string]string{
		"app.kubernetes.io/name": "metrics-exporter",
	})
	assert.NoError(c, err)
	assert.True(c, len(pods) > 0, "no metrics-exporter pods found")

	var dmePod *corev1.Pod
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			dmePod = &pods[i]
			break
		}
	}
	if dmePod == nil {
		assert.Fail(c, "no Running metrics-exporter pod found")
		return
	}

	var output string
	assert.Eventually(c, func() bool {
		var err error
		output, err = s.k8sclient.ExecCmdOnPod(ctx, s.restConfig, dmePod, "",
			fmt.Sprintf("curl -s localhost:%d/metrics", dmeMetricsPort))
		if err != nil {
			log.Printf("Op065: waiting for DME metrics: %v", err)
			return false
		}
		return len(output) > 0
	}, 90*time.Second, 5*time.Second, "DME metrics endpoint did not respond within 90s")

	validComputePartitionTypes := map[string]bool{
		"SPX": true, "DPX": true, "TPX": true, "QPX": true, "CPX": true,
	}

	type partitionKey struct {
		gpuID, partitionID, computePartition, memoryPartition string
	}
	partitionedGPUs := map[partitionKey]bool{}
	perPartitionMetrics := map[string]map[string]bool{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		lbStart := strings.Index(line, "{")
		lbEnd := strings.Index(line, "}")
		if lbStart < 0 || lbEnd < 0 || lbEnd <= lbStart {
			continue
		}
		metricName := line[:lbStart]
		lbls := map[string]string{}
		for _, part := range strings.Split(line[lbStart+1:lbEnd], ",") {
			part = strings.TrimSpace(part)
			eqIdx := strings.Index(part, "=")
			if eqIdx < 0 {
				continue
			}
			lbls[strings.TrimSpace(part[:eqIdx])] = strings.Trim(strings.TrimSpace(part[eqIdx+1:]), `"`)
		}

		partID := lbls["gpu_partition_id"]
		computeType := strings.ToUpper(lbls["gpu_compute_partition_type"])
		if partID == "" || computeType == "" || computeType == "NONE" {
			continue
		}
		pk := partitionKey{
			gpuID:            lbls["gpu_id"],
			partitionID:      partID,
			computePartition: computeType,
			memoryPartition:  strings.ToUpper(lbls["gpu_memory_partition_type"]),
		}
		partitionedGPUs[pk] = true
		for _, pm := range []string{"gpu_gfx_busy_instantaneous", "gpu_total_vram", "gpu_used_vram"} {
			if metricName == pm {
				if perPartitionMetrics[pm] == nil {
					perPartitionMetrics[pm] = map[string]bool{}
				}
				perPartitionMetrics[pm][partID] = true
			}
		}
	}

	if len(partitionedGPUs) == 0 {
		log.Print("Op065: no partitioned GPUs detected — skipping partition-specific validation")
		c.Skip("no partitioned GPUs detected (gpu_compute_partition_type=none for all GPUs)")
		return
	}
	log.Printf("Op065: detected %d partitioned GPU instance(s)", len(partitionedGPUs))

	allPartitionIDs := map[string]bool{}
	for pk := range partitionedGPUs {
		log.Printf("Op065: GPU %s partition_id=%s compute_type=%s memory_type=%s",
			pk.gpuID, pk.partitionID, pk.computePartition, pk.memoryPartition)
		assert.True(c, isNonNegativeInt(pk.partitionID),
			"GPU %s: gpu_partition_id %q is not a non-negative integer", pk.gpuID, pk.partitionID)
		assert.True(c, validComputePartitionTypes[pk.computePartition],
			"GPU %s partition %s: unexpected compute partition type %q", pk.gpuID, pk.partitionID, pk.computePartition)
		assert.True(c, pk.memoryPartition != "" && pk.memoryPartition != "NONE",
			"GPU %s partition %s: gpu_memory_partition_type is %q (expected non-empty, non-NONE)",
			pk.gpuID, pk.partitionID, pk.memoryPartition)
		allPartitionIDs[pk.partitionID] = true
	}

	for _, metricName := range []string{"gpu_gfx_busy_instantaneous", "gpu_total_vram", "gpu_used_vram"} {
		for pid := range allPartitionIDs {
			present := perPartitionMetrics[metricName] != nil && perPartitionMetrics[metricName][pid]
			assert.True(c, present, "per-partition metric %q not found for partition_id=%s", metricName, pid)
		}
		log.Printf("Op065: per-partition metric %q present for all %d partition(s)", metricName, len(allPartitionIDs))
	}
	log.Printf("Op065: partition validation passed for %d GPU partition(s)", len(partitionedGPUs))
}

// ---- Op070: GPU workload ------------------------------------------------

// TestOp070ScheduleGPUWorkload submits a GPU workload pod (rocminfo + amd-smi) requesting
// amd.com/gpu:1 and asserts it completes successfully, verifying GPU scheduling works end-to-end.
func (s *E2ESuite) TestOp070ScheduleGPUWorkload(c *C) {
	ctx := context.Background()

	gpuNodes, err := s.k8sclient.GetNodesByLabel(ctx, map[string]string{
		"feature.node.kubernetes.io/amd-gpu": "true",
	})
	assert.NoError(c, err)
	assert.True(c, len(gpuNodes) > 0, "no GPU nodes available for workload scheduling")

	log.Printf("Op070: submitting GPU workload pod %s in namespace %s", gpuWorkloadPodName, s.ns)
	_ = s.k8sclient.DeletePod(ctx, s.ns, gpuWorkloadPodName)
	_ = s.k8sclient.WaitForPodDeleted(ctx, s.ns, gpuWorkloadPodName, 30*time.Second)

	// Use a lightweight ROCm image rather than a full PyTorch image.
	// rocm/rocm-terminal has rocminfo + amd-smi pre-installed and is ~2 GB vs ~40 GB for
	// full pytorch images. On k3s Docker-mode clusters /dev/kfd is not automatically
	// passed by the device plugin, so we mount it via a hostPath volume and run
	// privileged to ensure the GPU is accessible.
	kfdHostPath := corev1.HostPathType("")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpuWorkloadPodName,
			Namespace: s.ns,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				{
					Name: "kfd",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/dev/kfd",
							Type: &kfdHostPath,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "rocm-workload",
					Image:   *workloadImage,
					Command: []string{"/bin/bash", "-c"},
					Args: []string{
						"rocminfo | grep -E 'Name|gfx' | head -10 && " +
							"amd-smi list && " +
							"echo 'ROCm available: True' && " +
							"echo 'DONE'",
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: func() *bool { b := true; return &b }(),
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "kfd", MountPath: "/dev/kfd"},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"amd.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{Operator: corev1.TolerationOpExists},
			},
		},
	}

	err = s.k8sclient.CreatePod(ctx, s.ns, pod)
	assert.NoError(c, err, "failed to create GPU workload pod")

	log.Print("Op070: waiting for GPU workload pod to complete (timeout=15m)")
	err = s.k8sclient.WaitForPodSucceeded(ctx, s.ns, gpuWorkloadPodName, 15*time.Minute)
	assert.NoError(c, err, "GPU workload pod did not succeed")

	logs, logErr := s.k8sclient.GetPodLogs(ctx, s.ns, gpuWorkloadPodName)
	if logErr == nil {
		log.Printf("Op070: workload logs:\n%s", logs)
		assert.True(c, strings.Contains(logs, "DONE"), "expected 'DONE' in workload output")
		assert.True(c, strings.Contains(logs, "ROCm available: True"), "expected ROCm to be available")
	}
	_ = s.k8sclient.DeletePod(ctx, s.ns, gpuWorkloadPodName)
}

// ---- Op900: teardown ----------------------------------------------------

// TestOp900TearDownOperator uninstalls the GPU Operator and cert-manager.
func (s *E2ESuite) TestOp900TearDownOperator(c *C) {
	if s.existingDeploy {
		c.Skip("skipping teardown: existing deploy mode")
	}
	log.Print("Op900: uninstalling GPU Operator")
	if err := s.helmClient.UninstallChartByName(operatorReleaseName); err != nil {
		log.Printf("Op900: warning — GPU Operator uninstall: %v", err)
	}
	log.Print("Op900: uninstalling cert-manager")
	if err := s.helmClient.UninstallChartByName(certManagerReleaseName); err != nil {
		log.Printf("Op900: warning — cert-manager uninstall: %v", err)
	}
	log.Print("Op900: teardown complete")
}

// ---- helpers ------------------------------------------------------------

// isNonNegativeInt returns true if s is a string representation of a non-negative integer.
func isNonNegativeInt(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
