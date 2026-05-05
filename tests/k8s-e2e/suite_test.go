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

// GPU Operator e2e tests.
//
// This package contains GPU Operator lifecycle tests (install, infra verification,
// GPU workload, teardown) that exercise the full operator stack with DME deployed
// as a managed DaemonSet.
//
// Run full install+verify+teardown:
//
//	go test -v -failfast \
//	  -operatorchart helm-charts-k8s \
//	  -operatortag v1.4.1 \
//	  -kubeconfig /path/to/kubeconfig \
//	  -test.timeout 60m
//
// Run verify only against a pre-deployed cluster:
//
//	go test -v -failfast -existing \
//	  -kubeconfig /path/to/kubeconfig \
//	  -check.f 'TestOp010|TestOp020|TestOp030|TestOp040|TestOp050|TestOp060|TestOp065|TestOp070' \
//	  -test.timeout 30m

package gpuope2e

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ROCm/gpu-operator/tests/k8s-e2e/clients"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// sliceFlag is a repeatable string flag (used for -helmset).
type sliceFlag []string

func (f *sliceFlag) String() string { return fmt.Sprintf("%v", []string(*f)) }
func (f *sliceFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

var kubeConfig = flag.String("kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "absolute path to the kubeconfig file")
var operatorNS = flag.String("namespace", "kube-amd-gpu", "namespace for GPU Operator deployment")
var operatorChart = flag.String("operatorchart", "helm-charts-k8s", "GPU Operator helm chart (local path, OCI ref, or repo/chart)")
var operatorTag = flag.String("operatortag", "v1.4.1", "GPU Operator chart version/tag")
var existingDeploy = flag.Bool("existing", false, "when true, skip install/teardown (verify only)")
var noTeardown = flag.Bool("noteardown", false, "when true, skip TearDownSuite namespace deletion (leave operator installed)")
var workloadImage = flag.String("workloadimage", "rocm/rocm-terminal:latest", "ROCm workload image for Op070 GPU workload test")
var helmSet sliceFlag

func init() {
	flag.Var(&helmSet, "helmset", "extra helm --set override (repeatable, e.g. -helmset foo=bar)")
}

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	TestingT(t)
}

var suite = &E2ESuite{}
var _ = Suite(suite)

func (s *E2ESuite) SetUpSuite(c *C) {
	log.Print("SetUpSuite:")
	s.kubeconfig = *kubeConfig
	s.ns = *operatorNS
	s.operatorChart = *operatorChart
	s.operatorTag = *operatorTag
	s.existingDeploy = *existingDeploy
	s.helmSet = []string(helmSet)
	ctx := context.Background()

	config, err := clientcmd.BuildConfigFromFlags("", s.kubeconfig)
	c.Assert(err, IsNil)
	s.restConfig = config

	cs, err := clients.NewK8sClient(config)
	c.Assert(err, IsNil)
	s.k8sclient = cs

	hClient, err := clients.NewHelmClient(
		clients.WithNameSpaceOption(s.ns),
		clients.WithKubeConfigOption(config),
	)
	c.Assert(err, IsNil)
	s.helmClient = hClient

	if s.existingDeploy {
		log.Printf("SetUpSuite: existing deploy mode — skipping namespace delete/create (ns=%s)", s.ns)
	} else {
		log.Printf("SetUpSuite: deleting namespace %s (if exists)", s.ns)
		if err = s.k8sclient.DeleteNamespaceAndWait(ctx, s.ns, "", 3*time.Minute); err != nil {
			log.Printf("SetUpSuite: namespace delete/wait: %v (continuing)", err)
		}
		// Clean up cluster-scoped resources from any previous operator or cert-manager release
		// (including differently-named releases like "amd-gpu-operator").
		for _, oldRelease := range []string{operatorReleaseName, "amd-gpu-operator", certManagerReleaseName} {
			log.Printf("SetUpSuite: cleaning up cluster-scoped resources for release %q", oldRelease)
			s.k8sclient.CleanupClusterScopedResources(ctx, oldRelease)
		}
		// Also delete the cert-manager namespace if it exists (prevents install conflicts).
		if s.k8sclient.NamespaceExists(ctx, "cert-manager") {
			log.Print("SetUpSuite: deleting cert-manager namespace (leftover from previous run)")
			if err = s.k8sclient.DeleteNamespaceAndWait(ctx, "cert-manager", "", 3*time.Minute); err != nil {
				log.Printf("SetUpSuite: cert-manager namespace delete: %v (continuing)", err)
			}
		}
		// Delete cert-manager namespace-scoped Roles in kube-system (these lack helm labels
		// after a raw kubectl delete of the cert-manager namespace, preventing helm reinstall).
		s.k8sclient.DeleteCertManagerKubeSystemRoles(ctx)
		err = s.k8sclient.CreateNamespace(ctx, s.ns)
		assert.NoError(c, err)
	}

	if s.suiteHook != nil {
		if err := s.suiteHook(ctx); err != nil {
			assert.NoError(c, err, "suite hook failed")
		}
	}
}

func (s *E2ESuite) TearDownSuite(c *C) {
	log.Print("TearDownSuite:")
	if !s.existingDeploy && !*noTeardown {
		log.Printf("TearDownSuite: deleting namespace %s", s.ns)
		if err := s.k8sclient.DeleteNamespaceAndWait(context.Background(), s.ns, "", 3*time.Minute); err != nil {
			log.Printf("TearDownSuite: namespace delete: %v", err)
		}
		// Clean up cluster-scoped resources left by the operator helm release.
		s.k8sclient.CleanupClusterScopedResources(context.Background(), operatorReleaseName)
	} else if *noTeardown {
		log.Print("TearDownSuite: skipping teardown (-noteardown flag set)")
	}
	if s.helmClient != nil {
		s.helmClient.Cleanup()
	}
}

// operatorReleaseName is the helm release name used for the GPU Operator.
const operatorReleaseName = "e2e-gpu-operator"

// helmSetJoin joins -helmset values into a log-friendly string.
func helmSetJoin(vals []string) string { return strings.Join(vals, ", ") }
