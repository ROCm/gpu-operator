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
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type NodeKernelVersionPredicate struct {
	predicate.Funcs
}

func (NodeKernelVersionPredicate) Create(e event.CreateEvent) bool {
	return true
}

func (NodeKernelVersionPredicate) Update(e event.UpdateEvent) bool {
	oldNode, okOld := e.ObjectOld.(*v1.Node)
	newNode, okNew := e.ObjectNew.(*v1.Node)
	if !okOld || !okNew {
		return false
	}

	oldKernelVersion := oldNode.Status.NodeInfo.KernelVersion
	newKernelVersion := newNode.Status.NodeInfo.KernelVersion

	// if kernel version changed
	// reconcile the deviceconfig to update kernel mapping of KMM CR
	return oldKernelVersion != newKernelVersion
}

func (NodeKernelVersionPredicate) Delete(e event.DeleteEvent) bool {
	return true
}
