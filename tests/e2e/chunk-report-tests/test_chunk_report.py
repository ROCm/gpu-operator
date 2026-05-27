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

import os
import sys
import tempfile
import textwrap
import unittest

THIS_DIR = os.path.dirname(__file__)
PARENT = os.path.dirname(THIS_DIR)
sys.path.insert(0, PARENT)

from chunk_report import fmt_duration, parse_chunk_log, render, strip_ansi


# Fixture catalog: name -> raw log body. Synthesized inline so the repo carries
# zero *.log artifacts (per PR #1468 review feedback).
FIXTURES = {
    "cluster-rbac-allpass": textwrap.dedent('''\
        ==================== chunk: cluster-rbac ====================
        === RUN   Test
        time="2026-05-16T10:00:00-07:00" level=info msg="==================== Starting Test: E2ESuite.TestKubeRbacProxyClusterIP ====================" func="..."
        time="2026-05-16T10:02:30-07:00" level=info msg="==================== Finished Test: E2ESuite.TestKubeRbacProxyClusterIP ====================" func="..."
        time="2026-05-16T10:02:30-07:00" level=info msg="==================== Starting Test: E2ESuite.TestServiceMonitor ====================" func="..."
        time="2026-05-16T10:05:00-07:00" level=info msg="==================== Finished Test: E2ESuite.TestServiceMonitor ====================" func="..."
        OOPS: 2 passed, 0 skipped, 0 FAILED
        --- PASS: Test (300.00s)
        PASS
        ok      github.com/ROCm/gpu-operator/tests/e2e  300.234s
        '''),
    "cluster-core-mixed": textwrap.dedent('''\
        ==================== chunk: cluster-core ====================
        === RUN   Test
        time="2026-05-16T10:00:00-07:00" level=info msg="==================== Starting Test: E2ESuite.TestBasicSkipDriverInstall ====================" func="..."
        time="2026-05-16T10:00:32-07:00" level=info msg="==================== Finished Test: E2ESuite.TestBasicSkipDriverInstall ====================" func="..."
        time="2026-05-16T10:00:32-07:00" level=info msg="==================== Starting Test: E2ESuite.TestDeployment ====================" func="..."
        time="2026-05-16T10:08:42-07:00" level=info msg="==================== Finished Test: E2ESuite.TestDeployment ====================" func="..."
        ----------------------------------------------------------------------
        FAIL: cluster_test.go:869: E2ESuite.TestDeployment
        cluster_test.go:877:
            s.checkNodeLabellerStatus(s.ns, c, devCfg)
        ... Error: infra failure: node-labeller init container stuck waiting for amdgpu module
        time="2026-05-16T10:08:42-07:00" level=info msg="==================== Starting Test: E2ESuite.TestDeploymentWithPreInstalledKMMAndNFD ====================" func="..."
        time="2026-05-16T10:09:10-07:00" level=info msg="SKIPPED_TEST: E2ESuite.TestDeploymentWithPreInstalledKMMAndNFD | Skipping for non amd gpu testbed" func="..."
        time="2026-05-16T10:09:10-07:00" level=info msg="==================== Finished Test: E2ESuite.TestDeploymentWithPreInstalledKMMAndNFD ====================" func="..."
        OOPS: 1 passed, 1 skipped, 1 FAILED
        --- FAIL: Test (550.00s)
        FAIL
        FAIL    github.com/ROCm/gpu-operator/tests/e2e  550.123s
        '''),
    "kubevirt-allskip": textwrap.dedent('''\
        ==================== chunk: kubevirt ====================
        === RUN   Test
        time="2026-05-16T10:00:00-07:00" level=info msg="==================== Starting Test: E2ESuite.TestVFPassthrough ====================" func="..."
        time="2026-05-16T10:00:05-07:00" level=info msg="SKIPPED_TEST: E2ESuite.TestVFPassthrough | Skipping for non amd gpu testbed" func="..."
        time="2026-05-16T10:00:05-07:00" level=info msg="==================== Finished Test: E2ESuite.TestVFPassthrough ====================" func="..."
        time="2026-05-16T10:00:05-07:00" level=info msg="==================== Starting Test: E2ESuite.TestPFPassthrough ====================" func="..."
        time="2026-05-16T10:00:10-07:00" level=info msg="SKIPPED_TEST: E2ESuite.TestPFPassthrough | Skipping for non amd gpu testbed" func="..."
        time="2026-05-16T10:00:10-07:00" level=info msg="==================== Finished Test: E2ESuite.TestPFPassthrough ====================" func="..."
        OOPS: 0 passed, 2 skipped, 0 FAILED
        PASS
        ok      github.com/ROCm/gpu-operator/tests/e2e  10.000s
        '''),
    "crash-no-finish": textwrap.dedent('''\
        ==================== chunk: dra ====================
        === RUN   Test
        time="2026-05-16T10:00:00-07:00" level=info msg="==================== Starting Test: E2ESuite.TestDRADriverDaemonSetReadyAndCleanup ====================" func="..."
        panic: test timed out after 20m0s
        running 1 tests
        '''),
}


