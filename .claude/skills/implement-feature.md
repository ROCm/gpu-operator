---
name: implement-feature
description: >
  Orchestrates the sequential GPU Operator feature development workflow from PRD to production-ready code.
  Supports autonomous (default) and interactive modes. Converts PRDs into implementation with full test coverage.
  Triggered with /implement-feature.
version: 0.3.0
---

# Implement Feature - GPU Operator Sequential Workflow

Converts a feature PRD into production-ready GPU Operator code through sequential phases with validation gates.

## Usage

```bash
# Simply invoke the skill - it will ask you which PRD and which mode
/implement-feature

# Or provide the PRD path directly
/implement-feature docs/feature-prds/add-newfeature.md

# Or specify mode
/implement-feature --mode=interactive
```

## How It Works

When you run `/implement-feature`, the skill will:
1. **Ask which PRD** to implement (shows list of available PRDs)
2. **Ask which mode** (Autonomous or Interactive)
3. Then execute the sequential workflow

## Important: Knowledge Base

All agents consult the project knowledge base in `knowledge/`:
- `codebase-structure.md` - Repository layout
- `architecture-overview.md` - Operator architecture
- `deviceconfig-api-spec.md` - CRD specifications  
- `component-details.md` - Component patterns

Agents use this to ensure implementations follow established patterns.

## Workflow: Sequential Phases with Validation Gates

---

### Phase 0: PRD and Mode Selection

**Step 1: Ask user to select PRD**

```python
# List available PRDs
prd_files = glob("docs/feature-prds/*.md")
prd_files = [f for f in prd_files if "TEMPLATE" not in f.upper()]

if not prd_files:
    error("No PRD files found in docs/feature-prds/")
    suggest("Create a PRD using docs/feature-prds/TEMPLATE.md")
    exit()

# Ask user to select
response = AskUserQuestion(
    questions: [{
        question: "Which feature would you like to implement?",
        header: "Select PRD",
        multiSelect: false,
        options: [
            {
                label: Path(f).stem,  # Filename without .md
                description: f"Implement feature from {Path(f).name}"
            } 
            for f in prd_files
        ]
    }]
)

prd_file = response  # Full path to selected PRD
```

**Step 2: Ask user to select mode**

```python
response = AskUserQuestion(
    questions: [{
        question: "How would you like to run the workflow?",
        header: "Execution Mode",
        multiSelect: false,
        options: [
            {
                label: "Autonomous (Recommended)",
                description: "Run all phases automatically, stop only on errors"
            },
            {
                label: "Interactive",
                description: "Ask for approval before each phase"
            }
        ]
    }]
)

mode = "auto" if "Autonomous" in response else "interactive"

print(f"✅ PRD: {prd_file}")
print(f"✅ Mode: {mode}")
```

---

### Phase 1: PRD Validation & Planning

**Actions:**
1. Read PRD file (from Phase 0 selection)
2. Verify all required sections present:
   - Feature Overview
   - Technical Specification
   - Implementation Plan
   - Testing Requirements
   - Documentation Updates
3. Parse execution mode (--mode parameter)
4. Create TodoWrite task breakdown
5. Report plan to user

**Validation:**
- PRD must have all required sections
- File paths in implementation plan must be valid

**Output:**
- Task list created
- Mode determined
- Ready to start implementation

---

### Phase 2: Code Implementation

**Interactive Gate (if mode=interactive):**
```
Ready to start Phase 2: Code Implementation?

The implementation will:
- Add CRD types to api/v1alpha1/
- Create/update component handlers
- Update controller reconciliation logic
- Modify cmd/main.go for initialization

Options:
→ Continue - Start implementation
  Skip - Skip to unit tests
  Stop - End workflow now
```

**Agent Execution:**
```python
Agent(
  subagent_type: "operator-implementation",
  description: "Implement CRD and controller code",
  prompt: """Implement the feature from {prd_file}.

IMPORTANT: Read knowledge base files first:
1. knowledge/codebase-structure.md
2. knowledge/architecture-overview.md
3. knowledge/deviceconfig-api-spec.md
4. knowledge/component-details.md

Then implement:
1. CRD types (DeviceConfigSpec/Status fields)
2. Component handler (internal/<component>/handler.go)
3. Controller integration (internal/controllers/)
4. Main initialization (cmd/main.go)

After implementation, run validation:
- make generate
- make manifests
- make build

Report all validation results. STOP if any fail.
Update TodoWrite as you complete tasks.""",
  run_in_background: false  # BLOCKING - wait for completion
)
```

