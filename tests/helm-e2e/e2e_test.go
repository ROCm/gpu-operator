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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/ROCm/gpu-operator/tests/e2e/client"
)

var logger = logrus.Logger{
	Out: os.Stdout,
	Formatter: &logrus.TextFormatter{
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			return fmt.Sprintf("%v()", f.Function), fmt.Sprintf("%v:%v", path.Base(f.File), f.Line)
		},
	},
	Hooks:        make(logrus.LevelHooks),
	Level:        logrus.InfoLevel,
	ExitFunc:     os.Exit,
	ReportCaller: true,
}

var kubeConfig = flag.String("kubeConfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "absolute path to the kubeconfig file")
var helmChart = flag.String("helmchart", "", "helmchart")
var operatorNS = flag.String("namespace", "kube-amd-gpu", "namespace")

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

var _ = Suite(&E2ESuite{})

func (s *E2ESuite) SetUpSuite(c *C) {
	logger.Infof("setupSuite:")
	s.helmChart = *helmChart
	s.kubeconfig = *kubeConfig
	s.ns = *operatorNS

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", s.kubeconfig)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}

	dcCli, err := client.Client(config)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}
	s.dClient = dcCli

	// creates the clientset
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}
	s.clientSet = cs

	cmd := exec.Command("helm", "delete", releaseName, "-n", s.ns)
	logger.Info(fmt.Sprintf("cleaning up leftover helm chart in case test case forgot to uninstall: %+v", cmd.String()))
	err = cmd.Run()
	if err != nil {
		logger.Info(fmt.Sprintf("pre-suite helm clean up return %+v, ignoring the error to execute test cases", err))
	}
}

func (s *E2ESuite) SetUpTest(c *C) {
	logger.Info("setupTest:")
}
func (s *E2ESuite) TearDownTest(c *C) {
	logger.Info("TearDownTest:")
	cmd := exec.Command("helm", "delete", releaseName, "-n", s.ns)
	logger.Info(fmt.Sprintf("cleaning up leftover helm chart in case test case forgot to uninstall: %+v", cmd.String()))
	err := cmd.Run()
	if err != nil {
		logger.Info(fmt.Sprintf("post-test helm clean up return %+v, ignoring the error to execute the next test case", err))
	}

	devCfgList, err := s.dClient.DeviceConfigs(s.ns).List(v1.ListOptions{})
	if err == nil {
		assert.Error(c, err, "expect error for listing DeviceConfig but got nil, CRD should have been deleted")
	}
	if devCfgList != nil {
		assert.True(c, len(devCfgList.Items) == 0, fmt.Sprintf("expect all DeviceConfig got deleted after uninstalling helm chart but got %+v", devCfgList.Items))
	}
}

func (s *E2ESuite) TearDownSuite(c *C) {
	logger.Info("TearDownSuite:")
}
