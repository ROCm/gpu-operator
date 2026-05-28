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
	"time"

	. "gopkg.in/check.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DriverInstallSuite groups the driver-install and driver-upgrade tests so
// they execute on a clean cluster (own SetUpSuite wipes any leftover
// DeviceConfig + driver state) and tear driver state down at end-of-suite so
// downstream chunks (cluster-rbac, dcm, dra, exporter, kubevirt, npd,
// remediation, testrunner) don't inherit a polluted cluster.
//
// E2ESuite is embedded by value: fields and helper methods are promoted, so
// the moved test bodies compile unchanged. Gocheck dispatches SetUp/TearDown
// per-suite (no promotion), so the hooks declared here override E2ESuite's
// without inheriting them; explicit delegations to s.E2ESuite.* preserve the
// original bootstrap behavior.
type DriverInstallSuite struct {
	E2ESuite
}

var _ = Suite(&DriverInstallSuite{})

func (s *DriverInstallSuite) SetUpSuite(c *C) {
	// Full E2ESuite bootstrap (kubeconfig, clients, testMonitor, helm
	// readiness, CaptureRunnerBaseline).
	s.E2ESuite.SetUpSuite(c)

	// Driver-install tests must start on a cluster with no lingering
	// DeviceConfigs from a previous run; otherwise upgrade tests double-
	// install or read stale CR state. Best-effort cleanup — never Fatalf.
	wipeDriverState(s, "SetUpSuite")
}

func (s *DriverInstallSuite) SetUpTest(c *C) {
	s.E2ESuite.SetUpTest(c)
}

func (s *DriverInstallSuite) TearDownTest(c *C) {
	// E2ESuite.TearDownTest already calls CaptureDriverState on c.Failed()
	// and runs the DeviceConfig delete loop without Fatalf.
	s.E2ESuite.TearDownTest(c)
}

func (s *DriverInstallSuite) TearDownSuite(c *C) {
	// Leave the cluster clean for downstream chunks.
	wipeDriverState(s, "TearDownSuite")
	s.E2ESuite.TearDownSuite(c)
}

// wipeDriverState deletes any DeviceConfig left behind by a prior chunk and
// waits a short bounded interval for driver daemonsets to drain. Logs at
// Error level on failure but never aborts — cleanup is best-effort by design.
func wipeDriverState(s *DriverInstallSuite, where string) {
	if s.dClient == nil {
		return
	}
	l, err := s.dClient.DeviceConfigs(s.ns).List(metav1.ListOptions{})
	if err != nil {
		logger.Errorf("DriverInstallSuite.%s: list DeviceConfigs failed: %v", where, err)
		return
	}
	for _, cfg := range l.Items {
		logger.Infof("DriverInstallSuite.%s: delete DeviceConfig %s", where, cfg.Name)
		if _, err := s.dClient.DeviceConfigs(s.ns).Delete(cfg.Name); err != nil {
			logger.Errorf("DriverInstallSuite.%s: delete DeviceConfig %s failed: %v", where, cfg.Name, err)
		}
	}
	if len(l.Items) > 0 {
		// Bounded wait — give the operator a chance to drain its
		// daemonsets before the next test installs a fresh CR.
		time.Sleep(30 * time.Second)
	}
	// Configmap cleanup mirrors E2ESuite.TearDownTest's behavior.
	_ = s.clientSet.CoreV1().ConfigMaps(s.ns).Delete(context.TODO(), s.cfgName, metav1.DeleteOptions{})
}
