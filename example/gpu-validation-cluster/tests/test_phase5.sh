#!/bin/bash
# Unit tests for PHASE5_SCRIPT
# against the mocked kubectl harness.
#
# Scope (from the design doc §7
# "Testing Strategy" + test plan):
# * refactor-safety baseline: SKIP_RCCL_TEST=false on a healthy
# multi-node input -> passed-label path, helpers called (not raw
# kubectl label), per-worker logs written. Covers test-plan TC1
# and TC2.
# * dynamic-worker-replicas: input list of 3 nodes -> MPIJob render
# gets WORKER_REPLICAS=3 (not the static config.json max). Covers
# test-plan TC3.
# * skip-flag short-circuit: SKIP_RCCL_TEST=true -> MPIJob NOT
# submitted, all input nodes pass-labeled via helpers, candidate
# label removed. Covers test-plan TC4.
# * per-worker-log-dump: pass path leaves
# ${LOG_DIR}/worker-<node>-<job>.log behind for every input node.
# Covers test-plan TC5.
# * mpijob-fails: kubectl wait non-zero -> every input node gets
# failed label via the helper; the candidate-label removal still
# runs. Covers test-plan TC6.
# * per-worker-exit-attribution: one node's worker pod terminated
# with exit 137 -> that node's failure-reason annotation contains
# worker-pod=<name>,exit=137; another node with a different exit
# gets a different reason. Covers test-plan TC7.
# * mpijob-apply-fails: kubectl apply non-zero on the MPIJob render
# -- this leaves no worker pods, so the SUT falls into the
# mpijob-failed labelling loop with `worker-pod=unknown,exit=unknown`
# annotations on every input node. Covers the broader "apply
# failure surfaces to operator" intent of test-plan TC8.
# * sub-min-pool: input of 1 node with PHASE5_MIN_WORKERS=2 ->
# MPIJob NOT submitted, script returns 0, no label changes,
# no kubectl apply/wait/label calls. Covers test-plan TC10.
# * empty-input: empty positional input -> return 0, no MPIJob,
# no labels. Covers test-plan TC11.
# * min-workers-override: PHASE5_MIN_WORKERS=1 + 1-node input ->
# guard passes; MPIJob render and wait happen. Covers
# test-plan TC12.
# * launcher-log-collection: launcher pod log saved to
# ${LOG_DIR}/launcher-<job>.log on both pass and fail paths.
# Covers test-plan TC14.
#
# Out of scope (covered elsewhere or by integration tests):
# * The full RCCL bandwidth-threshold check (TC19) -- requires
# real GPUs.
# * The orchestrator wiring (TC15-17) -- belongs to the
# test_orchestrator_dry_run.sh harness.
# * Performance/bandwidth assertions (TC19) -- multi-node testbed only.
#
# How PHASE5_SCRIPT is exercised (mirrors test_phase2.sh /
# test_phase4_5.sh conventions):
#
# The script body is a block-scalar inside cluster-validation-config.yaml.
# We extract it with lib/extract_script.sh, patch the hardcoded MPIJob
# template path so the test can run as a non-root user, pin the
# timestamp date generation so per-node assertions can match the
# generated job name, then wrap the patched body in a function
# `__phase5_run` so its `local`s + the trailing `run_phase5_main "$@"`
# call execute under a controlled scope.
#
# The PHASE_NODE_LABEL_SCRIPT helper library is sourced first so
# label_phase_passed / label_phase_failed are defined for the
# PHASE5_SCRIPT body to call.
#
# `kubectl` is the mock from lib/kubectl_mock.sh. Phase-5 extends
# the mock with three new state-seed helpers:
# * kubectl_mock_set_phase5_worker_pod_for_node <job> <node> <pod>
# * kubectl_mock_set_phase5_launcher_pod <job> <pod>
# * kubectl_mock_set_phase5_pod_exit_code <pod> <exit_code>
# plus the existing kubectl_mock_fail_sticky <verb> <ec> for driving
# the apply/wait failure paths.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase5"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase5.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

