---
name: ras-inject-test
description: Run RAS error injection tests on AMD GPUs via amdgpuras, verify ECC metrics in both amd-smi (ground truth) and device-metrics-exporter, collect AFID data, and generate a test report with Confluence publishing. Requires EINJ enabled, amdgpuras installed, and DME running on the target host.
---

You are a skill that performs end-to-end RAS (Reliability, Availability, Serviceability) error injection testing on AMD GPUs. You inject errors via `amdgpuras`, verify ECC counters in both `amd-smi` (ground truth) and the device-metrics-exporter (DME), collect AFID data, and generate a structured test report published to Confluence.

# Invocation

```
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> [--release v1.5.1] [--metrics-port 5000] [--num-gpus auto] [--blocks gfx,mmhub]
```

Example:
```
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD>
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> --release v1.5.2
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> --release v1.5.1 --blocks gfx,mmhub,pcie_bif,xgmi_wafl
/ras-inject-test <HOST_IP> <USERNAME> <PASSWORD> --metrics-port 5000 --num-gpus 4
```

All SSH commands use `sshpass -p <password> ssh -o StrictHostKeyChecking=no <user>@<host>`. All `amdgpuras` and `amd-smi` commands require `sudo`.

**Security note:** `sshpass -p` exposes the password via `ps`. For production use, prefer `SSHPASS` env var with `sshpass -e`, or SSH key-based auth. The `-p` form is acceptable for lab/test environments.

Default metrics port is 5000. If `--num-gpus` is `auto` (default), detect GPU count from the host. Default `--blocks` is `gfx,mmhub` (the two reliably testable blocks). Never include `umc` or `sdma` unless explicitly requested — they cause fatal GPU resets.

Default `--release` is the current GPU operator release. This controls which Confluence parent page the results are published under.

# Prerequisites

Check ALL of these at the start. If any fail, report clearly and stop:

```bash
# 1. SSH connectivity
sshpass -p "<password>" ssh -o StrictHostKeyChecking=no <user>@<host> "hostname"

# 2. amdgpu driver loaded
sshpass ... "lsmod | grep -w amdgpu"

# 3. amdgpuras installed
sshpass ... "which amdgpuras"

# 4. amd-smi installed
sshpass ... "which amd-smi"

# 5. DME serving metrics
sshpass ... "curl -sf http://localhost:<port>/metrics | head -1"
```

If prerequisites fail, print which ones failed and suggest fixes:
- No driver: "Run ansible-playbook install-amdgpu-driver.yml or modprobe amdgpu"
- No amdgpuras: "Install from http://10.67.79.109/artifactory/linux-ci-generic-local/amdgpuras-tool/"
- No DME: "Start device-metrics-exporter container or deploy via GPU operator"
- EINJ not enabled: "Run /bmc-einj-enable first"

# Block-to-Metric Mapping

This maps amdgpuras block IDs to both amd-smi per-block output and DME Prometheus metrics:

| Block ID | Name      | amdgpuras flags         | amd-smi per-block field              | DME UE metric                  | DME CE metric                 | Risk |
|----------|-----------|-------------------------|--------------------------------------|--------------------------------|-------------------------------|------|
| 0        | umc       | `-b 0 -s 0 -t 4 -a 0x800000000` | `UMC: UNCORRECTABLE_COUNT`  | `gpu_ecc_uncorrect_umc`        | `gpu_ecc_correct_umc`         | Fatal reset |
| 1        | sdma      | `-b 1 -s 0 -t 4`       | `SDMA: UNCORRECTABLE_COUNT`          | `gpu_ecc_uncorrect_sdma`       | `gpu_ecc_correct_sdma`        | Fatal reset |
| 2        | gfx       | `-b 2 -s 0 -t 4`       | `GFX: UNCORRECTABLE_COUNT`           | `gpu_ecc_uncorrect_gfx`        | `gpu_ecc_correct_gfx`         | Safe |
| 3        | mmhub     | `-b 3 -s 0 -t 4`       | `MMHUB: UNCORRECTABLE_COUNT`         | `gpu_ecc_uncorrect_mmhub`      | `gpu_ecc_correct_mmhub`       | Safe |
| 5        | pcie_bif  | `-b 5 -s 1 -m 2 -t 4`  | `PCIE_BIF: UNCORRECTABLE_COUNT`      | `gpu_ecc_uncorrect_bif`        | `gpu_ecc_correct_bif`         | No HW counters |
| 7        | xgmi_wafl | `-b 7 -s 0 -t 4`       | `XGMI_WAFL: UNCORRECTABLE_COUNT`     | `gpu_ecc_uncorrect_xgmi_wafl`  | `gpu_ecc_correct_xgmi_wafl`   | Unreliable |

