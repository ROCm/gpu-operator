# GPU Operator Development Workflow

This workflow automates the implementation of GPU Operator features from PRD to production-ready code.

## ⚠️ Note: Files Have Been Migrated

The workflow files are now installed in the standard Claude Code locations for automatic discovery:

- **Main workflow spec**: [`/CLAUDE.md`](../CLAUDE.md) (project root)
- **Agent definitions**: [`/.claude/agents/`](../.claude/agents/)
- **Skill definitions**: [`/.claude/skills/`](../.claude/skills/)

This directory now contains **documentation only**. The actual workflow files are in the locations above.

## Quick Start

```bash
# 1. Create a PRD in docs/feature-prds/
docs/feature-prds/add-newfeature.md

# 2. Run the workflow (skill auto-discovered from /.claude/skills/)
/implement-feature docs/feature-prds/add-newfeature.md
```

## What It Does

- Reads your PRD
- Implements CRD changes, controller logic, handlers
- Writes E2E and integration tests
- Updates documentation and Helm charts
- Validates with build and tests
- Generates completion report

## Installed Workflow Structure

```
/root/src/github.com/pensando/gpu-operator/
├── CLAUDE.md                    # Main workflow specification
├── .claude/
│   ├── agents/                  # Agent definitions
│   │   ├── operator-implementation.md
│   │   ├── e2e-test-agent.md
│   │   ├── pytest-agent.md
│   │   └── docs-agent.md
│   └── skills/                  # Workflow skills
│       └── implement-feature.md
└── gpu-operator-dev-workflow/   # Documentation (this directory)
    ├── README.md                # This file
    ├── QUICKSTART.md            # Quick start guide
    └── SETUP-COMPLETE.md        # Setup documentation
```

## PRD Requirements

Your PRD should include:
1. Feature Overview
2. Technical Specification (CRD changes, types)
3. Controller Changes
4. Implementation Plan (file checklist)
5. Testing Requirements
6. Documentation Updates

See `docs/feature-prds/template.md` for a template.

## For More Information

See [CLAUDE.md](CLAUDE.md) for the complete workflow specification.
