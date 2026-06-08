#!/bin/bash
# Unit tests for PHASE_NODE_LABEL_SCRIPT
# against the kubectl mock harness.
#
# Covers the four functions the orchestrator depends on:
# * label_phase_passed
# * label_phase_failed
# * annotate_phase_value
# * filter_passed_nodes
#
# Also covers contract invariants from design §5:
# * helpers use --overwrite for idempotency
# * label_phase_failed writes the failure-reason annotation
# * filter_passed_nodes drops nodes that are not exactly "=passed"
# * argument validation: empty args / wrong arity return non-zero
# with NO kubectl side effects

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${TEST_DIR}/../../.." && pwd)
CONFIGMAP="${REPO_ROOT}/example/gpu-validation-cluster/configs/cluster-validation-config.yaml"

# shellcheck source=./lib/assert.sh
source "${TEST_DIR}/lib/assert.sh"
# shellcheck source=./lib/kubectl_mock.sh
source "${TEST_DIR}/lib/kubectl_mock.sh"
# shellcheck source=./lib/extract_script.sh
source "${TEST_DIR}/lib/extract_script.sh"

echo "================================================================"
echo "  test_phase_node_label_script.sh"
echo "  ConfigMap: ${CONFIGMAP}"
echo "================================================================"

# --- one-time setup -------------------------------------------------
HELPER_SCRIPT=$(mktemp -t phase-helpers-XXXXXX.sh)
trap 'rm -f "$HELPER_SCRIPT"; kubectl_mock_cleanup' EXIT

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

# Source the helper library AFTER kubectl_mock_init so the kubectl
# shim is on PATH when the helpers call it. The failure-reason
# annotation suffix must mirror the ConfigMap default so we can assert
# on the expected annotation key.
export PHASE_FAILURE_REASON_ANNOTATION_SUFFIX="-failure-reason"
# shellcheck disable=SC1090
source "$HELPER_SCRIPT"

# Sanity: the four required functions are defined.
for fn in label_phase_passed label_phase_failed annotate_phase_value filter_passed_nodes; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FATAL: helper function $fn not defined after sourcing" >&2
        exit 1
    fi
done

# -------------------------------------------------------------------
# label_phase_passed
# -------------------------------------------------------------------

it "label_phase_passed writes <key>=passed with --overwrite" && {
    kubectl_mock_reset
    run label_phase_passed node-a amd.com/gpu-hw-acceptance
    assert_status 0
    assert_kubectl_call_count 1
    assert_kubectl_call \
        "label node node-a amd.com/gpu-hw-acceptance=passed --overwrite"
}

it "label_phase_passed exits non-zero on empty node arg" && {
    kubectl_mock_reset
    run label_phase_passed "" amd.com/gpu-hw-acceptance
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_passed exits non-zero on empty key arg" && {
    kubectl_mock_reset
    run label_phase_passed node-a ""
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_passed exits non-zero on wrong arity (1 arg)" && {
    kubectl_mock_reset
    run label_phase_passed node-a
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_passed exits non-zero on wrong arity (3 args)" && {
    kubectl_mock_reset
    run label_phase_passed node-a amd.com/x extra
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_passed propagates non-zero when kubectl label fails" && {
    kubectl_mock_reset
    kubectl_mock_fail label 1
    run label_phase_passed node-a amd.com/gpu-hw-acceptance
    assert_not_equals 0 "$LAST_STATUS"
    # The label call was still made.
    assert_kubectl_call_count 1
}

# -------------------------------------------------------------------
# label_phase_failed
# -------------------------------------------------------------------

it "label_phase_failed writes <key>=failed AND failure-reason annotation" && {
    kubectl_mock_reset
    run label_phase_failed node-b amd.com/nic-health "kernel oops"
    assert_status 0
    assert_kubectl_call_count 2
    assert_kubectl_call \
        "label node node-b amd.com/nic-health=failed --overwrite"
    assert_kubectl_call \
        "annotate node node-b amd.com/nic-health-failure-reason=kernel oops --overwrite"
}

it "label_phase_failed with empty reason still writes label, skips annotation" && {
    kubectl_mock_reset
    run label_phase_failed node-b amd.com/nic-health ""
    assert_status 0
    assert_kubectl_call_count 1
    assert_kubectl_call \
        "label node node-b amd.com/nic-health=failed --overwrite"
}

