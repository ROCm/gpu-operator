# Common Skills

This directory contains Claude Code skills that can be used across all agent types for the GPU Operator project.

## Available Skills

### `/test-plan-dev`

**File**: `test-plan-dev.md`

Generates comprehensive test plans from Product Requirement Documents (PRDs).

**Use cases**:

- Analyze PRDs and extract testable requirements
- Create structured test plans with scenario mapping
- Define test coverage matrix and priorities
- Identify test data and environment requirements
- Generate reviewable test plan documents for stakeholder approval

**When to use**: BEFORE implementing any testcases - this is the first step after receiving a PRD.

**Example invocations**:

```bash
/test-plan-dev Generate test plan from PRD at /path/to/PRD-GPU-20260406-01.md for device-metrics-exporter component
```

**Output**: Test plan document ready for human review and approval.

---

### `/pytest-dev`

**File**: `pytest-dev.md`

Implements pytest testcases from approved test plans for AMD GPU testing infrastructure.

**Use cases**:

- Implement pytest testcases from approved test plans
- Write new test functions following project patterns
- Create test fixtures and utilities
- Debug failing tests from CI job logs
- Understand test infrastructure and fixture relationships
- Run and analyze test results
- Navigate the test codebase
- Ensure cross-platform compatibility (K8s + OpenShift)

**Supported projects**:

- gpu-operator tests
- exporter helm-chart tests
- gpu-dra helm-chart tests
- amd-metrics-exporter (debian/docker) tests

**When to use**: AFTER test plan has been approved - this implements the actual pytest code.

**Example invocations**:

```bash
/pytest-dev Implement test scenarios from approved test plan at /path/to/test-plan.md
/pytest-dev Debug the failures in job log 30158283
/pytest-dev Explain the fixture hierarchy in conftest.py
/pytest-dev Help me understand how metrics validation works
```

**Workflow**: Use `/test-plan-dev` first → review/approve plan → use `/pytest-dev` to implement.

## Adding New Skills

To add a new common skill:

1. Create a new `.md` file in this directory with frontmatter:

```markdown
---
name: skill-name
description: Brief description of what the skill does
---

Skill instructions here...
```

1. Document the skill in this README
2. Commit and push to make it available to all team members

## Directory Structure

```text
kb_source/
├── common/
│   └── skills/           # Skills available to all agents
│       ├── README.md     # This file
│       └── pytest-dev.md # Pytest development agent
├── agents/               # Agent-specific skills (if needed)
└── <project_name>/       # Project-specific knowledge bases
```
