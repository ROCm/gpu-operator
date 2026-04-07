#!/bin/bash
# Launch a privileged debug container on a GPU node for kernel-level diagnostics
# Usage: ./launch_debug_container.sh <node-name> [namespace]

set -e

NODE=${1:?Error: Node name required. Usage: $0 <node-name> [namespace]}
NS=${2:-kube-amd-gpu}
POD_NAME="gpu-debug-$NODE"

echo "=== Launching debug container on node: $NODE ==="
echo "Namespace: $NS"
echo "Pod name: $POD_NAME"
echo

# Delete any existing debug pod to avoid immutability conflicts
echo "Cleaning up any existing debug pod..."
kubectl --kubeconfig=$KUBECONFIG delete pod $POD_NAME -n $NS --ignore-not-found

echo "Creating privileged debug pod..."
cat <<EOF | kubectl --kubeconfig=$KUBECONFIG apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: $POD_NAME
  namespace: $NS
  labels:
    app: gpu-debug
    node: $NODE
spec:
  nodeName: $NODE
  hostPID: true
  hostNetwork: true
  restartPolicy: Never
  tolerations:
  - operator: Exists
  containers:
  - name: debug
    image: ubuntu:22.04
    command: ["sleep", "3600"]
    securityContext:
      privileged: true
    volumeMounts:
    - name: host-root
      mountPath: /host
    - name: dev
      mountPath: /dev
  volumes:
  - name: host-root
    hostPath:
      path: /
  - name: dev
    hostPath:
      path: /dev
EOF

echo "Waiting for pod to be ready..."
kubectl --kubeconfig=$KUBECONFIG wait pod/$POD_NAME -n $NS \
  --for=condition=Ready --timeout=60s

echo
echo "=== Debug pod ready! ==="
echo
echo "To enter the debug pod:"
echo "  kubectl --kubeconfig=\$KUBECONFIG exec -it $POD_NAME -n $NS -- bash"
echo
echo "Inside the debug pod, use chroot /host for all commands:"
echo "  chroot /host lsmod | grep amdgpu"
echo "  chroot /host ls /dev/kfd /dev/dri/"
echo "  chroot /host dmesg | grep -E 'amdgpu|GPU|FAULT' | tail -50"
echo
echo "To clean up when done:"
echo "  kubectl --kubeconfig=\$KUBECONFIG delete pod $POD_NAME -n $NS"
echo
