#!/bin/bash
# Unit tests for Phase 3 (per-node NIC health check). Covers both the
# in-Job PHASE3_CHECK_SCRIPT body and the outer-driver
# PHASE3_SCRIPT.
#
# Scope (post-refactor: PHASE3_CHECK_SCRIPT emits a PHASE3_RESULT marker
# on stdout; PHASE3_SCRIPT parses `kubectl logs job/<name>` and owns all
# label/annotate writes -- the in-Job container no longer calls kubectl):
# * shellcheck on PHASE3_CHECK_SCRIPT (skipped if shellcheck absent).
# * PHASE3_CHECK_SCRIPT unit tests with mocked
# lspci / ip / rdma / ibv_devices / ibv_devinfo:
# - all 4 checks pass -> stdout has PHASE3_RESULT status=passed
# - NIC count mismatch -> status=failed reason includes nic-count
# - PF/VF mix is collapsed to PFs -> 8 PFs + many VFs => count==8 PASS
# - 1 NIC link DOWN -> status=failed reason link-state
# - 1 RDMA link INIT -> status=failed reason rdma-state
# - 1 device empty GID table -> status=failed reason gid-table
# - 1 device ibv_devinfo unresponsive -> status=failed reason ibv-devinfo
# - partial failure -> reason has only the failing class
# - annotation size truncation -> reason/failed_nics tokens <= MAX_BYTES
# - NODE_NAME unset is informational only (no exit-2 behavior)
# * PHASE3_SCRIPT outer-driver tests:
# - empty input list -> no-op, return 0
# - SKIP_NIC_VALIDATION=true -> pass-label every input node, no Jobs
# - SKIP_NIC_VALIDATION case-insensitive
# - missing required env -> all-fail with reason
# - missing job template -> all-fail with reason
# - kubectl apply failure -> reason=job-creation-failed
# - timeout -> reason=nic-not-allocated + cleanup
# - Job Complete + PHASE3_RESULT status=passed
# -> orchestrator writes label=passed (no annotation)
# - Job Failed + PHASE3_RESULT status=failed reason=. failed_nics=.
# -> orchestrator writes label=failed + failure-reason + failed-nics
# - Job Complete but NO PHASE3_RESULT line in logs
# -> orchestrator writes label=failed reason=no-result-line
# - parallel-submit ordering: every apply precedes the first poll
# - PHASE_NODES env-var fallback
#
# Implementation notes:
#
# The CHECK script is extracted with lib/extract_script.sh and sourced
# directly -- it uses no `local` / `declare -A`, so no function wrapping
# is needed. To mock lspci/ip/rdma/ibv_*, we prepend a per-test shim
# directory to PATH that contains tiny scripts which `cat` the right
# fixture file (selected via env vars exported by each test).
#
# The kubectl shim is the existing lib/kubectl_mock.sh; only the
# orchestrator-side (PHASE3_SCRIPT) calls it now. Per-job log content
# is seeded via `kubectl_mock_set_pod_log "job/<job-name>" "<body>"`,
# leveraging the mock's first-positional-token = pod-name dispatch
# (job/<name> is just a token).
#
# PHASE3_SCRIPT uses `local` / `declare -A`, so we wrap the extracted
# body in a function `__phase3_run` -- same shape as test_phase2.sh.
#
# Timeouts are exercised by setting PHASE3_JOB_WAIT_TIME=0 -- the wait
# loop checks `elapsed >= timeout` BEFORE the first kubectl get, so
# a 0-second budget short-circuits to TIMEOUT on the first iteration
# without sleeping. Same trick used by test_phase2.sh.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase3"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase3.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

PHASE3_DIR=$(mktemp -d -t phase3-tests-XXXXXX)
TPL_DIR="${PHASE3_DIR}/tpl"
SHIM_DIR="${PHASE3_DIR}/shims"
CHECK_BODY="${PHASE3_DIR}/phase3-check-body.sh"
PHASE3_BODY="${PHASE3_DIR}/phase3-body.sh"
HELPER_SCRIPT="${PHASE3_DIR}/phase-helpers.sh"
mkdir -p "$TPL_DIR" "$SHIM_DIR"

trap 'rm -rf "$PHASE3_DIR"; kubectl_mock_cleanup' EXIT

# --- shim binaries for in-Job tooling -------------------------------
#
# Each shim reads a per-test env var that names a fixture file under
# FIXTURES_DIR (or an absolute path). Missing/empty env var ->
# the shim emits nothing and exits 0 (matches behavior of the real
# tool on a misconfigured system: no devices found).
#
# Putting these on PATH lets PHASE3_CHECK_SCRIPT call them in any
# sub-shell or process substitution (e.g. `while . done < <(rdma .)`)
# without needing function overrides.

_make_shim() {
    local name="$1"
    local body="$2"
    cat >"${SHIM_DIR}/${name}" <<EOF
#!/bin/bash
${body}
EOF
    chmod +x "${SHIM_DIR}/${name}"
}

# lspci shim: emit the LSPCI_FIXTURE content. Ignores all args -- the
# real lspci is called as `lspci -Dnn`; we don't need to honor the
# command-line flags because the fixture itself supplies the full
# `-Dnn` output (including the trailing `[vendor:device]` tag) that
# Check 1's `grep -cE "\[(vid:did|.)\]"` filter operates on.
_make_shim "lspci" '
if [[ -n "${LSPCI_FIXTURE:-}" && -f "${LSPCI_FIXTURE}" ]]; then
    cat "${LSPCI_FIXTURE}"
fi
exit 0
'

# ip shim: only supports `ip -br link` (list mode) and `ip -br link show <iface>`
# (per-iface mode). Both forms are used by PHASE3_CHECK_SCRIPT.
_make_shim "ip" '
mode="list"
target_iface=""
saw_show=0
for a in "$@"; do
    case "$a" in
        show) saw_show=1 ;;
        -*|link|-br) : ;;
        *) target_iface="$a" ;;
    esac
done
if [[ "$saw_show" -eq 1 && -n "$target_iface" ]]; then
    mode="show-iface"
fi
fixture="${IP_LINK_FIXTURE:-}"
if [[ -z "$fixture" || ! -f "$fixture" ]]; then
    exit 0
fi
case "$mode" in
    list)
        cat "$fixture"
        ;;
    show-iface)
        # Emit only the line matching target_iface (or nothing if absent).
        grep -E "^[[:space:]]*${target_iface}[[:space:]]" "$fixture" || true
        ;;
esac
exit 0
'

# rdma shim: only the `rdma link show` form is used.
_make_shim "rdma" '
fixture="${RDMA_LINK_FIXTURE:-}"
if [[ -n "$fixture" && -f "$fixture" ]]; then
    cat "$fixture"
fi
exit 0
'

# ibv_devices shim: emit IBV_DEVICES_FIXTURE content.
_make_shim "ibv_devices" '
fixture="${IBV_DEVICES_FIXTURE:-}"
if [[ -n "$fixture" && -f "$fixture" ]]; then
    cat "$fixture"
fi
exit 0
'

# NOTE: the nicctl shim that previously lived here was removed when
# Check 5 was rewritten to read firmware from
# /sys/class/infiniband/<dev>/fw_ver. The pre-flight gate no longer
# probes nicctl either (only `ibv_devinfo` remains), so no nicctl
# binary is shimmed at all -- the production script must not call it.

