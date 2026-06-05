# Standalone bash script for project version bumps

- **Date:** 2026-06-03
- **Author:** Yan Sun
- **Related PR(s):** pensando/gpu-operator#1514
- **Related issue(s) / JIRA:** infra team request

## Context

Project version bumps (the `vX.Y.Z` value carried by `PROJECT_VERSION`,
helm chart metadata, Dockerfile labels, the OLM CSV, CI job config, Go
operand-image defaults, and helm chart artifacts) have historically been
driven through the `Makefile` targets `update-version`,
`update-helm-metadata`, and `update-version-in-ci`. Those three targets
covered only a subset of the files that actually need to move during a
release cut — files like `helm-charts-k8s/Chart.yaml`,
`hack/openshift-patch/metadata-patch/Chart.yaml`, `docs/conf.py`, the
OLM CSV `name:`/`version:`, the generated CRD chart labels, the
helm README badges, and the `Makefile`'s own `PROJECT_VERSION ?=`
default were hand-edited in every bump commit (see e.g.
`2f50d830`, `e5376ef8`). The result was a half-mechanical, half-manual
process that drifted and was easy to get wrong.

The infra team asked for a single bash script that performs the full
render **without** invoking `make` at all, so it can be wired into
release automation that doesn't have a Go toolchain or `make` in its
environment, and so the bump can be reproduced from one CLI entry
point.

## Approach

Add `hack/bump-version.sh` and replace the three Makefile targets with
a single delegating target.

### Script

`hack/bump-version.sh <PROJECT_VERSION> [IMAGE_TAG]`

- `PROJECT_VERSION` (required, `X.Y.Z` or `vX.Y.Z`) — drives the
  release identifier: chart `version:`, Go operand-image defaults, CI
  `PROJECT_VERSION` env, `Makefile` `PROJECT_VERSION ?=` default, OLM
  CSV `name:`/`version:`, docs version, etc.
- `IMAGE_TAG` (optional, defaults to `PROJECT_VERSION`) — drives the
  build-artifact tag: Dockerfile `version=`/`release=` labels and helm
  `appVersion:`. For a dev artifact pass `dev`; for a release pass the
  release version (or omit and let it default).

The script always rewrites the full file set listed below. There is
no "minimal" mode — an earlier iteration of this PR had a `--release`
flag that opted into the broader rewrites, but it caused real bumps to
silently skip files the user expected, so the flag was removed.

Files rewritten on every run:

- `Makefile` — `PROJECT_VERSION ?=` default
- `helm-charts-k8s/Chart.yaml`,
  `hack/k8s-patch/metadata-patch/Chart.yaml`,
  `hack/openshift-patch/metadata-patch/Chart.yaml` — `version:` and
  `appVersion:`
- `Dockerfile`, `internal/utils_container/Dockerfile` — `version=`
  and `release=` labels
- `internal/configmanager/configmanager.go`,
  `internal/metricsexporter/metricsexporter.go`,
  `internal/testrunner/testrunner.go` — `defaultXxxImage` constants
- `.job.yml` — `PROJECT_VERSION=` and `HELM_CHARTS_VERSION=`
- `asset-build/gpuoperator-asset-push.sh` — `PROJECT_VERSION:-`
  default
- `docs/conf.py` — Sphinx `version =`
- `bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml` —
  CSV `name:` and `version:`
- `helm-charts-k8s/crds/deviceconfig-crd.yaml`,
  `helm-charts-openshift/crds/deviceconfig-crd.yaml` — chart labels
- `helm-charts-k8s/README.md`, `helm-charts-openshift/README.md` —
  version badges (regex matches both `vX.Y.Z` and `dev` so the rewrite
  works whether `main` is at a release tag or the dev default)

Rewrites are left **unstaged**. Branch creation, commit, and push stay
in the caller's hands (engineer or release automation), keeping the
script's sole responsibility "render the codebase for the given
version".

### Makefile

The three targets `update-helm-metadata`, `update-version`, and
`update-version-in-ci` (~22 lines of inline `sed`) collapse into a
single `update-version` (~3 lines) that delegates to the script:

```make
.PHONY: update-version
update-version: ## Render all version-bearing files via hack/bump-version.sh based on PROJECT_VERSION / IMAGE_TAG.
	./hack/bump-version.sh $(PROJECT_VERSION) $(IMAGE_TAG)
	${MAKE} fmt
```

`update-version` is still a dependency of `manifests`, so
`make manifests` continues to render version files transitively.
`$(MAKE) fmt` stays in the Makefile so the script doesn't need to
know about Go tooling.

`update-registry` (image-URL rewriter, no `PROJECT_VERSION` anywhere)
is left untouched — separate concern from version bumps and not
duplicated in the script.

