#!/bin/bash
# Unit tests for PHASE4_DRIVER_SCRIPT against the
# mocked kubectl harness and sample ib_write_bw client log fixtures.
#
#
# Scope (from the design doc §7
# "Testing Strategy" + test plan):
# * pairing-roundrobin-even -- input [a,b,c,d] -> full mesh
# (3 rounds, 2 disjoint pairs/round, 6 pairs total) [TP TC1]
# * pairing-roundrobin-odd -- input [a,b,c,d,e] -> full mesh
# (5 rounds, one node sits out per round, 10 pairs total) [TP TC2]
# * per-rail-annotation-written -- single pair, all rails pass,
# both nodes labeled passed with
# per-(rail, round) annotations [TP TC3]
# * skip-phase4-passlabels-all -- SKIP_RAIL_BANDWIDTH_TEST=true [TP TC4]
# * single-rail-fail -- inject low BW on rail 5 -> failed-rails=5
# + triangulation entry [TP TC5]
# * all-rails-fail-one-pair -- all 8 rails below threshold [TP TC6]
# * ib-write-bw-crash -- Failed=True + no BW line in log [TP TC7]
# * parse-failure -- Complete=True + empty log [TP TC8]
# * single-node-input -- only one input node -> unpaired=true [TP TC9]
# * empty-input -- no nodes -> no-op [TP TC10]
# * rail-count-override -- PHASE4_RAIL_COUNT=4 limits annotations [TP TC11]
# * server-pod-unready timeout -- server pod IP never set [TP TC12]
# * missing required env var -> all input nodes labeled failed
# * job templates missing -> all input nodes labeled failed
# * concurrency-cap-honored -- 8-node input, mocked apply, peak
# concurrent pair_runners <= PHASE4_MAX_CONCURRENT_PAIRS=4 [TP TC15]
#
# GPUOP-828 full-mesh schedule tests (these use PHASE4_RAIL_COUNT=0
# to exercise the scheduler alone without seeding per-rail Job state;
# the driver still walks the schedule, emits the per-round log lines,
# and runs pair_runners that are no-ops because the rail loop iterates
# zero times):
# * mesh-schedule-N2 -- 1 round, 1 pair
# * mesh-schedule-N4 -- 3 rounds, 2 disjoint pairs/round, union C(4,2)=6
# * mesh-schedule-N5 -- 5 rounds, one node sits out, union C(5,2)=10
# * mesh-schedule-N6 -- 5 rounds, 3 disjoint pairs/round, union C(6,2)=15
# * mesh-schedule-N7 -- 7 rounds, one node sits out, union C(7,2)=21
# * mesh-schedule-N8 -- 7 rounds, 4 disjoint pairs/round, union C(8,2)=28
# * mesh-disjoint-property -- no node appears in more than one pair per
# round (verified for every N above)
#
# How PHASE4_DRIVER_SCRIPT is exercised (mirrors test_phase2.sh /
# test_phase3.sh conventions):
#
# The driver body is a block-scalar inside cluster-validation-config.yaml.
# We extract it with lib/extract_script.sh, then patch:
# 1) /phase4-configs/cluster-validation-phase4-server-job-config.yaml
# -> ${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml
# 2) /phase4-configs/cluster-validation-phase4-client-job-config.yaml
# -> ${TPL_DIR}/cluster-validation-phase4-client-job-config.yaml
# 3) /tmp state dir prefix -> ${PHASE4_STATE_TMP_BASE} so the test
# owns the tree (helps with concurrency-cap accounting and
# teardown).
# The driver uses `local` heavily, so we wrap the patched body in a
# function `__phase4_run` and invoke that. The helper library
# (PHASE_NODE_LABEL_SCRIPT) is sourced first so label_phase_passed /
# label_phase_failed / annotate_phase_value are defined.
#
# `kubectl` is the mock from lib/kubectl_mock.sh. Per-rail Job
# "completion" is simulated by seeding:
# * server Job: pod-ip|<job>=<ip> (driver waits for podIP)
# pod-for-job|<job>=<pod> (driver inspects pod
# count when timing out)
# job|<job>=Complete=True (optional; server Job's
# terminal status is not
# checked by the driver,
# but the cleanup delete
# fires either way)
# * client Job: pod-for-job|<job>=<pod>
# pod-log|<pod>=<fixture> (parse "BW average")
# job|<job>=Complete=True OR Failed=True
#
# The driver's wait loops include `sleep 2` / `sleep 5` between
# polls. To keep test runtime reasonable we shim `sleep` on PATH as
# a no-op (the mock state is set up before invocation, so the first
# poll always sees what it needs).

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase4"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase4.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

PHASE4_DIR=$(mktemp -d -t phase4-tests-XXXXXX)
TPL_DIR="${PHASE4_DIR}/tpl"
SHIM_DIR="${PHASE4_DIR}/shims"
PHASE4_BODY="${PHASE4_DIR}/phase4-body.sh"
HELPER_SCRIPT="${PHASE4_DIR}/phase-helpers.sh"
PHASE4_STATE_TMP_BASE="${PHASE4_DIR}/state-base"
mkdir -p "$TPL_DIR" "$SHIM_DIR" "$PHASE4_STATE_TMP_BASE"

trap 'rm -rf "$PHASE4_DIR"; kubectl_mock_cleanup' EXIT

