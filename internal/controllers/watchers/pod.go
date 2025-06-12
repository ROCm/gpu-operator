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
	"fmt"

	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//go:generate mockgen -source=pod.go -package=watchers -destination=mock_pod.go PodEventHandlerAPI
type PodEventHandlerAPI interface {
	Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
}

func NewPodEventHandler(client client.Client, workerMgr workermgr.WorkerMgrAPI) PodEventHandlerAPI {
	return &PodEventHandler{
		client:    client,
		workerMgr: workerMgr,
	}
}

type PodEventHandler struct {
	client    client.Client
	workerMgr workermgr.WorkerMgrAPI
}

// Create handle pod create event
func (h *PodEventHandler) Create(
	ctx context.Context,
	evt event.TypedCreateEvent[client.Object],
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Handle create event if needed
}

// Delete handle pod delete event
func (h *PodEventHandler) Delete(
	ctx context.Context,
	evt event.TypedDeleteEvent[client.Object],
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Handle delete event if needed
}

// Create handle pod generic event
func (h *PodEventHandler) Generic(
	ctx context.Context,
	evt event.TypedGenericEvent[client.Object],
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Handle generic event if needed
}

// Update handle pod update event
func (h *PodEventHandler) Update(
	ctx context.Context,
	evt event.TypedUpdateEvent[client.Object],
	q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Handle update event
	objNew := evt.ObjectNew
	if objNew == nil {
		return
	}

	pod, ok := objNew.(*v1.Pod)
	if !ok {
		return
	}

	logger := log.FromContext(ctx)
	// if any builder pod container status went to ContainerStatusUnknown, delete the pod
	// otherwise the build pod won't be automatically retriggered
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "ContainerStatusUnknown" {
			if err := h.client.Delete(ctx, pod); err != nil {
				logger.Error(err, fmt.Sprintf("failed to delete ContainerStatusUnknown pod %+v", pod.GetName()))
			}
			break
		}
	}

	// if the pod is workerMgr pod, do proper handling based on pod state
	if action, ok := pod.Labels[utils.WorkerActionLabelKey]; ok {
		h.handleWorkerMgrPodEvt(ctx, logger, pod, action)
	}
}

func (h *PodEventHandler) handleWorkerMgrPodEvt(ctx context.Context, logger logr.Logger, pod *v1.Pod, action string) {
	foundDeviceConfigOwner := false
	var nsn types.NamespacedName
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == utils.KindDeviceConfig {
			nsn = types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			}
			foundDeviceConfigOwner = true
		}
	}
	if !foundDeviceConfigOwner {
		logger.Info(fmt.Sprintf("cannot find DeviceConfig owner for worker pod %+v", pod.GetObjectMeta()))
		return
	}
	switch pod.Status.Phase {
	case v1.PodSucceeded:
		// if the worker pod already succeed
		// modify the node label based on action
		switch action {
		case utils.LoadVFIOAction:
			h.workerMgr.AddWorkReadyLabel(ctx, logger, nsn, pod.Spec.NodeName)
		case utils.UnloadVFIOAction:
			h.workerMgr.RemoveWorkReadyLabel(ctx, logger, nsn, pod.Spec.NodeName)
		}
		// remove the completed pod
		logger.Info(fmt.Sprintf("remove worker pod %v after its completion", pod.Name))
		err := h.client.Delete(ctx, pod)
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, fmt.Sprintf("failed to delete completed worker pod %v", pod.Name))
			return
		}
	case v1.PodFailed, v1.PodUnknown:
		logger.Info(fmt.Sprintf("remove worker pod %v due to its %v status", pod.Name, pod.Status.Phase))
		err := h.client.Delete(ctx, pod)
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, fmt.Sprintf("failed to delete stale worker pod %v", pod.Name))
			return
		}
	}
}

type PodLabelPredicate struct {
	predicate.Funcs
}

func (PodLabelPredicate) Update(e event.UpdateEvent) bool {
	return hasExpectedPodLabel(e.ObjectNew)
}

func hasExpectedPodLabel(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	value := labels["kmm.node.kubernetes.io/pod-type"]
	isKMMBuilder := value == "builder"
	_, isWorkerMgrPod := labels[utils.WorkerActionLabelKey]
	return isKMMBuilder || isWorkerMgrPod
}
