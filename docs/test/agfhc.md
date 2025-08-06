# AGFHC (AMD GPU Field Health Check) Support
In addition to RVS (ROCm Validation Suite), the test runner supports AGFHC (AMD GPU Field Health Check) to ensure the health of AMD GPUs in production environments.

# Triggering AGFHC Tests

To support more than one test framework, the test runner allows you to specify the test framework in the `config.json` file.

Example Config Map to use AGFHC test framework:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agfhc-config-map
  namespace: default
data:
  config.json: |
    {
      "TestConfig": {
        "GPU_HEALTH_CHECK": {
          "TestLocationTrigger": {
            "global": {
              "TestParameters": {
                "MANUAL": {
                  "TestCases": [
                    {
                      "Framework": "AGFHC",
                      "Recipe": "all_lvl1",
                      "Iterations": 1,
                      "StopOnFailure": true,
                      "TimeoutSeconds": 2400,
                      "DeviceIDs": ["0", "1"],
                      "Arguments": "--ignore-dmesg,--disable-sysmon"
                    }
                  ]
                }
              }
            }
          }
        }
      }
    }
```
The default framework is RVS if not specified, but you can switch to AGFHC by setting the `Framework` field to `AGFHC` in the `TestCases` section of the `config.json`. The `Recipe` field specifies the test suite to run from the specified framework. You can supply additional optional arguments to the test cases using the `Arguments` field. At present, only 1 `testcase` can be run at a time.

Please refer to the AGFHC documentation for available test recipes and additional configuration options.

### Recipes

For example, for MI300X GPUs, the following test recipes are currently available:

| Name               | Title                             | Path                                              |
|--------------------|-----------------------------------|---------------------------------------------------|
| all_burnin_12h     | A \~12h check across system        | /opt/amd/agfhc/recipes/mi300x/all_burnin_12h.yml  |
| all_burnin_24h     | A \~24h check across system        | /opt/amd/agfhc/recipes/mi300x/all_burnin_24h.yml  |
| all_burnin_4h      | A \~4h check across system         | /opt/amd/agfhc/recipes/mi300x/all_burnin_4h.yml   |
| all_lvl1           | A  \~5m check across system        | /opt/amd/agfhc/recipes/mi300x/all_lvl1.yml        |
| all_lvl2           | A \~10m check across system        | /opt/amd/agfhc/recipes/mi300x/all_lvl2.yml        |
| all_lvl3           | A \~30m check across system        | /opt/amd/agfhc/recipes/mi300x/all_lvl3.yml        |
| all_lvl4           | A  \~1h check across system        | /opt/amd/agfhc/recipes/mi300x/all_lvl4.yml        |
| all_lvl5           | A  \~2h check across system        | /opt/amd/agfhc/recipes/mi300x/all_lvl5.yml        |
| all_perf           | Run all performance based tests    | /opt/amd/agfhc/recipes/mi300x/all_perf.yml        |
| dma_lvl1           | A  \~5m DMA workload               | /opt/amd/agfhc/recipes/mi300x/dma_lvl1.yml        |
| dma_lvl2           | A  \~10m DMA workload              | /opt/amd/agfhc/recipes/mi300x/dma_lvl2.yml        |
| dma_lvl3           | A  \~30m DMA workload              | /opt/amd/agfhc/recipes/mi300x/dma_lvl3.yml        |
| dma_lvl4           | A  \~1h DMA workload               | /opt/amd/agfhc/recipes/mi300x/dma_lvl4.yml        |
| gfx_lvl1           | A  \~5m GFX workload               | /opt/amd/agfhc/recipes/mi300x/gfx_lvl1.yml        |
| gfx_lvl2           | A \~10m GFX workload               | /opt/amd/agfhc/recipes/mi300x/gfx_lvl2.yml        |
| gfx_lvl3           | A \~30m GFX workload               | /opt/amd/agfhc/recipes/mi300x/gfx_lvl3.yml        |
| gfx_lvl4           | A  \~1h GFX workload               | /opt/amd/agfhc/recipes/mi300x/gfx_lvl4.yml        |
| hbm_burnin_24h     | A \~24h extended hbm test          | /opt/amd/agfhc/recipes/mi300x/hbm_burnin_24h.yml  |
| hbm_burnin_8h      | A \~8h extended hbm test           | /opt/amd/agfhc/recipes/mi300x/hbm_burnin_8h.yml   |
| hbm_lvl1           | A  \~5m HBM workload               | /opt/amd/agfhc/recipes/mi300x/hbm_lvl1.yml        |
| hbm_lvl2           | A \~10m HBM workload               | /opt/amd/agfhc/recipes/mi300x/hbm_lvl2.yml        |
| hbm_lvl3           | A \~30m HBM workload               | /opt/amd/agfhc/recipes/mi300x/hbm_lvl3.yml        |
| hbm_lvl4           | A  \~1h HBM workload               | /opt/amd/agfhc/recipes/mi300x/hbm_lvl4.yml        |
| hbm_lvl5           | A  \~2h HBM workload               | /opt/amd/agfhc/recipes/mi300x/hbm_lvl5.yml        |
| hsio               | Run all HSIO tests once            | /opt/amd/agfhc/recipes/mi300x/hsio.yml            |
| pcie_lvl1          | A  \~5m PCIe workload              | /opt/amd/agfhc/recipes/mi300x/pcie_lvl1.yml       |
| pcie_lvl2          | A \~10m PCIe workload              | /opt/amd/agfhc/recipes/mi300x/pcie_lvl2.yml       |
| pcie_lvl3          | A \~30m PCIe workload              | /opt/amd/agfhc/recipes/mi300x/pcie_lvl3.yml       |
| pcie_lvl4          | A  \~1h PCIe workload              | /opt/amd/agfhc/recipes/mi300x/pcie_lvl4.yml       |
| rochpl_isolation   | Run rocHPL on each GPU             | /opt/amd/agfhc/recipes/mi300x/rochpl_isolation.yml|
| single_pass        | Run all tests once                 | /opt/amd/agfhc/recipes/mi300x/single_pass.yml     |
| thermal            | Verify thermal solution            | /opt/amd/agfhc/recipes/mi300x/thermal.yml         |
| xgmi_lvl1          | A  \~5m xGMI workload              | /opt/amd/agfhc/recipes/mi300x/xgmi_lvl1.yml       |
| xgmi_lvl2          | A \~10m xGMI workload              | /opt/amd/agfhc/recipes/mi300x/xgmi_lvl2.yml       |
| xgmi_lvl3          | A \~30m xGMI workload              | /opt/amd/agfhc/recipes/mi300x/xgmi_lvl3.yml       |
| xgmi_lvl4          | A  \~1h xGMI workload              | /opt/amd/agfhc/recipes/mi300x/xgmi_lvl4.yml       |

NOTE: Each one of the aforementioned recipes could consist of multiple test cases. Execution of _individual_ AGFHC test case is currently not supported.

### Supported Partition Profile

The Instinct GPU models could be configured with certain GPU partition profiles to execute AGFHC tests, the supported partition profiles are:

| GPU Model   | Compute Partition | Memory Partition | Number of GPUs for testing |
|-------------|------------------|------------------|------------------------|
| mi300a      | SPX              | NPS1             | 1                      |
| mi300a      | SPX              | NPS1             | 2                      |
| mi300a      | SPX              | NPS1             | 4                      |
| mi300x      | SPX              | NPS1             | 1                      |
| mi300x      | SPX              | NPS1             | 8                      |
| mi300x      | DPX              | NPS2             | 2                      |
| mi300x      | DPX              | NPS2             | 16                     |
| mi308x      | SPX              | NPS1             | 1                      |
| mi308x      | SPX              | NPS1             | 8                      |
| mi325x      | SPX              | NPS1             | 1                      |
| mi325x      | SPX              | NPS1             | 8                      |
| mi308x-hf   | SPX              | NPS1             | 1                      |
| mi308x-hf   | SPX              | NPS1             | 8                      |
| mi300x-hf   | SPX              | NPS1             | 1                      |
| mi300x-hf   | SPX              | NPS1             | 8                      |
| mi350x      | SPX              | NPS1             | 1                      |
| mi350x      | SPX              | NPS1             | 8                      |
| mi355x      | SPX              | NPS1             | 1                      |
| mi355x      | SPX              | NPS1             | 8                      |

### AGFHC arguments

As for the AGFHC arguments, please refer to AGFHC official documents for the full list of available arguments. Here is a list of frequently used arguments:

| Argument                   | Description                                                                                           | Default/Example                                  |
|----------------------------|-------------------------------------------------------------------------------------------------------|--------------------------------------------------|
| --update-interval UPDATE_INTERVAL | Set the interval to print elapsed timing updates on the console.                                 | `--update-interval 20s` - updates every 20s      |
| --sysmon-interval SYSMON_INTERVAL | Set to update the default sysmon interval                                                        |                                                  |
| --tar-logs                 | Generate a tar file of all logs                                                                       |                                                  |
| --disable-sysmon           | Set to disable system monitoring data collection.                                                     | Default: enabled                                 |
| --disable-numa-control     | Set to disable control of numa balancing.                                                             | Default: enabled                                 |
| --disable-ras-checks       | Set to disable ras checks.                                                                            | Default: enabled                                 |
| --disable-bad-pages-checks | Set to disable bad pages checks.                                                                      | Default: enabled                                 |
| --disable-dmesg-checks     | Set to disable dmesg checks.                                                                          | Default: enabled                                 |
| --ignore-dmesg             | Set to ignore dmesg fails, logs will still be created.                                                | Default: dmesg fails enabled                     |
| --ignore-ras               | Set to ignore ras fails, logs will still be created.                                                  | Default: ras fails enabled                       |
| --ignore-performance       | Set to ignore performance to skip the performance analysis and perform only RAS/dmesg checks.         | Default: performance analysis enabled            |
| --known-dmesg-only         | Do not fail on any unknown dmesg, but mark them as expected.                                          | Default: any unknown dmesg fails                 |
| --disable-hsio-gather      | Set to disable hsio gather.                                                                           | Default: enabled                                 |

# Kubernetes events

Upon successful execution of the AGFHC test recipe, the results are output as Kubernetes events. You can view these events using the following command:

```bash
$ kubectl get events
LAST SEEN   TYPE      REASON                    OBJECT                                 MESSAGE
113s        Normal    TestPassed                pod/test-runner-manual-trigger-hqb7b   [{"number":1,"suitesResult":{"0":{"gfx_dgemm":"success","hbm_bw":"success","pcie_bidi_peak":"success","pcie_link_status":"success","xgmi_a2a":"success"},"1":{"gfx_dgemm":"success","hbm_bw":"success","pcie_bidi_peak":"success","pcie_link_status":"success","xgmi_a2a":"success"}},"status":"completed"}]
```

If the test fails, the event will indicate a failure status.
```bash
$ kubectl get events
LAST SEEN   TYPE      REASON                 OBJECT                                 MESSAGE
63s         Warning   TestFailed             pod/test-runner-manual-trigger-fs64h   [{"number":1,"suitesResult":{"0":{"gfx_dgemm":"success","hbm_bw":"success","pcie_bidi_peak":"success","pcie_link_status":"success","xgmi_a2a":"success"},"2":{"gfx_dgemm":"success","hbm_bw":"failure","pcie_bidi_peak":"success","pcie_link_status":"success","xgmi_a2a":"success"}},"status":"completed"}]
```

# Log export
By default, test execution logs are saved to `/var/log/amd-test-runner/` on the host. Log export functionality is also supported, similar to RVS. AGFHC provides more detailed logs than RVS and all the logs provided by the framework are included in the tarball.