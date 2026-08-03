# CLAUDE.md — Repository Contribution Rules

These rules apply to every contributor (human or AI) working on this repo.
They are enforced by CI; PRs that violate them cannot be merged.

## Plan-File Requirement (MANDATORY for every PR)

Every pull request **must** be associated with a plan file living under
`docs-internal/knowledge/`. The plan file is the durable record of *what*
is being changed and *why*, and is the gating artifact for review and merge.

The `docs-internal/` tree is deliberately kept *outside* `.claude/` so that
contributor design notes are decoupled from the Claude Code harness, and
named `internal` to make it clear these documents are not for the
open-source distribution at this time.

A PR satisfies this rule if **either** of the following is true:

1. **Existing plan reference.** The PR description (body) contains a
   reference to a plan file that already exists in the repo (i.e. exists
   on the PR's base branch), using a path under `docs-internal/knowledge/`.
   Examples of acceptable references in the PR body:

   - `Plan: docs-internal/knowledge/plans/2026-05-14-add-mi350p.md`
   - `See docs-internal/knowledge/prds/driver-rollout.md`

2. **New plan added.** The PR diff adds, modifies, or renames at least
   one file under `docs-internal/knowledge/plans/` (i.e., the PR
   introduces or updates its own plan). `TEMPLATE.md` and `README.md`
   under that directory do not count as a plan for this check.

If neither condition holds, the `pr-plan-check` GitHub workflow fails and
the PR is blocked from merging.

### What a plan file must contain

At minimum:

- **Context** — what problem the change addresses and why now.
- **Approach** — the chosen design and any alternatives considered.
- **Scope** — what is in / out of scope for the PR(s) tied to this plan.
- **Validation** — how the change is verified (tests, manual steps, hardware).
- **Risks / Rollback** — known risks and how to revert if needed.

Use `docs-internal/knowledge/plans/TEMPLATE.md` as a starting point for
new plans.

### Where plans live

- `docs-internal/knowledge/plans/` — primary location for PR-scoped
  plans. New plans should go here. Use the filename convention
  `YYYY-MM-DD-short-slug.md` (e.g. `2026-05-14-add-mi350p.md`).
- Other subdirectories under `docs-internal/knowledge/` (e.g. `prds/`,
  `products/`, `testing/`) are also valid for longer-lived design notes;
  PRs may reference any path under `docs-internal/knowledge/`.

### Why this rule exists

- Preserves design context for future contributors and AI assistants.
- Keeps PR descriptions linked to a single durable artifact instead of
  scattered comments.
- Makes review faster: reviewers read the plan first, then the diff.

## Project-Scoped Claude Code Agents & Skills

This repo ships its own Claude Code **agents** under `.claude/agents/`
and **skills** under `.claude/skills/`. Claude Code auto-discovers both
at session start when run from this working tree, with no extra
configuration.

When a project-scoped agent or skill shares a name with a user-global
one in `~/.claude/`, **the project copy wins**. This is the standard
Claude Code precedence model (project overrides user). We rely on it so
that this repo's guardrails — for example, the test-intent and stub-tag
checks that gate PR #1465-style e2e-sim work — run with the exact rules
this project intends, not whatever a contributor happens to have
installed locally.

Practical guidance for contributors:

- Before reaching for a global agent or skill, browse `.claude/agents/`
  and `.claude/skills/` to see what the project already provides; prefer
  those.
- When adding a new agent or skill that is specific to this repo's
  workflows, place it under `.claude/agents/` or `.claude/skills/` (not
  in your user-global `~/.claude/`). That keeps it versioned with the
  code it guards, and ensures it takes precedence for every contributor.
- Treat new or modified agents/skills as code: cover them in the PR's
  plan file (Context / Approach / Validation) just like any other change.

## Enforcement

The workflow `.github/workflows/pr-plan-check.yml` runs on every pull
request to `main` and fails when the rule above is not met. Configure
this workflow as a **required status check** on the protected branch so
that no PR can be merged without it passing.

## Build & Repo Gotchas

Things Claude cannot derive from `Makefile` help text or the README:

- **Canonical full build is two steps**: `make docker-build-env` to
  produce the dev container, then `make all` *inside that container*.
  `go build ./...` on the host often works but is not what CI runs.
- **Two root entrypoint files exist** — `entrypoint.sh` and
  `entrypoint_build.sh`. Both are real, hand-edited source. Don't
  conflate them.
- **Generated files — do not hand-edit.** A PreToolUse hook
  (`.claude/hooks/protect-generated.sh`) will block edits to:
  - `api/v1alpha1/zz_generated*.go` — regenerate via `make generate`.
  - `config/crd/bases/**` — regenerate via `make manifests`.
  - `bundle/manifests/**`, `bundle/metadata/**` — regenerate via
    `make bundle-build`.
  - `vendor/**` — refresh via `go.mod` + `go mod vendor`.
  - `helm-charts-k8s/charts/*.tgz`, root `*gpu-operator*.tgz` —
    regenerate via `make helm-k8s`.
- **`gofmt -w` runs automatically** on every `.go` edit via
  `.claude/hooks/gofmt-on-edit.sh`. No action needed; CI enforces
  formatting anyway.
- **Session-end transcript capture** writes non-trivial sessions
  (≥5 tool uses) to `docs-internal/knowledge/_pending/` (gitignored,
  local-only). Run `/curate-learnings` to distill durable, non-obvious
  findings into `docs-internal/knowledge/learnings.md`. Most sessions
  produce zero kept entries — that is correct.

## Claude Code Configuration

- `.claude/settings.json` — team-shared permission allowlist
  (read-only git/gh/go/docker, `make:*`) + hook wiring. Destructive
  operations (`git commit/push`, `gh pr create`, `docker run/build`,
  lab access) still prompt.
- `.claude/settings.local.json`, `CLAUDE.local.md` — personal
  overrides; gitignored.
- See `.claude/README.md` for layout details and the skill catalog.

---

# Pensando Repo — Claude Code Context

> This file is loaded automatically by Claude Code on every session.
> It instructs Claude how to assist developers with the branch workflow.

---

## Your Role

When a developer asks about branches, PRs, or git workflow, proactively guide them toward
the branch naming convention. Do not wait to be asked — if you see someone about to create
a branch with the wrong format, correct them before they push.

**Whenever you create or suggest a branch name, you MUST:**

**Step 1 — Resolve the GitHub username:**
```bash
if command -v gh &>/dev/null; then
  LOGIN=$(gh api user --jq .login 2>/dev/null)
else
  # gh not installed — fall back to a cached value
  LOGIN=$(git config github.user 2>/dev/null)
fi
if [[ -z "$LOGIN" ]]; then
  echo "ERROR: Cannot resolve GitHub username."
  echo "  Option A (recommended): install gh CLI and run: gh auth login"
  echo "  Option B: set it manually: git config --global github.user <your-github-login>"
  exit 1
fi
```
`gh api user` returns the exact GitHub org username (e.g. `jsmith-amd`, not `jsmith`).
If `gh` is not installed, cache your username once with `git config --global github.user <your-github-login>`.
**Never infer the username from git config `user.name` or email — the org login may differ.**

**Step 2 — Construct the branch name:**
```
user/${LOGIN}/short-description
```
Include a ticket ID if one exists (e.g. `user/${LOGIN}/fix-rdma-timeout` or `user/${LOGIN}/JIRA-1234-fix-rdma`).
Description must be **ASCII only** — no Unicode, emoji, or special characters. Use hyphens, underscores, or dots as separators. Do not start the description with a dot (`.`) — Git rejects branch names with a leading dot.

**Step 3 — Validate the name matches one of the allowed prefixes:**
- Personal: `^user/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
- Team: `^team/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
- Bot: `^bot/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
- Collab: `^collab/` or `^collab-`
- Auto: `^revert-`, `^copilot/`, `^dependabot/`

Never skip Step 1. A missing or wrong login creates an invalid branch name silently.

---

## Branch Naming Convention

See `AGENTS.md` for the full branch naming reference — format, allowed prefixes, create/push steps.
`AGENTS.md` is the shared baseline read by all AI tools (Claude Code, Copilot, Codex, Gemini).

The short version: `user/<github-login>/<description>` — always resolve login via:
```bash
LOGIN=$(gh api user --jq .login)
```
Never infer from git config or email. A wrong login creates an invalid branch name silently.

---

## Fetch Setup (run once per clone)

With many branches in the repo, `git fetch` can be slow. Scope it to only what you need:

```bash
# Resolve GitHub username (see Step 1 above)
if command -v gh &>/dev/null; then
  LOGIN=$(gh api user --jq .login 2>/dev/null)
else
  LOGIN=$(git config github.user 2>/dev/null)
fi

# Resolve default (trunk) branch
if command -v gh &>/dev/null; then
  DEFAULT=$(gh api repos/pensando/$(basename $(git rev-parse --show-toplevel)) --jq .default_branch 2>/dev/null)
fi
if [[ -z "$DEFAULT" ]]; then
  # Fallback: detect from remote HEAD or ask git
  DEFAULT=$(git remote show origin 2>/dev/null | awk '/HEAD branch/{print $NF}')
fi
DEFAULT=${DEFAULT:-master}  # last resort default

git config --unset-all remote.origin.fetch
git config --add remote.origin.fetch "+refs/heads/${DEFAULT}:refs/remotes/origin/${DEFAULT}"
git config --add remote.origin.fetch '+refs/heads/team/*:refs/remotes/origin/team/*'
git config --add remote.origin.fetch "+refs/heads/user/${LOGIN}/*:refs/remotes/origin/user/${LOGIN}/*"
git config --add remote.origin.fetch '^refs/heads/user/*'
```

This pulls only the default branch, `team/*`, and your own `user/<login>/*` branches.

To fetch a colleague's branch explicitly:
```bash
git fetch origin user/<colleague-login>/branch-name
```

---

## Opening a PR

```bash
# After pushing your branch:
gh pr create \
  --head "user/${LOGIN}/short-description" \
  --title "Short description of change" \
  --body "Description of change"
```

For code review: anyone with write access can push directly to your `user/<login>/*`
branch — you don't need to create a new PR if someone wants to fix a CI failure or
add a small change.

---

## Common Scenarios — How to Help

**Scenario: Developer tries to create `feat/fix-something`**
→ Say: "That branch name will be blocked. Use `user/<login>/fix-something` instead.
  Run: `git checkout -b user/$(gh api user --jq .login)/fix-something origin/<default-branch>`"

**Scenario: Developer asks how to collaborate on someone's branch**
→ Say: "User branches are open — anyone with write access can push.
  Fetch it: `git fetch origin user/<login>/branch-name`
  Check out: `git checkout -b local-fix origin/user/<login>/branch-name`
  Push back: `git push origin HEAD:user/<login>/branch-name`"

**Scenario: Developer's branch was auto-deleted after PR merged**
→ Say: "Branches are auto-deleted when PRs merge. For follow-up work:
  `git checkout -b user/<login>/followup-work origin/<default-branch>`"

**Scenario: Developer asks about deleting their own branch**
→ Say: "You can delete your own branches via the mergers team or the self-service
  deletion tool (coming soon). After PR merge, branches are auto-deleted."

**Scenario: Developer asks why `git fetch` is slow**
→ Say: "Configure scoped fetch using the commands in the Fetch Setup section above.
  This reduces fetch to only the branches you need."

**Scenario: Developer is writing CI automation / a bot**
→ Say: "Use the `bot/<your-bot-account>/<description>` prefix.
  Example: `bot/ci-bot/fix-pr4521`
  This is the designated namespace for automated branches."

**Scenario: Developer's branch was accidentally deleted (not via merge)**
→ Say: "GitHub keeps deleted branch data — go to the repo's branch list or a related PR
  and click 'Restore branch'. There is no official time limit for restoration."

## Further Reading

For the full ruleset design, bypass policies, and org-wide enforcement details, refer to the internal Confluence page:
[GitHub Branch Protection Rulesets](https://amd.atlassian.net/wiki/spaces/EN/pages/1810205147/GitHub+Branch+Protection+Rulesets)