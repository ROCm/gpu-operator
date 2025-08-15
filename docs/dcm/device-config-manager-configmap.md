# Device Config Manager ConfigMap

The Device Config Manager (DCM) job is to monitor for and apply different configurations on nodes in your cluster. This is done by defining different profiles that can then be applied to each node on your cluster. As such, DCM relies on a Kubernetes ConfigMap that contains the definitions of each configuration profile. This ConfigMap is required to be present for the Device Config Manager to function properly. Once profiles have been defined, specific node labels can be put on the nodes in the cluster to specify which profile should be applied. DCM monitors for any changes in the ConfigMap or changes to the profile node label and applies the correct configuration accordingly. This ConfigMap approach helps to simplify the rollout of different config profiles across all the nodes in the cluster.

## ConfigMap

As mentioned, the `config.json` data specifies different GPU partitioning profiles that can be set on the GPU nodes in your cluster. Below is an example Device Config Manager ConfigMap. This example ConfigMap is also available in the GPU Operator repo here: [_example/configmap.yaml_](https://github.com/ROCm/gpu-operator/blob/main/example/configManager/configmap.yaml)

```yaml  
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-manager-config
  namespace: kube-amd-gpu
data:
  config.json: |
    {
      "gpu-config-profiles":
      {
          "cpx-profile":
          {
              "skippedGPUs": {
                  "ids": []
              },
              "profiles": [
                  {
                      "computePartition": "CPX",
                      "memoryPartition": "NPS4",
                      "numGPUsAssigned": 8
                  }
              ]
          },
          "spx-profile":
          {
              "skippedGPUs": {
                  "ids": []
              },
              "profiles": [
                  {
                      "computePartition": "SPX",
                      "memoryPartition": "NPS1",
                      "numGPUsAssigned": 8
                  }
              ]
          }
      },
      "gpuClientSystemdServices": {
          "names": ["amd-metrics-exporter", "gpuagent"]
      }
    }
```

Below is an explanation of each field in the ConfigMap:

| **Field** | **Description** |
|-------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `gpu-config-profiles`   | Defines a set of partitioning config profiles from which the user can choose the profile to apply                                                                 |
| `cpx-profile` and `spx-profile` | Example profile names                                                                                                                                           |
| `skippedGPUs` (Optional) | List of GPU IDs to skip partitioning                                                                                                                             |
| `computePartition`      | Compute partition type                                                                                                                                           |
| `memoryPartition`       | Memory partition type                                                                                                                                            |
| `numGPUsAssigned`       | Number of GPUs to be partitioned on the node                                                                                                                     |
| `gpuClientSystemdServices`       | Defines a list of systemd service unit files to be stopped/restarted on the node                                                                                                                   |

```{note}
Users can create a heterogeneous partitioning config profile by specifying more than one `computePartition` scheme in the `profiles` array, however this is not a recommmended or supported configuration by AMD. Note that NPS4 memory partition mode does not work with heterogenous parition schemes and only supports CPX on MI300X systems.

    ``` 
    apiVersion: v1
    kind: ConfigMap
    metadata:
    name: config-manager-config
    namespace: kube-amd-gpu
    data:
    config.json: |
        {
        "gpu-config-profiles":
        {
          "heterogenous":
            {
            "skippedGPUs": {
                "ids": []
            },
            "profiles": [
                {
                    "computePartition": "SPX",
                    "memoryPartition": "NPS1",
                    "numGPUsAssigned": 4
                },
                {
                    "computePartition": "CPX",
                    "memoryPartition": "NPS1",
                    "numGPUsAssigned": 4
                }
            ]
            }
          },
          "gpuClientSystemdServices": {
              "names": ["amd-metrics-exporter", "gpuagent"]
          }
        }

```

## ConfigMap Profile Checks

- Let's assume a node with 8 GPUs in it.

### List of profiles checks

- Total number of all `numGPUsAssigned` values of a single profile must be equal to the total number of GPUs on the node.
  - In `default` profile, you can observe that, we are requesting 6 GPUs of type CPX-NPS1 and 2 GPUs of SPX-NPS1 which is valid since it comes to a total of 8 GPUs
  - If `skippedGPUs` field is present, we need to account for those IDs as well.
  - Hence, `Sum of numGPUsAssigned + len(skippedGPUs) = TotalGPUCount`
- `skippedGPUs` field
  - GPU IDs in the list can range from `0` to `total number of GPUs - 1`
  - Length of list must be equal to `total number of GPUs` - `sum of numGPUsAssigned` in that profile
  - Example, in `profile-1`, we have 5 GPUs set to CPX-NPS1 and exactly 3 more GPU IDs mentioned in the skip list

## Supported Partition Modes

- Compute types supported are SPX and CPX.

- Memory types supported are NPS1 and NPS4
  - NPS4 is supported only for CPX compute type
  - Combination of NPS1 and NPS4 memory types cannot be used in a single profile
