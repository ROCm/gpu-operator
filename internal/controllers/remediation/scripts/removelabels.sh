set -e
NODE_NAME="{{inputs.parameters.node_name}}"
NODE_LABELS="{{inputs.parameters.node_labels}}"

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not present in the utils container. Proceeding without removing labels from the node"
    exit 0
fi

# Get the length of the array
LENGTH=$(echo "$NODE_LABELS" | jq 'length')

if [ "$LENGTH" -eq 0 ]; then
    echo "No labels to remove"
    exit 0
fi

echo "Removing $LENGTH labels from node '$NODE_NAME'..."

# Loop through each label in the JSON array
for i in $(seq 0 $((LENGTH - 1))); do
    LABEL=$(echo "$NODE_LABELS" | jq -r ".[$i]")
    
    if [ "$LABEL" == "null" ] || [ -z "$LABEL" ]; then
        echo "Warning: Skipping empty label at index $i"
        continue
    fi
    
    # Extract the key from the key=value format
    LABEL_KEY="${LABEL%%=*}"
    
    if [ -z "$LABEL_KEY" ]; then
        echo "Warning: Could not extract key from label '$LABEL'"
        continue
    fi
    
    echo "Removing label key '$LABEL_KEY' (from '$LABEL')..."
    
    if kubectl label node "$NODE_NAME" "$LABEL_KEY"-; then
        echo "Successfully removed label '$LABEL_KEY'"
    else
        echo "Failed to remove label '$LABEL_KEY'"
    fi
done

echo "Done removing all labels from node '$NODE_NAME'"