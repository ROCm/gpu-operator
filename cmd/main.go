/*
Copyright 2022.

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

package main

import (
	"flag"

	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
	"github.com/ROCm/gpu-operator/internal/cmd"
	"github.com/ROCm/gpu-operator/internal/config"
	"github.com/ROCm/gpu-operator/internal/configmanager"
	"github.com/ROCm/gpu-operator/internal/controllers"
	"github.com/ROCm/gpu-operator/internal/kmmmodule"
	"github.com/ROCm/gpu-operator/internal/metricsexporter"
	"github.com/ROCm/gpu-operator/internal/nodelabeller"
	"github.com/ROCm/gpu-operator/internal/testrunner"
	//+kubebuilder:scaffold:imports
)

var (
	GitCommit = "undefined"
	Version   = "undefined"
	BuildTag  = "undefined"
	scheme    = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gpuev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kmmv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	logConfig := textlogger.NewConfig()
	logConfig.AddFlags(flag.CommandLine)

	var configFile string

	flag.StringVar(&configFile, "config", "", "The path to the configuration file.")

	flag.Parse()

	logger := textlogger.NewLogger(logConfig).WithName("amd-gpu")

	ctrl.SetLogger(logger)

	setupLogger := logger.WithName("setup")

	setupLogger.Info("Creating manager", "version", Version, "git commit", GitCommit, "build tag", BuildTag)

	setupLogger.Info("Parsing configuration file", "path", configFile)

	cfg, err := config.ParseFile(configFile)
	if err != nil {
		cmd.FatalError(setupLogger, err, "could not parse the configuration file", "path", configFile)
	}

	options := cfg.ManagerOptions()
	options.Scheme = scheme

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), *options)
	if err != nil {
		cmd.FatalError(setupLogger, err, "unable to create manager")
	}

	client := mgr.GetClient()
	isOpenShift := utils.IsOpenShift(setupLogger)
	kmmHandler := kmmmodule.NewKMMModule(client, scheme, isOpenShift)
	nlHandler := nodelabeller.NewNodeLabeller(scheme, isOpenShift)
	metricsHandler := metricsexporter.NewMetricsExporter(scheme)
	testrunnerHandler := testrunner.NewTestRunner(scheme)
	configmanagerHandler := configmanager.NewConfigManager(scheme)
	dcr := controllers.NewDeviceConfigReconciler(
		mgr.GetConfig(),
		client,
		kmmHandler,
		nlHandler,
		metricsHandler,
		testrunnerHandler,
		configmanagerHandler)
	if err = dcr.SetupWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.DeviceConfigReconcilerName)
	}

	ctx := ctrl.SetupSignalHandler()

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up health check")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up ready check")
	}

	setupLogger.Info("starting manager")
	if err = mgr.Start(ctx); err != nil {
		cmd.FatalError(setupLogger, err, "problem running manager")
	}
}
