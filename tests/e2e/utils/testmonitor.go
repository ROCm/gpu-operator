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

package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stern/stern/stern"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	yaml "sigs.k8s.io/yaml"
)

const (
	defaultSnapshotInterval = 30 * time.Second
	diagSeparator           = "@@TESTMON_DIAG_SEPARATOR@@"
	diagPodPrefix           = "testmon-diag-"
	gpuNodeLabel            = "feature.node.kubernetes.io/amd-gpu=true"
)

var deviceConfigGVR = schema.GroupVersionResource{
	Group:    "amd.com",
	Version:  "v1alpha1",
	Resource: "deviceconfigs",
}

// TestMonitor observes cluster state during an e2e test. It supports
// independent modules that can be enabled/disabled via functional options:
//
//   - Log collection: lists existing pods then watches for new ones,
//     streaming all container logs (init + regular) to a single chronological
//     file with pod/container prefixes. Uses SinceTime to scope logs from
//     long-running pods to the current test. Handles container restarts by
//     re-following when a stream ends.
//   - Snapshots: periodically dumps resource state (pods, daemonsets,
//     deployments, events) to timestamped files.
//
// Create one TestMonitor per namespace. If you need to watch multiple
// namespaces, create multiple instances.
//
// Usage:
//
//	// Both modules:
//	mon := NewTestMonitor(cs, "kube-amd-gpu", "e2e-artifacts",
//	    WithLogCollection(),
//	    WithSnapshots(),
//	)
//
//	// Only snapshots:
//	mon := NewTestMonitor(cs, "kube-amd-gpu", "e2e-artifacts",
//	    WithSnapshots(),
//	)
//
//	mon.Start("E2ESuite.TestDeployment")
//	// ... test runs ...
//	mon.Stop()
type TestMonitor struct {
	clientSet     kubernetes.Interface
	dynamicClient dynamic.Interface
	namespace     string
	baseDir       string

	// Module flags
	logCollectionEnabled bool
	snapshotsEnabled     bool
	snapshotInterval     time.Duration
	nodeDiagEnabled      bool
	diagImage            string
	nodeDiagSelector     string

	// per-test state (set on Start, cleared on Stop)
	mu        sync.Mutex
	testDir   string
	startTime metav1.Time
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Option configures a TestMonitor.
type Option func(*TestMonitor)

// WithLogCollection enables the log collection module. Pod logs (init +
// regular containers) are streamed to files under <testDir>/logs/.
func WithLogCollection() Option {
	return func(tm *TestMonitor) {
		tm.logCollectionEnabled = true
	}
}

// WithSnapshots enables the periodic resource snapshot module. Resource
// state is dumped every snapshotInterval (default 30s) to <testDir>/snapshots/.
func WithSnapshots() Option {
	return func(tm *TestMonitor) {
		tm.snapshotsEnabled = true
	}
}

// WithSnapshotInterval enables snapshots and sets a custom interval.
func WithSnapshotInterval(d time.Duration) Option {
	return func(tm *TestMonitor) {
		tm.snapshotsEnabled = true
		tm.snapshotInterval = d
	}
}

// WithNodeDiagnostics enables collection of dmesg and lsmod from GPU worker
// nodes (those labelled feature.node.kubernetes.io/amd-gpu=true) at the end
// of each test. Diagnostics are saved under
// <testDir>/node-diagnostics/<nodeName>/.
// The container image defaults to the E2E_NODE_DIAG_IMAGE env var
// (set via dev.env / Makefile), falling back to busybox:1.36.
func WithNodeDiagnostics() Option {
	return func(tm *TestMonitor) {
		tm.nodeDiagEnabled = true
		if tm.nodeDiagSelector == "" {
			tm.nodeDiagSelector = gpuNodeLabel
		}
		if tm.diagImage == "" {
			if img := os.Getenv("E2E_NODE_DIAG_IMAGE"); img != "" {
				tm.diagImage = img
			} else {
				tm.diagImage = "busybox:1.36"
			}
		}
	}
}

// WithNodeDiagnosticsImage enables node diagnostics with a custom container
// image (must have nsenter available).
func WithNodeDiagnosticsImage(image string) Option {
	return func(tm *TestMonitor) {
		tm.nodeDiagEnabled = true
		tm.diagImage = image
	}
}

// WithNodeDiagnosticsSelector overrides the default node label selector
// used to pick which nodes to collect diagnostics from.
// Default: "feature.node.kubernetes.io/amd-gpu=true".
func WithNodeDiagnosticsSelector(selector string) Option {
	return func(tm *TestMonitor) {
		tm.nodeDiagSelector = selector
	}
}

// WithDynamicClient sets a dynamic Kubernetes client on the TestMonitor,
// enabling snapshots of custom resources (e.g. DeviceConfig CRs).
func WithDynamicClient(dc dynamic.Interface) Option {
	return func(tm *TestMonitor) {
		tm.dynamicClient = dc
	}
}

// NewTestMonitor creates a new TestMonitor for a single namespace.
// Pass one or more Option values to enable modules. If no options are
// passed, nothing is collected (the monitor is inert).
func NewTestMonitor(clientSet kubernetes.Interface, namespace string, baseDir string, opts ...Option) *TestMonitor {
	tm := &TestMonitor{
		clientSet:        clientSet,
		namespace:        namespace,
		baseDir:          baseDir,
		snapshotInterval: defaultSnapshotInterval,
	}
	for _, opt := range opts {
		opt(tm)
	}
	return tm
}

// Start begins observation for a test. It records the current time so that
// logs from long-running pods (like the operator controller) are only
// collected from this point forward.
func (tm *TestMonitor) Start(testName string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.logCollectionEnabled && !tm.snapshotsEnabled && !tm.nodeDiagEnabled {
		log.Infof("[TestMonitor] No modules enabled, skipping for %s", testName)
		return
	}

	safeName := sanitizeTestName(testName)
	tm.testDir = filepath.Join(tm.baseDir, safeName, tm.namespace)
	tm.startTime = metav1.Now()
	tm.ctx, tm.cancel = context.WithCancel(context.Background())

	log.Infof("[TestMonitor] Starting for test %q ns=%s (logs=%t snapshots=%t diag=%t) -> %s",
		testName, tm.namespace, tm.logCollectionEnabled, tm.snapshotsEnabled, tm.nodeDiagEnabled, tm.testDir)

	if tm.logCollectionEnabled {
		if err := os.MkdirAll(filepath.Join(tm.testDir, "logs"), 0755); err != nil {
			log.Warnf("[TestMonitor] Failed to create logs directory: %v", err)
		} else {
			tm.wg.Add(1)
			go tm.runStern()
		}
	}

	if tm.snapshotsEnabled {
		if err := os.MkdirAll(filepath.Join(tm.testDir, "snapshots"), 0755); err != nil {
			log.Warnf("[TestMonitor] Failed to create snapshots directory: %v", err)
			return
		}
		tm.takeSnapshot("initial")
		tm.wg.Add(1)
		go tm.periodicSnapshots()
	}
}

// Stop halts all observation goroutines, waits for them to finish, and
// takes a final resource snapshot (if snapshots are enabled).
func (tm *TestMonitor) Stop() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.cancel == nil {
		return
	}

	log.Infof("[TestMonitor] Stopping for %s", tm.testDir)

	if tm.snapshotsEnabled {
		tm.takeSnapshot("final")
	}

	if tm.nodeDiagEnabled {
		tm.collectNodeDiagnostics()
	}

	tm.cancel()
	tm.wg.Wait()

	tm.cancel = nil
	tm.ctx = nil
	log.Infof("[TestMonitor] Stopped. Artifacts in: %s", tm.testDir)
}

