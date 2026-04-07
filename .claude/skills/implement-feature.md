---
name: implement-feature
description: >
  Use this skill when the user asks to "implement a feature", "implement the PRD",
  "build from PRD", or provides a path to a feature PRD file.
  Orchestrates the full PRD to Implementation to Test to Build workflow using parallel agents.
  Also triggered with /implement-feature.
version: 0.2.0
---

# Implement Feature - GPU Operator Development Workflow

Converts a feature PRD into production-ready GPU Operator code with tests and documentation.

## Usage

```bash
/implement-feature docs/feature-prds/add-newfeature.md
```

## Important: Knowledge Base

All agents will consult the project knowledge base located in `knowledge/`:
- codebase-structure.md - Repository layout
- architecture-overview.md - Operator architecture
- deviceconfig-api-spec.md - CRD specifications  
- component-details.md - Component patterns

Agents use this to ensure implementations follow established patterns.

## Workflow Steps

### Step 1: PRD Validation

Read and verify PRD sections.

### Step 2: Task Breakdown

Create TodoWrite checklist.

### Step 3: Dispatch Parallel Agents

Launch all 4 agents with instructions to consult knowledge base:

```python
Agent(
  subagent_type: "operator-implementation",
  description: "Implement CRD and controller",
  prompt: """Implement the feature in docs/feature-prds/<prd-file>.
  
  FIRST: Read knowledge base files:
  - knowledge/codebase-structure.md
  - knowledge/architecture-overview.md
  - knowledge/deviceconfig-api-spec.md
  - knowledge/component-details.md
  
  Use these to understand patterns before implementing.
  Update TodoWrite as you work.""",
  run_in_background: true
)

Agent(
  subagent_type: "e2e-test-agent",
  description: "Write E2E tests",
  prompt: """Write E2E tests for docs/feature-prds/<prd-file>.
  
  Reference knowledge/architecture-overview.md for test patterns.
  Update TodoWrite.""",
  run_in_background: true
)

Agent(
  subagent_type: "pytest-agent",
  description: "Write integration tests",
  prompt: """Write pytest tests for docs/feature-prds/<prd-file>.
  
  Check existing test patterns in knowledge base.
  Update TodoWrite.""",
  run_in_background: true
)

Agent(
  subagent_type: "docs-agent",
  description: "Update documentation",
  prompt: """Update docs for docs/feature-prds/<prd-file>.
  
  Follow documentation patterns from knowledge base.
  Update TodoWrite.""",
  run_in_background: true
)
```

### Step 4-6: [Same as before - wait, validate, report]

## Key Changes from v0.1.0

- Added knowledge base integration
- Agents now consult knowledge/ before implementing
- More accurate pattern following
- Better adherence to GPU Operator conventions
