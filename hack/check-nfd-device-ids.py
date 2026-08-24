#!/usr/bin/env python3
"""Verify that every copy of the AMD GPU PCI device-ID list agrees.

The list of device IDs that NFD matches on to apply the
``feature.node.kubernetes.io/amd-gpu`` / ``amd-vgpu`` labels is duplicated
across several hand-maintained files. Nothing in the build enforces that they
agree, and they have silently diverged before: commit 9c5ef17e added the Radeon
AI PRO R9700 (0x7551) to the NodeFeatureRule but not to the OpenShift install
docs, so OLM users on Radeon hardware got no GPU labels at all and every test
failed with "No nodes with AMD/GPU found" (GPUOP-1062).

Source of truth is hack/k8s-patch/template-patch/gpu-nfd-default-rule.yaml.
``make helm-k8s`` does ``rm -rf helm-charts-k8s`` and then repopulates it with
``cp hack/k8s-patch/template-patch/* helm-charts-k8s/templates/``, so the copy
under helm-charts-k8s is a build output -- editing it directly is futile.

Exit codes:
  0  all copies agree
  1  drift found (details on stdout)
  2  internal error: a file is missing or could not be parsed

Python 3 standard library only.
"""

from __future__ import annotations

import argparse
import os
import re
import sys

# A rule opens with a YAML sequence entry like "- name: amd-gpu".
RULE_NAME = re.compile(r"^\s*-\s+name:\s*([A-Za-z0-9_.-]+)\s*$")

# Device lists appear as "device: {op: In, value: [...]}". The list is written
# inline on one line in the NodeFeatureRule templates and spread over several
# lines in the docs, so the closing bracket may be on a later line.
#
# Only "device:" starts a capture -- "vendor: {op: In, value: ["1002"]}" sits
# right above it and 1002 would otherwise be collected as a device ID.
DEVICE_START = re.compile(r"device:\s*\{\s*op:\s*In\s*,\s*value:\s*\[")

# Device IDs are always quoted; unquoted 4-digit runs in trailing comments
# (e.g. "# RX 7900 XT") must not match.
HEX_ID = re.compile(r"\"([0-9a-fA-F]{4})\"")

# Fenced code blocks in Markdown.
FENCE = re.compile(r"^\s*```")

# The rules the documentation is expected to carry. The source of truth also
# defines convenience labels (amd-gpu-mi210, amd-gpu-mi300x) that the docs
# deliberately omit, so the docs are only held to this subset.
DOC_RULES = ("amd-gpu", "amd-vgpu")

SOURCE_OF_TRUTH = "hack/k8s-patch/template-patch/gpu-nfd-default-rule.yaml"

# Targets checked against the source of truth.
#
#   rules            -- which rule names to compare; None means "all rules
#                       defined in the source of truth"
#   min_blocks       -- for Markdown, how many fenced blocks are expected to
#                       carry the rules. Guards against a block being deleted
#                       wholesale, which would otherwise look like "in sync".
TARGETS = (
    {
        "path": "helm-charts-k8s/templates/gpu-nfd-default-rule.yaml",
        "rules": None,
        "min_blocks": None,
        "note": (
            "build output of 'make helm-k8s'. Drift means the wrong file was "
            "edited, or 'make helm-k8s' was not re-run before committing."
        ),
    },
    {
        "path": "docs/installation/openshift-olm.md",
        "rules": DOC_RULES,
        "min_blocks": 2,
        "note": (
            "copy-paste YAML for OpenShift users. The OLM bundle ships no "
            "NodeFeatureRule, so this doc is the only source users have."
        ),
    },
)


class ParseError(Exception):
    """Raised when a file cannot be parsed into rules."""


def parse_rules(lines):
    """Map rule name -> set of device IDs, for one YAML document.

    Returns an empty dict when the text defines no rules with device lists.
    """
    rules = {}
    current = None
    collecting = False

    for line in lines:
        if not collecting:
            match = RULE_NAME.match(line)
            if match:
                current = match.group(1)
                continue

            start = DEVICE_START.search(line)
            if not start:
                continue
            if current is None:
                # A device list outside any named rule; nothing to attribute
                # it to, so skip rather than guess.
                continue

            rest = line[start.end():]
            rules.setdefault(current, set()).update(HEX_ID.findall(rest))
            collecting = "]" not in rest
            continue

        # Continuation of a multi-line device list.
        head = line.split("]", 1)[0]
        rules[current].update(HEX_ID.findall(head))
        if "]" in line:
            collecting = False

    return rules


def parse_yaml_file(path, lines):
    """Parse a YAML file as a single document."""
    rules = parse_rules(lines)
    if not rules:
        raise ParseError(
            "no NodeFeatureRule device lists found. The file layout probably "
            "changed; update the parser in {}.".format(os.path.basename(__file__))
        )
    return [(1, rules)]


