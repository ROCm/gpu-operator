#!/bin/bash
# Unit tests for PHASE1_SCRIPT against
# the mocked kubectl harness and result.json fixtures. Supersedes the
#/764 single-Job-per-node design.
#
# Scope (matches the plan and the multi-stage contract
# documented at the top of PHASE1_SCRIPT in cluster-validation-config.yaml):
#
# Carry-over (still relevant under multi-stage):
# * empty input list (no-op pass)
# * single node pass (one stage)
# * single node fail (subtest fail)
# * mixed pass/fail across multiple nodes (one stage)
# * missing result.json -> failed with reason=test-runner-did-not-emit-results
# * recipe-not-found marker in result.json -> reason=recipe-not-found
# * SKIP_GPU_HW_ACCEPTANCE=true -> no Jobs, no CMs, pass-label all
# * parallel-submit: N input nodes -> N submissions before any wait
# * configmap-creation-failure -> reason=configmap-creation-failed
# * PHASE_NODES env fallback
#
# New:
# * missing required env var (GPU_VALIDATION_STAGES_JSON / GPU_PER_WORKER /
# PHASE1_LABEL_KEY) -> fail-fast every input node with reason
# phase1-missing-env:.
# * GPU_VALIDATION_STAGES_JSON empty array -> fail every node with reason
# phase1-stages-empty-or-invalid
# * GPU_VALIDATION_STAGES_JSON missing per-stage required field -> fail
# every node with reason phase1-stages-missing-fields:.
# * multi-stage all-pass: every stage emits its own per-stage annotation
# and the final aggregate label is =passed
# * multi-stage first-fails: failing stage records its annotation, the
# node is dropped, NO further stages submit Jobs/CMs for that node,
# failed-subtest reflects the failing stage's recipe, aggregate label
# is =failed
# * multi-stage cleanup: each stage deletes its Job + per-stage CM
#
# How PHASE1_SCRIPT is exercised:
#
# The script body is a block-scalar inside cluster-validation-config.yaml.
# We extract it with lib/extract_script.sh, then patch two hardcoded
# absolute paths so the test can run as a non-root user without
# touching /test-runner-configs or /var/log/cluster-validation:
# /test-runner-configs/cluster-validation-test-runner-job-config.yaml
# -> ${TPL_DIR}/cluster-validation-test-runner-job-config.yaml
# /var/log/cluster-validation
# -> ${RESULTS_ROOT} (a per-test tmpdir)
# The script uses `local` / `declare -A`, so we wrap the patched body
# in a function `__phase1_run` and invoke that. The helper library
# (PHASE_NODE_LABEL_SCRIPT) is sourced first so label_phase_passed /
# label_phase_failed / annotate_phase_value are defined.
#
# `kubectl` is the mock from lib/kubectl_mock.sh. Test-runner Job
# "completion" is simulated by seeding state via
# kubectl_mock_set_job_condition + kubectl_mock_set_pod_for_job, and
# by dropping a result.json under ${RESULTS_ROOT}/<pod>/result.json.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase1"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase1.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

PHASE1_DIR=$(mktemp -d -t phase1-tests-XXXXXX)
TPL_DIR="${PHASE1_DIR}/tpl"
RESULTS_ROOT="${PHASE1_DIR}/results"
PHASE1_BODY="${PHASE1_DIR}/phase1-body.sh"
HELPER_SCRIPT="${PHASE1_DIR}/phase-helpers.sh"
mkdir -p "$TPL_DIR" "$RESULTS_ROOT"

trap 'rm -rf "$PHASE1_DIR"; kubectl_mock_cleanup' EXIT

# Minimal job template stand-in. PHASE1_SCRIPT pipes a sed-rendered copy
# to `kubectl apply -f -`; the four sed placeholders are listed below.
# Since kubectl is mocked, the only constraints on contents are:
# * the file must exist (`[[ ! -f "$job_template" ]]` guard)
# * the sed expressions must not produce non-zero
cat >"${TPL_DIR}/cluster-validation-test-runner-job-config.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: cluster-validation-test-runner-job
spec:
  template:
    spec:
      nodeName: $$NODE
      containers:
        - name: test-runner
          image: $$TEST_RUNNER_IMAGE
          resources:
            limits:
              amd.com/gpu: $$GPU_PER_WORKER
          envFrom:
            - configMapRef:
                name: $$PHASE1_CONFIG_MAP
YAML

# Extract PHASE1_SCRIPT and patch the hardcoded paths so the test can
# run as a non-root user without /test-runner-configs or
# /var/log/cluster-validation existing. Also wrap the body in a function
# so `local` / `declare -A` work, and pin the timestamp the script puts
# into Job/CM names so seeded mock state always matches what the script
# looks up.
RAW_PHASE1=$(extract_configmap_data "$CONFIGMAP" "PHASE1_SCRIPT")
if [[ -z "$RAW_PHASE1" ]]; then
    echo "FATAL: PHASE1_SCRIPT extraction produced empty output" >&2
    exit 1
fi

