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
	"errors"
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

func TestMissingNonVFIONodeRetainsReservationForKMMModuleRejoin(t *testing.T) {
	ctx := context.Background()
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "returning-node", Labels: map[string]string{"pool": "old"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeContainer, true, node)
	dc.Spec.Selector = map[string]string{"pool": "old"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		node.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipManaged},
	}
	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "old"}},
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
		t.Fatal("reservation was released while the owned Module still selected the missing node")
	}
	if _, ok := helper.nodeAssignments[node.Name]["test/dc"]; !ok {
		t.Fatal("assignment was released before the node could safely rejoin")
	}

	cli.nodes[node.Name] = node.DeepCopy()
	if err := helper.validateNodeAssignments("test/other", &v1.NodeList{Items: []v1.Node{*node}}); err == nil {
		t.Fatal("competing DeviceConfig was allowed to claim the returning node")
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
			enabled := true
			dc.Spec.Driver.Enable = &enabled
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

func TestUnknownReservationReleasesWhenKMMWasNeverRequestedAndAPIsAreAbsent(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypeVFPassthrough, false, oldNode)
	helper.kmmWatchEnabled = false
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	cli.moduleNoMatch = true
	cli.nmcNoMatch = true

	pending, requeue, err := helper.reconcileKMMNodeReservations(context.Background(), dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatalf("result = pending %t, requeue %t; want no KMM wait for explicitly disabled config with absent APIs", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; ok {
		t.Fatal("legacy unknown reservation remained after KMM was never requested and both APIs were absent")
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

func TestPFPassthroughUnknownReservationReleasesWithoutKMMState(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	helper.updateNodeAssignments("test/dc", nodeListForReservations(dc.Status.NodeReservations), false)

	if err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; ok {
		t.Fatal("legacy unknown PF reservation remained without KMM state")
	}
	if _, ok := helper.nodeAssignments[oldNode.Name]; ok {
		t.Fatal("released PF reservation remained in node assignments")
	}
}

func TestPFPassthroughUnknownReservationStaysHeldWithKMMEvidence(t *testing.T) {
	for _, evidence := range []string{"module", "nmc", "worker", "historical-status", "managed-ownership"} {
		t.Run(evidence, func(t *testing.T) {
			oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
			helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
			ownership := amdv1alpha1.KMMOwnershipUnknown
			if evidence == "managed-ownership" {
				ownership = amdv1alpha1.KMMOwnershipManaged
			}
			dc.Status.NodeReservationsInitialized = true
			dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
				oldNode.Name: {KMMOwnership: ownership},
			}
			switch evidence {
			case "module":
				cli.modules[types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name}] = &kmmv1beta1.Module{
					ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
				}
			case "nmc":
				cli.nmcs[oldNode.Name] = &kmmv1beta1.NodeModulesConfig{
					ObjectMeta: metav1.ObjectMeta{Name: oldNode.Name},
					Status: kmmv1beta1.NodeModulesConfigStatus{Modules: []kmmv1beta1.NodeModuleStatus{{
						ModuleItem: kmmv1beta1.ModuleItem{Namespace: dc.Namespace, Name: dc.Name},
					}}},
				}
			case "worker":
				cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "kmm-worker-old"}] = &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: "kmm-worker-old", Labels: map[string]string{
						kmmModuleNameLabel: dc.Name, kmmWorkerComponentLabel: kmmWorkerComponent,
					}},
					Spec: v1.PodSpec{NodeName: oldNode.Name},
				}
			case "historical-status":
				dc.Status.NodeModuleStatus = map[string]amdv1alpha1.ModuleStatus{
					oldNode.Name: {ContainerImage: "previous-kmm-image"},
				}
			}

			if err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{}); err != nil {
				t.Fatal(err)
			}
			if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
				t.Fatal("PF reservation was released despite KMM evidence")
			}
		})
	}
}

func TestPFPassthroughUnknownReservationWaitsForVFIOCleanupMarkers(t *testing.T) {
	for _, marker := range []string{"cleanup-pending", "binding-boot-id"} {
		t.Run(marker, func(t *testing.T) {
			oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
			helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
			reservation := amdv1alpha1.NodeReservationStatus{KMMOwnership: amdv1alpha1.KMMOwnershipUnknown}
			if marker == "cleanup-pending" {
				reservation.VFIOCleanupPending = true
			} else {
				reservation.VFIOBindingBootID = "boot-1"
			}
			dc.Status.NodeReservationsInitialized = true
			dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{oldNode.Name: reservation}

			if err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{}); err != nil {
				t.Fatal(err)
			}
			if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
				t.Fatal("PF reservation was released before VFIO cleanup completed")
			}
		})
	}
}

