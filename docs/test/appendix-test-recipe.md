# Appendix - Test Recipes

## RVS Test Recipes

The test runner's test recipes are built upon ROCm Validation Suite (RVS). Here is a full list of supported test recipes by RVS.

| GPU     | babel | gpup_single | gst_single | iet_single | pbqt_single | pebb_single | tst_single | gst_ext | gst_selfcheck | gst_stress | iet_stress |
|---------|------------|------------------|-----------------|-----------------|------------------|------------------|-----------------|--------------|--------------------|-----------------|-----------------|
| MI210   |     ✓      |        ✓         |        ✓        |        ✓        |        ✓         |        ✓         |        ✓        |              |                    |                 |                 |
| MI300X  |     ✓      |                  |        ✓        |        ✓        |        ✓         |        ✓         |                 |      ✓       |         ✓          |        ✓        |        ✓        |

For more information of test recipe details and explanation, please check [RVS official documentation](https://rocm.docs.amd.com/projects/ROCmValidationSuite/en/latest/conceptual/rvs-modules.html).
