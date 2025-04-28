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

package utils

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	expectedAllLabelKeys = map[string]bool{
		"amd.com/gpu.family":                     true,
		"amd.com/gpu.driver-version":             true,
		"amd.com/gpu.driver-src-version":         true,
		"amd.com/gpu.firmware":                   true,
		"amd.com/gpu.device-id":                  true,
		"amd.com/gpu.product-name":               true,
		"amd.com/gpu.vram":                       true,
		"amd.com/gpu.simd-count":                 true,
		"amd.com/gpu.cu-count":                   true,
		"amd.com/compute-partitioning-supported": true,
		"amd.com/memory-partitioning-supported":  true,
		"amd.com/compute-memory-partition":       true,
	}
	expectedAllExperimentalLabelKeys = map[string]bool{
		"beta.amd.com/gpu.family":             true,
		"beta.amd.com/gpu.driver-version":     true,
		"beta.amd.com/gpu.driver-src-version": true,
		"beta.amd.com/gpu.firmware":           true,
		"beta.amd.com/gpu.device-id":          true,
		"beta.amd.com/gpu.product-name":       true,
		"beta.amd.com/gpu.vram":               true,
		"beta.amd.com/gpu.simd-count":         true,
		"beta.amd.com/gpu.cu-count":           true,
	}
)

func TestInitLabelLists(t *testing.T) {
	labelMap := map[string]bool{}
	for _, label := range allAMDComLabels {
		labelMap[label] = true
	}
	if !reflect.DeepEqual(labelMap, expectedAllLabelKeys) {
		t.Errorf("failed to get expected all labels during init, got %+v, expect %+v", labelMap, expectedAllLabelKeys)
	}
	experimentalLabelMap := map[string]bool{}
	for _, label := range allBetaAMDComLabels {
		experimentalLabelMap[label] = true
	}
	if !reflect.DeepEqual(experimentalLabelMap, expectedAllExperimentalLabelKeys) {
		t.Errorf("failed to get expected all experimental labels during init, got %+v, expect %+v", labelMap, expectedAllLabelKeys)
	}
}

func TestRemoveOldNodeLabels(t *testing.T) {
	testCases := []struct {
		inputNode     *v1.Node
		expectLabels  map[string]string
		expectUpdated bool
	}{
		{
			inputNode: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"amd.com/gpu.cu-count":                          "104",
						"amd.com/gpu.device-id":                         "740f",
						"amd.com/gpu.driver-version":                    "6.10.5",
						"amd.com/gpu.family":                            "AI",
						"amd.com/gpu.product-name":                      "Instinct_MI210",
						"amd.com/gpu.simd-count":                        "416",
						"amd.com/gpu.vram":                              "64G",
						"beta.amd.com/gpu.cu-count":                     "104",
						"beta.amd.com/gpu.cu-count.104":                 "1",
						"beta.amd.com/gpu.device-id":                    "740f",
						"beta.amd.com/gpu.device-id.740f":               "1",
						"beta.amd.com/gpu.family":                       "HPC",
						"beta.amd.com/gpu.family.HPC":                   "1",
						"beta.amd.com/gpu.product-name":                 "Instinct_MI300X",
						"beta.amd.com/gpu.product-name.Instinct_MI300X": "1",
						"beta.amd.com/gpu.simd-count":                   "416",
						"beta.amd.com/gpu.simd-count.416":               "1",
						"beta.amd.com/gpu.vram":                         "64G",
						"beta.amd.com/gpu.vram.64G":                     "1",
						"dummyLabel1":                                   "1",
						"dummyLabel2":                                   "2",
						// users may want to use label like this to integrate with their system
						"amd.com/gpu": "true",
					},
				},
			},
			expectLabels: map[string]string{
				"dummyLabel1": "1",
				"dummyLabel2": "2",
				"amd.com/gpu": "true",
			},
			expectUpdated: true,
		},
		{
			inputNode: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						// users may want to use label like this to integrate with their system
						"amd.com/gpu":                         "true",
						"feature.node.kubernetes.io/amd-gpu":  "true",
						"feature.node.kubernetes.io/amd-vgpu": "true",
						"kubernetes.io/host-name":             "node",
					},
				},
			},
			expectLabels: map[string]string{
				"amd.com/gpu":                         "true",
				"feature.node.kubernetes.io/amd-gpu":  "true",
				"feature.node.kubernetes.io/amd-vgpu": "true",
				"kubernetes.io/host-name":             "node",
			},
			expectUpdated: false,
		},
	}

	for _, tc := range testCases {
		updated := RemoveOldNodeLabels(tc.inputNode)
		if !reflect.DeepEqual(tc.inputNode.Labels, tc.expectLabels) {
			t.Errorf("failed to get expected node labels after removing old labels, got %+v, expect %+v", tc.inputNode.Labels, tc.expectLabels)
		}
		if updated != tc.expectUpdated {
			t.Errorf("failed to get expected node labels updated flag, got %+v, expect %+v", updated, tc.expectUpdated)
		}
	}
}