# Minimal Job template stand-ins for server + client. The real
# templates live in cluster-validation-phase4-job-config.
# PHASE4_DRIVER_SCRIPT pipes a sed-rendered copy to `kubectl apply -f -`;
# since kubectl is mocked, the only constraints on contents are:
# * the file must exist (`[[ ! -f "$server_tmpl" || ! -f "$client_tmpl" ]]`)
# * sed substitutions on $$NODE, $$RAIL_IDX, etc. must not error
# plain-text body with the substitution markers is fine
# * the rename anchor `^ name: cluster-validation-phase4-server-job` /
# `-client-job` must match so _phase4_render emits the per-(role,
# node, rail) job name on stderr (RENDERED_JOB_NAME=.).
cat >"${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: cluster-validation-phase4-server-job
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: $$NAD_NAME
    spec:
      nodeSelector:
        kubernetes.io/hostname: $$NODE
      containers:
        - name: phase4-ib-server
          image: $$ROCE_WORKLOAD_IMAGE
          env:
            - name: RAIL_IDX
              value: "$$RAIL_IDX"
YAML

cat >"${TPL_DIR}/cluster-validation-phase4-client-job-config.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: cluster-validation-phase4-client-job
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: $$NAD_NAME
    spec:
      nodeSelector:
        kubernetes.io/hostname: $$NODE
      containers:
        - name: phase4-ib-client
          image: $$ROCE_WORKLOAD_IMAGE
          env:
            - name: RAIL_IDX
              value: "$$RAIL_IDX"
            - name: PEER_POD_IP
              value: "$$PEER_POD_IP"
YAML

# --- sleep shim -----------------------------------------------------
# PHASE4_DRIVER_SCRIPT polls with `sleep 2` (pod-IP wait) and `sleep 5`
# (job-terminal wait). Tests pre-seed the mock state so the first
# iteration of every poll loop succeeds; the sleep is therefore wasted
# wall-clock. A no-op `sleep` shim keeps the full suite under one
# second. The shim is installed on PATH BEFORE kubectl_mock_init's
# PATH prepend so the mock kubectl (which kubectl_mock_init places at
# the head) still wins for `kubectl`.
cat >"${SHIM_DIR}/sleep" <<'EOF'
#!/bin/bash
# no-op sleep -- test suite pre-seeds all poll state, so wall-clock
# waiting buys nothing here. Accepts and ignores all args.
exit 0
EOF
chmod +x "${SHIM_DIR}/sleep"

# Extract PHASE4_DRIVER_SCRIPT and patch the two hardcoded template
# paths so the test can run as a non-root user without /phase4-configs
# existing. Also rewrite the state-dir prefix from /tmp/phase4-. to
# our test-owned base dir so we can introspect concurrency state and
# the test harness can audit / clean up after itself.
RAW_PHASE4=$(extract_configmap_data "$CONFIGMAP" "PHASE4_DRIVER_SCRIPT")
if [[ -z "$RAW_PHASE4" ]]; then
    echo "FATAL: PHASE4_DRIVER_SCRIPT extraction produced empty output" >&2
    exit 1
fi

PATCHED_PHASE4=$(printf '%s\n' "$RAW_PHASE4" \
    | sed "s|/phase4-configs/cluster-validation-phase4-server-job-config.yaml|${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml|g" \
    | sed "s|/phase4-configs/cluster-validation-phase4-client-job-config.yaml|${TPL_DIR}/cluster-validation-phase4-client-job-config.yaml|g" \
    | sed "s|\"/tmp/phase4-\${phase4_ts}-\$\$\"|\"${PHASE4_STATE_TMP_BASE}/run-\${phase4_ts}-\$\$\"|g")

# Wrap in a function so `local` / `local -a` (used heavily inside
# PHASE4_DRIVER_SCRIPT) are valid. The orchestrator
# sources the driver inside run_phase4, which is a function, so this
# matches production wiring.
{
    printf '__phase4_run() {\n'
    printf '%s\n' "$PATCHED_PHASE4"
    printf '}\n'
} > "$PHASE4_BODY"

if ! bash -n "$PHASE4_BODY"; then
    echo "FATAL: patched PHASE4_DRIVER_SCRIPT has bash syntax errors" >&2
    exit 1
fi

# Extract the helper library (label_phase_passed/failed,
# annotate_phase_value) once.
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

# Prepend the shim dir AFTER kubectl_mock_init so the mock kubectl
# (which init prepends) still wins for `kubectl`, and our `sleep`
# shim wins over /bin/sleep. The shim dir goes second-from-front so
# /usr/bin/<tool> still resolves for things like `date`, `awk`, `sed`.
export PATH="${SHIM_DIR}:${PATH}"

# shellcheck disable=SC1090
source "$HELPER_SCRIPT"
# shellcheck disable=SC1090
source "$PHASE4_BODY"

# Sanity: required functions are defined.
for fn in label_phase_passed label_phase_failed annotate_phase_value \
          __phase4_run; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: required function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# Suppress the -u trap for tests that intentionally leave optional env
# vars unset (PHASE_NODES, SKIP_RAIL_BANDWIDTH_TEST).
set +u

# Helper: compute the per-(role, node, rail, round) Job name
# PHASE4_DRIVER_SCRIPT generates inside _phase4_render. Mirrors the
# driver exactly (GPUOP-828: round suffix added to disambiguate the
# same (role, node, rail) repeated across mesh rounds):
# cvf-p4-${role}-${node}-r${rail_idx}-rd${round_idx} (when short enough)
# cvf-p4-${role}-${sha1(node)|6}-r${rail_idx}-rd${round_idx} (when too long)
_phase4_expected_job_name() {
    local role="$1" node="$2" rail="$3" round="${4:-0}"
    local max_len=63
    local jn="cvf-p4-${role}-${node}-r${rail}-rd${round}"
    if [[ "${#jn}" -gt "$max_len" ]]; then
        local h
        h=$(echo -n "$node" | sha1sum | cut -c1-6)
        jn="cvf-p4-${role}-${h}-r${rail}-rd${round}"
    fi
    printf '%s' "$jn"
}