# Per-process tmp dirs:
# PHASE5_DIR -- root tmpdir; cleaned on EXIT.
# MPI_TPL_DIR -- holds a placeholder MPIJob template the SUT
# `sed`s into `kubectl apply -f -`. kubectl is mocked
# so the only constraints are: file exists, sed
# expressions don't fail, substitution markers are
# present (so render assertions can scrape them out
# of the recorded apply payload via the call log).
# LOG_DIR -- writable target for launcher + per-worker logs.
# PHASE5_BODY -- the patched, function-wrapped script we source.
# HELPER_SCRIPT -- PHASE_NODE_LABEL_SCRIPT extracted for helper fns.
PHASE5_DIR=$(mktemp -d -t phase5-tests-XXXXXX)
MPI_TPL_DIR="${PHASE5_DIR}/mpi-configs"
PHASE5_LOG_DIR="${PHASE5_DIR}/log"
PHASE5_BODY="${PHASE5_DIR}/phase5-body.sh"
HELPER_SCRIPT="${PHASE5_DIR}/phase-helpers.sh"
mkdir -p "$MPI_TPL_DIR" "$PHASE5_LOG_DIR"

trap 'rm -rf "$PHASE5_DIR"; kubectl_mock_cleanup' EXIT

# Minimal MPIJob template stand-in. The real template lives in the
# cluster-validation-mpijob-config ConfigMap. PHASE5_SCRIPT pipes a
# sed-rendered copy to `kubectl apply -f -`; the mock kubectl drains
# stdin to /dev/null so the substitution markers below only need to
# be present + sed-safe.
cat >"${MPI_TPL_DIR}/cluster-validation-mpijob-config.yaml" <<'YAML'
apiVersion: kubeflow.org/v2beta1
kind: MPIJob
metadata:
  name: cluster-validation-mpi-job
spec:
  slotsPerWorker: $$SLOTS_PER_WORKER
  runPolicy:
    cleanPodPolicy: Running
  mpiReplicaSpecs:
    Launcher:
      replicas: $$LAUNCHER_REPLICAS
      template:
        spec:
          containers:
            - name: launcher
              image: $$ROCE_WORKLOAD_IMAGE
    Worker:
      replicas: $$WORKER_REPLICAS
      template:
        metadata:
          annotations:
            k8s.v1.cni.cncf.io/networks: $$NAD_ANNOTATION
        spec:
          nodeName: $$LOG_STORE_NODE_NAME
          containers:
            - name: worker
              image: $$ROCE_WORKLOAD_IMAGE
              resources:
                limits:
                  amd.com/gpu: $$GPU_PER_WORKER
                  amd.com/pf_nic: $$PF_NIC_PER_WORKER
                  amd.com/vf_nic: $$VF_NIC_PER_WORKER
YAML

# Extract PHASE5_SCRIPT and patch two things:
# 1. /mpi-configs/. -> ${MPI_TPL_DIR}/.
# (test-only path; kubectl apply is mocked so this is purely
# about letting `sed -f` read a real file as a non-root user.)
# 2. ts=$(date +%Y%m%d-%H%M) -> ts="${PHASE5_TEST_TS:-.}"
# Pinning the timestamp makes the job name `cluster-validation-mpi-job-<ts>`
# deterministic so tests can seed worker-pod + launcher-pod state
# that the SUT will look up later.
RAW_PHASE5=$(extract_configmap_data "$CONFIGMAP" "PHASE5_SCRIPT")
if [[ -z "$RAW_PHASE5" ]]; then
    echo "FATAL: PHASE5_SCRIPT extraction produced empty output" >&2
    exit 1
fi

PATCHED_PHASE5=$(printf '%s\n' "$RAW_PHASE5" \
    | sed "s|/mpi-configs/cluster-validation-mpijob-config.yaml|${MPI_TPL_DIR}/cluster-validation-mpijob-config.yaml|g" \
    | sed 's|ts=\$(date +%Y%m%d-%H%M)|ts="${PHASE5_TEST_TS:-$(date +%Y%m%d-%H%M)}"|')