Also track totals: `gpu_ecc_uncorrect_total`, `gpu_ecc_correct_total`, `gpu_health`, `gpu_afid_errors`.

**Block-specific notes:**
- **UMC (b=0)**: Requires address flag `-a 0x800000000`. Causes fatal GPU reset — all GPUs enter "resuming" state requiring FullPowerCycle.
- **SDMA (b=1)**: Also causes fatal GPU reset. Exclude unless explicitly requested.
- **GFX (b=2)**: Reliably works. No GPU reset. Generates AFID 30 (FATAL).
- **MMHUB (b=3)**: Works when GPU is not recovering. Also generates AFID 30. May fail with "device resuming" if injected too soon after another block.
- **PCIe BIF (b=5)**: Requires `-s 1 -m 2` (sub-block 1, method ecrc_tx). amd-smi reports `N/A` for this block — hardware does not expose ECC counters via RAS sysfs. Injection is accepted but not observable. Mark as SKIP.
- **XGMI/WAFL (b=7)**: Injection accepted on specific XGMI link but counters don't increment. May need different injection method or firmware support. Mark as FAIL if counters don't change.

# Procedure

## Step 1 — Gather System Info

Collect ALL of the following from the host and display as a table:

```bash
# Hostname, kernel, OS
sshpass ... "hostname && uname -r && grep PRETTY_NAME /etc/os-release"

# Driver version
sshpass ... "cat /sys/module/amdgpu/version"

# amd-smi version (includes ROCm version)
sshpass ... "amd-smi version 2>&1 | head -1"

# amdgpuras version
sshpass ... "dpkg -l amdgpuras 2>/dev/null | grep amdgpuras || rpm -qa amdgpuras 2>/dev/null"

# GPU count
sshpass ... "ls /dev/dri/renderD* 2>/dev/null | wc -l"

# GPU model from metrics
sshpass ... "curl -s http://localhost:<port>/metrics | grep -m1 'card_model=' | sed 's/.*card_model=\"\([^\"]*\)\".*/\1/'"

# GPU inventory (BDF, UUID, serial)
sshpass ... "sudo amd-smi list 2>&1"

# DME container version
sshpass ... "sudo docker ps --filter name=device-metrics-exporter --format '{{.Image}}' 2>/dev/null || echo 'not docker'"

# ROCm firmware info
sshpass ... "sudo amd-smi version 2>&1"
```

Print summary:
```
=== System Info ===
Host:         <hostname> (<ip>)
Kernel:       <version>
OS:           <pretty_name>
Driver:       amdgpu <version>
ROCm:         <version>
amd-smi:      <version>
amdgpuras:    <version>
GPUs:         <count> × <model>
DME:          <image:tag>
```

## Step 2 — Discover Injectable Blocks

Run `amdgpuras -l` on the host. Build the command table based on the `--blocks` argument, filtered to only blocks that exist in the device listing.

Default safe blocks: `gfx,mmhub`

Reference: `tests/pytests/scripts/amdgpuras_util.py:get_amdgpuras_valid_command_list()` for parsing patterns.

## Step 3 — Test Loop (per GPU, per block, sequentially)

**CRITICAL: Inject ONE block at a time per GPU, verify, wait for recovery, then move to the next. Stacking injections causes GPU resets that clear counters before they can be read.**

For each GPU (0 to N-1), for each requested block:

### 3a. Capture Baseline (three sources)

