set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Untainting node $NODE_NAME"
kubectl taint node "$NODE_NAME" amd-gpu-unhealthy:NoSchedule-