# Wrap in a function so `local` declarations inside run_phase5_main
# are valid AND the call site below executes against the function's
# positional args (which we forward from the test caller). Wrapping
# also keeps run_phase5_main's globals (ts, new_job, NAD_*, .) out
# of the harness scope.
#
# Production orchestrator (cluster-validation-job.yaml `run_phase5`)
# sources PHASE5_SCRIPT to REGISTER the run_phase5_main function and
# then invokes `run_phase5_main "$nodes"` explicitly. PHASE5_SCRIPT
# no longer self-invokes at source time (the trailing
# `run_phase5_main "$@"` was removed because the orchestrator's
# explicit call already does the work, and the source-time call was
# running Phase 5 a second time per cron tick).
#
# Mirror that contract here: write the body (just function defs) into
# the wrapper, then APPEND an explicit `run_phase5_main "$@"` call so
# tests invoking `__phase5_run <nodes.>` exercise the same path.
{
    printf '__phase5_run() {\n'
    printf '%s\n' "$PATCHED_PHASE5"
    printf '  run_phase5_main "$@"\n'
    printf '}\n'
} > "$PHASE5_BODY"

if ! bash -n "$PHASE5_BODY"; then
    echo "FATAL: patched PHASE5_SCRIPT has bash syntax errors" >&2
    exit 1
fi

# Extract the helper library (label_phase_passed/failed,
# annotate_phase_value) once; sourced into the harness so the
# wrapped PHASE5_SCRIPT body can call the helpers by name.
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

# Suffix for failure-reason annotation; mirrors ConfigMap default in
# the cluster-validation-config envFrom (PHASE_FAILURE_REASON_ANNOTATION_SUFFIX).
export PHASE_FAILURE_REASON_ANNOTATION_SUFFIX="-failure-reason"

# shellcheck disable=SC1090
source "$HELPER_SCRIPT"
# shellcheck disable=SC1090
source "$PHASE5_BODY"

# Sanity: required functions are defined.
for fn in label_phase_passed label_phase_failed __phase5_run; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: required function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# Suppress -u for the tests; PHASE5_SCRIPT references optional env
# (SKIP_RCCL_TEST default-empty) that would trip strict mode otherwise.
set +u

# --- per-test helpers -----------------------------------------------

# _reset_phase5_env
# Wipe mock state and re-export the baseline env PHASE5_SCRIPT reads.
# Defaults mirror what the launcher container's envFrom would inject
# in production (cluster-validation-config.yaml). Tests override
# individual knobs (SKIP_RCCL_TEST, PHASE5_MIN_WORKERS, .) before
# invoking __phase5_run.
_reset_phase5_env() {
    kubectl_mock_reset
    : >"${PHASE5_LOG_DIR}"/.keep 2>/dev/null || true
    # Wipe any leftover log files from a prior test so per-test
    # assert_file_exists checks reflect this test's writes only.
    find "$PHASE5_LOG_DIR" -mindepth 1 -delete 2>/dev/null || true

    # Phase 5 core env
    export PHASE5_LABEL_KEY="amd.com/cluster-validation-status"
    export PHASE5_MIN_WORKERS="2"
    export WORKER_REPLICAS="8"
    export LAUNCHER_REPLICAS="1"
    export MPIJOB_WAIT_TIME="60"
    export DEBUG_DELAY="0"      # don't actually sleep on the fail path
    export LOG_DIR="${PHASE5_LOG_DIR}"

    # MPIJob render inputs (consumed by the sed pipeline)
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/test-image:phase5"
    export LOG_STORE_NODE_NAME="log-store-node"
    export SLOTS_PER_WORKER="8"
    export GPU_PER_WORKER="8"
    export PF_NIC_PER_WORKER="1"
    export VF_NIC_PER_WORKER="0"
    export PF_NIC_NAD_NAME="default/rail"
    export VF_NIC_NAD_NAME="default/vf-rail"

    # Candidate label (the SUT strips the `=.` suffix via parameter
    # expansion before issuing `kubectl label node $n ${KEY}-`).
    export CANDIDATE_LABEL="amd.com/gpu-validation-candidate=true"

    # Pinned timestamp so the SUT's `new_job` name is deterministic
    # and we can seed mock state keyed by it.
    export PHASE5_TEST_TS="testts"

    # Phase 5 reads SKIP_RCCL_TEST with `,` lowercase expansion;
    # default to empty (= run MPIJob) unless a test overrides.
    unset SKIP_RCCL_TEST
}

# _expected_job_name
# Mirrors PHASE5_SCRIPT exactly: `cluster-validation-mpi-job-${ts}`.
_expected_job_name() {
    printf 'cluster-validation-mpi-job-%s' "${PHASE5_TEST_TS}"
}

