#!/bin/bash
# Unit tests for PHASE2_SCRIPT against the mocked
# kubectl harness and phase2.log fixtures.
#
# Scope (from the design doc §4
# "Code Path: PHASE2_SCRIPT" / §7 "Testing Strategy"):
# * pass case (BW above threshold) [TC2]
# * bus-bw-below-threshold fail [TC5]
# * rccl-crash (mpirun non-zero exit) [TC6]
# * timeout (Job stays pending past PHASE2_JOB_WAIT_TIME) [TC9 + TC10]
# * SKIP_GPU_MESH_VALIDATION=true short-circuit [TC3]
# * threshold-too-high inject (PHASE2_BW_THRESHOLD=9999) [TC5]
# * empty input list [TC7]
# * missing-env fast-fail
# * job-template-missing fast-fail
# * Failed=True with no recognized marker -> default rccl-crash
# * PHASE_NODES env-var fallback (when no positional args)
#
# How PHASE2_SCRIPT is exercised:
#
# The script body is a block-scalar inside cluster-validation-config.yaml.
# We extract it with lib/extract_script.sh, then patch the one hardcoded
# absolute path so the test can run as a non-root user without touching
# /phase2-configs:
# /phase2-configs/cluster-validation-phase2-job-config.yaml
# -> ${TPL_DIR}/cluster-validation-phase2-job-config.yaml
# The script uses `local` / `declare -A`, so we wrap the patched body
# in a function `__phase2_run` and invoke that. The helper library
# (PHASE_NODE_LABEL_SCRIPT) is sourced first so label_phase_passed /
# label_phase_failed / annotate_phase_value are defined.
#
# `kubectl` is the mock from lib/kubectl_mock.sh. Job "completion" is
# simulated by seeding state via kubectl_mock_set_job_condition (Phase 1
# helper, reused) and kubectl_mock_set_pod_for_job, plus the new
# kubectl_mock_set_pod_log helper for the `kubectl logs` arm added in
# this change.
#
# Timeouts are exercised by SETTING THE TIMEOUT TO 0 -- the poll-wait
# loop checks `elapsed >= timeout` BEFORE the first kubectl get, so
# a 0-second budget short-circuits to TIMEOUT on the first iteration
# without sleeping. Without this, the only way to hit the timeout
# branch would be to actually wait for PHASE2_JOB_WAIT_TIME seconds.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase2"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase2.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

# Per-process tmp dirs:
# TPL_DIR -- holds a placeholder phase2 job template (the real
# template lives in cluster-validation-phase2-job-config
# ConfigMap,; for these tests PHASE2_SCRIPT
# only needs the file to exist + be sed-able, then
# `kubectl apply` is mocked).
# PHASE2_BODY -- the patched, function-wrapped script we source.
PHASE2_DIR=$(mktemp -d -t phase2-tests-XXXXXX)
TPL_DIR="${PHASE2_DIR}/tpl"
PHASE2_BODY="${PHASE2_DIR}/phase2-body.sh"
HELPER_SCRIPT="${PHASE2_DIR}/phase-helpers.sh"
mkdir -p "$TPL_DIR"

trap 'rm -rf "$PHASE2_DIR"; kubectl_mock_cleanup' EXIT

# Minimal job template stand-in. The real template lives in the
# cluster-validation-phase2-job-config ConfigMap. PHASE2_SCRIPT pipes a
# sed-rendered copy to `kubectl apply -f -`; since kubectl is mocked,
# the only constraints on contents are:
# * the file must exist (`[[ ! -f "$job_template" ]]` guard)
# * the sed expressions must not produce non-zero -- both are fine on
# any plain-text file with the substitution markers present.
cat >"${TPL_DIR}/cluster-validation-phase2-job-config.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: cluster-validation-phase2-job
spec:
  template:
    spec:
      nodeSelector:
        kubernetes.io/hostname: $$NODE
      containers:
        - name: phase2-rccl
          image: $$ROCE_WORKLOAD_IMAGE
          resources:
            limits:
              amd.com/gpu: 8
