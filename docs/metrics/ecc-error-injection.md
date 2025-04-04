## ECC Error Injection Testing

The Metric Exporter has the capability to check for unhealthy GPUs via the monitoring of ECC Errors that can occur when a GPU is not functioning as expected. When an ECC error is detected the Metrics Exporter will now mark the offending GPU as unhealthy and add a node label to indicate which GPU on the node is unhealthy. The Kubernetes Device Plugin also listens to the health metrics coming from the Metrics Exporter to determine GPU status, marking GPUs as schedulable if healthy and unschedulable if unhealthy.

This health check workflow runs automatically on every node the Device Metrics Exporter is running on, with the Metrics Exporter polling GPUs every 30 seconds and the device plugin checking health status at the same interval, ensuring updates within one minute. Users can customize the default ECC error threshold (set to 0) via the `HealthThresholds` field in the metrics exporter ConfigMap. As part of this workflow healthy GPUs are made available for Kubernetes job scheduling, while ensuring no new jobs are scheduled on an unhealthy GPUs.

## To do error injection follow these steps

We have added a new `metricsclient` to the Device Metrics Exporter pod that can be used to inject ECC errors into an otherwise healthy GPU for testing the above health check workflow. This is fairly simple and don't worry this does not harm your GPU as any errors that are being injected are debugging in nature and not real errors. The steps to do this have been outlined below:

### 1. Set Node Name

Use an environment variable to set the Kubernetes node name to indicate which node you want to test error injection on:

```bash
NODE_NAME=<node-name>
```

Replace <node-name> with the name of the node you want to test. If you are running this from the same node you want to test you can grab the hostname using:

```bash
NODE_NAME=$(hostname)
```

### 2. Set Metrics Exporter Pod Name

Since you have to execute the `metricsclient` from directly within the Device Metrics Exporter pod we need to get the Metrics Exporter pod name running on the node:

```bash
METRICS_POD=$(kubectl get pods -n kube-amd-gpu --field-selector spec.nodeName=$NODE_NAME --no-headers -o custom-columns=":metadata.name" | grep '^gpu-operator-metrics-exporter-' | head -n 1)
```

### 3. Check Metrics Client to see GPU Health

Now that you have the name of the metrics exporter pod you can use the metricsclient to check the current health of all GPUs on the node:

```bash
kubectl exec -n kube-amd-gpu $METRICS_POD -c metrics-exporter-container -- metricsclient
```

You should see a list of all the GPUs on that node along with their corresponding status. In most cases all GPUs should report as being `healthy`.

```bash
ID      Health  Associated Workload
------------------------------------------------
1       healthy []
0       healthy []
7       healthy []
6       healthy []
5       healthy []
4       healthy []
3       healthy []
2       healthy []
------------------------------------------------
```

### 4. Inject ECC Errors on GPU 0

In order to simulate errors on a GPU we will be using a json file that specifies a GPU ID along with counters for several ECC Uncorrectable error fields that are being monitored by the Device Metrics Exporter. In the below example you can see that we are specifying `GPU 0` and injecting 1 `GPU_ECC_UNCORRECT_SEM` error and 2 `GPU_ECC_UNCORRECT_FUSE` errors. We use the `metricslient -ecc-file-path <file.json>` command to specify the json file we want to inject into the metrics table. To create the json file and execute the metricsclient command all in in one go run the following:

```bash
kubectl exec -n kube-amd-gpu $METRICS_POD -c metrics-exporter-container -- sh -c 'cat > /tmp/ecc.json <<EOF
{
        "ID": "0",
        "Fields": [
                "GPU_ECC_UNCORRECT_SEM",
                "GPU_ECC_UNCORRECT_FUSE"
        ],
        "Counts" : [
                1, 2
        ]
}
EOF
metricsclient -ecc-file-path /tmp/ecc.json'
```

The metricsclient should report back the current status of the GPUs as well as the new json string you just injected.

```bash
ID      Health  Associated Workload
------------------------------------------------
6       healthy []
5       healthy []
4       healthy []
3       healthy []
2       healthy []
1       healthy []
0       healthy []
7       healthy []
------------------------------------------------
{"ID":"0","Fields":["GPU_ECC_UNCORRECT_SEM","GPU_ECC_UNCORRECT_FUSE"]}
```

### 5. Query the Mericsclient to See the Unhealthy GPU

Since the Metric Exporter will check every 30 seconds for GPU health status you will need to wait this amount of time before executing the following command again to see the unhealthy GPU:

```bash
kubectl exec -n kube-amd-gpu $METRICS_POD -c metrics-exporter-container -- metricsclient
```

You should now see that one of the GPUs, `GPU 0`, in this case has been marked as unhealthy:

```bash
 ID      Health  Associated Workload
------------------------------------------------
0       unhealthy       []
7       healthy []
6       healthy []
5       healthy []
4       healthy []
3       healthy []
2       healthy []
1       healthy []
------------------------------------------------
```

### 6. Checking the Unhealthy GPU Node label

The Metrics Exporter should of also added an unhealthy GPU label to your affected node to identify which GPU is unhealthy. Run the following to check for unhealth gpu node labels:

```bash
kubectl describe node $NODE_NAME | grep unhealthy
```

The command should return back one label indicating `gpu.0.state` as unhealthy:

```yaml
metricsexporter.amd.com.gpu.0.state=unhealthy
```

### 7. Check Number of Allocatable GPUs

In order to confirm that the unhealthy GPU resource has in fact been removed from the Kubernetes Scheduler we can check the number of total GPUs on the node and compare it with the number of allocatable GPUs. To do so run the following:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,"Total GPUs:.status.capacity.amd\.com/gpu","Allocatable GPUs:.status.allocatable.amd\.com/gpu"
```

You should now have one less GPU that is allocatable on your node:

```bash
NAME                     Total GPUs   Allocatable GPUs
amd-mi300x-gpu-worker1   8            7
```

### 8. Clear ECC Errors on GPU 0

Now that we have tested to ensure the Health Check workflow is working we can clear the ECC errors on GPU0 by using the metrics client in a similar fashion to 4. This time we are setting the error counts to 0 for both GPU_ECC_UNCORRECT error fields.

```bash
kubectl exec -n kube-amd-gpu $METRICS_POD -c metrics-exporter-container -- sh -c 'cat > /tmp/delete_ecc.json <<EOF
{
        "ID": "0",
        "Fields": [
                "GPU_ECC_UNCORRECT_SEM",
                "GPU_ECC_UNCORRECT_FUSE"
        ],
        "Counts" : [
                0, 0
        ]
}
EOF
metricsclient -ecc-file-path /tmp/delete_ecc.json'
```

### 9. Check to see GPU 0 Become Healthy Again

After waiting another 30 seconds or so you can check the metrics client again to see that all GPUs are now healthy again:

```bash
kubectl exec -n kube-amd-gpu $METRICS_POD -c metrics-exporter-container -- metricsclient
```

You should see the following:

```bash
ID      Health  Associated Workload
------------------------------------------------
4       healthy []
3       healthy []
2       healthy []
1       healthy []
0       healthy []
7       healthy []
6       healthy []
5       healthy []
------------------------------------------------
```

### 10. Check that all GPUs are Allocatable Again

Lastly check the number of allocatable GPUs on your node to ensure that it matches the total number of GPUs:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,"Total GPUs:.status.capacity.amd\.com/gpu","Allocatable GPUs:.status.allocatable.amd\.com/gpu"
```

Following the above steps will help you successfully test the new GPU Health Check Feature.
