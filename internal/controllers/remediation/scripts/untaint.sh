set -e
NODE_NAME="{{inputs.parameters.node_name}}"
NODE_TAINTS="{{inputs.parameters.node_taints}}"

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not present in the utils container. Proceeding without applying labels on the node"
    exit 0
fi

LENGTH=$(echo "$NODE_TAINTS" | jq 'length')

for i in $(seq 0 $((LENGTH - 1))); do
    TAINT=$(echo "$NODE_TAINTS" | jq -r ".[$i]")
    if [ "$TAINT" == "null" ] || [ -z "$TAINT" ]; then
        echo "Warning: Skipping empty taint at index $i"
        continue
    fi
    echo "Removing taint $TAINT from node $NODE_NAME"
    if kubectl taint node "$NODE_NAME" "$TAINT"-; then
        echo "Successfully removed taint '$TAINT'"
    else
        echo "Failed to remove taint '$TAINT'"
    fi
done

echo "Done removing all remediation taints on node '$NODE_NAME'"