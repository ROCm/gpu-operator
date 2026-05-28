#!/usr/bin/env python3

'''
 Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
'''

"""chunk_report: render a pretty per-test verdict from tests/e2e/chunk-logs/*.log.

Pure Python 3 stdlib. Discovery-based: adding a new test or chunk requires no
edits here as long as the new chunk's stdout is tee'd into chunk-logs/<name>.log
by the Makefile recipe.
"""

import os
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime
from typing import List, Dict, Optional

# Marker regexes (sourced from internal/plugin/plugin.go and
# tests/e2e/e2e_test.go::SetUpTest/TearDownTest).
RE_START = re.compile(r"==================== Starting Test: (\S+) ====================")
RE_END = re.compile(r"==================== Finished Test: (\S+) ====================")
RE_SKIP = re.compile(r"SKIPPED_TEST: (\S+) \| ([^\"]+?)(?=\"|$)")
RE_FAIL = re.compile(r"^FAIL: \S+: (\S+)$")
RE_ERROR = re.compile(r"^\.\.\. Error: (.+)$")
RE_TIME = re.compile(r'time="([^"]+)"')


@dataclass
class TestRecord:
    name: str
    status: str  # PASS | FAIL | SKIP | CRASH
    reason: str = ""


@dataclass
class Chunk:
    name: str
    tests: List[TestRecord] = field(default_factory=list)
    totals: Dict[str, int] = field(default_factory=lambda: {"PASS": 0, "FAIL": 0, "SKIP": 0, "CRASH": 0})
    wall_seconds: int = 0


def parse_chunk_log(path: str) -> Chunk:
    """Parse one chunk's captured stdout into a structured Chunk.

    State model:
      - `current_inflight` tracks the test between a START and END marker.
        On END it gets committed; on the next START before END it gets
        committed as CRASH.
      - `pending_reason_target` tracks a TestRecord that's waiting for its
        `... Error: ...` line. This is separate from current_inflight
        because gocheck emits the `FAIL:` and `... Error:` lines AFTER the
        `Finished Test:` marker for the SAME test — by the time we see
        them, the test is already committed as PASS in chunk.tests, and
        we retro-fix it. The reason-target then points at the already-
        committed record, NOT at current_inflight.
    """
    chunk_name = os.path.splitext(os.path.basename(path))[0]
    chunk = Chunk(name=chunk_name)

    current_inflight: Optional[TestRecord] = None
    pending_reason_target: Optional[TestRecord] = None
    first_ts: Optional[datetime] = None
    last_ts: Optional[datetime] = None

    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.rstrip("\n")

            if m := RE_TIME.search(line):
                try:
                    ts = datetime.fromisoformat(m.group(1).replace("Z", "+00:00"))
                    if first_ts is None:
                        first_ts = ts
                    last_ts = ts
                except ValueError:
                    pass

            if m := RE_START.search(line):
                if current_inflight is not None:
                    # Previous test never finished -> CRASH
                    current_inflight.status = "CRASH"
                    current_inflight.reason = current_inflight.reason or "test process exited before Finished marker"
                    _commit(chunk, current_inflight)
                current_inflight = TestRecord(name=m.group(1), status="RUNNING")
                continue

            if m := RE_SKIP.search(line):
                # SKIPPED_TEST may arrive before or after Finished depending on logrus ordering.
                if current_inflight is None or current_inflight.name != m.group(1):
                    current_inflight = TestRecord(name=m.group(1), status="SKIP", reason=m.group(2).strip())
                else:
                    current_inflight.status = "SKIP"
                    current_inflight.reason = m.group(2).strip()
                continue

            if m := RE_FAIL.match(line):
                # gocheck emits FAIL: AFTER the Finished marker. The test is
                # already in chunk.tests as PASS; retro-fix it.
                name = m.group(1)
                for t in reversed(chunk.tests):
                    if t.name == name and t.status == "PASS":
                        t.status = "FAIL"
                        chunk.totals["PASS"] -= 1
                        chunk.totals["FAIL"] += 1
                        pending_reason_target = t
                        break
                else:
                    # Rare: test body Fatalf'd before END marker -> still in_flight.
                    if current_inflight is not None and current_inflight.name == name:
                        current_inflight.status = "FAIL"
                        pending_reason_target = current_inflight
                continue

            if pending_reason_target is not None and (m := RE_ERROR.match(line)):
                pending_reason_target.reason = m.group(1).strip()
                pending_reason_target = None
                continue

            if RE_END.search(line):
                if current_inflight is not None:
                    if current_inflight.status == "RUNNING":
                        current_inflight.status = "PASS"
                    _commit(chunk, current_inflight)
                    current_inflight = None
                continue

    # End of log: any test still in_flight crashed (panic/timeout).
    if current_inflight is not None:
        if current_inflight.status == "RUNNING":
            current_inflight.status = "CRASH"
            current_inflight.reason = current_inflight.reason or "test process exited before Finished marker"
        _commit(chunk, current_inflight)

    if first_ts and last_ts:
        chunk.wall_seconds = int((last_ts - first_ts).total_seconds())

    return chunk


