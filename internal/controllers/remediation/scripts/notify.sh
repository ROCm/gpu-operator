set -e
NODE_NAME="{{inputs.parameters.nodeName}}"
NOTIFY_MESSAGE="{{inputs.parameters.notifyMessage}}"
EVENT_NAME="{{inputs.parameters.eventName}}"

kubectl create -f - <<EOF
apiVersion: v1
kind: Event
metadata:
  namespace: {{workflow.namespace}}
  generateName: ${EVENT_NAME}-
  labels:
    app.kubernetes.io/part-of: amd-gpu-operator
firstTimestamp: $(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
involvedObject:
  apiVersion: v1
  kind: Node
  name: ${NODE_NAME}
  namespace: {{workflow.namespace}}
message: '${NOTIFY_MESSAGE}'
reason: AMDGPUUnhealthy
reportingComponent: amd-gpu-node-remediation-workflow
reportingInstance: amd-gpu-node-remediation-workflow
source:
  component: {{workflow.name}}
  host: ${NODE_NAME}
type: Warning
EOF