# Sequential Workflow Changes

This document outlines the changes needed to convert from parallel to sequential workflow execution.

## Execution Modes

The workflow supports TWO execution modes:

### 1. Autonomous Mode (Default)
```bash
/implement-feature docs/feature-prds/my-feature.md
# or explicitly:
/implement-feature docs/feature-prds/my-feature.md --mode=auto
```
- Runs all phases automatically
- Stops only on validation failures
- Reports progress at each phase
- Best for: Well-defined PRDs, experienced users

### 2. Interactive Mode
```bash
/implement-feature docs/feature-prds/my-feature.md --mode=interactive
```
- Asks for approval before each phase
- Shows results of previous phase
- User can: Continue, Skip, or Stop
- Best for: Complex features, learning, careful review

## Current vs New Workflow

### Current (Parallel)
```
Phase 1: PRD Analysis
Phase 2: Parallel Execution
  ├─ operator-implementation (parallel)
  ├─ e2e-test-agent (parallel)
  ├─ pytest-agent (parallel)
  └─ docs-agent (parallel)
Phase 3: Build & Test Validation
Phase 4: Final Report
```

### New (Sequential with Validation Gates)
```
Phase 1: PRD Analysis & Planning
Phase 2: Code Implementation
  └─ operator-implementation agent
  └─ Validation: make generate && make manifests && make build
Phase 3: Unit Test Generation
  └─ unit-test-agent (NEW)
  └─ Validation: make test (Go unit tests must pass)
Phase 4: E2E Test Generation
  └─ e2e-test-agent
  └─ Validation: make test-e2e (E2E tests must pass)
Phase 5: Integration Test Generation  
  └─ pytest-agent
  └─ Validation: pytest runs (integration tests must pass)
Phase 6: Documentation
  └─ docs-agent
  └─ Validation: docs build successfully
Phase 7: Final Report & Summary
```

## Files to Change

### 1. CLAUDE.md

**Changes:**
- Line 10: Change "four phases executed in parallel" → "sequential phases with validation gates"
- Line 36: Update agents table - add unit-test-agent
- Lines 44-68: Replace Phase 2-4 with new sequential phases
- Line 72: Change "Maximize parallelism" → "Sequential validation gates"
- Line 74: Change "Independent agents" → "Dependent agents with validation"

**New content:**
```markdown
## Workflow Phases

### Phase 1: PRD Analysis & Planning
1. Read PRD file from docs/feature-prds/
2. Extract implementation requirements
3. Create structured task list with TodoWrite
4. Validate PRD completeness
5. Generate implementation plan

### Phase 2: Code Implementation
1. Launch operator-implementation agent
2. Wait for completion
3. Validate: make generate
4. Validate: make manifests
5. Validate: make build
6. ❌ STOP if any validation fails

### Phase 3: Unit Test Generation
1. Launch unit-test-agent
2. Generate Go unit tests (*_test.go)
3. Validate: make test
4. ❌ STOP if tests fail

### Phase 4: E2E Test Generation
1. Launch e2e-test-agent
2. Generate Ginkgo/Gomega tests
3. Validate: make test-e2e
4. ❌ STOP if tests fail

### Phase 5: Integration Test Generation
1. Launch pytest-agent
2. Generate Python integration tests
3. Validate: pytest execution
4. ❌ STOP if tests fail

### Phase 6: Documentation
1. Launch docs-agent
2. Update user/developer docs
3. Update Helm charts
4. Validate: docs build

### Phase 7: Final Validation
1. Verify all phases completed
2. Verify all tests passing
3. Generate completion report
4. Suggest next steps (PR creation)
```

### 2. .claude/skills/implement-feature.md

**Changes:**
- Line 6-7: Update description - remove "parallel"
- Line 8: Bump version to 0.3.0
- Lines 41-91: Replace parallel dispatch with sequential execution
- Add mode parameter support

**New content:**
```markdown
## Usage

```bash
# Autonomous mode (default) - runs all phases automatically
/implement-feature docs/feature-prds/add-newfeature.md

# Interactive mode - asks before each phase
/implement-feature docs/feature-prds/add-newfeature.md --mode=interactive
```

## Parameters

- `prd_file` (required): Path to PRD file
- `--mode` (optional): Execution mode
  - `auto` (default): Autonomous execution, stop only on errors
  - `interactive`: Ask for approval before each phase

## Workflow Steps

### Step 1: PRD Validation
Read and verify PRD has all required sections.

### Step 2: Task Breakdown
Create TodoWrite with all implementation tasks.

### Step 3: Detect Execution Mode
Parse --mode parameter or default to "auto".

### Step 4: Sequential Agent Execution with Validation Gates

#### Phase A: Implementation
```python
# INTERACTIVE MODE: Ask before starting
if mode == "interactive":
  response = AskUserQuestion(
    question: "Ready to start Phase 2: Code Implementation?",
    options: [
      "Continue - Start implementation",
      "Skip - Skip to next phase", 
      "Stop - End workflow now"
    ]
  )
  if response == "Stop":
    exit_workflow()
  if response == "Skip":
    goto_phase_B()

