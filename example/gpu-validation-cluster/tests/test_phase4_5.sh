#!/bin/bash
# Unit tests for PHASE45_PREFLIGHT_SCRIPT (.* body) against
# the mocked kubectl harness.
#
# Scope (from the design doc §7
# "Testing Strategy" + test plan):
# * SSH-mesh pair-iteration: all-pass / single-pair-fail / all-fail
# -- covers test-plan TC2 (all-checks-pass) and TC4
# (single-pair-ssh-fail) for the SSH mesh branch.
# * WORKER_REPLICAS=1 degenerate case: self-pair only, no divide-by-
# zero, no hang -- covers test-plan TC8.
# * DNS forward-miss fixture -- covers test-plan TC5.
# * MPI spawn fail -- covers test-plan TC6.
# * RCCL topology hard-fail (non-zero, non-124 exit) and soft-fail
# (124 timeout) -- covers test-plan TC7 (rccl-topology-timeout)
# and the hard-fail companion path from design §4 verdict block.
# * Annotation classification: multi-class union, comma-joined
# (`ssh-mesh,dns,mpi-spawn,rccl-topology`) annotated on every
# participating node -- covers test-plan TC9
# (annotation-includes-all-failed-classes).
# * Verdict-block contracts:
# - happy path: zero annotate calls, exit 0
# - hard-failed: exit 1, annotate ran with the comma-joined
# classes, --overwrite present on every annotate
# - soft-only (rccl_topo_timeout alone): annotate ran with
# "rccl-topology", exit 0 (design §6 carve-out)
#
# How PHASE45_PREFLIGHT_SCRIPT is exercised (mirrors test_phase4.sh
# conventions):
#
# The script body is a block-scalar inside cluster-validation-config.yaml.
# We extract it with lib/extract_script.sh, then wrap it in a
# function `__phase45_run` so the test can drive it under `set -u`
# without polluting the harness's globals (the body itself runs
# under `set -euo pipefail`).
#
# `kubectl` is the mock from lib/kubectl_mock.sh:
# * `kubectl get mpijob .` -- seeded with kubectl_mock_set_mpijob_names.
# * `kubectl get pods -n NS -l <labels> -o jsonpath=.` -- seeded
# with kubectl_mock_set_pod_names / set_pod_ips / set_node_names
# (Phase 4.5 uses Kubeflow training labels, NOT job-name=, so
# it takes the dedicated phase45-* state routes added for this
# change rather than the job-name=-selector early routes).
# * `kubectl wait .` -- default pass; can be failed via
# kubectl_mock_fail wait <ec>.
# * `kubectl exec .` -- per-call response queue via
# kubectl_mock_queue_exec. Each test calls _queue_*_path
# helpers (defined below) to enqueue exactly the responses the
# script will consume in order.
# * `kubectl annotate node .` -- recorded; pass-through.
#
# We also shim host-side `timeout` (used to wrap the RCCL probe)
# as a no-op pass-through (`timeout 60 kubectl exec .` -> just
# exec the inner command), because the mock kubectl already returns
# the desired exit code (including 124 for the soft-fail timeout
# simulation) on its own.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"
FIXTURES_DIR="${TEST_DIR}/fixtures/phase4_5"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase4_5.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "  Fixtures:  ${FIXTURES_DIR}"
echo "================================================================"

# --- one-time setup -------------------------------------------------

PHASE45_DIR=$(mktemp -d -t phase4_5-tests-XXXXXX)
SHIM_DIR="${PHASE45_DIR}/shims"
PHASE45_BODY="${PHASE45_DIR}/phase4_5-body.sh"
mkdir -p "$SHIM_DIR"

trap 'rm -rf "$PHASE45_DIR"; kubectl_mock_cleanup' EXIT

# Shims:
# * `timeout` -- strip the leading duration arg and exec the rest.
# The mock kubectl directly returns 124 (or anything)
# when the test wants to drive the rccl_topo_timeout
# branch.
# * `sleep` -- no-op; the SSH-readiness loop inside `bash -c` is
# never reached because that whole exec is mocked,
# but defensive-shim anyway in case any subshell
# evaluation does reach a real sleep.
cat >"${SHIM_DIR}/timeout" <<'EOF'
#!/bin/bash
# Strip the duration arg (and an optional --foreground / -k <n> form)
# and exec the rest. The Phase 4.5 caller is:
# timeout 60 kubectl exec -n NS POD -- bash -c '.'
# i.e. a plain `timeout <secs> <cmd.>`, so the simple shift is
# sufficient.
shift || true
exec "$@"
EOF
chmod +x "${SHIM_DIR}/timeout"

