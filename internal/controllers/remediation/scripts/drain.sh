set -e
echo "Fetching node name..."
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Identified node: $NODE_NAME"
echo "Finding pods on node $NODE_NAME with volume mount path starting with /dev/dri..."
PODS=$(kubectl get pods --all-namespaces -o json | jq -r '
  .items[] |
    select(.spec.nodeName == "'"$NODE_NAME"'") |
    select(
      (
        [.spec.volumes[]? | select(.hostPath?.path != null and (.hostPath.path | startswith("/dev/dri")))]
        | length > 0
      ) or (
        [.spec.containers[]? | select(.resources.requests["amd.com/gpu"] != null)]
        | length > 0
      )
    ) |
    "\(.metadata.namespace) \(.metadata.name)"
')
if [ -z "$PODS" ]; then
  echo "No pods with /dev/dri mounts found on node $NODE_NAME."
else
  echo "Evicting pods:"
  echo "$PODS"
  echo "$PODS" | while read -r ns name; do
    echo "Deleting pod $name in namespace $ns"
    kubectl delete pod "$name" -n "$ns" --grace-period=0 --force || true
  done
fi