YAML

# Extract PHASE2_SCRIPT and patch the one hardcoded path so the test
# can run as a non-root user without /phase2-configs existing. Also
# pin the timestamp so seeded mock state always matches what the script
# generates -- the production script calls `date +%Y%m%d-%H%M%S` once
# per submit; we replace that with a fixed test value.
RAW_PHASE2=$(extract_configmap_data "$CONFIGMAP" "PHASE2_SCRIPT")
if [[ -z "$RAW_PHASE2" ]]; then
    echo "FATAL: PHASE2_SCRIPT extraction produced empty output" >&2
    exit 1
fi

PATCHED_PHASE2=$(printf '%s\n' "$RAW_PHASE2" \
    | sed "s|/phase2-configs/cluster-validation-phase2-job-config.yaml|${TPL_DIR}/cluster-validation-phase2-job-config.yaml|g" \
    | sed 's|ts=\$(date +%Y%m%d-%H%M%S)|ts="${PHASE2_TEST_TS:-$(date +%Y%m%d-%H%M%S)}"|')

# Wrap in a function so `local` and `declare -A` (used heavily inside
# PHASE2_SCRIPT) are valid. The orchestrator sources the
# script body inside run_phase2, which is a function, so this matches
# production wiring.
{
    printf '__phase2_run() {\n'
    printf '%s\n' "$PATCHED_PHASE2"
    printf '}\n'
} > "$PHASE2_BODY"

if ! bash -n "$PHASE2_BODY"; then
    echo "FATAL: patched PHASE2_SCRIPT has bash syntax errors" >&2
    exit 1
fi

# Extract the helper library (label_phase_passed/failed,
# annotate_phase_value) once; sourced before every test so each test
# gets a fresh function definition.
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

# shellcheck disable=SC1090
source "$HELPER_SCRIPT"
# shellcheck disable=SC1090
source "$PHASE2_BODY"

# Sanity: required functions are defined.
for fn in label_phase_passed label_phase_failed annotate_phase_value __phase2_run; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: required function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# Per-test reset: wipe the kubectl call log and any seeded state, and
# re-export the baseline env PHASE2_SCRIPT reads. Tests override pieces
# of this (notably SKIP_GPU_MESH_VALIDATION and PHASE2_BW_THRESHOLD)
# before calling __phase2_run.
_reset_phase2_env() {
    kubectl_mock_reset
    export PHASE2_LABEL_KEY="amd.com/gpu-mesh-validation"
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/roce-workload:test"
    # 60s is large enough that the poll loop's first iteration (which
    # checks Complete=True / Failed=True immediately, seeded by the
    # tests) breaks out before any sleep. Tests that exercise the
    # timeout branch override this to 0.
    export PHASE2_JOB_WAIT_TIME="60"
    export PHASE2_BW_THRESHOLD="200"
    # Pin the timestamp PHASE2_SCRIPT puts into job names so seeded
    # state always matches what the script looks up.
    export PHASE2_TEST_TS="testts0001"
    unset SKIP_GPU_MESH_VALIDATION PHASE_NODES
}

# Suppress the -u trap for tests that intentionally leave optional env
# vars unset (PHASE_NODES, SKIP_GPU_MESH_VALIDATION).
set +u

# Helper: compute the job name PHASE2_SCRIPT will generate for <node>
# with the pinned PHASE2_TEST_TS. Mirrors PHASE2_SCRIPT exactly:
# cvf-phase2-${node}-${ts} (when short enough)
# cvf-phase2-${sha1(node)|6}-${ts} (when over 63 chars)
_phase2_expected_job_name() {
    local node="$1" ts="$2" max_len=63 prefix="cvf-phase2"
    local jn="${prefix}-${node}-${ts}"
    if [[ "${#jn}" -gt "$max_len" ]]; then
        local h
        h=$(echo -n "$node" | sha1sum | cut -c1-6)
        jn="${prefix}-${h}-${ts}"
    fi
    printf '%s' "$jn"
}

