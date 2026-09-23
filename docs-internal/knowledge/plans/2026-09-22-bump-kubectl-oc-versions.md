# Make kubectl dynamic (stable.txt) in Dockerfile.build

- **Date:** 2026-09-22
- **Author:** spraveenio
- **Related PR(s):** TBD

## Context

`Dockerfile.build` had a hardcoded kubectl version (`v1.36.2`), requiring a manual
bump PR every time a new minor release ships. The other Dockerfiles in the repo
already resolve kubectl dynamically via `https://dl.k8s.io/release/stable.txt`,
so `Dockerfile.build` was the only outlier.

## Approach

Replace the hardcoded pin with a dynamic lookup at build time:

```dockerfile
RUN KUBECTL_VERSION=$(curl -Ls https://dl.k8s.io/release/stable.txt) && \
    curl -o /usr/local/bin/kubectl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" && \
    chmod +x /usr/local/bin/kubectl
```

This matches the pattern already used in `Dockerfile` and `tests/k8s-e2e/Dockerfile.e2e`.

### Alternatives considered

- Keep a pin but bump it to current (`v1.37.0`) — rejected; still requires a manual
  PR for every upstream kubectl release, with no benefit over dynamic resolution.

## Scope

- **In scope:** `Dockerfile.build` kubectl install line.
- **Out of scope:** oc version changes (already dynamic via `ocp/latest`), other
  Dockerfiles (already dynamic), Go toolchain updates.

## Validation

- `docker build -f Dockerfile.build .` succeeds.
- `kubectl version --client` inside the resulting image reports the current stable release.
- CI Dockerfile lint passes.

## Risks / Rollback

- **Risk:** A new upstream kubectl stable release breaks a CI script at the time
  `Dockerfile.build` is rebuilt. Low probability; kubectl is backwards-compatible.
- **Rollback:** Revert to a pinned version in `Dockerfile.build`.