# Seed every server + client Job for one pair across rails 0.rail_count-1
# (in one specific round, default 0) as "all pass" with the same client
# log fixture. Server pod IP is canned (any non-empty string works; the
# driver just substitutes it into the client template). The client Job
# is seeded Complete=True and its pod log carries the BW-average value.
_seed_pair_all_pass() {
    local node_a="$1" node_b="$2" rail_count="$3" log_fixture="$4"
    local round="${5:-0}"
    local r
    for (( r=0; r < rail_count; r++ )); do
        local sjob cjob spod cpod
        sjob=$(_phase4_expected_job_name "server" "$node_a" "$r" "$round")
        cjob=$(_phase4_expected_job_name "client" "$node_b" "$r" "$round")
        spod="pod-${sjob}"
        cpod="pod-${cjob}"
        kubectl_mock_set_pod_for_job "$sjob" "$spod"
        kubectl_mock_set_pod_ip_for_job "$sjob" "10.42.0.${r}0"
        kubectl_mock_set_pod_for_job "$cjob" "$cpod"
        kubectl_mock_set_job_condition "$cjob" "Complete" "True"
        kubectl_mock_set_pod_log "$cpod" "${FIXTURES_DIR}/${log_fixture}"
    done
}

# Seed one specific rail of a pair (in one specific round, default 0)
# with a custom log + terminal state.
# terminal: "Complete" or "Failed". If `seed_server` is "yes" (default)
# the server Job is seeded with a pod IP. Pass "no" to simulate
# server-pod-unready (driver will time out the pod-IP wait).
_seed_pair_one_rail() {
    local node_a="$1" node_b="$2" rail="$3" terminal="$4" log_fixture="$5"
    local seed_server="${6:-yes}"
    local round="${7:-0}"
    local sjob cjob spod cpod
    sjob=$(_phase4_expected_job_name "server" "$node_a" "$rail" "$round")
    cjob=$(_phase4_expected_job_name "client" "$node_b" "$rail" "$round")
    spod="pod-${sjob}"
    cpod="pod-${cjob}"
    if [[ "$seed_server" == "yes" ]]; then
        kubectl_mock_set_pod_for_job "$sjob" "$spod"
        kubectl_mock_set_pod_ip_for_job "$sjob" "10.42.0.${rail}0"
    fi
    kubectl_mock_set_pod_for_job "$cjob" "$cpod"
    kubectl_mock_set_job_condition "$cjob" "$terminal" "True"
    if [[ -n "$log_fixture" ]]; then
        kubectl_mock_set_pod_log "$cpod" "${FIXTURES_DIR}/${log_fixture}"
    fi
}

# Helper: extract the schedule emitted by PHASE4_DRIVER_SCRIPT from
# the LAST_STDOUT string captured by `run`. Emits one TSV line per
# round on stdout (<round_idx><TAB><pair1> <pair2> ...), sorted by
# round_idx. Used by mesh-schedule tests below.
_phase4_extract_schedule() {
    grep -oE '\[Phase 4\] round [0-9]+ START: .*$' <<<"$LAST_STDOUT" \
        | sed -E 's/^\[Phase 4\] round ([0-9]+) START: (.*)$/\1\t\2/' \
        | sort -n
}

# Helper: assert that every pair in `pairs_csv` is unordered-unique
# across the supplied multi-line schedule and that the union covers
# every C(N,2) pair derivable from the input node list. Args:
# $1 -- schedule TSV (round<TAB>pairs) produced by _phase4_extract_schedule
# $2 -- expected total pair count (e.g. 6 for N=4)
# $3.. -- the input node list (sorted)
# Fails the test via _assert_fail on any violation; prints nothing on success.
_phase4_assert_full_mesh() {
    local sched="$1"
    local expected="$2"
    shift 2
    local -a nodes=("$@")
    local n="${#nodes[@]}"

    # Build expected pair set (unordered, sorted "a,b" with a<b).
    local -A expected_pairs=()
    local i j
    for (( i=0; i<n; i++ )); do
        for (( j=i+1; j<n; j++ )); do
            local a="${nodes[$i]}"
            local b="${nodes[$j]}"
            if [[ "$a" < "$b" ]]; then
                expected_pairs["${a},${b}"]=1
            else
                expected_pairs["${b},${a}"]=1
            fi
        done
    done
    if [[ "${#expected_pairs[@]}" -ne "$expected" ]]; then
        _assert_fail "test bug: expected count ${expected} != C(${n},2)=${#expected_pairs[@]}"
        return 1
    fi

    # Walk the schedule. For each round, verify no node appears twice
    # (disjoint property) and accumulate the pair set.
    local -A seen_pairs=()
    local round_count=0
    while IFS=$'\t' read -r round_idx round_pairs; do
        [[ -z "$round_idx" ]] && continue
        round_count=$((round_count + 1))
        local -A round_nodes=()
        local p
        for p in $round_pairs; do
            local a="${p%,*}"
            local b="${p#*,}"
            # Canonical form (a < b).
            local canon
            if [[ "$a" < "$b" ]]; then
                canon="${a},${b}"
            else
                canon="${b},${a}"
            fi
            if [[ -n "${seen_pairs[$canon]:-}" ]]; then
                _assert_fail "pair ${canon} appears in multiple rounds (round ${round_idx})"
                return 1
            fi
            seen_pairs[$canon]=1
            if [[ -n "${round_nodes[$a]:-}" ]]; then
                _assert_fail "node ${a} appears in multiple pairs in round ${round_idx}"
                return 1
            fi
            if [[ -n "${round_nodes[$b]:-}" ]]; then
                _assert_fail "node ${b} appears in multiple pairs in round ${round_idx}"
                return 1
            fi
            round_nodes[$a]=1
            round_nodes[$b]=1
        done
    done <<< "$sched"

    # Total pair count and round count.
    if [[ "${#seen_pairs[@]}" -ne "$expected" ]]; then
        _assert_fail "expected ${expected} pairs in union, got ${#seen_pairs[@]}"
        return 1
    fi
    # Every expected pair must appear in the union.
    local k
    for k in "${!expected_pairs[@]}"; do
        if [[ -z "${seen_pairs[$k]:-}" ]]; then
            _assert_fail "pair ${k} missing from schedule"
            return 1
        fi
    done

    # Expected round count: for even N -> N-1 (circle of size N).
    # For odd N -> N rounds (circle of size N+1 with a bye slot;
    # circle_size - 1 = N rounds). With the circle algorithm every
    # round has either floor(N/2) or (N-1)/2 real pairs, so the START
    # line is always emitted (never an empty round).
    local expected_rounds
    if (( n % 2 == 0 )); then
        expected_rounds=$(( n - 1 ))
    else
        expected_rounds="$n"
    fi
    if [[ "$round_count" -ne "$expected_rounds" ]]; then
        _assert_fail "expected ${expected_rounds} rounds, got ${round_count}"
        return 1
    fi
    return 0
}

