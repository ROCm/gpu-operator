set -e
NODE_NAME='{{inputs.parameters.node_name}}'
OLD_BOOT_ID='{{inputs.parameters.old_boot_id}}'
WAIT_FOR_REBOOT_DURATION='{{inputs.parameters.wait_for_reboot_duration}}'
POLL_INTERVAL=30
STABLE_THRESHOLD=4

# Convert a Go-style duration string (e.g. "30s", "15m", "4h", "1h30m") into
# total seconds. Falls back to 900s (15m) if the value is empty or malformed.
# Sub-second units (ns/us/µs/ms) round down to 0 seconds.
MAX_SECONDS=$(awk -v d="$WAIT_FOR_REBOOT_DURATION" 'BEGIN {
  if (d == "") { print 900; exit }
  total = 0
  matched = 0
  while (match(d, /^[0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h)/)) {
    token = substr(d, 1, RLENGTH)
    d = substr(d, RLENGTH + 1)
    if (match(token, /(ns|us|µs|ms|s|m|h)$/) == 0) { print 900; exit }
    unit = substr(token, RSTART)
    num = substr(token, 1, RSTART - 1) + 0
    if      (unit == "h")  total += num * 3600
    else if (unit == "m")  total += num * 60
    else if (unit == "s")  total += num
    matched = 1
  }
  if (!matched || length(d) > 0) { print 900; exit }
  secs = int(total)
  if (secs <= 0) { print 900 } else { print secs }
}')

if [ -n "$OLD_BOOT_ID" ]; then
  echo "Waiting for node $NODE_NAME to reboot (old bootID: $OLD_BOOT_ID) and remain Ready for at least 2 minutes (timeout: ${WAIT_FOR_REBOOT_DURATION} / ${MAX_SECONDS}s)..."
else
  echo "Old bootID not provided; waiting for node $NODE_NAME to remain Ready for at least 2 minutes (timeout: ${WAIT_FOR_REBOOT_DURATION} / ${MAX_SECONDS}s)..."
fi

ELAPSED=0
STABLE_COUNT=0

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

echo "Timeout: Node $NODE_NAME did not reboot and remain Ready within ${WAIT_FOR_REBOOT_DURATION} (${MAX_SECONDS}s)."
exit 1