# ibv_devinfo shim: rc override precedence -> IBV_DEVINFO_RC_<dev> wins
# over IBV_DEVINFO_RC_DEFAULT, which wins over fixture serving (so the
# "tool unresponsive" branch can be exercised even when a fallback
# fixture is configured). Otherwise serves a per-device fixture if
# IBV_DEVINFO_FIXTURE_<dev>[_V] is set, else falls back to
# IBV_DEVINFO_FIXTURE_DEFAULT[_V]. The `-v` flag selects the verbose
# fixture (so empty-GID cases can serve a different body for the
# verbose listing).
_make_shim "ibv_devinfo" '
dev=""
verbose=0
i=1
while [[ $i -le $# ]]; do
    a="${!i}"
    case "$a" in
        -d)
            j=$((i + 1))
            if [[ $j -le $# ]]; then
                dev="${!j}"
            fi
            ;;
        -v) verbose=1 ;;
    esac
    i=$((i + 1))
done
# Per-device rc override: takes precedence over fixture serving so the
# "unresponsive driver" path can be exercised against the same default
# fixture set the pass case uses.
if [[ -n "$dev" ]]; then
    rc_var="IBV_DEVINFO_RC_${dev}"
    if [[ -n "${!rc_var:-}" ]]; then
        exit "${!rc_var}"
    fi
fi
if [[ -n "${IBV_DEVINFO_RC_DEFAULT:-}" ]]; then
    # GPUOP-836: optional stderr message so the pre-flight
    # `ibv_devinfo 2>&1 >/dev/null` capture carries a representative
    # error string (e.g. "libibverbs: failed to load driver ionic_rdma").
    if [[ -n "${IBV_DEVINFO_STDERR_DEFAULT:-}" ]]; then
        echo "${IBV_DEVINFO_STDERR_DEFAULT}" >&2
    fi
    exit "${IBV_DEVINFO_RC_DEFAULT}"
fi
fixture=""
if [[ -n "$dev" ]]; then
    var="IBV_DEVINFO_FIXTURE_${dev}"
    if [[ "$verbose" -eq 1 ]]; then
        var="${var}_V"
    fi
    fixture="${!var:-}"
fi
if [[ -z "$fixture" ]]; then
    if [[ "$verbose" -eq 1 ]]; then
        fixture="${IBV_DEVINFO_FIXTURE_DEFAULT_V:-${IBV_DEVINFO_FIXTURE_DEFAULT:-}}"
    else
        fixture="${IBV_DEVINFO_FIXTURE_DEFAULT:-}"
    fi
fi
if [[ -n "$fixture" && -f "$fixture" ]]; then
    cat "$fixture"
fi
exit 0
'

# Job template stand-in for PHASE3_SCRIPT (mirror of test_phase2.sh).
# Real template lives in cluster-validation-job.yaml (cluster-validation-phase3-job-config
# ConfigMap,). PHASE3_SCRIPT only needs a sed-able file to
# exist -- the actual `kubectl apply` is mocked.
cat >"${TPL_DIR}/cluster-validation-phase3-job-config.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: cluster-validation-phase3-job
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: $$NODE
      containers:
        - name: nic-health
          # GPUOP-829: image is sed-substituted from
          # ROCE_WORKLOAD_IMAGE by PHASE3_SCRIPT.
          image: $$ROCE_WORKLOAD_IMAGE
          resources:
            limits:
              amd.com/nic: $$EXPECTED_NIC_COUNT
YAML

# --- extract scripts under test -------------------------------------

RAW_CHECK=$(extract_configmap_data "$CONFIGMAP" "PHASE3_CHECK_SCRIPT")
if [[ -z "$RAW_CHECK" ]]; then
    echo "FATAL: PHASE3_CHECK_SCRIPT extraction produced empty output" >&2
    exit 1
fi
printf '%s\n' "$RAW_CHECK" > "$CHECK_BODY"
if ! bash -n "$CHECK_BODY"; then
    echo "FATAL: extracted PHASE3_CHECK_SCRIPT has bash syntax errors" >&2
    exit 1
fi

RAW_PHASE3=$(extract_configmap_data "$CONFIGMAP" "PHASE3_SCRIPT")
if [[ -z "$RAW_PHASE3" ]]; then
    echo "FATAL: PHASE3_SCRIPT extraction produced empty output" >&2
    exit 1
fi
# Patch the one hardcoded path so the test can run as a non-root user
# without /phase3-configs existing. Also pin the timestamp PHASE3_SCRIPT
# embeds in job names so seeded mock state always matches what the
# script generates.
PATCHED_PHASE3=$(printf '%s\n' "$RAW_PHASE3" \
    | sed "s|/phase3-configs/cluster-validation-phase3-job-config.yaml|${TPL_DIR}/cluster-validation-phase3-job-config.yaml|g" \
    | sed 's|ts=\$(date +%Y%m%d-%H%M%S)|ts="${PHASE3_TEST_TS:-$(date +%Y%m%d-%H%M%S)}"|')

# Wrap in a function so `local` / `declare -A` (used heavily inside
# PHASE3_SCRIPT) are valid.
{
    printf '__phase3_run() {\n'
    printf '%s\n' "$PATCHED_PHASE3"
    printf '}\n'
} > "$PHASE3_BODY"

if ! bash -n "$PHASE3_BODY"; then
    echo "FATAL: patched PHASE3_SCRIPT has bash syntax errors" >&2
    exit 1
fi

# Extract the helper library (label_phase_passed/failed,
# annotate_phase_value) once; sourced before the outer-driver tests.
extract_configmap_data "$CONFIGMAP" "PHASE_NODE_LABEL_SCRIPT" \
    > "$HELPER_SCRIPT"
if [[ ! -s "$HELPER_SCRIPT" ]]; then
    echo "FATAL: PHASE_NODE_LABEL_SCRIPT extraction produced empty output" >&2
    exit 1
fi
if ! bash -n "$HELPER_SCRIPT"; then
    echo "FATAL: extracted helper script has bash syntax errors" >&2
    exit 1
fi

kubectl_mock_init

# Suffix for failure-reason annotation; mirrors ConfigMap default.
export PHASE_FAILURE_REASON_ANNOTATION_SUFFIX="-failure-reason"

# GPUOP-835: Check 5 (driver/firmware compat) defaults to enabled in the
# ConfigMap (fail-closed production stance). The legacy test bodies below
# do not yet supply Check 5 fixtures -- per-test ConfigMap-default-enabled
# Check 5 cases land with GPUOP-836. Disable Check 5 at the harness level
# so the pre-GPUOP-835 cases continue to test checks 1-4 in isolation; the
# pre-flight `nicctl --help` / `ibv_devinfo` shims above still cover the
# pre-flight gate. Individual tests that exercise Check 5 are expected to
# locally `export PHASE3_DRIVER_FW_CHECK_ENABLED=true` and provide
# PHASE3_DRIVER_SYSFS_PATH + NICCTL_FW_FIXTURE.
export PHASE3_DRIVER_FW_CHECK_ENABLED="false"

# Prepend the shim dir AFTER kubectl_mock_init so the mock kubectl
# (which init prepends) still wins for `kubectl`, and our shims win
# for lspci/ip/rdma/ibv_*.
export PATH="${SHIM_DIR}:${PATH}"

# shellcheck disable=SC1090
source "$HELPER_SCRIPT"
# shellcheck disable=SC1090
source "$PHASE3_BODY"

# Sanity: required functions are defined.
for fn in label_phase_passed label_phase_failed __phase3_run; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: required function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# Suppress the -u trap for tests that intentionally leave optional env
# vars unset (SKIP_NIC_VALIDATION, PHASE_NODES, fixture vars).
set +u

# -------------------------------------------------------------------
# PART A: shellcheck on PHASE3_CHECK_SCRIPT
# -------------------------------------------------------------------
#
# Static analysis. Skip cleanly if shellcheck is not on PATH so the
# suite still runs in minimal CI containers. SC1091 (cannot follow
# sourced file) is irrelevant -- there are no `source` calls in the
# CHECK script.

it "shellcheck PHASE3_CHECK_SCRIPT (skip if shellcheck not on PATH)" && {
    if ! command -v shellcheck >/dev/null 2>&1; then
        echo "      SKIP: shellcheck not on PATH"
    else
        run shellcheck --severity=warning "$CHECK_BODY"
        assert_status 0
    fi
}

# -------------------------------------------------------------------
# PART B: PHASE3_CHECK_SCRIPT in-Job behavior
# -------------------------------------------------------------------
#
# Each test:
# * resets kubectl mock state
# * exports PHASE3_* config + per-shim fixture pointers
# * runs PHASE3_CHECK_SCRIPT in a sub-shell (so the in-script `exit`
# does not kill the harness)
# * asserts on exit code + kubectl call log

_reset_check_env() {
    kubectl_mock_reset
    unset LSPCI_FIXTURE IP_LINK_FIXTURE RDMA_LINK_FIXTURE \
          IBV_DEVICES_FIXTURE IBV_DEVINFO_FIXTURE_DEFAULT \
          IBV_DEVINFO_FIXTURE_DEFAULT_V \
          IBV_DEVINFO_RC_DEFAULT IBV_DEVINFO_STDERR_DEFAULT \
          ROCE_WORKLOAD_IMAGE PHASE3_DRIVER_FW_STRICT \
          PHASE3_IB_SYSFS_DIR PHASE3_NET_SYSFS_DIR
    # Clear any per-device fixture / rc overrides from earlier tests.
    while IFS= read -r v; do
        unset "$v"
    done < <(compgen -v | grep -E '^IBV_DEVINFO_(FIXTURE|RC)_rocep' || true)
    export NODE_NAME="node-under-test"
    export PHASE3_LABEL_KEY="amd.com/nic-health"
    export PHASE3_AMD_NIC_PCI_IDS="1dd8:1002"
    export PHASE3_EXPECTED_NIC_COUNT="8"
    export PHASE3_MIN_GID_COUNT="1"
    export PHASE3_ANNOTATION_MAX_BYTES="250"
    # Default sysfs roots point at empty tmp dirs so the check doesn't
    # accidentally read the host /sys (test runs are typically on hosts
    # without ionic interfaces, but we want hermetic behavior).
    export PHASE3_IB_SYSFS_DIR="${PHASE3_DIR}/mock_sysfs_ib_empty"
    export PHASE3_NET_SYSFS_DIR="${PHASE3_DIR}/mock_sysfs_net_empty"
    mkdir -p "$PHASE3_IB_SYSFS_DIR" "$PHASE3_NET_SYSFS_DIR"
}

# Per-suite shared "drivers" directory: the production sysfs places
# `/sys/class/<X>/<dev>/device/driver` as a symlink whose TARGET's
# basename is the kernel driver name (e.g. `ionic`). The CHECK script
# resolves that basename via `basename $(readlink ...)`. We point our
# fake symlinks at paths inside this directory so the basenames match
# the driver name exactly (e.g. `<DRIVERS_DIR>/ionic`). The target
# files do not need to exist -- bash's `readlink` (no -f) returns the
# raw target path so `basename` works on a dangling link.
PHASE3_FAKE_DRIVERS_DIR="${PHASE3_DIR}/fake-drivers"
mkdir -p "$PHASE3_FAKE_DRIVERS_DIR"

# _seed_sysfs_net <root> <iface> <driver-name> <operstate>
# Populate a fake /sys/class/net/<iface>/ tree under <root>:
#   <root>/<iface>/operstate          (file with operstate text)
#   <root>/<iface>/device/driver -> <DRIVERS_DIR>/<driver-name>
_seed_sysfs_net() {
    local root="$1" iface="$2" drv="$3" state="$4"
    local d="${root}/${iface}"
    mkdir -p "${d}/device"
    printf '%s\n' "$state" > "${d}/operstate"
    ln -sfn "${PHASE3_FAKE_DRIVERS_DIR}/${drv}" "${d}/device/driver"
}

# _seed_sysfs_ib <root> <dev> <driver-name> <fw_ver>
# Populate a fake /sys/class/infiniband/<dev>/ tree:
#   <root>/<dev>/fw_ver               (file with firmware string)
#   <root>/<dev>/device/driver -> <DRIVERS_DIR>/<driver-name>
# Pass an empty <fw_ver> to skip the fw_ver file entirely (simulates
# unreadable / missing fw_ver, which the script tolerates by
# skipping that device).
_seed_sysfs_ib() {
    local root="$1" dev="$2" drv="$3" fw="$4"
    local d="${root}/${dev}"
    mkdir -p "${d}/device"
    if [[ -n "$fw" ]]; then
        printf '%s\n' "$fw" > "${d}/fw_ver"
    fi
    ln -sfn "${PHASE3_FAKE_DRIVERS_DIR}/${drv}" "${d}/device/driver"
}

# _new_sysfs_net_root / _new_sysfs_ib_root
# Allocate a fresh per-test mock sysfs root + export PHASE3_*_SYSFS_DIR
# to point at it. Stashes the path in a global variable so the test
# body can also reference it via $NEW_SYSFS_NET_ROOT / $NEW_SYSFS_IB_ROOT.
# Do NOT call via command substitution: command-substitution subshells
# would discard the export. The functions print the path on stdout so
# tests written as `var=$(_new_sysfs_X_root)` still see the path string,
# but the export only persists when the function is called in the
# current shell (e.g. just `_new_sysfs_X_root` and then read
# $NEW_SYSFS_X_ROOT or $PHASE3_X_SYSFS_DIR).
NEW_SYSFS_NET_ROOT=""
NEW_SYSFS_IB_ROOT=""
_new_sysfs_net_root() {
    local p
    p=$(mktemp -d -p "$PHASE3_DIR" mock_sysfs_net-XXXXXX)
    export PHASE3_NET_SYSFS_DIR="$p"
    NEW_SYSFS_NET_ROOT="$p"
    printf '%s' "$p"
}
_new_sysfs_ib_root() {
    local p
    p=$(mktemp -d -p "$PHASE3_DIR" mock_sysfs_ib-XXXXXX)
    export PHASE3_IB_SYSFS_DIR="$p"
    NEW_SYSFS_IB_ROOT="$p"
    printf '%s' "$p"
}

# Run the CHECK script in a sub-shell. `bash` (not `source`) so the
# script's own `exit` only ends the sub-shell.
_run_check() {
    bash "$CHECK_BODY"
}

# -------------------------------------------------------------------
# B1. All 4 checks pass -> =passed, exit 0, no failure annotation.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT all checks pass -> stdout PHASE3_RESULT status=passed, exit 0" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    if grep -F "PHASE3_RESULT status=failed" <<<"$LAST_STDOUT" >/dev/null; then
        _assert_fail "pass-path must not emit a failed marker:
${LAST_STDOUT}"
    fi
    # New contract: PHASE3_CHECK_SCRIPT never invokes kubectl.
    assert_kubectl_no_calls
    assert_stdout_contains "PHASE3_CHECK_SCRIPT done: PASS"
}

# -------------------------------------------------------------------
# B2. NIC count mismatch (lspci returns 7, expected 8).
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT nic-count mismatch -> PHASE3_RESULT status=failed reason nic-count" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-count-mismatch.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "reason=nic-count:expected=8,actual=7"
    assert_stdout_contains "CHECK 1 FAIL"
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# B2b. PF-only count filter: a host that exposes 8 PFs (.0) plus many
# VFs/SR-IOV/sub-functions (.1+) must collapse to nic_count=8 so the
# check passes. Catches the regression where raw `lspci -d <vendor>:`
# wc -l would have returned 48 against an 8-NIC node with 5 VFs each.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT 8 PFs + 40 VFs -> nic_count=8 PASS" && {
    _reset_check_env
    # Build a fixture with 8 PFs (function .0) interleaved with VFs (.1.5).
    # PFs carry device ID 1002 (the allowlisted PF); VFs carry 1003 so the
    # `[1dd8:1002]` filter excludes them by device ID.
    pf_vf_fixture="${PHASE3_DIR}/lspci-pfs-and-vfs.txt"
    : >"$pf_vf_fixture"
    for bus in 05 06 07 08 09 0a 0b 0c; do
        printf '0000:%s:00.0 Ethernet controller [0200]: Pensando Systems DSC Ethernet Controller [1dd8:1002] (rev 03)\n' \
            "$bus" >>"$pf_vf_fixture"
        for fn in 1 2 3 4 5; do
            printf '0000:%s:00.%d Ethernet controller [0200]: Pensando Systems DSC Virtual Function [1dd8:1003] (rev 03)\n' \
                "$bus" "$fn" >>"$pf_vf_fixture"
        done
    done
    export LSPCI_FIXTURE="$pf_vf_fixture"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 0
    assert_stdout_contains "CHECK 1 PASS: nic_count=8"
    assert_stdout_contains "PHASE3_RESULT status=passed"
}

