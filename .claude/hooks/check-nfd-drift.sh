#!/usr/bin/env bash
# PostToolUse hook: report when an edit leaves the AMD GPU PCI device-ID lists
# out of sync across their copies.
#
# The device-ID list is duplicated across the NodeFeatureRule template, its
# generated helm copy, and two copy-paste YAML blocks in the OpenShift install
# docs. Nothing in the build enforces agreement, and they have diverged before:
# commit 9c5ef17e added the Radeon AI PRO R9700 (0x7551) to the rule but not to
# the docs, so OLM users on Radeon hardware got no GPU labels at all
# (GPUOP-1062). This hook catches that at the moment it is introduced, rather
# than at PR time.
#
# The same check runs as 'make check-nfd-device-ids', which is what covers
# contributors who are not using Claude Code.
#
# Failure mode: this hook FAILS OPEN. If python3 is missing or the checker is
# absent, we exit 0 so editing is never impeded. A silent hook is acceptable
# because the Makefile target is the real backstop.
set -uo pipefail

input=$(cat)

# Fast path: almost every edit touches none of the tracked files. Bail before
# spawning python3 unless a tracked filename appears somewhere in the payload.
case "$input" in
  *gpu-nfd-default-rule*|*openshift-olm.md*) ;;
  *) exit 0 ;;
esac

root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
checker="$root/hack/check-nfd-device-ids.py"

[[ -f "$checker" ]] || exit 0
command -v python3 >/dev/null 2>&1 || exit 0

output=$(python3 "$checker" --root "$root" --quiet 2>&1)
case $? in
  1)
    printf '%s\n\n' "$output" >&2
    printf 'The file you just edited is one of several copies of the AMD GPU device-ID list.\n' >&2
    printf 'Update the other copies so they match, then re-run: make check-nfd-device-ids\n' >&2
    exit 2
    ;;
  2)
    printf '%s\n\n' "$output" >&2
    printf 'The device-ID drift checker could not parse one of its inputs, so it is\n' >&2
    printf 'currently blind. Fix hack/check-nfd-device-ids.py before relying on it.\n' >&2
    exit 2
    ;;
esac

exit 0