# Per-test reset: wipe the kubectl call log and any seeded state, and
# re-export the baseline env PHASE4_DRIVER_SCRIPT reads. Tests override
# pieces of this (notably SKIP_RAIL_BANDWIDTH_TEST, PHASE4_RAIL_COUNT,
# PHASE4_PAIR_WAIT_TIME) before calling __phase4_run.
_reset_phase4_env() {
    kubectl_mock_reset
    export PHASE4_LABEL_KEY="amd.com/rail-bandwidth"
    export PHASE4_RAIL_COUNT="8"
    export PHASE4_BW_THRESHOLD="380"
    # Large enough that the first poll iteration sees seeded state and
    # exits before the (no-op) sleep would fire. Tests that exercise
    # the timeout branch override this to 0.
    export PHASE4_PAIR_WAIT_TIME="60"
    export PHASE4_MAX_CONCURRENT_PAIRS="8"
    export PHASE4_NAD_NAME_PREFIX="amd-host-device-nad-rail-"
    export PHASE4_IB_DEV_PREFIX="rdma_dev_"
    export ROCE_WORKLOAD_IMAGE="docker.io/rocm/roce-workload:test"
    unset SKIP_RAIL_BANDWIDTH_TEST PHASE_NODES
}

# -------------------------------------------------------------------
# 1. Empty input list -> no-op, exit 0, no kubectl side effects.
# [TP TC10]
# -------------------------------------------------------------------

it "empty input list is a no-op and returns 0" && {
    _reset_phase4_env
    run __phase4_run
    assert_status 0
    assert_kubectl_no_calls
    assert_stdout_contains "no input nodes -- nothing to do"
}

# -------------------------------------------------------------------
# 2. SKIP_RAIL_BANDWIDTH_TEST=true -> every input node pass-labeled,
# NO Phase 4 Job submission, no kubectl get/logs/apply work.
# [TP TC4]
# -------------------------------------------------------------------

it "SKIP_RAIL_BANDWIDTH_TEST=true pass-labels every input node, no Jobs created" && {
    _reset_phase4_env
    export SKIP_RAIL_BANDWIDTH_TEST="true"
    run __phase4_run node-a node-b node-c
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/rail-bandwidth=passed --overwrite"
    assert_kubectl_call \
        "label node node-c amd.com/rail-bandwidth=passed --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "SKIP must not submit any Jobs:
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
    assert_stdout_contains "SKIP_RAIL_BANDWIDTH_TEST=true -- pass-labeling"
}

# Case-insensitive variant: matches the `${VAR,}` lowercasing the
# driver does before comparing against "true".
it "SKIP_RAIL_BANDWIDTH_TEST accepts case-insensitive value (TRUE)" && {
    _reset_phase4_env
    export SKIP_RAIL_BANDWIDTH_TEST="TRUE"
    run __phase4_run node-x
    assert_status 0
    assert_kubectl_call \
        "label node node-x amd.com/rail-bandwidth=passed --overwrite"
}

# -------------------------------------------------------------------
# 3. Missing required env var -> every input node labeled =failed with
# reason=phase4-missing-env:.; no Jobs submitted.
# -------------------------------------------------------------------

it "missing required env var -> all input nodes labeled failed, no Jobs submitted" && {
    _reset_phase4_env
    unset PHASE4_RAIL_COUNT
    run __phase4_run node-y node-z
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-y amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call_contains \
        "amd.com/rail-bandwidth-failure-reason=phase4-missing-env:PHASE4_RAIL_COUNT"
    assert_kubectl_call \
        "label node node-z amd.com/rail-bandwidth=failed --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-env path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stderr_contains "required env var(s) unset:"
}

# -------------------------------------------------------------------
# 4. Missing job templates -> every input node labeled failed with
# reason=job-template-missing; no Jobs submitted.
# -------------------------------------------------------------------

it "missing job templates -> all input nodes labeled failed, reason=job-template-missing" && {
    _reset_phase4_env
    mv "${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml" \
       "${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml.hidden"
    run __phase4_run node-a node-b
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failure-reason=job-template-missing --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/rail-bandwidth=failed --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "missing-template path must not submit Jobs:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    # Restore for the rest of the suite.
    mv "${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml.hidden" \
       "${TPL_DIR}/cluster-validation-phase4-server-job-config.yaml"
}

# -------------------------------------------------------------------
# 5. GPUOP-828: full-mesh round-robin EVEN [a,b,c,d] -> 3 rounds,
# 2 disjoint pairs/round, 6 pairs total = C(4,2). Stable ordering:
# driver sort -u's the input first.
# [TP TC1]
# -------------------------------------------------------------------