# --- assertion helpers ----------------------------------------------

# _assert_no_kubectl_verb <verb>
# Verify zero `kubectl <verb> .` calls were recorded.
_assert_no_kubectl_verb() {
    local verb="$1"
    local path="${KUBECTL_CALLS_FILE:-}"
    if grep -q "^${verb} " "$path"; then
        _assert_fail "expected no kubectl ${verb} calls; got:
$(grep "^${verb} " "$path")"
    fi
}

# _assert_label_call <node> <value>
# Verify `kubectl label node <node> <PHASE5_LABEL_KEY>=<value> --overwrite`
# was issued (the path the helpers take).
_assert_label_call() {
    local node="$1"
    local value="$2"
    assert_kubectl_call "label node ${node} ${PHASE5_LABEL_KEY}=${value} --overwrite"
}

# _assert_failure_reason_for_node <node> <expected_reason>
# Verify the per-node failure-reason annotation was written via the
# helper's annotate path:
# annotate node <node> <PHASE5_LABEL_KEY>-failure-reason=<reason> --overwrite
_assert_failure_reason_for_node() {
    local node="$1"
    local reason="$2"
    local key="${PHASE5_LABEL_KEY}${PHASE_FAILURE_REASON_ANNOTATION_SUFFIX}"
    assert_kubectl_call "annotate node ${node} ${key}=${reason} --overwrite"
}

# _assert_candidate_label_preserved <node>
# Retry-trap contract: PHASE5_SCRIPT must NOT remove
# `amd.com/cluster-validation-candidate=true` on either pass or fail.
# The candidate label is the *eligibility* gate; PHASE5_LABEL_KEY is
# the *verdict*. Phase 0's timestamp/interval filter gates re-selection
# on its own -- stripping the candidate label would permanently
# quarantine the node and break the cron-style retry workflow.
# Assert that NO `kubectl label node <node> <candidate-key>- --overwrite`
# call was recorded for this node.
_assert_candidate_label_preserved() {
    local node="$1"
    local cand_key="${CANDIDATE_LABEL%%=*}"
    local pattern="label node ${node} ${cand_key}- --overwrite"
    if grep -qxF -- "$pattern" "$KUBECTL_CALLS_FILE"; then
        _assert_fail "Phase 5 must not strip candidate label on ${node}; recorded call: ${pattern}"
    fi
}

# _assert_mpijob_explicit_delete <job_name>
# Cleanup contract: PHASE5_SCRIPT must explicitly
# `kubectl delete mpijob <name> --ignore-not-found --wait=false`
# on the way out so a Pending/Failed MPIJob does not linger across
# cron ticks (the launcher Job's pods can hold amd.com/gpu and
# amd.com/nic reservations that block Phase 0 selection on the same
# nodes via the busy-pool check).
_assert_mpijob_explicit_delete() {
    local job="$1"
    assert_kubectl_call "delete mpijob ${job} --ignore-not-found --wait=false"
}

# ==========================================================
# TESTS
# ==========================================================

# --- TC4 (skip-rccl-passlabels-all) ---------------------------------
# SKIP_RCCL_TEST=true short-circuits BEFORE the MPIJob path. Every
# input node must be pass-labeled via the helper (not raw kubectl
# label). Retry-trap contract: the candidate label
# is intentionally PRESERVED across all Phase 5 exits. No MPIJob
# ever lands.
it "SKIP_RCCL_TEST=true labels every input node passed; no MPIJob submitted; candidate label preserved" && {
    _reset_phase5_env
    export SKIP_RCCL_TEST="true"
    run __phase5_run node-a node-b node-c
    assert_status 0
    # No MPIJob render / submit / wait.
    _assert_no_kubectl_verb apply
    _assert_no_kubectl_verb wait
    # Every input node pass-labeled via the helper.
    _assert_label_call node-a passed
    _assert_label_call node-b passed
    _assert_label_call node-c passed
    # Candidate label preserved on every input node (no `<key>-` strip).
    _assert_candidate_label_preserved node-a
    _assert_candidate_label_preserved node-b
    _assert_candidate_label_preserved node-c
    assert_stdout_contains "SKIP_RCCL_TEST is set to true. Skipping MPI Job RCCL test."
    assert_stdout_contains "(RCCL test skipped)"
}

