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

package gpuope2e

import (
	"context"

	"github.com/ROCm/gpu-operator/tests/k8s-e2e/clients"
	restclient "k8s.io/client-go/rest"
)

// E2ESuite holds configuration shared across all GPU Operator e2e tests.
type E2ESuite struct {
	k8sclient  *clients.K8sClient
	helmClient *clients.HelmClient
	restConfig *restclient.Config
	ns         string
	kubeconfig string

	// GPU Operator install parameters.
	operatorChart string
	operatorTag   string
	helmSet       []string

	existingDeploy bool // true: skip install/teardown (verify only)

	// suiteHook is an optional callback invoked at the end of SetUpSuite.
	suiteHook func(ctx context.Context) error
}
