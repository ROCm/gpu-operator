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

package controllers

import (
	"context"
	"fmt"
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/conditions"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"github.com/ROCm/gpu-operator/internal/test"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type lifecycleClient struct {
	client.Client
	deviceConfig   *amdv1alpha1.DeviceConfig
	holdPodDeletes bool
	pods           map[types.NamespacedName]*v1.Pod
	nodes          map[string]*v1.Node
	modules        map[types.NamespacedName]*kmmv1beta1.Module
}

func (c *lifecycleClient) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	switch out := obj.(type) {
	case *v1.Pod:
		if pod := c.pods[key]; pod != nil {
			*out = *pod.DeepCopy()
			return nil
		}
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "pods"}, key.Name)
	case *v1.Node:
		if node := c.nodes[key.Name]; node != nil {
			*out = *node.DeepCopy()
			return nil
		}
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "nodes"}, key.Name)
	case *kmmv1beta1.Module:
		if module := c.modules[key]; module != nil {
			*out = *module.DeepCopy()
			return nil
		}
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "modules"}, key.Name)
	case *amdv1alpha1.DeviceConfig:
		*out = *c.deviceConfig.DeepCopy()
		return nil
	}
	return k8serrors.NewNotFound(schema.GroupResource{Resource: "objects"}, key.Name)
}

func (c *lifecycleClient) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	switch out := list.(type) {
	case *v1.PodList:
		for _, pod := range c.pods {
			out.Items = append(out.Items, *pod.DeepCopy())
		}
	case *v1.NodeList:
		for _, node := range c.nodes {
			out.Items = append(out.Items, *node.DeepCopy())
		}
	default:
		return fmt.Errorf("unsupported List object %T", list)
	}
	return nil
}

func (c *lifecycleClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	switch value := obj.(type) {
	case *v1.Pod:
		c.pods[client.ObjectKeyFromObject(value)] = value.DeepCopy()
	case *kmmv1beta1.Module:
		c.modules[client.ObjectKeyFromObject(value)] = value.DeepCopy()
	default:
		return fmt.Errorf("unsupported Create object %T", obj)
	}
	return nil
}

func (c *lifecycleClient) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	switch obj.(type) {
	case *v1.Pod:
		if c.holdPodDeletes {
			pod := c.pods[client.ObjectKeyFromObject(obj)]
			now := metav1.Now()
			pod.DeletionTimestamp = &now
			return nil
		}
		delete(c.pods, client.ObjectKeyFromObject(obj))
		return nil
	}
	return fmt.Errorf("unsupported Delete object %T", obj)
}

func (c *lifecycleClient) Patch(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	switch value := obj.(type) {
	case *v1.Node:
		c.nodes[value.Name] = value.DeepCopy()
	case *v1.Pod:
		c.pods[client.ObjectKeyFromObject(value)] = value.DeepCopy()
	case *amdv1alpha1.DeviceConfig:
		c.deviceConfig = value.DeepCopy()
	default:
		return fmt.Errorf("unsupported Patch object %T", obj)
	}
	return nil
}

func lifecycleFixture(t *testing.T, driverType string, enabled bool, nodes ...*v1.Node) (*deviceConfigReconcilerHelper, *amdv1alpha1.DeviceConfig, *lifecycleClient, *runtime.Scheme) {
	t.Helper()
	scheme, err := test.TestScheme()
	if err != nil {
		t.Fatal(err)
	}
	dc := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "dc", UID: types.UID("dc-uid"), Finalizers: []string{deviceConfigFinalizer}},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Selector: map[string]string{"selected": "true"},
			Driver:   amdv1alpha1.DriverSpec{DriverType: driverType, Enable: &enabled},
		},
	}
	cli := &lifecycleClient{
		deviceConfig: dc,
		pods:         map[types.NamespacedName]*v1.Pod{}, nodes: map[string]*v1.Node{}, modules: map[types.NamespacedName]*kmmv1beta1.Module{},
	}
	for _, node := range nodes {
		cli.nodes[node.Name] = node.DeepCopy()
	}
	return &deviceConfigReconcilerHelper{
		client: cli, kmmWatchEnabled: true, kmmPostProcessor: workermgr.NewWorkerMgr(cli, scheme),
	}, dc, cli, scheme
}