**amd-smi per-block ECC (ground truth):**
```bash
sshpass ... "timeout 20 sudo amd-smi metric --ecc-block --gpu <gpu_id> 2>&1"
```
Parse the per-block output to extract `<BLOCK_NAME>: UNCORRECTABLE_COUNT` and `CORRECTABLE_COUNT`. This command can be slow (10-15s); the `timeout 20` prevents hangs.

**amd-smi aggregate ECC:**
```bash
sshpass ... "sudo amd-smi metric --ecc --gpu <gpu_id> 2>&1"
```
Extract `TOTAL_UNCORRECTABLE_COUNT`, `TOTAL_CORRECTABLE_COUNT`, `CACHE_UNCORRECTABLE_COUNT`.

**amd-smi AFID:**
```bash
sshpass ... "sudo amd-smi ras --cper --gpu=<gpu_id> --folder /tmp/.amdsmi_afid_temp 2>&1"
```
Count the number of CPER entries for diff later.

**DME per-block metrics:**
```bash
sshpass ... "curl -s http://localhost:<port>/metrics | grep '^gpu_ecc_uncorrect_<block>{' | grep 'gpu_id=\"<gpu_id>\"'"
sshpass ... "curl -s http://localhost:<port>/metrics | grep '^gpu_ecc_uncorrect_total{' | grep 'gpu_id=\"<gpu_id>\"'"
sshpass ... "curl -s http://localhost:<port>/metrics | grep '^gpu_health{' | grep 'gpu_id=\"<gpu_id>\"'"
```

### 3b. Check GPU Ready

```bash
sshpass ... "sudo amdgpuras -l 2>&1 | grep -c 'resuming'"
```
If any device shows "resuming", wait up to 120 seconds polling every 10 seconds. If still stuck, the host needs a reboot (see Important Notes).

### 3c. Inject Error

```bash
sshpass ... "sudo amdgpuras -d <gpu> -b <block_id> -s <sub_block> [-m <method>] -t <type> [-a 0x800000000]"
```

Record the injection result. Check output for:
- `"Error Inject Successfully"` → injection OK
- `"Error Inject Failed"` or `"resuming"` → record as failed
- `"Invalid injection parameters"` → missing `-m` method flag, retry with method

### 3d. Wait for Metrics Update

Wait 35 seconds for the exporter to poll and update metrics.

### 3e. Capture Post-Injection (same three sources as 3a)

### 3f. Cross-Validate and Record Result

Compare baseline vs post-injection across ALL sources:

1. **amd-smi per-block diff**: Did the specific block's `UNCORRECTABLE_COUNT` increment?
2. **amd-smi total diff**: Did `TOTAL_UNCORRECTABLE_COUNT` increment?
3. **DME per-block diff**: Did `gpu_ecc_uncorrect_<block>` increment?
4. **DME total diff**: Did `gpu_ecc_uncorrect_total` increment?
5. **Health**: Did `gpu_health` change from 1 to 0?
6. **AFID**: Did new AFID entries appear in `amd-smi ras --cper`?

Determine result:
- **PASS**: amd-smi per-block counter incremented AND DME per-block metric matches
- **PARTIAL**: amd-smi shows increment but DME does NOT match (potential DME bug)
- **FAIL**: amd-smi shows NO increment despite injection success
- **RESET**: GPU reset detected (counters cleared, device "resuming")
- **SKIP**: Block not testable (amd-smi reports N/A) or injection command failed

### 3g. Wait for GPU Recovery

Poll until GPU is no longer in resuming state (max 120s, 10s intervals). Then proceed to the next block.

## Step 4 — Generate Report

Save a markdown report to a timestamped file: `ras-inject-report-<hostname>-<YYYYMMDD-HHMMSS>.md`

The report must contain all of:
1. **System Info table** — hostname, kernel, OS, driver version, ROCm version, amd-smi version, amdgpuras version, GPU series/model, DME version, date
2. **GPU Inventory table** — GPU ID, BDF, UUID, serial number
3. **Results Summary table** — GPU, block, type, injection status, amd-smi per-block before/after, DME per-block before/after, health, AFID, result
4. **Cross-Validation Detail** — per block, showing amd-smi per-block, amd-smi total, DME per-block, DME total, health, with match indicators
5. **Totals** — PASS/PARTIAL/FAIL/RESET/SKIP counts
6. **AFID Summary** — per GPU, AFID count, severity breakdown
7. **Detailed AFID Log** — timestamp, GPU, severity, filename, AFID number
8. **Notes** — block-specific observations, known limitations