cat >"${SHIM_DIR}/sleep" <<'EOF'
#!/bin/bash
exit 0
EOF
chmod +x "${SHIM_DIR}/sleep"

# Extract PHASE45_PREFLIGHT_SCRIPT and wrap in a function. The body
# uses no `local`, but wrapping in a function keeps its globals
# (ssh_mesh_failed, dns_failed, etc.) out of the harness scope so
# back-to-back `it` blocks don't leak state across tests.
RAW_PHASE45=$(extract_configmap_data "$CONFIGMAP" "PHASE45_PREFLIGHT_SCRIPT")
if [[ -z "$RAW_PHASE45" ]]; then
    echo "FATAL: PHASE45_PREFLIGHT_SCRIPT extraction produced empty output" >&2
    exit 1
fi

{
    printf '__phase45_run() {\n'
    printf '%s\n' "$RAW_PHASE45"
    printf '}\n'
} > "$PHASE45_BODY"

if ! bash -n "$PHASE45_BODY"; then
    echo "FATAL: patched PHASE45_PREFLIGHT_SCRIPT has bash syntax errors" >&2
    exit 1
fi

kubectl_mock_init

# Shim dir goes after the mock kubectl so `kubectl` still resolves
# to the mock; `timeout` and `sleep` come from our shims.
export PATH="${SHIM_DIR}:${PATH}"

# shellcheck disable=SC1090
source "$PHASE45_BODY"

if ! declare -F __phase45_run >/dev/null; then
    echo "FATAL: __phase45_run not defined after sourcing" >&2
    exit 1
fi

# Suppress -u so tests that intentionally exercise optional env can
# leave knobs unset.
set +u

# --- per-test helpers -----------------------------------------------

# _reset_phase45_env
# Wipe mock state and re-export the baseline env PHASE45_PREFLIGHT_SCRIPT
# reads. The defaults below mirror what the launcher init-container
# manifest passes in production (cluster-validation-job.yaml).
_reset_phase45_env() {
    kubectl_mock_reset
    export WORKER_REPLICAS="2"
    export WAIT_FOR_WORKERS="true"
    export ENABLE_SSH_CHECK="true"
    export WORKER_READY_TIMEOUT="300"
    export SSH_CHECK_TIMEOUT="60"
    export SSH_CHECK_INTERVAL="5"
    export JOB_NAME="cluster-validation-mpi"
    export PERF_TEST_DIR="/opt/rccl-tests/build"
    # Mirror the production discovery: 1 MPIJob revision exists.
    kubectl_mock_set_mpijob_names "cluster-validation-mpi-1"
}

# _seed_2pod_topology
# Two healthy worker pods on two distinct nodes. Default fabric layout
# used by most tests; individual tests override the pod count with
# _seed_1pod_topology when exercising the degenerate path.
_seed_2pod_topology() {
    kubectl_mock_set_pod_names  "worker-pod-a worker-pod-b"
    kubectl_mock_set_pod_ips    "10.42.0.10 10.42.0.11"
    kubectl_mock_set_node_names "node-a" "node-b"
}

_seed_1pod_topology() {
    export WORKER_REPLICAS="1"
    kubectl_mock_set_pod_names  "worker-pod-a"
    kubectl_mock_set_pod_ips    "10.42.0.10"
    kubectl_mock_set_node_names "node-a"
}

# --- exec-queue helpers ---------------------------------------------
# PHASE45_PREFLIGHT_SCRIPT issues exec calls in a fixed order. These
# helpers enqueue the right number of responses for each phase so the
# test bodies can compose them by intent rather than counting calls.

# _queue_launcher_ssh_pass / _fail
_queue_launcher_ssh_pass() { kubectl_mock_queue_exec 0; }
_queue_launcher_ssh_fail() { kubectl_mock_queue_exec 1; }

# _queue_mesh_all_pass <N>
# One exec response per (src, dst_ip) pair. The mesh loop is
# `for src in WORKER_PODS; for dst_ip in WORKER_IPS`. For N pods
# that's N*N pairs.
_queue_mesh_all_pass() {
    local n="$1"
    local total=$((n * n))
    local i
    for (( i = 0; i < total; i++ )); do
        kubectl_mock_queue_exec 0
    done
}

