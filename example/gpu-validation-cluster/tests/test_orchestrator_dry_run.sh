#!/bin/bash
# Orchestrator DRY_RUN=1 tests.
#
# Covers the three scenarios from the design doc §7 "Orchestrator
# dry-run" bullet and the explicit list in the description:
# 1. All five skip flags true -> exits 0, NO kubectl write calls
# 2. Phases 1+2 enabled, 3-5 skipped -> ends after Phase 2 filter
# 3. Phase 3 enabled but no amd-nic=true nodes -> empty pool, exits 0
#
# Approach: the orchestrator body is embedded in the CronJob YAML
# (cluster-validation-job.yaml). We extract it once with
# `extract_cronjob_orchestrator`, drop the body to a tmp file, then
# invoke it as a sub-shell with:
# * DRY_RUN=1 in the environment
# * a mock `kubectl` first on PATH (lib/kubectl_mock.sh)
# * a candidate-label state pre-seeded so Phase 0 can find nodes
# to walk the pipeline with
# * the PHASE_NODE_LABEL_SCRIPT body fed via the env var the
# orchestrator reads (`echo "$PHASE_NODE_LABEL_SCRIPT" > /tmp/.`)
# * every other ConfigMap key the orchestrator reads supplied as
# a normal env var
#
# We then assert on:
# * exit status of the orchestrator
# * the kubectl-call log (DRY_RUN must produce zero writes)
# * the orchestrator's stdout (the planned phase order is the only
# contract for engineers reading the cronjob log)

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
CRONJOB="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-job.yaml"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_orchestrator_dry_run.sh"
echo "  CronJob: ${CRONJOB}"
echo "================================================================"

ORCH_SCRIPT=$(mktemp -t orchestrator-XXXXXX.sh)
trap 'rm -f "$ORCH_SCRIPT"; kubectl_mock_cleanup' EXIT

extract_cronjob_orchestrator "$CRONJOB" >"$ORCH_SCRIPT"
if [[ ! -s "$ORCH_SCRIPT" ]]; then
    echo "FATAL: orchestrator extraction produced empty output" >&2
    exit 1
fi
if ! bash -n "$ORCH_SCRIPT"; then
    echo "FATAL: extracted orchestrator has bash syntax errors" >&2
    exit 1
fi

# Read PHASE_NODE_LABEL_SCRIPT once for re-use across tests. The
# orchestrator sources whatever string is present in the env var, so
# we pass the helper library through that channel.
PHASE_NODE_LABEL_SCRIPT=$(extract_configmap_data "$CONFIGMAP" \
    "PHASE_NODE_LABEL_SCRIPT")

kubectl_mock_init

# Common env shape supplied to every orchestrator run. Each test may
# override specific keys (notably the SKIP_* flags). The set mirrors
# what the ConfigMap envFrom in the real CronJob delivers.
_run_orchestrator() {
    # Args: <skip1> <skip2> <skip3> <skip4> <skip5>
    local skip1="$1" skip2="$2" skip3="$3" skip4="$4" skip5="$5"
    # Sub-shell so per-test env vars do not leak across tests.
    DRY_RUN=1 \
    LOG_DIR="$(mktemp -d -t orch-log-XXXXXX)" \
    CANDIDATE_LABEL="amd.com/cluster-validation-candidate=true" \
    SUCCESS_LABEL="amd.com/cluster-validation-status=passed" \
    FAILURE_LABEL="amd.com/cluster-validation-status=failed" \
    TIMESTAMP_ANNOTATION="amd.com/cluster-validation-last-run-timestamp" \
    PHASE1_LABEL_KEY="amd.com/gpu-hw-acceptance" \
    PHASE2_LABEL_KEY="amd.com/gpu-mesh-validation" \
    PHASE3_LABEL_KEY="amd.com/nic-health" \
    PHASE4_LABEL_KEY="amd.com/rail-bandwidth" \
    PHASE5_LABEL_KEY="amd.com/cluster-validation-status" \
    PHASE_FAILURE_REASON_ANNOTATION_SUFFIX="-failure-reason" \
    PHASE_NODE_LABEL_SCRIPT="$PHASE_NODE_LABEL_SCRIPT" \
    SKIP_GPU_HW_ACCEPTANCE="$skip1" \
    SKIP_GPU_MESH_VALIDATION="$skip2" \
    SKIP_NIC_VALIDATION="$skip3" \
    SKIP_RAIL_BANDWIDTH_TEST="$skip4" \
    SKIP_RCCL_TEST="$skip5" \
    DEBUG_DELAY="0" \
    bash "$ORCH_SCRIPT"
}

# Suppress the bash-shim's `set -uo pipefail` propagation from tripping
# on our env-var references in tests that intentionally leave some
# vars unset.
set +u

# -------------------------------------------------------------------
# Scenario 1: all 5 skip flags true -> exit 0, no kubectl writes
# -------------------------------------------------------------------

