# Manual/Scheduled Test

## Manual test trigger

To start the manual test, directly use the test runner image to create the Kubernetes job and related resources, then the test will be triggered manually.

## Use Case 1 - GPU is unhealthy on the node

When any GPU on a specific worker node is unhealthy, you can manually trigger a test / benchmark run on that worker node to check more details on the unhealthy state. The test job requires RBAC config to grant the test runner access to export events and add node labels to the cluster. Here is an example of configuring the RBAC and Job resources:

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
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      nodeSelector:
        kubernetes.io/hostname: node1 # requesting to run test on node1
      volumes: # mount driver related directory and device interface
      - name: kfd
        hostPath:
          path: /dev/kfd
          type: CharDevice
      - name: dri
        hostPath:
          path: /dev/dri
          type: Directory
      containers:
      - name: amd-test-runner
        image: docker.io/rocm/test-runner:v1.2.0-beta.0
        imagePullPolicy: IfNotPresent
        securityContext: # setup security context for container to get access to device related interfaces
          privileged: true
        volumeMounts:
        - mountPath: /dev/dri
          name: dri
        - mountPath: /dev/kfd
          name: kfd
        env:
        - name: TEST_TRIGGER
          value: "MANUAL" # Set the TEST_TRIGGER environment variable to MANUAL for manual test
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
      restartPolicy: Never
  backoffLimit: 0
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```

## Use Case 2 - GPUs are healthy on the node

When all the GPUs on a specific worker node are healthy, you can manually trigger a benchmark test run by requesting all the GPU resources ```amd.com/gpu``` on that worker node. The test job requires RBAC config to grant the test runner access to export events and add node labels to the cluster. Here is an example of configuring the RBAC and Job resources:

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
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      nodeSelector:
        kubernetes.io/hostname: node1 # requesting to run test on node1
      containers:
      - resources:
          limits:
            amd.com/gpu: 8 # requesting all GPUs on the node
        name: amd-test-runner
        image: docker.io/rocm/test-runner:v1.2.0-beta.0
        imagePullPolicy: IfNotPresent
        env:
        - name: TEST_TRIGGER
          value: "MANUAL" # Set the TEST_TRIGGER environment variable to MANUAL for manual test
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
      restartPolicy: Never
  backoffLimit: 0
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```

When test is running:

```bash
$ kubectl get job
NAME                         STATUS    COMPLETIONS   DURATION   AGE
test-runner-manual-trigger   Running   0/1           31s        31s

$ kubectl get pod
NAME                               READY   STATUS    RESTARTS   AGE
test-runner-manual-trigger-fnvhn   1/1     Running   0          65s
```

When test is completed:

```bash
$ kubectl get job
NAME                         STATUS     COMPLETIONS   DURATION   AGE
test-runner-manual-trigger   Complete   1/1           6m10s      7m21s

$ kubectl get pod
NAME                               READY   STATUS      RESTARTS   AGE
test-runner-manual-trigger-fnvhn   0/1     Completed   0          7m19s
```

## Scheduled Job

Furthermore, test runner images can also be utilized in Kubernetes CronJob, which allows the test job to be scheduled in the cluster.

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
apiVersion: batch/v1
kind: CronJob
metadata:
  name: test-runner-manual-trigger-cron-job-midnight
spec:
  # check specific schedule config at https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#writing-a-cronjob-spec
  schedule: "0 0 * * *" # This schedule runs the job daily at midnight
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: test-run
          nodeSelector:
            kubernetes.io/hostname: node1 # requesting to run test on node1
          volumes: # mount driver related directory and device interface
          - name: kfd
            hostPath:
              path: /dev/kfd
              type: CharDevice
          - name: dri
            hostPath:
              path: /dev/dri
              type: Directory
          containers:
          - name: init-test-runner
            image: docker.io/rocm/test-runner:v1.2.0-beta.0
            imagePullPolicy: IfNotPresent
            securityContext: # setup security context for container to get access to device related interfaces
              privileged: true
            volumeMounts:
            - mountPath: /dev/dri
              name: dri
            - mountPath: /dev/kfd
              name: kfd
            env:
            - name: TEST_TRIGGER
              value: "MANUAL"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          restartPolicy: Never
      backoffLimit: 0
      ttlSecondsAfterFinished: 120
```

When the job gets scheduled, the CronJob resource will show active jobs and the job and pod resources will be created.

```bash
$ kubectl get cronjob
NAME                                           SCHEDULE     TIMEZONE   SUSPEND   ACTIVE   LAST SCHEDULE   AGE
test-runner-manual-trigger-cron-job-midnight   0 0 * * *   <none>     False     1        2s              86s