func lifecyclePod(t *testing.T, scheme *runtime.Scheme, dc *amdv1alpha1.DeviceConfig, node string, action string, phase v1.PodPhase) *v1.Pod {
	t.Helper()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: "worker-" + dc.Name + "-" + node,
			Labels: map[string]string{utils.WorkerActionLabelKey: action}},
		Spec:   v1.PodSpec{NodeName: node},
		Status: v1.PodStatus{Phase: phase},
	}
	if err := controllerutil.SetControllerReference(dc, pod, scheme); err != nil {
		t.Fatal(err)
	}
	return pod
}

func workerOnNode(t *testing.T, cli client.Client, namespace, name, node string) *v1.Pod {
	t.Helper()
	pod := &v1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: "worker-" + name + "-" + node}, pod); err != nil {
		t.Fatal(err)
	}
	return pod
}

func TestVFDetectorWaitsForKMMAndRecoversLegacyTimeoutOnce(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"selected": "true"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true, node)
	nodes := &v1.NodeList{Items: []v1.Node{*node}}
	if err := helper.ensureVFIOWorkers(ctx, dc, nodes); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: "worker-dc-node-a"}, &v1.Pod{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("worker started before KMM module exists: %v", err)
	}
	module := &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name}, Status: kmmv1beta1.ModuleStatus{
		ModuleLoader: kmmv1beta1.DaemonSetStatus{AvailableNumber: 1},
	}}
	if err := cli.Create(ctx, module); err != nil {
		t.Fatal(err)
	}
	legacy := lifecyclePod(t, scheme, dc, node.Name, utils.LoadVFIOAction, v1.PodFailed)
	legacy.Status.InitContainerStatuses = []v1.ContainerStatus{{Name: "pci-device-detector", State: v1.ContainerState{
		Terminated: &v1.ContainerStateTerminated{ExitCode: 124},
	}}}
	if err := cli.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := helper.ensureVFIOWorkers(ctx, dc, nodes); err != nil {
		t.Fatal(err)
	}
	workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name) // Retained until the KMM node label appears.
	nodes.Items[0].Labels["kmm.node.kubernetes.io/test.dc.ready"] = ""
	if err := helper.ensureVFIOWorkers(ctx, dc, nodes); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(legacy), &v1.Pod{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("legacy timed-out detector was not removed: %v", err)
	}
	if err := helper.ensureVFIOWorkers(ctx, dc, nodes); err != nil {
		t.Fatal(err)
	}
	replacement := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name)
	if replacement.Annotations[workermgr.VFDetectorRetryMarker] != "true" {
		t.Fatal("replacement VF worker lacks bounded-retry marker")
	}
	replacement.Status = *legacy.Status.DeepCopy()
	cli.pods[client.ObjectKeyFromObject(replacement)].Status = *replacement.Status.DeepCopy()
	if err := helper.ensureVFIOWorkers(ctx, dc, nodes); err != nil {
		t.Fatal(err)
	}
	if got := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name); got.Status.Phase != v1.PodFailed || got.Annotations[workermgr.VFDetectorRetryMarker] != "true" {
		t.Fatal("marked failed detector was retried again")
	}
}

