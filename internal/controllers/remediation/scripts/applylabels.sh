set -e
NODE_NAME="{{inputs.parameters.node_name}}"
NODE_LABELS="{{inputs.parameters.node_labels}}"

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not present in the utils container. Proceeding without applying labels on the node"
    exit 0
fi

# Get the length of the array
LENGTH=$(echo "$NODE_LABELS" | jq 'length')

if [ "$LENGTH" -eq 0 ]; then
    echo "No labels to apply"
    exit 0
fi

echo "Applying $LENGTH labels to node '$NODE_NAME'..."

# Loop through each label in the JSON array
for i in $(seq 0 $((LENGTH - 1))); do
    LABEL=$(echo "$NODE_LABELS" | jq -r ".[$i]")
    
    if [ "$LABEL" == "null" ] || [ -z "$LABEL" ]; then
        echo "Warning: Skipping empty label at index $i"
        continue
    fi
    
    echo "Applying label '$LABEL'..."
    
    if kubectl label node "$NODE_NAME" "$LABEL" --overwrite; then
        echo "Successfully applied label '$LABEL'"
    else
        echo "Failed to apply label '$LABEL'"
    fi
done

echo "Done applying all labels to node '$NODE_NAME'"