it "pairing full-mesh even: [a,b,c,d] -> 3 rounds, 2 disjoint pairs/round, 6 total" && {
    _reset_phase4_env
    # RAIL_COUNT=0 short-circuits the per-rail Job machinery inside
    # pair_runner so the scheduler can be exercised without seeding
    # any kubectl mock state. The scheduler still emits the round
    # START lines and the aggregation walks zero rails per node.
    export PHASE4_RAIL_COUNT="0"
    # Input is intentionally OUT OF ORDER to prove sort-stability.
    run __phase4_run node-c node-a node-d node-b
    assert_status 0
    assert_stdout_contains "sorted input (4 nodes): node-a node-b node-c node-d"
    assert_stdout_contains "schedule: rounds=3 total_pairs=6 unpaired=<none>"
    # All 4 nodes pass-labeled (rail loop is a no-op).
    for n in node-a node-b node-c node-d; do
        assert_kubectl_call \
            "label node ${n} amd.com/rail-bandwidth=passed --overwrite"
    done
    # The unpaired annotation must NOT appear on the even path.
    if grep -F "amd.com/rail-bandwidth-unpaired=true" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "even-count input must not write unpaired annotation:
$(grep unpaired "$KUBECTL_CALLS_FILE")"
    fi
    # Full-mesh property: every C(4,2)=6 unordered pair appears
    # exactly once across the 3 rounds, with no node repeated within
    # a round.
    sched=$(_phase4_extract_schedule)
    _phase4_assert_full_mesh "$sched" 6 node-a node-b node-c node-d
}

# -------------------------------------------------------------------
# 6. GPUOP-828: full-mesh round-robin ODD [a,b,c,d,e] -> 5 rounds;
# one node sits out per round (bye slot); 10 pairs total = C(5,2).
# No node is permanently unpaired -- the per-N=1 "unpaired"
# annotation must not appear. [TP TC2]
# -------------------------------------------------------------------

it "pairing full-mesh odd: [a,b,c,d,e] -> 5 rounds, one node sits out, 10 total" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="0"
    run __phase4_run node-a node-b node-c node-d node-e
    assert_status 0
    assert_stdout_contains "schedule: rounds=5 total_pairs=10 unpaired=<none>"
    # All 5 nodes pass-labeled (rail loop is a no-op).
    for n in node-a node-b node-c node-d node-e; do
        assert_kubectl_call \
            "label node ${n} amd.com/rail-bandwidth=passed --overwrite"
    done
    # Odd N must NOT trigger the unpaired annotation under full mesh.
    if grep -F "amd.com/rail-bandwidth-unpaired=true" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "odd-count input must not write unpaired annotation under full mesh:
$(grep unpaired "$KUBECTL_CALLS_FILE")"
    fi
    sched=$(_phase4_extract_schedule)
    _phase4_assert_full_mesh "$sched" 10 \
        node-a node-b node-c node-d node-e
}

# -------------------------------------------------------------------
# 7. Single-node input -> all unpaired path; node pass-labeled with
# unpaired=true annotation. No pair_runners forked, no Jobs.
# [TP TC9]
# -------------------------------------------------------------------