# Launch implementation agent (BLOCKING - no run_in_background)
Agent(
  subagent_type: "operator-implementation",
  description: "Implement CRD and controller",
  prompt: """Implement feature from {prd_file}.
  
  Read knowledge base first, then implement:
  1. CRD types
  2. Controller logic
  3. Component handlers
  4. Update main.go
  
  After implementation, run:
  - make generate
  - make manifests
  - make build
  
  Report any errors immediately."""
)

# Validation Gate 1: Build must succeed
if build_failed:
  if mode == "interactive":
    response = AskUserQuestion(
      question: "Build failed! What should we do?",
      options: [
        "Stop and review errors",
        "Retry implementation",
        "Continue anyway (not recommended)"
      ]
    )
    handle_response(response)
  else:  # auto mode
    report_error_and_stop()
```

#### Phase B: Unit Tests
```python
Agent(
  subagent_type: "unit-test-agent",
  description: "Generate Go unit tests",
  prompt: """Generate unit tests for {prd_file} implementation.
  
  Create *_test.go files alongside implementation.
  Cover:
  - Type validation
  - Handler logic
  - Error cases
  - Edge conditions
  
  After generation, run: make test
  Report results."""
)

# Validation Gate 2: Unit tests must pass
if unit_tests_failed:
  report_failures_and_stop()
```

#### Phase C: E2E Tests
```python
Agent(
  subagent_type: "e2e-test-agent",
  description: "Generate E2E tests",
  prompt: """Generate E2E tests for {prd_file}.
  
  Implementation is complete and unit tested.
  Write tests in tests/e2e/.
  
  After generation, run: make test-e2e
  Report results."""
)

# Validation Gate 3: E2E tests must pass
if e2e_tests_failed:
  report_failures_and_stop()
```

#### Phase D: Integration Tests
```python
Agent(
  subagent_type: "pytest-agent",
  description: "Generate integration tests",
  prompt: """Generate pytest integration tests for {prd_file}.
  
  Write tests in tests/pytests/.
  
  After generation, run pytest.
  Report results."""
)

# Validation Gate 4: Integration tests must pass
if pytest_failed:
  report_failures_and_stop()
```

#### Phase E: Documentation
```python
Agent(
  subagent_type: "docs-agent",
  description: "Update documentation",
  prompt: """Update all docs for {prd_file}.
  
  All implementation and tests are complete and passing.
  Update:
  - User documentation
  - Helm charts
  - Examples
  - Release notes"""
)
```

### Step 4: Final Report
Generate comprehensive completion report with:
- ✅ Implementation files modified
- ✅ Unit test coverage
- ✅ E2E test results
- ✅ Integration test results
- ✅ Documentation updated
- 🎯 Next step: Create PR
```

### 3. NEW FILE: .claude/agents/unit-test-agent.md

