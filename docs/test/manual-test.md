# Manual/Scheduled Test

## Manual test trigger

To start the manual test, test runner doesn't need to be brought up by operator. Just directly create the Kubernetes job resource by using the test runner image with proper configuration, then the test will be triggered.

## Configure manual test job
The test job requires RBAC config to grant the test runner access to export events and add node labels to the cluster. Here is an example of configuring the RBAC and Job resources:

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
      containers:
      - name: amd-test-runner
        image: registry.test.pensando.io:5000/test-runner/test-runner:dev
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            amd.com/gpu: 1 # requesting a GPU
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
  backoffLimit: 1
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```

## Check test runner job and pod

When test is running:
```
$ kubectl get job
NAME                         STATUS    COMPLETIONS   DURATION   AGE
test-runner-manual-trigger   Running   0/1           31s        31s

$ kubectl get pod
NAME                               READY   STATUS    RESTARTS   AGE
test-runner-manual-trigger-fnvhn   1/1     Running   0          65s
```

When test is completed:
```
$ kubectl get job
NAME                         STATUS     COMPLETIONS   DURATION   AGE
test-runner-manual-trigger   Complete   1/1           6m10s      7m21s

$ kubectl get pod
NAME                               READY   STATUS      RESTARTS   AGE
test-runner-manual-trigger-fnvhn   0/1     Completed   0          7m19s
```

## Scheduled Job
Furthermore, test runner images can also be utilized in Kubernetes CrobJob, which allow the test job to be regularly scheduled in the system.

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
          containers:
          - name: init-test-runner
            image: registry.test.pensando.io:5000/test-runner/test-runner:dev
            imagePullPolicy: IfNotPresent
            resources:
              limits:
                amd.com/gpu: 1 # requesting a GPU
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
      backoffLimit: 1
      ttlSecondsAfterFinished: 120
```
When the job gets scheduled, the CronJob resource will show active jobs and the job and pod resources will be created.

```
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
When the test is ongoing the corresponding label will be added to the node resource: ```"amd.testrunner.GPU_HEALTH_CHECK.gst_single": "running"```, the test running label will be removed once the test completed.

## Check test result event
The test runner generated event can be found from Job resource defined namespace
```bash
$ kubectl get events
LAST SEEN   TYPE     REASON       OBJECT                                    MESSAGE
107s        Normal   TestPassed   pod/test-deviceconfig-test-runner-r9gjr   {"35824":{"gpustress-8000-device-false":"success","gpustress-8000-dgemm-false":"success","gpustress-8000-dgemm-true":"success","gpustress-8000-hgemm-false":"success","gpustress-8000-hgemm-true":"success","gpustress-8000-sgemm-true":"success","gpustress-9000-sgemm-false":"success"}}
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
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      volumes:
      - name: config-volume
        configMap:
          name: test-runner-config-map
      containers:
      - name: amd-test-runner
        image: registry.test.pensando.io:5000/test-runner/test-runner:dev
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            amd.com/gpu: 1 # requesting a GPU
        volumeMounts:
        - name: config-volume
          mountPath: /etc/test-runner/
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
  backoffLimit: 1
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
apiVersion: batch/v1
kind: Job
metadata:
  name: test-runner-manual-trigger
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: test-run
      volumes:
      - hostPath: # Specify to use this directory on the host as volume
          path: /var/log/amd-test-runner
          type: DirectoryOrCreate
        name: test-runner-volume
      containers:
      - name: amd-test-runner
        image: registry.test.pensando.io:5000/test-runner/test-runner:dev
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            amd.com/gpu: 1 # requesting a GPU
        volumeMounts: # Specify to mount host path volume into specific directory
        - mountPath: /var/log/amd-test-runner
          name: test-runner-volume
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
  backoffLimit: 1
  ttlSecondsAfterFinished: 120 # TTL for the job to be auto cleaned
```


