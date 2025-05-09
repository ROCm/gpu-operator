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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/controllers/workermgr"
)

func (s *E2ESuite) verifyVFIOReadyLabel(devCfg *v1alpha1.DeviceConfig, expectLabel bool, c *C) {
	assert.Eventually(c, func() bool {
		nodes, err := s.clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
			LabelSelector: func() string {
				s := []string{}
				for k, v := range devCfg.Spec.Selector {
					s = append(s, fmt.Sprintf("%v=%v", k, v))
				}
				return strings.Join(s, ",")
			}(),
		})
		if err != nil {
			logger.Errorf("failed to get nodes %v", err)
			return false
		}

		workerMgr := workermgr.NewWorkerMgr(nil, nil)
		vfioReadyLabel := workerMgr.GetWorkReadyLabel(types.NamespacedName{
			Namespace: devCfg.Namespace,
			Name:      devCfg.Name,
		})
		for _, node := range nodes.Items {
			if expectLabel {
				if _, ok := node.Labels[vfioReadyLabel]; !ok {
					logger.Errorf("cannot find vfio ready label on node %v", node.Name)
					return false
				}
			} else {
				if _, ok := node.Labels[vfioReadyLabel]; ok {
					logger.Errorf("vfio ready label still exists on node %v", node.Name)
					return false
				}
			}
		}
		return true
	}, 5*time.Minute, 3*time.Second)
}

func (s *E2ESuite) TestVFPassthroughDeployment(c *C) {
	// only run this test case when all the worker node has AMD GPU model supported by GIM driver for VF Passthrough
	if s.simEnable {
		c.Skip("Skipping for non amd gpu testbed")
	}
	_, err := s.dClient.DeviceConfigs(s.ns).Get(s.cfgName, metav1.GetOptions{})
	assert.Errorf(c, err, fmt.Sprintf("config %v exists", s.cfgName))

	logger.Infof("create %v", s.cfgName)
	devCfg := s.getDeviceConfig(c)

	enableDriver := true
	enableNodeLabeller := true
	devCfg.Spec.Driver.Enable = &enableDriver
	devCfg.Spec.Driver.DriverType = utils.DriverTypeVFPassthrough
	devCfg.Spec.Driver.Version = "8.0.0.K" // this is the version of the first opensource version GIM driver
	devCfg.Spec.DevicePlugin.EnableNodeLabeller = &enableNodeLabeller
	devCfg.Spec.DevicePlugin.DevicePluginImage = kubeVirtHostDevicePluginImage
	devCfg.Spec.DevicePlugin.NodeLabellerImage = kubeVirtHostNodeLabellerImage

	s.createDeviceConfig(devCfg, c)
	s.checkNFDWorkerStatus(s.ns, c, "")
	s.checkNodeLabellerStatus(s.ns, c, devCfg)
	s.checkMetricsExporterStatus(devCfg, s.ns, v1.ServiceTypeClusterIP, c)
	s.verifyDeviceConfigStatus(devCfg, c)
	s.verifyNodeGPULabel(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, true, c)

	// delete
	s.deleteDeviceConfig(devCfg, c)
	s.verifyVFIOReadyLabel(devCfg, false, c)
}
