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
	"testing"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestReserveNodeAssignmentsRetainsPreviousNonVFIONodes(t *testing.T) {
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypeContainer, true)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{"old-node": {}}
	if err := helper.reserveNodeAssignments(context.Background(), dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := dc.Status.NodeReservations["old-node"]; !ok {
		t.Fatal("zero-match reconcile dropped the previous node reservation")
	}
	if _, ok := helper.nodeAssignments["old-node"]["test/dc"]; !ok {
		t.Fatal("old node assignment was not restored")
	}

	newNode := v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "new-node"}}
	if err := helper.reserveNodeAssignments(context.Background(), dc, &v1.NodeList{Items: []v1.Node{newNode}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := dc.Status.NodeReservations["old-node"]; !ok {
		t.Fatal("selector drift released the old reservation before KMM cleanup")
	}
	if _, ok := dc.Status.NodeReservations["new-node"]; !ok {
		t.Fatal("newly selected node was not reserved")
	}
}

func TestNonVFIOReservationsSkipKMMWorkerListWithoutStaleNodes(t *testing.T) {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeContainer, true, node)
	pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{Items: []v1.Node{*node}})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatal("current node reservation was reported as pending")
	}
	if cli.podListCalls != 0 {
		t.Fatalf("listed KMM worker pods %d times without a stale reservation", cli.podListCalls)
	}
}

func TestNonVFIOReservationWaitsForKMMAndWorkerToReleaseNode(t *testing.T) {
	ctx := context.Background()
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node", Labels: map[string]string{"pool": "old"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeContainer, true, oldNode)
	dc.Spec.Selector = map[string]string{"pool": "missing"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{"old-node": {}}

	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name, UID: types.UID("module-uid")},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "old"}},
	}
	if err := controllerutil.SetControllerReference(dc, module, scheme); err != nil {
		t.Fatal(err)
	}
	cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()
	cli.nmcs[oldNode.Name] = &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: oldNode.Name},
		Spec: kmmv1beta1.NodeModulesConfigSpec{Modules: []kmmv1beta1.NodeModuleSpec{{
			ModuleItem: kmmv1beta1.ModuleItem{Namespace: dc.Namespace, Name: dc.Name},
		}}},
		Status: kmmv1beta1.NodeModulesConfigStatus{Modules: []kmmv1beta1.NodeModuleStatus{{
			ModuleItem: kmmv1beta1.ModuleItem{Namespace: dc.Namespace, Name: dc.Name},
		}}},
	}

	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatal("reservation was released while KMM still listed the module")
	}
	if got := cli.modules[client.ObjectKeyFromObject(module)].Spec.Selector; got["pool"] != "missing" {
		t.Fatalf("KMM Module selector = %v, want current zero-match selector", got)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("old reservation was released while KMM still managed the node")
	}

	nmc := cli.nmcs[oldNode.Name]
	nmc.Spec.Modules = nil
	nmc.Status.Modules = nil
	cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "kmm-worker-old"}] = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: "kmm-worker-old", Labels: map[string]string{
			kmmModuleNameLabel: dc.Name, kmmWorkerComponentLabel: kmmWorkerComponent,
		}},
		Spec: v1.PodSpec{NodeName: oldNode.Name},
	}
	pending, requeue, err = helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatal("reservation was released while a KMM worker still targeted the node")
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("old reservation was released while a KMM worker still targeted the node")
	}

	delete(cli.pods, types.NamespacedName{Namespace: dc.Namespace, Name: "kmm-worker-old"})
	pending, requeue, err = helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatal("reservation remained after KMM spec, status, and worker were gone")
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; ok {
		t.Fatal("old reservation was retained after KMM released the node")
	}
	if _, ok := helper.nodeAssignments[oldNode.Name]; ok {
		t.Fatal("old in-memory node assignment remained after release")
	}
}

func TestNeverManagedReservationReleasesWhenKMMAPIIsUnavailable(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeContainer, false, oldNode)
	helper.kmmWatchEnabled = false
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{}
	cli.moduleNoMatch = true
	cli.nmcNoMatch = true

	if err := helper.reserveNodeAssignments(context.Background(), dc, &v1.NodeList{Items: []v1.Node{*oldNode}}); err != nil {
		t.Fatal(err)
	}
	if got := dc.Status.NodeReservations[oldNode.Name].KMMOwnership; got != amdv1alpha1.KMMOwnershipNeverManaged {
		t.Fatalf("new reservation KMM ownership = %q, want never-managed", got)
	}
	if err := helper.reserveNodeAssignments(context.Background(), dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatal("reservation remained pending although KMM was never enabled")
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; ok {
		t.Fatal("reservation remained after confirming KMM never managed the node")
	}
}