# -------------------------------------------------------------------
# B2c. Real Pensando layout: each card exposes 6 PCI functions of the
# same vendor -- 2 PCI bridges (Salina Upstream + Virtual Downstream),
# 3 Processing accelerators (TAWK IPC, Register/Memory, DSC PDS Core),
# and 1 Ethernet controller. Raw lspci returns 48 lines on an 8-card
# node; only the 8 Ethernet controllers should be counted. Catches the
# regression that produced `actual=24` on the live smc300x-ccs node
# when the previous /\.0$/ filter admitted bridges and accelerators.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT real Pensando 6-functions-per-card layout -> nic_count=8 PASS" && {
    _reset_check_env
    # Each Pensando card exposes 6 PCI functions sharing the 1dd8 vendor
    # but distinct device IDs: 0008 + 1001 (PCI bridges), 1012 + 100f +
    # 100c (Processing accelerators), and 1002 (Ethernet controller PF).
    # The `[1dd8:1002]` allowlist must keep only the 8 Ethernet PFs.
    real_pensando_fixture="${PHASE3_DIR}/lspci-real-pensando.txt"
    : >"$real_pensando_fixture"
    for bus in 08 25 45 68 88 a5 c5 e8; do
        bridge_a=$(printf '%02x' $((16#${bus} - 2)))
        bridge_b=$(printf '%02x' $((16#${bus} - 1)))
        printf '0000:%s:00.0 PCI bridge [0604]: AMD Pensando Systems DSC3 Salina Upstream Port [1dd8:0008]\n'     "$bridge_a" >>"$real_pensando_fixture"
        printf '0000:%s:00.0 PCI bridge [0604]: AMD Pensando Systems DSC Virtual Downstream Port [1dd8:1001]\n'   "$bridge_b" >>"$real_pensando_fixture"
        printf '0000:%s:00.0 Processing accelerators [1200]: AMD Pensando Systems TAWK IPC Device [1dd8:1012]\n'  "$bus"      >>"$real_pensando_fixture"
        printf '0000:%s:00.1 Processing accelerators [1200]: AMD Pensando Systems Register/Memory Resource Device [1dd8:100f]\n' "$bus" >>"$real_pensando_fixture"
        printf '0000:%s:00.2 Processing accelerators [1200]: AMD Pensando Systems DSC PDS Core Management [1dd8:100c]\n' "$bus" >>"$real_pensando_fixture"
        printf '0000:%s:00.3 Ethernet controller [0200]: AMD Pensando Systems DSC Ethernet Controller [1dd8:1002]\n' "$bus"  >>"$real_pensando_fixture"
    done
    export LSPCI_FIXTURE="$real_pensando_fixture"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 0
    assert_stdout_contains "CHECK 1 PASS: nic_count=8"
    assert_stdout_contains "PHASE3_RESULT status=passed"
}

# -------------------------------------------------------------------
# B2d. Multi device-ID allowlist: PHASE3_AMD_NIC_PCI_IDS accepts a
# comma-separated list (e.g. "1dd8:1002,1dd8:1003") so a fleet with
# a mixed PF generation -- 4 cards on 1002 + 4 cards on 1003 -- counts
# to 8 PFs. Catches the regression where the list was not regex-joined
# and only the first vendor:device pair was honoured.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT multi-device PCI ID list -> nic_count counts both IDs" && {
    _reset_check_env
    export PHASE3_AMD_NIC_PCI_IDS="1dd8:1002,1dd8:1003"
    mixed_fixture="${PHASE3_DIR}/lspci-mixed-device-ids.txt"
    : >"$mixed_fixture"
    for bus in 05 06 07 08; do
        printf '0000:%s:00.0 Ethernet controller [0200]: Pensando Systems DSC Ethernet Controller [1dd8:1002] (rev 03)\n' \
            "$bus" >>"$mixed_fixture"
    done
    for bus in 09 0a 0b 0c; do
        printf '0000:%s:00.0 Ethernet controller [0200]: Pensando Systems DSC Ethernet Controller [1dd8:1003] (rev 04)\n' \
            "$bus" >>"$mixed_fixture"
    done
    # Add a 1dd8:1004 line that is *not* in the allowlist -- must NOT
    # be counted.
    printf '0000:0d:00.0 Ethernet controller [0200]: Pensando Systems DSC Future Function [1dd8:1004]\n' \
        >>"$mixed_fixture"
    export LSPCI_FIXTURE="$mixed_fixture"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 0
    assert_stdout_contains "CHECK 1 PASS: nic_count=8"
    assert_stdout_contains "PHASE3_RESULT status=passed"
}

# -------------------------------------------------------------------
# B3. One ip link DOWN -> =failed, failed-nics carries that NIC,
# reason includes link-state:<iface>=DOWN.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT one NIC link DOWN -> PHASE3_RESULT status=failed reason link-state, failed_nics set" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    # Lay down 8 ionic netdevs in mock sysfs; one (enP7p1s0f0) is down.
    _new_sysfs_net_root >/dev/null
    net_root="$PHASE3_NET_SYSFS_DIR"
    _seed_sysfs_net "$net_root" "enP5p1s0f0"  "ionic" "up"
    _seed_sysfs_net "$net_root" "enP6p1s0f0"  "ionic" "up"
    _seed_sysfs_net "$net_root" "enP7p1s0f0"  "ionic" "down"
    _seed_sysfs_net "$net_root" "enP8p1s0f0"  "ionic" "up"
    _seed_sysfs_net "$net_root" "enP9p1s0f0"  "ionic" "up"
    _seed_sysfs_net "$net_root" "enP10p1s0f0" "ionic" "up"
    _seed_sysfs_net "$net_root" "enP11p1s0f0" "ionic" "up"
    _seed_sysfs_net "$net_root" "enP12p1s0f0" "ionic" "up"
    # A non-ionic interface that MUST be ignored even though its
    # operstate would fail.
    _seed_sysfs_net "$net_root" "eth0" "e1000" "down"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "link-state:enP7p1s0f0=down"
    assert_stdout_contains "failed_nics=enP7p1s0f0"
    assert_stdout_contains "CHECK 2 FAIL"
    # eth0 is non-ionic -> must not appear in failed_nics.
    if grep -F "eth0" <<<"$LAST_STDOUT" >/dev/null; then
        _assert_fail "non-ionic eth0 must not be enumerated by Check 2:
${LAST_STDOUT}"
    fi
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# B4. One rdma link in INIT state -> =failed, reason rdma-state.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT one rdma link INIT -> PHASE3_RESULT status=failed reason rdma-state" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-one-init.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "rdma-state:rocep7s0/1=INIT"
    assert_stdout_contains "CHECK 3 FAIL"
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# B5. Empty GID table on one device (verbose listing has 0 `GID[` lines).
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT empty GID table on one device -> PHASE3_RESULT status=failed reason gid-table" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    # Default is pass; override rocep7s0's verbose listing to the
    # empty-GID body. Non-verbose form is still served (responds) so
    # only Check 4's GID-count branch trips, not the unresponsive
    # branch.
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_rocep7s0_V="${FIXTURES_DIR}/ibv-devinfo-empty-gid.txt"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "gid-table:rocep7s0=0"
    assert_stdout_contains "failed_nics=rocep7s0"
    assert_stdout_contains "CHECK 4 FAIL"
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# B6. Tools missing: ibv_devinfo returns non-zero for one device
# (simulates the image regression case where the device file is
# present but the driver tool errors out).
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT ibv_devinfo unresponsive on one device -> PHASE3_RESULT status=failed reason ibv-devinfo" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    # Non-zero exit for rocep6s0 only -- the non-verbose form is what
    # the script tests for responsiveness, so the rc override must
    # apply to that call (no -v fixture is checked first per shim).
    # We force rc=1 for rocep6s0 across all invocations by clearing
    # its fixture overrides and setting the rc.
    unset IBV_DEVINFO_FIXTURE_rocep6s0 IBV_DEVINFO_FIXTURE_rocep6s0_V
    export IBV_DEVINFO_RC_rocep6s0="1"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "ibv-devinfo:rocep6s0=unresponsive"
    assert_stdout_contains "failed_nics=rocep6s0"
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# B7. Partial failure: counts and links OK, but rdma INIT trips Check 3
# while Checks 1/2/4 stay green. The PHASE3_RESULT marker must
# carry only Check 3's reason -- no spurious entries from passing
# checks. (Aggregate is still failed: any single check failing is
# a node-level fail.)
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT partial failure (rdma only) -> PHASE3_RESULT reason has only rdma-state, no others" && {
    _reset_check_env
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-one-init.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    run _run_check
    assert_status 1
    assert_kubectl_no_calls
    # The PHASE3_RESULT line must include rdma-state but no other class.
    result_line=$(grep -E '^PHASE3_RESULT status=failed' <<<"$LAST_STDOUT" \
        | tail -n 1 || true)
    if [[ -z "$result_line" ]]; then
        _assert_fail "expected a PHASE3_RESULT status=failed line; stdout:
${LAST_STDOUT}"
    fi
    for forbidden in "nic-count:" "link-state:" "ibv-devinfo:" "gid-table:"; do
        if grep -qF -- "$forbidden" <<<"$result_line"; then
            _assert_fail "partial-failure PHASE3_RESULT must not include '${forbidden}', got: ${result_line}"
        fi
    done
    if ! grep -qF "rdma-state:" <<<"$result_line"; then
        _assert_fail "partial-failure must include 'rdma-state:', got: ${result_line}"
    fi
}

# -------------------------------------------------------------------
# B8. Marker truncation: many failures -> reason= and failed_nics=
# values on the PHASE3_RESULT line must NOT exceed
# PHASE3_ANNOTATION_MAX_BYTES (the orchestrator forwards these
# verbatim to the annotation, which has a 256-byte k8s ceiling).
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT large failure list truncates PHASE3_RESULT tokens to MAX_BYTES" && {
    _reset_check_env
    # Force a worst case: every check fails on many synthetic devices.
    # Build a giant rdma fixture so the joined reason string would
    # easily exceed 250 bytes.
    big_rdma="${PHASE3_DIR}/rdma-link-many-init.txt"
    : >"$big_rdma"
    for i in $(seq 1 60); do
        printf 'link rocep%ds0/1 state INIT physical_state LINK_UP netdev enP%dp1s0f0\n' "$i" "$i" \
            >>"$big_rdma"
    done
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export IP_LINK_FIXTURE="${FIXTURES_DIR}/ip-link-pass.txt"
    export RDMA_LINK_FIXTURE="$big_rdma"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export PHASE3_ANNOTATION_MAX_BYTES="250"
    run _run_check
    assert_status 1
    assert_kubectl_no_calls
    # Extract the PHASE3_RESULT line and inspect the value lengths.
    result_line=$(grep -E '^PHASE3_RESULT status=failed' <<<"$LAST_STDOUT" \
        | tail -n 1 || true)
    if [[ -z "$result_line" ]]; then
        _assert_fail "expected a PHASE3_RESULT status=failed line; stdout:
${LAST_STDOUT}"
    fi
    reason_v=$(sed -nE 's/.*reason=([^ ]+).*/\1/p' <<<"$result_line")
    nics_v=$(sed -nE   's/.*failed_nics=([^ ]+).*/\1/p' <<<"$result_line")
    if [[ -z "$reason_v" ]]; then
        _assert_fail "PHASE3_RESULT line missing reason=...: ${result_line}"
    fi
    if [[ "${#reason_v}" -gt 250 ]]; then
        _assert_fail "PHASE3_RESULT reason length ${#reason_v} > 250"
    fi
    if [[ -n "$nics_v" && "${#nics_v}" -gt 250 ]]; then
        _assert_fail "PHASE3_RESULT failed_nics length ${#nics_v} > 250"
    fi
}

# -------------------------------------------------------------------
# PART C: PHASE3_SCRIPT outer-driver behavior
# -------------------------------------------------------------------

_reset_phase3_env() {
    kubectl_mock_reset
    export PHASE3_LABEL_KEY="amd.com/nic-health"
    export PHASE3_EXPECTED_NIC_COUNT="8"
    # GPUOP-829: PHASE3_SCRIPT sed-substitutes $$ROCE_WORKLOAD_IMAGE.
    # Default to a recognizable test tag so tests that exercise the
    # submit path render a valid (placeholder-free) Job template.
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/roce-workload:test-tag"
    # 60s is large enough that the poll loop's first iteration (which
    # checks Complete=True / Failed=True immediately, seeded by tests)
    # breaks out before any sleep.
    export PHASE3_JOB_WAIT_TIME="60"
    # Pin the timestamp PHASE3_SCRIPT puts into job names so seeded
    # state always matches what the script looks up.
    export PHASE3_TEST_TS="testts0001"
    unset SKIP_NIC_VALIDATION PHASE_NODES
}

# Helper: compute the job name PHASE3_SCRIPT will generate for <node>
# with the pinned PHASE3_TEST_TS. Mirrors PHASE3_SCRIPT exactly:
# cvf-phase3-${node}-${ts} (when short enough)
# cvf-phase3-${sha1(node)|6}-${ts} (when over 63 chars)
_phase3_expected_job_name() {
    local node="$1" ts="$2" max_len=63 prefix="cvf-phase3"
    local jn="${prefix}-${node}-${ts}"
    if [[ "${#jn}" -gt "$max_len" ]]; then
        local h
        h=$(echo -n "$node" | sha1sum | cut -c1-6)
        jn="${prefix}-${h}-${ts}"
    fi
    printf '%s' "$jn"
}

# Seed mock state for one job: Complete=True AND a PHASE3_RESULT
# status=passed line in the job's pod log. PHASE3_SCRIPT calls
# `kubectl logs job/<name> --tail=20` and greps for PHASE3_RESULT to
# decide pass/fail labeling, so both bits must be seeded for the
# orchestrator to take the pass branch.
_seed_job_complete() {
    local node="$1" ts="$2"
    local job
    job=$(_phase3_expected_job_name "$node" "$ts")
    kubectl_mock_set_job_condition "$job" "Complete" "True"
    kubectl_mock_set_pod_log "job/${job}" "PHASE3_RESULT status=passed"
}

# Seed mock state for one job: Failed=True AND a PHASE3_RESULT
# status=failed line in the job's pod log (with reason + failed_nics
# tokens the orchestrator forwards to the failure-reason / failed-nics
# annotations). Optional 3rd/4th args customize the marker content.
_seed_job_failed() {
    local node="$1" ts="$2"
    local reason="${3:-link-state:enP7p1s0f0=DOWN}"
    local failed_nics="${4:-enP7p1s0f0}"
    local job
    job=$(_phase3_expected_job_name "$node" "$ts")
    kubectl_mock_set_job_condition "$job" "Failed" "True"
    kubectl_mock_set_pod_log "job/${job}" \
        "PHASE3_RESULT status=failed reason=${reason} failed_nics=${failed_nics}"
}

ts=$(printf '%s' "testts0001")

# -------------------------------------------------------------------
# C1. Empty input list -> no-op, exit 0, no kubectl side effects.
# -------------------------------------------------------------------

it "PHASE3_SCRIPT with empty input list is a no-op and returns 0" && {
    _reset_phase3_env
    run __phase3_run
    assert_status 0
    assert_kubectl_no_calls
    assert_stdout_contains "no input nodes -- nothing to do"
}

# -------------------------------------------------------------------
# C2. SKIP_NIC_VALIDATION=true -> every input node pass-labeled,
# NO Phase 3 Job submission, no kubectl get/apply work.
# -------------------------------------------------------------------

it "SKIP_NIC_VALIDATION=true pass-labels every input node, no Jobs created" && {
    _reset_phase3_env
    export SKIP_NIC_VALIDATION="true"
    run __phase3_run node-a node-b node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/nic-health=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/nic-health=passed --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/nic-health=passed --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP_NIC_VALIDATION=true must not submit any Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP must not poll Jobs:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "SKIP_NIC_VALIDATION=true -- pass-labeling"
}

it "SKIP_NIC_VALIDATION accepts case-insensitive value (TRUE)" && {
    _reset_phase3_env
    export SKIP_NIC_VALIDATION="TRUE"
    run __phase3_run node-x
    assert_status 0
    assert_kubectl_call \
        "label node node-x amd.com/nic-health=passed --overwrite"
}

# -------------------------------------------------------------------
# C3. Missing required env var (PHASE3_EXPECTED_NIC_COUNT) -> every
# input node labeled =failed with reason=phase3-missing-env:.;
# no Jobs submitted.
# -------------------------------------------------------------------

it "missing required env -> all input nodes labeled failed, no Jobs submitted" && {
    _reset_phase3_env
    unset PHASE3_EXPECTED_NIC_COUNT
    run __phase3_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/nic-health=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failure-reason=phase3-missing-env:PHASE3_EXPECTED_NIC_COUNT"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-env path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "required env var(s) unset:"
}

# -------------------------------------------------------------------
# C4. Missing job template -> every input node labeled =failed with
# reason=job-template-missing; no Jobs submitted.
# -------------------------------------------------------------------

it "missing job template -> all input nodes labeled failed, reason=job-template-missing" && {
    _reset_phase3_env
    mv "${TPL_DIR}/cluster-validation-phase3-job-config.yaml" \
       "${TPL_DIR}/cluster-validation-phase3-job-config.yaml.hidden"
    run __phase3_run node-a node-b
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-a amd.com/nic-health=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/nic-health-failure-reason=job-template-missing --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/nic-health=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/nic-health-failure-reason=job-template-missing --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-template path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Restore for the rest of the suite.
    mv "${TPL_DIR}/cluster-validation-phase3-job-config.yaml.hidden" \
       "${TPL_DIR}/cluster-validation-phase3-job-config.yaml"
}

# -------------------------------------------------------------------
# C5. kubectl apply failure -> node failed with reason=job-creation-failed.
# PHASE3_SCRIPT must NOT poll for that job since it never landed.
# -------------------------------------------------------------------

it "kubectl apply failure -> node failed with reason=job-creation-failed" && {
    _reset_phase3_env
    kubectl_mock_fail_sticky apply 1
    run __phase3_run node-z
    assert_status 0
    assert_kubectl_call \
        "label node node-z amd.com/nic-health=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-z amd.com/nic-health-failure-reason=job-creation-failed --overwrite"
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "submit-failed job must not be polled:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "kubectl apply failed for job="
}

# -------------------------------------------------------------------
# C6. Timeout: no Job conditions seeded + PHASE3_JOB_WAIT_TIME=0
# -> first iteration of the poll loop hits elapsed >= timeout
# immediately -> classified TIMEOUT, reason=nic-not-allocated,
# and the hung Job is explicitly deleted at cleanup. Mirrors the
# PHASE2_SCRIPT TC9/TC10 pattern.
# -------------------------------------------------------------------

it "no conditions + PHASE3_JOB_WAIT_TIME=0 -> reason=nic-not-allocated + cleanup delete" && {
    _reset_phase3_env
    export PHASE3_JOB_WAIT_TIME="0"
    run __phase3_run node-f
    assert_status 0
    assert_kubectl_call \
        "label node node-f amd.com/nic-health=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-f amd.com/nic-health-failure-reason=nic-not-allocated --overwrite"
    expected_job=$(_phase3_expected_job_name "node-f" "$ts")
    assert_kubectl_call \
        "delete job ${expected_job} --ignore-not-found=true --wait=false"
    assert_stdout_contains "TIMEOUT after 0s"
    assert_stdout_contains "deleting hung job"
}

# -------------------------------------------------------------------
# C7. Complete=True with PHASE3_RESULT status=passed in pod logs ->
# orchestrator parses the log marker and writes the =passed label.
# No failure-reason annotation is written.
# -------------------------------------------------------------------

it "Job Complete=True + passed marker -> orchestrator labels node passed, no annotation" && {
    _reset_phase3_env
    _seed_job_complete "node-pass" "$ts"
    run __phase3_run node-pass
    assert_status 0
    assert_kubectl_call \
        "label node node-pass amd.com/nic-health=passed --overwrite"
    if grep -F "annotate node node-pass amd.com/nic-health-failure-reason" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "passed-marker path must not write failure-reason:
$(grep 'annotate node node-pass' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "annotate node node-pass amd.com/nic-health-failed-nics" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "passed-marker path must not write failed-nics:
$(grep 'annotate node node-pass' "$KUBECTL_CALLS_FILE")"
    fi
}

# -------------------------------------------------------------------
# C8. Failed=True with PHASE3_RESULT status=failed in pod logs ->
# orchestrator parses reason= and failed_nics= tokens, writes the
# =failed label + failure-reason annotation + failed-nics annotation.
# -------------------------------------------------------------------

it "Job Failed=True + failed marker -> orchestrator labels failed + writes reason + failed-nics" && {
    _reset_phase3_env
    _seed_job_failed "node-jobfail" "$ts" \
        "rdma-state:rocep7s0/1=INIT" "rocep7s0"
    run __phase3_run node-jobfail
    assert_status 0
    assert_kubectl_call \
        "label node node-jobfail amd.com/nic-health=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failure-reason=rdma-state:rocep7s0/1=INIT"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failed-nics=rocep7s0"
}

# -------------------------------------------------------------------
# C8b. Complete=True but NO PHASE3_RESULT line in pod logs (e.g. the
# container exited 0 before the marker was emitted, or logs were
# truncated) -> orchestrator labels =failed with reason=no-result-line
# so the missing-signal failure is visible at the cluster level.
# -------------------------------------------------------------------

it "Job Complete=True but missing PHASE3_RESULT marker -> labeled failed reason=no-result-line" && {
    _reset_phase3_env
    # Seed only the Complete condition; do NOT seed a log marker.
    expected_job=$(_phase3_expected_job_name "node-nomarker" "$ts")
    kubectl_mock_set_job_condition "$expected_job" "Complete" "True"
    run __phase3_run node-nomarker
    assert_status 0
    assert_kubectl_call \
        "label node node-nomarker amd.com/nic-health=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failure-reason=no-result-line"
}

# -------------------------------------------------------------------
# C9. Parallel-submit ordering: N input nodes -> exactly N `kubectl apply`
# invocations BEFORE any `kubectl get job` poll. Mirrors PHASE1 /
# PHASE2 contract.
# -------------------------------------------------------------------

it "parallel-submit: N input nodes -> N submits, all before any wait poll" && {
    _reset_phase3_env
    _seed_job_complete "node-a" "$ts"
    _seed_job_complete "node-b" "$ts"
    _seed_job_complete "node-c" "$ts"
    run __phase3_run node-a node-b node-c
    assert_status 0
    n_apply=$(grep -cE "^apply( |$)" "$KUBECTL_CALLS_FILE" || true)
    assert_equals "3" "$n_apply"
    last_apply_line=$(grep -nE "^apply" "$KUBECTL_CALLS_FILE" \
        | tail -1 | cut -d: -f1)
    first_getjob_line=$(grep -nE "^get job" "$KUBECTL_CALLS_FILE" \
        | head -1 | cut -d: -f1)
    if [[ -z "$first_getjob_line" ]]; then
        _assert_fail "expected at least one 'get job' poll call"
    elif [[ "$last_apply_line" -ge "$first_getjob_line" ]]; then
        _assert_fail "submits must all precede any poll (last apply=${last_apply_line}, first get-job=${first_getjob_line}):
$(cat "$KUBECTL_CALLS_FILE")"
    fi
}

# -------------------------------------------------------------------
# C10. PHASE_NODES env-var fallback: when positional args are empty
# but PHASE_NODES is exported, the script uses that list.
# -------------------------------------------------------------------

it "PHASE_NODES env var is used when no positional args are given" && {
    _reset_phase3_env
    _seed_job_complete "node-env" "$ts"
    export PHASE_NODES="node-env"
    run __phase3_run    # NB: no positional args
    assert_status 0
    # Job was submitted for that node.
    expected_job=$(_phase3_expected_job_name "node-env" "$ts")
    assert_kubectl_call_contains "$expected_job"
    # And the orchestrator parsed the passed marker and wrote the label.
    assert_kubectl_call \
        "label node node-env amd.com/nic-health=passed --overwrite"
}

# -------------------------------------------------------------------
# PART D: PHASE3_SCRIPT sed pipeline rendering (GPUOP-829)
#
# These tests do NOT exercise __phase3_run end-to-end (the kubectl
# mock's `apply` arm drains stdin and discards it, so rendered YAML
# is not observable through the mock). Instead, they reach into
# the rendering helper logic by sed-substituting the real
# cluster-validation-job.yaml-embedded Phase 3 template the same way
# PHASE3_SCRIPT does (sed pipeline pinned to: $$NODE, metadata.name
# rename, $$EXPECTED_NIC_COUNT, $$ROCE_WORKLOAD_IMAGE). This is the
# most direct way to gate the new substitution + the new host-sys
# volume/mount.
#
# The fixture template is extracted live from the source manifest
# (configs/cluster-validation-job.yaml) so a regression in either
# the template (placeholder removed) or the sed pipeline (key
# dropped) lights up here.
# -------------------------------------------------------------------

# Extract the embedded Phase 3 Job template from the real source
# manifest (configs/cluster-validation-job.yaml) so the tests below
# render the actual shipped template, not the test stand-in
# fixture written above at TPL_DIR.
PHASE3_JOB_SOURCE_YAML="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-job.yaml"
REAL_PHASE3_TPL=$(mktemp)
trap 'rm -f "$REAL_PHASE3_TPL"' EXIT
python3 - "$PHASE3_JOB_SOURCE_YAML" >"$REAL_PHASE3_TPL" <<'PYEOF'
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if (d and d.get("kind") == "ConfigMap"
            and d.get("metadata", {}).get("name")
                == "cluster-validation-phase3-job-config"):
        sys.stdout.write(d["data"]["cluster-validation-phase3-job-config.yaml"])
        break
PYEOF
if [[ ! -s "$REAL_PHASE3_TPL" ]]; then
    echo "FATAL: could not extract Phase 3 Job template from ${PHASE3_JOB_SOURCE_YAML}" >&2
    exit 1
fi

# Render the real template the same way PHASE3_SCRIPT does. Mirrors
# the sed expression in cluster-validation-config.yaml PHASE3_SCRIPT
# (around line ~2441 post-GPUOP-834). Caller sets NODE, JOB_NAME,
# EXPECTED_NIC_COUNT, IMG.
_render_phase3_real() {
    local node="$1" job_name="$2" nic_count="$3" img="$4"
    sed "s|\$\$NODE|${node}|g; \
         s/^  name: cluster-validation-phase3-job/  name: ${job_name}/; \
         s|\$\$EXPECTED_NIC_COUNT|${nic_count}|g; \
         s|\$\$ROCE_WORKLOAD_IMAGE|${img}|g" \
        "$REAL_PHASE3_TPL"
}

# D1. With a populated ROCE_WORKLOAD_IMAGE, the $$-placeholder is
# fully substituted (no residual $$ tokens anywhere in the
# rendered template).
it "PHASE3 template render: \$\$ROCE_WORKLOAD_IMAGE is substituted (no residual placeholders)" && {
    rendered=$(_render_phase3_real "node-x" "cvf-phase3-node-x-ts" "8" \
               "docker.io/rocm/roce-workload:my-tag-123")
    img_line=$(printf '%s\n' "$rendered" | grep -E '^[[:space:]]+image:' || true)
    if [[ "$img_line" != *"image: docker.io/rocm/roce-workload:my-tag-123"* ]]; then
        _assert_fail "expected image: docker.io/rocm/roce-workload:my-tag-123 in rendered template, got: $img_line"
    fi
    if printf '%s\n' "$rendered" | grep -qE '\$\$'; then
        _assert_fail "rendered template still contains \$\$ placeholders:
$(printf '%s\n' "$rendered" | grep -E '\$\$')"
    fi
}

# D2. ROCE_WORKLOAD_IMAGE env var injected into nic-health container
# (so Check 5 can substring-match each NIC's fw_ver against the
# full image reference) AND the old host-sys hostPath volume +
# /sys-host mount are ABSENT. The new Check 5 reads
# /sys/class/infiniband/<dev>/fw_ver from the pod's auto-mounted
# sysfs (kernel sysfs is netns-agnostic for class paths), so a
# dedicated hostPath mount is no longer required. Assert here that
# the volume is gone so we don't accidentally re-introduce it.
#
# Implementation note: we write the rendered YAML to a tmp file
# and pass its path to python via argv. Mixing a heredoc-supplied
# python script with stdin-piped YAML breaks because the heredoc
# itself becomes python's stdin.
it "PHASE3 template render: ROCE_WORKLOAD_IMAGE env injected, host-sys volume absent" && {
    _render_phase3_real "node-y" "cvf-phase3-node-y-ts" "8" \
        "docker.io/rocm/roce-workload:any-tag-42" > "${TPL_DIR}/d2_rendered.yaml"
    out=$(python3 - "${TPL_DIR}/d2_rendered.yaml" <<'PYEOF'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1]))
spec = d["spec"]["template"]["spec"]
vols = spec.get("volumes", []) or []
host_sys_vol = next((v for v in vols if v.get("name") == "host-sys"), None)
assert host_sys_vol is None, \
    f"host-sys volume must be absent after Check 5 rewrite; got: {host_sys_vol}"
c = spec["containers"][0]
assert c["name"] == "nic-health", f"first container is {c['name']}"
mounts = c.get("volumeMounts", []) or []
host_sys_mt = next((m for m in mounts if m.get("name") == "host-sys"), None)
assert host_sys_mt is None, \
    f"host-sys volumeMount must be absent; got: {host_sys_mt}"
# ROCE_WORKLOAD_IMAGE env var must be injected into the container
# with the rendered image value (so Check 5's substring match has a
# reference to compare fw_ver against).
envs = c.get("env", []) or []
roce_env = next((e for e in envs if e.get("name") == "ROCE_WORKLOAD_IMAGE"), None)
assert roce_env is not None, f"ROCE_WORKLOAD_IMAGE env var must be set; envs={envs}"
assert roce_env.get("value") == "docker.io/rocm/roce-workload:any-tag-42", \
    f"ROCE_WORKLOAD_IMAGE value={roce_env.get('value')}"
print("OK")
PYEOF
)
    rc=$?
    assert_equals "0" "$rc"
    assert_equals "OK" "$out"
}

