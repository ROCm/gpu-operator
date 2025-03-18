# Health Checks

## Features

Health Monitoring is done by the metrics exporter and this data is exposed
through grpc socket for the clients for consumption. K8 Device plugin
will make use the health socket to make AMD GPU available for the k8s
scheduling if the health is good else make it unavailable if health is bad.

GPU Operator has the necessary changes to bring up the k8s
cluster with health check feature if configured to include both device plugin and metrics exporter as part of the device configuration.

## Requirements

1. metrics exporter : v1.2.0 and up
2. k8s-device-plugin : latest
3. gpu operator : v1.2.0 and up

## Health Check Workflow

The health check workflow is automatic always on, and runs on every node as
demonset.

The default threshold for ECC error is 0, if the user wants to change ECC threshold then the user can set the HealthThresholds field in metrics exporter config map, more details can be found on the [device-metrics-exporter/README.md](https://github.com/ROCm/device-metrics-exporter/blob/main/README.md)

Metrics exporter polls the GPUs every 30 seconds to get the health status. Device plugin checks the health of the GPUs every 30 seconds to get the health status from the metrics exporter. Worst case the GPU health will get reflected at 1 min for change of health status.

### GPU Health Status : Healthy

The GPU health status if reported as "Healthy" on any node, makes the GPU available for
k8s jobs on that node. Any "Healthy" GPU is available for k8s for scheduling jobs.
This information is reflected on the `Node` details of the respective GPU's
compute node.

The command output `kubectl describe node <node_name>` will have the following
information.

1. Capacity will list all available GPU's reported disregarding the health
   status of the GPU on that node. This will not change unless the GPU is
   physically removed/added to the node.

   For a singe GPU node

   ```bash
   Capacity:
     amd.com/gpu:        1
   ```

2. Allocatable will reflect only the GPU's reported as Healthy on that node.
   If all the GPUs are healthy then this should be equal to the Capacity
   reported.

   For a singe GPU node

   ``` bash
   Allocatable:
     amd.com/gpu:        1
   ```

### GPU Health Status : Unhealthy

The GPU health status if reported as "Unhealthy" on any node, makes the GPU
unavailable for k8s jobs, any job requesting AMD GPU will not be scheduled in
unhealthy GPU, but if any job is already scheduled will not be evicted when
the GPU transitions from Healthy -> Unhealthy. If there are no job assciated
with the GPU and a new request for GPU on unhealthy is created on K8s, the Job
will be pending state and will not be allowed to run on an unhealthy GPU.

This will reduce the number of Allocatable entries on the node by the total
number of unhealthy GPU reported on that node.

The command output `kubectl describe node <node_name>` will have the following
information.

1. Label field will have the list of all GPU health status with index as below

   ```bash
   metricsexporter.amd.com.gpu.<GPU_ID>.state=unhealthy
   ```

2. Capacity will list all available GPU's reported disregarding the health
   status of the GPU on that node. This will not change unless the GPU is
   physically removed/added to the node.

   For a singe GPU node

   ```bash
   Capacity:
     amd.com/gpu:        1
   ```

3. Allocatable will reflect only the GPU's reported as Healthy on that node.
   If all the GPUs are healthy then this should be equal to the Capacity
   reported.

   For a singe GPU node

   ```bash
   Allocatable:
     amd.com/gpu:        0
   ```

## Health State Transitions

As per the current behavior, when a GPU get an uncorrectable ECC error, the
only way to bring this back to health state is to physically remove the GPU
and servicing it.

1. Healthy -> Unhealthy
2. Unhealthy -> Healthy (can be done through metricsclient tools provided though this
   is not expected in field)

[Testing Mock Tool](https://github.com/ROCm/device-metrics-exporter/blob/main/internal/README.md)
