# BMC EINJ Enable Skill

- **Date:** 2026-06-24
- **Author:** Claude / Srivatsa
- **Related PR(s):** TBD
- **Related issue(s) / JIRA:** N/A

## Context

Before `amdgpuras` can inject RAS (Reliability, Availability, Serviceability)
errors on AMD GPU hardware, the EINJ (Error Injection) capability must be
enabled on the BMC via the Redfish API. This is a manual multi-step process
involving OAM slot discovery, Redfish POST calls, and a host power cycle.

Currently only SMCI (Supermicro) BMCs are known to expose the required OEM
endpoint (`Chassis.ErrInjection` under `Oem.Ami.AMD` or directly under `Oem` depending on firmware version).

This plan adds a Claude Code skill to automate the entire flow from a single
invocation.

## Approach

Create a new skill at `.claude/skills/bmc-einj-enable/SKILL.md` that:

1. **Discovers EINJ-capable OAM slots** — iterates OAM_0..OAM_7 via
   `GET /redfish/v1/Chassis/OAM_X`, checks for `EINJState` at both
   `Oem.EINJState` (newer firmware) and `Oem.Ami.AMD.EINJState` (older).
2. **Enables EINJ** — POSTs `{"ErrInjection": "Enable"}` to each slot
   where state is `"Disable"`.
3. **Verifies state transition** — re-queries to confirm `"Pending to Enable"`.
4. **Power cycles the host via Redfish** — discovers the reset endpoint at
   `/redfish/v1/Systems/1` (or `/Systems/Self`), asks user for confirmation,
   then issues `FullPowerCycle` (required — `GracefulRestart` does NOT activate EINJ).
5. **Post-reboot verification** — polls `PowerState` until `"On"`, then
   re-queries OAM slots to confirm `EINJState` is `"Enabled"` or `"Enable"` (varies by firmware).
6. **Reports next steps** — instructs user to SSH to the GPU host and run
   `amdgpuras` (which can only execute locally on the host).

All Redfish calls use `curl -k` (self-signed BMC certs).

### Alternatives considered

- **Script-based approach** (standalone Python/shell script under
  `tests/pytests/scripts/`) — rejected because the workflow is interactive
  (needs user confirmation before power cycle) and benefits from Claude's
  ability to adapt to unexpected Redfish responses.
- **IPMI-only** (`ipmitool chassis power cycle`) — doesn't cover the EINJ
  enable step which requires the Redfish OEM endpoint.

## Scope

- **In scope:** OAM EINJ discovery, enable, power cycle via Redfish,
  post-reboot verification. Skill README entry.
- **Out of scope:** `amdgpuras` execution (host-only), non-SMCI BMC support,
  automated SSH to GPU host, NPD/ANR integration testing.

## Validation

- Manual: invoke `/bmc-einj-enable <IP> <user> <pass>` against a SMCI BMC
  with AMD GPUs. Confirm OAM discovery, EINJ enable, power cycle, and
  post-reboot EINJState = Enable.
- Negative: invoke against a non-SMCI BMC and confirm graceful error
  reporting (no EINJState found).

## Risks and rollback

- **Host downtime** — the skill performs a power cycle. Mitigated by
  requiring user confirmation before the reset.
- **BMC compatibility** — untested BMC vendors will fail at OAM discovery;
  the skill reports this clearly.
- **Rollback** — delete `.claude/skills/bmc-einj-enable/` and revert the
  README entry. No production code is affected.