# D3. The container image MUST NOT be the obsolete
# network-operator-utils tag anywhere in the source template
# (post-GPUOP-834, this image is gone). Guards against accidental
# revert.
it "PHASE3 template source: network-operator-utils:v1.1.0 has been removed" && {
    if grep -F 'docker.io/rocm/network-operator-utils:v1.1.0' \
       "$PHASE3_JOB_SOURCE_YAML" >/dev/null; then
        _assert_fail "regressed: network-operator-utils:v1.1.0 still referenced in ${PHASE3_JOB_SOURCE_YAML}"
    fi
    # And the obsolete patchable comment must be gone too (GPUOP-829
    # design doc §4: removed because the image is now centrally
    # pinned via ROCE_WORKLOAD_IMAGE).
    if grep -F 'patchable: cluster-validation-framework.images.nic-health' \
       "$PHASE3_JOB_SOURCE_YAML" >/dev/null; then
        _assert_fail "regressed: obsolete patchable comment for nic-health still present in ${PHASE3_JOB_SOURCE_YAML}"
    fi
}

# D4. PHASE3_SCRIPT sed pipeline includes the new substitution
# expression. Guards against the sed key being dropped from the
# pipeline (which would leave a literal $$ROCE_WORKLOAD_IMAGE in
# the rendered template and the kubelet would fail to pull it).
it "PHASE3_SCRIPT sed pipeline substitutes \$\$ROCE_WORKLOAD_IMAGE" && {
    # The sed expression lives in cluster-validation-config.yaml
    # under PHASE3_SCRIPT. The literal stored in the ConfigMap key
    # is `s|\$\$ROCE_WORKLOAD_IMAGE|${image}|g` (backslash-escaped
    # so YAML/sed don't expand the placeholders). To gate on the
    # presence of the substitution in PHASE3_SCRIPT specifically
    # (not the unrelated PHASE2/PHASE4/PHASE5 occurrences), we
    # extract PHASE3_SCRIPT and grep its body.
    raw=$(extract_configmap_data "$CONFIGMAP" "PHASE3_SCRIPT")
    if ! printf '%s\n' "$raw" \
         | grep -F 's|\$\$ROCE_WORKLOAD_IMAGE|${image}|g' >/dev/null; then
        _assert_fail "PHASE3_SCRIPT sed pipeline missing \$\$ROCE_WORKLOAD_IMAGE substitution in ${CONFIGMAP}"
    fi
}