**Validation Gates:**
1. `make generate` must succeed
2. `make manifests` must succeed
3. `make build` must succeed

**Autonomous Mode:** If any validation fails → STOP and report error

**Interactive Mode:** If validation fails, ask:
```
Build validation failed!

Error: [error details]

What should we do?
→ Stop and review errors (recommended)
  Retry - Fix and regenerate
  Continue anyway (not recommended)
```

**Output:**
- CRD types implemented
- Controller code written
- Build successful
- Ready for unit tests

---

### Phase 3: Unit Test Generation

**Interactive Gate (if mode=interactive):**
```
Phase 2 complete! Results:
✅ Implementation successful
✅ Build validation passed

Files modified:
- api/v1alpha1/deviceconfig_types.go
- internal/<component>/handler.go
- internal/controllers/device_config_reconciler.go

Ready to start Phase 3: Unit Test Generation?
→ Continue - Generate Go unit tests
  Skip - Skip to E2E tests
  Stop - End here (write tests manually)
```

**Agent Execution:**
```python
Agent(
  subagent_type: "unit-test-agent",
  description: "Generate Go unit tests",
  prompt: """Generate Go unit tests for the implementation in {prd_file}.

Implementation is complete and building successfully.

Generate unit tests:
1. Create *_test.go files alongside implementation
2. Test handler Reconcile() and Cleanup() methods
3. Test validation logic
4. Test error cases
5. Use table-driven tests where appropriate

After generation, run: make test

Report test results. All tests must pass.
Update TodoWrite.""",
  run_in_background: false
)
```

**Validation Gate:**
- `make test` - All Go unit tests must pass

**Failure Handling:**
- **Auto mode**: STOP, report which tests failed
- **Interactive mode**: Ask user (Stop/Retry/Continue)

**Output:**
- Unit tests created
- All tests passing
- Ready for E2E tests

---

### Phase 4: E2E Test Generation

**Interactive Gate (if mode=interactive):**
```
Phase 3 complete! Results:
✅ Unit tests generated: 8 test cases
✅ make test: 8/8 passed

Ready to start Phase 4: E2E Test Generation?
→ Continue - Generate E2E tests
  Skip - Skip to integration tests
  Stop - End here
```

**Agent Execution:**
```python
Agent(
  subagent_type: "e2e-test-agent",
  description: "Generate E2E tests",
  prompt: """Generate E2E tests for {prd_file}.

Implementation and unit tests are complete and passing.

Generate E2E tests in tests/e2e/:
1. Feature enable/disable scenarios
2. Configuration application tests
3. Status reporting verification
4. Upgrade scenario tests
5. OpenShift compatibility (if applicable)

Use Ginkgo/Gomega framework.
Reference knowledge/architecture-overview.md for test patterns.

After generation, tests will be run in CI.
Update TodoWrite.""",
  run_in_background: false
)
```

**Validation Gate:**
- E2E tests created
- Syntax validation (Go build)
- (Optional) Run `make test-e2e` if cluster available

**Output:**
- E2E tests created
- Ready for integration tests

---

### Phase 5: Integration Test Generation

**Interactive Gate (if mode=interactive):**
```
Phase 4 complete! Results:
✅ E2E tests generated: 6 test cases
✅ Tests validated

Ready to start Phase 5: Integration Test Generation?
→ Continue - Generate pytest tests
  Skip - Skip to documentation
  Stop - End here
```

**Agent Execution:**
```python
Agent(
  subagent_type: "pytest-agent",
  description: "Generate integration tests",
  prompt: """Generate pytest integration tests for {prd_file}.

All implementation and E2E tests are complete.

Generate tests in tests/pytests/:
1. Configuration validation tests
2. Status reporting tests
3. Error handling tests
4. Platform-specific tests (if applicable)

Follow existing pytest patterns in the codebase.
Update TodoWrite.""",
  run_in_background: false
)
```

**Validation Gate:**
- Pytest tests created
- Syntax validation (pytest --collect-only)

**Output:**
- Integration tests created
- Ready for documentation

---

### Phase 6: Documentation

