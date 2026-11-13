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

	amdv1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
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

// KMM labels its helper pods with kmm.node.kubernetes.io/pod-type. Build pods
// carry the value "build" and sign pods "sign" in the deployed ROCm KMM fork;
// the package that defines these is internal to KMM and not importable, so the
// values are duplicated here.
const (
	kmmPodTypeLabelKey   = "kmm.node.kubernetes.io/pod-type"
	kmmPodTypeBuildValue = "build"
	kmmPodTypeSignValue  = "sign"
)

// isKMMBuildOrSignPod reports whether the pod is a KMM build or sign helper pod.
// Both share the same recreate-if-missing lifecycle (KMM's Sync respawns them),
// so both can be safely deleted to retrigger KMM.
func isKMMBuildOrSignPod(labels map[string]string) bool {
	podType := labels[kmmPodTypeLabelKey]
	return podType == kmmPodTypeBuildValue || podType == kmmPodTypeSignValue
}

//go:generate mockgen -source=pod.go -package=watchers -destination=mock_pod.go PodEventHandlerAPI
type PodEventHandlerAPI interface {
	Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
}

func NewPodEventHandler(client client.Client) PodEventHandlerAPI {
	return &PodEventHandler{client: client}
}

type PodEventHandler struct {
	client client.Client
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
	pod, ok := evt.Object.(*v1.Pod)
	if !ok {
		return
	}
	action, ok := pod.Labels[utils.WorkerActionLabelKey]
	if !ok || action != utils.LoadVFIOAction && action != utils.UnloadVFIOAction {
		return
	}
	_, nsn, ok := h.getWorkerDeviceConfig(ctx, pod, action)
	if !ok {
		return
	}
	q.Add(reconcile.Request{NamespacedName: nsn})
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
	// Delete ContainerStatusUnknown build/sign pods to retrigger KMM, but only when the node
	// is Ready — deleting while NotReady causes KMM to spawn a replacement before the kubelet
	// volume registry re-syncs, producing a FailedMount on the Dockerfile ConfigMap.
	if isKMMBuildOrSignPod(pod.Labels) {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "ContainerStatusUnknown" {
				node := &v1.Node{}
				if err := h.client.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
					logger.Error(err, "failed to get node for ContainerStatusUnknown pod, skipping deletion",
						"pod", pod.GetName(), "node", pod.Spec.NodeName)
					break
				}
				nodeReady := false
				for _, cond := range node.Status.Conditions {
					if cond.Type == v1.NodeReady && cond.Status == v1.ConditionTrue {
						nodeReady = true
						break
					}
				}
				if !nodeReady {
					logger.Info("skipping ContainerStatusUnknown pod deletion: node is not Ready, will retry when node recovers",
						"pod", pod.GetName(), "node", pod.Spec.NodeName)
					break
				}
				if err := h.client.Delete(ctx, pod); err != nil {
					logger.Error(err, "failed to delete ContainerStatusUnknown pod", "pod", pod.GetName())
				}
				break
			}
		}
	}

	// if the pod is workerMgr pod, do proper handling based on pod state
	if action, ok := pod.Labels[utils.WorkerActionLabelKey]; ok {
		h.handleWorkerMgrPodEvt(ctx, logger, pod, action, q)
	}
}

func (h *PodEventHandler) handleWorkerMgrPodEvt(ctx context.Context, logger logr.Logger, pod *v1.Pod, action string, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	devConfig, nsn, ok := h.getWorkerDeviceConfig(ctx, pod, action)
	if !ok {
		return
	}
	if action == utils.LoadVFIOAction &&
		((devConfig.Spec.Driver.Enable != nil && !*devConfig.Spec.Driver.Enable) || !devConfig.DeletionTimestamp.IsZero()) {
		q.Add(reconcile.Request{NamespacedName: nsn})
		return
	}
	if !devConfig.DeletionTimestamp.IsZero() &&
		(action != utils.UnloadVFIOAction || pod.Status.Phase != v1.PodSucceeded) {
		q.Add(reconcile.Request{NamespacedName: nsn})
		return
	}
	switch pod.Status.Phase {
	case v1.PodSucceeded:
		q.Add(reconcile.Request{NamespacedName: nsn})
	case v1.PodFailed, v1.PodUnknown:
		logger.Info(fmt.Sprintf("retain worker pod %v due to its %v status", pod.Name, pod.Status.Phase))
		q.Add(reconcile.Request{NamespacedName: nsn})
	}
}

func (h *PodEventHandler) getWorkerDeviceConfig(ctx context.Context, pod *v1.Pod, action string) (*amdv1alpha1.DeviceConfig, types.NamespacedName, bool) {
	if action != utils.LoadVFIOAction && action != utils.UnloadVFIOAction {
		return nil, types.NamespacedName{}, false
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.Kind != utils.KindDeviceConfig || pod.Name != fmt.Sprintf("worker-%v-%v", owner.Name, pod.Spec.NodeName) {
		return nil, types.NamespacedName{}, false
	}
	nsn := types.NamespacedName{Namespace: pod.Namespace, Name: owner.Name}
	devConfig := &amdv1alpha1.DeviceConfig{}
	if err := h.client.Get(ctx, nsn, devConfig); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.FromContext(ctx).Error(err, "failed to get DeviceConfig for worker pod", "pod", pod.Name)
		}
		return nil, types.NamespacedName{}, false
	}
	if owner.UID != devConfig.UID {
		return nil, types.NamespacedName{}, false
	}
	return devConfig, nsn, true
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
	isKMMBuildOrSign := isKMMBuildOrSignPod(labels)
	_, isWorkerMgrPod := labels[utils.WorkerActionLabelKey]
	return isKMMBuildOrSign || isWorkerMgrPod
}
