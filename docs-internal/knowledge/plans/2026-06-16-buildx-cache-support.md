# Add buildx cache support to docker-build-env

## Context

ROCm/gpu-operator#576 adds a GitHub Actions CI workflow for PR sanity
checks (build + copyrights). The initial implementation reimplemented
the Makefile's `docker-build-env` logic inline in the workflow YAML —
extracting Makefile variables with `sed`, using `docker/build-push-action`,
and running `docker run` manually. This is fragile and will drift from
the Makefile.

To make the CI workflow a thin caller of Makefile targets, the
`docker-build-env` target needs to support external cache backends
(e.g. GitHub Actions cache) so CI doesn't need separate tooling.

## Approach

1. Add `DOCKER_CACHE_FROM` and `DOCKER_CACHE_TO` variables (default empty)
2. Switch `docker-build-env` from `docker build` to `docker buildx build`
3. Conditionally append `--cache-from` / `--cache-to` when variables are set
4. Add `--load` flag (required by buildx to load into local daemon)
5. Remove buggy `INSECURE_REGISTRY` conditional — always pass the build-arg

### Alternatives considered

- **Keep `docker build`, only CI uses buildx**: Loses the goal of having
  CI call Makefile targets directly.
- **Always-on inline cache**: Unnecessary complexity — local builds already
  benefit from Docker's built-in layer cache.

## Scope

**In scope**: `docker-build-env` target and cache variables in the Makefile.

**Out of scope**: CI workflow changes (separate PR on ROCm repo),
`default` target modifications, other docker build targets.

## Validation

- `make docker-build-env` builds identically to before when cache vars unset
- `CI=true make default` passes end-to-end (verified on ROCm PR branch)
- Cache variables work with `type=gha` and `type=local` backends

## Risks / Rollback

- **Low risk**: `docker buildx build` has been the default builder since
  Docker 23+. Falls back gracefully on older Docker versions if buildx
  plugin is installed.
- **Rollback**: Revert the commit — switches back to `docker build` with
  the old `INSECURE_REGISTRY` conditional.
