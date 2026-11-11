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

package watchers

import (
	"context"
	"testing"

	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
	"github.com/go-logr/logr"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeWatcherLeavesVFWorkerStartToReconciliation(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := &NodeEventHandler{workerMgr: workermgr.NewMockWorkerMgrAPI(ctrl)}
	oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}
	newNode := oldNode.DeepCopy()
	newNode.Labels = map[string]string{"gpu.operator.amd.com/test.dc.driver": utils.DriverTypeVFPassthrough}
	h.handlePostProcess(context.Background(), logr.Discard(), oldNode, newNode)
}
