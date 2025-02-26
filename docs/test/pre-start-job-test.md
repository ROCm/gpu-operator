# Pre-start Job Test

## Pre-start Job Test trigger

Test runner can be embedded as an init container within your Kubernetes workload pod definition. The init container will be executed before the actual workload containers start, in that way the system could be tested right before the workload start to use the hardware resource.

## Configure pre-start init container

The init container requires RBAC config to grant the pod access to export events and add node labels to the cluster. Here is an example of configuring the RBAC and Job resources:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: test-run
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: test-run-cluster-role
rules:
- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - get
  - list
  - watch
  - create
  - update
- apiGroups:
  - ""
  resources:
  - nodes
  verbs:
  - patch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: test-run-rb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: test-run-cluster-role
subjects:
- kind: ServiceAccount
  name: test-run
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pytorch-gpu-deployment
  namespace: default
  labels:
    purpose: demo-pytorch-amdgpu
spec:
  replicas: 1
  selector:
    matchLabels:
      purpose: demo-pytorch-amdgpu
  template:
    metadata:
      labels:
        purpose: demo-pytorch-amdgpu
    spec:
      serviceAccountName: test-run
      initContainers:
      - name: init-test-runner
        image: docker.io/rocm/test-runner:v1.2.0-beta.0
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            amd.com/gpu: 1 # requesting a GPU
        env:
        - name: TEST_TRIGGER
          value: "PRE_START_JOB_CHECK" # Set the TEST_TRIGGER environment variable to PRE_START_JOB_CHECK for test runner as init container
        - name: POD_NAME # Use downward API to pass pod name to test runner container
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE # Use downward API to pass pod namespace to test runner container
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: NODE_NAME # Use downward API to pass host name to test runner container
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
      containers:
      - name: pytorch-gpu-workload
        image: busybox:latest
        command: ["/bin/sh", "-c", "--"]
        args: ["sleep 6000"]
        resources:
          limits:
            amd.com/gpu: 1 # requesting a GPU
```

## Check test runner init container

When test runner is running:

```bash
$ kubectl get pod
NAME                                      READY   STATUS     RESTARTS   AGE
pytorch-gpu-deployment-7c6bb979f5-p2wlk   0/1     Init:0/1   0          2m52s
```

Check test runner container logs:
```$ kubectl logs pytorch-gpu-deployment-7c6bb979f5-p2wlk -c init-test-runner```

When test runner is completed, the workload container started to run:

```bash
$ kubectl get pod
NAME                                      READY   STATUS    RESTARTS   AGE
pytorch-gpu-deployment-7c6bb979f5-p2wlk   1/1     Running   0          7m46s
```

## Check test running node labels

When the test is ongoing the corresponding label will be added to the node resource: ```"amd.testrunner.gpu_health_check.gst_single": "running"```, the test running label will be removed once the test completed.

## Check test result event

The test runner generated event can be found from Job resource defined namespace

```bash
$ kubectl get events -n kube-amd-gpu
LAST SEEN   TYPE      REASON                    OBJECT                                            MESSAGE
8m8s        Normal    TestFailed                pod/test-runner-manual-trigger-c4hpw              [{"number":1,"suitesResult":{"42924":{"gpustress-3000-dgemm-false":"success","gpustress-41000-fp32-false":"failure","gst-1215Tflops-4K4K8K-rand-fp8":"failure","gst-8096-150000-fp16":"success"}}}]
```

More detailed information about test result events can be found in [this section](./auto-unhealthy-device-test.md#check-test-result-event).

## Advanced Configuration - ConfigMap

You can create a config map to customize the test triggger and recipe configs. For the example config map and explanation please check [this section](./auto-unhealthy-device-test.md#advanced-test-configuration).

After creating the config map, you can specify the volume and volume mount to mount the config map into test runner container.

* In the config map the file name must be named as ```config.json```
* Within the test runner container the mount path should be ```/etc/test-runner/```

The example of mounting config map into test runner container can be found in [this section](./manual-test.md#advanced-configuration---configmap).

## Advanced Configuration - Logs Mount

The test runner compressed test logs are saved at ```/var/log/amd-test-runner/``` folder within container. If you don't configure the logs mount, by default the logs won't be exported. There are many types of [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/) to mount into the test runner container so that the test logs can be persisted at your desired place.

Here is an example of mounting the ```hostPath``` into the container, the key points are:

* Define a ```hostPath``` volume
* Mount the volume to a directory within test runner container
* Use ```LOG_MOUNT_DIR``` environment variable to ask test runner to save logs into the mounted directory

The example of mounting the ```hostPath``` volume into test runner container can be found at [this section](./manual-test.md#advanced-configuration---logs-mount).
