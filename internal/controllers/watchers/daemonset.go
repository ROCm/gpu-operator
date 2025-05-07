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
	"strings"

	v1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	workqueue "k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

type DaemonsetPredicate struct {
	predicate.Funcs
}

func ownedByDeviceConfig(obj client.Object) bool {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.Kind == utils.KindDeviceConfig {
			return true
		}
	}
	return false
}

func (DaemonsetPredicate) Update(e event.UpdateEvent) bool {
	return ownedByDeviceConfig(e.ObjectNew)
}

func (DaemonsetPredicate) Generic(e event.GenericEvent) bool {
	return ownedByDeviceConfig(e.Object)
}

func (DaemonsetPredicate) Delete(e event.DeleteEvent) bool {
	return ownedByDeviceConfig(e.Object)
}

//go:generate mockgen -source=daemonset.go -package=watchers -destination=mock_daemonset.go DaemonsetEventHandlerAPI
type DaemonsetEventHandlerAPI interface {
	Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
	Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request])
}

func NewDaemonsetEventHandler(client client.Client) DaemonsetEventHandlerAPI {
	return &DaemonsetEventHandler{
		client: client,
	}
}

type DaemonsetEventHandler struct {
	client client.Client
}

// Create handle create event
func (h *DaemonsetEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	devConfigName := h.fetchOwnerDeviceConfigName(evt.Object)
	if devConfigName == "" {
		// if there is no DeviceConfig owner, stop processing event for this daemonset
		return
	}
	h.patchDeviceConfigNodeStatus(ctx, evt.Object, devConfigName)
}

// Create handle generic event
func (h *DaemonsetEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	devConfigName := h.fetchOwnerDeviceConfigName(evt.Object)
	if devConfigName == "" {
		// if there is no DeviceConfig owner, stop processing event for this daemonset
		return
	}
	h.patchDeviceConfigNodeStatus(ctx, evt.Object, devConfigName)
}

// Delete handle delete event
func (h *DaemonsetEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	devConfigName := h.fetchOwnerDeviceConfigName(evt.Object)
	if devConfigName == "" {
		// if there is no DeviceConfig owner, stop processing event for this daemonset
		return
	}
	h.patchDeviceConfigNodeStatus(ctx, evt.Object, devConfigName)
}

// Update handle update event
func (h *DaemonsetEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	devConfigName := h.fetchOwnerDeviceConfigName(evt.ObjectNew)
	if devConfigName == "" {
		// if there is no DeviceConfig owner, stop processing event for this daemonset
		return
	}
	h.patchDeviceConfigNodeStatus(ctx, evt.ObjectNew, devConfigName)
	// if the managed daemonset got spec changed or deleted, reconcile the owner DeviceConfig
	if evt.ObjectOld.GetGeneration() != evt.ObjectNew.GetGeneration() ||
		(evt.ObjectOld.GetDeletionTimestamp() == nil && evt.ObjectNew.GetDeletionTimestamp() != nil) ||
		(evt.ObjectOld.GetDeletionTimestamp() != nil && evt.ObjectNew.GetDeletionTimestamp() == nil) {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: evt.ObjectNew.GetNamespace(),
				Name:      devConfigName,
			},
		})
	}
}

func (h *DaemonsetEventHandler) patchDeviceConfigNodeStatus(ctx context.Context, obj client.Object, devConfigName string) {
	logger := log.FromContext(ctx)
	// whenever NMC object get updated
	// push the NMC status information to corresponding DeviceConfig status
	ds, ok := obj.(*v1.DaemonSet)
	if !ok {
		return
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		devConfig := &v1alpha1.DeviceConfig{}
		err := h.client.Get(ctx, types.NamespacedName{Name: devConfigName, Namespace: ds.Namespace}, devConfig)
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "cannot get DeviceConfig for handling daemonset event",
				"namesace", ds.Namespace, "name", ds.Name)
			return err
		}

		latestDS := &v1.DaemonSet{}
		err = h.client.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, latestDS)
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "cannot fetch daemonset for handling daemonset event",
				"namesace", ds.Namespace, "name", ds.Name)
			return err
		}
		// if err == nil the latest status counter will be pushed to DeviceConfig
		// OR if err == NotFound, zero counter values will be pushed to DeviceConfig

		devConfigCopy := devConfig.DeepCopy()
		update := false
		switch {
		case strings.HasSuffix(latestDS.Name, utils.MetricsExporterNameSuffix):
			update = h.handleMetricsExporterStatus(latestDS, devConfig)
		case strings.HasSuffix(latestDS.Name, utils.DevicePluginNameSuffix):
			update = h.handleDevicePluginStatus(latestDS, devConfig)
		}
		if update {
			err = h.client.Status().Patch(ctx, devConfig, client.MergeFrom(devConfigCopy))
			if err != nil && !k8serrors.IsNotFound(err) {
				logger.Error(err, "cannot patch DeviceConfig status")
			}
			return err
		}
		return nil
	}); err != nil {
		logger.Error(err, fmt.Sprintf("failed to patch device config status for daemonset %+v", ds.Name))
	}
}

func (h *DaemonsetEventHandler) handleMetricsExporterStatus(ds *v1.DaemonSet, devConfig *v1alpha1.DeviceConfig) bool {
	if devConfig.Status.MetricsExporter.AvailableNumber == ds.Status.NumberAvailable &&
		devConfig.Status.MetricsExporter.NodesMatchingSelectorNumber == ds.Status.NumberAvailable &&
		devConfig.Status.MetricsExporter.DesiredNumber == ds.Status.DesiredNumberScheduled {
		// if there is nothing to update, skip the patch operation
		return false
	}
	devConfig.Status.MetricsExporter.AvailableNumber = ds.Status.NumberAvailable
	devConfig.Status.MetricsExporter.NodesMatchingSelectorNumber = ds.Status.NumberAvailable
	devConfig.Status.MetricsExporter.DesiredNumber = ds.Status.DesiredNumberScheduled
	return true
}

func (h *DaemonsetEventHandler) handleDevicePluginStatus(ds *v1.DaemonSet, devConfig *v1alpha1.DeviceConfig) bool {
	if devConfig.Status.DevicePlugin.AvailableNumber == ds.Status.NumberAvailable &&
		devConfig.Status.DevicePlugin.NodesMatchingSelectorNumber == ds.Status.NumberAvailable &&
		devConfig.Status.DevicePlugin.DesiredNumber == ds.Status.DesiredNumberScheduled {
		// if there is nothing to update, skip the patch operation
		return false
	}
	devConfig.Status.DevicePlugin.AvailableNumber = ds.Status.NumberAvailable
	devConfig.Status.DevicePlugin.NodesMatchingSelectorNumber = ds.Status.NumberAvailable
	devConfig.Status.DevicePlugin.DesiredNumber = ds.Status.DesiredNumberScheduled
	return true
}

func (h *DaemonsetEventHandler) fetchOwnerDeviceConfigName(obj client.Object) string {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.Kind == utils.KindDeviceConfig {
			return owner.Name
		}
	}
	return ""
}
