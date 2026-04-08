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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ROCm/gpu-operator/internal/validator"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gpuev1alpha1.AddToScheme(scheme))
}

func main() {
	var namespace string
	var deviceConfigName string
	var enableDebug bool

	flag.StringVar(&namespace, "namespace", "", "Namespace of the DeviceConfig")
	flag.StringVar(&deviceConfigName, "deviceconfig-name", "", "Name of the DeviceConfig to validate")
	flag.BoolVar(&enableDebug, "debug", false, "Enable debug logging")
	flag.Parse()

	// Setup logging
	opts := zap.Options{
		Development: enableDebug,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Validate required flags
	if namespace == "" {
		setupLog.Error(fmt.Errorf("namespace is required"), "missing required flag")
		os.Exit(1)
	}
	if deviceConfigName == "" {
		setupLog.Error(fmt.Errorf("deviceconfig-name is required"), "missing required flag")
		os.Exit(1)
	}

	setupLog.Info("Starting GPU Operator Cluster Validator",
		"namespace", namespace,
		"deviceconfig", deviceConfigName,
	)

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		setupLog.Error(err, "failed to get in-cluster config")
		os.Exit(1)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "failed to create Kubernetes client")
		os.Exit(1)
	}

	// Create cluster validator
	v := validator.NewClusterValidator(k8sClient, namespace, deviceConfigName)

	// Run validation
	ctx := context.Background()
	if err := v.Validate(ctx); err != nil {
		setupLog.Error(err, "validation failed")
		os.Exit(1)
	}

	setupLog.Info("Validation completed successfully")
	os.Exit(0)
}