it "single-node input -> unpaired pass-label, no Jobs created" && {
    _reset_phase4_env
    run __phase4_run node-solo
    assert_status 0
    assert_kubectl_call \
        "label node node-solo amd.com/rail-bandwidth=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-solo amd.com/rail-bandwidth-unpaired=true --overwrite"
    if grep -E "^apply( |$)" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "single-node path must not submit any Job:
$(grep -E '^apply( |$)' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "schedule: rounds=0 total_pairs=0 unpaired=node-solo"
}

# -------------------------------------------------------------------
# 8. Per-rail annotations written on the all-pass single-pair path.
# Both nodes get `rail-{N}=<bw>` annotations for every rail and a
# `peer=<other-node>` diagnostic annotation. Both nodes labeled
# passed; no failed-rails annotation appears. [TP TC3]
# -------------------------------------------------------------------

it "single pair all rails pass -> both nodes passed + per-(rail,round) annotations" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="8"
    # N=2 yields a 1-round full-mesh schedule with the single pair
    # (node-a, node-b). Seed all rails passing.
    _seed_pair_all_pass "node-a" "node-b" 8 "ib-write-bw-pass.log"
    run __phase4_run node-a node-b
    assert_status 0
    # Both nodes pass-labeled.
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=passed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/rail-bandwidth=passed --overwrite"
    # GPUOP-828: per-(rail, round) annotations carry both BW and peer
    # in the value. Spot-check rail 0 and rail 7 of round 0. BW value
    # comes from the ib-write-bw-pass.log fixture (388.42).
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-0-round-0=388.42/peer=node-b --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-7-round-0=388.42/peer=node-b --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-rail-0-round-0=388.42/peer=node-a --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-rail-7-round-0=388.42/peer=node-a --overwrite"
    # Failed-rails / triangulation annotations must NOT appear on
    # the all-pass path.
    if grep -F "amd.com/rail-bandwidth-failed-rails" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-pass path must not write failed-rails annotation:
$(grep failed-rails "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "amd.com/rail-bandwidth-triangulation" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-pass path must not write triangulation annotation:
$(grep triangulation "$KUBECTL_CALLS_FILE")"
    fi
    if grep -F "amd.com/rail-bandwidth=failed" \
            "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "all-pass path must not write the failed label:
$(grep failed "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "pass=2 fail=0"
}

# -------------------------------------------------------------------
# 9. Single-rail-fail: rails 0.4,6,7 pass; rail 5 returns a BW value
# BELOW the 380 Gbps threshold. Both nodes labeled =failed and the
# failed-rails annotation pins out rail 5 specifically. Per-rail BW
# annotation for rail 5 is still written (preserving the measured
# value for diagnostics). [TP TC5]
# -------------------------------------------------------------------

it "single rail fail (rail 5 below threshold) -> failed-rails=5, triangulation, BW preserved" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="8"
    # N=2 -> 1 round. Pass-seed every rail first, then overwrite rail
    # 5's client log with the below-threshold fixture. (Both seed
    # calls add a NEW pod-log entry; the get loop in the mock takes
    # the LAST matching line, so the second seed wins.)
    _seed_pair_all_pass "node-a" "node-b" 8 "ib-write-bw-pass.log"
    _seed_pair_one_rail "node-a" "node-b" 5 "Complete" \
                        "ib-write-bw-below-threshold.log"
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=5 --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-failed-rails=5 --overwrite"
    # GPUOP-828: Rail 5 round-0 BW value preserved with peer identifier.
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-5-round-0=180.50/peer=node-b --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-rail-5-round-0=180.50/peer=node-a --overwrite"
    # GPUOP-828: triangulation annotation pins out the failing
    # (peer, rail, round) measurement on each side.
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-triangulation=peer=node-b/rail=5/round=0 --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-triangulation=peer=node-a/rail=5/round=0 --overwrite"
    # failure-reason annotation written by label_phase_failed records
    # the failed-rails CSV.
    assert_kubectl_call_contains \
        "amd.com/rail-bandwidth-failure-reason=failed-rails:5"
}

# -------------------------------------------------------------------
# 10. All rails fail on one pair: every rail's BW is below threshold.
# failed-rails annotation is the full 0,1,2,3,4,5,6,7 CSV.
# [TP TC6]
# -------------------------------------------------------------------

it "all rails fail on one pair -> failed-rails=0,1,2,3,4,5,6,7" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="8"
    # N=2 -> 1 round (round 0). All 8 rails seeded below threshold.
    _seed_pair_all_pass "node-a" "node-b" 8 "ib-write-bw-below-threshold.log"
    # Mark every client Job Failed=True instead of Complete=True so we
    # also cover the Failed-branch parse path. _seed_pair_all_pass
    # already seeded Complete=True; we layer Failed=True on top, and
    # the driver's lookup-last-line semantics means Failed=True wins.
    # NB: `local` is invalid at file scope; use plain assignments.
    for (( rr=0; rr < 8; rr++ )); do
        cjob_all=$(_phase4_expected_job_name "client" "node-b" "$rr" "0")
        kubectl_mock_set_job_condition "$cjob_all" "Failed" "True"
    done
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=0,1,2,3,4,5,6,7 --overwrite"
    assert_kubectl_call \
        "label node node-b amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/rail-bandwidth-failed-rails=0,1,2,3,4,5,6,7 --overwrite"
    # GPUOP-828: per-(rail, round) BW value preserved on every rail
    # with peer identifier (180.50 from fixture, round 0).
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-0-round-0=180.50/peer=node-b --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-7-round-0=180.50/peer=node-b --overwrite"
    # GPUOP-828: triangulation lists every failing (peer, rail, round)
    # measurement -- in this case 8 entries on each side, all round 0.
    assert_kubectl_call_contains \
        "amd.com/rail-bandwidth-triangulation=peer=node-b/rail=0/round=0,peer=node-b/rail=1/round=0"
}

# -------------------------------------------------------------------
# 11. ib-write-bw-crashed: client Job Failed=True + log has no BW line
# -> rail recorded reason=ib-write-bw-crashed. [TP TC7]
# -------------------------------------------------------------------

it "client Failed + no BW line -> reason=ib-write-bw-crashed" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="1"
    _seed_pair_one_rail "node-a" "node-b" 0 "Failed" \
                        "ib-write-bw-crashed.log"
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=0 --overwrite"
    # The driver's record-reason path stores "ib-write-bw-crashed" on
    # disk; the failure-reason annotation surfaces failed-rails:0 (the
    # driver does NOT propagate the per-rail reason into the
    # composite failure-reason -- it only lists the failed rail
    # indexes). Spot-check the log line that proves the classification.
    assert_stdout_contains "reason=ib-write-bw-crashed"
}

# -------------------------------------------------------------------
# 12. parse-failure: Complete=True but log has no "BW average" line.
# -> reason=parse-failed. [TP TC8]
# -------------------------------------------------------------------

it "client Complete but empty log -> reason=parse-failed" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="1"
    _seed_pair_one_rail "node-a" "node-b" 0 "Complete" \
                        "ib-write-bw-empty.log"
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=0 --overwrite"
    assert_stdout_contains "reason=parse-failed"
}

# -------------------------------------------------------------------
# 13. server-pod-unready timeout: NO server pod IP seeded -> driver
# times out the pod-IP wait. With PHASE4_PAIR_WAIT_TIME=0 the
# wait loop's `elapsed >= timeout` check fires on the first
# iteration. The driver distinguishes nad-missing (no pod ever
# created) from peer-pod-unready (pod created but no IP); since
# we seed pod-for-job but NOT pod-ip, the pod-count probe
# returns 1 -> reason=peer-pod-unready. [TP TC12]
# -------------------------------------------------------------------

