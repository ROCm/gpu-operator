## Test Runner Overview

The test runner component offers hardware validation, diagnostics and benchmarking capabilities for your GPU Worker nodes. The new capabilities include:

- Automatically triggering of configurable tests on unhealthy GPUs

- Scheduling or Manually triggering tests within the Kubernetes cluster

- Running pre-start job tests as init containers within your GPU workload pods to ensure GPU health and stability before execution of long running jobs

- Reporting test results as Kubernetes events

Under the hood the Device Test runner leverages the ROCm Validation Suite (RVS) and AMD GPU Field Health Check (AGFHC) toolkit to run any number of tests including GPU stress tests, PCIE bandwidth benchmarks, memory tests, and longer burn-in tests if so desired. 

```{note}
The [public test runner image](https://hub.docker.com/r/rocm/test-runner) includes only RVS and supports RVS tests exclusively.

To access the full test runner image with both RVS and AGFHC toolkit, please contact your AMD representative for the required authorization process.
```
