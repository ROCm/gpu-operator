#!/bin/bash
# Hand-rolled bash assertion library for the gpu-validation-cluster
# test suite. Provides a minimal, dependency-free
# alternative to `bats` so tests can run in any environment that has
# bash >= 4 on PATH.
#
# Usage:
# source "$(dirname "$0")/lib/assert.sh"
#
# it "label_phase_passed writes the passed label" && {
# run label_phase_passed node-a amd.com/x
# assert_status 0
# assert_kubectl_call "label node node-a amd.com/x=passed --overwrite"
# }
#
# Conventions:
# * Each test file is a normal bash script. It sources this library,
# then issues `it "<name>" && { . }` blocks. The library tracks
# per-test pass/fail and prints a summary at exit.
# * `run <cmd>` executes the SUT under test in a sub-shell so a
# non-zero exit cannot abort the harness; captures stdout, stderr,
# and exit status into globals (LAST_STDOUT, LAST_STDERR,
# LAST_STATUS).
# * Assertion failures are non-fatal -- they record the failure and
# let the rest of the test (and the rest of the file) continue, so
# a single broken expectation does not mask later regressions.

set -uo pipefail

# --- counters and state ---------------------------------------------
ASSERT_TESTS_TOTAL=0
ASSERT_TESTS_FAILED=0
ASSERT_CURRENT_TEST=""
ASSERT_CURRENT_FAILED=0
ASSERT_FAILED_NAMES=()

# Globals populated by `run`.
LAST_STDOUT=""
LAST_STDERR=""
LAST_STATUS=0

# --- test lifecycle -------------------------------------------------

# it <name>
# Begins a new test case. Always returns 0 so callers chain with `&&`.
# A test starts with no failures; failures accumulate as assertions
# fire.
it() {
    # Flush the previous test if any.
    _assert_finalize_current
    ASSERT_CURRENT_TEST="$1"
    ASSERT_CURRENT_FAILED=0
    ASSERT_TESTS_TOTAL=$((ASSERT_TESTS_TOTAL + 1))
    printf '  - %s ... ' "$ASSERT_CURRENT_TEST"
    return 0
}

# _assert_finalize_current
# Internal: emit PASS/FAIL for the in-flight test (called by `it` and
# at end-of-file via `assert_summary`).
_assert_finalize_current() {
    if [[ -z "$ASSERT_CURRENT_TEST" ]]; then
        return 0
    fi
    if [[ "$ASSERT_CURRENT_FAILED" -ne 0 ]]; then
        printf 'FAIL\n'
        ASSERT_TESTS_FAILED=$((ASSERT_TESTS_FAILED + 1))
        ASSERT_FAILED_NAMES+=("$ASSERT_CURRENT_TEST")
    else
        printf 'PASS\n'
    fi
    ASSERT_CURRENT_TEST=""
    ASSERT_CURRENT_FAILED=0
}

# assert_summary
# Emit the per-file totals on stdout and return 0 if all tests passed,
# 1 otherwise. Call at the end of every test file.
assert_summary() {
    _assert_finalize_current
    echo
    echo "  ----------------------------------------------------------"
    if [[ "$ASSERT_TESTS_FAILED" -eq 0 ]]; then
        echo "  ${ASSERT_TESTS_TOTAL} test(s) passed"
        return 0
    fi
    echo "  ${ASSERT_TESTS_FAILED}/${ASSERT_TESTS_TOTAL} test(s) FAILED:"
    local n
    for n in "${ASSERT_FAILED_NAMES[@]}"; do
        echo "    * ${n}"
    done
    return 1
}

# _assert_fail <reason>
# Record a failure on the current test. Multiple calls accumulate so
# you can see every broken expectation in one run.
_assert_fail() {
    ASSERT_CURRENT_FAILED=$((ASSERT_CURRENT_FAILED + 1))
    # Newline so the per-test PASS/FAIL is not glued onto the message.
    if [[ "$ASSERT_CURRENT_FAILED" -eq 1 ]]; then
        printf '\n'
    fi
    echo "      FAIL: $*" >&2
}

# --- command runner -------------------------------------------------

# run <cmd.>
# Execute a command in a subshell, capturing stdout/stderr/status into
# LAST_STDOUT / LAST_STDERR / LAST_STATUS so subsequent assertions can
# inspect the result without re-running the SUT.
run() {
    local out_file err_file
    out_file=$(mktemp)
    err_file=$(mktemp)
    # `set +e` so a failing SUT just sets LAST_STATUS; the harness
    # stays alive.
    set +e
    ( "$@" ) >"$out_file" 2>"$err_file"
    LAST_STATUS=$?
    set -e
    LAST_STDOUT=$(cat "$out_file")
    LAST_STDERR=$(cat "$err_file")
    rm -f "$out_file" "$err_file"
}

# --- value assertions -----------------------------------------------

assert_status() {
    local expected="$1"
    if [[ "$LAST_STATUS" -ne "$expected" ]]; then
        _assert_fail "expected exit status ${expected}, got ${LAST_STATUS} (stderr: ${LAST_STDERR})"
    fi
}

