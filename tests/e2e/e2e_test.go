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
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ROCm/gpu-operator/tests/e2e/utils"
	"github.com/stretchr/testify/assert"

	"github.com/ROCm/gpu-operator/tests/e2e/client"
	monitoringClient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
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
var cfgName = flag.String("deviceConfigName", "deviceconfig-example", "deviceConfig name")
var registry = flag.String("registry", "10.11.18.9:5000/ubuntu:amdgpu-6.3.3", "driver container registry")
var driverVersion = flag.String("driverVersion", "6.3.3", "the default driver version for e2e test")
var openshift = flag.Bool("openshift", false, "openshift deployment")
var simEnable = flag.Bool("simEnable", false, "testbed without amd gpus")
var ciEnv = flag.Bool("ciEnv", false, "testbed for CI environment")

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
	s.cfgName = *cfgName
	s.registry = *registry
	s.defaultDriverVersion = *driverVersion
	s.openshift = *openshift
	s.simEnable = *simEnable
	s.ciEnv = *ciEnv

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

	err = apiextv1.AddToScheme(scheme.Scheme)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}

	// creates the clientset
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}
	s.clientSet = cs

	// create the apiext clientset
	apiClientSet, err := apiextClient.NewForConfig(config)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}
	s.apiClientSet = apiClientSet

	// create the monitoring client
	monClient, err := monitoringClient.NewForConfig(config)
	if err != nil {
		c.Fatalf("Error: %v", err.Error())
	}
	s.monClient = monClient

	s.clusterType = utils.GetClusterType(config)

	if s.openshift == false {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmDeployment(cs, s.ns, true); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	} else {
		assert.Eventually(c, func() bool {
			if err := utils.CheckHelmOCDeployment(cs, true); err != nil {
				logger.Infof("%v", err)
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second)
	}
	if s.ciEnv {
		if err := utils.PatchKMMDeploymentWithCIENVFlag(s.clientSet); err != nil {
			c.Fatalf("%v", err)
		}
		if err := utils.PatchOperatorControllerDeploymentWithCIENVFlag(s.clientSet); err != nil {
			c.Fatalf("%v", err)
		}
	}
}

func (s *E2ESuite) SetUpTest(c *C) {
	logger.Infof("==================== Starting Test: %s ====================", c.TestName())
	if err := s.clientSet.CoreV1().ConfigMaps(s.ns).Delete(context.TODO(), s.cfgName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Warnf("Failed to delete configmap %s during SetUpTest for test %s: %v", s.cfgName, c.TestName(), err)
	}
}

func (s *E2ESuite) TearDownTest(c *C) {
	logger.Info("TearDownTest:")
	if l, err := s.dClient.DeviceConfigs(s.ns).List(metav1.ListOptions{}); err == nil {
		for _, cfg := range l.Items {
			logger.Infof("delete %v", cfg.Name)
			if _, err := s.dClient.DeviceConfigs(s.ns).Delete(cfg.Name); err != nil {
				logger.Errorf("Error deleting deviceconfig %s during TearDownTest for test %s: %v", cfg.Name, c.TestName(), err)
				c.Fatalf("Error: %v", err.Error())
			}
		}

		if len(l.Items) > 0 && !s.simEnable {
			nodes := utils.GetAMDGpuWorker(s.clientSet, s.openshift)
			if err := utils.HandleNodesReboot(context.TODO(), s.clientSet, nodes); err != nil {
				logger.Errorf("Error handling node reboot during TearDownTest for test %s: %v", c.TestName(), err)
				c.Fatalf("Error: %v", err.Error())
			}
		}

	} else {
		logger.Warnf("Failed to list deviceconfigs during TearDownTest for test %s: %v", c.TestName(), err)
	}

	if err := s.clientSet.CoreV1().ConfigMaps(s.ns).Delete(context.TODO(), s.cfgName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Warnf("Failed to delete configmap %s at the end of TearDownTest for test %s: %v", s.cfgName, c.TestName(), err)
	}
	time.Sleep(20 * time.Second)
	logger.Infof("==================== Finished Test: %s ====================", c.TestName())
}

func (s *E2ESuite) TearDownSuite(c *C) {
	logger.Info("TearDownSuite:")
	if l, err := s.dClient.DeviceConfigs(s.ns).List(metav1.ListOptions{}); err == nil {
		for _, cfg := range l.Items {
			logger.Infof("delete %v", cfg.Name)
			if _, err := s.dClient.DeviceConfigs(s.ns).Delete(cfg.Name); err != nil {
				c.Fatalf("Error: %v", err.Error())
			}
		}
		time.Sleep(30 * time.Second)
	}
	_ = s.clientSet.CoreV1().ConfigMaps(s.ns).Delete(context.TODO(), s.cfgName, metav1.DeleteOptions{})

}