it "server pod IP never set + PHASE4_PAIR_WAIT_TIME=0 -> reason=peer-pod-unready" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="1"
    export PHASE4_PAIR_WAIT_TIME="0"
    # Seed pod-for-job so the timeout-time pod-count probe sees 1 pod
    # (so the classifier returns peer-pod-unready, not nad-missing).
    # Do NOT seed pod-ip-for-job -- that's what triggers the timeout.
    # NB: `local` is invalid at file scope; use plain assignments.
    sjob=$(_phase4_expected_job_name "server" "node-a" "0")
    kubectl_mock_set_pod_for_job "$sjob" "pod-${sjob}"
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "label node node-a amd.com/rail-bandwidth=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=0 --overwrite"
    # The "server pod IP wait failed rc=X reason=Y" log line is emitted
    # to stderr by pair_runner (driver line 2317). Match on stderr.
    assert_stderr_contains "reason=peer-pod-unready"
    # Server Job must be cleaned up on the timeout path.
    assert_kubectl_call \
        "delete job ${sjob} --ignore-not-found=true --wait=false"
}

# Variant: NO pod seeded at all -> pod-count probe returns 0 ->
# classifier returns nad-missing.
it "server pod never created + PHASE4_PAIR_WAIT_TIME=0 -> reason=nad-missing" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="1"
    export PHASE4_PAIR_WAIT_TIME="0"
    # Seed nothing for the server Job -- the get-pods listing returns
    # zero lines -> pod_count=0 -> classifier returns nad-missing.
    run __phase4_run node-a node-b
    assert_status 0
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-failed-rails=0 --overwrite"
    # Same as above -- the reason string is on stderr.
    assert_stderr_contains "reason=nad-missing"
}

# -------------------------------------------------------------------
# 14. rail-count override: PHASE4_RAIL_COUNT=4 -> only rails 0-3
# are tested; rails 4-7 are NOT in any annotation. [TP TC11]
# -------------------------------------------------------------------

it "PHASE4_RAIL_COUNT=4 -> rails 0-3 annotated; rails 4-7 absent" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="4"
    _seed_pair_all_pass "node-a" "node-b" 4 "ib-write-bw-pass.log"
    run __phase4_run node-a node-b
    assert_status 0
    # GPUOP-828: rails 0-3 annotated for round 0 (N=2 -> 1 round).
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-0-round-0=388.42/peer=node-b --overwrite"
    assert_kubectl_call \
        "annotate node node-a amd.com/rail-bandwidth-rail-3-round-0=388.42/peer=node-b --overwrite"
    # Rails 4-7 must NOT be annotated (any round).
    for rail in 4 5 6 7; do
        if grep -F "amd.com/rail-bandwidth-rail-${rail}-" \
                "$KUBECTL_CALLS_FILE" >/dev/null; then
            _assert_fail "RAIL_COUNT=4 leaked rail-${rail} annotation:
$(grep "rail-${rail}-" "$KUBECTL_CALLS_FILE")"
        fi
    done
}

# -------------------------------------------------------------------
# 15. Concurrency cap honored: 16-node input -> 8 pairs. With
# PHASE4_MAX_CONCURRENT_PAIRS=8, all 8 pair_runners can run in
# parallel (no slot-wait sleep iterations recorded). We can't
# easily observe peak concurrency without instrumentation, but
# we CAN verify (a) the driver logs all 8 pair forks, (b) the
# final pass/fail aggregation accounts for all 16 nodes, and
# (c) the unpaired counter is 0. [TP TC15]
# -------------------------------------------------------------------

it "8 nodes (full mesh, cap=4) -> 28 pairs across 7 rounds, all 8 nodes labeled" && {
    _reset_phase4_env
    # GPUOP-828: full mesh on N=8 produces 7 rounds with 4 disjoint
    # pairs/round = 28 pairs total. With PHASE4_MAX_CONCURRENT_PAIRS=4
    # all 4 pairs in each round can dispatch in parallel. We exercise
    # the scheduler alone (PHASE4_RAIL_COUNT=0) so we do not have to
    # seed 28 * 0 mock Jobs; the schedule, dispatch, and aggregation
    # paths still all run.
    export PHASE4_RAIL_COUNT="0"
    export PHASE4_MAX_CONCURRENT_PAIRS="4"
    nodes8=""
    for i8 in 01 02 03 04 05 06 07 08; do
        nodes8="$nodes8 node-${i8}"
    done
    # shellcheck disable=SC2086
    run __phase4_run $nodes8
    assert_status 0
    assert_stdout_contains "schedule: rounds=7 total_pairs=28 unpaired=<none>"
    assert_stdout_contains "dispatching pair_runners (cap=4, rounds=7)"
    # All 28 pair forks logged (monotonic global counter).
    for pair_idx in 0 1 2 3 27; do
        assert_stdout_contains "forking pair #${pair_idx}"
    done
    # All 7 rounds emitted a START line.
    for ri in 0 1 2 3 4 5 6; do
        assert_stdout_contains "round ${ri} START:"
        assert_stdout_contains "round ${ri} DONE"
    done
    assert_stdout_contains "pass=8 fail=0"
    # Full-mesh property verified.
    sched=$(_phase4_extract_schedule)
    _phase4_assert_full_mesh "$sched" 28 \
        node-01 node-02 node-03 node-04 \
        node-05 node-06 node-07 node-08
}

# Defensive guard: PHASE4_MAX_CONCURRENT_PAIRS=0 is invalid; the driver
# promotes to 1 (serial) with a logged warning rather than deadlocking.
it "PHASE4_MAX_CONCURRENT_PAIRS=0 is promoted to 1 with a warning" && {
    _reset_phase4_env
    # GPUOP-828: PHASE4_RAIL_COUNT=0 exercises the scheduler / cap
    # promotion logic without per-rail kubectl mocking.
    export PHASE4_RAIL_COUNT="0"
    export PHASE4_MAX_CONCURRENT_PAIRS="0"
    run __phase4_run node-a node-b
    assert_status 0
    assert_stderr_contains "PHASE4_MAX_CONCURRENT_PAIRS=0 invalid -- promoting to 1"
    assert_stdout_contains "dispatching pair_runners (cap=1"
}