# _queue_mesh_one_fail <N> <fail_idx>
# All N*N exec responses succeed except the one at <fail_idx>
# (0-based, in row-major (src major) order).
_queue_mesh_one_fail() {
    local n="$1"
    local fail_idx="$2"
    local total=$((n * n))
    local i
    for (( i = 0; i < total; i++ )); do
        if [[ "$i" -eq "$fail_idx" ]]; then
            kubectl_mock_queue_exec 1
        else
            kubectl_mock_queue_exec 0
        fi
    done
}

# _queue_mesh_all_fail <N>
_queue_mesh_all_fail() {
    local n="$1"
    local total=$((n * n))
    local i
    for (( i = 0; i < total; i++ )); do
        kubectl_mock_queue_exec 1
    done
}

# _queue_dns_clean / _queue_dns_fwd_miss
# DNS check is one exec that streams DNS:<host>:. lines for any
# miss; clean runs produce no output. The script captures stdout into
# dns_misses[] and uses the array length as the verdict.
_queue_dns_clean()    { kubectl_mock_queue_exec 0; }
_queue_dns_fwd_miss() {
    kubectl_mock_queue_exec 0 "$(cat "${FIXTURES_DIR}/dns-fwd-miss.txt")"
}

# _queue_mpi_pass / _queue_mpi_fail
_queue_mpi_pass() { kubectl_mock_queue_exec 0; }
_queue_mpi_fail() { kubectl_mock_queue_exec 1; }

# _queue_rccl_pass / _queue_rccl_hard_fail / _queue_rccl_timeout
# RCCL is one exec wrapped by host-side `timeout 60 .`. The shim
# strips the duration and execs the inner command; the mock kubectl
# returns whatever exit code we enqueue. Exit 124 -> soft-fail
# (rccl_topo_timeout=true); any other non-zero -> hard-fail
# (rccl_topo_failed=true).
_queue_rccl_pass()      {
    kubectl_mock_queue_exec 0 "$(cat "${FIXTURES_DIR}/rccl-pass.txt")"
}
_queue_rccl_hard_fail() { kubectl_mock_queue_exec 1; }
_queue_rccl_timeout()   { kubectl_mock_queue_exec 124; }

# --- all-pass scaffold ---------------------------------------------
# Every test starts from a healthy 2-pod cluster and overrides one
# phase's exec response to inject the failure under test. This helper
# enqueues the full "all pass" sequence; tests that want to mutate
# one phase reset the queue (via _reset_phase45_env -> kubectl_mock_reset)
# and re-enqueue piece by piece.
_queue_all_pass_2pod() {
    _queue_launcher_ssh_pass    # 1 call
    _queue_mesh_all_pass 2      # 4 calls
    _queue_dns_clean            # 1 call
    _queue_mpi_pass             # 1 call
    _queue_rccl_pass            # 1 call
}

_queue_all_pass_1pod() {
    _queue_launcher_ssh_pass    # 1 call
    _queue_mesh_all_pass 1      # 1 call
    _queue_dns_clean            # 1 call
    _queue_mpi_pass             # 1 call
    _queue_rccl_pass            # 1 call
}

# --- assertion helpers ----------------------------------------------

# _assert_no_annotate
# Verify zero `kubectl annotate .` calls were recorded.
_assert_no_annotate() {
    local path="${KUBECTL_CALLS_FILE:-}"
    if grep -q '^annotate ' "$path"; then
        _assert_fail "expected no annotate calls; got:
$(grep '^annotate ' "$path")"
    fi
}

# _assert_annotate_classes <comma-joined-reason>
# Verify EVERY recorded `annotate node` call carries the exact
# `amd.com/phase4_5-failure-reason=<reason>` value AND --overwrite.
# Also verify at least one such call exists.
_assert_annotate_classes() {
    local reason="$1"
    local path="${KUBECTL_CALLS_FILE:-}"
    local match_line="amd.com/phase4_5-failure-reason=${reason}"
    if ! grep -F -- "$match_line" "$path" >/dev/null; then
        _assert_fail "expected annotate call with [${match_line}], got:
$(grep '^annotate ' "$path" || echo '<no annotate calls>')"
        return
    fi
    # Every annotate call must use --overwrite (design §4 verdict
    # block: annotations replace stale values from prior failed runs).
    while IFS= read -r line; do
        if [[ "$line" == "annotate "* && "$line" != *"--overwrite"* ]]; then
            _assert_fail "annotate call missing --overwrite: ${line}"
        fi
    done <"$path"
}

