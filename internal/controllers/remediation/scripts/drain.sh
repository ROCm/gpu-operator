set -e
echo "Fetching node name..."
NODE_NAME="{{inputs.parameters.node_name}}"
DRAIN_POLICY="{{inputs.parameters.drain_policy}}"

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not present in the utils container. Cannot parse drain policy"
    exit 1
fi

# Parse drain policy JSON and extract fields
FORCE=$(echo "$DRAIN_POLICY" | jq -r '.force')
TIMEOUT_SECONDS=$(echo "$DRAIN_POLICY" | jq -r '.timeoutSeconds')
GRACE_PERIOD_SECONDS=$(echo "$DRAIN_POLICY" | jq -r '.gracePeriodSeconds')
IGNORE_DAEMONSETS=$(echo "$DRAIN_POLICY" | jq -r '.ignoreDaemonSets')

# Parse ignoreNamespaces as an array
if [ "$(echo "$DRAIN_POLICY" | jq -r '.ignoreNamespaces')" != "null" ]; then
    readarray -t IGNORE_NAMESPACES < <(echo "$DRAIN_POLICY" | jq -r '.ignoreNamespaces[]')
else
    IGNORE_NAMESPACES=()
fi

echo "Drain policy configuration:"
echo "  Force: $FORCE"
echo "  Timeout: $TIMEOUT_SECONDS seconds"
echo "  Grace period: $GRACE_PERIOD_SECONDS seconds"
echo "  Ignore DaemonSets: $IGNORE_DAEMONSETS"
echo "  Ignore Namespaces: ${IGNORE_NAMESPACES[*]}"

echo "Identified node: $NODE_NAME"
echo "Finding pods on node $NODE_NAME matching the drain policy criteria..."

# Convert IGNORE_NAMESPACES array to JSON array for jq
IGNORE_NAMESPACES_JSON=$(printf '%s\n' "${IGNORE_NAMESPACES[@]}" | jq -R . | jq -s .)

PODS=$(kubectl get pods --all-namespaces -o json | jq --argjson ignoreNs "$IGNORE_NAMESPACES_JSON" --arg ignoreDaemonSets "$IGNORE_DAEMONSETS" -r '
  .items[] |
    select(.spec.nodeName == "'"$NODE_NAME"'") |
    select((.metadata.namespace as $ns | $ignoreNs | index($ns) | not)) |
    select(
      if $ignoreDaemonSets == "true" then
        ([.metadata.ownerReferences[]? | select(.kind == "DaemonSet")] | length) == 0
      else
        true
      end
    ) |
    "\(.metadata.namespace) \(.metadata.name)"
')
if [ -z "$PODS" ]; then
  echo "No pods matching the drain policy criteria found on node $NODE_NAME."
  exit 0
fi

echo "Draining pods:"
echo "$PODS"
echo "$PODS" | while read -r ns name; do
  echo "Deleting pod $name in namespace $ns"

  # Build kubectl delete command with drain policy settings
  DELETE_CMD="kubectl delete pod \"$name\" -n \"$ns\""

  # Add --grace-period if specified
  if [ "$GRACE_PERIOD_SECONDS" != "null" ] && [ -n "$GRACE_PERIOD_SECONDS" ]; then
    DELETE_CMD="$DELETE_CMD --grace-period=$GRACE_PERIOD_SECONDS"
  fi

  # Add --timeout if specified
  if [ "$TIMEOUT_SECONDS" != "null" ] && [ -n "$TIMEOUT_SECONDS" ]; then
    DELETE_CMD="$DELETE_CMD --timeout=${TIMEOUT_SECONDS}s"
  fi

  # Add --force flag if FORCE is true
  if [ "$FORCE" = "true" ]; then
    DELETE_CMD="$DELETE_CMD --force"
  fi

  eval "$DELETE_CMD" || true
done