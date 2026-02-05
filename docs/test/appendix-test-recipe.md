# Appendix - Test Recipes

## RVS Test Recipes

The test runner's test recipes are built upon ROCm Validation Suite (RVS). Here is a full list of supported test recipes by RVS.

| GPU       | babel | gpup_single | gst_single | iet_single | pbqt_single | pebb_single | tst_single | gst_ext | gst_selfcheck | gst_stress | iet_stress | gst_thermal | iet_thermal |
|-----------|-------|-------------|------------|------------|-------------|-------------|------------|---------|---------------|------------|------------|-------------|-------------|
| MI210     | ✓     | ✓           | ✓          | ✓          | ✓           | ✓           | ✓          |         |               |            |            |             |             |
| MI300X    | ✓     |             | ✓          | ✓          | ✓           | ✓           |            | ✓       | ✓             | ✓          | ✓          |             |             |
| MI300A    |       |             |            |            |             | ✓           |            |         |               |            | ✓          |             |             |
| MI300X-HF |       |             | ✓          |            |             |             |            |         |               |            | ✓          |             |             |
| MI308X    | ✓     |             | ✓          | ✓          |             |             |            |         |               |            | ✓          | ✓           | ✓           |
| MI308X-HF | ✓     |             | ✓          |            |             |             |            |         |               |            | ✓          | ✓           | ✓           |
| MI325X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |
| MI350X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |
| MI355X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |

### Level based test recipes

Pre-configured test recipes are available at five levels of complexity and coverage. To use a level-based test recipe, specify the test recipe name in the config map using the format `levels/rvs_level_1`.

| GPU       | `levels/rvs_level_1` | `levels/rvs_level_2` | `levels/rvs_level_3` | `levels/rvs_level_4` | `levels/rvs_level_5` |
|-----------|----------------------|----------------------|----------------------|----------------------|----------------------|
| MI300X    |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI300X-HF |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI308X    |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI308X-HF |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI325X    |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI350X    |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |
| MI355X    |          ✓           |          ✓           |          ✓           |          ✓           |          ✓           |

### Partitioned GPU test recipes

Test recipes are available for GPUs with specific partition profiles. To use a partitioned GPU test recipe, specify the test recipe name in the config map using the format `NPS4/CPX/gst_single`.

| GPU Model + Partition Profile (Memory / Compute)  | gst_single |
|---------------------------------------------------|------------|
| MI300X with `NPS4/CPX`                            |     ✓      |
| MI300X with `NPS2/DPX`                            |     ✓      |
| MI325X with `NPS2/DPX`                            |     ✓      |
| MI350X with `NPS2/DPX`                            |     ✓      |
| MI350X with `NPS2/CPX`                            |     ✓      |
| MI355X with `NPS2/DPX`                            |     ✓      |
| MI355X with `NPS2/CPX`                            |     ✓      |

## RVS Arguments

| Argument                   | Description                                                                                           | Default/Example                                  |
|----------------------------|-------------------------------------------------------------------------------------------------------|--------------------------------------------------|
| `--parallel`, `-p` | Enables or Disables parallel execution across multiple GPUs, this will help accelerate the RVS tests. | By default if this option is not specified the test won't execute in parallel. Use `-p` or `-p true` to enable parallel execution or use `-p false` to disable the parallel execution. |
| `--debug`, `-d` | Specify the debug level for the output log. The range is 0-5 with 5 being the highest verbose level.| Example: Use `-d 5` to get the highest level debug output. |

For more information of test recipe details and explanation, please check [RVS official documentation](https://rocm.docs.amd.com/projects/ROCmValidationSuite/en/latest/conceptual/rvs-modules.html).