func TestPFPassthroughUnknownReservationReleasesWhenKMMAPIsAreAbsent(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	cli.moduleNoMatch = true
	cli.nmcNoMatch = true

	if err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; ok {
		t.Fatal("unknown PF reservation remained although the KMM APIs were absent")
	}
}

func TestPFPassthroughUnknownReservationRetriesKMMAPIErrors(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	cli.moduleErr = errors.New("KMM API unavailable")

	err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{})
	if err == nil {
		t.Fatal("expected an error while KMM API state was unavailable")
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("PF reservation was released while KMM state was unavailable")
	}
}

func TestPFPassthroughUnknownReservationRetriesVFIOWorkerReadErrors(t *testing.T) {
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node"}}
	helper, dc, cli, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, false, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipUnknown},
	}
	cli.workerErr = errors.New("worker API unavailable")

	err := helper.ensureVFIOWorkers(context.Background(), dc, &v1.NodeList{})
	if err == nil {
		t.Fatal("expected an error while VFIO worker state was unavailable")
	}
	if _, ok := dc.Status.NodeReservations[oldNode.Name]; !ok {
		t.Fatal("PF reservation was released while VFIO worker state was unavailable")
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

func TestMissingRetiredVFIONodeStopsBlockingKMMReadinessAndKeepsAssignment(t *testing.T) {
	ctx := context.Background()
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node", Labels: map[string]string{"pool": "old"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true, oldNode)
	dc.Spec.Selector = map[string]string{"pool": "new"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipManaged},
	}
	dc.Status.NodeModuleStatus = map[string]amdv1alpha1.ModuleStatus{
		oldNode.Name: {ContainerImage: "retired-kmm-image"},
	}
	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "new"}},
	}
	if err := controllerutil.SetControllerReference(dc, module, scheme); err != nil {
		t.Fatal(err)
	}
	cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()
	if err := helper.reserveNodeAssignments(ctx, dc, &v1.NodeList{Items: []v1.Node{*oldNode}}); err != nil {
		t.Fatal(err)
	}
	delete(cli.nodes, oldNode.Name)

	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatalf("missing retired node result = pending %t, requeue %t; want Ready unblocked", pending, requeue)
	}
	reservation, ok := dc.Status.NodeReservations[oldNode.Name]
	if !ok {
		t.Fatal("retired node reservation was removed before a same-name Node could rejoin")
	}
	if reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged {
		t.Fatalf("retired node KMM ownership = %q, want never-managed", reservation.KMMOwnership)
	}
	if !reservation.KMMRetirementPending {
		t.Fatal("retired node did not retain its KMM rejoin tombstone")
	}
	if !reservation.KMMNodeLabelsCaptured || reservation.KMMNodeLabels["pool"] != "old" {
		t.Fatalf("retired node label snapshot = captured %t, labels %v; want captured old labels", reservation.KMMNodeLabelsCaptured, reservation.KMMNodeLabels)
	}
	if _, ok := dc.Status.NodeModuleStatus[oldNode.Name]; ok {
		t.Fatal("stale per-node KMM status remained after the node retirement was proven")
	}
	if hasUnselectedKMMReservation(dc, &v1.NodeList{}) {
		t.Fatal("proven retired tombstone still blocks KMM readiness")
	}
	if _, ok := helper.nodeAssignments[oldNode.Name]["test/dc"]; !ok {
		t.Fatal("retired node assignment was not preserved for safe rejoin handling")
	}
}

func TestMissingVFIONodeRequiresCapturedSelectorLabelPresence(t *testing.T) {
	for _, captured := range []bool{false, true} {
		name := "not-captured"
		if captured {
			name = "captured-absent"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true)
			dc.Spec.Selector = map[string]string{"pool": "old"}
			dc.Status.NodeReservationsInitialized = true
			dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
				"gone-node": {
					KMMOwnership:          amdv1alpha1.KMMOwnershipManaged,
					KMMNodeLabels:         map[string]string{},
					KMMNodeLabelsCaptured: captured,
				},
			}
			module := &kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
				Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "old"}},
			}
			if err := controllerutil.SetControllerReference(dc, module, scheme); err != nil {
				t.Fatal(err)
			}
			cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()

			pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
			if err != nil {
				t.Fatal(err)
			}
			reservation, ok := dc.Status.NodeReservations["gone-node"]
			if !ok {
				t.Fatal("missing-node reservation was removed before it could safely rejoin")
			}
			if captured {
				if pending || requeue || reservation.KMMOwnership != amdv1alpha1.KMMOwnershipNeverManaged || !reservation.KMMRetirementPending {
					t.Fatalf("captured absent label result = pending %t, requeue %t, reservation %+v; want retired tombstone", pending, requeue, reservation)
				}
			} else if !pending || !requeue || reservation.KMMOwnership == amdv1alpha1.KMMOwnershipNeverManaged {
				t.Fatalf("uncaptured label result = pending %t, requeue %t, reservation %+v; want ownership retained", pending, requeue, reservation)
			}
		})
	}
}