func TestFailedPFLoadIsReplacedByCleanupAndRetainedUntilReenable(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{
		"selected": "true", "gpu.operator.amd.com/test.dc.driver": utils.DriverTypePFPassthrough,
	}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, node)
	load := lifecyclePod(t, scheme, dc, node.Name, utils.LoadVFIOAction, v1.PodFailed)
	if err := cli.Create(ctx, load); err != nil {
		t.Fatal(err)
	}
	selected := &v1.NodeList{Items: []v1.Node{*node}}
	cli.holdPodDeletes = true
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	if got := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name); got.Labels[utils.WorkerActionLabelKey] != utils.LoadVFIOAction {
		t.Fatal("unload worker replaced a load pod before its deletion completed")
	}
	cli.holdPodDeletes = false
	if err := cli.Delete(ctx, load); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(load), &v1.Pod{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("failed PF load was not deleted: %v", err)
	}
	currentNode := &v1.Node{}
	if err := cli.Get(ctx, types.NamespacedName{Name: node.Name}, currentNode); err != nil {
		t.Fatal(err)
	}
	if _, ok := currentNode.Labels[utils.GetDriverTypeNodeLabel(dc)]; !ok {
		t.Fatal("driver label was removed before PF cleanup completed")
	}
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	unload := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name)
	if unload.Labels[utils.WorkerActionLabelKey] != utils.UnloadVFIOAction {
		t.Fatal("failed PF load was not replaced by an unload worker")
	}
	unload.Status.Phase = v1.PodSucceeded
	cli.pods[client.ObjectKeyFromObject(unload)].Status.Phase = v1.PodSucceeded
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name) // Successful cleanup remains as the disabled-state marker.
	if err := cli.Get(ctx, types.NamespacedName{Name: node.Name}, currentNode); err != nil {
		t.Fatal(err)
	}
	if _, ok := currentNode.Labels[utils.GetDriverTypeNodeLabel(dc)]; ok {
		t.Fatal("driver label remained after PF cleanup")
	}
	complete, err := helper.pfCleanupComplete(ctx, dc, &v1.NodeList{Items: []v1.Node{*currentNode}})
	if err != nil || !complete {
		t.Fatalf("PF cleanup not complete: %v", err)
	}
	enabled := true
	dc.Spec.Driver.Enable = &enabled
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(unload), &v1.Pod{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("cleanup marker was not removed before re-enable: %v", err)
	}
	if err := helper.ensureVFIOWorkers(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	if got := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name); got.Labels[utils.WorkerActionLabelKey] != utils.LoadVFIOAction {
		t.Fatal("PF load worker was not recreated after cleanup marker deletion")
	}
}

func TestPFFinalizerWaitsForEveryAffectedNode(t *testing.T) {
	ctx := context.Background()
	nodeA := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"selected": "true"}}}
	nodeB := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b", Labels: map[string]string{
		"gpu.operator.amd.com/test.dc.driver":     utils.DriverTypePFPassthrough,
		"gpu.operator.amd.com/test.dc.vfio.ready": "",
	}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypePFPassthrough, true, nodeA, nodeB)
	helper.kmmWatchEnabled = false
	load := lifecyclePod(t, scheme, dc, nodeB.Name, utils.LoadVFIOAction, v1.PodFailed)
	if err := cli.Create(ctx, load); err != nil {
		t.Fatal(err)
	}
	selected := &v1.NodeList{Items: []v1.Node{*nodeA}}
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err == nil {
		t.Fatal("finalizer completed while a formerly selected node still had a failed load worker")
	}
	if len(cli.deviceConfig.Finalizers) == 0 {
		t.Fatal("finalizer was removed before cleanup")
	}
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err == nil {
		t.Fatal("finalizer completed while cleanup workers were pending")
	}
	for _, name := range []string{nodeA.Name, nodeB.Name} {
		if workerOnNode(t, cli, dc.Namespace, dc.Name, name).Labels[utils.WorkerActionLabelKey] != utils.UnloadVFIOAction {
			t.Fatalf("node %s has no unload worker", name)
		}
	}
	cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "worker-dc-node-a"}].Status.Phase = v1.PodSucceeded
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err == nil {
		t.Fatal("finalizer completed before every node unloaded")
	}
	workerOnNode(t, cli, dc.Namespace, dc.Name, nodeA.Name) // Keep the successful marker until node B completes.
	cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "worker-dc-node-b"}].Status.Phase = v1.PodFailed
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err == nil {
		t.Fatal("finalizer completed after a failed unload")
	}
	cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "worker-dc-node-b"}].Status.Phase = v1.PodSucceeded
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err == nil {
		t.Fatal("finalizer completed before the VFIO-ready label was removed")
	}
	if err := helper.finalizeDeviceConfig(ctx, dc, selected); err != nil {
		t.Fatal(err)
	}
	if len(cli.deviceConfig.Finalizers) != 0 {
		t.Fatal("finalizer remained after every PF unload succeeded")
	}
	if len(cli.pods) != 0 {
		t.Fatal("completed PF cleanup pods were not removed during finalization")
	}
	if _, ok := cli.nodes[nodeB.Name].Labels["gpu.operator.amd.com/test.dc.vfio.ready"]; ok {
		t.Fatal("formerly selected node kept its VFIO-ready label")
	}
}

