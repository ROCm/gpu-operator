# Automatic Unhealthy Device Test

## Automatic Unhealthy Device Test trigger

Test runner is periodically watching for the device health status from device metrics exporter per 30 seconds. Once exporter reported GPU status is unhealthy, test runner will start to run one-time test on the unhealthy GPU. The test result will be exported as Kubernetes event.

## Configure test runner

To start the Test Runner along with the GPU Operator, Device Metrics Exporter must be enabled since Test Runner is depending on the exported health status. Configure the ``` spec/metricsExporter/enable ``` field in deviceconfig Custom Resource(CR) to enable/disable metrics exporter and configure the ``` spec/testRunner/enable ``` field in deviceconfig Custom Resource(CR) to enable/disable test runner.

```yaml
# Specify the metrics exporter config
metricsExporter:
    # To enable/disable the metrics exporter, disabled by default
    enable: True

    # kubernetes service type for metrics exporter, clusterIP(default) or NodePort
    serviceType: "NodePort"

    # Node port for metrics exporter service, metrics endpoint $node-ip:$nodePort
    nodePort: 32500

    # image for the metrics-exporter container
    image: "rocm/device-metrics-exporter:v1.2.0"

# Specify the test runner config
testRunner:
    # To enable/disable the test runner, disabled by default
    enable: true

    # image for the test runner container
    image: docker.io/rocm/test-runner:v1.2.0-beta.0

    # specify the mount for test logs
    logsLocation:
      # mount path inside test runner container
      mountPath: "/var/log/amd-test-runner"

      # host path to be mounted into test runner container
      hostPath: "/var/log/amd-test-runner"
```

## Check test runner pod logs

```bash
kube-amd-gpu        test-deviceconfig-metrics-exporter-6g286                1/1     Running                  0             18m
kube-amd-gpu        test-deviceconfig-test-runner-r9gjr                     1/1     Running                  0             18m
```

Once device metrics exporter and test runner were brought up by applying the corresponding deviceconfig Custom Resource(CR), the test runner pod logs can be viewed by running ```kubectl logs``` command, for the above example it is:
```kubectl logs -n kube-amd-gpu test-deviceconfig-test-runner-r9gjr```

## Check test running node labels

When the test is ongoing the corresponding label will be added to the node resource: ```"amd.testrunner.gpu_health_check.gst_single": "running"```, the test running label will be removed once the test completed.

## Check test result event

The test runner generated event can be found from the operator's namespace:

```bash
$ kubectl get events -n kube-amd-gpu
LAST SEEN   TYPE      REASON                    OBJECT                                            MESSAGE
8m8s        Normal    TestFailed                pod/test-runner-manual-trigger-c4hpw              [{"number":1,"suitesResult":{"42924":{"gpustress-3000-dgemm-false":"success","gpustress-41000-fp32-false":"failure","gst-1215Tflops-4K4K8K-rand-fp8":"failure","gst-8096-150000-fp16":"success"}}}]
```

Test runner generated event can be retrieved by filtering the source component: ```kubectl get events -n kube-amd-gpu -o=jsonpath='{.items[?(@.source.component=="amd-test-runner")]}'``` Here is an example event resource:

```json
{
  "apiVersion": "v1",
  "count": 1,
  "eventTime": null,
  "firstTimestamp": "2025-02-07T17:39:50Z",
  "involvedObject": {
    "kind": "Pod",
    "name": "gpu-operator-test-runner-58jrf",
    "namespace": "kube-amd-gpu"
  },
  "kind": "Event",
  "lastTimestamp": "2025-02-07T17:39:50Z",
  "message": "[{\"number\":1,\"suitesResult\":{\"35824\":{\"gpustress-3000-dgemm-false\":\"success\",\"gpustress-41000-fp32-false\":\"failure\",\"gst-1215Tflops-4K4K8K-rand-fp8\":\"failure\",\"gst-8096-150000-fp16\":\"success\"}},\"status\":\"completed\"}]",
  "metadata": {
    "creationTimestamp": "2025-02-07T17:39:50Z",
    "generateName": "amd-test-runner-gpu_health_check-",
    "labels": {
      "testrunner.amd.com/category": "gpu_health_check",
      "testrunner.amd.com/gpu.id.0": "35824",
      "testrunner.amd.com/gpu.kfd.35824": "0",
      "testrunner.amd.com/hostname": "leto",
      "testrunner.amd.com/recipe": "gst_single",
      "testrunner.amd.com/trigger": "auto_unhealthy_gpu_watch"
    },
    "managedFields": [
      {
        "apiVersion": "v1",
        "fieldsType": "FieldsV1",
        "fieldsV1": {
          "f:count": {},
          "f:firstTimestamp": {},
          "f:involvedObject": {},
          "f:lastTimestamp": {},
          "f:message": {},
          "f:metadata": {
            "f:generateName": {},
            "f:labels": {
              ".": {},
              "f:testrunner.amd.com/category": {},
              "f:testrunner.amd.com/gpu.id.0": {},
              "f:testrunner.amd.com/gpu.kfd.35824": {},
              "f:testrunner.amd.com/hostname": {},
              "f:testrunner.amd.com/recipe": {},
              "f:testrunner.amd.com/trigger": {}
            }
          },
          "f:reason": {},
          "f:source": {
            "f:component": {},
            "f:host": {}
          },
          "f:type": {}
        },
        "manager": "amd-test-runner",
        "operation": "Update",
        "time": "2025-02-07T17:39:50Z"
      }
    ],
    "name": "amd-test-runner-gpu_health_check-2266p",
    "namespace": "kube-amd-gpu",
    "resourceVersion": "779260",
    "uid": "1993b984-6806-403a-a25c-2c135828e6c2"
  },
  "reason": "TestFailed",
  "reportingComponent": "",
  "reportingInstance": "",
  "source": {
    "component": "amd-test-runner",
    "host": "leto"
  },
  "type": "Warning"
}
```

* ```involvedObject``` shows which test runner pod executed the test and generated the event.
* ```lastTimestamp``` shows the time when event was generated.
* ```message``` contains detailed test result, which can be further passsed into json format:

```bash
$ kubectl get events -o=jsonpath='{.items[?(@.source.component=="amd-test-runner")]}' -n kube-amd-gpu | jq -r .message | jq .
[
  {
    "number": 1,
    "suitesResult": {
      "35824": {
        "gpustress-3000-dgemm-false": "success",
        "gpustress-41000-fp32-false": "failure",
        "gst-1215Tflops-4K4K8K-rand-fp8": "failure",
        "gst-8096-150000-fp16": "success"
      }
    },
    "status": "completed"
  }
]                                                    
```

in the above example ```35824``` is the GPU's KFD ID reported by amd-smi (in rocm-smi it is named as GUID), ```gpustress-3000-dgemm-false``` represents a specific test action and ```success``` is the action's result.

* ```metadata``` contains the basic information of event.
  * ```generateName``` is the common prefix of the generated event name, which is in the format of ```amd-test-runner-<test category>-```.
  * ```name``` is the generated event name, which is in the format of ```<generatedName>-<generated unique suffix>```
  * ```namespace``` is the namespace where the test runner is running.
  * ```labels``` contains all the information regarding the event and is helpful for filtering the event.

    ```yaml
    "labels": {
      "testrunner.amd.com/category": "gpu_health_check",
      "testrunner.amd.com/gpu.id.0": "35824",
      "testrunner.amd.com/gpu.kfd.35824": "0",
      "testrunner.amd.com/hostname": "leto",
      "testrunner.amd.com/recipe": "gst_single",
      "testrunner.amd.com/trigger": "auto_unhealthy_gpu_watch"
    },
    ```

    * ```testrunner.amd.com/category``` is which kind of test the event is related to.
    * ```testrunner.amd.com/trigger``` is how the test was triggered.
    * ```testrunner.amd.com/recipe``` is the test recipe name.
    * ```testrunner.amd.com/hostname``` is the name of the host where the test happened.
    * ```testrunner.amd.com/gpu.id.X``` shows which GPU was involved and ```X``` is the GPU index number, the value is corresponding GPU KFD ID.
    * ```testrunner.amd.com/gpu.kfd.Y``` shows which GPU was involved and ```Y``` is the GPU KFD ID, the value is corresponding GPU index number.