def parse_markdown_file(path, lines, wanted, min_blocks):
    """Parse each fenced code block in a Markdown file independently.

    The blocks are kept separate on purpose: merging them would let one block
    fall behind while the other stayed current, which is exactly the failure
    this script exists to catch.

    Returns a list of (start_line, rules) for blocks that carry any wanted rule.
    """
    blocks = []
    start = None
    body = []

    for index, line in enumerate(lines, start=1):
        if FENCE.match(line):
            if start is None:
                start, body = index, []
            else:
                blocks.append((start + 1, body))
                start = None
        elif start is not None:
            body.append(line)

    relevant = []
    for first_line, body in blocks:
        rules = parse_rules(body)
        if any(name in rules for name in wanted):
            relevant.append((first_line, rules))

    if min_blocks is not None and len(relevant) < min_blocks:
        raise ParseError(
            "expected at least {} code block(s) defining {}, found {}. A block "
            "was probably removed or restructured; if that was intentional, "
            "update min_blocks in {}.".format(
                min_blocks,
                " / ".join(wanted),
                len(relevant),
                os.path.basename(__file__),
            )
        )
    return relevant


def read_lines(root, rel):
    path = os.path.join(root, rel)
    if not os.path.isfile(path):
        raise ParseError("file not found: {}".format(rel))
    with open(path, "r", encoding="utf-8") as handle:
        return handle.read().splitlines()


def fmt_ids(ids):
    """Render IDs sorted, wrapped, and indented for a terminal."""
    ordered = sorted(ids)
    lines, row = [], []
    for value in ordered:
        row.append(value)
        if len(row) == 8:
            lines.append(" ".join(row))
            row = []
    if row:
        lines.append(" ".join(row))
    return lines


def compare(source, rules, wanted):
    """Yield (rule, missing, extra) for each compared rule with a difference."""
    for name in wanted:
        expected = source.get(name, set())
        actual = rules.get(name, set())
        missing = expected - actual
        extra = actual - expected
        if missing or extra:
            yield name, missing, extra


def main():
    parser = argparse.ArgumentParser(
        description="Check that AMD GPU PCI device-ID lists are in sync."
    )
    parser.add_argument(
        "--root",
        default=os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd(),
        help="repository root (default: $CLAUDE_PROJECT_DIR or cwd)",
    )
    parser.add_argument(
        "-q",
        "--quiet",
        action="store_true",
        help="print nothing when everything is in sync",
    )
    args = parser.parse_args()
    root = args.root

    try:
        source_lines = read_lines(root, SOURCE_OF_TRUTH)
        source = parse_yaml_file(SOURCE_OF_TRUTH, source_lines)[0][1]
    except ParseError as exc:
        print("ERROR: {}: {}".format(SOURCE_OF_TRUTH, exc), file=sys.stderr)
        return 2

    problems = []

    for target in TARGETS:
        rel = target["path"]
        wanted = target["rules"] or sorted(source)

        try:
            lines = read_lines(root, rel)
            if rel.endswith(".md"):
                units = parse_markdown_file(
                    rel, lines, wanted, target["min_blocks"]
                )
            else:
                units = parse_yaml_file(rel, lines)
        except ParseError as exc:
            print("ERROR: {}: {}".format(rel, exc), file=sys.stderr)
            return 2

        for first_line, rules in units:
            diffs = list(compare(source, rules, wanted))
            if diffs:
                problems.append((rel, first_line, target["note"], diffs))

    if not problems:
        if not args.quiet:
            print("OK: AMD GPU device IDs are in sync across all copies.")
        return 0

    out = sys.stdout
    print("FAIL: AMD GPU device-ID drift\n", file=out)
    print("source of truth:", file=out)
    print("  {}\n".format(SOURCE_OF_TRUTH), file=out)

    last = None
    for rel, first_line, note, diffs in problems:
        if rel != last:
            print("{}".format(rel), file=out)
            print("  ({})".format(note), file=out)
            last = rel
        print("  block at line {}:".format(first_line), file=out)
        for name, missing, extra in diffs:
            if missing:
                print(
                    "    rule '{}' missing {}:".format(name, len(missing)),
                    file=out,
                )
                for row in fmt_ids(missing):
                    print("      {}".format(row), file=out)
            if extra:
                print(
                    "    rule '{}' has {} not in source:".format(name, len(extra)),
                    file=out,
                )
                for row in fmt_ids(extra):
                    print("      {}".format(row), file=out)
        print("", file=out)

    print("Add the missing IDs, or if a new GPU was added, update every copy.", file=out)
    return 1


if __name__ == "__main__":
    sys.exit(main())