func TestLegacyReservationKMMOwnershipDefaultsToUnknown(t *testing.T) {
	oldNode := "old-node"
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypeContainer, false)
	helper.kmmWatchEnabled = false
	dc.Status.NodeModuleStatus = map[string]amdv1alpha1.ModuleStatus{oldNode: {}}

	if err := helper.reserveNodeAssignments(context.Background(), dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	if got := dc.Status.NodeReservations[oldNode].KMMOwnership; got != amdv1alpha1.KMMOwnershipUnknown {
		t.Fatalf("legacy reservation KMM ownership = %q, want unknown", got)
	}
}

func TestUnknownKMMReservationStaysHeldWithoutAPI(t *testing.T) {
	for _, state := range []amdv1alpha1.KMMOwnership{amdv1alpha1.KMMOwnershipUnknown, amdv1alpha1.KMMOwnershipManaged} {
		t.Run(string(state), func(t *testing.T) {
			oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
			helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeContainer, false, oldNode)
			dc.Status.NodeReservationsInitialized = true
			dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
				oldNode.Name: {KMMOwnership: state},
			}
			cli.moduleNoMatch = true

			pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{})
			if err != nil {
				t.Fatal(err)
			}
			if !pending || !requeue {
				t.Fatalf("result = pending %t, requeue %t; want retry while KMM API is unavailable", pending, requeue)
			}
			if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
				t.Fatal("reservation was released without proving KMM ownership ended")
			}
		})
	}
}

func TestUnknownKMMReservationStaysHeldWhenNMCAPIIsUnavailable(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeContainer, false, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	cli.nmcNoMatch = true

	pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatalf("result = pending %t, requeue %t; want retry while NMC API is unavailable", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("reservation was released while KMM state was not observable")
	}
}

func TestPreviouslyManagedVFIOReservationRetriesWhileKMMWatchIsDisabled(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeVFPassthrough, false, oldNode)
	helper.kmmWatchEnabled = false
	dc.Spec.Selector = map[string]string{"pool": "new"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipManaged},
	}
	cli.moduleNoMatch = true
	cli.nmcNoMatch = true

	pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatalf("result = pending %t, requeue %t; want retry until the previously used KMM APIs return", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("reservation was released while previously managed KMM state was unavailable")
	}
}

func TestDisabledKMMKeepsNewNodeReservedForUnownedModule(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "new-node", Labels: map[string]string{"pool": "new"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, false, node)
	helper.kmmWatchEnabled = false
	dc.Spec.Selector = map[string]string{"pool": "new"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		node.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipNeverManaged},
	}
	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "new"}},
	}
	previousOwner := dc.DeepCopy()
	previousOwner.UID = types.UID("previous-dc-uid")
	if err := controllerutil.SetControllerReference(previousOwner, module, scheme); err != nil {
		t.Fatal(err)
	}
	cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()

	if pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{Items: []v1.Node{*node}}); err != nil || pending || requeue {
		t.Fatalf("selected-node reconciliation = pending %t, requeue %t, err %v", pending, requeue, err)
	}
	if got := dc.Status.NodeReservations[node.Name].KMMOwnership; got != amdv1alpha1.KMMOwnershipUnknown {
		t.Fatalf("selected node ownership = %q, want unknown from unowned lingering Module selector", got)
	}
	if got := cli.modules[client.ObjectKeyFromObject(module)].Spec.Selector; got["pool"] != "new" {
		t.Fatalf("lingering Module selector = %v, want unchanged", got)
	}

	dc.Spec.Selector = map[string]string{"pool": "other"}
	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatalf("stale-node reconciliation = pending %t, requeue %t; want reservation held for lingering Module", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[node.Name]; !ok {
		t.Fatal("reservation was released while the lingering Module still selected the node")
	}
	if got := cli.modules[client.ObjectKeyFromObject(module)].Spec.Selector; got["pool"] != "new" {
		t.Fatalf("lingering Module selector changed to %v while KMM was disabled", got)
	}
}

func TestDisabledKMMKeepsMissingNodeReservedForLingeringModule(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "returning-node", Labels: map[string]string{"pool": "new"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, false, node)
	helper.kmmWatchEnabled = false
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		node.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipManaged},
	}
	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "new"}},
	}
	if err := controllerutil.SetControllerReference(dc, module, scheme); err != nil {
		t.Fatal(err)
	}
	cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()
	helper.updateNodeAssignments("test/dc", nodeListForReservations(dc.Status.NodeReservations), false)
	delete(cli.nodes, node.Name)

	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatalf("missing-node reconciliation = pending %t, requeue %t; want reservation held", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[node.Name]; !ok {
		t.Fatal("reservation was released while an owned Module could still select the missing node")
	}
	if _, ok := helper.nodeAssignments[node.Name]["test/dc"]; !ok {
		t.Fatal("node assignment was released while the node was missing")
	}

	cli.nodes[node.Name] = node.DeepCopy()
	pending, requeue, err = helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if !pending || !requeue {
		t.Fatalf("rejoined-node reconciliation = pending %t, requeue %t; want reservation held", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[node.Name]; !ok {
		t.Fatal("reservation was released after the node returned under a matching Module selector")
	}
}
