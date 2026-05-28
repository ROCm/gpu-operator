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

package utils

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCaptureRunnerBaseline_WritesFile(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "dind-cluster-1c2w-worker"},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion:           "5.15.0-171-generic",
					OSImage:                 "Ubuntu 22.04.4 LTS",
					ContainerRuntimeVersion: "containerd://1.7.0",
				},
			},
		},
	)

	dir := t.TempDir()
	if err := CaptureRunnerBaseline(context.TODO(), cs, dir); err != nil {
		t.Fatalf("CaptureRunnerBaseline failed: %v", err)
	}

	out := filepath.Join(dir, "_baseline", "runner-state.txt")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("baseline file not written: %v", err)
	}
	s := string(data)
	for _, want := range []string{"kernel: 5.15.0-171-generic", "os: Ubuntu 22.04.4 LTS", "node: dind-cluster-1c2w-worker"} {
		if !strings.Contains(s, want) {
			t.Errorf("baseline missing %q\nGot:\n%s", want, s)
		}
	}
}

func TestCaptureDriverState_WritesArtifacts(t *testing.T) {
	// Production code passes c.TestName() which is the gocheck
	// fully-qualified name "<Suite>.<Method>". The renderer (chunk_report.py)
	// looks up artifacts under the leaf component only. Exercise both
	// suite prefixes so a regression in either dispatch path is caught.
	for _, fqn := range []string{"E2ESuite.TestDeployment", "DriverInstallSuite.TestDeployment"} {
		t.Run(fqn, func(t *testing.T) {
			cs := fake.NewSimpleClientset(
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kmm-worker-dind-cluster-1c2w-worker-deviceconfig-example",
						Namespace: "kube-amd-gpu",
						Labels:    map[string]string{"app.kubernetes.io/name": "kmm-worker"},
					},
					Spec: v1.PodSpec{
						NodeName:   "dind-cluster-1c2w-worker",
						Containers: []v1.Container{{Name: "worker", Image: "172.17.0.2:5000/root-e2e:ubuntu-22.04-5.15.0-171-generic-6.3.3"}},
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				},
			)

			dir := t.TempDir()
			tm := NewTestMonitor(cs, "kube-amd-gpu", dir)

			if err := tm.CaptureDriverState(fqn); err != nil {
				t.Fatalf("CaptureDriverState(%q) failed: %v", fqn, err)
			}

			// On disk must be the leaf, NOT the fully-qualified name.
			out := filepath.Join(dir, "TestDeployment", "driver-state.txt")
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("expected leaf-form artifact at %s, got: %v", out, err)
			}
			if !strings.Contains(string(data), "kmm-worker-dind-cluster-1c2w-worker-deviceconfig-example") {
				t.Errorf("driver-state missing kmm-worker pod name:\n%s", string(data))
			}
			if !strings.Contains(string(data), "172.17.0.2:5000/root-e2e:ubuntu-22.04-5.15.0-171-generic-6.3.3") {
				t.Errorf("driver-state missing resolved image tag:\n%s", string(data))
			}

			// Negative: the FQN-named directory must NOT be created.
			if _, err := os.Stat(filepath.Join(dir, fqn)); !os.IsNotExist(err) {
				t.Errorf("unexpected FQN-named directory %q (err=%v); leaf normalization missing", fqn, err)
			}
		})
	}
}

func TestLeafTestName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"E2ESuite.TestDeployment", "TestDeployment"},
		{"DriverInstallSuite.TestDriverUpgradeByPushingNewCR", "TestDriverUpgradeByPushingNewCR"},
		{"TestPlain", "TestPlain"},
		{"Pkg.Sub.Name", "Name"},
		{"with spaces", "with_spaces"},
		{"slash/inside", "slash_inside"},
		{"dots.then/slash", "then_slash"},
		{"", ""},
		// Pathological forms — never produced by c.TestName() but exercised
		// to lock in the "no chars after the dot → keep input, just sanitize"
		// branch so future refactors don't silently change the semantics.
		{".", "_"},
		{"trailing.", "trailing_"},
	}
	for _, c := range cases {
		got := leafTestName(c.in)
		if got != c.want {
			t.Errorf("leafTestName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
