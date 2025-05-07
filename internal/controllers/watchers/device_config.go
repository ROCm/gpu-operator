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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SpecChangedOrDeletionPredicate implements predicate.Predicate interface.
// triggering reconciliation
// only if the Spec field has changed or the DeletionTimestamp has been updated.
type SpecChangedOrDeletionPredicate struct {
	predicate.Funcs
}

// Update implements the update event filter for Spec or DeletionTimestamp changes.
func (SpecChangedOrDeletionPredicate) Update(e event.UpdateEvent) bool {
	// 1. Check if the DeletionTimestamp has changed.
	// This catches the case where the object is marked for deletion.
	if (e.ObjectOld.GetDeletionTimestamp() == nil && e.ObjectNew.GetDeletionTimestamp() != nil) ||
		(e.ObjectOld.GetDeletionTimestamp() != nil && e.ObjectNew.GetDeletionTimestamp() == nil) {
		return true // Reconcile when deletion starts
	}

	// 2. Check if the Spec has changed.
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}

	// If neither DeletionTimestamp nor Spec changed, don't reconcile.
	return false
}

// Create returns true, allowing reconciliation when a new resource is created.
func (SpecChangedOrDeletionPredicate) Create(e event.CreateEvent) bool {
	return true
}

// Delete returns true, allowing reconciliation when a resource is deleted.
func (SpecChangedOrDeletionPredicate) Delete(e event.DeleteEvent) bool {
	return true
}
