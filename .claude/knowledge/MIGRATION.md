# KB Migration Guide

## Latest: Moved to .claude/ (2026-04-10)

All Claude Code configuration moved from `kb_source/` to `.claude/` to follow standard conventions.

### Old Structure

```bash
kb_source/
├── skills/              # Executable skills
├── products/            # Product knowledge
├── testing/             # Test knowledge
├── prds/                # Requirements
└── common/
```

### New Structure

```bash
.claude/
├── skills/              # Executable workflows (was: kb_source/skills/ + kb_source/common/skills/)
├── knowledge/           # Reference documentation (was: kb_source/*)
│   ├── products/
│   ├── testing/
│   ├── prds/
│   └── *.md
└── agents/              # Custom agents (new)
```

**Benefits:**

- Standard `.claude/` location for Claude Code project config
- Clear separation: executable skills vs reference knowledge
- Aligned with global `~/.claude/` structure
- Easier discovery and organization

---

## Previous: Separated Product/Test Knowledge (2026-04-07)

The knowledge base was reorganized to separate product knowledge from testing knowledge.

### Structure at that time

```bash
kb_source/
├── products/            # Product behavior (to be populated)
│   ├── dcm/
│   └── npd/
├── testing/             # Test patterns and debugging
│   ├── dcm/             # DCM test KB (migrated from common/kb/dcm/)
│   ├── npd/
│   └── common/
└── skills/              # Skills (migrated from common/skills/)
    ├── pytest-dcm-dev.md
    └── pytest-npd-dev.md
```

## Migration Status

### ✅ Completed

1. **Created new directory structure**
   - `products/` with DCM and NPD placeholders
   - `testing/` with proper organization
   - `skills/` at top level

2. **Migrated DCM test KB**
   - `common/kb/dcm/*.md` → `testing/dcm/*.md`
   - 4 KB entries successfully migrated
   - Cross-references updated

3. **Migrated skills**
   - `common/skills/*.md` → `skills/*.md`
   - 2 skills migrated (pytest-dcm-dev, pytest-npd-dev)

4. **Created documentation**
   - Main `README.md` explaining structure
   - `products/README.md` for product knowledge
   - `testing/README.md` for test knowledge
   - Feature-specific READMEs

### 📝 Old Directory (common/)

The `common/` directory still exists with old content. It can be removed after verification.

**To verify migration before cleanup:**

```bash

# Check all files migrated

diff -r common/kb/dcm/ testing/dcm/
diff -r common/skills/ skills/

# If diffs show only header changes or cross-reference updates, migration is complete

```

**To remove old directory:**

```bash
cd /home/srivatsa/ws-3/gpu-operator/kb_source
rm -rf common/
```

## File Mapping

### KB Entries

| Old Location | New Location |
|-------------|-------------|
| `common/kb/dcm/cleanup-on-failure.md` | `testing/dcm/cleanup-on-failure.md` |
| `common/kb/dcm/driver-reload-timing.md` | `testing/dcm/driver-reload-timing.md` |
| `common/kb/dcm/partition-profile-files.md` | `testing/dcm/partition-profile-files.md` |
| `common/kb/dcm/verify-label-multi-node.md` | `testing/dcm/verify-label-multi-node.md` |

### Skills

| Old Location | New Location |
|-------------|-------------|
| `common/skills/pytest-dcm-dev.md` | `skills/pytest-dcm-dev.md` |
| `common/skills/pytest-npd-dev.md` | `skills/pytest-npd-dev.md` |

### Documentation

| Old Location | New Location | Notes |
|-------------|-------------|-------|
| `common/README.md` | Multiple READMEs | Split into topic-specific docs |
| N/A | `README.md` | New main README |
| N/A | `products/README.md` | New products overview |
| N/A | `testing/README.md` | New testing overview |

## Changes to Content

### Updated Cross-References

KB entries updated to use relative paths in new structure:

**Example** (`testing/dcm/driver-reload-timing.md`):

```markdown

## Related Issues

- verify_label() must check all nodes, not just first one (see [verify-label-multi-node.md](verify-label-multi-node.md))
```

### No Content Changes

The actual technical content remains unchanged - only organization improved.

## What's Next

### Immediate (User Action)

1. **Verify migration**: Check that all files copied correctly
2. **Test skills**: Try using skills with new paths
3. **Remove old directory**: Once verified, delete `common/`

### Future (To Be Populated)

1. **Product knowledge**: Extract from `/docs/dcm/`, `/docs/device_plugin/`
2. **NPD test patterns**: Document NPD testing approaches
3. **Common patterns**: Generic pytest/K8s test patterns

## How to Use New Structure

### Finding Information

**Before** (old structure):

```bash
@claude check common/kb/dcm/driver-reload-timing.md
```

**After** (new structure):

```bash
@claude check testing/dcm/driver-reload-timing.md
```

### Using Skills

**Skills paths changed** but usage same:

```bash
@claude use pytest-dcm-dev to debug partition tests
@claude use pytest-npd-dev to add NPD tests
```

Skills automatically reference KB in correct locations.

### Understanding Flow

1. **Learn product**: `products/dcm/` (when populated) or `/docs/dcm/`
2. **Learn testing**: `testing/dcm/` for test patterns
3. **Use workflow**: `skills/pytest-dcm-dev.md` combines both

## Questions?

See main `README.md` for:

- Full structure explanation
- Quick navigation guide
- Contributing guidelines
- Organization principles