it "DRY_RUN with all 5 skip flags true exits 0 and writes nothing to kubectl" && {
    kubectl_mock_reset
    # Seed one candidate node so Phase 0 has something to discover.
    kubectl_mock_set_label node-1 \
        amd.com/cluster-validation-candidate true
    run _run_orchestrator true true true true true
    assert_status 0
    # Every skip flag is true -> the only kubectl calls the orchestrator
    # makes are read-only (`get` for nodes_with_label / candidate
    # listing). Assert that NO label/annotate WRITES happened.
    if grep -E "^(label|annotate)( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "DRY_RUN produced kubectl write calls:
$(grep -E '^(label|annotate)( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Sanity: orchestrator printed the DRY_RUN banner.
    assert_stdout_contains "DRY_RUN=1 -- no kubectl writes"
    # Pass-through trail for every skipped phase.
    assert_stdout_contains "SKIP_GPU_HW_ACCEPTANCE=true"
    assert_stdout_contains "SKIP_GPU_MESH_VALIDATION=true"
    assert_stdout_contains "SKIP_NIC_VALIDATION=true"
    assert_stdout_contains "SKIP_RAIL_BANDWIDTH_TEST=true"
    assert_stdout_contains "SKIP_RCCL_TEST=true"
}

# -------------------------------------------------------------------
# Scenario 2: phases 1+2 enabled, 3-5 skipped -> ends after Phase 2
# -------------------------------------------------------------------

it "DRY_RUN with phases 1+2 enabled and 3-5 skipped ends after Phase 2 filter" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-1 \
        amd.com/cluster-validation-candidate true
    # Mark node-1 as having passed Phase 1 and Phase 2 so the
    # filter_passed_nodes gate downstream of each run_phaseN stub
    # carries it forward. The DRY_RUN stubs are no-ops, so the
    # filter must already see "=passed" on the prior labels for the
    # node to survive into the downstream pass-through.
    kubectl_mock_set_label node-1 amd.com/gpu-hw-acceptance passed
    kubectl_mock_set_label node-1 amd.com/gpu-mesh-validation passed
    run _run_orchestrator false false true true true
    assert_status 0
    # No kubectl write calls -- DRY_RUN.
    if grep -E "^(label|annotate)( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "DRY_RUN produced kubectl write calls:
$(grep -E '^(label|annotate)( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Phase 1 + Phase 2 must have run (banners present).
    assert_stdout_contains "[Phase 1] DRY_RUN -- skipping run_phase1"
    assert_stdout_contains "[Phase 2] DRY_RUN -- skipping run_phase2"
    # Phases 3, 4, 5 must take the SKIP_* pass-through branch (not the
    # active-phase branch), i.e. they emit the documented "pass-through"
    # log line. Phase 4.5 has no skip flag; it pass-throughs.
    assert_stdout_contains "[Phase 3] SKIP_NIC_VALIDATION=true -- pass-through"
    assert_stdout_contains "[Phase 4] SKIP_RAIL_BANDWIDTH_TEST=true -- pass-through"
    assert_stdout_contains "[Phase 5] SKIP_RCCL_TEST=true -- pass-through"
    # Phase 1 banner must precede Phase 2 banner in the log.
    # The orchestrator now prefixes every line with a wrapper
    # timestamp, so the inner `[Phase N] DRY_RUN` banner is no
    # longer at column 0 -- drop the `^` anchor.
    line_p1=$(printf '%s\n' "$LAST_STDOUT" | grep -n "\[Phase 1\] DRY_RUN" | head -1 | cut -d: -f1)
    line_p2=$(printf '%s\n' "$LAST_STDOUT" | grep -n "\[Phase 2\] DRY_RUN" | head -1 | cut -d: -f1)
    if [[ -z "$line_p1" || -z "$line_p2" || "$line_p1" -ge "$line_p2" ]]; then
        _assert_fail "Phase 1 banner must precede Phase 2 banner (p1=${line_p1}, p2=${line_p2})"
    fi
}

# -------------------------------------------------------------------
# Scenario 3: phase 3 enabled but no amd-nic=true nodes -> empty pool
# -------------------------------------------------------------------

it "DRY_RUN with Phase 3 enabled but no amd-nic nodes -> Phase 3 empty pool, exit 0" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-1 \
        amd.com/cluster-validation-candidate true
    # Mark node-1 passed for Phase 1+2 so the pool reaches Phase 3.
    kubectl_mock_set_label node-1 amd.com/gpu-hw-acceptance passed
    kubectl_mock_set_label node-1 amd.com/gpu-mesh-validation passed
    # Intentionally do NOT set feature.node.kubernetes.io/amd-nic=true
    # on any node. Phase 3 intersect must return an empty set.
    run _run_orchestrator false false false true true
    assert_status 0
    # No kubectl write calls -- DRY_RUN.
    if grep -E "^(label|annotate)( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "DRY_RUN produced kubectl write calls:
$(grep -E '^(label|annotate)( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Phase 3 must take the "no NIC-capable nodes" branch.
    assert_stdout_contains "[Phase 3] no NIC-capable nodes -- skipping"
    # And the run_phase3 stub must NOT have been reached.
    assert_stdout_not_contains "[Phase 3] DRY_RUN -- skipping run_phase3"
}

# -------------------------------------------------------------------
# Bonus contract checks
# -------------------------------------------------------------------

it "DRY_RUN exits 0 when no candidate nodes are present (empty Phase 0 pool)" && {
    kubectl_mock_reset
    # No candidate nodes seeded.
    run _run_orchestrator false false false false false
    assert_status 0
    assert_stdout_contains "empty candidate pool after Phase 0"
    # Empty pool -> no phase banners (we exit before Phase 1).
    assert_stdout_not_contains "[Phase 1] DRY_RUN"
}

it "DRY_RUN preserves cleanup pass-through (no MPIJob deletions, no log collection)" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-1 \
        amd.com/cluster-validation-candidate true
    run _run_orchestrator true true true true true
    assert_status 0
    # Cleanup / log collection branches both honor DRY_RUN by short-circuiting.
    assert_stdout_contains "[Cleanup] DRY_RUN -- skipping old-MPIJob cleanup"
    assert_stdout_contains "[Logs] DRY_RUN -- skipping launcher log collection"
}

