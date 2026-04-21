set -e
NODE_NAME='{{inputs.parameters.node_name}}'
OLD_BOOT_ID='{{inputs.parameters.old_boot_id}}'
TIMEOUT_MINUTES=15
POLL_INTERVAL=30
STABLE_THRESHOLD=4

if [ -n "$OLD_BOOT_ID" ]; then
  echo "Waiting for node $NODE_NAME to reboot (old bootID: $OLD_BOOT_ID) and remain Ready for at least 2 minutes (timeout: ${TIMEOUT_MINUTES} minutes)..."
else
  echo "Old bootID not provided; waiting for node $NODE_NAME to remain Ready for at least 2 minutes (timeout: ${TIMEOUT_MINUTES} minutes)..."
fi

ELAPSED=0
STABLE_COUNT=0
MAX_SECONDS=$((TIMEOUT_MINUTES * 60))

while [ "$ELAPSED" -lt "$MAX_SECONDS" ]; do
  READY=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
  CURRENT_BOOT_ID=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.nodeInfo.bootID}' 2>/dev/null || echo "")
  echo "[$(date)] Node Ready: $READY, current bootID: $CURRENT_BOOT_ID"

  REBOOT_CONFIRMED=true
  if [ -n "$OLD_BOOT_ID" ]; then
    if [ -z "$CURRENT_BOOT_ID" ] || [ "$CURRENT_BOOT_ID" = "$OLD_BOOT_ID" ]; then
      REBOOT_CONFIRMED=false
    fi
  fi

  if [ "$READY" = "True" ] && [ "$REBOOT_CONFIRMED" = "true" ]; then
    STABLE_COUNT=$((STABLE_COUNT + 1))
    echo "Node is Ready (and rebooted) for $STABLE_COUNT consecutive check(s)"
    if [ "$STABLE_COUNT" -ge "$STABLE_THRESHOLD" ]; then
      echo "Node $NODE_NAME confirmed rebooted (new bootID: $CURRENT_BOOT_ID) and Ready. Proceeding..."
      exit 0
    fi
  else
    if [ "$STABLE_COUNT" -gt 0 ]; then
      echo "Node became not Ready, resetting stability counter."
    fi
    STABLE_COUNT=0
    if [ "$REBOOT_CONFIRMED" = "false" ]; then
      echo "Node has not rebooted yet (bootID unchanged). Retrying in ${POLL_INTERVAL}s..."
    else
      echo "Node is not ready yet. Retrying in ${POLL_INTERVAL}s..."
    fi
  fi
  sleep "$POLL_INTERVAL"
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

echo "Timeout: Node $NODE_NAME did not reboot and remain Ready within ${TIMEOUT_MINUTES} minutes."
exit 1
