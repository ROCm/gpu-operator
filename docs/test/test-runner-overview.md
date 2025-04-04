## Test Runner Overview

The test runner component offers hardware validation, diagnostics and benchmarking capabilities for your GPU Worker nodes. The new capabilities include:

- Automatically triggering of configurable tests on unhealthy GPUs

- Scheduling or Manually triggering tests within the Kubernetes cluster

- Running pre-start job tests as init containers within your GPU workload pods to ensure GPU health and stability before execution of long running jobs

- Reporting test results as Kubernetes events

Under the hood the Device Test runner leverages the ROCm Validation Suite (RVS) to run any number of tests including GPU stress tests, PCIE bandwidth benchmarks, memory tests, and longer burn-in tests if so desired. The DeviceConfig custom resource has also been updated to provide new configuration options for the Test Runner:

```bash
  testRunner:
    # To enable/disable the testrunner, disabled by default
    enable: True

    # testrunner image
    image: docker.io/rocm/test-runner:v1.2.0-beta.0

    # image pull policy for the testrunner
    # default value is IfNotPresent for valid tags, Always for no tag or "latest" tag
    imagePullPolicy: "Always"

    # specify the mount for test logs
    logsLocation:
      # mount path inside test runner container
      mountPath: "/var/log/amd-test-runner"

      # host path to be mounted into test runner container
      hostPath: "/var/log/amd-test-runner"
```