class _FixtureMixin:
    '''Materialize FIXTURES[name] into a tempfile so the parser can open it.

    The tempfile basename is "<name>.log" (under a per-test temp dir) so
    `chunk.name` ("<basename-without-.log>") still equals the fixture name
    and existing assertions on c.name continue to work unchanged.
    '''

    def _fix(self, name):
        if name not in FIXTURES:
            raise KeyError('unknown fixture: %r' % name)
        tmpdir = tempfile.mkdtemp(prefix='chunkreport_fix_')
        self.addCleanup(self._rmtree, tmpdir)
        path = os.path.join(tmpdir, name + '.log')
        with open(path, 'w', encoding='utf-8') as f:
            f.write(FIXTURES[name])
        return path

    @staticmethod
    def _rmtree(d):
        for root, dirs, files in os.walk(d, topdown=False):
            for f in files:
                try:
                    os.unlink(os.path.join(root, f))
                except OSError:
                    pass
            for sub in dirs:
                try:
                    os.rmdir(os.path.join(root, sub))
                except OSError:
                    pass
        try:
            os.rmdir(d)
        except OSError:
            pass


class TestParser(_FixtureMixin, unittest.TestCase):

    def test_allpass(self):
        c = parse_chunk_log(self._fix("cluster-rbac-allpass"))
        self.assertEqual(c.name, "cluster-rbac-allpass")
        self.assertEqual(len(c.tests), 2)
        self.assertEqual({t.status for t in c.tests}, {"PASS"})
        self.assertEqual(c.totals["PASS"], 2)
        self.assertEqual(c.totals["FAIL"], 0)
        self.assertEqual(c.totals["SKIP"], 0)
        self.assertEqual(c.totals["CRASH"], 0)

    def test_mixed(self):
        c = parse_chunk_log(self._fix("cluster-core-mixed"))
        self.assertEqual(c.totals["PASS"], 1)
        self.assertEqual(c.totals["FAIL"], 1)
        self.assertEqual(c.totals["SKIP"], 1)
        fail = next(t for t in c.tests if t.status == "FAIL")
        self.assertEqual(fail.name, "E2ESuite.TestDeployment")
        self.assertIn("amdgpu module", fail.reason)

    def test_skips_grouped(self):
        c = parse_chunk_log(self._fix("kubevirt-allskip"))
        self.assertEqual(c.totals["SKIP"], 2)
        reasons = [t.reason for t in c.tests if t.status == "SKIP"]
        self.assertTrue(all("Skipping for non amd gpu testbed" in r for r in reasons))

    def test_crash(self):
        c = parse_chunk_log(self._fix("crash-no-finish"))
        self.assertEqual(c.totals["CRASH"], 1)
        crash = next(t for t in c.tests if t.status == "CRASH")
        self.assertEqual(crash.name, "E2ESuite.TestDRADriverDaemonSetReadyAndCleanup")


class TestWallClock(_FixtureMixin, unittest.TestCase):

    def test_first_and_last_test_timestamps(self):
        c = parse_chunk_log(self._fix("cluster-rbac-allpass"))
        # cluster-rbac-allpass: first start 10:00:00, last finish 10:05:00 = 5m
        self.assertEqual(c.wall_seconds, 300)

    def test_wall_clock_missing(self):
        c = parse_chunk_log(self._fix("crash-no-finish"))
        # Started but never finished; wall is from first start to last log entry
        # crash fixture has one start at 10:00:00 only -> wall = 0
        self.assertEqual(c.wall_seconds, 0)


class TestANSI(unittest.TestCase):

    def test_strip_ansi(self):
        self.assertEqual(strip_ansi("\033[31mfoo\033[0m"), "foo")
        self.assertEqual(strip_ansi("plain"), "plain")

    def test_fmt_duration(self):
        self.assertEqual(fmt_duration(45), "45s")
        self.assertEqual(fmt_duration(125), "2m05s")
        self.assertEqual(fmt_duration(3725), "1h02m")


class TestRender(_FixtureMixin, unittest.TestCase):

    def test_banner_counts(self):
        chunks = [
            parse_chunk_log(self._fix("cluster-rbac-allpass")),
            parse_chunk_log(self._fix("cluster-core-mixed")),
            parse_chunk_log(self._fix("kubevirt-allskip")),
        ]
        out = render(chunks)
        plain = strip_ansi(out)
        # 2 passed (rbac) + 1 passed (core) + 0 passed (kubevirt) = 3
        # 3 skipped (kubevirt 2 + core 1)
        # 1 failed (core)
        self.assertIn("3 passed", plain)
        self.assertIn("3 skipped", plain)
        self.assertIn("1 failed", plain)

    def test_chunk_section_collapsed_on_pass(self):
        chunks = [parse_chunk_log(self._fix("cluster-rbac-allpass"))]
        plain = strip_ansi(render(chunks))
        self.assertIn("cluster-rbac-allpass", plain)
        self.assertIn("2/2 PASS", plain)
        # Individual test names NOT shown for fully-passing chunk
        self.assertNotIn("TestKubeRbacProxyClusterIP", plain)
        self.assertNotIn("TestServiceMonitor", plain)

    def test_chunk_section_expanded_on_fail(self):
        chunks = [parse_chunk_log(self._fix("cluster-core-mixed"))]
        plain = strip_ansi(render(chunks))
        self.assertIn("TestDeployment", plain)
        self.assertIn("FAIL", plain)
        self.assertIn("amdgpu module", plain)


if __name__ == "__main__":
    unittest.main()