def _commit(chunk: Chunk, test: TestRecord) -> None:
    chunk.tests.append(test)
    chunk.totals[test.status] = chunk.totals.get(test.status, 0) + 1


# ANSI palette. When NO_COLOR=1 or stdout is not a TTY, all helpers return the
# raw text. The renderer also emits a plain-text mirror to report.txt, where
# we deliberately strip ANSI regardless.
_ANSI_ON = sys.stdout.isatty() and os.environ.get("NO_COLOR") != "1"


def _ansi(code: str, s: str) -> str:
    if not _ANSI_ON:
        return s
    return f"\033[{code}m{s}\033[0m"


def green(s: str) -> str: return _ansi("32", s)
def red(s: str) -> str: return _ansi("31", s)
def yellow(s: str) -> str: return _ansi("33", s)
def magenta(s: str) -> str: return _ansi("35", s)
def dim(s: str) -> str: return _ansi("2", s)
def bold(s: str) -> str: return _ansi("1", s)


def fmt_duration(seconds: int) -> str:
    if seconds < 60:
        return f"{seconds}s"
    m, s = divmod(seconds, 60)
    if m < 60:
        return f"{m}m{s:02d}s"
    h, m = divmod(m, 60)
    return f"{h}h{m:02d}m"


def strip_ansi(s: str) -> str:
    return re.sub(r"\033\[[0-9;]*m", "", s)


RULE = "═" * 75


def render(chunks: List[Chunk], baseline_path: str = "", artifacts_dir: str = "") -> str:
    """Render the verdict. Returns a single string suitable for stdout."""
    lines: List[str] = []
    totals = {"PASS": 0, "FAIL": 0, "SKIP": 0, "CRASH": 0}
    wall = 0
    for c in chunks:
        for k, v in c.totals.items():
            totals[k] = totals.get(k, 0) + v
        wall += c.wall_seconds

    # Banner
    lines.append(bold(RULE))
    lines.append(bold(" operator-e2e-sim verdict"))
    lines.append(bold(RULE))
    summary = (
        f" {green('✓ ' + str(totals['PASS']) + ' passed')}   "
        f"{yellow('⊘ ' + str(totals['SKIP']) + ' skipped')}   "
        f"{red('✗ ' + str(totals['FAIL']) + ' failed')}   "
        f"{magenta('⌁ ' + str(totals['CRASH']) + ' crashed')}"
    )
    lines.append(summary)
    lines.append(f" Wall: {fmt_duration(wall)}")
    if baseline_path and os.path.isfile(baseline_path):
        lines.append(f" Runner pre-flight: see {baseline_path}")
    lines.append(bold(RULE))
    lines.append("")

    # Per-chunk sections
    for c in chunks:
        lines.extend(_render_chunk(c, artifacts_dir))
        lines.append("")

    # Footer
    failed_chunks = [c.name for c in chunks if c.totals["FAIL"] > 0 or c.totals["CRASH"] > 0]
    passed_chunks = [c.name for c in chunks if c.totals["FAIL"] == 0 and c.totals["CRASH"] == 0]
    lines.append(bold(RULE))
    if failed_chunks:
        lines.append(
            f" {green(str(len(passed_chunks)) + ' chunks PASS')}, "
            f"{red(str(len(failed_chunks)) + ' chunks FAIL')}: "
            f"{', '.join(failed_chunks)}"
        )
    else:
        lines.append(f" {green('All ' + str(len(chunks)) + ' chunks PASS')}")
    lines.append(bold(RULE))

    return "\n".join(lines) + "\n"


