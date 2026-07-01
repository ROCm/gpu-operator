# Appendix - Test Recipes

## RVS Test Recipes

The test runner's test recipes are built upon ROCm Validation Suite (RVS). Here is a full list of supported test recipes by RVS.

| GPU       | babel | gpup_single | gst_single | iet_single | pbqt_single | pebb_single | tst_single | gst_ext | gst_selfcheck | gst_stress | iet_stress | gst_thermal | iet_thermal |
|--------------|-------|-------------|------------|------------|-------------|-------------|------------|---------|---------------|------------|------------|-------------|-------------|
| MI210     | ✓     | ✓           | ✓          | ✓          | ✓           | ✓           | ✓          |         |               |            |            |             |             |
| MI300X    | ✓     |             | ✓          | ✓          | ✓           | ✓           |            | ✓       | ✓             | ✓          | ✓          |             |             |
| MI300A    |       |             |            |            |             | ✓           |            |         |               |            | ✓          |             |             |
| MI300X-HF |       |             | ✓          |            |             |             |            |         |               |            | ✓          |             |             |
| MI308X    | ✓     |             | ✓          | ✓          |             |             |            |         |               |            | ✓          | ✓           | ✓           |
| MI308X-HF | ✓     |             | ✓          |            |             |             |            |         |               |            | ✓          | ✓           | ✓           |
| MI325X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |
| MI350P-450W  | ✓     |             | ✓          |            |             |             |            |         |               |            | ✓          |             |             |
| MI350P-600W  | ✓     |             | ✓          |            |             |             |            |         |               |            | ✓          |             |             |
| MI350X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |
| MI355X    | ✓     |             | ✓          |            | ✓           | ✓           |            |         |               |            | ✓          |             |             |

### Radeon GPU test recipes

| GPU              | mem | gpup_single | gst_single | iet_single | rcqt_single | pbqt_single | pebb_single | peqt_single | pesm_1 | gst_stress_3_hrs |
|------------------|-----|-------------|------------|------------|-------------|-------------|-------------|-------------|--------|------------------|
| W6800            | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 6800          | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 6800XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 6900XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 6950XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7700            | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7700          | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7700XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7800XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7800M         | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7800            | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7800 48GB       | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7900            | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7900 Dual Slot  | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| W7900D           | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7900XT        | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7900XTX       | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7900GRE       | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| RX 7900M         | ✓   | ✓           | ✓          | ✓          | ✓           | ✓           | ✓           | ✓           | ✓      | ✓                |
| AI PRO 9600D     |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| AI PRO 9700      |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| AI PRO 9700S     |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| RX 9060          |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| RX 9060XT        |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| RX 9070          |     |             | ✓          | ✓          |             |             |             |             |        |                  |
| RX 9070XT        |     |             | ✓          | ✓          |             |             |             |             |        |                  |

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
