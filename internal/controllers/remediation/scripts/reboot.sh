set -e
NODE_NAME='{{inputs.parameters.node_name}}'
BOOT_ID_FILE=/tmp/boot_id

BOOT_ID=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.nodeInfo.bootID}' 2>/dev/null || true)
if [ -n "$BOOT_ID" ]; then
  printf '%s' "$BOOT_ID" > "$BOOT_ID_FILE"
  echo "Captured pre-reboot bootID for node $NODE_NAME: $BOOT_ID"
else
  # Fall back to an empty bootID; downstream wait step will degrade to a Ready-only check.
  printf '' > "$BOOT_ID_FILE"
  echo "Warning: could not capture bootID for node $NODE_NAME; downstream wait will fall back to Ready-only check." >&2
fi

echo "Triggering host reboot via nsenter (shutdown -r +1)..."
exec /nsenter --mount --pid --target=1 -- /sbin/shutdown -r +1