$ kubectl get job
NAME                                                    STATUS    COMPLETIONS   DURATION   AGE
test-runner-manual-trigger-cron-job-midnight-28936820   Running   0/1           6s         6s

$ kubectl get pod
NAME                                                          READY   STATUS    RESTARTS   AGE
test-runner-manual-trigger-cron-job-midnight-28936820-kkqnj   1/1     Running   0          16s
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

You can create a config map to customize the test triggger and recipe configs. For the example config map and explanation please check [this section](./auto-unhealthy-device-test.md#advanced-configuration---configmap).

After creating the config map, you can specify the volume and volume mount to mount the config map into test runner container.

* In the config map the file name must be named as ```config.json```
* Within the test runner container the mount path should be ```/etc/test-runner/```

Here is an example of applying the customized config map

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
apiVersion: v1
kind: ConfigMap
metadata:
  name: manual-config-map
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
                "MANUAL": {
                  "TestCases": [
                    {
                      "Recipe": "babel",
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
---
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      nodeSelector:
        kubernetes.io/hostname: node1 # requesting to run test on node1
      volumes: # mount driver related directory and device interface
      - name: kfd
        hostPath:
          path: /dev/kfd
          type: CharDevice
      - name: dri
        hostPath:
          path: /dev/dri
          type: Directory
      - name: config-volume
        configMap:
          name: manual-config-map
      containers:
      - name: amd-test-runner
        image: docker.io/rocm/test-runner:v1.2.0-beta.0
        imagePullPolicy: IfNotPresent
        securityContext: # setup security context for container to get access to device related interfaces
          privileged: true
        volumeMounts:
        - mountPath: /dev/dri
          name: dri
        - mountPath: /dev/kfd
          name: kfd
        - mountPath: /etc/test-runner/
          name: config-volume
        env:
        - name: TEST_TRIGGER
          value: "MANUAL" # Set the TEST_TRIGGER environment variable to MANUAL for manual test
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
      restartPolicy: Never
  backoffLimit: 0
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```

## Advanced Configuration - Logs Mount

The test runner compressed test logs are saved at ```/var/log/amd-test-runner/``` folder within container. If you don't configure the logs mount, by default the logs won't be exported. There are many types of [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/) to mount into the test runner container so that the test logs can be persisted at your desired place.

Here is an example of mounting the ```hostPath``` into the container, the key points are:

* Define a ```hostPath``` volume
* Mount the volume to a directory within test runner container
* Use ```LOG_MOUNT_DIR``` environment variable to ask test runner to save logs into the mounted directory

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
apiVersion: v1
kind: ConfigMap
metadata:
  name: manual-config-map
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
                "MANUAL": {
                  "TestCases": [
                    {
                      "Recipe": "babel",
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
---
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      nodeSelector:
        kubernetes.io/hostname: node1 # requesting to run test on node1
      volumes: # mount driver related directory and device interface
      - name: kfd
        hostPath:
          path: /dev/kfd
          type: CharDevice
      - name: dri
        hostPath:
          path: /dev/dri
          type: Directory
      - name: config-volume
        configMap:
          name: manual-config-map
      - hostPath: # Specify to use this directory on the host as volume
          path: /var/log/amd-test-runner
          type: DirectoryOrCreate
        name: test-runner-volume
      containers:
      - name: amd-test-runner
        image: docker.io/rocm/test-runner:v1.2.0-beta.0
        imagePullPolicy: IfNotPresent
        securityContext: # setup security context for container to get access to device related interfaces
          privileged: true
        volumeMounts:
        - mountPath: /dev/dri
          name: dri
        - mountPath: /dev/kfd
          name: kfd
        - mountPath: /var/log/amd-test-runner # Specify to mount host path volume into specific directory
          name: test-runner-volume
        - mountPath: /etc/test-runner/
          name: config-volume
        env:
        - name: LOG_MOUNT_DIR # Use LOG_MOUNT_DIR envrionment variable to ask test runner to save logs in mounted directory
          value: /var/log/amd-test-runner
        - name: TEST_TRIGGER
          value: "MANUAL" # Set the TEST_TRIGGER environment variable to MANUAL for manual test
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
      restartPolicy: Never
  backoffLimit: 0
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```

## Cleanup Manual / Scheduled Test

When you create the manual or scheduled test resources, it is recommended to put all of them into one YAML file. By running commands like ```kubectl apply -f xxx.yaml``` all the related resources will be created. When you want to remove those resources jus run commands ```kubectl delete -f xxx.yaml``` to remove those resources from the cluster.

```{warning}
  For the Manual or Scheduled Test run, when you delete the resources that interrupts the test run, you need to double check the node labels to manually remove the test running label like ```"amd.testrunner.gpu_health_check.gst_single": "running"```.
```
