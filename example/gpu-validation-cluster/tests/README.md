# gpu-validation-cluster bash unit tests

These tests exercise the bash artifacts shipped by the
`cluster-validation-config` ConfigMap and the
`cluster-validation-cron-job` orchestrator without requiring a real
Kubernetes cluster. They run anywhere `bash`, `awk`, `sed`, and
`grep` are available.

## What is covered

- **`PHASE3_CHECK_SCRIPT` + `PHASE3_SCRIPT`** —
  per-node NIC health gate (NIC count via `lspci`, link state via
  `ip link`, RDMA link state via `rdma link show`, GID table via
  `ibv_devinfo`) plus the outer Job submit/wait driver:
  - `shellcheck PHASE3_CHECK_SCRIPT` (skipped automatically in CI
    images that don't ship shellcheck).
  - In-Job CHECK pass case: all 4 checks pass -> `amd.com/nic-health=passed`
    self-label, exit 0, no failure annotation.
  - NIC count mismatch -> `=failed`, reason
    `nic-count:expected=.,actual=.`.
  - One NIC link DOWN -> `=failed`, failed-nics carries the iface
    name, reason includes `link-state:<iface>=DOWN`.
  - One RDMA link in `INIT` state -> `=failed`, reason `rdma-state:`.
  - One device with an empty GID table -> `=failed`, reason
    `gid-table:<dev>=0`.
  - `ibv_devinfo` unresponsive on one device -> `=failed`, reason
    `ibv-devinfo:<dev>=unresponsive`.
  - Partial failure (only Check 3 fails) -> failure annotation contains
    only the rdma-state reason, never a spurious entry from a passing
    check class.
  - Annotation size truncation -- worst-case all-NIC failure values
    are clamped to `PHASE3_ANNOTATION_MAX_BYTES` (default 250) so the
    node object cannot blow the 256 KiB annotation budget.
  - `NODE_NAME` unset -> exit 2, zero kubectl side effects.
  - Outer driver `PHASE3_SCRIPT`:
    - empty input list -> no-op, return 0.
    - `SKIP_NIC_VALIDATION=true` short-circuit (case-insensitive) ->
      every input node pass-labeled, no Jobs created, no kubectl
      get/apply work.
    - Missing required env -> all input nodes labeled failed with
      `failure-reason=phase3-missing-env:.`; no Jobs submitted.
    - Missing job template -> all input nodes labeled failed with
      `failure-reason=job-template-missing`.
    - `kubectl apply` failure -> per-node `failure-reason=job-creation-failed`,
      no poll work for the affected job.
    - `PHASE3_JOB_WAIT_TIME=0` + no conditions -> `=failed`,
      `failure-reason=nic-not-allocated`, hung Job explicitly deleted
      at cleanup.
    - `Complete=True` / `Failed=True` -> the orchestrator must NOT
      write the node label (in-pod kubectl owns the passed/failed
      labels per design §4 step 4); only submit-failed / timeout
      cases get orchestrator-side labels.
    - Parallel-submit ordering: every `kubectl apply` precedes any
      `kubectl get job` poll (no per-node serialization).
    - `PHASE_NODES` env-var fallback when no positional args.
  - Sample shim fixtures live under `tests/fixtures/phase3/`
    (lspci pass / count-mismatch / empty; ip link pass / one-down;
    rdma link pass / one-init; ibv_devices listing; ibv_devinfo
    pass / empty-gid).

- **`PHASE2_SCRIPT` driver** — orchestrates the
  per-node intra-node RCCL `all_reduce_perf` Job:
  - pass case: `Complete=True` + log with `Avg bus bandwidth` line ->
    `amd.com/gpu-mesh-validation=passed` + `measured-bw` annotation.
  - `bus-bw-below-threshold` fail: `Failed=True` + log marker
    `phase2 bandwidth below threshold` -> failed label,
    `failure-reason=bus-bw-below-threshold`, measured-bw annotation.
  - `rccl-crash` fail: `Failed=True` + log marker
    `phase2 mpirun exited` -> failed label,
    `failure-reason=rccl-crash`.
  - Failed without marker -> default `failure-reason=rccl-crash`
    (design §6 default).
  - `timeout`: no Job conditions seeded + `PHASE2_JOB_WAIT_TIME=0`
    -> failed label, `failure-reason=timeout`, hung Job explicitly
    deleted at cleanup.
  - `SKIP_GPU_MESH_VALIDATION=true` short-circuit: every input node
    pass-labeled, no Jobs created, no kubectl get/logs/apply work.
  - Threshold-too-high inject (`PHASE2_BW_THRESHOLD=9999`): contract
    pin — `PHASE2_SCRIPT` never re-runs the validator, so the inject
    is observed only inside the Job container; classification by log
    marker is unchanged.
  - Missing-env fast-fail (e.g. `ROCE_WORKLOAD_IMAGE` unset).
  - Missing job template -> all input nodes labeled failed with
    `failure-reason=job-template-missing`.
  - Parallel-submit ordering: every `kubectl apply` precedes any
    `kubectl get job` poll (no per-node serialization).
  - `kubectl apply` failure -> per-node `failure-reason=job-creation-failed`,
    no poll/log work for the affected job.
  - `PHASE_NODES` env-var fallback when no positional args.
  - `PHASE2_RCCL_ENV_VARS` ConfigMap value contains no IB / fabric
    tunables (intra-node only — TC4 in `-test-plan.md`).
  - Sample `phase2.log` fixtures live under `tests/fixtures/phase2/`
    (pass / bw-below-threshold / rccl-crash / failed-no-marker).

- **`PHASE_NODE_LABEL_SCRIPT` helper library**
  against a recording `kubectl` mock:
  - `label_phase_passed` writes the `<key>=passed` label with
    `--overwrite`.
  - `label_phase_failed` writes the `<key>=failed` label AND a
    `<key>-failure-reason` annotation.
  - `annotate_phase_value` writes
    `<phase_key>-<sub_key>=<value>` annotations.
  - `filter_passed_nodes` returns the subset of input nodes whose
    label is exactly `passed`, preserving input order.
  - Argument validation (empty args / wrong arity must return
    non-zero with no kubectl side effects).
  - `kubectl` failure propagation (return non-zero when the
    underlying call fails).
  - Contract invariants: `--overwrite` on every write, diagnostics
    to stderr only.

- **`PHASE4_DRIVER_SCRIPT`** —
  pairwise per-rail RDMA bandwidth test driver. Covers:
  - Round-robin pairing (even input -> N/2 pairs; odd input -> N/2
    pairs + the trailing node tagged `unpaired=true`).
  - Empty / single-node input fast paths.
  - `SKIP_RAIL_BANDWIDTH_TEST=true` (case-insensitive) short-circuit:
    every input node pass-labeled, no Jobs created.
  - Missing required env -> all input nodes labeled failed with
    `failure-reason=phase4-missing-env:.`; no Jobs submitted.
  - Missing job templates -> all input nodes labeled failed with
    `failure-reason=job-template-missing`.
  - All-rails-pass single pair -> both nodes labeled passed with
    per-rail BW annotations (`amd.com/rail-bandwidth-rail-N=<bw>`)
    and a `peer=<other-node>` diagnostic annotation.
  - Single rail below `PHASE4_BW_THRESHOLD` -> `failed-rails=<idx>`
    annotation; per-rail BW preserved for diagnostics.
  - All rails below threshold -> `failed-rails=0,1,.,7`.
  - `ib_write_bw` crashed (client Job `Failed=True` + no BW line in
    log) -> rail recorded `reason=ib-write-bw-crashed`.
  - `ib_write_bw` Complete but empty log -> `reason=parse-failed`.
  - Server pod IP never set + `PHASE4_PAIR_WAIT_TIME=0` ->
    `reason=peer-pod-unready` (pod created but no IP) or
    `reason=nad-missing` (no pod created at all).
  - `PHASE4_RAIL_COUNT=4` override -> only rails 0-3 annotated;
    rails 4-7 never appear in any annotation.
  - 16 nodes (8 pairs) with `PHASE4_MAX_CONCURRENT_PAIRS=8` ->
    every pair forked, all 16 nodes labeled (concurrency cap
    sanity check).
  - `PHASE4_MAX_CONCURRENT_PAIRS=0` -> promoted to 1 with a
    logged warning rather than deadlocking.
  - `PHASE_NODES` env-var fallback when no positional args.
  - Sample `ib_write_bw` client log fixtures live under
    `tests/fixtures/phase4/` (pass, below-threshold, crashed, empty).

- **`PHASE45_PREFLIGHT_SCRIPT`**
  the Phase 4.5 pre-flight gate inside the Phase 5 launcher
  init-container. Four checks (SSH mesh, DNS, MPI spawn, RCCL
  topology) plus the verdict block. Covers:
  - All-checks-pass on a healthy 2-pod cluster -> exit 0, zero
    annotate calls, verdict banner present.
  - SSH mesh single-pair fail -> ssh_mesh_failed=true, annotation
    class `ssh-mesh`, exit 1.
  - SSH mesh all-pairs fail -> failed_pairs count = N*N, still
    single `ssh-mesh` class.
  - `WORKER_REPLICAS=1` degenerate self-pair -> no divide-by-zero
    or hang, passes.
  - DNS forward-miss fixture -> dns_failed=true, class `dns`, exit 1.
  - mpirun --hostfile no-op spawn fail -> mpi_spawn_failed=true,
    class `mpi-spawn`, exit 1.
  - RCCL probe non-timeout non-zero exit -> rccl_topo_failed=true,
    class `rccl-topology`, exit 1 (hard-fail).
  - RCCL probe exit 124 (timeout) -> rccl_topo_timeout=true, class
    `rccl-topology`, exit 0 (soft-fail per design §6).
  - All four checks fail -> annotation reason
    `ssh-mesh,dns,mpi-spawn,rccl-topology` (union, fixed order),
    exit 1, every participating node annotated.
  - Hard fail (ssh-mesh) + RCCL soft-fail -> classes include both,
    hard wins the exit code (1).
  - `ENABLE_SSH_CHECK=false` short-circuit -> whole pre-flight body
    is gated off, exit 0, zero kubectl exec calls.
  - `WAIT_FOR_WORKERS=false` -> kubectl wait not invoked but the
    four checks still run.
  - Sample DNS / RCCL stdout fixtures live under
    `tests/fixtures/phase4_5/`.

- **Orchestrator `DRY_RUN=1` mode** from the
  three scenarios documented in `-config-framework-orchestration-design.md`
  §7 "Orchestrator dry-run":
  - All five skip flags `true` -> exit 0, zero `kubectl label`/
    `kubectl annotate` calls.
  - Phases 1+2 enabled, 3-5 skipped -> Phase 1 and Phase 2 banners
    appear in order; Phases 3-5 take the `SKIP_*=true -- pass-through`
    branch.
  - Phase 3 enabled but no `feature.node.kubernetes.io/amd-nic=true`
    nodes -> Phase 3 takes the "no NIC-capable nodes" branch and
    exits 0.
  - Plus: empty Phase-0 pool exits 0 cleanly; cleanup and log
    collection honor `DRY_RUN`.

## Layout

```
tests/
  README.md                              # this file
  run_all.sh                             # entry point
  test_phase_node_label_script.sh        # helper library
  test_orchestrator_dry_run.sh           # orchestrator DRY_RUN mode
  test_phase1.sh                         # PHASE1_SCRIPT
  test_phase2.sh                         # PHASE2_SCRIPT
  test_phase3.sh                         # PHASE3_CHECK_SCRIPT + PHASE3_SCRIPT
  test_phase4.sh                         # PHASE4_DRIVER_SCRIPT
  test_phase4_5.sh                       # PHASE45_PREFLIGHT_SCRIPT
  lib/
    assert.sh                            # hand-rolled bash assertions
    kubectl_mock.sh                      # recording kubectl shim
    extract_script.sh                    # YAML block-scalar extractor
  fixtures/
    phase1/                              # AGFHC result.json fixtures
    phase2/                              # phase2.log fixtures
    phase3/                              # lspci / ip / rdma / ibv_* shim fixtures
    phase4/                              # ib_write_bw client-log fixtures
    phase4_5/                            # DNS / RCCL exec-stdout fixtures
```

## How to run

```sh
# Run the full suite
./example/gpu-validation-cluster/tests/run_all.sh

# Run a single file
./example/gpu-validation-cluster/tests/run_all.sh test_orchestrator_dry_run.sh

# Run one test file directly (bypasses the suite summary)
bash example/gpu-validation-cluster/tests/test_phase_node_label_script.sh
```

`run_all.sh` exits 0 only when every test file reports 0 failures.

## How the kubectl mock works

`lib/kubectl_mock.sh` installs a small bash script named `kubectl`
in a temp directory and prepends that directory to `$PATH`. This is
the only reliable way to intercept calls in CI environments where a
real `kubectl` is installed -- a bash `function kubectl { . }`
override is only visible to the current shell, and the orchestrator
writes per-phase scripts to `/tmp` and `source`s them, so any
function override would be invisible to those sub-shells.

The mock:
- records every invocation as a single line in
  `$KUBECTL_CALLS_FILE` (one arg per token, joined with single
  spaces);
- serves canned label values for `kubectl get node . -o jsonpath=.`
  from `$KUBECTL_STATE_FILE` (seeded via `kubectl_mock_set_label`);
- supports one-shot or sticky failure injection for `label`,
  `annotate`, and `get` via `kubectl_mock_fail` /
  `kubectl_mock_fail_sticky`;
- returns exit 99 for any kubectl verb the helpers and the
  orchestrator are not supposed to invoke -- catches accidental
  real-world calls in tests.

## How the YAML extraction works

The artifacts under test are embedded as multi-line `|` block
scalars inside Kubernetes manifests:

- `PHASE_NODE_LABEL_SCRIPT` lives under
  `configs/cluster-validation-config.yaml` -> `data:`.
- The orchestrator body lives under
  `configs/cluster-validation-job.yaml` -> the `submit-mpijob`
  container's `args:` list.

`lib/extract_script.sh` provides two pure-bash/awk extractors so the
tests do not depend on PyYAML or `yq`:

- `extract_configmap_data <yaml> <KEY>` -- emits the body of
  `data.<KEY>: |` on stdout.
- `extract_cronjob_orchestrator <yaml>` -- emits the body of the
  `submit-mpijob` container's `args: - |` block on stdout.

Both extractors strip the YAML block's leading indent and stop on
the first less-indented non-blank line, matching kubectl's own
normalization.

## How to add a new test

1. Create `tests/test_<thing>.sh`.
2. Source the three libs:
   ```bash
   TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
   source "${TEST_DIR}/lib/assert.sh"
   source "${TEST_DIR}/lib/kubectl_mock.sh"
   source "${TEST_DIR}/lib/extract_script.sh"
   ```
3. `kubectl_mock_init` once, then `kubectl_mock_reset` between
   tests.
4. Use `it "<name>" && { run <cmd>; assert_* .; }` per case.
5. Call `assert_summary` at the end -- it sets the file's exit
   status to 0 only when every `it` passed.

Tests are discovered by `run_all.sh` via the `test_*.sh` glob;
no registration step is needed.

## Test-plan coverage mapping

These tests realize the following cases from
`docs-internal/codie/test-plans/-test-plan.md`:

| Test case | Description | Realized by |
|---|---|---|
| TC2 | helper-label-pass-fail-annotate | `test_phase_node_label_script.sh` (`label_phase_passed` / `label_phase_failed` / `annotate_phase_value` blocks) |
| TC3 | filter-passed-nodes-returns-subset | `test_phase_node_label_script.sh` (`filter_passed_nodes` block) |
| TC4 | dryrun-all-skipped-exits-zero | `test_orchestrator_dry_run.sh` scenario 1 |
| TC7 | helper-rejects-missing-args | `test_phase_node_label_script.sh` (negative-validation cases per helper) |
| TC8 | empty-candidate-pool | `test_orchestrator_dry_run.sh` "DRY_RUN exits 0 when no candidate nodes are present" |
| TC10 | helper-kubectl-failure | `test_phase_node_label_script.sh` (`kubectl_mock_fail` cases per helper) |
| TC14 | dryrun-phase1-2-enabled-only | `test_orchestrator_dry_run.sh` scenario 2 |

These tests realize the following cases from
`docs-internal/codie/test-plans/-test-plan.md` (PHASE2_SCRIPT
behavior):

| Test case | Description | Realized by |
|---|---|---|
| TC2 | phase2-pass-labels-node | `test_phase2.sh` ("single node pass" + "mixed pass/fail") |
| TC3 | skip-phase2-passlabels-all | `test_phase2.sh` ("SKIP_GPU_MESH_VALIDATION=true .") |
| TC4 | rccl-env-no-ib-vars | `test_phase2.sh` ("PHASE2_RCCL_ENV_VARS contains no IB/fabric tunables") |
| TC5 | bw-below-threshold-fails | `test_phase2.sh` ("Failed + bw-below-threshold marker ." + "PHASE2_BW_THRESHOLD=9999 inject .") |
| TC6 | rccl-crash-fails | `test_phase2.sh` ("Failed + mpirun-exited marker ." + default-marker case) |
| TC7 | empty-input-list | `test_phase2.sh` ("PHASE2_SCRIPT with empty input list .") |
| TC9 | gpu-not-allocated-timeout | `test_phase2.sh` ("no conditions + PHASE2_JOB_WAIT_TIME=0 -> reason=timeout .") |
| TC10 | hung-job-cleanup | `test_phase2.sh` (same case — verifies `delete job . --ignore-not-found=true --wait=false`) |

These tests realize the following cases from
`docs-internal/codie/test-plans/-test-plan.md` (Phase 3 NIC health):

| Test case | Description | Realized by |
|---|---|---|
| TC2 | phase3-pass-labels-node | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT all checks pass -> =passed") |
| TC3 | skip-phase3-passlabels-all | `test_phase3.sh` ("SKIP_NIC_VALIDATION=true ." + case-insensitive) |
| TC4 | shellcheck-clean | `test_phase3.sh` ("shellcheck PHASE3_CHECK_SCRIPT .") |
| TC5 | nic-count-mismatch | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT nic-count mismatch .") |
| TC6 | link-down-one-nic | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT one NIC link DOWN .") |
| TC7 | rdma-state-not-active | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT one rdma link INIT .") |
| TC8 | empty-gid-table | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT empty GID table .") |
| TC10 | annotation-size-truncation | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT large failure list truncates .") |
| TC11 | tools-missing-image | `test_phase3.sh` ("PHASE3_CHECK_SCRIPT ibv_devinfo unresponsive .") |
| TC12 | nic-not-allocated-timeout | `test_phase3.sh` ("no conditions + PHASE3_JOB_WAIT_TIME=0 -> reason=nic-not-allocated .") |

These tests realize the following cases from
`docs-internal/codie/test-plans/-test-plan.md` (Phase 4 pairwise
RDMA bandwidth):

| Test case | Description | Realized by |
|---|---|---|
| TC1 | pairing-roundrobin-even | `test_phase4.sh` ("pairing round-robin even: [a,b,c,d] -> pairs=2 .") |
| TC2 | pairing-roundrobin-odd | `test_phase4.sh` ("pairing round-robin odd: [a,b,c,d,e] -> pairs=2 unpaired=node-e") |
| TC3 | per-rail-annotation-written | `test_phase4.sh` ("single pair all rails pass -> both nodes passed + per-rail annotations") |
| TC4 | skip-phase4-passlabels-all | `test_phase4.sh` ("SKIP_RAIL_BANDWIDTH_TEST=true ." + case-insensitive) |
| TC5 | single-rail-fail | `test_phase4.sh` ("single rail fail (rail 5 below threshold) -> failed-rails=5 .") |
| TC6 | all-rails-fail-one-pair | `test_phase4.sh` ("all rails fail on one pair -> failed-rails=0,1,2,3,4,5,6,7") |
| TC7 | ib-write-bw-crash | `test_phase4.sh` ("client Failed + no BW line -> reason=ib-write-bw-crashed") |
| TC8 | parse-failure | `test_phase4.sh` ("client Complete but empty log -> reason=parse-failed") |
| TC9 | single-node-input | `test_phase4.sh` ("single-node input -> unpaired pass-label .") |
| TC10 | empty-input | `test_phase4.sh` ("empty input list is a no-op .") |
| TC11 | rail-count-override | `test_phase4.sh` ("PHASE4_RAIL_COUNT=4 -> rails 0-3 annotated; rails 4-7 absent") |
| TC12 | server-pod-unready-timeout | `test_phase4.sh` ("server pod IP never set + PHASE4_PAIR_WAIT_TIME=0 -> reason=peer-pod-unready") |
| TC15 | concurrency-cap-honored | `test_phase4.sh` ("16 nodes (8 pairs) with cap=8 -> .") |

These tests realize the following cases from
`docs-internal/codie/test-plans/-test-plan.md` (Phase 4.5 cross-node
connectivity matrix pre-flight):

| Test case | Description | Realized by |
|---|---|---|
| TC2 | all-checks-pass | `test_phase4_5.sh` ("all checks pass on a healthy 2-pod cluster .") |
| TC4 | single-pair-ssh-fail | `test_phase4_5.sh` ("single SSH mesh pair fails .") |
| TC5 | dns-fail-fwd | `test_phase4_5.sh` ("DNS forward miss .") |
| TC6 | mpi-spawn-fail | `test_phase4_5.sh` ("mpirun --hostfile no-op fails .") |
| TC7 | rccl-topology-timeout | `test_phase4_5.sh` ("RCCL probe times out (exit 124) .") |
| TC8 | worker-replicas-1 | `test_phase4_5.sh` ("WORKER_REPLICAS=1 self-pair only .") |
| TC9 | annotation-includes-all-failed-classes | `test_phase4_5.sh` ("all four checks fail .") |