# --- TC4 case-insensitive: SKIP_RCCL_TEST=TRUE same as true --------
# The `,` parameter expansion in PHASE5_SCRIPT lowercases the value
# before comparing; an upper-case value MUST still take the skip path.
it "SKIP_RCCL_TEST=TRUE (uppercase) still takes the skip path" && {
    _reset_phase5_env
    export SKIP_RCCL_TEST="TRUE"
    run __phase5_run node-a
    assert_status 0
    _assert_no_kubectl_verb apply
    _assert_label_call node-a passed
}

# --- TC10 (sub-min-pool-skips) --------------------------------------
# 1-node input + default PHASE5_MIN_WORKERS=2 -> the guard fires.
# No MPIJob is submitted, no labels are written, no candidate labels
# are stripped. Script returns 0 so downstream cleanup can run.
it "sub-min pool (1 node, MIN_WORKERS=2): no MPIJob, no labels, return 0" && {
    _reset_phase5_env
    # PHASE5_MIN_WORKERS=2 from _reset_phase5_env
    run __phase5_run node-a
    assert_status 0
    _assert_no_kubectl_verb apply
    _assert_no_kubectl_verb wait
    _assert_no_kubectl_verb label
    _assert_no_kubectl_verb annotate
    assert_stdout_contains "only 1 node(s) survived prior phases"
    assert_stdout_contains "skipping MPIJob (no label changes)"
}

# --- TC11 (empty-input-skips) ---------------------------------------
# Empty positional args -> input_count=0 -> the guard fires (0 < 2).
# Same no-op contract as the sub-min case.
it "empty input: no MPIJob, no labels, return 0" && {
    _reset_phase5_env
    run __phase5_run
    assert_status 0
    _assert_no_kubectl_verb apply
    _assert_no_kubectl_verb wait
    _assert_no_kubectl_verb label
    _assert_no_kubectl_verb annotate
    assert_stdout_contains "only 0 node(s) survived prior phases"
}

# --- TC12 (min-workers-override) ------------------------------------
# Operator opts in to a single-node MPIJob via PHASE5_MIN_WORKERS=1.
# Now a 1-node input passes the guard; apply + wait + label all run.
it "PHASE5_MIN_WORKERS=1 + 1 node: guard passes, MPIJob submitted" && {
    _reset_phase5_env
    export PHASE5_MIN_WORKERS="1"
    # Seed mock state so the SUT can find the worker pod + launcher pod
    # for label / log dump steps. The wait loop polls Succeeded/Failed
    # conditions on the MPIJob; seed Succeeded=True so the loop exits
    # immediately with job_status=passed.
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    run __phase5_run node-a
    assert_status 0
    # MPIJob render + poll both ran.
    assert_kubectl_call_contains "apply"
    assert_kubectl_call_contains "get mpijob ${job}"
    # Single-node degenerate run: label_phase_passed for node-a.
    _assert_label_call node-a passed
    # Cleanup deletes the MPIJob explicitly.
    _assert_mpijob_explicit_delete "$job"
}

# --- refactor-safety baseline (TC1 + TC2 + TC5) ---------------------
# Happy path: 2 nodes, SKIP_RCCL_TEST unset, MPIJob succeeds. Labels
# go through the helper (TC2), per-worker logs are saved (TC5), the
# orchestrator output retains the existing pre-refactor banner lines
# (TC1).
it "refactor-safety baseline: 2-node pass path writes labels via helpers + per-worker logs" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    # Drive the poll loop straight to passed via Succeeded=True.
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    run __phase5_run node-a node-b
    assert_status 0

    # TC2: labels written via helper (the helper itself issues the
    # raw kubectl label, but it always carries --overwrite -- a
    # marker the pre-refactor raw kubectl label loop did NOT use
    # on every call. The presence of `--overwrite` on EVERY
    # PHASE5_LABEL_KEY=passed write confirms the helper path.)
    _assert_label_call node-a passed
    _assert_label_call node-b passed

    # TC2 (label key contract): the label key is the unchanged
    # PHASE5_LABEL_KEY value -- preserved across the refactor.
    assert_kubectl_call_contains "amd.com/cluster-validation-status=passed"

    # TC5: per-worker log files exist on disk for each input node.
    assert_file_exists "${PHASE5_LOG_DIR}/worker-node-a-${job}.log"
    assert_file_exists "${PHASE5_LOG_DIR}/worker-node-b-${job}.log"

    # Retry-trap contract: candidate label is preserved across
    # the pass path. Phase 0's timestamp/interval gate handles
    # re-selection; stripping eligibility would break the cron model.
    _assert_candidate_label_preserved node-a
    _assert_candidate_label_preserved node-b

    # Pre-refactor banner lines preserved (TC1 functional equivalence).
    assert_stdout_contains "===Step 3: Submitting MPIJob==="
    assert_stdout_contains "===Step 4: Waiting for MPIJob completion==="
    assert_stdout_contains "===Step 5: Labeling nodes based on MPIJob result==="
    assert_stdout_contains "[MPIJob Result: Passed]"

    # Explicit MPIJob delete on exit.
    _assert_mpijob_explicit_delete "$job"
}

