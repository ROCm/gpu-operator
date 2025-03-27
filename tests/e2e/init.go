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
	"log"
	"os"
)

var (
	initContainerImage     string
	kubeRbacProxyCurlImage string
	exporterImage          string
	exporterImage2         string
	devicePluginImage      string
	nodeLabellerImage      string
	devicePluginImage2     string
	nodeLabellerImage2     string
	testRunnerImage        string
	driverImageRepo        string
)

func init() {
	var ok bool
	// read e2e related env variables
	initContainerImage, ok = os.LookupEnv("E2E_INIT_CONTAINER_IMAGE")
	if !ok {
		log.Fatalf("E2E_INIT_CONTAINER_IMAGE is not defined")
	}
	kubeRbacProxyCurlImage, ok = os.LookupEnv("E2E_KUBE_RBAC_PROXY_CURL_IMAGE")
	if !ok {
		log.Fatalf("E2E_KUBE_RBAC_PROXY_CURL_IMAGE is not defined")
	}
	exporterImage, ok = os.LookupEnv("E2E_EXPORTER_IMAGE")
	if !ok {
		log.Fatalf("E2E_EXPORTER_IMAGE is not defined")
	}
	exporterImage2, ok = os.LookupEnv("E2E_EXPORTER_IMAGE_2")
	if !ok {
		log.Fatalf("E2E_EXPORTER_IMAGE_2 is not defined")
	}
	devicePluginImage, ok = os.LookupEnv("E2E_DEVICE_PLUGIN_IMAGE")
	if !ok {
		log.Fatalf("E2E_DEVICE_PLUGIN_IMAGE is not defined")
	}
	nodeLabellerImage, ok = os.LookupEnv("E2E_NODE_LABELLER_IMAGE")
	if !ok {
		log.Fatalf("E2E_NODE_LABELLER_IMAGE is not defined")
	}
	devicePluginImage2, ok = os.LookupEnv("E2E_DEVICE_PLUGIN_IMAGE_2")
	if !ok {
		log.Fatalf("E2E_DEVICE_PLUGIN_IMAGE_2 is not defined")
	}
	nodeLabellerImage2, ok = os.LookupEnv("E2E_NODE_LABELLER_IMAGE_2")
	if !ok {
		log.Fatalf("E2E_NODE_LABELLER_IMAGE_2 is not defined")
	}
	testRunnerImage, ok = os.LookupEnv("E2E_TEST_RUNNER_IMAGE")
	if !ok {
		log.Fatalf("E2E_TEST_RUNNER_IMAGE is not defined")
	}
	driverImageRepo, ok = os.LookupEnv("E2E_DRIVER_IMAGE_REPO")
	if !ok {
		log.Fatalf("E2E_DRIVER_IMAGE_REPO is not defined. Please prepare a iamge registry repo to store your driver image and put the image repo URL into E2E_DRIVER_IMAGE_REPO. E.g. docker.io/<your username>/amdgpu-driver-image")
	}
}
