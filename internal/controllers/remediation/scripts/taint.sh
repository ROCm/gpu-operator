set -e
NODE_NAME="{{inputs.parameters.node_name}}"
NODE_TAINTS="{{inputs.parameters.node_taints}}"

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not present in the utils container. Proceeding without applying labels on the node"
    exit 0
fi

# Get the length of the array
LENGTH=$(echo "$NODE_TAINTS" | jq 'length')

for i in $(seq 0 $((LENGTH - 1))); do
    TAINT=$(echo "$NODE_TAINTS" | jq -r ".[$i]")
    if [ "$TAINT" == "null" ] || [ -z "$TAINT" ]; then
        echo "Warning: Skipping empty taint at index $i"
        continue
    fi
    echo "Tainting node $NODE_NAME with taint $TAINT"
    if kubectl taint node "$NODE_NAME" "$TAINT" --overwrite; then
        echo "Successfully applied taint '$TAINT'"
    else
        echo "Failed to apply taint '$TAINT'"
    fi
done

echo "Done applying all taints on node '$NODE_NAME'"