# -------------------------------------------------------------------
# every orchestrator stdout/stderr line carries a per-line
# [YYYY-MM-DD HH:MM:SS.mmm] timestamp prefix.
#
# The orchestrator container args install a `_ts_prefix` reader that
# pipes everything through a `printf '[%s] %s\n' "$(date '+%F %T.%3N')"`
# loop. Sourced phase scripts and helpers inherit the redirect, so
# every echo / kubectl output line lands in the per-run log with the
# same prefix shape -- which is what we assert here.
#
# Per-line readers consuming the cron pod log (operators, log scrapers,
# downstream automation) depend on this format being deterministic, so
# treat any unprefixed line as a regression.
# -------------------------------------------------------------------
it "every orchestrator stdout line carries a [YYYY-MM-DD HH:MM:SS.mmm] timestamp prefix" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-1 \
        amd.com/cluster-validation-candidate true
    run _run_orchestrator true true true true true
    assert_status 0
    # Every non-empty line must match the wrapper format. Allow an
    # unprefixed final newline only.
    bad_lines=$(printf '%s\n' "$LAST_STDOUT" \
        | grep -nvE '^\[[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}\] ' \
        | grep -v '^[0-9]*:$' || true)
    if [[ -n "$bad_lines" ]]; then
        _assert_fail "found stdout lines missing the [YYYY-MM-DD HH:MM:SS.mmm] prefix:
${bad_lines}"
    fi
    # Smoke check: at least one prefixed line was actually emitted
    # (so the assertion above can't trivially pass on an empty stream).
    if ! printf '%s\n' "$LAST_STDOUT" | grep -qE '^\[[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}\] '; then
        _assert_fail "orchestrator stdout was empty or never produced a prefixed line"
    fi
}

# -------------------------------------------------------------------
# configs/*.yaml must apply standalone: parse as valid YAML AND have
# no leftover __XXX__ placeholder tokens. Guards against accidental
# re-introduction of render-time substitution markers. Operators who
# want to deploy without gpu-cluster.sh need this contract intact.
# -------------------------------------------------------------------
it "configs/*.yaml parse standalone with no __PLACEHOLDER__ tokens" && {
    CFG_DIR=$(cd -- "${TEST_DIR}/../configs" && pwd)
    python3 - "$CFG_DIR" <<'PY'
import os, sys, yaml
cfg_dir = sys.argv[1]
checked = 0
for name in ("cluster-validation-config.yaml",
             "cluster-validation-job.yaml",
             "nad-per-rail.yaml"):
    path = os.path.join(cfg_dir, name)
    docs = list(yaml.safe_load_all(open(path)))
    checked += 1
    for doc in docs:
        if not isinstance(doc, dict):
            continue
        data = doc.get("data") or {}
        for k, v in data.items():
            if "__" in str(v):
                if any(tok in str(v) for tok in (
                    "__WORKER_REPLICAS__", "__LAUNCHER_REPLICAS__",
                    "__SLOTS_PER_WORKER__", "__GPU_PER_WORKER__",
                    "__PF_NIC_PER_WORKER__", "__VF_NIC_PER_WORKER__",
                    "__NODE_VALIDATION_INTERVAL_MINS__",
                    "__NODE_SELECTOR_LABELS__",
                    "__SKIP_GPU_HW_ACCEPTANCE__", "__SKIP_GPU_MESH_VALIDATION__",
                    "__SKIP_NIC_VALIDATION__", "__SKIP_RAIL_BANDWIDTH_TEST__",
                    "__SKIP_RCCL_TEST__", "__CRONJOB_SCHEDULE__")):
                    sys.exit(f"placeholder leak in {name} data[{k}]: {v!r}")
print(f"{checked} YAML files parsed standalone; no placeholder tokens")
PY
    if [[ $? -ne 0 ]]; then
        _assert_fail "configs/*.yaml standalone check failed"
    fi
}

assert_summary