## Step 5 — Publish to Confluence

Results are published to Confluence under a release-based page hierarchy:

```
GPU Operator (page ID: 985276253, space ID: 566493184, space key: EN)
├── RAS Error Injection Test Results — v1.5.1  (parent, one per release)
│   ├── Run: exporter-0.0.1-342 / MI350X / SMCI / 2026-06-24
│   └── Run: exporter-0.0.1-350 / MI350X / SMCI / 2026-06-27
├── RAS Error Injection Test Results — v1.5.2
│   └── Run: ...
└── ...
```

**Procedure:**

1. **Find or create the release parent page.** Search for a child page of `985276253` titled `"RAS Error Injection Test Results — <release>"`. If it exists, use it. If not, create it with the standard overview template (overview table, per-block validation mapping, block coverage, result legend).

2. **Create the run child page** under the release parent with title: `"Run: <dme_version> / <gpu_series> / <platform> / <date>"`. Body contains the full report with system info, GPU inventory, results summary, cross-validation, AFID log, and notes.

3. **Update the release parent overview table** — add/update a row for this run showing DME version, platform, GPU series, driver, date, per-block results (PASS/FAIL/SKIP lozenges), and overall score.

If the Atlassian MCP tools are not connected, print the report path and instruct the user to authenticate:
```
=== Report Generated (local only) ===
File: <path-to-report.md>

Confluence upload skipped — Atlassian MCP not connected.
Run /mcp to authenticate, then re-run or manually upload.
```

# Important Notes

- **amd-smi is the source of truth** for ECC counters and AFID data. Both per-block (`amd-smi metric --ecc-block`) and aggregate (`amd-smi metric --ecc`) should be captured. DME metrics are the system under test — compare per-block (`gpu_ecc_uncorrect_<block>`) and total (`gpu_ecc_uncorrect_total`) against amd-smi.
- **One injection at a time.** Never stack multiple injections without waiting for verification and GPU recovery. Rapid injections cause GPU resets that clear counters.
- **UMC block requires `-a 0x800000000`** address flag. Other blocks do not.
- **PCIe BIF requires `-s 1 -m 2`** (sub-block 1, method ecrc_tx). Sub-block 0 does not exist. Without `-m`, amdgpuras returns "Invalid injection parameters".
- **PCIe BIF has no HW counters** — amd-smi reports N/A. Mark as SKIP, not FAIL.
- **XGMI/WAFL injection succeeds but counters may not increment** on current firmware. Mark as FAIL if no change observed.
- **GPU recovery can take 30-120 seconds.** Poll `amdgpuras -l` for "resuming" state before proceeding.
- **Exporter polling interval is ~30 seconds.** Wait at least 35 seconds after injection before checking DME metrics.
- **All commands run on the remote host via SSH**, not locally. The skill only generates the report locally.
- **Do not ask for user confirmation** on read-only queries or individual injections. Only ask if something unexpected happens (e.g., all GPUs are in resuming state, or SSH drops).
- **Reboot when GPUs are stuck.** If GPUs are in "resuming" state and do not recover within 120 seconds, the host needs a full power cycle. Use the BMC Redfish `FullPowerCycle` endpoint (same as `/bmc-einj-enable`). After reboot: wait for SSH, reload the amdgpu driver (`sudo modprobe amdgpu`), restart the DME container (`sudo docker start device-metrics-exporter`), then resume testing. A `GracefulRestart` is NOT sufficient — only `FullPowerCycle` clears the GPU firmware state.
- **BMC credentials are separate from host credentials.** The skill takes host SSH creds. If a reboot is needed, ask the user for the BMC IP and credentials, or check if `/bmc-einj-enable` was run earlier in the session and reuse those.
- **Cleanup after testing.** Stop and remove the DME container, and reboot the host to clear all GPU error state before releasing the node.