# D5. End-to-end shape check: a full render with a typical image
# tag produces a parseable Kubernetes Job manifest (no broken
# YAML from the sed substitution, no leftover $$ tokens, image
# is the expected value).
it "PHASE3 template render: yields a parseable Job manifest with the expected image" && {
    img_in="docker.io/rocm/roce-workload:ubuntu24_rocm-7.0.2_rccl-7.0.2_anp-v1.2.0_ainic-1.117.1-a-63"
    _render_phase3_real "node-z" "cvf-phase3-node-z-ts" "8" "$img_in" \
        > "${TPL_DIR}/d5_rendered.yaml"
    # Pass img_in to python via env so the heredoc can stay
    # single-quoted (no shell expansion).
    out=$(IMG="$img_in" python3 - "${TPL_DIR}/d5_rendered.yaml" <<'PYEOF'
import os, sys, yaml
img_in = os.environ["IMG"]
d = yaml.safe_load(open(sys.argv[1]))
assert d["kind"] == "Job", f"kind={d.get('kind')}"
assert d["metadata"]["name"] == "cvf-phase3-node-z-ts", \
    f"name={d['metadata']['name']}"
c = d["spec"]["template"]["spec"]["containers"][0]
assert c["image"] == img_in, f"image={c['image']}"
assert c["resources"]["limits"]["amd.com/nic"] == 8, \
    f"nic limit={c['resources']['limits']['amd.com/nic']}"
ns = d["spec"]["template"]["spec"]["nodeSelector"]
assert ns["kubernetes.io/hostname"] == "node-z", \
    f"nodeSelector={ns}"
print("OK")
PYEOF
)
    rc=$?
    assert_equals "0" "$rc"
    assert_equals "OK" "$out"
}

