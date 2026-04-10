# Product Knowledge Base

This directory contains product-level documentation about how GPU Operator features actually work - their architecture, behavior, and user-facing functionality.

## Purpose

Product KB answers questions like:

- How does this feature work?
- What are the supported configurations?
- What happens when X occurs?
- How do components interact?

This is **separate from** test knowledge (see `../testing/`) which covers how to write/debug tests.

## Directory Structure

```bash
products/
├── dcm/          # Device Config Manager
│   └── README.md # (Placeholder - to be populated)
│
└── npd/          # Node Problem Detector
    └── README.md # (Placeholder - to be populated)
```

## Planned Content

### DCM (Device Config Manager)

- Architecture and components
- Partition modes and GPU series support
- ConfigMap format and validation
- Node labels and taints control plane
- Partition workflow and state transitions
- Systemd service integration
- Error conditions and recovery

### NPD (Node Problem Detector)

- NPD architecture and custom plugins
- amdgpuhealth tool integration
- Condition types and event generation
- ConfigMap structure
- Monitor types (kernel, system-log, custom-plugin)

## Contributing Product Knowledge

When adding product KB:

1. **Create topic-focused files**: One concept per file
2. **Document actual behavior**: How it works, not how to test it
3. **Include examples**: ConfigMaps, labels, commands
4. **Reference official docs**: Link to AMD/K8s documentation
5. **Keep it current**: Update when product behavior changes

## Relationship to Other Directories

- **`products/`** (here) - What the feature does
- **`testing/`** - How to test the feature
- **`skills/`** - Workflows combining product + test knowledge

Example:

- `products/dcm/partition-modes.md` - What partition modes exist and how they work
- `testing/dcm/partition-test-patterns.md` - How to write tests for partition modes
- `skills/pytest-dcm-dev.md` - Complete workflow referencing both

## Status

🚧 **This directory is currently unpopulated.**

Test-related knowledge has been organized in `../testing/`. Product knowledge will be added as needed.
