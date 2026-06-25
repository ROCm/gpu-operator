# RAS Error Injection Test Skill

- **Date:** 2026-06-24
- **Author:** Claude / Srivatsa
- **Related PR(s):** TBD
- **Related issue(s) / JIRA:** N/A

## Context

RAS error injection testing validates that the device-metrics-exporter (DME)
correctly surfaces hardware ECC errors detected by `amd-smi`. The test uses
`amdgpuras` to inject errors into specific GPU blocks (UMC, SDMA, GFX, MMHUB,
PCIe, XGMI) and verifies:

1. `amd-smi` (ground truth) shows the ECC counter increment
2. DME Prometheus metrics match `amd-smi`
3. AFID (AMD Fault ID) data is generated and correlates with metrics

This was validated manually on SMCI MI350X hardware:
- GFX UE injection: `gpu_ecc_uncorrect_gfx` incremented 0→1, `gpu_health` flipped 1→0
- UMC and SDMA injections succeeded but stacking them rapidly caused a GPU reset
- Key learning: inject one block at a time, wait for recovery between injections

## Approach

Create a Claude Code skill at `.claude/skills/ras-inject-test/SKILL.md` that:

1. Connects to GPU host via SSH, validates prerequisites
2. Gathers system info (driver version, GPU count, DME version)
3. Discovers injectable blocks via `amdgpuras -l`
4. For each GPU × block: captures baseline from amd-smi + DME, injects, waits,
   captures post-injection, cross-validates, records PASS/PARTIAL/FAIL/RESET/SKIP
5. Collects AFID data via `amd-smi ras --cper`
6. Generates a markdown test report with per-GPU per-block results

**amd-smi is the source of truth.** DME is the system under test. If amd-smi
shows an increment but DME doesn't, that's flagged as PARTIAL (potential DME bug).

### Separate concerns

- **Driver installation** → existing `ci-internal/ansible/install-amdgpu-driver.yml`
- **DME deployment** → future `/deploy-dme` skill (Docker, Debian, K8s modes)
- **EINJ enablement** → existing `/bmc-einj-enable` skill
- **Confluence upload** → integrated in skill (Step 5, publishes when Atlassian MCP is connected, local-only otherwise)

### Alternatives considered

- Reusing `amdgpuras_util.py` directly as a pytest — rejected because the test
  needs to run on bare-metal hosts that may not have the test framework installed.
  The skill uses SSH from the dev machine instead.
- Running all blocks in parallel — rejected because stacking injections causes
  GPU resets that clear counters before verification.

## Scope

- **In scope:** Error injection, amd-smi/DME cross-validation, AFID collection,
  report generation, all GPUs in system.
- **Out of scope:** Driver/DME deployment, EINJ enablement,
  CE (correctable error) injection testing (future enhancement).
- **Confluence publishing:** In scope — optional, publishes when Atlassian MCP
  is connected, falls back to local-only report otherwise.

## Validation

1. Invoke `/ras-inject-test <HOST_IP> <USER> <PASS>` against a
   SMCI350 host where EINJ is enabled and DME is running
2. Confirm system info is gathered correctly
3. Confirm at least GFX block injection produces PASS result
4. Confirm report file is generated with all GPUs and blocks
5. Confirm AFID data appears in the report

## Risks and rollback

- **GPU resets** — mitigated by sequential injection with recovery waits
- **Host unreachable after injection** — GPU resets can temporarily drop SSH;
  the skill retries SSH after recovery wait
- **Rollback** — delete `.claude/skills/ras-inject-test/` and revert README.
  No production code affected.
