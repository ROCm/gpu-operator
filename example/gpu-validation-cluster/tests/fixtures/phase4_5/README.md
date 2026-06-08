# Phase 4.5 fixtures

Canned stdout bodies for the `kubectl exec` calls issued by
`PHASE45_PREFLIGHT_SCRIPT`. The test harness feeds these into the
mock `kubectl`'s exec-response queue (see
`lib/kubectl_mock.sh::kubectl_mock_queue_exec`).

The pre-flight script issues exec calls in this order (per healthy
2-pod run):

| # | Phase | Stdout shape we care about |
|---|-------|----------------------------|
| 1 | launcher->worker SSH readiness (one big exec) | -- (only exit code matters) |
| 2.N+1 | N*N SSH mesh (one exec per (src, dst_ip) pair) | -- (only exit code) |
| -- | DNS forward+reverse (one exec) | `DNS:<host>:fwd=<addr> rev=<state>` lines, or empty when clean |
| -- | MPI no-op spawn (one exec) | -- (only exit code) |
| -- | RCCL topology probe (wrapped in `timeout 60`) | NCCL INFO lines (empty is fine), but exit code drives classification |

## Fixtures

- `dns-clean.txt` -- empty body; clean DNS run produces no MISS lines.
- `dns-fwd-miss.txt` -- one host fails forward resolution.
- `rccl-pass.txt` -- a handful of `NCCL INFO` lines, simulating a
  fast topology discovery.
- `rccl-empty.txt` -- empty stdout; the grep|head pipeline produces
  nothing on a non-NCCL-aware host. We pair this with a non-zero
  exit code for the `rccl_topo_failed` branch and with exit 124 for
  the soft-fail `rccl_topo_timeout` branch (the host-side `timeout`
  shim writes nothing on timeout either).