# Seed mock state for one job: Complete=True + pod-for-job + canned pod log.
_seed_job_complete() {
    local node="$1" ts="$2" pod="$3" log_fixture="$4"
    local job
    job=$(_phase2_expected_job_name "$node" "$ts")
    kubectl_mock_set_job_condition "$job" "Complete" "True"
    kubectl_mock_set_pod_for_job   "$job" "$pod"
    kubectl_mock_set_pod_log       "$pod" "${FIXTURES_DIR}/${log_fixture}"
}

# Seed mock state for one job: Failed=True + pod-for-job + canned pod log.
_seed_job_failed() {
    local node="$1" ts="$2" pod="$3" log_fixture="$4"
    local job
    job=$(_phase2_expected_job_name "$node" "$ts")
    kubectl_mock_set_job_condition "$job" "Failed" "True"
    kubectl_mock_set_pod_for_job   "$job" "$pod"
    kubectl_mock_set_pod_log       "$pod" "${FIXTURES_DIR}/${log_fixture}"
}

# Seed mock state for one job: neither Complete nor Failed seeded ->
# kubectl returns empty string for both jsonpath queries -> PHASE2_SCRIPT
# loops until elapsed >= PHASE2_JOB_WAIT_TIME. Tests pair this with
# PHASE2_JOB_WAIT_TIME=0 to force an immediate TIMEOUT classification.
_seed_job_pending() {
    : # no state seeding -- absence == empty jsonpath response
}

ts=$(printf '%s' "testts0001")

# -------------------------------------------------------------------
# 1. Empty input list -> no-op, exit 0, no kubectl side effects.
# -------------------------------------------------------------------

it "PHASE2_SCRIPT with empty input list is a no-op and returns 0" && {
    _reset_phase2_env
    run __phase2_run
    assert_status 0
    assert_kubectl_no_calls
    assert_stdout_contains "no input nodes -- nothing to do"
}

# -------------------------------------------------------------------
# 2. SKIP_GPU_MESH_VALIDATION=true -> every input node pass-labeled,
# NO Phase 2 Job submission, no kubectl get/logs/apply work.
# -------------------------------------------------------------------