**Create new agent for Go unit test generation:**
```markdown
---
name: unit-test-agent
description: Unit test agent for GPU Operator. Generates Go unit tests for implementation code.
model: sonnet
color: cyan
tools: Read, Write, Edit, Glob, Grep, Bash, TodoWrite
---

You are the **unit-test-agent** for GPU Operator.
You generate Go unit tests for newly implemented code.

## Your Responsibilities

1. Read the implementation code
2. Identify functions/methods to test
3. Generate *_test.go files
4. Cover happy paths and error cases
5. Run make test and verify
6. Update TodoWrite
7. Report coverage

## Test Patterns

### Pattern 1: Handler Tests
```go
func TestNewFeatureHandler_Reconcile(t *testing.T) {
    tests := []struct {
        name    string
        dc      *gpuev1alpha1.DeviceConfig
        wantErr bool
    }{
        {
            name: "feature enabled",
            dc: &gpuev1alpha1.DeviceConfig{
                Spec: gpuev1alpha1.DeviceConfigSpec{
                    NewFeature: &gpuev1alpha1.NewFeatureSpec{
                        Enabled: pointer.Bool(true),
                    },
                },
            },
            wantErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            h := &NewFeatureHandler{}
            err := h.Reconcile(context.Background(), tt.dc)
            if (err != nil) != tt.wantErr {
                t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Coverage Requirements
- All public functions
- Error cases
- Validation logic
- Edge conditions
```

## Key Principle Changes

**OLD:**
- Maximize parallelism
- Independent agents
- All agents run simultaneously

**NEW:**
- Sequential validation gates
- Dependent agents (each depends on previous success)
- Stop on first failure
- Progressive validation (build → unit → e2e → integration → docs)

## Benefits of Sequential Approach

1. **Early failure detection** - Catch build issues before writing tests
2. **Clean separation** - Each phase has clear inputs/outputs
3. **Better debugging** - Know exactly which phase failed
4. **Progressive validation** - Each phase builds on previous success
5. **Clearer progress** - User sees phase-by-phase completion

## Example Execution Flows

### Autonomous Mode (Success Case)
```
User: /implement-feature docs/feature-prds/add-metrics.md

Claude:
✅ Phase 1: PRD validated
✅ Phase 2: Creating task breakdown...
📋 Tasks created: 12 items

🤖 Phase 2: Starting implementation...
   operator-implementation agent working...
   ✅ CRD types added
   ✅ Handler created
   ✅ Controller updated
   ✅ make generate: SUCCESS
   ✅ make manifests: SUCCESS
   ✅ make build: SUCCESS

🤖 Phase 3: Generating unit tests...
   unit-test-agent working...
   ✅ Created 8 test cases
   ✅ make test: 8/8 passed

🤖 Phase 4: Generating E2E tests...
   e2e-test-agent working...
   ✅ Created 6 E2E test cases
   ✅ make test-e2e: 6/6 passed

🤖 Phase 5: Generating integration tests...
   pytest-agent working...
   ✅ Created 4 integration tests
   ✅ pytest: 4/4 passed

🤖 Phase 6: Updating documentation...
   docs-agent working...
   ✅ Updated 5 documentation files

🎉 All phases complete! Feature ready for PR.
```

### Autonomous Mode (Failure Case)
```
User: /implement-feature docs/feature-prds/complex-feature.md

Claude:
✅ Phase 1: PRD validated
✅ Phase 2: Creating task breakdown...

🤖 Phase 2: Starting implementation...
   operator-implementation agent working...
   ✅ CRD types added
   ✅ Handler created
   ❌ make build: FAILED

Error: undefined: NewFeatureSpec.InvalidField
  at api/v1alpha1/deviceconfig_types.go:123

❌ Workflow stopped at Phase 2: Build validation failed

Next steps:
1. Review the error above
2. Fix the implementation issue
3. Re-run /implement-feature or fix manually
```

### Interactive Mode
```
User: /implement-feature docs/feature-prds/add-metrics.md --mode=interactive

Claude:
✅ Phase 1: PRD validated
✅ Task breakdown created: 12 items

Ready to start Phase 2: Code Implementation?
→ Continue - Start implementation
  Skip - Skip to next phase
  Stop - End workflow now

User: [Selects "Continue"]

Claude:
🤖 Phase 2: Implementation running...
   ✅ Implementation complete
   ✅ Build validation: SUCCESS

Phase 2 complete. Results:
- Modified: api/v1alpha1/deviceconfig_types.go
- Created: internal/metrics/handler.go
- Updated: internal/controllers/device_config_reconciler.go
- Build: ✅ SUCCESS

Ready to start Phase 3: Unit Test Generation?
→ Continue - Generate unit tests
  Skip - Skip to E2E tests
  Stop - End here, I'll write tests manually

User: [Selects "Continue"]

Claude:
🤖 Phase 3: Unit test generation...
   ✅ Created 8 test cases
   ✅ make test: 8/8 passed

Ready to start Phase 4: E2E Test Generation?
→ Continue
  Skip
  Stop

[... continues for each phase ...]
```

### Interactive Mode (With Failure)
```
🤖 Phase 3: Unit test generation...
   ✅ Created 8 test cases
   ❌ make test: 2/8 failed

Unit tests failed! What should we do?
→ Stop and review errors (recommended)
  Retry - Fix and regenerate tests
  Continue anyway - Proceed to E2E tests

User: [Selects "Stop and review errors"]

Claude:
❌ Workflow paused at Phase 3

Failed tests:
- TestMetricsHandler_Reconcile (validation error)
- TestMetricsHandler_Cleanup (nil pointer)

You can:
1. Fix the tests manually
2. Run: make test to verify fixes
3. Resume with: /implement-feature --resume-from=phase-4
```

## Implementation Pattern in implement-feature.md

```markdown
## Phase Execution Pattern

For each phase:

1. **Check mode**
   ```python
   if mode == "interactive":
       ask_user_permission()
   ```

2. **Execute agent**
   ```python
   agent_result = Agent(
       subagent_type: "...",
       prompt: "...",
       run_in_background: false  # Always blocking for sequential
   )
   ```

3. **Validate results**
   ```python
   validation_result = run_validation_command()
   ```

4. **Handle outcome**
   ```python
   if validation_failed:
       if mode == "interactive":
           ask_user_what_to_do()  # Stop/Retry/Continue
       else:  # auto mode
           report_and_stop()
   else:
       report_success()
       if mode == "interactive":
           ask_before_next_phase()
   ```

5. **Continue to next phase** (if approved/auto)
```

## Implementation Steps

1. Update CLAUDE.md with new phases and modes
2. Update implement-feature.md with sequential logic + mode handling
3. Create unit-test-agent.md
4. Add mode parameter parsing
5. Add AskUserQuestion calls for interactive mode
6. Test both modes with a simple PRD
7. Iterate based on results
