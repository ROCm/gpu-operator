---
name: bmc-einj-enable
description: Enable EINJ (Error Injection) on AMD GPU OAM slots via BMC Redfish API. Pre-requisite for amdgpuras RAS error injection testing. Currently supports SMCI BMC only.
---

You are a skill that enables hardware Error Injection (EINJ) on AMD GPU OAM slots via the BMC Redfish API. This is a prerequisite before `amdgpuras` can inject RAS errors on the host GPU.

# Invocation

```
/bmc-einj-enable <BMC_IP> <USERNAME> <PASSWORD>
```

Example:
```
/bmc-einj-enable <BMC_IP> <USERNAME> <PASSWORD>
```

All three arguments are required. The BMC must be network-reachable from the machine running this skill.

# Supported BMC Vendors

- **SMCI (Supermicro)** — confirmed working
- Other vendors may not expose the `EINJState` / `Chassis.ErrInjection` Redfish OEM endpoint

# Procedure

Work through the steps below in order. Use `curl -k` (insecure TLS) for all Redfish calls since BMC certificates are typically self-signed.

## Step 1 — Discover which OAM slot supports EINJ

Iterate through OAM_0 to OAM_7 and check which slot(s) expose an `EINJState` field. Not all OAM slots on every server support EINJ.

The `EINJState` field location varies by BMC firmware. Check both known paths:
- `Oem.EINJState` (seen on newer SMCI firmware)
- `Oem.Ami.AMD.EINJState` (seen on older SMCI firmware)

For each slot X in [0..7]:
```bash
curl -sk -u "<user>:<password>" \
  "https://<BMC_IP>/redfish/v1/Chassis/OAM_${X}" \
  | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'error' in data:
        print(f'OAM_{sys.argv[1]}: not found')
        sys.exit(0)
    oem = data.get('Oem', {})
    # Try direct path first (newer firmware), then nested path (older firmware)
    einj = oem.get('EINJState')
    if einj is None:
        einj = oem.get('Ami', {}).get('AMD', {}).get('EINJState')
    if einj is not None:
        boot_einj = oem.get('BootEINJState', 'N/A')
        print(f'OAM_{sys.argv[1]}: EINJState={einj}, BootEINJState={boot_einj}')
    else:
        print(f'OAM_{sys.argv[1]}: no EINJState field')
except Exception as e:
    print(f'OAM_{sys.argv[1]}: error - {e}')
" "${X}"
```

Collect all OAM slots that return an `EINJState` value. If none are found, report failure:
```
ERROR: No OAM slots with EINJState found on BMC <BMC_IP>.
This BMC may not support EINJ via Redfish (only SMCI BMCs are known to work).
```

## Step 2 — Check current EINJ state

For each OAM slot that has `EINJState`:

- If `EINJState` is `"Enabled"` or `"Enable"` — already enabled, report and skip to summary.
- If `EINJState` is `"Disabled"` or `"Disable"` — proceed to Step 3 to enable it.
- If `EINJState` contains `"pending"` (e.g. `"pending to enable"`) — a power cycle is pending. Report this and skip to Step 4.

Note: state string format varies by firmware — match case-insensitively.

## Step 3 — Enable EINJ

For each OAM slot where `EINJState` is disabled, POST the enable action:

```bash
curl -sk -u "<user>:<password>" \
  -X POST \
  "https://<BMC_IP>/redfish/v1/Chassis/OAM_${X}/Actions/Oem/AMD/Chassis.ErrInjection" \
  -H "Content-Type: application/json" \
  -d '{"ErrInjection": "Enable"}'
```

After the POST, re-query the OAM slot to verify the state changed:

```bash
curl -sk -u "<user>:<password>" \
  "https://<BMC_IP>/redfish/v1/Chassis/OAM_${X}" \
  | python3 -c "
import sys, json
data = json.load(sys.stdin)
oem = data.get('Oem', {})
einj = oem.get('EINJState') or oem.get('Ami', {}).get('AMD', {}).get('EINJState')
print(f'OAM_{sys.argv[1]}: EINJState={einj}')
" "${X}"
```

Expected: `EINJState` should now show `"pending to enable"`.

If the POST returns an error (HTTP 4xx/5xx), report the full response body — common causes:
- BMC firmware too old
- OAM slot in a failed state
- Insufficient BMC user privileges (need admin role)

## Step 4 — Power cycle via Redfish (FullPowerCycle required)

EINJ enablement only takes effect after a **full power cycle** (cold boot). A GracefulRestart (warm reboot) is NOT sufficient — the GPU/OAM firmware needs a complete power-off/power-on cycle.