PATCHED_PHASE1=$(printf '%s\n' "$RAW_PHASE1" \
    | sed "s|/test-runner-configs/cluster-validation-test-runner-job-config.yaml|${TPL_DIR}/cluster-validation-test-runner-job-config.yaml|g" \
    | sed "s|/var/log/cluster-validation|${RESULTS_ROOT}|g" \
    | sed 's|ts=\$(date +%Y%m%d-%H%M%S)|ts="${PHASE1_TEST_TS:-$(date +%Y%m%d-%H%M%S)}"|')

{
    printf '__phase1_run() {\n'
    printf '%s\n' "$PATCHED_PHASE1"
    printf '}\n'
} > "$PHASE1_BODY"

if ! bash -n "$PHASE1_BODY"; then
    echo "FATAL: patched PHASE1_SCRIPT has bash syntax errors" >&2
    exit 1
fi

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
source "$PHASE1_BODY"

for fn in label_phase_passed label_phase_failed annotate_phase_value __phase1_run; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: required function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# --- per-test helpers -----------------------------------------------

# Default single-stage config used by most tests. Single recipe so a
# pass/fail decision is a clean signal. Tests that need multi-stage
# override GPU_VALIDATION_STAGES_JSON after _reset_phase1_env.
#
# Stage Name is "gst-single" -- matches the deployed default. The Image
# value is irrelevant to the mock (used only for $$TEST_RUNNER_IMAGE
# sed substitution) but must be non-empty so the per-stage field
# validator does not flag it as missing.
_default_stages_json() {
    cat <<'JSON'
[{"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""}]
JSON
}

# Per-test reset: wipe the kubectl call log + seeded state, wipe any
# leftover result.json files, and re-export the baseline env. Tests
# override individual pieces (notably SKIP_GPU_HW_ACCEPTANCE and
# GPU_VALIDATION_STAGES_JSON) before calling __phase1_run.
_reset_phase1_env() {
    kubectl_mock_reset
    rm -rf "${RESULTS_ROOT:?}"/*
    export PHASE1_LABEL_KEY="amd.com/gpu-hw-acceptance"
    # TEST_RUNNER_IMAGE / TEST_RUNNER_JOB_WAIT_TIME /
    # GPU_VALIDATION_TESTS_JSON were removed. Each stage now carries its
    # own Image + TimeoutSeconds inside GPU_VALIDATION_STAGES_JSON.
    export GPU_VALIDATION_STAGES_JSON
    GPU_VALIDATION_STAGES_JSON="$(_default_stages_json)"
    export GPU_PER_WORKER="8"
    # Pin the timestamp PHASE1_SCRIPT puts into Job/CM names so seeded
    # state always matches what the script looks up.
    export PHASE1_TEST_TS="testts0001"
    # result-file discovery uses `find -newermt @stage_start`
    # to skip stale artifacts. Tests seed fixtures BEFORE invoking
    # __phase1_run, so pin stage_start to epoch=1 -- any current-time
    # mtime trivially passes the filter.
    export PHASE1_TEST_STAGE_START_EPOCH="1"
    unset SKIP_GPU_HW_ACCEPTANCE PHASE_NODES
}

# Seed a result fixture so PHASE1_SCRIPT's Step-3 file-finder picks it
# up. rocm/test-runner:v1.4.0 writes one artifact per
# iteration named <UTC-ts>_MANUAL_<recipe>_result(.gz). In production
# the file lands at the volume root. For multi-node unit tests where
# every node shares a single RESULTS_ROOT, we drop fixtures under a
# per-pod subdir so concurrent pods running the same recipe do not
# overwrite each other; the parser checks the per-pod subdir before
# the volume root.
_seed_result_json() {
    local fixture="$1" recipe="$2" pod_name="${3:-}"
    local ts_name="2026-01-01T00-00-00.000000Z"
    local dest_dir="${RESULTS_ROOT}"
    if [[ -n "$pod_name" ]]; then
        dest_dir="${RESULTS_ROOT}/${pod_name}"
        mkdir -p "$dest_dir"
    fi
    cp "${FIXTURES_DIR}/${fixture}" \
        "${dest_dir}/${ts_name}_MANUAL_${recipe}_result"
}

_phase1_now_ts() {
    printf '%s' "testts0001"
}

# Compute the 6-char SHA1 prefix PHASE1_SCRIPT uses for k8s names.
_phase1_node_hash() {
    echo -n "$1" | sha1sum | cut -c1-6
}

# Job name format:
# cvf-tr-${stage_name}-${node_hash}-${ts}
# Computed unconditionally (no length-conditional bypass), since the
# stage_name + ts contribution makes the 63-char ceiling marginal for
# any realistic node hostname.
_phase1_expected_job_name() {
    local node="$1" ts="$2" stage_name="$3"
    local h
    h=$(_phase1_node_hash "$node")
    printf '%s' "cvf-tr-${stage_name}-${h}-${ts}"
}

# ConfigMap name format:
# cvf-phase1-${stage_name}-${node_hash}-${ts}
_phase1_expected_cm_name() {
    local node="$1" ts="$2" stage_name="$3"
    local h
    h=$(_phase1_node_hash "$node")
    printf '%s' "cvf-phase1-${stage_name}-${h}-${ts}"
}

# Seed mock state so a (stage, node) Job completes with the given
# result.json fixture. Returns nothing; helpers compute deterministic
# Job/Pod names from the inputs. The recipe defaults to stage_name
# with hyphens converted to underscores (matches the canonical
# Name->Recipe mapping in the default stages config); pass an explicit
# 6th arg when the recipe differs from the stage Name.
_seed_job_pass() {
    local node="$1" ts="$2" pod="$3" fixture="$4" stage_name="${5:-gst-single}"
    local recipe="${6:-${stage_name//-/_}}"
    local job
    job=$(_phase1_expected_job_name "$node" "$ts" "$stage_name")
    kubectl_mock_set_job_condition "$job" "Complete" "True"
    kubectl_mock_set_pod_for_job   "$job" "$pod"
    _seed_result_json "$fixture" "$recipe" "$pod"
}

# Same as _seed_job_pass but no result.json file is dropped -- exercises
# the missing-result branch.
_seed_job_no_result() {
    local node="$1" ts="$2" pod="$3" stage_name="${4:-gst-single}"
    local job
    job=$(_phase1_expected_job_name "$node" "$ts" "$stage_name")
    kubectl_mock_set_job_condition "$job" "Complete" "True"
    kubectl_mock_set_pod_for_job   "$job" "$pod"
}

# Suppress the -u trap for tests that intentionally leave optional env
# vars unset (PHASE_NODES, SKIP_GPU_HW_ACCEPTANCE).
set +u

# -------------------------------------------------------------------
# 1. Empty input list -> no-op, exit 0, no kubectl side effects.
# -------------------------------------------------------------------

it "PHASE1_SCRIPT with empty input list is a no-op and returns 0" && {
    _reset_phase1_env
    run __phase1_run
    assert_status 0
    assert_kubectl_no_calls
    assert_stdout_contains "no input nodes -- nothing to do"
}

# -------------------------------------------------------------------
# 2. SKIP_GPU_HW_ACCEPTANCE=true -> every input node pass-labeled,
# NO Test Runner Job / ConfigMap created, no parsing entered.
# -------------------------------------------------------------------

it "SKIP_GPU_HW_ACCEPTANCE=true pass-labels every input node, no Jobs/CMs created" && {
    _reset_phase1_env
    export SKIP_GPU_HW_ACCEPTANCE="true"
    run __phase1_run node-a node-b node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/gpu-hw-acceptance=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/gpu-hw-acceptance=passed --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/gpu-hw-acceptance=passed --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP_GPU_HW_ACCEPTANCE=true must not submit any Jobs/CMs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^create( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP_GPU_HW_ACCEPTANCE=true must not create any CMs:
$(grep -E '^create( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^get job( |$|s)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP must not poll Jobs:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "SKIP_GPU_HW_ACCEPTANCE=true -- pass-labeling"
}

it "SKIP_GPU_HW_ACCEPTANCE accepts case-insensitive value (TRUE)" && {
    _reset_phase1_env
    export SKIP_GPU_HW_ACCEPTANCE="TRUE"
    run __phase1_run node-x
    assert_status 0
    assert_kubectl_call \
        "label node node-x amd.com/gpu-hw-acceptance=passed --overwrite"
}

# -------------------------------------------------------------------
# 3. Single node pass: Job Complete=True + result.json says all pass
# -> per-stage annotation =passed, aggregate label =passed, NO
# failed-subtest annotation. Verifies per-stage CM is created and
# cleaned up.
# -------------------------------------------------------------------

it "single node pass: per-stage annotation + aggregate label, no failure annotation" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod="cvf-pod-node-a-001"
    _seed_job_pass "node-a" "$ts" "$pod" "result-pass.json" "gst-single"
    cm_name=$(_phase1_expected_cm_name "node-a" "$ts" "gst-single")
    job_name=$(_phase1_expected_job_name "node-a" "$ts" "gst-single")
    run __phase1_run node-a
    assert_status 0
    # Per-stage annotation (=passed for gst-single).
    assert_kubectl_call \
        "annotate node node-a amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    # Aggregate label =passed.
    assert_kubectl_call \
        "label node node-a amd.com/gpu-hw-acceptance=passed --overwrite"
    # Per-stage ConfigMap was created (dry-run + apply pipeline).
    assert_kubectl_call_contains \
        "create configmap ${cm_name} --from-literal=GPU_VALIDATION_TESTS_JSON="
    # Cleanup: stage's Job + CM deleted.
    assert_kubectl_call \
        "delete job ${job_name} --ignore-not-found=true --wait=false"
    assert_kubectl_call \
        "delete configmap ${cm_name} --ignore-not-found=true --wait=false"
    # No failed label or failed-subtest annotation.
    if grep -F "node-a amd.com/gpu-hw-acceptance=failed" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass-path must not write the failed label:
$(grep failed "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "annotate node node-a amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "pass-path must not write a failed-subtest annotation:
$(grep failed-subtest "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "stage=gst-single done: passed=1 failed=0"
}

# -------------------------------------------------------------------
# 4. Single node fail: AGFHC hbm_lvl1 sub-test failed
# -> per-stage annotation =failed, aggregate label =failed,
# failure-reason includes stage prefix, failed-subtest annotation.
# -------------------------------------------------------------------

it "single node fail: hbm_lvl1 subtest -> stage annotation failed + aggregate failed" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod="cvf-pod-node-b-002"
    _seed_job_pass "node-b" "$ts" "$pod" "result-hbm-fail.json" "gst-single"
    run __phase1_run node-b
    assert_status 0
    # Per-stage annotation.
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    # Aggregate label.
    assert_kubectl_call \
        "label node node-b amd.com/gpu-hw-acceptance=failed --overwrite"
    # failure-reason now carries the stage prefix so a single
    # annotation pins which stage tripped.
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-hw-acceptance-failure-reason=stage-gst-single:subtest-failed:hbm_lvl1 --overwrite"
    # failed-subtest annotation (human-readable alias).
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-hw-acceptance-failed-subtest=hbm_lvl1 --overwrite"
    assert_stdout_contains "failed subtest=hbm_lvl1"
    assert_stdout_contains "stage=gst-single done: passed=0 failed=1"
}

# -------------------------------------------------------------------
# 5. Mixed pass/fail across 3 nodes (single stage): node-b fails on
# hbm_lvl1, node-a + node-c pass. Per-stage annotations + aggregate
# labels written independently.
# -------------------------------------------------------------------

it "mixed pass/fail across 3 nodes labels each node independently" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod_a="cvf-pod-node-a-mix"
    pod_b="cvf-pod-node-b-mix"
    pod_c="cvf-pod-node-c-mix"
    _seed_job_pass "node-a" "$ts" "$pod_a" "result-pass.json"     "gst-single"
    _seed_job_pass "node-b" "$ts" "$pod_b" "result-hbm-fail.json" "gst-single"
    _seed_job_pass "node-c" "$ts" "$pod_c" "result-pass.json"     "gst-single"
    run __phase1_run node-a node-b node-c
    assert_status 0
    # Per-stage annotations.
    assert_kubectl_call \
        "annotate node node-a amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-c amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    # Aggregate labels.
    assert_kubectl_call \
        "label node node-a amd.com/gpu-hw-acceptance=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/gpu-hw-acceptance=passed --overwrite"
    # failed-subtest on node-b only.
    assert_kubectl_call \
        "annotate node node-b amd.com/gpu-hw-acceptance-failed-subtest=hbm_lvl1 --overwrite"
    if grep -F "annotate node node-a amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "node-a (passed) must not get a failed-subtest annotation"
    fi
    if grep -F "annotate node node-c amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "node-c (passed) must not get a failed-subtest annotation"
    fi
    assert_stdout_contains "stage=gst-single done: passed=2 failed=1"
}

# -------------------------------------------------------------------
# 6. Missing result.json: Job condition is Complete=True (so the
# poll-wait loop exits successfully) but no result.json fixture is
# dropped -> stage failed with reason=test-runner-did-not-emit-results.
# -------------------------------------------------------------------

it "missing result.json -> failed with reason=test-runner-did-not-emit-results" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod="cvf-pod-node-d-noresult"
    _seed_job_no_result "node-d" "$ts" "$pod" "gst-single"
    run __phase1_run node-d
    assert_status 0
    assert_kubectl_call \
        "annotate node node-d amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    assert_kubectl_call \
        "label node node-d amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-d amd.com/gpu-hw-acceptance-failure-reason=stage-gst-single:test-runner-did-not-emit-results --overwrite"
    assert_kubectl_call \
        "annotate node node-d amd.com/gpu-hw-acceptance-failed-subtest=unknown --overwrite"
    assert_stderr_contains "MISSING result file"
}

# -------------------------------------------------------------------
# 7. recipe-not-found marker inside result.json -> stage failed with
# reason=recipe-not-found, failed-subtest=<recipe>.
# -------------------------------------------------------------------

it "recipe-not-found marker in result.json -> reason=recipe-not-found" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH; recipe-not-found path needs jq" >&2
        return 0
    fi
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod="cvf-pod-node-e-recipe"
    _seed_job_pass "node-e" "$ts" "$pod" "result-recipe-not-found.json" "gst-single"
    run __phase1_run node-e
    assert_status 0
    assert_kubectl_call \
        "annotate node node-e amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    assert_kubectl_call \
        "label node node-e amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-e amd.com/gpu-hw-acceptance-failure-reason=stage-gst-single:recipe-not-found --overwrite"
    assert_kubectl_call \
        "annotate node node-e amd.com/gpu-hw-acceptance-failed-subtest=xgmi_lvl1 --overwrite"
}

# -------------------------------------------------------------------
# 8. Parallel-submit: N input nodes -> exactly N (CM+Job) submissions
# BEFORE any `kubectl get job` poll. Per (stage, node) the script
# emits one `create configmap`, one apply for the CM, and one apply
# for the Job -- so 3 nodes * 1 stage -> 3 creates + 6 applies
# before any poll.
# -------------------------------------------------------------------

it "parallel-submit: N nodes -> N CMs + N Jobs submitted, all before any wait poll" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod_a="cvf-pod-node-a-par"
    pod_b="cvf-pod-node-b-par"
    pod_c="cvf-pod-node-c-par"
    _seed_job_pass "node-a" "$ts" "$pod_a" "result-pass.json" "gst-single"
    _seed_job_pass "node-b" "$ts" "$pod_b" "result-pass.json" "gst-single"
    _seed_job_pass "node-c" "$ts" "$pod_c" "result-pass.json" "gst-single"
    run __phase1_run node-a node-b node-c
    assert_status 0
    # 3 create-configmap calls.
    n_create=$(grep -cE "^create configmap " "$KUBECTL_CALLS_FILE" || true)
    assert_equals "3" "$n_create"
    # 6 apply calls (3 CM applies + 3 Job applies).
    n_apply=$(grep -cE "^apply( |$)" "$KUBECTL_CALLS_FILE" || true)
    assert_equals "6" "$n_apply"
    # All submits precede any `get job` poll.
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
# 9. ConfigMap-creation failure: `kubectl apply` returns non-zero
# sticky, so the CM apply in the create|apply pipeline fails first.
# -> stage failed with reason=configmap-creation-failed,
# failed-subtest=unknown; no Job apply for that node, no wait/parse.
# -------------------------------------------------------------------

it "kubectl apply failure -> node failed with reason=configmap-creation-failed" && {
    _reset_phase1_env
    # Sticky apply failure trips the CM create|apply pipeline FIRST
    # (CM apply runs before Job apply within the per-stage submit loop).
    kubectl_mock_fail_sticky apply 1
    run __phase1_run node-z
    assert_status 0
    # Per-stage annotation =failed and aggregate label =failed are
    # still attempted via the helper library, which itself fails because
    # `kubectl label` and `kubectl annotate` are not failure-injected
    # in this test -- only apply.
    assert_kubectl_call \
        "annotate node node-z amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    assert_kubectl_call \
        "label node node-z amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-z amd.com/gpu-hw-acceptance-failure-reason=stage-gst-single:configmap-creation-failed --overwrite"
    assert_kubectl_call \
        "annotate node node-z amd.com/gpu-hw-acceptance-failed-subtest=unknown --overwrite"
    # No `get job` poll (submit-failed entries skip the wait/parse phases).
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "submit-failed job must not be polled:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "failed to create configmap="
}

# -------------------------------------------------------------------
# 10. Missing required env var -> every input node labeled =failed with
# reason=phase1-missing-env:.; no Jobs/CMs submitted.
# -------------------------------------------------------------------

it "missing GPU_VALIDATION_STAGES_JSON -> all input nodes labeled failed, no submissions" && {
    _reset_phase1_env
    unset GPU_VALIDATION_STAGES_JSON
    run __phase1_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-missing-env:GPU_VALIDATION_STAGES_JSON"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-env path must not submit Jobs/CMs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^create( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-env path must not create CMs:
$(grep -E '^create( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "required env var(s) unset:"
}

it "missing GPU_PER_WORKER -> all input nodes labeled failed" && {
    _reset_phase1_env
    unset GPU_PER_WORKER
    run __phase1_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-missing-env:GPU_PER_WORKER"
}

# -------------------------------------------------------------------
# 10b. GPU_VALIDATION_STAGES_JSON empty array / invalid JSON / missing
# per-stage field. Each shape exercises a distinct fail-fast branch
# in the stages-validation block.
# -------------------------------------------------------------------

it "empty GPU_VALIDATION_STAGES_JSON array -> all nodes labeled failed (stages-empty-or-invalid)" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON="[]"
    run __phase1_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-stages-empty-or-invalid"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "empty-stages path must not submit Jobs/CMs"
    fi
}

it "invalid JSON in GPU_VALIDATION_STAGES_JSON -> all nodes labeled failed" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON="not-a-json {"
    run __phase1_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-stages-empty-or-invalid"
}

it "GPU_VALIDATION_STAGES_JSON missing per-stage required field -> all nodes labeled failed" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    # Stage with no `Image` field -- per-stage validator should reject.
    export GPU_VALIDATION_STAGES_JSON='[{"Name":"gst-single","Framework":"RVS","Recipe":"gst_single","TimeoutSeconds":60}]'
    run __phase1_run node-y
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-stages-missing-fields"
    # Reason should name the missing field for at least one stage.
    assert_kubectl_call_contains "stage[0].Image"
}

# -------------------------------------------------------------------
# 11. PHASE_NODES env-var fallback: when positional args are empty
# but PHASE_NODES is exported, the script uses that list.
# -------------------------------------------------------------------

it "PHASE_NODES env var is used when no positional args are given" && {
    _reset_phase1_env
    ts=$(_phase1_now_ts)
    pod="cvf-pod-env-fallback"
    _seed_job_pass "node-env" "$ts" "$pod" "result-pass.json" "gst-single"
    export PHASE_NODES="node-env"
    run __phase1_run    # NB: no positional args
    assert_status 0
    assert_kubectl_call \
        "label node node-env amd.com/gpu-hw-acceptance=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-env amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
}

# -------------------------------------------------------------------
# 12. Multi-stage all-pass: 2 stages, single node, both
# pass. Both per-stage annotations written, aggregate label
# =passed, no failed-subtest. Verifies stage-2 Job submission
# happens AFTER stage-1 completes.
# -------------------------------------------------------------------

it "multi-stage all-pass: both per-stage annotations + aggregate passed" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":""}
    ]'
    ts=$(_phase1_now_ts)
    pod1="cvf-pod-ms-pass-s1"
    pod2="cvf-pod-ms-pass-s2"
    _seed_job_pass "node-m" "$ts" "$pod1" "result-pass.json" "gst-single"
    _seed_job_pass "node-m" "$ts" "$pod2" "result-pass.json" "xgmi-lvl1"

    run __phase1_run node-m
    assert_status 0

    # Per-stage annotations for both stages.
    assert_kubectl_call \
        "annotate node node-m amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-m amd.com/gpu-hw-acceptance-stage-xgmi-lvl1=passed --overwrite"
    # Aggregate label =passed.
    assert_kubectl_call \
        "label node node-m amd.com/gpu-hw-acceptance=passed --overwrite"
    # No failed-subtest annotation on the pass path.
    if grep -F "annotate node node-m amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-pass multi-stage must not write failed-subtest annotation"
    fi
    # Stage-2 Job was actually submitted (one CM per stage * 1 node = 2 CMs).
    cm1=$(_phase1_expected_cm_name "node-m" "$ts" "gst-single")
    cm2=$(_phase1_expected_cm_name "node-m" "$ts" "xgmi-lvl1")
    assert_kubectl_call_contains "create configmap ${cm1} --from-literal="
    assert_kubectl_call_contains "create configmap ${cm2} --from-literal="
    # Stage-2 progress banner appears after stage-1.
    assert_stdout_contains "stage=gst-single done: passed=1 failed=0"
    assert_stdout_contains "stage=xgmi-lvl1 done: passed=1 failed=0"
}

# -------------------------------------------------------------------
# 13. Multi-stage stop-on-first-failure: stage 1 fails on
# hbm_lvl1; stage 2 MUST NOT submit a Job/CM for that node.
# Aggregate label =failed, failure-reason carries the first
# failing stage name.
# -------------------------------------------------------------------

it "multi-stage first-fails: stage-2 NOT submitted, aggregate failed" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":""}
    ]'
    ts=$(_phase1_now_ts)
    pod1="cvf-pod-ms-fail-s1"
    # Stage 1 returns hbm-fail; stage 2 would pass IF it ran. We seed
    # stage 2 too so a regression that DOES submit stage 2 wouldn't be
    # masked by a missing-result fallback -- the assertion explicitly
    # checks NO submission happened.
    _seed_job_pass "node-f" "$ts" "$pod1" "result-hbm-fail.json" "gst-single"

    run __phase1_run node-f
    assert_status 0

    # Stage 1 annotation =failed.
    assert_kubectl_call \
        "annotate node node-f amd.com/gpu-hw-acceptance-stage-gst-single=failed --overwrite"
    # Stage 2 annotation MUST NOT be written -- node was dropped before
    # stage 2's per-stage iteration submitted anything.
    if grep -F "amd.com/gpu-hw-acceptance-stage-xgmi-lvl1" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "stage-2 annotation must not be written when stage-1 failed:
$(grep xgmi-lvl1 "$KUBECTL_CALLS_FILE")"
    fi
    # Stage-2 Job/CM MUST NOT be submitted: no create configmap or apply
    # mentioning the stage-2 name.
    cm2=$(_phase1_expected_cm_name "node-f" "$ts" "xgmi-lvl1")
    job2=$(_phase1_expected_job_name "node-f" "$ts" "xgmi-lvl1")
    if grep -F "create configmap ${cm2}" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "stage-2 CM must not be created (stage-1 failed):
$(grep "${cm2}" "$KUBECTL_CALLS_FILE")"
    fi
    # Aggregate label =failed.
    assert_kubectl_call \
        "label node node-f amd.com/gpu-hw-acceptance=failed --overwrite"
    # failed-subtest reflects stage-1's recipe.
    assert_kubectl_call \
        "annotate node node-f amd.com/gpu-hw-acceptance-failed-subtest=hbm_lvl1 --overwrite"
    # failure-reason carries the first failing stage name.
    assert_kubectl_call \
        "annotate node node-f amd.com/gpu-hw-acceptance-failure-reason=stage-gst-single:subtest-failed:hbm_lvl1 --overwrite"
    # Skipped-stage progress banner.
    assert_stdout_contains "stage=xgmi-lvl1 skipped (no alive nodes left)"
}

# -------------------------------------------------------------------
# 14. Multi-stage cleanup: per-stage Job + per-stage CM are deleted
# after each stage. With 2 stages * 1 node we expect 2 delete-job
# and 2 delete-configmap calls (one per stage).
# -------------------------------------------------------------------

it "multi-stage cleanup: each stage deletes its Job and per-stage CM" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":""}
    ]'
    ts=$(_phase1_now_ts)
    pod1="cvf-pod-cleanup-s1"
    pod2="cvf-pod-cleanup-s2"
    _seed_job_pass "node-c1" "$ts" "$pod1" "result-pass.json" "gst-single"
    _seed_job_pass "node-c1" "$ts" "$pod2" "result-pass.json" "xgmi-lvl1"
    job1=$(_phase1_expected_job_name "node-c1" "$ts" "gst-single")
    job2=$(_phase1_expected_job_name "node-c1" "$ts" "xgmi-lvl1")
    cm1=$(_phase1_expected_cm_name "node-c1" "$ts" "gst-single")
    cm2=$(_phase1_expected_cm_name "node-c1" "$ts" "xgmi-lvl1")

    run __phase1_run node-c1
    assert_status 0

    # Per-stage Job + CM deletes for BOTH stages.
    assert_kubectl_call \
        "delete job ${job1} --ignore-not-found=true --wait=false"
    assert_kubectl_call \
        "delete configmap ${cm1} --ignore-not-found=true --wait=false"
    assert_kubectl_call \
        "delete job ${job2} --ignore-not-found=true --wait=false"
    assert_kubectl_call \
        "delete configmap ${cm2} --ignore-not-found=true --wait=false"

    # Exactly 2 delete-job and 2 delete-configmap calls (no leaks).
    n_del_job=$(grep -cE "^delete job " "$KUBECTL_CALLS_FILE" || true)
    n_del_cm=$(grep -cE "^delete configmap " "$KUBECTL_CALLS_FILE" || true)
    assert_equals "2" "$n_del_job"
    assert_equals "2" "$n_del_cm"
}

# -------------------------------------------------------------------
# 15. Skip-single-stage: middle stage Skip=true. Stage 0
# runs and passes. Stage 1 is annotated skipped without any Job
# submission. Stage 2 still runs and passes. Aggregate label
# =passed (the node had at least one non-skip stage that passed).
# -------------------------------------------------------------------

it "skip-single-stage: middle stage Skip=true -> annotation=skipped, others run, aggregate=passed" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":"","Skip":true},
      {"Name":"pcie-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"pcie_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":""}
    ]'
    ts=$(_phase1_now_ts)
    pod1="cvf-pod-skip-mid-s1"
    pod3="cvf-pod-skip-mid-s3"
    _seed_job_pass "node-sk" "$ts" "$pod1" "result-pass.json" "gst-single"
    _seed_job_pass "node-sk" "$ts" "$pod3" "result-pass.json" "pcie-lvl1"

    run __phase1_run node-sk
    assert_status 0

    # Stage 0 ran and passed -> annotation present.
    assert_kubectl_call \
        "annotate node node-sk amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    # Stage 1 was skipped -> annotation=skipped, NO Job/CM submitted.
    assert_kubectl_call \
        "annotate node node-sk amd.com/gpu-hw-acceptance-stage-xgmi-lvl1=skipped --overwrite"
    cm_skipped=$(_phase1_expected_cm_name "node-sk" "$ts" "xgmi-lvl1")
    if grep -F "create configmap ${cm_skipped}" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "skipped stage must not create configmap:
$(grep "${cm_skipped}" "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "delete configmap ${cm_skipped}" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "skipped stage must not delete configmap (it never created one):
$(grep "${cm_skipped}" "$KUBECTL_CALLS_FILE")"
    fi
    # Stage 2 still ran and passed -> annotation present.
    assert_kubectl_call \
        "annotate node node-sk amd.com/gpu-hw-acceptance-stage-pcie-lvl1=passed --overwrite"
    # Aggregate label =passed (>=1 non-skip stage was submitted+passed).
    assert_kubectl_call \
        "label node node-sk amd.com/gpu-hw-acceptance=passed --overwrite"
    # No failed-subtest annotation on this path.
    if grep -F "annotate node node-sk amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "skip-single-stage must not write failed-subtest annotation"
    fi
    assert_stdout_contains "stage=xgmi-lvl1 idx=1 SKIPPED (Skip=true)"
}

# -------------------------------------------------------------------
# 16. Skip-all-stages: every stage Skip=true. No Jobs
# submitted, no CMs created. Every stage annotated skipped.
# Aggregate label =skipped (third value, not passed).
# -------------------------------------------------------------------

it "skip-all-stages: all Skip=true -> no Jobs submitted, aggregate label=skipped" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":"","Skip":true},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":"","Skip":true}
    ]'

    run __phase1_run node-allsk
    assert_status 0

    # Every stage annotated skipped.
    assert_kubectl_call \
        "annotate node node-allsk amd.com/gpu-hw-acceptance-stage-gst-single=skipped --overwrite"
    assert_kubectl_call \
        "annotate node node-allsk amd.com/gpu-hw-acceptance-stage-xgmi-lvl1=skipped --overwrite"
    # Aggregate label =skipped (tri-state: not passed, not failed).
    assert_kubectl_call \
        "label node node-allsk amd.com/gpu-hw-acceptance=skipped --overwrite"
    # Must NOT label =passed or =failed.
    if grep -F "label node node-allsk amd.com/gpu-hw-acceptance=passed" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not write aggregate label=passed"
    fi
    if grep -F "label node node-allsk amd.com/gpu-hw-acceptance=failed" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not write aggregate label=failed"
    fi
    # No failed-subtest annotation on the all-skipped path.
    if grep -F "annotate node node-allsk amd.com/gpu-hw-acceptance-failed-subtest" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not write failed-subtest annotation"
    fi
    # No Job/CM submissions of any kind.
    if grep -E "^create configmap " "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not create configmaps:
$(grep -E '^create configmap ' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not apply Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    if grep -E "^get job" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-skipped path must not poll Jobs:
$(grep -E '^get job' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "passed=0 failed=0 skipped=1"
}

# -------------------------------------------------------------------
# 17. Skip-bad-type: non-boolean Skip ("true" string)
# must fail fast at validation, BEFORE any stage iteration runs.
# Every input node labeled =failed with the Skip-type reason.
# -------------------------------------------------------------------

it "skip-bad-type: non-boolean Skip -> fail-fast, no Job/CM submitted" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    # Quoted "true" must be rejected (boolean type check), even though
    # an accidentally-stringified value is a likely real-world mistake
    # since YAML/JSON conversion can produce strings.
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":"","Skip":"true"}
    ]'

    run __phase1_run node-bskip
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-bskip amd.com/gpu-hw-acceptance=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=phase1-stages-bad-skip-type"
    # Reason must name the offending stage index.
    assert_kubectl_call_contains "stage[0].Skip=true"
    # Fail-fast: no submissions of any kind happened.
    if grep -E "^create configmap " "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "bad-Skip-type path must not create configmaps"
    fi
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "bad-Skip-type path must not apply Jobs"
    fi
}

# -------------------------------------------------------------------
# 18. Skip+fail interleave: Skip stage between two real
# stages. Stage 0 passes, stage 1 is skipped, stage 2 fails.
# Skipped stage MUST NOT count toward the "first failing stage"
# bookkeeping -- aggregate failed-subtest comes from stage 2's
# recipe, not the skipped stage.
# -------------------------------------------------------------------

it "skip-then-fail: skipped stage does not poison failure-reason" && {
    if ! command -v jq >/dev/null 2>&1; then
        echo "      SKIP: jq not on PATH" >&2
        return 0
    fi
    _reset_phase1_env
    export GPU_VALIDATION_STAGES_JSON='[
      {"Name":"gst-single","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"RVS","Recipe":"gst_single","Iterations":1,"TimeoutSeconds":60,"Arguments":""},
      {"Name":"xgmi-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"xgmi_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":"","Skip":true},
      {"Name":"pcie-lvl1","Image":"docker.io/rocm/test-runner:v1.4.0","Framework":"AGFHC","Recipe":"pcie_lvl1","Iterations":1,"TimeoutSeconds":60,"Arguments":""}
    ]'
    ts=$(_phase1_now_ts)
    pod1="cvf-pod-skf-s1"
    pod3="cvf-pod-skf-s3"
    _seed_job_pass "node-skf" "$ts" "$pod1" "result-pass.json" "gst-single"
    # Stage 2's result is the hbm-fail fixture -- ensures the runner
    # returns subtest=hbm_lvl1 in the failure-reason annotation.
    _seed_job_pass "node-skf" "$ts" "$pod3" "result-hbm-fail.json" "pcie-lvl1"

    run __phase1_run node-skf
    assert_status 0

    # Stage 0 passed annotation.
    assert_kubectl_call \
        "annotate node node-skf amd.com/gpu-hw-acceptance-stage-gst-single=passed --overwrite"
    # Stage 1 skipped annotation.
    assert_kubectl_call \
        "annotate node node-skf amd.com/gpu-hw-acceptance-stage-xgmi-lvl1=skipped --overwrite"
    # Stage 2 failed annotation.
    assert_kubectl_call \
        "annotate node node-skf amd.com/gpu-hw-acceptance-stage-pcie-lvl1=failed --overwrite"
    # Aggregate label =failed.
    assert_kubectl_call \
        "label node node-skf amd.com/gpu-hw-acceptance=failed --overwrite"
    # failure-reason names stage 2 (the first real failure), NOT the
    # skipped stage 1.
    assert_kubectl_call_contains \
        "amd.com/gpu-hw-acceptance-failure-reason=stage-pcie-lvl1:subtest-failed:hbm_lvl1"
    # failed-subtest comes from stage 2's runner output.
    assert_kubectl_call \
        "annotate node node-skf amd.com/gpu-hw-acceptance-failed-subtest=hbm_lvl1 --overwrite"
}

assert_summary
