# GPU Operator Development Workflow - Quick Start

## What Is This?

An automated workflow that converts feature PRDs into production-ready GPU Operator code with full test coverage.

## Quick Start

### 1. Create a Feature PRD

```bash
# Copy the template
cp docs/feature-prds/TEMPLATE.md docs/feature-prds/my-new-feature.md

# Edit it to describe your feature
# Fill in all sections: Overview, Technical Spec, Implementation Plan, Tests, Docs
```

### 2. Run the Workflow

```bash
# In Claude Code, run:
/implement-feature docs/feature-prds/my-new-feature.md
```

### 3. The Workflow Will:

- ✅ Parse your PRD and create task breakdown
- ✅ Launch 4 parallel agents:
  - **operator-implementation**: Writes CRD types, controller logic, handlers
  - **e2e-test-agent**: Writes E2E tests in tests/e2e/
  - **pytest-agent**: Writes integration tests in tests/pytests/
  - **docs-agent**: Updates documentation and Helm charts
- ✅ Run `make generate`, `make manifests`, `make build`
- ✅ Run tests: `make test`
- ✅ Generate completion report

### 4. Review and Merge

```bash
# Review the changes
git diff

# Test manually on your cluster

# Create PR
gh pr create --title "Add my-new-feature" --body "Implements PRD-GPU-OPERATOR-..."
```

## What Gets Created

### Code
- `api/v1alpha1/deviceconfig_types.go` - CRD spec/status types
- `internal/<component>/handler.go` - Component handler
- `internal/controllers/device_config_reconciler.go` - Controller integration
- `cmd/main.go` - Handler initialization

### Tests
- `tests/e2e/<feature>_test.go` - End-to-end tests
- `tests/pytests/test_<feature>.py` - Integration tests

### Documentation
- `docs/<feature>/README.md` - Feature documentation
- `helm-charts-k8s/values.yaml` - Helm values
- `config/samples/sample_<feature>.yaml` - Example DeviceConfig

### Generated
- `config/crd/bases/*.yaml` - CRD manifests (via make manifests)
- `api/v1alpha1/zz_generated.deepcopy.go` - DeepCopy methods (via make generate)

## PRD Requirements

Your PRD must include:

1. **Feature Overview** - What, why, goals
2. **Technical Specification** - CRD changes, new types
3. **Controller Changes** - Reconciliation logic
4. **Implementation Plan** - File checklist with exact changes
5. **Testing Requirements** - E2E and integration test scenarios
6. **Documentation Updates** - Docs to update

## Example Workflow Run

```
User: /implement-feature docs/feature-prds/add-gpu-metrics.md

Claude: 
1. Reading PRD... ✅
2. Creating task list... ✅ (12 tasks)
3. Launching agents in parallel...
   - operator-implementation: Running in background
   - e2e-test-agent: Running in background
   - pytest-agent: Running in background
   - docs-agent: Running in background
4. Waiting for agents... (takes 3-5 minutes)
   - operator-implementation: Complete ✅
   - e2e-test-agent: Complete ✅
   - pytest-agent: Complete ✅
   - docs-agent: Complete ✅
5. Running validation...
   - make generate: SUCCESS ✅
   - make manifests: SUCCESS ✅
   - make build: SUCCESS ✅
   - make test: 30/30 passed ✅
6. Generating report...

# Feature Implementation Complete: GPU Metrics Collection

## Status: ✅ SUCCESS

Files modified: 8
Tests added: 15
Documentation updated: 4 files

Next: Review changes and create PR
```

## Tips

1. **Write detailed PRDs** - The better your PRD, the better the implementation
2. **Include file checklist** - Be specific about which files need changes
3. **Specify test scenarios** - Clear test requirements produce better tests
4. **Review agent output** - Agents report what they did
5. **Test before merging** - Always test on a real cluster

## Troubleshooting

### PRD validation fails
- Check that all required sections are present
- Ensure file checklist is complete
- Verify technical specifications are clear

### Build fails
- Check agent output for errors
- Review generated code
- Run `make generate` and `make manifests` manually

### Tests fail
- Review test output for specific failures
- Check if feature logic has bugs
- Verify tests match actual behavior

## Learn More

- [CLAUDE.md](CLAUDE.md) - Complete workflow specification
- [docs/feature-prds/TEMPLATE.md](../docs/feature-prds/TEMPLATE.md) - PRD template
- [agents/](agents/) - Individual agent specifications
