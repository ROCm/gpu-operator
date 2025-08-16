# Appendix - Test Recipes

## RVS Test Recipes

The test runner's test recipes are built upon ROCm Validation Suite (RVS). Here is a full list of supported test recipes by RVS.

| GPU       | babel | gpup_single | gst_single | iet_single | pbqt_single | pebb_single | tst_single | gst_ext | gst_selfcheck | gst_stress | iet_stress | gst_thermal | iet_thermal |
|-----------|-------|-------------|------------|------------|-------------|-------------|------------|---------|---------------|------------|------------|-------------|-------------|
| MI210     |   ✓   |      ✓      |     ✓      |     ✓      |     ✓       |     ✓       |     ✓      |         |               |            |            |             |             |
| MI300X    |   ✓   |             |     ✓      |     ✓      |     ✓       |     ✓       |            |    ✓    |       ✓       |     ✓      |     ✓      |             |             |
| MI300A    |       |             |            |            |             |     ✓       |            |         |               |            |     ✓      |             |             |
| MI300X-HF |       |             |     ✓      |            |             |             |            |         |               |            |     ✓      |             |             |
| MI308X    |   ✓   |             |     ✓      |     ✓      |             |             |            |         |               |            |     ✓      |     ✓       |     ✓       |
| MI308X-HF |   ✓   |             |     ✓      |            |             |             |            |         |               |            |     ✓      |     ✓       |     ✓       |
| MI325X    |   ✓   |             |     ✓      |            |     ✓       |     ✓       |            |         |               |            |     ✓      |             |             |
| MI350X    |   ✓   |             |     ✓      |            |             |             |            |         |               |            |     ✓      |             |             |
| MI355X    |   ✓   |             |     ✓      |            |             |             |            |         |               |            |     ✓      |             |             |

## RVS Arguments

| Argument                   | Description                                                                                           | Default/Example                                  |
|----------------------------|-------------------------------------------------------------------------------------------------------|--------------------------------------------------|
| `--parallel`, `-p` | Enables or Disables parallel execution across multiple GPUs, this will help accelerate the RVS tests. | By default if this option is not specified the test won't execute in parallel. Use `-p` or `-p true` to enable parallel execution or use `-p false` to disable the parallel execution. |
| `--debug`, `-d` | Specify the debug level for the output log. The range is 0-5 with 5 being the highest verbose level.| Example: Use `-d 5` to get the highest level debug output. |


For more information of test recipe details and explanation, please check [RVS official documentation](https://rocm.docs.amd.com/projects/ROCmValidationSuite/en/latest/conceptual/rvs-modules.html).