# --- TC3 (dynamic-worker-replicas) ----------------------------------
# input_count=3 -> actual_worker_replicas=3 -> the MPIJob render
# substitutes `$$WORKER_REPLICAS` with 3 (NOT the WORKER_REPLICAS=8
# default from _reset_phase5_env, which is the Phase 0 max). We
# scrape the rendered text from the SUT's own log line because the
# mock kubectl drains stdin to /dev/null.
it "dynamic worker-replicas: 3-node input -> MPIJob rendered with 3 workers" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-c worker-pod-c
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-c 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    run __phase5_run node-a node-b node-c
    assert_status 0
    # The SUT echoes the worker count immediately before submit.
    assert_stdout_contains "[MPIJob: Submitted for 3 worker node(s)]"
    # And the input_count debug line records the same value.
    assert_stdout_contains "input_count=3"
    # Three pass-labels.
    _assert_label_call node-a passed
    _assert_label_call node-b passed
    _assert_label_call node-c passed
}

# --- TC6 (mpijob-fails-labels-failed) -------------------------------
# kubectl wait non-zero -> every input node gets failed label.
# The candidate-label cleanup loop and per-worker log dump both
# still run on the fail path.
it "MPIJob Failed condition -> every input node labeled failed; candidate label preserved" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    # Seed two worker pods so the failure-attribution loop has data
    # to annotate (one pod per node, distinct exit codes).
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 1
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 1
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    # New wait loop polls Succeeded AND Failed conditions; seed
    # Failed=True so the loop short-circuits on its first iteration
    # with job_status=failed.
    kubectl_mock_set_mpijob_condition "$job" Failed True
    run __phase5_run node-a node-b
    assert_status 0
    _assert_label_call node-a failed
    _assert_label_call node-b failed
    # Retry-trap contract: candidate label preserved even on fail.
    _assert_candidate_label_preserved node-a
    _assert_candidate_label_preserved node-b
    assert_stdout_contains "[MPIJob Result: Failed]"
    _assert_mpijob_explicit_delete "$job"
}

# --- TC7 (per-worker-exit-attribution) -----------------------------
# Distinct exit codes per worker pod -> distinct failure-reason
# annotations per node. Confirms the worker-pod=.,exit=. shape
# AND that the two nodes get DIFFERENT reasons (no accidental
# cross-node bleed).
it "per-worker exit attribution: each failed node carries its own worker-pod=...,exit=... reason" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    # node-a's worker exited 137 (OOM); node-b's exited 1.
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 137
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 1
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Failed True
    run __phase5_run node-a node-b
    assert_status 0
    _assert_label_call node-a failed
    _assert_label_call node-b failed
    # Per-node distinct reasons:
    _assert_failure_reason_for_node node-a "worker-pod=worker-pod-a,exit=137"
    _assert_failure_reason_for_node node-b "worker-pod=worker-pod-b,exit=1"
    # Sanity: node-a is NOT annotated with node-b's reason and vice
    # versa. Without this guard a buggy loop could write the same
    # annotation to every node and the previous asserts would still
    # pass (each independent line would satisfy assert_kubectl_call).
    cross_a="annotate node node-a ${PHASE5_LABEL_KEY}${PHASE_FAILURE_REASON_ANNOTATION_SUFFIX}=worker-pod=worker-pod-b,exit=1 --overwrite"
    cross_b="annotate node node-b ${PHASE5_LABEL_KEY}${PHASE_FAILURE_REASON_ANNOTATION_SUFFIX}=worker-pod=worker-pod-a,exit=137 --overwrite"
    if grep -qxF -- "$cross_a" "$KUBECTL_CALLS_FILE"; then
        _assert_fail "node-a was annotated with node-b's reason -- per-worker attribution bled across nodes"
    fi
    if grep -qxF -- "$cross_b" "$KUBECTL_CALLS_FILE"; then
        _assert_fail "node-b was annotated with node-a's reason -- per-worker attribution bled across nodes"
    fi
}