### Alternatives considered

- **Generate the script from the Makefile recipes at runtime.**
  Rejected: parsing Makefile recipes into portable shell is more
  fragile than just having one source of truth.
- **Move the sed logic into a Python script.** Rejected: infra
  explicitly asked for bash with no extra runtime dependencies.
- **Have the script also stage and commit the result.** Rejected:
  unstaged output lets the caller eyeball the diff and choose the
  commit message, matching how prior bump-up commits (`2f50d830`,
  `e5376ef8`) were authored.
- **Have the script create a new git branch.** Initial revision did
  this. Removed per infra request: release automation owns its own
  git state; the script should only render files.
- **Two-mode script with a `--release` flag** (minimal Makefile-target
  parity by default, broader release-cut rewrites opt-in). Rejected
  after testing: the minimal mode silently skipped files the user
  expected during a real bump. A single always-on mode is less
  surprising even though `make manifests` now rewrites a wider set.
- **Keep the Makefile targets and just add the script alongside.**
  Rejected: defeats the "one place to maintain" goal that prompted
  the consolidation request. The Makefile target now delegates to the
  same script, so the patterns live in exactly one place.

## Scope

- **In scope:**
  - New file `hack/bump-version.sh`.
  - Collapse of `update-helm-metadata`, `update-version-in-ci`, and
    the body of `update-version` into a single delegating
    `update-version` target.
  - This plan document.
- **Out of scope:**
  - Removing or modifying `update-registry` (image-URL rewriter, not
    a version concern).
  - Wiring the script into CI / release jobs (infra owns that).
  - Bumping the project version itself — this PR only adds tooling.
  - Reconciling `docs/conf.py` (currently hard-coded to `1.5.0`)
    against the `Makefile` `PROJECT_VERSION ?= v0.0.1` default. See
    **Risks** below.

## Validation

- **Script syntax:** `bash -n hack/bump-version.sh` passes.
- **Release bump** (`./hack/bump-version.sh 1.6.0` or
  `make PROJECT_VERSION=v1.6.0 IMAGE_TAG=v1.6.0 update-version`):
  rewrote all 14 expected files. Spot-checked `Chart.yaml`,
  `Dockerfile`, OLM CSV `name:`/`version:`, README badges, `.job.yml`,
  Makefile default — all flipped to `v1.6.0` correctly.
- **Dev-tagged bump** (`./hack/bump-version.sh 1.6.0 dev`): chart
  `version:` → `v1.6.0`, chart `appVersion:` → `"dev"`, Dockerfile
  `release="dev"` (IMAGE_TAG honored), OLM CSV
  `name: amd-gpu-operator.v1.6.0`, Makefile default → `v1.6.0`.
- **Coverage parity with historical bumps:** file list matches the
  union of what historical commits `2f50d830` (v1.3.0) and
  `e5376ef8` (v1.3.1) touched by hand.

## Risks and rollback

- **Risk — `docs/conf.py` drift on first `make manifests`.**
  `docs/conf.py` currently has `version = "1.5.0"` hard-coded while
  the `Makefile` default is `PROJECT_VERSION ?= v0.0.1`. The next
  `make manifests` anyone runs will silently flip `docs/conf.py` to
  `0.0.1` — a docs-version regression if committed inadvertently.
  Mitigation: a follow-up PR should align the `Makefile` default with
  the actual current release version on `main` (e.g. bump default to
  `v1.5.0`); in the meantime, reviewers should revert that hunk from
  unrelated PRs that include `make manifests` output.
- **Risk — `make manifests` now rewrites more files.** Previously
  `make manifests` was a near-no-op for version files when defaults
  were in effect. Now every run rewrites Dockerfile labels, all chart
  metadata, OLM CSV, README badges, etc. to whatever `PROJECT_VERSION`
  and `IMAGE_TAG` are set to. For developers working with the
  defaults this is mostly idempotent (the tree already matches
  defaults), except for the `docs/conf.py` case above.
- **Risk — over-broad sed match in generated artifacts.** CRD label
  and README badge rewrites target shapes
  (`gpu-operator-charts-vX.Y.Z`, `Version-vX.Y.Z-informational`,
  `dev`-form variants) that today appear only in the generated
  artifacts; verified by greps. Caught at review time because the
  script leaves the diff unstaged.
- **Risk — script vs Makefile drift.** Now that there's only one
  source of truth (the script), this is no longer a duplication
  concern. A new file added to the bump set must be added to the
  script, not the Makefile.
- **Rollback:** revert this PR. Restores the three original Makefile
  targets and removes the script. No external caller depends on
  either yet.