# _assert_annotated_nodes <node1> <node2> .
# Verify each named node received an annotate call.
_assert_annotated_nodes() {
    local path="${KUBECTL_CALLS_FILE:-}"
    local n
    for n in "$@"; do
        if ! grep -E "^annotate node ${n} " "$path" >/dev/null; then
            _assert_fail "expected annotate for node ${n}; got:
$(grep '^annotate ' "$path" || echo '<no annotate calls>')"
        fi
    done
}

# ==========================================================
# TESTS
# ==========================================================

# --- TC2 (all-checks-pass) for 2 pods -------------------------------
it "all checks pass on a healthy 2-pod cluster -> exit 0, no annotate" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_all_pass_2pod
    run __phase45_run
    assert_status 0
    _assert_no_annotate
    assert_stdout_contains "Phase 4.5 pre-flight passed: ssh-mesh, dns, mpi-spawn, rccl-topology all OK."
}

# --- TC4 (single-pair-ssh-fail) -- mesh loop continues, annotates ---
it "single SSH mesh pair fails -> ssh_mesh_failed=true, classes=ssh-mesh, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    # 4 mesh probes; fail the 2nd pair (worker-pod-a -> 10.42.0.11)
    _queue_mesh_one_fail 2 1    # 4
    _queue_dns_clean            # 1
    _queue_mpi_pass             # 1
    _queue_rccl_pass            # 1
    run __phase45_run
    assert_status 1
    assert_stdout_contains "WARN: N*N SSH mesh check failed for 1 pair(s)"
    assert_stdout_contains "worker-pod-a->10.42.0.11"
    assert_stdout_contains "FATAL: Phase 4.5 pre-flight failed: ssh-mesh"
    _assert_annotate_classes "ssh-mesh"
    _assert_annotated_nodes node-a node-b
}

# --- mesh all-fail: all pairs broken -> still single class ---------
it "all SSH mesh pairs fail -> failed_pairs=4, classes=ssh-mesh, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_fail 2      # 4
    _queue_dns_clean            # 1
    _queue_mpi_pass             # 1
    _queue_rccl_pass            # 1
    run __phase45_run
    assert_status 1
    assert_stdout_contains "WARN: N*N SSH mesh check failed for 4 pair(s)"
    _assert_annotate_classes "ssh-mesh"
}

# --- TC8 (worker-replicas-1) -- degenerate self-pair --------------
it "WORKER_REPLICAS=1 self-pair only -> no divide-by-zero, exit 0" && {
    _reset_phase45_env
    _seed_1pod_topology
    _queue_all_pass_1pod
    run __phase45_run
    assert_status 0
    # Self-pair (worker-pod-a -> 10.42.0.10) should mesh-OK exactly
    # once (1 src * 1 dst).
    assert_stdout_contains "__ mesh OK: worker-pod-a -> 10.42.0.10"
    assert_stdout_contains "Phase 4.5 pre-flight passed"
    _assert_no_annotate
}

# --- TC5 (dns-fail-fwd) ---------------------------------------------
it "DNS forward miss -> dns_failed=true, classes=dns, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_pass 2      # 4
    _queue_dns_fwd_miss         # 1 -- emits DNS:worker-b:fwd=MISS rev=SKIP
    _queue_mpi_pass             # 1
    _queue_rccl_pass            # 1
    run __phase45_run
    assert_status 1
    assert_stdout_contains "WARN: DNS check recorded 1 miss(es)"
    assert_stdout_contains "DNS:worker-b:fwd=MISS rev=SKIP"
    assert_stdout_contains "FATAL: Phase 4.5 pre-flight failed: dns"
    _assert_annotate_classes "dns"
}

# --- TC6 (mpi-spawn-fail) -------------------------------------------
it "mpirun --hostfile no-op fails -> mpi_spawn_failed=true, classes=mpi-spawn, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_pass 2      # 4
    _queue_dns_clean            # 1
    _queue_mpi_fail             # 1
    _queue_rccl_pass            # 1
    run __phase45_run
    assert_status 1
    assert_stdout_contains "WARN: mpirun --hostfile no-op spawn failed (mpi_spawn_failed=true)"
    assert_stdout_contains "FATAL: Phase 4.5 pre-flight failed: mpi-spawn"
    _assert_annotate_classes "mpi-spawn"
}

