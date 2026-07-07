# Mythos AI-scan security fixes — batch 1 (KUBE-33, KUBE-31)

- **Date:** 2026-07-01
- **Author:** Nitish Bhat
- **Related PR(s):** pensando #1570 (ROCm counterpart: ROCm/gpu-operator #588)
- **Related issue(s) / JIRA:** KUBE-33, KUBE-31 (KUBE-27/KUBE-30 are ROCm-only, no counterpart here)

## Context

The Mythos AI security scan filed a set of findings against the GPU
operator repos. This batch addresses the two that apply to the pensando
fork. (The KUBE-27/KUBE-30 workflow finding does not apply here — this
fork has no `.github/workflows/linting.yml` or `dependabot.yml`.)

## Approach

- **KUBE-33** — the e2e `nodeapp` test image was built on `alpine:3.7`
  (EOL since Nov 2019). Bumped to a digest-pinned `alpine:3.22`, added a
  `.dockerignore` scoping the build context to only the files the
  Dockerfile copies, and anchored the `.gitignore` rules for the
  ephemeral SSH key / built binary to `tests/e2e/nodeapp/` (the bare
  `nodeapp` pattern was matching the whole directory).
- **KUBE-31** — `Dockerfile.build` installed helm by piping the
  `get-helm-3` script fetched from helm's `main` branch with no integrity
  check. Replaced with a pinned, sha256-verified tarball install; version
  and checksum are Makefile args (`HELM_VERSION`/`HELM_SHA256`) forwarded
  to `docker-build-env`, matching the existing base-image build-arg
  pattern.

### Alternatives considered

- KUBE-33: full rework to mount the SSH key via a runtime Secret instead
  of baking it into the image — rejected as disproportionate for a
  test-only image whose key is already regenerated per build.
- KUBE-31: bump to helm 4 — rejected; that is a separate major-version
  decision. Pinned to latest helm 3 (v3.19.0) to preserve behavior.

## Scope

- **In scope:** `tests/e2e/nodeapp/` Dockerfile + `.dockerignore`,
  `.gitignore`, `Dockerfile.build`, `Makefile`.
- **Out of scope:** the same unpinned-helm pattern in
  `example/gpu-validation-cluster/build/Dockerfile` and the
  developer-guide snippet (separate follow-up); nodeapp SSH-key redesign.

## Validation

- Unit tests: n/a (build/config only).
- Integration / e2e tests: `make docker-build-env` builds successfully;
  the helm step reports `helm.tar.gz: OK`. helm v3.19.0 checksum
  confirmed against the official `.sha256sum` and the actual tarball.
- Manual / hardware steps: none.

## Risks and rollback

- Known risks: low — test/build tooling only, no operator runtime or
  operand behavior change.
- Rollback plan: revert the two commits on this branch.