# -------------------------------------------------------------------
# PART E: PHASE3_CHECK_SCRIPT Check 5 (firmware <-> workload-image
# alignment) in-pod behavior. The new Check 5 reads
# /sys/class/infiniband/<dev>/fw_ver per ionic device and requires
# the running firmware string to appear as a substring of
# $ROCE_WORKLOAD_IMAGE. There is no compat map / nicctl /
# driver-version sysfs file anymore.
#
# Each Check 5 test:
#   * _reset_check5_env -- shared reset + Check 5 opt-in + PASS
#     fixtures for checks 1-4 so any failure has to come from Check 5
#     (or pre-flight). The per-test mock /sys/class/infiniband/ tree
#     is laid down via _seed_sysfs_ib (defined in PART B above).
#   * supplies ROCE_WORKLOAD_IMAGE and optionally
#     PHASE3_DRIVER_FW_STRICT.
# -------------------------------------------------------------------

_reset_check5_env() {
    _reset_check_env
    export PHASE3_DRIVER_FW_CHECK_ENABLED="true"
    # Pass-fixture defaults for checks 1-4 so any failure has to come
    # from Check 5 (or pre-flight). IP_LINK_FIXTURE is harmless now
    # that Check 2 reads sysfs (the `ip` shim is unused) but is kept
    # for symmetry with legacy tests.
    export LSPCI_FIXTURE="${FIXTURES_DIR}/lspci-pass.txt"
    export RDMA_LINK_FIXTURE="${FIXTURES_DIR}/rdma-link-pass.txt"
    export IBV_DEVICES_FIXTURE="${FIXTURES_DIR}/ibv-devices.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    export IBV_DEVINFO_FIXTURE_DEFAULT_V="${FIXTURES_DIR}/ibv-devinfo-pass.txt"
    # Each test that exercises Check 5 must populate a per-test mock
    # ib sysfs tree via _new_sysfs_ib_root + _seed_sysfs_ib.
}