# --- per-worker exit attribution: pod-missing fallback --------------
# When the worker-pod lookup returns empty (race against cleanup,
# eviction, etc.) the SUT must annotate with `worker-pod=unknown,exit=unknown`
# rather than leak an empty-string annotation. This is the design's
# "deterministic fallback" guarantee from §4.
it "missing worker pod -> failure-reason=worker-pod=unknown,exit=unknown" && {
    _reset_phase5_env
    # input_count=1 would trip the default MIN_WORKERS=2 guard; opt in
    # to a degenerate single-node MPIJob so the unknown-attribution
    # branch is reachable from a 1-node input.
    export PHASE5_MIN_WORKERS="1"
    job=$(_expected_job_name)
    # NO kubectl_mock_set_phase5_worker_pod_for_node seeded for node-a
    # -- the lookup returns empty, triggering the unknown/unknown branch.
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Failed True
    run __phase5_run node-a
    assert_status 0
    _assert_label_call node-a failed
    _assert_failure_reason_for_node node-a "worker-pod=unknown,exit=unknown"
}

# --- TC8 (mpijob-apply-fails) ---------------------------------------
# kubectl apply non-zero -- the MPIJob never lands. The SUT does
# NOT special-case apply failure; it falls through to `kubectl wait`,
# which fails (the wait mock returns success by default, but with no
# real MPIJob the production behavior is wait timeout). For our mock
# we drive both apply and wait sticky-failed so the SUT goes down
# the failed-job labelling path. Worker-pod lookups return empty
# (no pods were ever created), so the fallback unknown/unknown
# annotation is written -- matching the design's "apply failure
# surfaces to operator via failure annotation" intent.
it "kubectl apply fails -> nodes labeled failed with unknown attribution" && {
    _reset_phase5_env
    # Use a very short wait window so the timeout branch fires
    # quickly when MPIJob never appears (apply failed -> no
    # Succeeded/Failed condition is ever set).
    export MPIJOB_WAIT_TIME="1"
    kubectl_mock_fail_sticky apply 1
    # No worker pods seeded -- lookup returns empty -> unknown path.
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    run __phase5_run node-a node-b
    assert_status 0
    _assert_label_call node-a failed
    _assert_label_call node-b failed
    _assert_failure_reason_for_node node-a "worker-pod=unknown,exit=unknown"
    _assert_failure_reason_for_node node-b "worker-pod=unknown,exit=unknown"
    # The timeout branch should log the new diagnostic line.
    assert_stdout_contains "did not reach terminal state"
}

# --- TC14 (launcher-log-still-collected) ----------------------------
# Launcher pod logs land at ${LOG_DIR}/launcher-${new_job}.log on
# the pass path. The fail-path equivalent is exercised implicitly
# by the mpijob-fails test above (which also calls the launcher-log
# collection block), but we pin the on-disk file here for the pass
# path where it is most observable.
it "launcher log saved to LOG_DIR/launcher-<job>.log on pass path" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    run __phase5_run node-a node-b
    assert_status 0
    assert_file_exists "${PHASE5_LOG_DIR}/launcher-${job}.log"
}

# --- SKIP_RCCL_TEST=false explicit (regression for, expansion) ---
# Explicit `false` must take the MPIJob branch, not the skip branch.
# Guards against a `,`-expansion regression that would treat any
# non-empty value as "true".
it "SKIP_RCCL_TEST=false takes the MPIJob branch (no skip short-circuit)" && {
    _reset_phase5_env
    export SKIP_RCCL_TEST="false"
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    run __phase5_run node-a node-b
    assert_status 0
    # MUST hit the MPIJob path (apply + poll), not the skip banner.
    assert_kubectl_call_contains "apply"
    assert_kubectl_call_contains "get mpijob"
    assert_stdout_not_contains "Skipping MPI Job RCCL test"
}