**Interactive Gate (if mode=interactive):**
```
Phase 5 complete! Results:
✅ Integration tests generated: 4 test cases

Ready to start Phase 6: Documentation?
→ Continue - Update all documentation
  Skip - Skip docs (update manually)
  Stop - End here
```

**Agent Execution:**
```python
Agent(
  subagent_type: "docs-agent",
  description: "Update documentation",
  prompt: """Update all documentation for {prd_file}.

All implementation and tests are complete and passing.

Update:
1. User documentation (docs/<feature>/)
2. Helm chart values and README
3. Example DeviceConfigs (config/samples/)
4. Release notes

Follow documentation patterns from existing features.
Update TodoWrite.""",
  run_in_background: false
)
```

**Validation Gate:**
- Documentation files updated
- Markdown/YAML syntax valid

**Output:**
- Documentation complete
- Ready for final report

---

### Phase 7: Final Report

**Generate comprehensive report:**

```markdown
# Feature Implementation Complete: {feature-name}

## Status: ✅ SUCCESS

### Implementation Summary
- **Files Modified**: [list of files]
- **CRD Changes**: [spec/status fields added]
- **New Components**: [handlers created]

### Test Coverage
- ✅ Unit Tests: X/X passed
- ✅ E2E Tests: Y test cases created
- ✅ Integration Tests: Z test cases created

### Documentation
- ✅ User docs updated
- ✅ Helm charts updated
- ✅ Examples added
- ✅ Release notes updated

### Build Validation
- ✅ make generate: SUCCESS
- ✅ make manifests: SUCCESS
- ✅ make build: SUCCESS
- ✅ make test: SUCCESS

### Next Steps
1. Review all changes: git diff
2. Test on real cluster
3. Create pull request: gh pr create
4. Reference PRD: {prd_file}

### Files Changed
[Complete list of modified/created files]
```

---

## Error Handling

### Build Failures
**Auto Mode:**
```
❌ Phase 2 Failed: Build validation

Error: undefined: NewFeatureSpec.InvalidField
  at api/v1alpha1/deviceconfig_types.go:123

Workflow stopped. Fix the error and retry:
  /implement-feature {prd_file}
```

**Interactive Mode:**
```
❌ Build failed!

Error: [details]

What should we do?
→ Stop and review (recommended)
  Retry implementation
  Continue anyway
```

### Test Failures
**Auto Mode:**
```
❌ Phase 3 Failed: Unit tests

Failed tests (2/8):
- TestHandler_Reconcile: validation error
- TestHandler_Cleanup: nil pointer

Fix tests and run: make test
```

**Interactive Mode:**
User chooses: Stop/Retry/Continue

---

## Mode Detection Pattern

```python
# Parse mode from args
mode = parse_args(args).get("--mode", "auto")

# Validate mode
if mode not in ["auto", "interactive"]:
  error("Invalid mode. Use 'auto' or 'interactive'")

# Store mode for all phases
workflow_mode = mode
```

## Phase Transition Pattern

```python
# Before each phase (if interactive mode):
if workflow_mode == "interactive":
  response = AskUserQuestion(
    question: "Ready to start Phase X: {phase_name}?",
    header: f"Phase {X}",
    options: [
      {"label": "Continue", "description": "Start {phase_name}"},
      {"label": "Skip", "description": "Skip to next phase"},
      {"label": "Stop", "description": "End workflow here"}
    ]
  )
  
  if response == "Stop":
    generate_partial_report_and_exit()
  elif response == "Skip":
    goto_next_phase()
  # else: continue

# Execute agent (blocking)
result = Agent(subagent_type: "...", ...)

# Validate
validation_result = run_validation()

# Handle validation failure
if validation_failed:
  if workflow_mode == "interactive":
    ask_user_what_to_do()
  else:
    report_error_and_stop()
```

## Success Criteria

Workflow is complete when:

✅ All 7 phases executed successfully
✅ All validation gates passed
✅ All tests passing (unit + e2e + integration)
✅ Build succeeds with no errors
✅ Documentation updated
✅ Final report generated

## Key Changes from v0.2.0

- ✅ Added sequential execution (removed parallelism)
- ✅ Added validation gates after each phase
- ✅ Added autonomous and interactive modes
- ✅ Added unit-test-agent phase
- ✅ Added stop-on-failure behavior
- ✅ Agents run synchronously (blocking)
- ✅ Progressive test coverage (unit → e2e → integration)