# --- RCCL hard-fail (non-timeout non-zero) --------------------------
it "RCCL probe non-zero exit -> rccl_topo_failed=true, classes=rccl-topology, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_pass 2      # 4
    _queue_dns_clean            # 1
    _queue_mpi_pass             # 1
    _queue_rccl_hard_fail       # 1
    run __phase45_run
    assert_status 1
    assert_stdout_contains "WARN: RCCL topology probe failed with exit 1 (rccl_topo_failed=true)"
    assert_stdout_contains "FATAL: Phase 4.5 pre-flight failed: rccl-topology"
    _assert_annotate_classes "rccl-topology"
}

# --- TC7 (rccl-topology-timeout) -- soft-fail per design §6 ---------
it "RCCL probe times out (exit 124) -> annotate rccl-topology, exit 0" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_pass 2      # 4
    _queue_dns_clean            # 1
    _queue_mpi_pass             # 1
    _queue_rccl_timeout         # 1 -- exit 124
    run __phase45_run
    # Soft-fail: annotate but do NOT abort. Exit must be 0 so the
    # launcher init-container proceeds and Phase 5 is allowed to
    # exercise the warm-cache path (design §6).
    assert_status 0
    assert_stdout_contains "WARN: RCCL topology probe timed out after 60s (rccl_topo_timeout=true)"
    assert_stdout_contains "WARN: Phase 4.5 pre-flight soft-failed: rccl-topology"
    assert_stdout_contains "Phase 4.5 proceeding past soft-fail"
    _assert_annotate_classes "rccl-topology"
    _assert_annotated_nodes node-a node-b
}

# --- TC9 (annotation-includes-all-failed-classes) -------------------
it "all four checks fail -> classes=ssh-mesh,dns,mpi-spawn,rccl-topology, exit 1" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_all_fail 2      # 4
    _queue_dns_fwd_miss         # 1
    _queue_mpi_fail             # 1
    _queue_rccl_hard_fail       # 1
    run __phase45_run
    assert_status 1
    # Order is fixed by the verdict block:
    # ssh-mesh, dns, mpi-spawn, rccl-topology
    _assert_annotate_classes "ssh-mesh,dns,mpi-spawn,rccl-topology"
    assert_stdout_contains "FATAL: Phase 4.5 pre-flight failed: ssh-mesh,dns,mpi-spawn,rccl-topology"
    _assert_annotated_nodes node-a node-b
}

# --- mixed hard + soft: hard wins; reason includes both -------------
it "hard fail + rccl timeout -> classes include both, exit 1 (hard wins)" && {
    _reset_phase45_env
    _seed_2pod_topology
    _queue_launcher_ssh_pass    # 1
    _queue_mesh_one_fail 2 0    # 4 (fail first pair)
    _queue_dns_clean            # 1
    _queue_mpi_pass             # 1
    _queue_rccl_timeout         # 1 -- soft-fail
    run __phase45_run
    # Verdict: hard_failed=true (ssh-mesh) -> exit 1; annotation set
    # is union of HARD + SOFT classes, so rccl-topology is included
    # via the soft-fail branch.
    assert_status 1
    _assert_annotate_classes "ssh-mesh,rccl-topology"
    assert_stdout_contains "FATAL: aborting MPIJob via non-zero init-container exit"
}

# --- ENABLE_SSH_CHECK=false -> the whole pre-flight body is skipped -
it "ENABLE_SSH_CHECK=false skips the entire pre-flight body, exit 0" && {
    _reset_phase45_env
    _seed_2pod_topology
    export ENABLE_SSH_CHECK="false"
    # No exec responses needed -- the script's `if [ "$ENABLE_SSH_CHECK" = "true" ]`
    # gate skips every kubectl exec when false.
    run __phase45_run
    assert_status 0
    _assert_no_annotate
    # Sanity: the verdict banner should NOT appear when the body was
    # skipped wholesale.
    assert_stdout_not_contains "Phase 4.5 pre-flight passed"
    assert_stdout_not_contains "FATAL: Phase 4.5"
}

# --- WAIT_FOR_WORKERS=false -> kubectl wait is NOT called -----------
it "WAIT_FOR_WORKERS=false skips kubectl wait but still runs checks" && {
    _reset_phase45_env
    _seed_2pod_topology
    export WAIT_FOR_WORKERS="false"
    _queue_all_pass_2pod
    run __phase45_run
    assert_status 0
    # No `wait` verb recorded.
    if grep -q '^wait ' "$KUBECTL_CALLS_FILE"; then
        _assert_fail "expected no kubectl wait call, got:
$(grep '^wait ' "$KUBECTL_CALLS_FILE")"
    fi
    assert_stdout_contains "Phase 4.5 pre-flight passed"
}

assert_summary