assert_stdout_equals() {
    local expected="$1"
    if [[ "$LAST_STDOUT" != "$expected" ]]; then
        _assert_fail "stdout mismatch
        expected: [${expected}]
        actual:   [${LAST_STDOUT}]"
    fi
}

assert_stdout_empty() {
    if [[ -n "$LAST_STDOUT" ]]; then
        _assert_fail "expected empty stdout, got: [${LAST_STDOUT}]"
    fi
}

assert_stdout_contains() {
    local needle="$1"
    if ! grep -qF -- "$needle" <<<"$LAST_STDOUT"; then
        _assert_fail "expected stdout to contain [${needle}], got: [${LAST_STDOUT}]"
    fi
}

assert_stdout_not_contains() {
    local needle="$1"
    if grep -qF -- "$needle" <<<"$LAST_STDOUT"; then
        _assert_fail "expected stdout NOT to contain [${needle}], got: [${LAST_STDOUT}]"
    fi
}

assert_stderr_contains() {
    local needle="$1"
    if ! grep -qF -- "$needle" <<<"$LAST_STDERR"; then
        _assert_fail "expected stderr to contain [${needle}], got: [${LAST_STDERR}]"
    fi
}

assert_stderr_not_contains() {
    local needle="$1"
    if grep -qF -- "$needle" <<<"$LAST_STDERR"; then
        _assert_fail "expected stderr NOT to contain [${needle}], got: [${LAST_STDERR}]"
    fi
}

assert_equals() {
    local expected="$1"
    local actual="$2"
    if [[ "$expected" != "$actual" ]]; then
        _assert_fail "expected [${expected}], got [${actual}]"
    fi
}

assert_not_equals() {
    local unexpected="$1"
    local actual="$2"
    if [[ "$unexpected" == "$actual" ]]; then
        _assert_fail "expected NOT [${unexpected}], got [${actual}]"
    fi
}

assert_file_exists() {
    local path="$1"
    if [[ ! -f "$path" ]]; then
        _assert_fail "expected file to exist: ${path}"
    fi
}

assert_file_empty() {
    local path="$1"
    if [[ ! -f "$path" ]]; then
        _assert_fail "expected file (empty) to exist: ${path}"
        return
    fi
    if [[ -s "$path" ]]; then
        _assert_fail "expected file ${path} to be empty, got: [$(cat "$path")]"
    fi
}

assert_file_contains() {
    local path="$1"
    local needle="$2"
    if [[ ! -f "$path" ]]; then
        _assert_fail "expected file to exist for contains check: ${path}"
        return
    fi
    if ! grep -qF -- "$needle" "$path"; then
        _assert_fail "expected ${path} to contain [${needle}], got: [$(cat "$path")]"
    fi
}

# --- kubectl-mock-aware assertions ----------------------------------
# These look at the call log produced by `lib/kubectl_mock.sh`
# (KUBECTL_CALLS_FILE).

# assert_kubectl_no_calls
# Verify the mock kubectl recorded zero invocations.
assert_kubectl_no_calls() {
    local path="${KUBECTL_CALLS_FILE:-}"
    if [[ -z "$path" ]]; then
        _assert_fail "KUBECTL_CALLS_FILE not set -- did you source lib/kubectl_mock.sh?"
        return
    fi
    if [[ -s "$path" ]]; then
        _assert_fail "expected zero kubectl calls, got:
$(cat "$path")"
    fi
}

# assert_kubectl_call_count <n>
assert_kubectl_call_count() {
    local expected="$1"
    local path="${KUBECTL_CALLS_FILE:-}"
    local actual=0
    if [[ -n "$path" && -f "$path" ]]; then
        actual=$(wc -l <"$path" | tr -d ' ')
    fi
    if [[ "$actual" -ne "$expected" ]]; then
        _assert_fail "expected ${expected} kubectl call(s), got ${actual}:
$(cat "$path" 2>/dev/null || true)"
    fi
}

# assert_kubectl_call <expected_line>
# Each recorded call is a single line consisting of all positional
# args joined by a single space (see kubectl_mock.sh). The match is
# exact equality of at least one recorded line.
assert_kubectl_call() {
    local expected="$1"
    local path="${KUBECTL_CALLS_FILE:-}"
    if [[ -z "$path" || ! -f "$path" ]]; then
        _assert_fail "no kubectl call log to search (expected: ${expected})"
        return
    fi
    if ! grep -qxF -- "$expected" "$path"; then
        _assert_fail "expected kubectl call [${expected}] not found. Actual log:
$(cat "$path")"
    fi
}

# assert_kubectl_call_contains <needle>
# Looser variant: substring match against any recorded call line.
assert_kubectl_call_contains() {
    local needle="$1"
    local path="${KUBECTL_CALLS_FILE:-}"
    if [[ -z "$path" || ! -f "$path" ]]; then
        _assert_fail "no kubectl call log to search (needle: ${needle})"
        return
    fi
    if ! grep -qF -- "$needle" "$path"; then
        _assert_fail "expected some kubectl call to contain [${needle}]. Actual log:
$(cat "$path")"
    fi
}