// =========================================================================
// Log collection module (stern-based)
// =========================================================================

// runStern starts stern.Run with a Config that mirrors the defaults of the
// stern CLI command: `stern . -n <ns> --all-containers`.
// Key defaults replicated from cmd/cmd.go:
//   - ContainerStates: ALL_STATES (captures running, waiting, and terminated)
//   - InitContainers + EphemeralContainers: true
//   - Since: currennt time + 30s (captures only 30s of recent history for long-running pods)
//   - Follow: true
//   - MaxLogRequests: 50
//   - PodQuery: "." (matches all pods)
//   - ContainerQuery: ".*" (matches all containers)
//   - LabelSelector: everything, FieldSelector: everything
func (tm *TestMonitor) runStern() {
	defer tm.wg.Done()

	logPath := filepath.Join(tm.testDir, "logs", "all-pods.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Warnf("[TestMonitor] Failed to create log file %s: %v", logPath, err)
		return
	}
	defer logFile.Close()

	// Template without color — plain text suitable for file output.
	// Matches the "default" output format structure but without ANSI codes.
	tmpl, err := template.New("log").Parse("{{.PodName}} {{.ContainerName}} {{.Message}}\n")
	if err != nil {
		log.Warnf("[TestMonitor] Failed to parse log template: %v", err)
		return
	}

	cfg := &stern.Config{
		// Namespace scoping
		Namespaces:    []string{tm.namespace},
		AllNamespaces: false,

		// Match all pods and containers (equivalent to `stern . --all-containers`)
		PodQuery:       regexp.MustCompile("."),
		ContainerQuery: regexp.MustCompile(".*"),

		// Include all container types
		InitContainers:      true,
		EphemeralContainers: true,

		// Include containers in all states — this is critical for
		// capturing logs from existing running pods (not just new ones).
		// CLI default: []string{"all"} → []ContainerState{ALL_STATES}
		ContainerStates: []stern.ContainerState{stern.ALL_STATES},

		// Location is required by stern's timestamp formatting (Time.In panics on nil).
		// CLI sets this via time.LoadLocation(timezone); default is UTC.
		Location: time.UTC,

		// Time scope — only capture logs relevant to this test.
		// Compute duration from test start to now (nearly zero) plus a
		// small buffer so stern doesn't miss logs emitted between Start()
		// and the moment each container tail is established.
		Since: time.Since(tm.startTime.Time) + 30*time.Second,

		// Streaming behaviour
		Follow:         true,
		MaxLogRequests: 50,
		TailLines:      nil, // no line limit; Since duration governs scope

		// No filtering
		LabelSelector: labels.Everything(),
		FieldSelector: fields.Everything(),

		// Output
		Timestamps: true,
		Template:   tmpl,
		Out:        logFile,
		ErrOut:     logFile,
	}

	log.Infof("[TestMonitor] stern starting for ns=%s", tm.namespace)
	if err := stern.Run(tm.ctx, tm.clientSet, cfg); err != nil {
		if tm.ctx.Err() == nil {
			log.Warnf("[TestMonitor] stern exited with error: %v", err)
		}
	}
	log.Infof("[TestMonitor] stern stopped for ns=%s", tm.namespace)
}

