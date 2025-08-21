## Test Runner Overview

The test runner component offers hardware validation, diagnostics and benchmarking capabilities for your GPU Worker nodes. The new capabilities include:

- Automatically triggering of configurable tests on unhealthy GPUs

- Scheduling or Manually triggering tests within the Kubernetes cluster

- Running pre-start job tests as init containers within your GPU workload pods to ensure GPU health and stability before execution of long running jobs

- Reporting test results as Kubernetes events

Under the hood the Device Test runner leverages the ROCm Validation Suite (RVS) and AMD GPU Field Health Check (AGFHC) toolkit to run any number of tests including GPU stress tests, PCIE bandwidth benchmarks, memory tests, and longer burn-in tests if so desired. 

```{note}
1. The [public test runner image](https://hub.docker.com/r/rocm/test-runner) only supports executing RVS test.

2. The AGFHC toolkit is NOT publicly accessible and requires special authorization. It can be used not only with the test runner but also in various other workflows. For more details, see the [Instinct documentation website](https://instinct.docs.amd.com/).

3. To access the full test runner image, which includes both RVS and the AGFHC toolkit, please contact your AMD representative to complete the authorization process.
```