it "SKIP_GPU_MESH_VALIDATION=true pass-labels every input node, no Jobs created" && {
    _reset_phase2_env
    export SKIP_GPU_MESH_VALIDATION="true"
    run __phase2_run node-a node-b node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/gpu-mesh-validation=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/gpu-mesh-validation=passed --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/gpu-mesh-validation=passed --overwrite"
    # No `kubectl apply` (Job submission) anywhere.
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP_GPU_MESH_VALIDATION=true must not submit any Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP must not poll Jobs:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^logs " "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP must not fetch pod logs:
$(grep -E '^logs ' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "SKIP_GPU_MESH_VALIDATION=true -- pass-labeling"
}

it "SKIP_GPU_MESH_VALIDATION accepts case-insensitive value (TRUE)" && {
    _reset_phase2_env
    export SKIP_GPU_MESH_VALIDATION="TRUE"
    run __phase2_run node-x
    assert_status 0
    assert_kubectl_call \
        "label node node-x amd.com/gpu-mesh-validation=passed --overwrite"
}

# -------------------------------------------------------------------
# 3. Missing required env var (ROCE_WORKLOAD_IMAGE) -> every input
# node labeled =failed with reason=phase2-missing-env:.; no Jobs
# submitted.
# -------------------------------------------------------------------

it "missing required env var -> all input nodes labeled failed, no Jobs submitted" && {
    _reset_phase2_env
    unset ROCE_WORKLOAD_IMAGE
    run __phase2_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-mesh-validation-failure-reason=phase2-missing-env:ROCE_WORKLOAD_IMAGE"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-env path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "required env var(s) unset:"
}

# -------------------------------------------------------------------
# 4. Missing job template -> every input node labeled failed with
# reason=job-template-missing; no Jobs submitted.
# -------------------------------------------------------------------

it "missing job template -> all input nodes labeled failed, reason=job-template-missing" && {
    _reset_phase2_env
    # Hide the template the patched script expects. Restore before the
    # next test by recreating the file inline.
    mv "${TPL_DIR}/cluster-validation-phase2-job-config.yaml" \
       "${TPL_DIR}/cluster-validation-phase2-job-config.yaml.hidden"
    run __phase2_run node-a node-b
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-a amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/gpu-mesh-validation-failure-reason=job-template-missing --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-mesh-validation-failure-reason=job-template-missing --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-template path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Restore for the rest of the suite.
    mv "${TPL_DIR}/cluster-validation-phase2-job-config.yaml.hidden" \
       "${TPL_DIR}/cluster-validation-phase2-job-config.yaml"
}

# -------------------------------------------------------------------
# 5. Pass case: Complete=True + pass log -> =passed label + measured-bw
# annotation parsed from "Avg bus bandwidth: <value>" line.
# -------------------------------------------------------------------

it "single node pass: Complete=True + pass log -> =passed, measured-bw annotated" && {
    _reset_phase2_env
    pod="cvf-pod-node-a-pass"
    _seed_job_complete "node-a" "$ts" "$pod" "phase2-pass.log"
    run __phase2_run node-a
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/gpu-mesh-validation=passed --overwrite"
    # measured-bw annotation parsed from "Avg bus bandwidth: 234.7".
    assert_kubectl_call \
        "annotate node node-a amd.com/gpu-mesh-validation-measured-bw=234.7 --overwrite"
    # No failed label or failure-reason annotation on the pass path.
    if grep -F "node-a amd.com/gpu-mesh-validation=failed" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass-path must not write the failed label:
$(grep failed "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "annotate node node-a amd.com/gpu-mesh-validation-failure-reason" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass-path must not write a failure-reason annotation:
$(grep failure-reason "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "PASS (measured-bw=234.7)"
}

# -------------------------------------------------------------------
# 6. bus-bw-below-threshold: Failed=True + bw-below-threshold log
# -> =failed, reason=bus-bw-below-threshold, measured-bw annotated.
# -------------------------------------------------------------------

it "Failed + bw-below-threshold marker -> =failed reason=bus-bw-below-threshold + measured-bw" && {
    _reset_phase2_env
    pod="cvf-pod-node-b-bw"
    _seed_job_failed "node-b" "$ts" "$pod" "phase2-bw-below-threshold.log"
    run __phase2_run node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-b amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-mesh-validation-failure-reason=bus-bw-below-threshold --overwrite"
    # Measured BW from log fixture is 173.7.
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-mesh-validation-measured-bw=173.7 --overwrite"
    assert_stdout_contains "FAIL reason=bus-bw-below-threshold"
}

# -------------------------------------------------------------------
# 7. Threshold-too-high inject: PHASE2_BW_THRESHOLD=9999, but the
# classification is driven by the container log marker (validator
# runs inside the Job container, not in PHASE2_SCRIPT) -- so the
# log we serve is the same bw-below-threshold fixture, and the
# classification is still bus-bw-below-threshold. This test pins
# the contract: PHASE2_SCRIPT never re-runs the validator; it only
# classifies by log markers.
# -------------------------------------------------------------------

it "PHASE2_BW_THRESHOLD=9999 inject still classifies via log marker (bus-bw-below-threshold)" && {
    _reset_phase2_env
    export PHASE2_BW_THRESHOLD="9999"
    pod="cvf-pod-node-c-9999"
    _seed_job_failed "node-c" "$ts" "$pod" "phase2-bw-below-threshold.log"
    run __phase2_run node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-c amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-c amd.com/gpu-mesh-validation-failure-reason=bus-bw-below-threshold --overwrite"
    # The diagnostic line should echo the inject value.
    assert_stdout_contains "threshold=9999"
}

# -------------------------------------------------------------------
# 8. rccl-crash: Failed=True + mpirun-exited marker -> =failed,
# reason=rccl-crash.
# -------------------------------------------------------------------

it "Failed + mpirun-exited marker -> =failed reason=rccl-crash" && {
    _reset_phase2_env
    pod="cvf-pod-node-d-crash"
    _seed_job_failed "node-d" "$ts" "$pod" "phase2-rccl-crash.log"
    run __phase2_run node-d
    assert_status 0
    assert_kubectl_call \
        "label node node-d amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-d amd.com/gpu-mesh-validation-failure-reason=rccl-crash --overwrite"
    assert_stdout_contains "FAIL reason=rccl-crash"
}

# -------------------------------------------------------------------
# 9. Failed=True with NO recognized marker in log -> default rccl-crash.
# Defends the "any non-zero exit is treated as a crash signal unless
# the validator explicitly flagged bw-below-threshold" contract from
# design §6.
# -------------------------------------------------------------------

it "Failed + no recognized marker -> default reason=rccl-crash" && {
    _reset_phase2_env
    pod="cvf-pod-node-e-nomark"
    _seed_job_failed "node-e" "$ts" "$pod" "phase2-failed-no-marker.log"
    run __phase2_run node-e
    assert_status 0
    assert_kubectl_call \
        "label node node-e amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-e amd.com/gpu-mesh-validation-failure-reason=rccl-crash --overwrite"
    assert_stdout_contains "default, no marker found"
}

# -------------------------------------------------------------------
# 10. Timeout: no Job conditions seeded + PHASE2_JOB_WAIT_TIME=0
# -> first iteration of the poll loop hits elapsed >= timeout
# immediately -> classified TIMEOUT, reason=timeout, and the
# hung Job is explicitly deleted at cleanup.
# -------------------------------------------------------------------

it "no conditions + PHASE2_JOB_WAIT_TIME=0 -> reason=timeout + cleanup delete" && {
    _reset_phase2_env
    export PHASE2_JOB_WAIT_TIME="0"
    _seed_job_pending  # no seeded Complete/Failed -> empty jsonpath responses
    run __phase2_run node-f
    assert_status 0
    assert_kubectl_call \
        "label node node-f amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-f amd.com/gpu-mesh-validation-failure-reason=timeout --overwrite"
    # Cleanup: hung job must be deleted.
    expected_job=$(_phase2_expected_job_name "node-f" "$ts")
    assert_kubectl_call \
        "delete job ${expected_job} --ignore-not-found=true --wait=false"
    assert_stdout_contains "TIMEOUT after 0s"
    assert_stdout_contains "deleting hung job"
}

# -------------------------------------------------------------------
# 11. Mixed pass/fail across multiple nodes: 3 nodes -- one pass,
# one bw-below-threshold, one rccl-crash. Verifies each node is
# labeled independently and the per-node measured-bw annotation
# fires for the two nodes whose logs contain an Avg bus bandwidth
# line.
# -------------------------------------------------------------------

it "mixed pass/fail across 3 nodes labels each node independently" && {
    _reset_phase2_env
    pod_a="cvf-pod-node-a-mix"
    pod_b="cvf-pod-node-b-mix"
    pod_c="cvf-pod-node-c-mix"
    _seed_job_complete "node-a" "$ts" "$pod_a" "phase2-pass.log"
    _seed_job_failed   "node-b" "$ts" "$pod_b" "phase2-bw-below-threshold.log"
    _seed_job_failed   "node-c" "$ts" "$pod_c" "phase2-rccl-crash.log"
    run __phase2_run node-a node-b node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/gpu-mesh-validation=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/gpu-mesh-validation-measured-bw=234.7 --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-mesh-validation-failure-reason=bus-bw-below-threshold --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-mesh-validation-measured-bw=173.7 --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-c amd.com/gpu-mesh-validation-failure-reason=rccl-crash --overwrite"
    assert_stdout_contains "passed=1 failed=2"
}

# -------------------------------------------------------------------
# 12. Parallel-submit: N input nodes -> exactly N `kubectl apply`
# invocations BEFORE any `kubectl get job` poll. Verifies no
# per-node serialization of submit+wait (mirror of PHASE1_SCRIPT
# contract).
# -------------------------------------------------------------------

it "parallel-submit: N input nodes -> N submits, all before any wait poll" && {
    _reset_phase2_env
    pod_a="cvf-pod-node-a-par"
    pod_b="cvf-pod-node-b-par"
    pod_c="cvf-pod-node-c-par"
    _seed_job_complete "node-a" "$ts" "$pod_a" "phase2-pass.log"
    _seed_job_complete "node-b" "$ts" "$pod_b" "phase2-pass.log"
    _seed_job_complete "node-c" "$ts" "$pod_c" "phase2-pass.log"
    run __phase2_run node-a node-b node-c
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
# 13. Job-creation failure: `kubectl apply` returns non-zero for the
# single input node -> node failed with reason=job-creation-failed,
# and NO wait/poll/log work for that job.
# -------------------------------------------------------------------

it "kubectl apply failure -> node failed with reason=job-creation-failed" && {
    _reset_phase2_env
    kubectl_mock_fail_sticky apply 1
    run __phase2_run node-z
    assert_status 0
    assert_kubectl_call \
        "label node node-z amd.com/gpu-mesh-validation=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-z amd.com/gpu-mesh-validation-failure-reason=job-creation-failed --overwrite"
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "submit-failed job must not be polled:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^logs " "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "submit-failed job must not fetch pod logs:
$(grep -E '^logs ' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "kubectl apply failed for job="
}

# -------------------------------------------------------------------
# 14. PHASE_NODES env-var fallback: when positional args are empty
# but PHASE_NODES is exported, the script uses that list.
# -------------------------------------------------------------------

it "PHASE_NODES env var is used when no positional args are given" && {
    _reset_phase2_env
    pod="cvf-pod-env-fallback"
    _seed_job_complete "node-env" "$ts" "$pod" "phase2-pass.log"
    export PHASE_NODES="node-env"
    run __phase2_run    # NB: no positional args
    assert_status 0
    assert_kubectl_call \
        "label node node-env amd.com/gpu-mesh-validation=passed --overwrite"
}

# -------------------------------------------------------------------
# 15. PHASE2_RCCL_ENV_VARS sanity: the ConfigMap value contains no
# IB/fabric-specific tunables (single-node intra-node test). This
# is a static-content check on the ConfigMap, not on PHASE2_SCRIPT
# behavior, but lives here because it's part of the test plan for
# this change (rccl-env-no-ib-vars, TC4 test plan).
# -------------------------------------------------------------------

it "PHASE2_RCCL_ENV_VARS contains no IB/fabric tunables (rccl-env-no-ib-vars)" && {
    rccl_env_body=$(extract_configmap_data "$CONFIGMAP" "PHASE2_RCCL_ENV_VARS")
    if [[ -z "$rccl_env_body" ]]; then
        _assert_fail "PHASE2_RCCL_ENV_VARS extraction produced empty output"
    fi
    # The check skips comment lines so the design-doc note about
    # "No NCCL_NET_PLUGIN -- single node, ." doesn't trip the grep.
    # Only non-comment env vars are checked against the forbidden
    # patterns.
    non_comment=$(echo "$rccl_env_body" | grep -v -E "^[[:space:]]*#")
    for forbidden in NCCL_NET_PLUGIN NCCL_IB_ IONIC_ NCCL_SOCKET_IFNAME; do
        if echo "$non_comment" | grep -q "$forbidden"; then
            _assert_fail "PHASE2_RCCL_ENV_VARS must not contain ${forbidden} (single-node, no fabric):
$non_comment"
        fi
    done
}

assert_summary