* ```reason``` gives an overall result of the whole test run, it could be ```TestPassed```, ```TestFailed``` or ```TestTimedOut```.
* ```source``` shows where the event came from, including component name ```amd-test-runner``` and worker node's host name.
* ```type``` classifies the event into different level. For test runner generated event, ```TestPassed``` events are assigned with ```Normal``` event type while ```TestFailed``` and ```TestTimedOut``` events are assigned with ```Warning``` event type.

## Advanced Configuration - ConfigMap

You can provide a config map to specify test recipe details for the test runner. Create the config map then specify the config map name in the deviceconfig Custom Resource(CR) for test runner to pick up the config.

```{note}
  If you want to update the config for test runner on the fly for the ```auto_unhealthy_gpu_watch``` trigger, directly update the configmap then the test runner can pick up the new config. After reading the new config, test runner's ongoing test won't be interrupted and still going with old config. The new config will be applied to the next test run.
```

Here is an example config map:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-runner-config-map
  namespace: kube-amd-gpu
data:
  config.json: |
    {
      "TestConfig": {
        "GPU_HEALTH_CHECK": {
          "TestLocationTrigger": {
            "global": {
              "TestParameters": {
                "AUTO_UNHEALTHY_GPU_WATCH": {
                  "TestCases": [
                    {
                      "Recipe": "gst_single",
                      "Iterations": 1,
                      "StopOnFailure": true,
                      "TimeoutSeconds": 600
                    }
                  ]
                }
              }
            },
            "node1": {
              "TestParameters": {
                "AUTO_UNHEALTHY_GPU_WATCH": {
                  "TestCases": [
                    {
                      "Recipe": "mem",
                      "Iterations": 1,
                      "StopOnFailure": true,
                      "TimeoutSeconds": 600
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

The name of the config map should be put under the deviceconfigs Custom Resource's  ``` spec/testRunner/config/name ``` field:

```yaml
spec:
  testRunner:
    enable: true
    config:
      name: test-runner-config-map
```

Config map explanation:

* TestCategory:
  
  ```GPU_HEALTH_CHECK``` indicates all the config under this category is working for checking GPU device health status.

* Global and node specific config:
  
  Under ```TestLocationTrigger``` there are ```global``` configs and host specific configs. Worker node whose name is ```node1``` will pick up the configs under ```node1```, other worker nodes will pick up the configs under ```global```.

* Test Triggers:
  
  Under ```TestParameters``` there is a map from test trigger to specific test case configs, which means the configs are setup for the corresponding test triggers (```AUTO_UNHEALTHY_GPU_WATCH```, ```MANUAL``` and ```PRE_START_JOB_CHECK```)

* Test Cases:

  ```{note}
  Test case is a list under each test trigger, in the current release only the first test case in the list will be executed.
  ```

  * Recipe:

    String. Please check the [Appendix](./appendix-test-recipe.md) for more details about all available test recipes. Default test recipe is ```gst_single```.

  * Iterations:
  
    Positive integer. Number of iterations to run for each test run. Default value is ```1```.

  * StopOnFailure:
  
    Boolean. If any iteration of test run failed, whether to stop the entire test run or not. Default is ```true```.

  * TimeoutSeconds:

    Positive integer. Specifies the timeout for each test iteration. The default value is `600` seconds. If the running time exceeds this limit, the current test iteration will be terminated.

  * DeviceIDs (Only works for ```manual``` and ```pre-start-job-check``` test trigger):

    List of string for GPU 0-inedxed ID. A selector to filter which GPU would run the test. For example, if there are 2 GPUs the GPU ID would be 0 and 1. To select GPU0 to run the test only, please configure the DeviceIDs:

    ```yaml
    {
      "Recipe": "gst_single",
      "Iterations": 1,
      "StopOnFailure": true,
      "TimeoutSeconds": 600,
      "DeviceIDs": ["0"]
    }
    ```

    * If the DeviceIDs list is empty or not specified, all GPUs will be selected.
    * If the DeviceIDs list is specified and all the IDs in the list are invalid, test runner process would exit with error status.
