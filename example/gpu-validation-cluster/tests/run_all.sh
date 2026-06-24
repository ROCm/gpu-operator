#!/bin/bash
# Test runner entry point for the gpu-validation-cluster bash unit
# tests. Discovers every `test_*.sh` peer file, runs them
# sequentially, and aggregates results.
#
# Exit code:
# 0 -- every test file reported 0 failures
# 1 -- at least one test file reported failures
#
# Usage:
# ./tests/run_all.sh # run all
# ./tests/run_all.sh test_xyz.sh # run a specific file
#
# Each test file is its own bash process so a fatal error in one file
# cannot take the rest of the suite down with it.

set -uo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$TEST_DIR"

if [[ $# -gt 0 ]]; then
    FILES=("$@")
else
    # Sort for deterministic ordering across hosts.
    mapfile -t FILES < <(ls test_*.sh 2>/dev/null | sort)
fi

if [[ "${#FILES[@]}" -eq 0 ]]; then
    echo "run_all.sh: no test_*.sh files found in ${TEST_DIR}" >&2
    exit 1
fi

TOTAL_FILES=0
FAILED_FILES=0
FAILED_NAMES=()

for f in "${FILES[@]}"; do
    TOTAL_FILES=$((TOTAL_FILES + 1))
    if [[ ! -f "$f" ]]; then
        echo "run_all.sh: skipping missing file $f" >&2
        FAILED_FILES=$((FAILED_FILES + 1))
        FAILED_NAMES+=("$f (missing)")
        continue
    fi
    if ! bash "$f"; then
        FAILED_FILES=$((FAILED_FILES + 1))
        FAILED_NAMES+=("$f")
    fi
    echo
done

echo "=================================================================="
echo "  Suite summary"
echo "=================================================================="
echo "  Files run:    ${TOTAL_FILES}"
echo "  Files passed: $((TOTAL_FILES - FAILED_FILES))"
echo "  Files failed: ${FAILED_FILES}"
if [[ "${FAILED_FILES}" -gt 0 ]]; then
    echo "  Failed files:"
    for n in "${FAILED_NAMES[@]}"; do
        echo "    * ${n}"
    done
    exit 1
fi
exit 0
