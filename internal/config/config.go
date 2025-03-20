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

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type LeaderElection struct {
	Enabled    bool   `yaml:"enabled"`
	ResourceID string `yaml:"resourceID"`
}

type Config struct {
	HealthProbeBindAddress string         `yaml:"healthProbeBindAddress"`
	MetricsBindAddress     string         `yaml:"metricsBindAddress"`
	LeaderElection         LeaderElection `yaml:"leaderElection"`
}

func ParseFile(path string) (*Config, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open the configuration file: %v", err)
	}
	defer fd.Close()

	cfg := Config{}

	if err = yaml.NewDecoder(fd).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("could not decode configuration file: %v", err)
	}

	return &cfg, nil
}

func (c *Config) ManagerOptions() *manager.Options {
	return &manager.Options{
		HealthProbeBindAddress: c.HealthProbeBindAddress,
		LeaderElection:         c.LeaderElection.Enabled,
		LeaderElectionID:       c.LeaderElection.ResourceID,
		Metrics: server.Options{
			BindAddress: c.MetricsBindAddress,
		},
	}
}
