# Makefile: Allow overriding dev.env file path

- **Date:** 2026-09-25
- **Author:** Landon LaSmith
- **Related PR(s):** N/A
- **Related issue(s) / JIRA:** N/A

## Context

The `Makefile` hardcodes the `dev.env` filename when including developer environment
variables. Contributors who want to maintain multiple environment configurations (e.g.
one per cluster, one per test target) have no way to switch without renaming or
symlinking the file. This change introduces a `DEV_ENV_FILE` variable so any `.env`
file can be substituted at `make` invocation time.

## Approach

- Add `DEV_ENV_FILE ?= dev.env` before the conditional include block. The `?=` operator
  preserves the existing default so no existing workflow is affected.
- Replace the hardcoded `dev.env` in the `include` directive with `$(DEV_ENV_FILE)`.
- The wildcard guard (`ifneq ("$(wildcard ...)", "")`) is updated to use the variable,
  so the include is still silently skipped when the file does not exist.

### Alternatives considered

- **Symlink convention** — Contributors could symlink `dev.env` to the file they want.
  Rejected: symlinks are fiddly, can be accidentally committed, and require a separate
  documentation step.
- **Multiple named targets** (e.g. `make dev-env-foo`) — Rejected: adds surface area to
  the Makefile and makes it harder to compose with other targets.

## Scope

- **In scope:** `Makefile` change only (3 lines).
- **Out of scope:** changes to `dev.env` itself, documentation updates, or any CI
  pipeline changes.

## Validation

- Manual: `make DEV_ENV_FILE=custom.env <target>` with a custom env file loads the
  correct variables.
- Manual: `make` with no override and an existing `dev.env` continues to work as before.
- Manual: `make` with no override and no `dev.env` present silently skips the include
  (regression check).
- No automated tests needed — the change is a build-system convenience with no
  runtime code path.

## Risks and rollback

- **Known risks:** None. The default value preserves existing behavior exactly.
- **Rollback plan:** Revert the single Makefile commit; no downstream artifacts are
  affected.