func TestReserveNodeAssignmentsRefreshesStaleNodeLabels(t *testing.T) {
	ctx := context.Background()
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-node", Labels: map[string]string{"pool": "old", "zone": "west"}}}
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true, oldNode)
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		oldNode.Name: {KMMOwnership: amdv1alpha1.KMMOwnershipManaged},
	}

	if err := helper.reserveNodeAssignments(ctx, dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	reservation := dc.Status.NodeReservations[oldNode.Name]
	if !reservation.KMMNodeLabelsCaptured || reservation.KMMNodeLabels["pool"] != "old" || reservation.KMMNodeLabels["zone"] != "west" {
		t.Fatalf("stale reserved node snapshot = captured %t, labels %v; want full observed labels", reservation.KMMNodeLabelsCaptured, reservation.KMMNodeLabels)
	}
}

func TestVFIORejoinChecksTombstoneBeforeStaleCleanup(t *testing.T) {
	ctx := context.Background()
	returningNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "returning-node", Labels: map[string]string{"pool": "return"}}}
	helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true, returningNode)
	dc.Spec.Selector = map[string]string{"pool": "selected"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		returningNode.Name: {
			KMMOwnership:          amdv1alpha1.KMMOwnershipNeverManaged,
			KMMNodeLabels:         map[string]string{"pool": "retired"},
			KMMNodeLabelsCaptured: true,
			KMMRetirementPending:  true,
		},
	}
	module := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
		Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "return"}},
	}
	previousOwner := dc.DeepCopy()
	previousOwner.UID = types.UID("previous-dc-uid")
	if err := controllerutil.SetControllerReference(previousOwner, module, scheme); err != nil {
		t.Fatal(err)
	}
	cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()
	if err := helper.reserveNodeAssignments(ctx, dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}

	if err := helper.ensureVFIOWorkers(ctx, dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := dc.Status.NodeReservations[returningNode.Name]; !ok {
		t.Fatal("generic stale cleanup removed the tombstone before KMM rejoin checks")
	}
	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	reservation, ok := dc.Status.NodeReservations[returningNode.Name]
	if !ok {
		t.Fatal("reservation was released although the returning node matches a lingering Module selector")
	}
	if !pending || !requeue || reservation.KMMOwnership == amdv1alpha1.KMMOwnershipNeverManaged || !reservation.KMMRetirementPending {
		t.Fatalf("rejoined node result = pending %t, requeue %t, reservation %+v; want active KMM ownership", pending, requeue, reservation)
	}

	restored := &deviceConfigReconcilerHelper{nodeAssignments: map[string]map[string]struct{}{}}
	if err := restored.buildNodeAssignments(ctx, &amdv1alpha1.DeviceConfigList{Items: []amdv1alpha1.DeviceConfig{*dc.DeepCopy()}}); err != nil {
		t.Fatal(err)
	}
	if err := restored.validateNodeAssignments("other/config", &v1.NodeList{Items: []v1.Node{*returningNode}}); err == nil {
		t.Fatal("another DeviceConfig could claim the node before its owner reconciled the tombstone")
	}
}

func TestVFIORetirementTombstoneReleasesAfterSwitchToPFAndNodeRejoin(t *testing.T) {
	ctx := context.Background()
	returningNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "returning-node", Labels: map[string]string{"pool": "return"}}}
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypePFPassthrough, true, returningNode)
	dc.Spec.Selector = map[string]string{"pool": "selected"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		returningNode.Name: {
			KMMOwnership:          amdv1alpha1.KMMOwnershipNeverManaged,
			KMMNodeLabels:         map[string]string{"pool": "retired"},
			KMMNodeLabelsCaptured: true,
			KMMRetirementPending:  true,
		},
	}
	helper.updateNodeAssignments("test/dc", nodeListForReservations(dc.Status.NodeReservations), false)

	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	if pending || requeue {
		t.Fatalf("clean rejoin result = pending %t, requeue %t; want tombstone released", pending, requeue)
	}
	if _, ok := dc.Status.NodeReservations[returningNode.Name]; ok {
		t.Fatal("retired VF reservation remained after a clean rejoin in PF mode")
	}
	if _, ok := helper.nodeAssignments[returningNode.Name]; ok {
		t.Fatal("node assignment remained after the PF-mode tombstone was safely released")
	}
}