// =========================================================================
// Snapshot module
// =========================================================================

func (tm *TestMonitor) periodicSnapshots() {
	defer tm.wg.Done()

	ticker := time.NewTicker(tm.snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ts := time.Now().Format("15-04-05")
			tm.takeSnapshot(ts)
		case <-tm.ctx.Done():
			return
		}
	}
}

func (tm *TestMonitor) takeSnapshot(label string) {
	snapFile := filepath.Join(tm.testDir, "snapshots", fmt.Sprintf("%s.txt", label))
	f, err := os.Create(snapFile)
	if err != nil {
		log.Warnf("[TestMonitor] Failed to create snapshot file %s: %v", snapFile, err)
		return
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Fprintf(f, "=== Resource Snapshot: %s at %s (ns: %s) ===\n\n",
		label, time.Now().Format(time.RFC3339), tm.namespace)

	// Pods
	fmt.Fprintf(f, ">> Pods:\n")
	pods, err := tm.clientSet.CoreV1().Pods(tm.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(f, "  Error listing pods: %v\n", err)
	} else {
		fmt.Fprintf(f, "  %-60s %-12s %-8s %-30s %s\n", "NAME", "STATUS", "READY", "NODE", "CONTAINERS")
		for _, pod := range pods.Items {
			ready := podReadyCount(&pod)
			total := len(pod.Spec.Containers)
			containers := containerStatusSummary(&pod)
			fmt.Fprintf(f, "  %-60s %-12s %d/%-6d %-30s %s\n",
				pod.Name, string(pod.Status.Phase), ready, total, pod.Spec.NodeName, containers)
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				continue
			}
			if len(pod.Status.InitContainerStatuses) > 0 {
				fmt.Fprintf(f, "\n  Init containers for %s:\n", pod.Name)
				for _, ics := range pod.Status.InitContainerStatuses {
					state := containerStateString(ics.State)
					fmt.Fprintf(f, "    %s: ready=%t restarts=%d state=%s\n",
						ics.Name, ics.Ready, ics.RestartCount, state)
				}
			}
		}
	}

	// DaemonSets
	fmt.Fprintf(f, "\n>> DaemonSets:\n")
	dsList, err := tm.clientSet.AppsV1().DaemonSets(tm.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(f, "  Error listing daemonsets: %v\n", err)
	} else {
		fmt.Fprintf(f, "  %-50s %-10s %-10s %-10s %-10s %-10s\n",
			"NAME", "DESIRED", "CURRENT", "READY", "UP-TO-DATE", "AVAILABLE")
		for _, ds := range dsList.Items {
			fmt.Fprintf(f, "  %-50s %-10d %-10d %-10d %-10d %-10d\n",
				ds.Name, ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled,
				ds.Status.NumberReady, ds.Status.UpdatedNumberScheduled, ds.Status.NumberAvailable)
		}
	}

	// Deployments
	fmt.Fprintf(f, "\n>> Deployments:\n")
	depList, err := tm.clientSet.AppsV1().Deployments(tm.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(f, "  Error listing deployments: %v\n", err)
	} else {
		fmt.Fprintf(f, "  %-50s %-10s %-10s %-10s\n", "NAME", "DESIRED", "READY", "AVAILABLE")
		for _, dep := range depList.Items {
			desired := int32(0)
			if dep.Spec.Replicas != nil {
				desired = *dep.Spec.Replicas
			}
			fmt.Fprintf(f, "  %-50s %-10d %-10d %-10d\n",
				dep.Name, desired, dep.Status.ReadyReplicas, dep.Status.AvailableReplicas)
		}
	}

	// Events (only since test started)
	fmt.Fprintf(f, "\n>> Events (since test start):\n")
	events, err := tm.clientSet.CoreV1().Events(tm.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(f, "  Error listing events: %v\n", err)
	} else {
		count := 0
		for _, ev := range events.Items {
			evTime := ev.LastTimestamp.Time
			if evTime.IsZero() {
				evTime = ev.EventTime.Time
			}
			if evTime.Before(tm.startTime.Time) {
				continue
			}
			fmt.Fprintf(f, "  %s  %-8s %-40s %s\n",
				evTime.Format(time.RFC3339), ev.Type,
				fmt.Sprintf("%s/%s", ev.InvolvedObject.Kind, ev.InvolvedObject.Name),
				ev.Message)
			count++
		}
		if count == 0 {
			fmt.Fprintf(f, "  (no events since test start)\n")
		}
	}

	// DeviceConfig CRs
	fmt.Fprintf(f, "\n>> DeviceConfigs:\n")
	if tm.dynamicClient != nil {
		dcList, err := tm.dynamicClient.Resource(deviceConfigGVR).Namespace(tm.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(f, "  Error listing deviceconfigs: %v\n", err)
		} else if len(dcList.Items) == 0 {
			fmt.Fprintf(f, "  (none)\n")
		} else {
			for _, item := range dcList.Items {
				yamlBytes, err := toYAML(item.Object)
				if err != nil {
					fmt.Fprintf(f, "  Error marshalling %s: %v\n", item.GetName(), err)
					continue
				}
				fmt.Fprintf(f, "---\n%s\n", string(yamlBytes))
			}
		}
	} else {
		fmt.Fprintf(f, "  (dynamic client not configured)\n")
	}

	fmt.Fprintf(f, "\n")
}

