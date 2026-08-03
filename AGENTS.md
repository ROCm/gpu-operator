## Branch Naming Convention

All branches must follow the `user/<github-login>/<description>` format.

**Get your exact GitHub username first:**
```bash
if command -v gh &>/dev/null; then
  LOGIN=$(gh api user --jq .login 2>/dev/null)
else
  LOGIN=$(git config github.user 2>/dev/null)
fi
if [[ -z "$LOGIN" ]]; then
  echo "ERROR: Cannot resolve GitHub username."
  echo "  Option A (recommended): install gh CLI and run: gh auth login"
  echo "  Option B: set it manually: git config --global github.user <your-github-login>"
  exit 1
fi
```
Never infer from git config `user.name` or email — the org username may differ (e.g. `jsmith-amd` not `jsmith`).

**Create and push a branch:**
```bash
# Detect the default branch
DEFAULT=$(git remote show origin 2>/dev/null | awk '/HEAD branch/{print $NF}')
DEFAULT=${DEFAULT:-master}

git fetch origin
git checkout -b "user/${LOGIN}/short-description" origin/${DEFAULT}
git push -u origin "user/${LOGIN}/short-description"
```

**Description segment rules:** hyphens, underscores, and dots only — no slashes.
- Good: `user/<login>/fix-rdma`, `user/<login>/fix_timeout.v2`, `user/<login>/JIRA-1234-fix`
- Bad: `user/<login>/fix/rdma` (nested slash), `feat/fix-rdma` (wrong prefix)
- **ASCII only** — no Unicode, emoji, or special characters
- **No leading dot** — Git rejects branch names with a leading dot (e.g. `.fix-something` is invalid)

**Allowed prefixes (everything else is blocked at creation):**

| Prefix | Example | Created by |
|---|---|---|
| `user/<login>/*` | `user/<login>/fix-rdma` | Developers |
| `team/<name>/*` | `team/networking/vlan-rework` | Teams |
| `bot/<bot>/*` | `bot/ci-bot/fix-pr4521` | CI automation |
| `collab/*` | `collab/helios-integration` | Collaborative |
| `collab-*` | `collab-helios` | Legacy collaborative |
| `revert-*` | `revert-abc1234` | GitHub auto-created |
| `copilot/*` | `copilot/fix-bug-123` | GitHub Copilot |
| `dependabot/*` | `dependabot/npm/lodash` | Dependabot |

**Opening a PR:**
```bash
gh pr create \
  --head "user/${LOGIN}/short-description" \
  --title "Short description of change"
```

For full ruleset and bypass policy details, see:
[GitHub Branch Protection Rulesets](https://amd.atlassian.net/wiki/spaces/EN/pages/1810205147/GitHub+Branch+Protection+Rulesets)