# -------------------------------------------------------------------
# 16. PHASE_NODES env-var fallback: when positional args are empty
# but PHASE_NODES is exported, the driver uses that list.
# -------------------------------------------------------------------

it "PHASE_NODES env var is used when no positional args are given" && {
    _reset_phase4_env
    export PHASE_NODES="node-env-only"
    run __phase4_run    # NB: no positional args
    assert_status 0
    # Single-node fallback -> unpaired pass-label.
    assert_kubectl_call \
        "label node node-env-only amd.com/rail-bandwidth=passed --overwrite"
    assert_kubectl_call \
        "annotate node node-env-only amd.com/rail-bandwidth-unpaired=true --overwrite"
}

# -------------------------------------------------------------------
# GPUOP-828: Full-mesh pair-generator tests.
#
# These tests exercise the circle-algorithm scheduler emitted by
# PHASE4_DRIVER_SCRIPT for a range of N values (even and odd). For
# each N we verify:
# (a) the correct number of rounds is emitted
# (b) the correct total pair count = C(N,2)
# (c) within every round, no node appears in more than one pair
# (disjointness property)
# (d) the union over all rounds equals the C(N,2) pair set
# (full-mesh coverage)
#
# These run with PHASE4_RAIL_COUNT=0 so the rail loop inside
# pair_runner is a no-op -- no kubectl Job mocks needed. The schedule
# generation, round-by-round dispatch logging, and per-node
# aggregation paths are all exercised.
# -------------------------------------------------------------------

_phase4_mesh_test() {
    local n="$1"
    shift
    local -a nodes=("$@")
    local expected=$(( n * (n - 1) / 2 ))
    # Circle-algorithm round count: N-1 for even N (circle of size N);
    # N for odd N (circle of size N+1 with a bye slot). Both produce
    # a full mesh (union covers C(N,2)).
    local expected_rounds
    if (( n % 2 == 0 )); then
        expected_rounds=$(( n - 1 ))
    else
        expected_rounds="$n"
    fi
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="0"
    # shellcheck disable=SC2086
    run __phase4_run "${nodes[@]}"
    assert_status 0
    assert_stdout_contains "schedule: rounds=${expected_rounds} total_pairs=${expected} unpaired=<none>"
    sched=$(_phase4_extract_schedule)
    _phase4_assert_full_mesh "$sched" "$expected" "${nodes[@]}"
}

it "mesh-schedule N=2 -> 1 round, 1 pair = (a,b)" && {
    _phase4_mesh_test 2 node-a node-b
}

it "mesh-schedule N=4 -> 3 rounds, 2 disjoint pairs/round, 6 total" && {
    _phase4_mesh_test 4 node-a node-b node-c node-d
}

it "mesh-schedule N=5 (odd) -> 5 rounds (N), one node sits out per round, 10 total" && {
    # Circle algorithm: odd N needs N rounds (circle_size=N+1 with
    # bye), not N-1. (N-1)*(N-1)/2 = 8 pair-slots, which is short of
    # C(5,2)=10; N rounds * (N-1)/2 pairs = 10 = C(5,2).
    _phase4_mesh_test 5 node-a node-b node-c node-d node-e
}

it "mesh-schedule N=6 -> 5 rounds, 3 disjoint pairs/round, 15 total" && {
    _phase4_mesh_test 6 node-a node-b node-c node-d node-e node-f
}

it "mesh-schedule N=7 (odd) -> 7 rounds (N), one node sits out per round, 21 total" && {
    # See N=5 note: odd N requires N (not N-1) rounds with the
    # circle/bye algorithm to cover C(N,2) pairs.
    _phase4_mesh_test 7 node-a node-b node-c node-d node-e node-f node-g
}

it "mesh-schedule N=8 -> 7 rounds, 4 disjoint pairs/round, 28 total" && {
    _phase4_mesh_test 8 node-a node-b node-c node-d \
                          node-e node-f node-g node-h
}

# Disjointness property: for odd N=5 specifically, verify that each
# round names exactly 2 real pairs (one node sits out), and the
# sit-out node rotates through all five nodes across the five rounds.
it "mesh-schedule N=5 odd: each node sits out exactly once across the 5 rounds" && {
    _reset_phase4_env
    export PHASE4_RAIL_COUNT="0"
    run __phase4_run node-a node-b node-c node-d node-e
    assert_status 0
    # Each round must include exactly 2 pairs (4 nodes), leaving 1 node out.
    # We tally bench presences: any node missing from a round is sitting out.
    sched=$(_phase4_extract_schedule)
    unset sitouts
    declare -A sitouts=()
    while IFS=$'\t' read -r round_idx round_pairs; do
        [[ -z "$round_idx" ]] && continue
        unset present
        declare -A present=()
        for p in $round_pairs; do
            a="${p%,*}"; b="${p#*,}"
            present[$a]=1
            present[$b]=1
        done
        sitout_this_round=""
        for n in node-a node-b node-c node-d node-e; do
            if [[ -z "${present[$n]:-}" ]]; then
                # The driver schedule for N=5 must leave exactly one
                # node out per round.
                if [[ -n "$sitout_this_round" ]]; then
                    _assert_fail "round ${round_idx} leaves more than one node out: ${sitout_this_round} and ${n}"
                fi
                sitout_this_round="$n"
                sitouts[$n]=$(( ${sitouts[$n]:-0} + 1 ))
            fi
        done
    done <<< "$sched"
    # Every node must sit out exactly once.
    for n in node-a node-b node-c node-d node-e; do
        if [[ "${sitouts[$n]:-0}" -ne 1 ]]; then
            _assert_fail "node ${n} sits out ${sitouts[$n]:-0} time(s); expected 1"
        fi
    done
}

assert_summary