// =========================================================================
// Node diagnostics module
// =========================================================================

// collectNodeDiagnostics creates a short-lived privileged pod on every node
// to capture dmesg and lsmod output, saving them under
// <testDir>/node-diagnostics/<nodeName>/.
func (tm *TestMonitor) collectNodeDiagnostics() {
	diagDir := filepath.Join(tm.testDir, "node-diagnostics")
	if err := os.MkdirAll(diagDir, 0755); err != nil {
		log.Warnf("[TestMonitor] Failed to create diagnostics dir: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	listOpts := metav1.ListOptions{}
	if tm.nodeDiagSelector != "" {
		listOpts.LabelSelector = tm.nodeDiagSelector
	}
	nodes, err := tm.clientSet.CoreV1().Nodes().List(ctx, listOpts)
	if err != nil {
		log.Warnf("[TestMonitor] Failed to list nodes for diagnostics: %v", err)
		return
	}

	log.Infof("[TestMonitor] Collecting node diagnostics from %d node(s) (selector=%q)", len(nodes.Items), tm.nodeDiagSelector)

	var wg sync.WaitGroup
	for i := range nodes.Items {
		node := &nodes.Items[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.collectFromNode(ctx, diagDir, node.Name)
		}()
	}
	wg.Wait()
	log.Infof("[TestMonitor] Node diagnostics collection complete")
}

// collectFromNode creates a privileged pod on the given node, runs dmesg and
// lsmod via nsenter, captures the output into separate files, and cleans up.
func (tm *TestMonitor) collectFromNode(ctx context.Context, diagDir string, nodeName string) {
	nodeDir := filepath.Join(diagDir, sanitizeNodeName(nodeName))
	if err := os.MkdirAll(nodeDir, 0755); err != nil {
		log.Warnf("[TestMonitor] Failed to create node dir for %s: %v", nodeName, err)
		return
	}

	podName := diagPodPrefix + sanitizeNodeName(nodeName)
	if len(podName) > 63 {
		podName = podName[:63]
	}
	podName = strings.TrimRight(podName, "-")

	privileged := true
	var zero int64
	cmd := fmt.Sprintf("dmesg -T 2>/dev/null || dmesg; echo '%s'; lsmod", diagSeparator)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: tm.namespace,
		},
		Spec: corev1.PodSpec{
			NodeName:                      nodeName,
			HostPID:                       true,
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &zero,
			Containers: []corev1.Container{
				{
					Name:    "diag",
					Image:   tm.diagImage,
					Command: []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "--", "sh", "-c", cmd},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{Operator: corev1.TolerationOpExists},
			},
		},
	}

	// Clean up any leftover pod from a previous run
	_ = tm.clientSet.CoreV1().Pods(tm.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	time.Sleep(2 * time.Second)

	if _, err := tm.clientSet.CoreV1().Pods(tm.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		log.Warnf("[TestMonitor] Failed to create diag pod on node %s: %v", nodeName, err)
		return
	}
	defer func() {
		_ = tm.clientSet.CoreV1().Pods(tm.namespace).Delete(
			context.Background(), podName, metav1.DeleteOptions{})
	}()

	if err := tm.waitForPodDone(ctx, podName); err != nil {
		log.Warnf("[TestMonitor] Diag pod on node %s did not complete: %v", nodeName, err)
		return
	}

	// Fetch pod logs
	stream, err := tm.clientSet.CoreV1().Pods(tm.namespace).
		GetLogs(podName, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		log.Warnf("[TestMonitor] Failed to get diag logs from node %s: %v", nodeName, err)
		return
	}
	defer stream.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, stream); err != nil {
		log.Warnf("[TestMonitor] Failed to read diag logs from node %s: %v", nodeName, err)
		return
	}

	parts := strings.SplitN(buf.String(), diagSeparator, 2)

	dmesgData := ""
	lsmodData := ""
	if len(parts) >= 1 {
		dmesgData = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		lsmodData = strings.TrimSpace(parts[1])
	}

	if err := os.WriteFile(filepath.Join(nodeDir, "dmesg.log"), []byte(dmesgData+"\n"), 0644); err != nil {
		log.Warnf("[TestMonitor] Failed to write dmesg for node %s: %v", nodeName, err)
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "lsmod.log"), []byte(lsmodData+"\n"), 0644); err != nil {
		log.Warnf("[TestMonitor] Failed to write lsmod for node %s: %v", nodeName, err)
	}

	log.Infof("[TestMonitor] Collected diagnostics from node %s", nodeName)
}

