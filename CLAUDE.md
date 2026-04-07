# GPU Operator Development Workflow - Agent Instruction Set

This is the canonical instruction set for feature development in the GPU Operator.
All agents in this workflow read this file.

## What This Is

A structured software engineering workflow that converts Product Requirements
Documents (PRDs) into production-ready GPU Operator features with full test coverage
and automation. The workflow executes sequentially with validation gates at each phase,
ensuring quality and correctness before proceeding.

## Execution Modes

The workflow supports two execution modes:

### Autonomous Mode (Default)
- Runs all phases automatically
- Stops only on validation failures
- Zero user interaction unless errors occur
- Usage: `/implement-feature docs/feature-prds/my-feature.md`

### Interactive Mode
- Asks for approval before each phase
- Shows results after each phase completion
- User can Continue, Skip, or Stop at each gate
- Usage: `/implement-feature docs/feature-prds/my-feature.md --mode=interactive`

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
| **gpu-operator-orchestrator** | opus | Coordinator: parses PRD, creates task breakdown, dispatches sequential agents, validates each phase |
| **operator-implementation** | sonnet | Code: writes CRD types, Go implementation, controllers, handlers |
| **unit-test-agent** | sonnet | Unit Testing: writes Go unit tests (*_test.go files) |
| **e2e-test-agent** | sonnet | E2E Testing: writes tests for tests/e2e/ |
| **pytest-agent** | sonnet | Integration: writes Python tests for tests/pytests/ |
| **docs-agent** | sonnet | Documentation: updates user docs, developer docs, Helm charts, examples |

Agent definitions: `.claude/agents/`

## Workflow Phases

### Phase 1: PRD Analysis & Planning

1. Read PRD file from docs/feature-prds/
2. Extract implementation requirements
3. Create structured task list with TodoWrite
4. Validate PRD completeness
5. Determine execution mode (auto or interactive)

### Phase 2: Code Implementation

1. Launch operator-implementation agent (blocking, synchronous)
2. Wait for completion
3. **Validation Gate**: 
   - Run `make generate`
   - Run `make manifests`
   - Run `make build`
4. ❌ **STOP** if build fails (auto mode) or ask user (interactive mode)
5. ✅ Continue to Phase 3 if validation passes

### Phase 3: Unit Test Generation

1. Launch unit-test-agent (blocking, synchronous)
2. Generate Go unit tests in *_test.go files
3. **Validation Gate**:
   - Run `make test`
4. ❌ **STOP** if tests fail or ask user (interactive mode)
5. ✅ Continue to Phase 4 if all tests pass

### Phase 4: E2E Test Generation

1. Launch e2e-test-agent (blocking, synchronous)
2. Generate Ginkgo/Gomega tests in tests/e2e/
3. **Validation Gate**:
   - Run `make test-e2e`
4. ❌ **STOP** if tests fail or ask user (interactive mode)
5. ✅ Continue to Phase 5 if all tests pass

### Phase 5: Integration Test Generation

1. Launch pytest-agent (blocking, synchronous)
2. Generate Python tests in tests/pytests/
3. **Validation Gate**:
   - Run pytest
4. ❌ **STOP** if tests fail or ask user (interactive mode)
5. ✅ Continue to Phase 6 if all tests pass

### Phase 6: Documentation

1. Launch docs-agent (blocking, synchronous)
2. Update user documentation
3. Update Helm charts and examples
4. Add release notes
5. **Validation Gate**: Verify docs build successfully

### Phase 7: Final Validation & Report

1. Verify all phases completed successfully
2. Verify all tests passing
3. Generate comprehensive completion report
4. Suggest next steps (create PR, manual testing, etc.)

## Key Principles

1. **Always start from PRD** - Never implement without a PRD
2. **Sequential validation gates** - Each phase validates before proceeding
3. **Stop on failure** - Don't continue if validation fails (auto mode)
4. **Shared progress tracking** - All agents update TodoWrite
5. **Dependent execution** - Each agent depends on previous phase success
6. **Fail fast** - Report errors immediately at validation gates
7. **Use existing patterns** - Search codebase and knowledge base first
8. **Complete the checklist** - Every PRD item must be addressed
9. **Test what you build** - Progressive test coverage (unit → e2e → integration)
10. **User control** - Interactive mode allows phase-by-phase approval

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