# Default fw / image pair used by most Check 5 tests.
CHECK5_DEFAULT_FW="1.117.5-a-56"
CHECK5_DEFAULT_IMAGE="docker.io/rocm/roce-workload:ubuntu24_rocm-7.0.2_rccl-7.0.2_anp-v1.2.0_ainic-1.117.5-a-56"

# Lay down N ionic devices in the per-test ib sysfs root, all with
# the same fw string.
_seed_ionic_fw_uniform() {
    local root="$1" count="$2" fw="$3"
    local i
    for ((i=0; i<count; i++)); do
        _seed_sysfs_ib "$root" "ionic_${i}" "ionic" "$fw"
    done
}

# -------------------------------------------------------------------
# E1. PASS case: 8 ionic devices with matching fw + image tag.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 PASS: 8 ionic fw matches image tag" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    # observed_fw= field present and lists every device. Order is
    # not stable (bash associative array iteration) so check each
    # ionic_N=fw substring individually.
    assert_stdout_contains "observed_fw="
    for i in 0 1 2 3 4 5 6 7; do
        assert_stdout_contains "ionic_${i}=${CHECK5_DEFAULT_FW}"
    done
    # PASS path must not carry a mismatch token anywhere.
    if grep -F "fw-image-mismatch" <<<"$LAST_STDOUT" >/dev/null; then
        _assert_fail "PASS path must not emit fw-image-mismatch:
${LAST_STDOUT}"
    fi
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# E2. FAIL strict=true: fw does not appear in workload image tag.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 FAIL strict=true: fw not in image -> fw-image-mismatch" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    # Image tag carries a DIFFERENT fw value -- substring match fails.
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/roce-workload:ubuntu24_rocm-7.0.2_ainic-1.118.0-a-99"
    # strict defaults to true; assert explicitly for clarity.
    export PHASE3_DRIVER_FW_STRICT="true"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    # Reason carries the image-tag suffix (after the last colon), not
    # the full registry prefix.
    assert_stdout_contains "fw-image-mismatch:ionic_0=${CHECK5_DEFAULT_FW}/image=ubuntu24_rocm-7.0.2_ainic-1.118.0-a-99"
    assert_stdout_contains "observed_fw="
    assert_stdout_contains "ionic_0=${CHECK5_DEFAULT_FW}"
    # failed_nics must list every ionic device since they all mismatch.
    for i in 0 1 2 3 4 5 6 7; do
        assert_stdout_contains "ionic_${i}"
    done
}

# -------------------------------------------------------------------
# E3. FAIL -> PASS via strict=false: same mismatch as E2, but
#     warn-only mode suppresses the mismatch from the reason marker
#     while still surfacing observed_fw= and the warning log line.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 strict=false: mismatch surfaces in observed_fw + log, marker is passed" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/roce-workload:ubuntu24_rocm-7.0.2_ainic-1.118.0-a-99"
    export PHASE3_DRIVER_FW_STRICT="false"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    assert_stdout_contains "observed_fw="
    assert_stdout_contains "ionic_0=${CHECK5_DEFAULT_FW}"
    # Warning log line is still emitted (operator-visible) even in
    # warn-only mode -- the gate just doesn't trip the marker.
    assert_stdout_contains "[Phase 3] CHECK 5 MISMATCH: dev=ionic_0 fw=${CHECK5_DEFAULT_FW} not in image="
    # The marker's reason field MUST NOT carry fw-image-mismatch
    # tokens in warn-only mode. (The mismatch line is on stdout only
    # via the log message; check the PHASE3_RESULT line specifically.)
    result_line=$(grep -E '^PHASE3_RESULT ' <<<"$LAST_STDOUT" | tail -n 1 || true)
    if [[ "$result_line" == *"fw-image-mismatch"* ]]; then
        _assert_fail "warn-only marker must not carry fw-image-mismatch token: ${result_line}"
    fi
}

# -------------------------------------------------------------------
# E4. Check disabled: marker is byte-identical to the legacy
#     pre-Check-5 contract -- no observed_fw= field at all.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 disabled -> marker has no observed_fw= field" && {
    _reset_check5_env
    # Override the per-test enable: disable Check 5 explicitly.
    export PHASE3_DRIVER_FW_CHECK_ENABLED="false"
    # Seed the ib sysfs tree anyway -- the script must not read it
    # when the check is disabled (the tree being present is not an
    # implicit enable).
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    # observed_fw= field MUST be absent when Check 5 is disabled.
    result_line=$(grep -E '^PHASE3_RESULT ' <<<"$LAST_STDOUT" | tail -n 1 || true)
    if [[ "$result_line" == *"observed_fw="* ]]; then
        _assert_fail "disabled Check 5 must not emit observed_fw= field; got: ${result_line}"
    fi
    if [[ "$result_line" == *"fw-image-mismatch"* ]]; then
        _assert_fail "disabled Check 5 must not emit fw-image-mismatch; got: ${result_line}"
    fi
}

# -------------------------------------------------------------------
# E5. Missing ROCE_WORKLOAD_IMAGE env: "no data" => SKIP, never FAIL.
#     Phase 3 still passes (Checks 1-4 are clean in this fixture).
#     observed_fw= is still emitted so the read values are visible.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 missing ROCE_WORKLOAD_IMAGE -> SKIP, no fw-image-env-missing fail" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    # ROCE_WORKLOAD_IMAGE intentionally unset.
    unset ROCE_WORKLOAD_IMAGE
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    # observed_fw= is still emitted so operators see the read values.
    assert_stdout_contains "observed_fw="
    assert_stdout_contains "ionic_0=${CHECK5_DEFAULT_FW}"
    assert_stdout_contains "CHECK 5 SKIP: ROCE_WORKLOAD_IMAGE env not set"
    # No-data is a skip, not a fail. The legacy hard-fail reason must
    # not appear anywhere on the marker line.
    assert_stdout_not_contains "fw-image-env-missing"
}

# -------------------------------------------------------------------
# E6. Zero ionic devices in sysfs (empty tree): "no data" => SKIP,
#     never FAIL. Phase 3 still passes (Checks 1-4 are clean).
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 zero ionic devices -> SKIP, no sysfs-error fail" && {
    _reset_check5_env
    # Allocate empty ib sysfs root (no _seed_sysfs_ib calls).
    _new_sysfs_ib_root >/dev/null
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    assert_stdout_contains "CHECK 5 SKIP: no fw_ver readable under"
    assert_stdout_not_contains "sysfs-error:no-fw-ver"
}