func TestVFIORejoinRetainsReservationWhenHistoricalKMMStatusExists(t *testing.T) {
	ctx := context.Background()
	returningNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "returning-node", Labels: map[string]string{"pool": "return"}}}
	helper, dc, _, _ := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true, returningNode)
	dc.Spec.Selector = map[string]string{"pool": "selected"}
	dc.Status.NodeReservationsInitialized = true
	dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{
		returningNode.Name: {
			KMMOwnership:          amdv1alpha1.KMMOwnershipNeverManaged,
			KMMNodeLabels:         map[string]string{"pool": "retired"},
			KMMNodeLabelsCaptured: true,
			KMMRetirementPending:  true,
		},
	}
	dc.Status.NodeModuleStatus = map[string]amdv1alpha1.ModuleStatus{
		returningNode.Name: {ContainerImage: "rejoined-kmm-image"},
	}
	if err := helper.reserveNodeAssignments(ctx, dc, &v1.NodeList{}); err != nil {
		t.Fatal(err)
	}

	pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
	if err != nil {
		t.Fatal(err)
	}
	reservation := dc.Status.NodeReservations[returningNode.Name]
	if !pending || !requeue || reservation.KMMOwnership != amdv1alpha1.KMMOwnershipManaged || reservation.KMMRetirementPending {
		t.Fatalf("rejoined node result = pending %t, requeue %t, reservation %+v; want historical KMM ownership retained", pending, requeue, reservation)
	}
}

func TestMissingVFIONodeRetainsOwnershipWhenRetirementIsUnproven(t *testing.T) {
	for _, scenario := range []string{"selector-matches-snapshot", "unowned-module", "nmc-state", "worker-state", "vfio-cleanup-marker", "binding-marker", "api-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			helper, dc, cli, scheme := lifecycleFixture(t, utils.DriverTypeVFPassthrough, true)
			helper.kmmWatchEnabled = false
			dc.Spec.Selector = map[string]string{"pool": "new"}
			reservation := amdv1alpha1.NodeReservationStatus{
				KMMOwnership:          amdv1alpha1.KMMOwnershipUnknown,
				KMMNodeLabels:         map[string]string{"pool": "old"},
				KMMNodeLabelsCaptured: true,
			}
			dc.Status.NodeReservationsInitialized = true
			dc.Status.NodeReservations = map[string]amdv1alpha1.NodeReservationStatus{"gone-node": reservation}

			module := &kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: dc.Name},
				Spec:       kmmv1beta1.ModuleSpec{Selector: map[string]string{"pool": "new"}},
			}
			if scenario == "selector-matches-snapshot" {
				module.Spec.Selector = map[string]string{"pool": "old"}
			}
			if scenario == "unowned-module" {
				previousOwner := dc.DeepCopy()
				previousOwner.UID = types.UID("previous-dc-uid")
				if err := controllerutil.SetControllerReference(previousOwner, module, scheme); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "nmc-state" && scenario != "worker-state" && scenario != "vfio-cleanup-marker" && scenario != "binding-marker" && scenario != "api-unavailable" {
				if scenario != "unowned-module" {
					if err := controllerutil.SetControllerReference(dc, module, scheme); err != nil {
						t.Fatal(err)
					}
				}
				cli.modules[client.ObjectKeyFromObject(module)] = module.DeepCopy()
			}
			switch scenario {
			case "nmc-state":
				cli.nmcs["gone-node"] = &kmmv1beta1.NodeModulesConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "gone-node"},
					Spec: kmmv1beta1.NodeModulesConfigSpec{Modules: []kmmv1beta1.NodeModuleSpec{{
						ModuleItem: kmmv1beta1.ModuleItem{Namespace: dc.Namespace, Name: dc.Name},
					}}},
				}
			case "worker-state":
				cli.pods[types.NamespacedName{Namespace: dc.Namespace, Name: "kmm-worker-gone"}] = &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: dc.Namespace, Name: "kmm-worker-gone", Labels: map[string]string{
						kmmModuleNameLabel: dc.Name, kmmWorkerComponentLabel: kmmWorkerComponent,
					}},
					Spec: v1.PodSpec{NodeName: "gone-node"},
				}
			case "vfio-cleanup-marker":
				reservation.VFIOCleanupPending = true
			case "binding-marker":
				reservation.VFIOBindingBootID = "boot-1"
			case "api-unavailable":
				cli.moduleNoMatch = true
				cli.nmcNoMatch = true
			}
			if scenario == "vfio-cleanup-marker" || scenario == "binding-marker" {
				dc.Status.NodeReservations["gone-node"] = reservation
			}

			pending, requeue, err := helper.reconcileKMMNodeReservations(ctx, dc, &v1.NodeList{})
			if err != nil {
				t.Fatal(err)
			}
			if !pending || !requeue {
				t.Fatalf("result = pending %t, requeue %t; want retirement held for %s", pending, requeue, scenario)
			}
			got := dc.Status.NodeReservations["gone-node"]
			if got.KMMOwnership == amdv1alpha1.KMMOwnershipNeverManaged || got.KMMRetirementPending {
				t.Fatalf("reservation %+v was marked retired despite %s", got, scenario)
			}
		})
	}
}
