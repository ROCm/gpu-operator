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

package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//go:generate mockgen -source=pod_event_handler.go -package=controllers -destination=mock_pod_event_handler.go podEventHandlerAPI
type podEventHandlerAPI interface {
	Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface)
	Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface)
	Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface)
	Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface)
}

func newPodEventHandler(client client.Client) podEventHandlerAPI {
	return &PodEventHandler{
		client: client,
	}
}

type PodEventHandler struct {
	client client.Client
}

// Create handle pod create event
func (h *PodEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	// Handle create event if needed
}

// Delete handle pod delete event
func (h *PodEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	// Handle delete event if needed
}

// Create handle pod generic event
func (h *PodEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	// Handle generic event if needed
}

// Update handle pod update event
func (h *PodEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	// Handle update event
	pod, ok := evt.ObjectNew.(*v1.Pod)
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
}

type PodLabelPredicate struct {
	predicate.Funcs
}

func (PodLabelPredicate) Update(e event.UpdateEvent) bool {
	return hasBuilderLabel(e.ObjectNew)
}

func hasBuilderLabel(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	value, exists := labels["kmm.node.kubernetes.io/pod-type"]
	return exists && value == "builder"
}
