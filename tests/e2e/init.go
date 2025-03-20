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
	driverImageRepo string
)

func init() {
	var ok bool
	driverImageRepo, ok = os.LookupEnv("E2E_DRIVER_IMAGE_REPO")
	if !ok {
		log.Fatalf("E2E_DRIVER_IMAGE_REPO is not defined. Please prepare a iamge registry repo to store your driver image and put the image repo URL into E2E_DRIVER_IMAGE_REPO. E.g. docker.io/<your username>/amdgpu-driver-image")
	}
}