func TestDisabledPFCleanupWithNoSelectedNodesUpdatesReady(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node", Labels: map[string]string{
		"gpu.operator.amd.com/test.dc.driver": utils.DriverTypePFPassthrough,
	}}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, node)
	helper.conditionUpdater = conditions.NewDeviceConfigConditionMgr()
	empty := &v1.NodeList{}
	if err := helper.ensureVFIOWorkers(ctx, dc, empty); err != nil {
		t.Fatal(err)
	}
	unload := workerOnNode(t, cli, dc.Namespace, dc.Name, node.Name)
	if unload.Labels[utils.WorkerActionLabelKey] != utils.UnloadVFIOAction {
		t.Fatal("formerly selected node did not receive PF cleanup")
	}
	if err := helper.buildDeviceConfigStatus(ctx, dc, empty); err != nil {
		t.Fatal(err)
	}
	if ready := helper.conditionUpdater.GetReadyCondition(dc); ready.Status != metav1.ConditionFalse || ready.Reason != conditions.VFIOCleanupPending {
		t.Fatalf("ready during PF cleanup: %+v", ready)
	}
	cli.pods[client.ObjectKeyFromObject(unload)].Status.Phase = v1.PodSucceeded
	if err := helper.ensureVFIOWorkers(ctx, dc, empty); err != nil {
		t.Fatal(err)
	}
	if err := helper.buildDeviceConfigStatus(ctx, dc, empty); err != nil {
		t.Fatal(err)
	}
	if ready := helper.conditionUpdater.GetReadyCondition(dc); ready.Status != metav1.ConditionTrue {
		t.Fatalf("not ready after PF cleanup: %+v", ready)
	}
}

func TestDisabledPFWithEmptySelectorOnlyCleansKnownNodes(t *testing.T) {
	ctx := context.Background()
	known := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "known", Labels: map[string]string{
		"gpu.operator.amd.com/test.dc.driver": utils.DriverTypePFPassthrough,
	}}}
	unrelated := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, known, unrelated)
	dc.Spec.Selector = nil
	if err := helper.ensureVFIOWorkers(ctx, dc, &v1.NodeList{Items: []v1.Node{*known, *unrelated}}); err != nil {
		t.Fatal(err)
	}
	workerOnNode(t, cli, dc.Namespace, dc.Name, known.Name)
	if err := cli.Get(ctx, types.NamespacedName{Namespace: dc.Namespace, Name: "worker-dc-unrelated"}, &v1.Pod{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("empty selector started cleanup on an unrelated node: %v", err)
	}
}

func TestPFCleanupFindsOwnedWorkerWithoutNodeLabels(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "former-node"}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, node)
	pod := lifecyclePod(t, scheme, dc, node.Name, utils.LoadVFIOAction, v1.PodFailed)
	if err := cli.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	nodes, err := helper.pfCleanupNodes(ctx, dc, &v1.NodeList{})
	if err != nil || len(nodes.Items) != 1 || nodes.Items[0].Name != node.Name {
		t.Fatalf("owned worker node was not recovered: nodes=%v err=%v", nodes, err)
	}
}
