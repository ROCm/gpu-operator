set -e
NODE_NAME="{{inputs.parameters.node_name}}"
JOB_NAME="test-runner-manual-trigger-${NODE_NAME}"
CM_NAME="manual-config-map-${NODE_NAME}"
FRAMEWORK="{{inputs.parameters.framework}}"
RECIPE="{{inputs.parameters.recipe}}"
ITERATIONS="{{inputs.parameters.iterations}}"
STOPONFAILURE="{{inputs.parameters.stopOnFailure}}"
TIMEOUTSECONDS="{{inputs.parameters.timeoutSeconds}}"
TESTRUNNERIMAGE="{{inputs.parameters.testRunnerImage}}"
TESTRUNNERSA="{{inputs.parameters.testRunnerServiceAccount}}"
NAMESPACE="{{inputs.parameters.namespace}}"

if [ -z "$FRAMEWORK" ] || [ -z "$RECIPE" ] || [ -z "$ITERATIONS" ] || [ -z "$STOPONFAILURE" ] || [ -z "$TIMEOUTSECONDS" ]; then
  echo "Validation profile incomplete, skipping configmap and job creation. Please enter framework, recipe, iterations, stopOnFailure, timeoutSeconds as per testrunner requirements"
  exit 0
fi

echo "Creating test runner Job $JOB_NAME and ConfigMap $CM_NAME..."

cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${CM_NAME}
  namespace: ${NAMESPACE}
data:
  config.json: |
    {
      "TestConfig": {
        "GPU_HEALTH_CHECK": {
          "TestLocationTrigger": {
            "${NODE_NAME}": {
              "TestParameters": {
                "MANUAL": {
                  "TestCases": [
                    {
                      "Framework": "${FRAMEWORK}",
                      "Recipe": "${RECIPE}",
                      "Iterations": "${ITERATIONS}",
                      "StopOnFailure": "${STOPONFAILURE}",
                      "TimeoutSeconds": "${TIMEOUTSECONDS}"
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
  name: ${JOB_NAME}
  namespace: ${NAMESPACE}
spec:
  ttlSecondsAfterFinished: 120
  backoffLimit: 0
  template:
    spec:
      serviceAccountName: "${TESTRUNNERSA}"
      nodeSelector:
        kubernetes.io/hostname: ${NODE_NAME}
      tolerations:
      - key: "amd-gpu-unhealthy"
        operator: "Exists"
        effect: "NoSchedule"
      restartPolicy: Never
      volumes:
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
            name: ${CM_NAME}
        - hostPath:
            path: /var/log/amd-test-runner
            type: DirectoryOrCreate
          name: test-runner-volume
      containers:
        - name: amd-test-runner
          image: "${TESTRUNNERIMAGE}"
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          volumeMounts:
            - mountPath: /dev/dri
              name: dri
            - mountPath: /dev/kfd
              name: kfd
            - mountPath: /var/log/amd-test-runner
              name: test-runner-volume
            - mountPath: /etc/test-runner/
              name: config-volume
          env:
            - name: LOG_MOUNT_DIR # Use LOG_MOUNT_DIR environment variable to ask test runner to save logs in mounted directory
              value: /var/log/amd-test-runner
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
EOF

sleep 20

echo "Verifying Job creation..."
if ! kubectl get job "$JOB_NAME" -n "$NAMESPACE" &>/dev/null; then
  echo "Error: Job $JOB_NAME was not created in namespace $NAMESPACE"
  exit 1
fi

timeout=$((TIMEOUTSECONDS * ITERATIONS))
elapsed=0
echo "Overall timeout for the job is set to $timeout seconds."
echo "Waiting for Job $JOB_NAME to complete..."

while true; do
  job_status=$(kubectl get job "$JOB_NAME" -n "$NAMESPACE" -o jsonpath='{.status.conditions[0].type}' 2>/dev/null || true)
  if [ "$job_status" = "Complete" ]; then
    echo "Test runner job completed successfully."
	kubectl logs -n $NAMESPACE job/$JOB_NAME
    echo "Detailed run report can be found at /var/log/amd-test-runner"
    exit 0
  elif [ "$job_status" = "Failed" ]; then
    echo "Test runner job failed."
    kubectl logs -n $NAMESPACE job/$JOB_NAME
    echo "Detailed run report can be found at /var/log/amd-test-runner"
    exit 1
  else
    echo "Test runner job is still running. Waiting..."
    sleep 60
    elapsed=$((elapsed + 60))
    if [ "$elapsed" -gt "$timeout" ]; then
      echo "Timeout reached. Job did not complete within the specified time."
      exit 1
    fi
  fi
done