it "label_phase_failed rejects empty node" && {
    kubectl_mock_reset
    run label_phase_failed "" amd.com/x "reason"
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_failed rejects empty key" && {
    kubectl_mock_reset
    run label_phase_failed node-a "" "reason"
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_failed rejects wrong arity (2 args)" && {
    kubectl_mock_reset
    run label_phase_failed node-a amd.com/x
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "label_phase_failed returns non-zero when kubectl annotate fails" && {
    kubectl_mock_reset
    kubectl_mock_fail annotate 1
    run label_phase_failed node-b amd.com/nic-health "kernel oops"
    assert_not_equals 0 "$LAST_STATUS"
    # The label call still ran; the annotate call ran but failed.
    assert_kubectl_call_count 2
}

# -------------------------------------------------------------------
# annotate_phase_value
# -------------------------------------------------------------------

it "annotate_phase_value writes <phase_key>-<sub_key>=<value>" && {
    kubectl_mock_reset
    run annotate_phase_value node-c amd.com/rail-bandwidth rail0 94.2
    assert_status 0
    assert_kubectl_call_count 1
    assert_kubectl_call \
        "annotate node node-c amd.com/rail-bandwidth-rail0=94.2 --overwrite"
}

it "annotate_phase_value allows empty value" && {
    kubectl_mock_reset
    run annotate_phase_value node-c amd.com/x rail0 ""
    assert_status 0
    assert_kubectl_call \
        "annotate node node-c amd.com/x-rail0= --overwrite"
}

it "annotate_phase_value rejects empty node / phase_key / sub_key" && {
    kubectl_mock_reset
    run annotate_phase_value "" amd.com/x rail0 v
    assert_not_equals 0 "$LAST_STATUS"
    kubectl_mock_reset
    run annotate_phase_value node-c "" rail0 v
    assert_not_equals 0 "$LAST_STATUS"
    kubectl_mock_reset
    run annotate_phase_value node-c amd.com/x "" v
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "annotate_phase_value rejects wrong arity" && {
    kubectl_mock_reset
    run annotate_phase_value node-c amd.com/x rail0
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "annotate_phase_value returns non-zero when kubectl fails" && {
    kubectl_mock_reset
    kubectl_mock_fail annotate 1
    run annotate_phase_value node-c amd.com/x rail0 v
    assert_not_equals 0 "$LAST_STATUS"
}

# -------------------------------------------------------------------
# filter_passed_nodes
# -------------------------------------------------------------------

it "filter_passed_nodes returns only nodes with =passed" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-x amd.com/phase-test passed
    kubectl_mock_set_label node-y amd.com/phase-test failed
    # node-z is intentionally unlabeled.
    run filter_passed_nodes "node-x node-y node-z" amd.com/phase-test
    assert_status 0
    assert_stdout_equals "node-x"
}

it "filter_passed_nodes preserves input order across multiple passed nodes" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-x amd.com/phase-test passed
    kubectl_mock_set_label node-y amd.com/phase-test passed
    kubectl_mock_set_label node-z amd.com/phase-test passed
    run filter_passed_nodes "node-y node-x node-z" amd.com/phase-test
    assert_status 0
    assert_stdout_equals "node-y node-x node-z"
}

it "filter_passed_nodes handles dotted label keys (jsonpath escaping)" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-x amd.com/gpu-hw-acceptance passed
    run filter_passed_nodes "node-x" amd.com/gpu-hw-acceptance
    assert_status 0
    assert_stdout_equals "node-x"
}

it "filter_passed_nodes drops nodes with non-passed label values" && {
    kubectl_mock_reset
    kubectl_mock_set_label node-x amd.com/phase-test pending
    kubectl_mock_set_label node-y amd.com/phase-test unknown
    run filter_passed_nodes "node-x node-y" amd.com/phase-test
    assert_status 0
    assert_stdout_empty
}

it "filter_passed_nodes returns 0 on empty input and emits no output" && {
    kubectl_mock_reset
    run filter_passed_nodes "" amd.com/phase-test
    assert_status 0
    assert_stdout_empty
    # kubectl is only invoked per-input-node; empty input -> no calls.
    assert_kubectl_no_calls
}

it "filter_passed_nodes rejects empty key" && {
    kubectl_mock_reset
    run filter_passed_nodes "node-a" ""
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

it "filter_passed_nodes rejects wrong arity (1 arg)" && {
    kubectl_mock_reset
    run filter_passed_nodes "node-a"
    assert_not_equals 0 "$LAST_STATUS"
    assert_kubectl_no_calls
}

# -------------------------------------------------------------------
# Cross-cutting contract invariants
# -------------------------------------------------------------------

it "all helper write paths use --overwrite (idempotency contract)" && {
    kubectl_mock_reset
    label_phase_passed node-a amd.com/x >/dev/null 2>&1
    label_phase_failed node-a amd.com/x "r" >/dev/null 2>&1
    annotate_phase_value node-a amd.com/x sub v >/dev/null 2>&1
    # Every recorded line must end with `--overwrite`.
    if grep -v -- "--overwrite$" "$KUBECTL_CALLS_FILE" >/dev/null; then
        _assert_fail "found a kubectl write call missing --overwrite:
$(grep -v -- "--overwrite$" "$KUBECTL_CALLS_FILE")"
    fi
}

it "helper diagnostics go to stderr, not stdout" && {
    kubectl_mock_reset
    run label_phase_passed node-a amd.com/x
    # The helper's success-log line ("node=node-a amd.com/x=passed")
    # must NOT appear on stdout (would corrupt pipe consumers like
    # filter_passed_nodes).
    assert_stdout_empty
    assert_stderr_contains "node=node-a"
}

assert_summary
