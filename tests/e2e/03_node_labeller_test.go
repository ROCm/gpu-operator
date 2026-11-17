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
	"encoding/json"
	"fmt"
	"time"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	operatorutils "github.com/ROCm/gpu-operator/internal"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

// These tests exercise the deployed operator against real Node objects and API
// watches. Synthetic, unschedulable nodes isolate the cleanup contract from NFD,
// kubelet, GPU hardware, and the labeller binary. The test supplies the labels a
// labeller would write; it does not claim to test GPU discovery in that binary.
// This file is automatically included in the operator-e2e-sim CI chunk catalog.
// Run after driver-install chunks but before slower feature suites, so their
// timeouts do not prevent these cleanup regressions from being exercised.
func (s *E2ESuite) TestNodeLabellerCustomSelectorPreservesLabels(c *C) {
	s.verifyNodeLabellerCleanup(c, "", false)
}

func (s *E2ESuite) TestNodeLabellerGPUSelectorCleansStaleLabels(c *C) {
	s.verifyNodeLabellerCleanup(c, operatorutils.NodeFeatureLabelAmdGpu, false)
}

func (s *E2ESuite) TestNodeLabellerVGPUSelectorCleansStaleLabels(c *C) {
	s.verifyNodeLabellerCleanup(c, operatorutils.NodeFeatureLabelAmdVGpu, false)
}

// Use two disjoint selectors in different namespaces. The NFD config's stale
// query still reaches the custom config's node because it removes the NFD key.
func (s *E2ESuite) TestNodeLabellerGPUConfigPreservesOtherConfigLabels(c *C) {
	s.verifyNodeLabellerCleanup(c, operatorutils.NodeFeatureLabelAmdGpu, true)
}

func (s *E2ESuite) TestNodeLabellerVGPUConfigPreservesOtherConfigLabels(c *C) {
	s.verifyNodeLabellerCleanup(c, operatorutils.NodeFeatureLabelAmdVGpu, true)
}