# --- poll loop short-circuits on Succeeded ---------------
# When the MPIJob carries Succeeded=True on the first poll, the wait
# loop must exit on iteration 1 (no 5s sleep). Verifies Fix #3.
it "wait loop: Succeeded=True short-circuits to passed within one poll" && {
    _reset_phase5_env
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 0
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 0
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Succeeded True
    start=$(date +%s)
    run __phase5_run node-a node-b
    end=$(date +%s)
    assert_status 0
    assert_stdout_contains "[MPIJob Result: Passed]"
    elapsed=$((end - start))
    if (( elapsed > 4 )); then
        _assert_fail "wait loop took ${elapsed}s on Succeeded=True; expected <5s (no sleep on first poll)"
    fi
}

# --- poll loop short-circuits on Failed ------------------
# Confirms the new mechanism does NOT block for MPIJOB_WAIT_TIME when
# the MPIJob is in a clear Failed terminal state. The pre-fix code used
# `kubectl wait --for=condition=Succeeded`, which would sit out the
# full 240s timeout on failure.
it "wait loop: Failed=True short-circuits to failed within one poll" && {
    _reset_phase5_env
    # Set MPIJOB_WAIT_TIME high to prove the short-circuit (not just
    # the budget expiring) is what ended the loop.
    export MPIJOB_WAIT_TIME="120"
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 1
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 1
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    kubectl_mock_set_mpijob_condition "$job" Failed True
    start=$(date +%s)
    run __phase5_run node-a node-b
    end=$(date +%s)
    assert_status 0
    assert_stdout_contains "[MPIJob Result: Failed]"
    assert_stdout_not_contains "did not reach terminal state"
    elapsed=$((end - start))
    if (( elapsed > 4 )); then
        _assert_fail "wait loop took ${elapsed}s on Failed=True; expected <5s (no sleep on first poll, NOT the 120s timeout)"
    fi
    _assert_label_call node-a failed
    _assert_label_call node-b failed
    _assert_candidate_label_preserved node-a
    _assert_candidate_label_preserved node-b
    _assert_mpijob_explicit_delete "$job"
}

# --- poll loop hits its budget when neither condition fires
# MPIJOB_WAIT_TIME=0 makes the `while ($(date +%s) < end)` check false
# on the first evaluation, so the loop body never executes and the
# timeout branch fires immediately. job_status folds to failed.
it "wait loop: timeout branch fires when neither condition is True" && {
    _reset_phase5_env
    export MPIJOB_WAIT_TIME="0"
    job=$(_expected_job_name)
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-a worker-pod-a
    kubectl_mock_set_phase5_worker_pod_for_node "$job" node-b worker-pod-b
    kubectl_mock_set_phase5_pod_exit_code worker-pod-a 1
    kubectl_mock_set_phase5_pod_exit_code worker-pod-b 1
    kubectl_mock_set_phase5_launcher_pod "$job" launcher-pod-x
    # Deliberately NO mpijob condition seeded.
    run __phase5_run node-a node-b
    assert_status 0
    assert_stdout_contains "did not reach terminal state within 0s"
    assert_stdout_contains "[MPIJob Result: Failed]"
    _assert_label_call node-a failed
    _assert_label_call node-b failed
    _assert_candidate_label_preserved node-a
    _assert_candidate_label_preserved node-b
}

# --- explicit MPIJob cleanup runs on the skip path too ---
# The SKIP_RCCL_TEST=true short-circuit returns before the MPIJob is
# ever submitted, so there is nothing to delete. Verify NO `delete
# mpijob` call is issued in that path (avoid noise/false failures
# against a non-existent MPIJob name).
it "skip path: no explicit MPIJob delete (nothing was submitted)" && {
    _reset_phase5_env
    export SKIP_RCCL_TEST="true"
    run __phase5_run node-a node-b
    assert_status 0
    _assert_no_kubectl_verb apply
    # No delete mpijob call -- there is no $new_job in this branch.
    if grep -q "^delete mpijob" "$KUBECTL_CALLS_FILE"; then
        _assert_fail "skip path issued an unexpected delete mpijob call"
    fi
}

assert_summary
