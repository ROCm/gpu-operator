# GPU Operator Development Workflow - Agent Instruction Set

This is the canonical instruction set for feature development in the GPU Operator.
All agents in this workflow read this file.

## What This Is

A structured software engineering workflow that converts Product Requirements
Documents (PRDs) into production-ready GPU Operator features with full test coverage
and automation. The workflow consists of four phases executed in parallel where
possible.

## Path Resolution

All agents resolve paths through the PRD context:

```
PRD file: docs/feature-prds/<feature-name>.md
Working directory: <project-root>
Output structure:
  <project-root>/
    api/v1alpha1/            # CRD type definitions
    internal/controllers/    # reconciliation logic
    internal/<component>/    # component-specific handlers
    config/                  # Kubernetes manifests
    tests/e2e/              # end-to-end tests
    tests/pytests/          # integration tests
    docs/                   # documentation updates
    helm-charts-k8s/        # Helm chart updates
```

## Agents

| Agent | Model | Role |
|-------|-------|------|
| **gpu-operator-orchestrator** | opus | Coordinator: parses PRD, creates task breakdown, dispatches parallel agents, validates completion |
| **operator-implementation** | sonnet | Code: writes CRD types, Go implementation, controllers, handlers |
| **e2e-test-agent** | sonnet | E2E Testing: writes tests for tests/e2e/ |
| **pytest-agent** | sonnet | Integration: writes Python tests for tests/pytests/ |
| **docs-agent** | sonnet | Documentation: updates user docs, developer docs, Helm charts, examples |

Agent definitions: `agents/`

## Workflow Phases

### Phase 1: PRD Analysis & Task Breakdown (gpu-operator-orchestrator)

1. Read PRD file from docs/feature-prds/
2. Extract implementation requirements
3. Create structured task list with TodoWrite
4. Validate dependencies between tasks
5. Dispatch parallel agents

### Phase 2: Parallel Execution (4 agents in parallel)

All agents run in parallel via Agent tool with `run_in_background: true`

### Phase 3: Build & Test Validation (parallel)

After implementation agents complete, run build and tests in parallel.

### Phase 4: Final Validation (gpu-operator-orchestrator)

1. Verify all tasks completed
2. Verify build succeeded
3. Verify all tests passed
4. Generate completion report
5. Suggest next steps

## Key Principles

1. **Always start from PRD** - Never implement without a PRD
2. **Maximize parallelism** - Implementation, tests, docs run in parallel
3. **Shared progress tracking** - All agents update TodoWrite
4. **Independent agents** - Each agent reads PRD independently
5. **Validate before merge** - Build and test validation
6. **Fail fast** - Report errors immediately
7. **Use existing patterns** - Search codebase first
8. **Complete the checklist** - Every PRD item must be addressed
9. **Test what you build** - Full test coverage required

## GPU Operator Specific Patterns

### CRD Structure
- Main CRD: DeviceConfig (api/v1alpha1/deviceconfig_types.go)
- Spec contains component configurations
- Status contains deployment status and conditions
- Use pointer types for optional fields (*string, *int32)
- Follow existing naming conventions (camelCase)

### Controller Patterns
- Main reconciler: DeviceConfigReconciler
- Component handlers: kmmHandler, dpHandler, nlHandler, etc.
- Status updates use structured conditions
- Reconcile returns (ctrl.Result, error)

### Component Handler Pattern
```go
type ComponentHandler interface {
    Reconcile(ctx context.Context, dc *gpuev1alpha1.DeviceConfig) error
    Cleanup(ctx context.Context, dc *gpuev1alpha1.DeviceConfig) error
}
```

### Testing Patterns
- E2E tests in tests/e2e/ use Kubernetes test framework
- Python tests in tests/pytests/ use pytest
- Tests should cover vanilla K8s and OpenShift
- Include upgrade scenario tests

## Success Criteria

Workflow is complete when:

✅ All PRD tasks marked complete in TodoWrite
✅ All CRD types created/modified
✅ All controller/handler code implemented
✅ All manifests generated correctly
✅ All E2E tests written and passing
✅ All integration tests written and passing
✅ All documentation updated
✅ Build succeeds with no errors
✅ No regressions in existing tests
✅ Completion report generated

## Knowledge Base Integration

All agents must read the project knowledge base before starting work:

### Required Knowledge Files

Located in `knowledge/` directory:

1. **codebase-structure.md** - Repository layout, key files, module organization
2. **architecture-overview.md** - Operator architecture, reconciliation flow, design patterns
3. **deviceconfig-api-spec.md** - Complete DeviceConfig CRD specification
4. **component-details.md** - Component handler patterns and implementations

### Agent Knowledge Base Usage

Each agent must:
1. Read relevant knowledge base files at startup
2. Use knowledge base as authoritative source for patterns
3. Reference specific knowledge base sections in implementation
4. Report which knowledge files were consulted

### Knowledge Base Priority

When implementing features:
1. **First**: Check knowledge base for established patterns
2. **Second**: Search codebase for similar implementations
3. **Third**: Reference PRD for new requirements
4. **Never**: Invent patterns that conflict with knowledge base

This ensures all implementations follow established GPU Operator conventions.
