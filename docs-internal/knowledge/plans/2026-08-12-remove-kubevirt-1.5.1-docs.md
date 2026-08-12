# Remove KubeVirt from 1.5.1 documentation navigation

- **Date:** 2026-08-12
- **Author:** Nitish Bhat
- **Related PR(s):** #<pr>
- **Related issue(s) / JIRA:** #gpu-operator-dev Slack request (Shrey Ajmera / Praveen kumar Shanmugam, 2026-08-12)

## Context

KubeVirt is **not** part of the 1.5.1 GA release, but a "KubeVirt"
section was appearing in the published 1.5.1 documentation sidebar
(flagged with a screenshot in #gpu-operator-dev on 2026-08-12). The
KubeVirt integration is still in progress and should only be documented
on `main`, not in the 1.5.1 GA docs.

Praveen confirmed the fix scope: "Removing from the index file alone is
enough" — i.e. drop the nav entry; no need to delete the page content.

## Approach

Remove the `KubeVirt` caption block from the Sphinx table of contents on
the `v1.5.1` branch only:

- `docs/sphinx/_toc.yml` — the TOC `docs/conf.py` renders
  (`external_toc_path`).
- `docs/sphinx/_toc.yml.in` — its template, kept in sync.

`docs/kubevirt/kubevirt.md` is left in place (simply unlinked from the
nav). `main` is untouched — KubeVirt remains in the nav there, where the
feature is still being developed.

### Alternatives considered

- **Delete `docs/kubevirt/kubevirt.md` too** — rejected. Praveen said
  removing the nav entry is enough; the page is still valid content on
  `main`, and deleting it here would widen the diff for no benefit.

## Scope

- **In scope:** Remove the KubeVirt nav caption from the two Sphinx TOC
  files on `v1.5.1`.
- **Out of scope:** Any change to `main`; deleting the kubevirt page
  file; any non-nav docs content.

## Validation

- `grep -c kubevirt docs/sphinx/_toc.yml docs/sphinx/_toc.yml.in` → `0`
  in both after the change.
- No other docs link to the kubevirt page (the only remaining "kubevirt"
  string in docs is an unrelated external GitHub URL in
  `knownlimitations.md`).
- Orphaned-file safety: removing a file from the toctree while the file
  remains emits a Sphinx warning, not an error. Neither the Makefile
  `sphinx-build -b html` target nor `.readthedocs.yaml` uses
  warnings-as-errors (`-W` / `fail_on_warning`), so the docs build still
  succeeds.

## Risks and rollback

- **Risk:** minimal — nav-only change on the release branch.
- **Rollback:** revert the commit to restore the KubeVirt nav entry.
