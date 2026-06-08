#!/bin/bash
# Extracts a multi-line `data.<KEY>: |` script block from a Kubernetes
# ConfigMap YAML and writes the body to stdout. Pure bash + awk so the
# test suite does not depend on PyYAML / yq.
#
# Usage:
# extract_configmap_data /path/to/configmap.yaml KEY_NAME
#
# Limitations (acceptable for the cluster-validation-config layout):
# * The ConfigMap is the only document containing `data:` at column 0
# (or column 2 if nested under `metadata:`). We anchor on a `data:`
# line at column 0, then read keys at column 2.
# * The block scalar must use `|` (literal). `>` (folded) is not
# supported. The actual file uses `|` everywhere.
# * Trailing blank lines in the block are dropped by awk default;
# this matches kubectl's own normalization.

# extract_cronjob_orchestrator <yaml_path>
# Extract the multi-line bash body from the `submit-mpijob` container's
# `args: - |` block in cluster-validation-job.yaml. We anchor on the
# container name line so we only pick up that one args block (not the
# other init/launcher containers in the same file).
#
# Approach: find `name: submit-mpijob` (at any indent), then scan
# forward for the first `args:` line, then the `- |` line, then capture
# the body until indentation falls below the body's indent.
extract_cronjob_orchestrator() {
    local yaml_path="$1"
    if [[ ! -f "$yaml_path" ]]; then
        echo "extract_cronjob_orchestrator: file not found: $yaml_path" >&2
        return 2
    fi
    awk '
    function leading_spaces(s,    i) {
        for (i = 1; i <= length(s); i++) {
            if (substr(s, i, 1) != " ") return i - 1
        }
        return length(s)
    }
    BEGIN { state = 0; body_indent = -1 }
    # state 0: looking for the submit-mpijob container
    state == 0 && /name:[[:space:]]*submit-mpijob[[:space:]]*$/ {
        state = 1; next
    }
    # state 1: looking for the args: line
    state == 1 && /^[[:space:]]+args:[[:space:]]*$/ { state = 2; next }
    # state 2: looking for the literal-scalar opener `- |`
    state == 2 && /^[[:space:]]+-[[:space:]]+\|[[:space:]]*$/ {
        state = 3; body_indent = -1; next
    }
    # state 3: capture body lines until indent < body_indent (and line
    # is non-blank). Blank lines are preserved verbatim.
    state == 3 {
        if ($0 == "") { print ""; next }
        ls = leading_spaces($0)
        if (body_indent < 0) {
            body_indent = ls
        }
        if (ls < body_indent) {
            state = 4
            next
        }
        print substr($0, body_indent + 1)
    }
    ' "$yaml_path"
}

extract_configmap_data() {
    local yaml_path="$1"
    local key="$2"
    if [[ ! -f "$yaml_path" ]]; then
        echo "extract_configmap_data: file not found: $yaml_path" >&2
        return 2
    fi
    awk -v want="$key" '
    BEGIN { in_data = 0; in_block = 0; block_indent = 4 }
    function leading_spaces(s,    i) {
        for (i = 1; i <= length(s); i++) {
            if (substr(s, i, 1) != " ") return i - 1
        }
        return length(s)
    }
    # Start of the top-level data: block (column 0).
    /^data:[[:space:]]*$/ { in_data = 1; in_block = 0; next }
    # Leave data when we hit another top-level YAML key.
    in_data && /^[A-Za-z0-9_-]+:/ && !/^data:/ { in_data = 0; in_block = 0 }
    in_data {
        # Match ` KEY: |` -- exactly two-space indent for data keys.
        if (match($0, /^  ([A-Za-z0-9_-]+):[[:space:]]*\|[[:space:]]*$/, m)) {
            if (m[1] == want) {
                in_block = 1
                next
            } else if (in_block) {
                in_block = 0
            }
        }
        if (in_block) {
            # Blank lines stay part of the block.
            if ($0 == "") { print ""; next }
            ls = leading_spaces($0)
            # Any line less indented than block_indent ends the block
            # (matches YAML block-scalar semantics: a less-indented
            # non-blank line terminates the scalar).
            if (ls < block_indent) { in_block = 0; next }
            # Strip exactly block_indent leading spaces.
            print substr($0, block_indent + 1)
        }
    }
    ' "$yaml_path"
}
