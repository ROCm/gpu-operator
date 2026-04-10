# GPU Operator Knowledge Base and Skills

Organized documentation for GPU Operator testing and development.

## Directory Structure

```bash
kb_source/
├── README.md              # This file
│
├── products/              # How features work (product behavior)
│   ├── README.md
│   ├── dcm/               # Device Config Manager
│   │   └── README.md      # (Placeholder)
│   └── npd/               # Node Problem Detector
│       └── README.md      # (Placeholder)
│
├── testing/               # How to test features (test patterns)
│   ├── README.md
│   ├── dcm/               # DCM test patterns
│   │   ├── cleanup-on-failure.md
│   │   ├── driver-reload-timing.md
│   │   ├── partition-profile-files.md
│   │   └── verify-label-multi-node.md
│   ├── npd/               # NPD test patterns (to be added)
│   └── common/            # Generic patterns (to be added)
│
└── skills/                # Complete workflows (product + test)
    ├── pytest-dcm-dev.md  # DCM pytest development skill
    └── pytest-npd-dev.md  # NPD pytest development skill
```

## Three Types of Knowledge

### 1. Products (How it Works)

**Location**: `products/`

Product knowledge explains feature behavior and architecture:

- How does DCM partition GPUs?
- What ConfigMap format is required?
- What happens during a partition operation?
- What are the supported partition modes?

**Example**: `products/dcm/partition-modes.md` would explain SPX, DPX, QPX, CPX and which GPU series support them.

**Status**: 🚧 Placeholder - to be populated from product documentation

### 2. Testing (How to Test It)

**Location**: `testing/`

Testing knowledge covers test patterns and debugging:

- How do I write a partition test?
- Why is my test failing with JSON parse errors?
- How do I ensure cleanup runs on failure?
- What's the correct pattern for multi-node validation?

**Example**: `testing/dcm/driver-reload-timing.md` explains cascading test failures and how to fix them.

**Status**: ✅ Populated with DCM test patterns from recent work

### 3. Skills (Complete Workflows)

**Location**: `skills/`

Skills combine product and test knowledge for specific workflows:

- Developing DCM partition tests
- Debugging NPD integration issues
- Adding support for new GPU series
- Triaging test failures from job logs

**Example**: `skills/pytest-dcm-dev.md` references both product docs and test patterns.

**Status**: ✅ Two skills available (pytest-dcm-dev, pytest-npd-dev)

## Quick Navigation

### I want to...

**Understand how DCM works**
→ `products/dcm/` (when populated)
→ For now: `/docs/dcm/` in repo

**Write a DCM test**
→ `testing/dcm/partition-profile-files.md` for profile format
→ `skills/pytest-dcm-dev.md` for complete workflow

**Debug a failing DCM test**
→ `testing/dcm/driver-reload-timing.md` for JSON parse errors
→ `testing/dcm/cleanup-on-failure.md` for stuck nodes
→ `testing/dcm/verify-label-multi-node.md` for multi-node issues

**Develop NPD tests**
→ `skills/pytest-npd-dev.md` for complete workflow

**Add new GPU series support**
→ `skills/pytest-dcm-dev.md` section on "Adding Support for New GPU Series"

## Usage with Claude Code

### Use a Skill

```bash
@claude use pytest-dcm-dev to debug partition test failures
@claude use pytest-npd-dev to add health condition tests
```

### Reference KB Directly

```bash
@claude see testing/dcm/driver-reload-timing.md for the cascading failure pattern
@claude check testing/dcm/cleanup-on-failure.md for try/finally examples
```

## Contributing

### Adding Product Knowledge

1. Create topic file in `products/<feature>/`
2. Document actual behavior (not test patterns)
3. Include examples from product docs
4. Update skill to reference new content

### Adding Test Knowledge

1. Solve a test issue
2. Document in `testing/<feature>/`
3. Use KB entry template (see `testing/README.md`)
4. Include code examples and commits
5. Update skill if it's a common pattern

### Updating Skills

Skills should reference KB entries rather than duplicate content:

- **Don't**: Copy entire KB entry into skill
- **Do**: Reference KB entry and summarize key points

Example:

```markdown

## Driver Reload Timing

See [testing/dcm/driver-reload-timing.md](../testing/dcm/driver-reload-timing.md)

**Key point**: After untaint, wait for device-plugin using `K8Helper.wait_for_driver_reload()`
```

## Organization Principles

### Separation of Concerns

- **Products**: What happens (architecture, behavior)
- **Testing**: How to validate (patterns, debugging)
- **Skills**: Workflows (combines both)

### One Concept Per File

Each KB entry focuses on single issue/pattern:

- ✅ `driver-reload-timing.md` - One specific timing issue
- ❌ `dcm-issues.md` - Too broad

### Cross-Referencing

KB entries should link to related entries:

```markdown

## Related Issues

- See [verify-label-multi-node.md](verify-label-multi-node.md) for multi-node validation
- See [cleanup-on-failure.md](cleanup-on-failure.md) for finalizer patterns
```

## Current Status

| Directory | Status | Content |
|-----------|--------|---------|
| `products/dcm/` | 🚧 Placeholder | To be populated from `/docs/dcm/` |
| `products/npd/` | 🚧 Placeholder | To be populated |
| `testing/dcm/` | ✅ Complete | 4 KB entries covering recent fixes |
| `testing/npd/` | 📝 Planned | To be added |
| `testing/common/` | 📝 Planned | Generic pytest/K8s patterns |
| `skills/` | ✅ Complete | 2 skills (DCM, NPD) |

## Migration Notes

This structure was reorganized on 2026-04-07 to separate:

- Product knowledge (how it works)
- Testing knowledge (how to test it)

Previous structure mixed these in `common/kb/` and `common/skills/`.

Old locations (deprecated):

- `common/kb/dcm/*` → Moved to `testing/dcm/`
- `common/skills/*` → Moved to `skills/`
- `common/README.md` → Split into multiple READMEs

## Related Documentation

- **Product Docs**: `/docs/dcm/`, `/docs/device_plugin/`, etc.
- **Test Code**: `tests/pytests/k8/`, `tests/pytests/openshift/`
- **Test Libraries**: `tests/pytests/lib/`
- **User Memory**: `~/.claude/projects/-home-srivatsa-ws-3-gpu-operator/memory/`

## Version

- **Last Updated**: 2026-04-07
- **KB Version**: 2.0 (reorganized structure)
- **Test Framework**: pytest 9.0.2
- **GPU Operator**: v1.5.0+
