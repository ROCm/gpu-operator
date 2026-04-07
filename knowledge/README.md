# GPU Operator Knowledge Base

Comprehensive knowledge files for understanding the AMD GPU Operator codebase, architecture, and components.

## Knowledge Files

### Core Architecture
- [architecture-overview.md](architecture-overview.md) — High-level architecture, controller pattern, component hierarchy, and data flows
- [deviceconfig-api-spec.md](deviceconfig-api-spec.md) — Complete DeviceConfig CRD specification with all fields and validation rules
- [codebase-structure.md](codebase-structure.md) — Directory layout, file organization, key files, and import paths

### Component Details
- [component-details.md](component-details.md) — Deep dive into each component: driver mgmt, device plugin, DRA, DCM, metrics, tests, remediation

### Reconciler Implementation
- **[reconciler-patterns.md](reconciler-patterns.md)** — ⚠️ **CRITICAL**: Reconciler invariants, ordering constraints, watch patterns, and state management. **Required reading before modifying reconciliation logic.**

### Testing
- [e2e-test-patterns.md](e2e-test-patterns.md) — End-to-end testing patterns and framework documentation (gocheck, not Ginkgo)

### Documentation
- [documentation-patterns.md](documentation-patterns.md) — Complete documentation standards, structure, update procedures, TOC management, and Sphinx build system

## Reading Order for New Contributors

1. **architecture-overview.md** - Understand the big picture
2. **codebase-structure.md** - Learn where things are
3. **reconciler-patterns.md** - ⚠️ **Critical patterns and invariants**
4. **deviceconfig-api-spec.md** - CRD specification
5. **component-details.md** - Component-specific details
6. **e2e-test-patterns.md** - Testing practices
7. **documentation-patterns.md** - Documentation standards

## Reading Order for Agent Implementation

When implementing new features, agents should read:

1. **reconciler-patterns.md** - ⚠️ **FIRST** - Understand constraints
2. **architecture-overview.md** - High-level design patterns
3. **component-details.md** - Existing component patterns
4. **deviceconfig-api-spec.md** - CRD fields and validation
5. **codebase-structure.md** - File locations
6. **e2e-test-patterns.md** - Test implementation patterns
7. **documentation-patterns.md** - Documentation requirements (for docs-agent)