func (s *E2ESuite) verifyNodeLabellerCleanup(c *C, discoveryKey string, withCustomConfig bool) {
	ctx := context.Background()
	const poolKey = "e2e.amd.com/labeller-cleanup"
	const customKey = "e2e.amd.com/custom-selected"
	name := fmt.Sprintf("labeller-cleanup-%d", time.Now().UnixNano())
	selector := map[string]string{poolKey: name}
	if discoveryKey != "" {
		selector[discoveryKey] = "true"
	}
	cfg := &v1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.ns}, Spec: v1alpha1.DeviceConfigSpec{
		Selector:        selector,
		Driver:          v1alpha1.DriverSpec{Enable: ptr.To(false)},
		DevicePlugin:    v1alpha1.DevicePluginSpec{EnableDevicePlugin: ptr.To(false), EnableNodeLabeller: ptr.To(true), NodeLabellerArguments: []string{"device-id"}},
		MetricsExporter: v1alpha1.MetricsExporterSpec{Enable: ptr.To(false)},
	}}
	fixtures := []struct {
		name      string
		discovery map[string]string
		outside   bool
	}{
		{name: "missing"},
		{name: "stale"},
		{name: "gpu", discovery: map[string]string{operatorutils.NodeFeatureLabelAmdGpu: "true"}},
		{name: "vgpu", discovery: map[string]string{operatorutils.NodeFeatureLabelAmdVGpu: "true"}},
		{name: "both", discovery: map[string]string{operatorutils.NodeFeatureLabelAmdGpu: "true", operatorutils.NodeFeatureLabelAmdVGpu: "true"}},
		{name: "outside", outside: true},
	}
	nodes := []*v1.Node{}
	configs := []*v1alpha1.DeviceConfig{cfg}
	var customConfig *v1alpha1.DeviceConfig
	defer func() {
		// Delete both configs before their nodes so their finalizers can finish.
		for _, config := range configs {
			_, err := s.dClient.DeviceConfigs(config.Namespace).Delete(config.Name)
			if !apierrors.IsNotFound(err) {
				c.Check(err, IsNil)
			}
			assert.Eventually(c, func() bool {
				_, err := s.dClient.DeviceConfigs(config.Namespace).Get(config.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, time.Minute, time.Second, "DeviceConfig cleanup did not finish")
		}
		if customConfig != nil {
			err := s.clientSet.CoreV1().Namespaces().Delete(ctx, customConfig.Namespace, metav1.DeleteOptions{})
			if !apierrors.IsNotFound(err) {
				c.Check(err, IsNil)
			}
		}
		for _, node := range nodes {
			err := s.clientSet.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
			if !apierrors.IsNotFound(err) {
				c.Check(err, IsNil)
			}
		}
	}()
	for _, fixture := range fixtures {
		node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name + "-" + fixture.name, Labels: map[string]string{poolKey: name}}, Spec: v1.NodeSpec{Unschedulable: true}}
		if fixture.name == "missing" && withCustomConfig {
			node.Labels[customKey] = "true"
		}
		if fixture.outside {
			node.Labels[poolKey] = name + "-outside"
		}
		for key, value := range fixture.discovery {
			node.Labels[key] = value
		}
		created, err := s.clientSet.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		c.Assert(err, IsNil)
		nodes = append(nodes, created)
	}
	cfg, err := s.dClient.DeviceConfigs(s.ns).Create(cfg)
	c.Assert(err, IsNil)
	waitReconciled := func(config *v1alpha1.DeviceConfig, generation int64) {
		c.Assert(assert.Eventually(c, func() bool {
			actual, err := s.dClient.DeviceConfigs(config.Namespace).Get(config.Name, metav1.GetOptions{})
			return err == nil && actual.Status.ObservedGeneration >= generation
		}, time.Minute, time.Second, "operator did not finish reconciling %s generation %d", config.Name, generation), Equals, true)
	}
	waitReconciled(cfg, cfg.Generation)
	if withCustomConfig {
		_, err := s.clientSet.CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		c.Assert(err, IsNil)
		customConfig = &v1alpha1.DeviceConfig{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: name}, Spec: v1alpha1.DeviceConfigSpec{
			Selector:        map[string]string{poolKey: name, customKey: "true"},
			Driver:          v1alpha1.DriverSpec{Enable: ptr.To(false)},
			DevicePlugin:    v1alpha1.DevicePluginSpec{EnableDevicePlugin: ptr.To(false), EnableNodeLabeller: ptr.To(true), NodeLabellerArguments: []string{"device-id"}},
			MetricsExporter: v1alpha1.MetricsExporterSpec{Enable: ptr.To(false)},
		}}
		configs = append(configs, customConfig)
		createdConfig, err := s.dClient.DeviceConfigs(customConfig.Namespace).Create(customConfig)
		c.Assert(err, IsNil)
		customConfig = createdConfig
		waitReconciled(customConfig, customConfig.Generation)
	}
	ds, err := s.clientSet.AppsV1().DaemonSets(s.ns).Get(ctx, name+operatorutils.NodeLabellerNameSuffix, metav1.GetOptions{})
	c.Assert(err, IsNil)
	c.Assert(ds.Spec.Template.Spec.NodeSelector, DeepEquals, selector)

	// Write labels after deployment, then force another full reconciliation.
	// This catches deletion on later reconciles as well as initial deployment.
	ownedLabels := map[string]string{"amd.com/gpu.device-id": "75a3", "beta.amd.com/gpu.family": "AI", "beta.amd.com/gpu.family.AI": "1"}
	probeLabels := map[string]string{"amd.com/gpu.probe": "keep"}
	for key, value := range ownedLabels {
		probeLabels[key] = value
	}
	patch, err := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{"labels": probeLabels}})
	c.Assert(err, IsNil)
	for _, node := range nodes {
		_, err := s.clientSet.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		c.Assert(err, IsNil)
	}
	updateSpec := func(config *v1alpha1.DeviceConfig, mutate func(*v1alpha1.DeviceConfig)) int64 {
		var generation int64
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			actual, err := s.dClient.DeviceConfigs(config.Namespace).Get(config.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mutate(actual)
			updated, err := s.dClient.DeviceConfigs(config.Namespace).Update(actual)
			if err == nil {
				generation = updated.Generation
			}
			return err
		})
		c.Assert(err, IsNil)
		return generation
	}
	updateConfig := func(config *v1alpha1.DeviceConfig, mutate func(*v1alpha1.DeviceConfig)) {
		waitReconciled(config, updateSpec(config, mutate))
	}
	if withCustomConfig {
		updateConfig(customConfig, func(actual *v1alpha1.DeviceConfig) {
			actual.Spec.DevicePlugin.NodeLabellerArguments = []string{"device-id", "family"}
		})
	}
	updateConfig(cfg, func(actual *v1alpha1.DeviceConfig) {
		actual.Spec.DevicePlugin.NodeLabellerArguments = []string{"device-id", "family"}
	})
	checkLabels := func(disabled bool) {
		for i, node := range nodes {
			actual, err := s.clientSet.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
			c.Assert(err, IsNil)
			selected := !fixtures[i].outside && (discoveryKey == "" || fixtures[i].discovery[discoveryKey] == "true")
			stale := fixtures[i].name == "stale" || (fixtures[i].name == "missing" && !withCustomConfig)
			removed := (discoveryKey != "" && stale) || (disabled && selected)
			for key, value := range ownedLabels {
				if removed {
					_, exists := actual.Labels[key]
					c.Assert(exists, Equals, false, Commentf("%s still has %s", node.Name, key))
				} else {
					c.Assert(actual.Labels[key], Equals, value, Commentf("%s lost %s", node.Name, key))
				}
			}
			c.Assert(actual.Labels["amd.com/gpu.probe"], Equals, "keep")
			for key, value := range fixtures[i].discovery {
				c.Assert(actual.Labels[key], Equals, value)
			}
		}
	}
	// Keep checking after the reconcile barrier to catch watch-driven cleanup.
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		checkLabels(false)
		time.Sleep(100 * time.Millisecond)
	}
	if withCustomConfig {
		// Release the protected node using only B's selector update. B now has
		// no matching nodes and exits before ObservedGeneration is updated.
		// A must be enqueued by the DeviceConfig watch, without a Node mutation
		// or an update to A, to clean the newly unowned stale labels.
		updateSpec(customConfig, func(actual *v1alpha1.DeviceConfig) { actual.Spec.Selector[customKey] = "false" })
		c.Assert(assert.Eventually(c, func() bool {
			actual, err := s.clientSet.CoreV1().Nodes().Get(ctx, name+"-missing", metav1.GetOptions{})
			if err != nil {
				return false
			}
			for key := range ownedLabels {
				if _, exists := actual.Labels[key]; exists {
					return false
				}
			}
			return actual.Labels["amd.com/gpu.probe"] == "keep"
		}, time.Minute, time.Second, "selector change did not trigger stale-label cleanup"), Equals, true)

		// Restore ownership and verify that subsequent reconciles preserve
		// newly written labeller labels again.
		updateConfig(customConfig, func(actual *v1alpha1.DeviceConfig) { actual.Spec.Selector[customKey] = "true" })
		_, err := s.clientSet.CoreV1().Nodes().Patch(ctx, name+"-missing", types.MergePatchType, patch, metav1.PatchOptions{})
		c.Assert(err, IsNil)
		updateConfig(cfg, func(actual *v1alpha1.DeviceConfig) {
			actual.Spec.DevicePlugin.NodeLabellerArguments = []string{"device-id"}
		})
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			checkLabels(false)
			time.Sleep(100 * time.Millisecond)
		}
	}
	updateConfig(cfg, func(actual *v1alpha1.DeviceConfig) { actual.Spec.DevicePlugin.EnableNodeLabeller = ptr.To(false) })
	checkLabels(true)
	_, err = s.clientSet.AppsV1().DaemonSets(s.ns).Get(ctx, name+operatorutils.NodeLabellerNameSuffix, metav1.GetOptions{})
	c.Assert(apierrors.IsNotFound(err), Equals, true)
	if withCustomConfig {
		updateConfig(customConfig, func(actual *v1alpha1.DeviceConfig) { actual.Spec.DevicePlugin.EnableNodeLabeller = ptr.To(false) })
		actual, err := s.clientSet.CoreV1().Nodes().Get(ctx, name+"-missing", metav1.GetOptions{})
		c.Assert(err, IsNil)
		for key := range ownedLabels {
			_, exists := actual.Labels[key]
			c.Assert(exists, Equals, false)
		}
		c.Assert(actual.Labels["amd.com/gpu.probe"], Equals, "keep")
	}
}