# -------------------------------------------------------------------
# E7. Partial fw read: 8 devices in sysfs, but 2 have no fw_ver file
#     (simulates unreadable / driver bug). The 6 readable devices
#     all match the image tag -> PASS. observed_fw= lists only the
#     6 successfully-read devices.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 partial fw read: 6/8 readable + all match -> PASS, observed_fw lists 6" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    # 6 devices with readable fw matching the image tag.
    for i in 0 1 2 3 4 5; do
        _seed_sysfs_ib "$ib_root" "ionic_${i}" "ionic" "$CHECK5_DEFAULT_FW"
    done
    # 2 devices with NO fw_ver file (empty arg skips file creation).
    _seed_sysfs_ib "$ib_root" "ionic_6" "ionic" ""
    _seed_sysfs_ib "$ib_root" "ionic_7" "ionic" ""
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    assert_stdout_contains "observed_fw="
    # The 6 readable devices appear in observed_fw=.
    for i in 0 1 2 3 4 5; do
        assert_stdout_contains "ionic_${i}=${CHECK5_DEFAULT_FW}"
    done
    # The 2 unreadable devices DO NOT appear in observed_fw=.
    result_line=$(grep -E '^PHASE3_RESULT ' <<<"$LAST_STDOUT" | tail -n 1 || true)
    for i in 6 7; do
        if [[ "$result_line" == *"ionic_${i}="* ]]; then
            _assert_fail "ionic_${i} (no fw_ver) must not appear in observed_fw=: ${result_line}"
        fi
    done
}

# -------------------------------------------------------------------
# E8. Driver filter: a non-ionic IB device (e.g. mlx5) must be
#     skipped even if its fw_ver is populated -- only ionic devices
#     participate in Check 5.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT Check 5 non-ionic IB device is skipped" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    # A mlx5 IB device that would mismatch the image -- must be ignored.
    _seed_sysfs_ib "$ib_root" "mlx5_0" "mlx5_core" "99.99.99-NOT-IN-IMAGE"
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    run _run_check
    assert_status 0
    assert_stdout_contains "PHASE3_RESULT status=passed"
    # The mlx5 device must NOT appear in observed_fw=, nor cause a mismatch.
    result_line=$(grep -E '^PHASE3_RESULT ' <<<"$LAST_STDOUT" | tail -n 1 || true)
    if [[ "$result_line" == *"mlx5_0"* ]]; then
        _assert_fail "non-ionic mlx5_0 must not appear in marker: ${result_line}"
    fi
    if grep -F "fw-image-mismatch" <<<"$LAST_STDOUT" >/dev/null; then
        _assert_fail "non-ionic device must not trip fw-image-mismatch:
${LAST_STDOUT}"
    fi
}

# -------------------------------------------------------------------
# E9. Pre-flight FAIL via broken ibv_devinfo -> single
#     preflight-failed:ibv_devinfo=<msg> reason, checks 1-5 skipped.
#     This is the only pre-flight gate left -- nicctl is no longer
#     probed.
# -------------------------------------------------------------------

it "PHASE3_CHECK_SCRIPT pre-flight: broken ibv_devinfo -> FAIL preflight-failed:ibv_devinfo, checks skipped" && {
    _reset_check5_env
    _new_sysfs_ib_root >/dev/null
    ib_root="$PHASE3_IB_SYSFS_DIR"
    _seed_ionic_fw_uniform "$ib_root" 8 "$CHECK5_DEFAULT_FW"
    export ROCE_WORKLOAD_IMAGE="$CHECK5_DEFAULT_IMAGE"
    # Force ibv_devinfo to fail (non-zero exit + representative stderr).
    export IBV_DEVINFO_RC_DEFAULT="1"
    export IBV_DEVINFO_STDERR_DEFAULT="libibverbs: failed to load driver ionic_rdma"
    run _run_check
    assert_status 1
    assert_stdout_contains "PHASE3_RESULT status=failed"
    assert_stdout_contains "preflight-failed:ibv_devinfo=libibverbs:"
    # Pre-flight short-circuit: no per-check failure lines from
    # checks 1-5 (the script `exit 1`s before any of them runs).
    for forbidden in "CHECK 1 PASS" "CHECK 1 FAIL" "CHECK 2 FAIL" "CHECK 3 FAIL" "CHECK 4 FAIL" "CHECK 5 FAIL"; do
        if grep -qF -- "$forbidden" <<<"$LAST_STDOUT"; then
            _assert_fail "pre-flight short-circuit must skip [${forbidden}]; stdout:
${LAST_STDOUT}"
        fi
    done
}

# -------------------------------------------------------------------
# PART F: PHASE3_SCRIPT outer-driver marker parser -- observed_fw
# annotation writes. The new Check 5 only emits observed_fw= on the
# marker (observed_driver was removed when the compat-map path was
# retired), so the orchestrator's parser only writes the
# `observed-fw` annotation.
#
# These exercise the orchestrator's _phase3_parse_and_label code path
# (the sed-extraction of observed_fw= from the PHASE3_RESULT line
# and the matching annotate_phase_value call). The in-pod check body
# is irrelevant here -- the harness seeds the marker line directly
# via the kubectl mock's `kubectl_mock_set_pod_log job/<name>` hook.
# -------------------------------------------------------------------

# F1. Marker carries observed_fw on a pass line -> orchestrator
#     writes the observed-fw annotation and the =passed label. No
#     failure-reason / failed-nics annotations.
it "PHASE3_SCRIPT marker has observed_fw (pass) -> observed-fw annotation written" && {
    _reset_phase3_env
    expected_job=$(_phase3_expected_job_name "node-obs-fw" "$ts")
    kubectl_mock_set_job_condition "$expected_job" "Complete" "True"
    kubectl_mock_set_pod_log "job/${expected_job}" \
        "PHASE3_RESULT status=passed observed_fw=ionic_0=1.117.5-a-56,ionic_1=1.117.5-a-56"
    run __phase3_run node-obs-fw
    assert_status 0
    assert_kubectl_call \
        "label node node-obs-fw amd.com/nic-health=passed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/nic-health-observed-fw=ionic_0=1.117.5-a-56,ionic_1=1.117.5-a-56"
    # No failure-reason / failed-nics on a pass line.
    if grep -F "amd.com/nic-health-failure-reason" \
           "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass+observed-fw path must not write failure-reason:
$(grep amd.com/nic-health "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "amd.com/nic-health-failed-nics" \
           "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass+observed-fw path must not write failed-nics:
$(grep amd.com/nic-health "$KUBECTL_CALLS_FILE")"
    fi
}

# F2. Marker carries no observed_fw field (e.g. Check 5 disabled
#     in-pod) -> orchestrator writes no observed-fw annotation.
#     Today's pass label still goes through. The absence branch is
#     intentionally a no-op so any prior observed-fw annotation
#     stays as last-known-good for operators flipping the check off.
it "PHASE3_SCRIPT marker has no observed_fw field -> no observed-fw annotation written" && {
    _reset_phase3_env
    _seed_job_complete "node-obs-none" "$ts"   # legacy pass-only marker
    run __phase3_run node-obs-none
    assert_status 0
    assert_kubectl_call \
        "label node node-obs-none amd.com/nic-health=passed --overwrite"
    if grep -F "amd.com/nic-health-observed-fw" \
           "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "absent-observed_fw path must not write observed-fw annotation:
$(grep amd.com/nic-health "$KUBECTL_CALLS_FILE")"
    fi
    # observed-driver annotation is dead -- must never be written.
    if grep -F "amd.com/nic-health-observed-driver" \
           "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "observed-driver annotation must never be written:
$(grep amd.com/nic-health "$KUBECTL_CALLS_FILE")"
    fi
}

# F3. Marker on a FAILED node with fw-image-mismatch reason +
#     observed_fw -> orchestrator writes:
#       * label=failed
#       * failure-reason=<fw-image-mismatch:.>
#       * failed-nics=<csv>
#       * observed-fw=<csv>
#     i.e. today's failure annotations PLUS the new observed-fw
#     annotation (per the design "regardless of pass/fail outcome
#     whenever the marker carried them").
it "PHASE3_SCRIPT marker on failed node with fw-image-mismatch -> failure annotations + observed-fw written" && {
    _reset_phase3_env
    expected_job=$(_phase3_expected_job_name "node-fw-mismatch" "$ts")
    kubectl_mock_set_job_condition "$expected_job" "Failed" "True"
    kubectl_mock_set_pod_log "job/${expected_job}" \
        "PHASE3_RESULT status=failed reason=fw-image-mismatch:ionic_1=1.117.5-a-56/image=ainic-1.118.0-a-99 failed_nics=ionic_1 observed_fw=ionic_0=1.117.5-a-56,ionic_1=1.117.5-a-56"
    run __phase3_run node-fw-mismatch
    assert_status 0
    assert_kubectl_call \
        "label node node-fw-mismatch amd.com/nic-health=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failure-reason=fw-image-mismatch:ionic_1=1.117.5-a-56/image=ainic-1.118.0-a-99"
    assert_kubectl_call_contains \
        "amd.com/nic-health-failed-nics=ionic_1"
    assert_kubectl_call_contains \
        "amd.com/nic-health-observed-fw=ionic_0=1.117.5-a-56,ionic_1=1.117.5-a-56"
}

assert_summary