First, discover the system reset endpoint:

```bash
curl -sk -u "<user>:<password>" \
  "https://<BMC_IP>/redfish/v1/Systems/1" \
  | python3 -c "
import sys, json
data = json.load(sys.stdin)
actions = data.get('Actions', {})
reset = actions.get('#ComputerSystem.Reset', {})
target = reset.get('target', 'NOT FOUND')
allowed = reset.get('ResetType@Redfish.AllowableValues', [])
power_state = data.get('PowerState', 'Unknown')
print(f'PowerState: {power_state}')
print(f'Reset target: {target}')
print(f'Allowed types: {allowed}')
"
```

The system URI may be `/redfish/v1/Systems/1` or `/redfish/v1/Systems/Self` — try both if the first returns 404.

**Ask the user for confirmation before power cycling**, then issue a `FullPowerCycle` using the discovered reset target URI from above:

```bash
curl -sk -u "<user>:<password>" \
  -X POST \
  "https://<BMC_IP><discovered_reset_target>" \
  -H "Content-Type: application/json" \
  -d '{"ResetType": "FullPowerCycle"}'
```

Use the `target` value from the discovery step (e.g. `/redfish/v1/Systems/1/Actions/ComputerSystem.Reset`). Do NOT hardcode the Systems URI.

If `FullPowerCycle` is not in the allowed values, fall back to `PowerCycle`, then `ForceRestart` as a last resort (but warn that it may not activate EINJ).

After issuing the reset, report:

```
=== Power Cycle Initiated ===

BMC:           <BMC_IP>
Reset Type:    FullPowerCycle
OAM Slot(s):   OAM_<X> [list all slots that were enabled]

The host is rebooting. This typically takes 5-10 minutes for GPU servers.
```

## Step 5 — Post-reboot verification

Poll the BMC every 60 seconds to check if the host is back. Wait at least 3 minutes before checking EINJ state (GPU firmware needs time to initialize after power-on):

```bash
curl -sk -u "<user>:<password>" \
  "https://<BMC_IP>/redfish/v1/Systems/1" \
  | python3 -c "
import sys, json
data = json.load(sys.stdin)
state = data.get('PowerState', 'Unknown')
print(f'PowerState: {state}')
"
```

Once `PowerState` is `"On"` (and at least 3 minutes have elapsed), re-query the OAM slot(s):

```bash
curl -sk -u "<user>:<password>" \
  "https://<BMC_IP>/redfish/v1/Chassis/OAM_${X}" \
  | python3 -c "
import sys, json
data = json.load(sys.stdin)
oem = data.get('Oem', {})
einj = oem.get('EINJState') or oem.get('Ami', {}).get('AMD', {}).get('EINJState')
print(f'OAM_{sys.argv[1]}: EINJState={einj}')
" "${X}"
```

Expected: `EINJState = "Enabled"`.

Print the final summary:

```
=== EINJ Enable Complete ===

BMC:           <BMC_IP>
OAM Slot(s):   OAM_<X>
EINJState:     Enabled

Host is powered on and EINJ is active.

=== Next Steps (on the GPU host) ===

SSH to the GPU host and run amdgpuras to inject RAS errors:
  sudo amdgpuras -l                    # list available GPUs
  sudo amdgpuras -d 0 -b 2 -s 0 -t 4  # inject UE on GPU 0, GFX block

Note: amdgpuras must be run directly on the GPU host, not remotely.
```

If `EINJState` is still `"pending to enable"` after reboot, the reset may not have completed a full cold boot. Try `FullPowerCycle` again, or use the BMC WebUI to power down then power up.

# Important Notes

- **Minimize user prompts.** Do NOT ask for user confirmation on read-only queries (Steps 1, 2, 5). Only ask for confirmation before state-changing actions: enabling EINJ (Step 3) and issuing the power cycle (Step 4). Run discovery and verification steps automatically without prompting.
- **SMCI-only.** If the Redfish endpoint returns 404 or lacks `EINJState`, the BMC vendor likely does not support this OEM extension.
- **FullPowerCycle required.** GracefulRestart (warm reboot) does NOT activate EINJ — the GPU OAM firmware only picks up the change on a cold boot.
- **Admin credentials required.** The BMC user must have administrator privileges to POST the enable action.
- **Idempotent.** Re-running the skill when EINJ is already enabled is safe — it will detect the `"Enabled"` state and report no action needed.
- **amdgpuras is host-only.** This skill only configures the BMC via Redfish. The actual RAS error injection (`amdgpuras`) must be run by the user directly on the GPU host via SSH.
