# GPU Operator Claude Code Configuration

Project-specific Claude Code skills, knowledge base, and configuration.

## Directory Structure

```bash
.claude/
├── README.md              # This file
├── skills/                # Executable workflows (invoked with /skill-name)
│   ├── pytest-dcm-dev.md       # DCM pytest development
│   ├── pytest-npd-dev.md       # NPD pytest development
│   ├── pytest-dev.md           # Generic pytest development
│   └── test-plan-dev.md        # Test plan generation
│
├── knowledge/             # Reference documentation (read but not executable)
│   ├── README.md              # Knowledge base overview
│   ├── MIGRATION.md           # Migration history
│   ├── products/              # Product behavior and architecture
│   │   ├── dcm/               # Device Config Manager
│   │   └── npd/               # Node Problem Detector
│   ├── testing/               # Test patterns and debugging
│   │   ├── dcm/               # DCM test knowledge
│   │   ├── npd/               # NPD test knowledge
│   │   └── common/            # Generic patterns
│   ├── prds/                  # Product requirements documents
│   ├── device-metrics-exporter.md
│   └── platform-support.md
│
└── agents/                # Custom agent definitions (if needed)
```

## Skills vs Knowledge

### Skills (`skills/`)

**Executable workflows** - These are invoked by users with slash commands like `/pytest-dev`.

- Complete, self-contained instructions for Claude to perform a task
- Invoked with `/skill-name` syntax
- Examples: test plan generation, pytest development, debugging

### Knowledge (`knowledge/`)

**Reference documentation** - Claude reads these for context but doesn't execute them directly.

- Product documentation (how features work)
- Test patterns and debugging guides
- PRDs and requirements
- Historical information

## Using Skills

Skills can be invoked with the `/` prefix:

```bash
/pytest-dev Debug the failures in job log 30158283
/pytest-npd-dev Implement NPD integration tests
/pytest-dcm-dev Add DCM partition tests
/test-plan-dev Generate test plan from PRD-GPU-20260406-01.md
```

## Adding New Skills

1. Create a new `.md` file in `skills/` with frontmatter:

```markdown
---
name: my-skill
description: Brief description of what the skill does
---

Detailed instructions for Claude...
```

2. Document it in the appropriate README
3. Commit and the skill is available project-wide

## Adding Knowledge

Add reference documentation to `knowledge/` organized by:

- `products/` - How features work
- `testing/` - How to test features
- `prds/` - Requirements documents

No special frontmatter needed - just organized markdown files.

## Migration from kb_source/

This directory replaces the old `kb_source/` structure. See `knowledge/MIGRATION.md` for details.
