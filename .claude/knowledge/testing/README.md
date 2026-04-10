# Testing Knowledge Base

This directory contains test development and debugging knowledge - patterns, solutions, and best practices for writing and fixing GPU Operator tests.

## Purpose

Testing KB answers questions like:

- How do I write a test for X?
- Why is my test failing?
- What's the correct pattern for Y?
- How do I debug Z issue?

This is **separate from** product knowledge (see `../products/`) which covers how features actually work.

## Directory Structure

```bash
testing/
├── README.md           # This file
├── dcm/                # DCM test patterns and debugging
│   ├── cleanup-on-failure.md
│   ├── driver-reload-timing.md
│   ├── partition-profile-files.md
│   └── verify-label-multi-node.md
├── npd/                # NPD test patterns (to be added)
└── common/             # Generic pytest/K8s patterns (to be added)
```

## Current Content

### DCM Testing

#### [cleanup-on-failure.md](dcm/cleanup-on-failure.md)

**Issue**: Failed tests leave nodes tainted and labeled
**Solution**: Use try/finally blocks to ensure cleanup always runs

#### [driver-reload-timing.md](dcm/driver-reload-timing.md)

**Issue**: Tests fail with JSON parse errors after partition operations
**Solution**: Wait for driver reload using `K8Helper.wait_for_driver_reload()`

#### [partition-profile-files.md](dcm/partition-profile-files.md)

**Reference**: Complete guide to partition profile formats, validation rules, and test patterns

#### [verify-label-multi-node.md](dcm/verify-label-multi-node.md)

**Issue**: Tests pass when some nodes fail in multi-node clusters
**Solution**: Check ALL GPU nodes, not just first one

## Planned Content

### NPD Testing

- NPD DaemonSet deployment patterns
- ConfigMap volume mounting (critical for multi-file configs)
- amdgpuhealth binary placement testing
- Condition detection validation
- Cross-platform testing (K8s + OpenShift)

### Common Testing Patterns

- pytest parametrize best practices
- Kubernetes helper usage patterns
- Fixture design and scope
- Test cleanup with finalizers
- Multi-node test strategies
- Error handling and retry logic

## Contributing

When you solve a test issue:

1. **Document the pattern**: Create or update KB entry
2. **Include symptoms**: How to recognize this issue
3. **Show the solution**: Code examples and explanations
4. **Add verification**: How to confirm it's fixed
5. **Reference commits**: Link to actual fixes

### KB Entry Template

```markdown

# Test KB: [Problem Title]

## Issue

Brief description of the problem

## Root Cause

Why it happens

## Symptoms

How to recognize this issue in test failures

## Solution

Code examples and step-by-step fix

## Verification

How to confirm it's resolved

## Related Issues

Cross-references to other KB entries

## Files Modified

Test files affected

## Commits

Git commits with the fix
```

## Relationship to Other Directories

- **`products/`** - What the feature does
- **`testing/`** (here) - How to test the feature
- **`skills/`** - Workflows combining product + test knowledge

Example flow:

1. Understand the feature: `products/dcm/partition-modes.md`
2. Learn test patterns: `testing/dcm/partition-test-patterns.md`
3. Fix timing issues: `testing/dcm/driver-reload-timing.md`
4. Use complete workflow: `skills/pytest-dcm-dev.md`

## Usage Examples

### Debugging Test Failure

1. **Identify error type** from test logs
2. **Search KB** for matching symptoms
3. **Apply solution** from KB entry
4. **Verify fix** using verification steps
5. **Update KB** if new pattern discovered

### Writing New Tests

1. **Review test structure** in appropriate testing/ directory
2. **Follow patterns** from existing KB entries
3. **Add cleanup logic** (see cleanup-on-failure.md)
4. **Handle timing** (see driver-reload-timing.md)
5. **Validate multi-node** (see verify-label-multi-node.md)

## Quick Links

- DCM test file: `tests/pytests/k8/gpu-operator/test_config_manager.py`
- NPD test file: `tests/pytests/k8/gpu-operator/test_node_problem_detector.py`
- K8s utilities: `tests/pytests/lib/k8_util.py`
- Common helpers: `tests/pytests/lib/util.py`
- NPD utilities: `tests/pytests/lib/npd_util.py`
