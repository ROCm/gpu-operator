set -e
NODE_NAME="{{inputs.parameters.node_name}}"
echo "Waiting for {{inputs.parameters.node_condition}} condition to be False on node $NODE_NAME for 2 consecutive minutes (timeout: 15 minutes)"
STABLE_COUNT=0
TOTAL_WAIT=0
while [ "$TOTAL_WAIT" -lt 15 ]; do
  STATUS=$(kubectl get node "$NODE_NAME" -o jsonpath="{.status.conditions[?(@.type=='{{inputs.parameters.node_condition}}')].status}")
  echo "[$(date)] {{inputs.parameters.node_condition}} status: $STATUS"
  if [ "$STATUS" = "False" ]; then
    STABLE_COUNT=$((STABLE_COUNT + 1))
    echo "Condition is stable (False) for $STABLE_COUNT minute(s)"
    if [ "$STABLE_COUNT" -ge 2 ]; then
      echo "Condition has been False for 2 consecutive checks (~2 minutes). Proceeding..."
      exit 0
    fi
  else
    STABLE_COUNT=0
    echo "Condition is not stable (status: $STATUS)."
  fi
  sleep 60
  TOTAL_WAIT=$((TOTAL_WAIT + 1))
done
echo "{{inputs.parameters.node_condition}} did not remain False for 2 consecutive minutes within 15 minutes. Exiting with failure."
exit 1