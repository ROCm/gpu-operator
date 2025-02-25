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
    image: "rocm/device-metrics-exporter:v1.1.0"

# Specify the test runner config
testRunner:
    # To enable/disable the test runner, disabled by default
    enable: true

    # image for the test runner container
    image: registry.test.pensando.io:5000/test-runner/test-runner:dev

    # specify the mount for test logs
    logsLocation:
      # mount path inside test runner container
      mountPath: "/var/log/amd-test-runner"

      # host path to be mounted into test runner container
      hostPath: "/var/log/amd-test-runner"
```

## Check test runner pod logs

```
kube-amd-gpu        test-deviceconfig-metrics-exporter-6g286                1/1     Running                  0             18m
kube-amd-gpu        test-deviceconfig-test-runner-r9gjr                     1/1     Running                  0             18m
```

Once device metrics exporter and test runner were brought up by applying the corresponding deviceconfig Custom Resource(CR), the test runner pod logs can be viewed by running ```kubectl logs``` command, for the above example it is:
```kubectl logs -n kube-amd-gpu test-deviceconfig-test-runner-r9gjr```

## Check test running node labels
When the test is ongoing the corresponding label will be added to the node resource: ```"amd.testrunner.GPU_HEALTH_CHECK.gst_single": "running"```, the test running label will be removed once the test completed.

## Check test result event
The test runner generated event can be found from the operator's namespace: 
```bash
$ kubectl get events -n kube-amd-gpu
LAST SEEN   TYPE     REASON       OBJECT                                    MESSAGE
107s        Normal   TestPassed   pod/test-deviceconfig-test-runner-r9gjr   {"35824":{"gpustress-8000-device-false":"success","gpustress-8000-dgemm-false":"success","gpustress-8000-dgemm-true":"success","gpustress-8000-hgemm-false":"success","gpustress-8000-hgemm-true":"success","gpustress-8000-sgemm-true":"success","gpustress-9000-sgemm-false":"success"}}
```

Test runner generated event can be retrieved by filtering the source component: ```kubectl get events -n kube-amd-gpu -o=jsonpath='{.items[?(@.source.component=="amd-test-runner")]}'``` Here is an example event resource:

```json
{
  "apiVersion": "v1",
  "count": 1,
  "eventTime": null,
  "firstTimestamp": "2025-01-06T21:55:55Z",
  "involvedObject": {
    "kind": "Pod",
    "name": "test-deviceconfig-test-runner-r9gjr",
    "namespace": "kube-amd-gpu"
  },
  "kind": "Event",
  "lastTimestamp": "2025-01-06T21:55:55Z",
  "message": "{\"35824\":{\"gpustress-8000-device-false\":\"success\",\"gpustress-8000-dgemm-false\":\"success\",\"gpustress-8000-dgemm-true\":\"success\",\"gpustress-8000-hgemm-false\":\"success\",\"gpustress-8000-hgemm-true\":\"success\",\"gpustress-8000-sgemm-true\":\"success\",\"gpustress-9000-sgemm-false\":\"success\"}}",
  "metadata": {
    "creationTimestamp": "2025-01-06T21:55:55Z",
    "generateName": "amd-test-runner-gpu_health_check-auto_unhealthy_gpu_watch-gst_single-",
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
            "f:generateName": {}
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
        "time": "2025-01-06T21:55:55Z"
      }
    ],
    "name": "amd-test-runner-gpu_health_check-auto_unhealthy_gpu_watch-8q8xc",
    "namespace": "kube-amd-gpu",
    "resourceVersion": "10788979",
    "uid": "edf07bd9-cd50-4606-8138-9782ce1b4ee4"
  },
  "reason": "TestPassed",
  "reportingComponent": "",
  "reportingInstance": "",
  "source": {
    "component": "amd-test-runner",
    "host": "leto"
  },
  "type": "Normal"
}
```

* ```involvedObject``` shows which test runner pod executed the test and generated the event.
* ```lastTimestamp``` shows the time when event was generated.
* ```message``` contains detailed test result, which can be further passsed into json format: 
 
```bash
$ kubectl get events -o=jsonpath='{.items[?(@.source.component=="amd-test-runner")]}' -n kube-amd-gpu | jq -r .message | jq .
{
  "35824": {
    "gpustress-8000-device-false": "success",
    "gpustress-8000-dgemm-false": "success",
    "gpustress-8000-dgemm-true": "success",
    "gpustress-8000-hgemm-false": "success",
    "gpustress-8000-hgemm-true": "success",
    "gpustress-8000-sgemm-true": "success",
    "gpustress-9000-sgemm-false": "success"
  }
}
```
in the above example ```35824``` is the GPU's GUID, ```gpustress-8000-device-false``` represents a specific test action and ```success``` is the action's result.

* ```metadata``` contains the basic information of event.
* ```reason``` gives an overall result of the whole test run, it could be ```TestPassed```, ```TestFailed``` or ```TestTimedOut```.
* ```source``` shows where the event came from, including component name ```amd-test-runner``` and worker node's host name.
* ```type``` classifies the event into different level. For test runner generated event, ```TestPassed``` events are assigned with ```Normal``` event type while ```TestFailed``` and ```TestTimedOut``` events are assigned with ```Warning``` event type.

## Advanced Configuration - Test Recipes
You can provide a config map to specify test recipe details for the test runner. Create the config map then specify the config map name in the deviceconfig Custom Resource(CR) for test runner to pick up the config. Here is an example config map:

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

* Test Recipes:

  Please check the [Appendix](./appendix-test-recipe.md) for more details about all available test recipes.

