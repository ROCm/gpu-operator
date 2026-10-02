# Plan: Fix pip Dependabot alerts in docs/sphinx/requirements.txt (ROCm-baseline sync)

- **Date:** 2026-10-01
- **Author:** spraveenio
- **Status:** Proposed

## Context

The upstream **ROCm/gpu-operator** repo has Dependabot enabled and is
reporting open **pip** security alerts, all against the docs toolchain
lockfile `docs/sphinx/requirements.txt`:

<https://github.com/ROCm/gpu-operator/security/dependabot?q=is%3Aopen+ecosystem%3Apip>

The same vulnerable, pip-compiled transitive pins exist verbatim in this
(pensando) repo's copy of that lockfile. Dependabot only fires on ROCm,
but the risk is identical in both trees.

Six packages are affected (all **transitive** — none are direct entries
in `requirements.in`, which only pins `rocm-docs-core`,
`sphinx-reredirects`, `Sphinx-Substitution-Extensions`):

| Package      | Old      | New (first-patched) | Highest-severity alert covered |
|--------------|----------|---------------------|--------------------------------|
| cryptography | 43.0.3   | 49.0.0              | high — exponential path-building |
| gitpython    | 3.1.43   | 3.1.62              | critical — git-config injection RCE |
| pyjwt[crypto]| 2.10.0   | 2.15.0              | critical — asymmetric-PEM bypass |
| soupsieve    | 2.6      | 2.9.0               | medium — ReDoS |
| tornado      | 6.4.2    | 6.5.9               | high — path traversal / OOM |
| urllib3      | 2.2.3    | 2.8.0               | high — unbounded buffer / proxy TLS |

Each target is the **minimum** `first_patched_version` that clears every
open alert for that package (not "latest"), to keep the blast radius of
the bump as small as possible.

Two **companion bumps** are required by the resolver, not by an alert:
`cryptography==49.0.0` raises its own floors on `cffi` and
`typing-extensions`, so the frozen pins must move with it (otherwise
`pip install -r` fails with `ResolutionImpossible` on Python 3.10, as the
first CI run on this PR did — jobd target 34093944):

| Package           | Old     | New    | Required by |
|-------------------|---------|--------|-------------|
| cffi              | 1.17.1  | 2.0.0  | `cryptography 49 -> cffi>=2.0.0` |
| typing-extensions | 4.12.2  | 4.13.2 | `cryptography 49 -> typing-extensions>=4.13.2 (py<3.11)` |

Both companions also have byte-identical context in ROCm, so the commit
stays cleanly cherry-pickable.

## Approach

Workflow: **ROCm is the baseline; pensando is brought in sync so an
automated cherry-picker can later raise the equivalent PR against ROCm.**

- The fix is a surgical pin bump of only the six affected lines in
  `docs/sphinx/requirements.txt`. No other lines change.
- The two repos' lockfiles have diverged elsewhere (ROCm ships
  `rocm-docs-core[llms]==1.38.0`; pensando is on `1.18.1`), **but the six
  security lines and their 3-line git-diff context are byte-identical in
  both trees**. This was confirmed by comparing the full files. As a
  result the security commit cherry-picks onto ROCm `main` with no
  conflict.
- To keep that cherry-pick clean, the change is split into two commits on
  this branch:
  1. **`docs/sphinx/requirements.txt` only** — the cherry-pickable commit
     destined for ROCm.
  2. **This plan file** — pensando-internal, under `docs-internal/`, which
     is not part of the upstream distribution and must **not** be
     cherry-picked.

The `sec-fix` skill was also updated to document this Dependabot flow, but
that is shipped as a **separate PR** (`.claude/` tooling, independent of
the dependency fix) so this security PR stays minimal and cleanly
cherry-pickable.

### Alternatives considered

- *Wholesale-copy ROCm's `requirements.txt` into pensando.* Rejected: it
  would drag in a `rocm-docs-core` 1.18.1 -> 1.38.0 feature upgrade
  (plus `beartype`, `sphinx-markdown-builder`), which is a docs-infra
  change, not a security fix, and risks breaking the pensando docs build.
- *Re-run `pip-compile` to regenerate the lockfile.* Ideal in principle,
  but a full recompile pulls newer transitive deps and produces a diff
  that will not cherry-pick cleanly onto ROCm's divergent lockfile. A
  minimal hand-pin of the six CVE lines is the correct tradeoff here; a
  proper recompile can follow separately on each repo if desired.

## Scope

**In scope:** the six transitive pin bumps in
`docs/sphinx/requirements.txt`; this plan file.

**Out of scope:** the `sec-fix` skill documentation update (shipped as a
separate PR); the Go-ecosystem Dependabot alerts in `tests/k8s-e2e/go.mod`
(containerd, grpc) — the request was pip-only; `requirements.in` changes;
the operator image CVE scan (that is the existing Trivy flow in the same
skill); any `rocm-docs-core` upgrade.

## Validation

- `git diff docs/sphinx/requirements.txt` shows exactly six changed pins,
  each with unchanged `# via` provenance context.
- All first-patched versions confirmed present on PyPI
  (HTTP 200 for each exact version).
- **Full resolve verified** for the CI target interpreter: a cross-version
  dry-run (`pip install --dry-run --python-version 3.10
  --only-binary=:all: --abi cp310 -r requirements.txt`) resolves with
  exit 0 and "Would install ... cffi-2.0.0 cryptography-49.0.0 ...
  typing_extensions-4.13.2 ...". This reproduces CI's `make dep-docs`
  install and confirms no remaining `ResolutionImpossible`.
- Compatibility sanity: all six remain within their parents' declared
  ranges (pygithub 2.5.0 needs `pyjwt[crypto]>=2.4.0`, `urllib3>=1.26.0`;
  requests needs `urllib3<3`; beautifulsoup4 needs `soupsieve>1.2`;
  pyjwt crypto extra needs `cryptography>=3.4`). No parent pin is
  violated.
- Docs build (Sphinx) in CI exercises the lockfile; a gross
  incompatibility would surface there.
- Cherry-pick safety: the requirements-only commit was confirmed to apply
  onto ROCm `main`'s `docs/sphinx/requirements.txt` by direct
  context comparison of both full files.

## Risks / Rollback

- **Risk:** `cryptography` jumps six majors (43 -> 49). Required — alert
  GHSA path-building fix first lands in 49.0.0. Mitigated by CI docs
  build; pins stay minimal.
- **Risk:** a hand-edited lockfile drifts from what a fresh `pip-compile`
  would produce. Accepted for a minimal security bump; a later recompile
  reconciles it.
- **Rollback:** revert the single `docs/sphinx/requirements.txt` commit.
  No runtime/operator code is touched; impact is limited to the docs
  toolchain.
