/*
Copyright 2024.

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

package watchers

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	workqueue "k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
)

type NodePredicate struct {
	predicate.Funcs
}

func (NodePredicate) Create(e event.CreateEvent) bool {
	return true
}

func (NodePredicate) Update(e event.UpdateEvent) bool {
	oldNode, okOld := e.ObjectOld.(*v1.Node)
	newNode, okNew := e.ObjectNew.(*v1.Node)
	if !okOld || !okNew {
		return false
	}

	return nodeNeedsReconcile(oldNode, newNode)
}

func nodeNeedsReconcile(oldNode, newNode *v1.Node) bool {
	return oldNode.Status.NodeInfo.KernelVersion != newNode.Status.NodeInfo.KernelVersion ||
		oldNode.Generation != newNode.Generation ||
		oldNode.Status.NodeInfo.BootID != newNode.Status.NodeInfo.BootID ||
		!reflect.DeepEqual(oldNode.Labels, newNode.Labels) ||
		!reflect.DeepEqual(oldNode.Status.Allocatable, newNode.Status.Allocatable) ||
		!reflect.DeepEqual(oldNode.Status.Capacity, newNode.Status.Capacity) ||
		nodeReadyStatus(oldNode) != nodeReadyStatus(newNode)
}

func nodeReadyStatus(node *v1.Node) v1.ConditionStatus {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status
		}
	}
	return v1.ConditionUnknown
}

func (NodePredicate) Generic(e event.GenericEvent) bool {
	return true
}

func (NodePredicate) Delete(e event.DeleteEvent) bool {
	return true
}

//go:generate mockgen -source=node.go -package=watchers -destination=mock_node.go NodeEventHandlerAPI
type NodeEventHandlerAPI interface {
	Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
}

func NewNodeEventHandler(client client.Client, workerMgr workermgr.WorkerMgrAPI) NodeEventHandlerAPI {
	return &NodeEventHandler{
		client:    client,
		workerMgr: workerMgr,
	}
}

type NodeEventHandler struct {
	client    client.Client
	workerMgr workermgr.WorkerMgrAPI
}

// Create handle create event
func (h *NodeEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// if a Node was created, the kernel mapping may be updated
	// any DeviceConfig would be possible to manage this new Node
	// trigger the reconcile on all existing DeviceConfigs
	h.reconcileAllDeviceConfigs(ctx, q)
}

// Create handle generic event
func (h *NodeEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.reconcileRelatedDeviceConfig(ctx, evt.Object, q)
}

// Delete handle delete event
func (h *NodeEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// if a Node was deleted
	// trigger the reconcile when there exists a DeviceConfig managing the node
	h.reconcileRelatedDeviceConfig(ctx, evt.Object, q)
}

// Update handle update event
func (h *NodeEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	oldNode, okOld := evt.ObjectOld.(*v1.Node)
	newNode, okNew := evt.ObjectNew.(*v1.Node)
	logger := log.FromContext(ctx)
	if !okOld || !okNew {
		return
	}

	if nodeNeedsReconcile(oldNode, newNode) {
		h.reconcileAllDeviceConfigs(ctx, q)
	}
	h.handlePostProcess(ctx, logger, oldNode, newNode)
}

func (h *NodeEventHandler) reconcileAllDeviceConfigs(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	logger := log.FromContext(ctx)
	devConfigList := &amdv1alpha1.DeviceConfigList{}
	err := h.client.List(ctx, devConfigList)
	if err != nil {
		logger.Error(err, "failed to list deviceconfigs")
	}
	for _, dcfg := range devConfigList.Items {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: dcfg.Namespace,
				Name:      dcfg.Name,
			},
		})
	}
}

func (h *NodeEventHandler) reconcileRelatedDeviceConfig(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	logger := log.FromContext(ctx)
	nmc := &kmmv1beta1.NodeModulesConfig{}
	err := h.client.Get(ctx, types.NamespacedName{Name: obj.GetName()}, nmc)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.Error(err, "failed to get NMC for node")
		}
		return
	}
	foundDeviceConfig := false
	for _, module := range nmc.Spec.Modules {
		switch module.Config.Modprobe.ModuleName {
		case kmmmodule.ContainerDriverModuleName,
			kmmmodule.VFPassthroughDriverModuleName:
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: module.Namespace,
					Name:      module.Name,
				},
			})
			foundDeviceConfig = true
		}
		// once amdgpu related kernel module was found on this NMC
		// no need to continue for loop
		if foundDeviceConfig {
			break
		}
	}
}

func (h *NodeEventHandler) handlePostProcess(ctx context.Context, logger logr.Logger, oldNode, node *v1.Node) {
	// detect whether vfio bind status
	hasVFIOReadyLabel, _, vfioDevConfigNamespace, vfioDevConfigName := utils.HasNodeLabelTemplateMatch(node.Labels, utils.VFIOMountReadyLabelTemplate)
	if hasVFIOReadyLabel && oldNode.Status.NodeInfo.BootID != node.Status.NodeInfo.BootID {
		// if the node was rebooted, most of time devices need rebinding to vfio-pci
		// directly remove the VFIO ready label
		// so that the event handler will bring up a new vfio worker pod to load devices into VFIO
		h.workerMgr.RemoveWorkReadyLabel(ctx, logger, types.NamespacedName{
			Namespace: vfioDevConfigNamespace,
			Name:      vfioDevConfigName,
		}, node.Name)
	}
}