def _render_chunk(c: Chunk, artifacts_dir: str) -> List[str]:
    out: List[str] = []
    n_pass = c.totals["PASS"]
    n_fail = c.totals["FAIL"]
    n_skip = c.totals["SKIP"]
    n_crash = c.totals["CRASH"]
    failing = n_fail + n_crash
    total = sum(c.totals.values())
    duration = fmt_duration(c.wall_seconds)
    name_col = bold(f"{c.name:40s}")

    if failing == 0:
        # Collapsed: one-line summary
        if total == 0:
            out.append(f" ▸ {name_col} {dim('(no tests)')}")
        elif n_skip == total:
            label = yellow(f"⊘ {total}/{total} SKIP")
            out.append(f" ▸ {name_col} {label}  ({duration})")
        else:
            label = green(f"✓ {n_pass}/{total} PASS")
            out.append(f" ▸ {name_col} {label}  ({duration})")
        return out

    # Expanded: header + each FAIL/CRASH + grouped skips + pass count
    fail_label = red(f"✗ {failing}/{total} FAIL")
    out.append(f" ▸ {name_col} {fail_label}  ({duration})")

    for t in c.tests:
        if t.status in ("FAIL", "CRASH"):
            tag = red("✗ FAIL ") if t.status == "FAIL" else magenta("⌁ CRASH")
            out.append(f"   {tag} {t.name}")
            if t.reason:
                for r_line in t.reason.split("\n"):
                    out.append(f"       {r_line}")
            if artifacts_dir:
                drv = os.path.join(artifacts_dir, t.name.split(".")[-1], "driver-state.txt")
                kwl = os.path.join(artifacts_dir, t.name.split(".")[-1], "kmm-worker-pods.log")
                if os.path.isfile(drv):
                    out.append(f"       {dim('↳ driver-state:    ' + drv)}")
                if os.path.isfile(kwl):
                    out.append(f"       {dim('↳ kmm-worker logs: ' + kwl)}")

    # Group skips by reason
    skip_groups: Dict[str, int] = {}
    for t in c.tests:
        if t.status == "SKIP":
            r = t.reason or "(no reason)"
            skip_groups[r] = skip_groups.get(r, 0) + 1
    if skip_groups:
        parts = [f'{n}× "{r}"' for r, n in sorted(skip_groups.items(), key=lambda kv: -kv[1])]
        skip_label = yellow(f"⊘ {n_skip} skipped")
        out.append(f"   {skip_label}   ({', '.join(parts)})")

    if n_pass > 0:
        pass_label = green(f"✓ {n_pass} passed")
        out.append(f"   {pass_label}")

    return out


def main(argv: List[str]) -> int:
    if len(argv) < 2:
        log_dir = "chunk-logs"
    else:
        log_dir = argv[1]

    if not os.path.isdir(log_dir):
        print(f"chunk-report: log directory {log_dir!r} not found", file=sys.stderr)
        return 2

    log_files = sorted(
        os.path.join(log_dir, f)
        for f in os.listdir(log_dir)
        if f.endswith(".log") and f != "report.log"
    )
    if not log_files:
        print(f"chunk-report: no *.log files in {log_dir!r}", file=sys.stderr)
        return 2

    chunks = [parse_chunk_log(p) for p in log_files]

    baseline = os.path.join("e2e-artifacts", "_baseline", "runner-state.txt")
    artifacts = "e2e-artifacts"

    colored = render(chunks, baseline_path=baseline, artifacts_dir=artifacts)
    sys.stdout.write(colored)

    plain_path = os.path.join(log_dir, "report.txt")
    with open(plain_path, "w", encoding="utf-8") as f:
        f.write(strip_ansi(colored))

    failed = sum(c.totals["FAIL"] + c.totals["CRASH"] for c in chunks)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