// waitForPodDone polls until the pod reaches Succeeded or Failed phase.
func (tm *TestMonitor) waitForPodDone(ctx context.Context, podName string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pod, err := tm.clientSet.CoreV1().Pods(tm.namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get pod: %w", err)
		}

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			return fmt.Errorf("pod failed")
		}

		time.Sleep(2 * time.Second)
	}
}

// sanitizeNodeName produces a lowercase string safe for Kubernetes pod names.
func sanitizeNodeName(name string) string {
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name
}

// =========================================================================
// Helpers
// =========================================================================

func sanitizeTestName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		" ", "_",
		":", "_",
		"\\", "_",
	)
	return replacer.Replace(name)
}

func podReadyCount(pod *corev1.Pod) int {
	count := 0
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			count++
		}
	}
	return count
}

func containerStatusSummary(pod *corev1.Pod) string {
	var parts []string
	for _, cs := range pod.Status.ContainerStatuses {
		state := "unknown"
		if cs.State.Running != nil {
			state = "running"
		} else if cs.State.Waiting != nil {
			state = fmt.Sprintf("waiting:%s", cs.State.Waiting.Reason)
		} else if cs.State.Terminated != nil {
			state = fmt.Sprintf("terminated:%s", cs.State.Terminated.Reason)
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", cs.Name, state))
	}
	return strings.Join(parts, ", ")
}

func containerStateString(state corev1.ContainerState) string {
	if state.Running != nil {
		return fmt.Sprintf("running(since %s)", state.Running.StartedAt.Format(time.RFC3339))
	}
	if state.Waiting != nil {
		return fmt.Sprintf("waiting(%s: %s)", state.Waiting.Reason, state.Waiting.Message)
	}
	if state.Terminated != nil {
		return fmt.Sprintf("terminated(%s exit=%d)", state.Terminated.Reason, state.Terminated.ExitCode)
	}
	return "unknown"
}

// toYAML converts an object to YAML via JSON marshalling (handles
// unstructured maps cleanly).
func toYAML(obj interface{}) ([]byte, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(jsonBytes)
}
