# v1.5.1 OpenShift OperatorHub Version Note

- **Date:** 2026-09-23
- **Author:** Yan Sun
- **Related PR(s):** TBD
- **Related issue(s) / JIRA:** N/A

## Context

The v1.5.1 release was intended to use the same version in the OpenShift
OperatorHub catalog. Because the v1.5.1 OpenShift OLM bundle contained an
image SHA256 mismatch, a follow-up v1.5.2 bundle was published to the
certified-operators catalog. As a result, OpenShift users installing the
v1.5.1 release through OperatorHub see v1.5.2.

Without an explanation, this version difference can appear to be an
unexpected release or an inconsistency between the general release and the
OpenShift installation path.

## Approach

Add a version note to the v1.5.1 section of `docs/releasenotes.md` that:

- explicitly scopes the discrepancy to the v1.5.1 release;
- explains that the follow-up v1.5.2 bundle was required by the image SHA256
  mismatch; and
- tells OpenShift users to use v1.5.2 for the v1.5.1 OLM installation.

## Scope

- **In scope:** The v1.5.1 release-notes explanation of the OpenShift
  OperatorHub version.
- **Out of scope:** Operator code, OLM manifests, catalog contents, and
  changes to the general release version.

## Validation

- `git diff --check` passes.
- Confirm the note appears under the v1.5.1 release-notes heading.
- Confirm the OpenShift installation guide remains unchanged.

## Risks and rollback

- **Risk:** Readers could interpret v1.5.2 as a separate feature release.
  The note identifies it as the follow-up OLM bundle for v1.5.1.
- **Rollback:** Revert the documentation commit; no runtime impact.
