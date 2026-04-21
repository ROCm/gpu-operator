/**
# Copyright (c) Advanced Micro Devices, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the \"License\");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an \"AS IS\" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package clients

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	helm "github.com/mittwald/go-helm-client"
	helmValues "github.com/mittwald/go-helm-client/values"
	"helm.sh/helm/v3/pkg/repo"
	restclient "k8s.io/client-go/rest"
)

type HelmClientOpt func(client *HelmClient)

type HelmClient struct {
	client     helm.Client
	cache      string
	config     string
	ns         string
	restConfig *restclient.Config
	relName    string
}

func WithNameSpaceOption(namespace string) HelmClientOpt {
	return func(c *HelmClient) {
		c.ns = namespace
	}
}

func WithKubeConfigOption(kubeconf *restclient.Config) HelmClientOpt {
	return func(c *HelmClient) {
		c.restConfig = kubeconf
	}
}

func NewHelmClient(opts ...HelmClientOpt) (*HelmClient, error) {
	client := &HelmClient{}
	for _, opt := range opts {
		opt(client)
	}

	var err error
	client.cache, err = os.MkdirTemp("", ".hcache")
	if err != nil {
		return nil, err
	}

	configDir, err := os.MkdirTemp("", ".hconfig")
	if err != nil {
		return nil, err
	}
	// RepositoryConfig must be a file path (repositories.yaml), not a directory.
	client.config = configDir
	repoFile := configDir + "/repositories.yaml"
	restConfOptions := &helm.RestConfClientOptions{
		Options: &helm.Options{
			Namespace:        client.ns,
			RepositoryConfig: repoFile,
			Debug:            true,
			RepositoryCache:  client.cache,
			DebugLog: func(format string, v ...interface{}) {
				log.Printf(format, v...)
			},
		},
		RestConfig: client.restConfig,
	}

	helmClient, err := helm.NewClientFromRestConf(restConfOptions)
	if err != nil {
		return nil, err
	}
	client.client = helmClient
	return client, nil
}

func (h *HelmClient) InstallChart(ctx context.Context, chart string, params []string) (string, error) {
	values := helmValues.Options{
		Values: params,
	}

	chartSpec := &helm.ChartSpec{
		ReleaseName:   "e2e-test-k8s",
		ChartName:     chart,
		Namespace:     h.ns,
		GenerateName:  false,
		Wait:          true,
		Timeout:       5 * time.Minute,
		CleanupOnFail: false,
		DryRun:        false,
		ValuesOptions: values,
	}

	resp, err := h.client.InstallChart(ctx, chartSpec, nil)
	if err != nil {
		return "", err
	}
	log.Printf("helm chart install resp: %+v", resp)
	h.relName = resp.Name
	return resp.Name, err
}

func (h *HelmClient) UninstallChart() error {
	if h.relName == "" {
		return fmt.Errorf("helm chart is not installed by client")
	}
	return h.client.UninstallReleaseByName(h.relName)
}

// AddRepository adds a helm repository. url is the chart repo URL; name is the local alias.
func (h *HelmClient) AddRepository(name, url string) error {
	return h.client.AddOrUpdateChartRepo(repo.Entry{
		Name: name,
		URL:  url,
	})
}

// InstallChartWithTimeout is like InstallChart but accepts a custom timeout, release name, and
// optional chart version. version may be empty (uses the latest available version).
func (h *HelmClient) InstallChartWithTimeout(ctx context.Context, releaseName, chart, version string, params []string, timeout time.Duration) (string, error) {
	values := helmValues.Options{
		Values: params,
	}

	chartSpec := &helm.ChartSpec{
		ReleaseName:   releaseName,
		ChartName:     chart,
		Version:       version,
		Namespace:     h.ns,
		GenerateName:  false,
		Wait:          false, // individual Op010-Op070 tests verify each component's readiness
		Timeout:       timeout,
		CleanupOnFail: false,
		DryRun:        false,
		SkipCRDs:      false,
		ValuesOptions: values,
	}

	resp, err := h.client.InstallChart(ctx, chartSpec, nil)
	if err != nil {
		return "", err
	}
	log.Printf("helm chart install resp: %+v", resp)
	h.relName = resp.Name
	return resp.Name, nil
}

// UninstallChartByName uninstalls a helm release by name without requiring it was installed by this client.
func (h *HelmClient) UninstallChartByName(releaseName string) error {
	return h.client.UninstallReleaseByName(releaseName)
}

// UninstallAllReleases uninstalls all helm releases in the client's namespace.
// Errors are logged but not returned so cleanup continues regardless.
func (h *HelmClient) UninstallAllReleases() {
	releases, err := h.client.ListDeployedReleases()
	if err != nil {
		log.Printf("UninstallAllReleases: list: %v", err)
		return
	}
	for _, rel := range releases {
		log.Printf("UninstallAllReleases: uninstalling %s", rel.Name)
		if err := h.client.UninstallReleaseByName(rel.Name); err != nil {
			log.Printf("UninstallAllReleases: %s: %v", rel.Name, err)
		}
	}
}

func (h *HelmClient) Cleanup() {
	err := os.RemoveAll(h.cache)
	if err != nil {
		log.Printf("failed to delete directory %s; err: %v", h.cache, err)
	}

	err = os.RemoveAll(h.config)
	if err != nil {
		log.Printf("failed to delete directory %s; err: %v", h.config, err)
	}
}
