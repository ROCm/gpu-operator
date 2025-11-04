set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Tainting node $NODE_NAME"
kubectl taint node "$NODE_NAME" amd-gpu-unhealthy="{{inputs.parameters.node_condition}}":NoSchedule --overwrite