---
name: documentation-patterns
description: Complete guide to GPU Operator documentation structure, update procedures, standards, and build system
type: reference
---

# GPU Operator Documentation Patterns

This guide provides the authoritative reference for updating documentation in the AMD GPU Operator repository.

## Table of Contents

1. [Documentation Structure](#documentation-structure)
2. [File Formats](#file-formats)
3. [Documentation Standards](#documentation-standards)
4. [Update Procedures](#update-procedures)
5. [Build System](#build-system)
6. [Component Documentation Patterns](#component-documentation-patterns)
7. [Release Notes Format](#release-notes-format)
8. [Validation Checklist](#validation-checklist)

---

## Documentation Structure

### Directory Layout

```
docs/
├── index.md                          # Main entry point (features, compatibility, prerequisites)
├── overview.md                       # Architecture overview (all components)
├── releasenotes.md                   # Release notes (NOT RELEASE_NOTES.md!)
├── troubleshooting.md                # Troubleshooting guide
├── knownlimitations.md               # Known limitations
├── usage.rst                         # Quick start guide (.rst format!)
├── fulldeviceconfig.rst              # Complete CRD reference (.rst format!)
├── gpu-operator-features-support-matrix.md  # Feature support matrix
│
├── installation/                     # Installation guides
│   ├── kubernetes-helm.md            # Helm installation
│   └── openshift-olm.md              # OpenShift OLM installation
│
├── uninstallation/                   # Uninstallation procedures
│   └── uninstallation.md
│
├── upgrades/                         # Upgrade documentation
│   ├── upgrade.md                    # General upgrade procedures
│   └── componentupgrades.md          # Component-specific upgrade notes
│
├── drivers/                          # GPU driver documentation
│   ├── installation.md
│   ├── precompiled-driver.md
│   ├── secure-boot.md
│   ├── upgrading.md
│   └── kernel.md
│
├── device_plugin/                    # Device plugin component
│   ├── device-plugin.md
│   └── resource-allocation.md
│
├── dra/                              # Dynamic Resource Allocation
│   └── dra-driver.md
│
├── dcm/                              # Device Config Manager
│   ├── device-config-manager.md
│   ├── device-config-manager-configmap.md
│   ├── applying-partition-profiles.rst  # (.rst format!)
│   └── systemd_integration.md
│
├── metrics/                          # Metrics and monitoring
│   ├── exporter.md
│   ├── kube-rbac-proxy.md
│   ├── prometheus.md
│   ├── prometheus-openshift.md
│   ├── health.md
│   └── ecc-error-injection.md
│
├── test/                             # Testing documentation
│   ├── test-runner-overview.md
│   ├── auto-unhealthy-device-test.md
│   ├── manual-test.md
│   ├── pre-start-job-test.md
│   ├── logs-export.md
│   ├── agfhc.md
│   └── appendix-test-recipe.md
│
├── kubevirt/                         # KubeVirt integration
│   └── kubevirt.md
│
├── npd/                              # Node Problem Detector
│   └── node-problem-detector.md
│
├── autoremediation/                  # Auto remediation
│   └── auto-remediation.md
│
├── specialized_networks/             # Network configurations
│   ├── airgapped-install.md
│   ├── airgapped-install-openshift.md
│   └── http-proxy.md
│
├── slinky/                           # Slurm on Kubernetes
│   └── slinky-example.md
│
├── contributing/                     # Developer documentation
│   ├── developer-guide.md
│   ├── documentation-build-guide.md
│   └── documentation-standards.md    # ⚠️ MUST FOLLOW THESE STANDARDS
│
├── feature-prds/                     # Feature requirements
│   ├── TEMPLATE.md
│   └── *.md (feature PRDs)
│
├── cicd/                             # CI/CD documentation
│   └── automation.md
│
└── sphinx/                           # Build system configuration
    ├── _toc.yml                      # ⚠️ CRITICAL: Table of contents (auto-generated)
    ├── _toc.yml.in                   # TOC template
    ├── requirements.txt              # Python dependencies
    └── requirements.in               # Source requirements
```

### File Organization Principles

1. **Component-based structure** - Each component has its own directory
2. **Topic-based files** - Multiple files per component, organized by topic
3. **NO README.md pattern** - Use descriptive filenames (e.g., `device-plugin.md` not `README.md`)
4. **Lowercase with hyphens** - File naming: `my-feature-name.md` (not `My_Feature_Name.md`)

---

## File Formats

### Markdown (.md) - Primary Format

**Usage**: Most documentation files (90% of content)

**Features**:
- GitHub-flavored Markdown
- Rendered by Sphinx with myst-parser
- Supports admonitions, tables, code blocks

**Example**:
```markdown
# Feature Name

## Overview

Description of the feature.

## Configuration

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
spec:
  feature:
    enabled: true
` ``

## Verification

```bash
kubectl get deviceconfig -o yaml
` ``
```

### reStructuredText (.rst) - Special Cases

**Usage**: Specific high-complexity documents

**Files using .rst**:
1. **docs/usage.rst** - Quick start guide
2. **docs/fulldeviceconfig.rst** - Complete CRD reference
3. **docs/dcm/applying-partition-profiles.rst** - Partition profiles

**When to use .rst**:
- Complex tables requiring precise formatting
- Extensive cross-referencing needs
- Sphinx-specific directives required

**⚠️ CRITICAL**: When adding new DeviceConfig spec fields, update `fulldeviceconfig.rst`

---

## Documentation Standards

### Source of Truth

**File**: `docs/contributing/documentation-standards.md`

**⚠️ REQUIREMENT**: ALL documentation MUST follow these standards.

### Voice & Tone

| Rule | ❌ Wrong | ✅ Correct |
|------|---------|-----------|
| **Active voice** | "The driver will be installed" | "The operator installs the driver" |
| **Second person** | "The user can configure" | "You can configure" |
| **Present tense** | "The controller will create" | "The controller creates" |
| **Concise** | "It is possible to enable the feature" | "Enable the feature" |
| **Professional** | "Basically, just run this" | "Run the following command" |

### Terminology Standards

| ❌ Incorrect | ✅ Correct | Note |
|-------------|-----------|------|
| GPU operator | AMD GPU Operator | Always include "AMD" |
| K8s | Kubernetes | No abbreviations in formal docs |
| deviceconfig | DeviceConfig | Capital D and C, one word |
| crd | Custom Resource Definition (CRD) | Expand acronym on first use |
| AMDGPU driver | AMD GPU driver | Space between AMD and GPU |
| node-labeler | Node Feature Discovery (NFD) | Use full component name |

### Formatting Standards

#### Headers

```markdown
❌ ## prerequisites and installation
✅ ## Prerequisites and Installation
```

**Rule**: Title Case for all headers

#### Code Blocks

```markdown
❌ ```
   kubectl get deviceconfig
   ```

✅ ```bash
   kubectl get deviceconfig
   ```
```

**Rule**: Always specify language (bash, yaml, go, python, etc.)

#### Lists

```markdown
✅ Consistent punctuation:
- Item one
- Item two
- Item three

✅ Or complete sentences:
- Item one.
- Item two.
- Item three.
```

**Rule**: Consistent punctuation within list (all with or all without periods)

#### Admonitions

```markdown
!!! note
    This is a note with additional context.

!!! warning
    This is a warning about potential issues.

!!! important
    This is critical information.
```

**Types**: note, warning, important, tip, caution

#### Links

```markdown
✅ Internal: [DeviceConfig Reference](fulldeviceconfig.rst)
✅ External: [Kubernetes Documentation](https://kubernetes.io/docs/)
```

**Rule**: Descriptive link text (not "click here")

### File Naming Standards

```
✅ my-feature-name.md
✅ device-plugin.md
✅ kubernetes-helm.md

❌ MyFeatureName.md
❌ my_feature_name.md
❌ dp.md (not descriptive)
```

**Rules**:
- Lowercase only
- Use hyphens (not underscores or spaces)
- Descriptive names (no abbreviations)
- .md extension (unless .rst required)

---

## Update Procedures

### Complete Documentation Update Checklist

Use this checklist for every new feature or component change:

#### Phase 1: Pre-Documentation Analysis

- [ ] Read PRD from `docs/feature-prds/<feature>.md`
- [ ] Read `knowledge/deviceconfig-api-spec.md` (CRD specification)
- [ ] Read `knowledge/component-details.md` (component information)
- [ ] Read `knowledge/architecture-overview.md` (design context)
- [ ] Read `docs/contributing/documentation-standards.md` (style guide)

#### Phase 2: Core Documentation Files

- [ ] **docs/overview.md** - Add component to architecture overview (if new component)
- [ ] **docs/fulldeviceconfig.rst** - Add new spec fields (.rst format!)
- [ ] **docs/gpu-operator-features-support-matrix.md** - Add feature support matrix entry

#### Phase 3: Component-Specific Documentation

- [ ] Create `docs/<component>/` directory (if new component)
- [ ] Create `docs/<component>/<feature>.md` (main feature documentation)
- [ ] Update existing component docs (if enhancing existing component)
- [ ] Follow file naming: lowercase, hyphens, descriptive

#### Phase 4: Installation & Deployment

- [ ] **docs/installation/kubernetes-helm.md** - Update Helm installation (if needed)
- [ ] **docs/installation/openshift-olm.md** - Update OLM installation (if OpenShift-specific)
- [ ] **docs/upgrades/upgrade.md** - Add upgrade considerations (if breaking changes)
- [ ] **docs/upgrades/componentupgrades.md** - Add component upgrade notes (if applicable)

#### Phase 5: Configuration & Usage

- [ ] **config/samples/sample_<feature>.yaml** - Create example DeviceConfig
- [ ] **helm-charts-k8s/values.yaml** - Add configuration values (if applicable)
- [ ] **helm-charts-k8s/README.md** - Document new Helm values (if applicable)
- [ ] **helm-charts-k8s/templates/** - Update relevant templates (if applicable)

#### Phase 6: Testing Documentation

- [ ] **docs/test/test-runner-overview.md** - Add test coverage info (if applicable)
- [ ] Create specific test documentation (if new test patterns introduced)

#### Phase 7: Operational Documentation

- [ ] **docs/troubleshooting.md** - Add troubleshooting entries
- [ ] **docs/knownlimitations.md** - Add limitations (if any)
- [ ] **docs/metrics/** - Add metrics documentation (if feature exposes metrics)

#### Phase 8: Platform-Specific Documentation

- [ ] **docs/specialized_networks/** - Add network config (if airgapped/proxy considerations)
- [ ] **docs/kubevirt/** - Add KubeVirt integration (if VM workload support)

#### Phase 9: Build System Updates (⚠️ CRITICAL)

- [ ] **docs/sphinx/_toc.yml** - Add new files to table of contents
- [ ] Verify Sphinx build succeeds
- [ ] Check for broken links

#### Phase 10: Release Documentation

- [ ] **docs/releasenotes.md** - Add comprehensive release notes entry
- [ ] Include all required sections (see Release Notes Format below)

#### Phase 11: Validation

- [ ] Verify documentation follows standards in `docs/contributing/documentation-standards.md`
- [ ] Check terminology compliance
- [ ] Verify voice/tone (active, second person, present tense)
- [ ] Verify code blocks have language specified
- [ ] Run documentation build
- [ ] Review generated HTML for formatting

### Component-Specific Update Patterns

#### For New DeviceConfig Spec Fields

**Required Updates**:
1. `docs/fulldeviceconfig.rst` - Add field to complete reference (.rst format!)
2. `config/samples/sample_<feature>.yaml` - Show field in example
3. Component-specific doc - Explain field usage and behavior
4. `docs/gpu-operator-features-support-matrix.md` - Add support info

#### For New Component

**Required Updates**:
1. `docs/overview.md` - Add to architecture diagram/description
2. Create `docs/<component>/` directory
3. Create `docs/<component>/<component>.md` - Component overview
4. Update `docs/sphinx/_toc.yml` - Add new section
5. Update `docs/releasenotes.md` - New component announcement

#### For Installation Changes

**Required Updates**:
1. `docs/installation/kubernetes-helm.md` - Helm procedures
2. `docs/installation/openshift-olm.md` - OLM procedures (if applicable)
3. `helm-charts-k8s/values.yaml` - Configuration values
4. `helm-charts-k8s/README.md` - Document values

#### For Metrics Changes

**Required Updates**:
1. `docs/metrics/exporter.md` - Metrics documentation
2. `docs/metrics/prometheus.md` - Prometheus integration (if applicable)
3. Component doc - Explain what metrics mean
4. Examples showing metrics queries

---

## Build System

### Sphinx Configuration

**Build System**: Sphinx with AMD's custom rocm_docs_theme

**Configuration File**: `docs/conf.py`

**Key Settings**:
- Project: "AMD GPU Operator"
- Theme: rocm_docs_theme
- Extensions: myst-parser (Markdown support), sphinx-design, sphinx-copybutton
- Output: HTML

### Table of Contents (TOC)

**⚠️ CRITICAL**: The TOC controls what appears in generated documentation.

**File**: `docs/sphinx/_toc.yml` (auto-generated from `_toc.yml.in`)

**Structure**:
```yaml
root: index
entries:
  - file: overview
    title: Overview
  
  - caption: Usage
    entries:
      - file: usage
        title: Quick Start
      # Add new usage docs here
  
  - caption: Installation
    entries:
      - file: installation/kubernetes-helm
        title: Kubernetes with Helm
      - file: installation/openshift-olm
        title: OpenShift with OLM
      # Add new installation docs here
  
  - caption: Component Name
    entries:
      - file: component/feature
        title: Feature Name
      # Add new component docs here
```

**Adding New Files**:
1. Determine appropriate section (caption)
2. Add entry under that section
3. Use file path WITHOUT extension: `component/feature` not `component/feature.md`
4. Provide descriptive title (displayed in navigation)

**Example**:
```yaml
- caption: Device Config Manager
  entries:
    - file: dcm/device-config-manager
      title: Overview
    - file: dcm/applying-partition-profiles
      title: Partition Profiles
    - file: dcm/systemd_integration
      title: SystemD Integration
```

### Build Commands

**Local build**:
```bash
cd docs
python3 -m sphinx -T -E -b html -d _build/doctrees -D language=en . _build/html
```

**Auto-rebuild** (development):
```bash
cd docs
sphinx-autobuild . _build/html --port 8000
```

**Verify build**: Check for errors/warnings in output

### Build Verification Checklist

- [ ] No Sphinx warnings or errors
- [ ] All new files appear in TOC
- [ ] Internal links work (no 404s)
- [ ] Code blocks render correctly
- [ ] Admonitions render correctly
- [ ] Tables render correctly

---

## Component Documentation Patterns

### Pattern 1: Component Overview Document

**File**: `docs/<component>/<component>.md`

**Template**:
```markdown
# Component Name

## Overview

Brief description of what the component does and its role in the GPU Operator.

## Architecture

How the component fits into the overall system.

## When to Use

Use cases and scenarios where this component is needed.

## Prerequisites

- Kubernetes vX.Y+
- GPU Operator vZ.W+
- Other requirements

## Configuration

### Basic Configuration

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: example-config
spec:
  component:
    enabled: true
    config: "value"
```

### Advanced Configuration

More complex examples with explanations.

## Verification

How to verify the component is working correctly.

```bash
kubectl get deviceconfig example-config -o jsonpath='{.status.componentStatus}'
```

## Troubleshooting

Common issues and solutions.

## Related Documentation

- [Related Doc](../other/doc.md)
- [Knowledge Base](../../knowledge/component-details.md)
```

### Pattern 2: Feature Documentation

**File**: `docs/<component>/<feature>.md`

**Template**:
```markdown
# Feature Name

## Overview

What the feature does.

## Use Cases

When and why to use this feature.

## Prerequisites

Requirements for using this feature.

## Configuration

### DeviceConfig Specification

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: feature-example
spec:
  component:
    feature:
      enabled: true
      option1: "value1"
      option2: "value2"
```

### Field Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| enabled | boolean | No | false | Enable the feature |
| option1 | string | No | "" | Description |

## Behavior

Detailed explanation of how the feature works.

## Status Reporting

How to check feature status.

## Examples

### Example 1: Basic Usage

Description and YAML.

### Example 2: Advanced Usage

Description and YAML.

## Troubleshooting

Feature-specific troubleshooting.

## Known Limitations

Any limitations or constraints.
```

### Pattern 3: Installation Documentation

**File**: `docs/installation/<platform>.md`

**Structure**:
1. Prerequisites
2. Installation steps (numbered)
3. Verification steps
4. Configuration options
5. Troubleshooting

### Pattern 4: Testing Documentation

**File**: `docs/test/<test-type>.md`

**Structure**:
1. Test overview
2. When to run
3. Prerequisites
4. Running the test
5. Expected results
6. Interpreting results
7. Troubleshooting failures

---

## Release Notes Format

**File**: `docs/releasenotes.md` (NOT `RELEASE_NOTES.md`)

### Template

```markdown
## GPU Operator vX.Y.Z - YYYY-MM-DD

### New Features

- **Feature Name**: Brief description of the feature
  - **Configuration**: `spec.component.feature`
  - **Status**: `status.componentFeatureStatus`
  - **Platform Support**: Kubernetes vX.Y+, OpenShift vZ.W+
  - **Documentation**: [Feature Guide](component/feature.md)
  - **Use Case**: When you need to [specific use case]

### Enhancements

- **Component Name**: Description of enhancement
  - Impact: What improved
  - Configuration: If config changes needed

### Bug Fixes

- **Issue #XXX**: Description of bug
  - **Root Cause**: What was wrong
  - **Fix**: What was changed
  - **Impact**: Who is affected

### Known Issues

- **Issue Description**: What's not working as expected
  - **Workaround**: How to work around it (if available)
  - **Target Fix**: When fix is planned

### Upgrade Notes

**⚠️ Breaking Changes** (if any):
- Description of breaking change
- Migration steps required
- Impact on existing deployments

**Deprecations**:
- Feature/field being deprecated
- Replacement recommendation
- Removal timeline

### Compatibility

- Kubernetes: vX.Y+
- OpenShift: vZ.W+
- AMD GPU drivers: vA.B+
- Helm chart version: vC.D

### Documentation Updates

- [New Documentation](path/to/doc.md)
- [Updated Documentation](path/to/doc.md)
```

### Release Notes Best Practices

1. **User-focused language** - Explain benefits, not just changes
2. **Complete information** - Include config paths, status fields, docs links
3. **Clear upgrade path** - Document breaking changes prominently
4. **Known issues upfront** - Don't hide limitations
5. **Link to documentation** - Every feature should link to its docs

---

## Validation Checklist

### Standards Compliance

- [ ] **Voice**: Active voice, second person, present tense
- [ ] **Terminology**: "AMD GPU Operator", "Kubernetes", "DeviceConfig" (exact spelling)
- [ ] **Formatting**: Title case headers, language in code blocks
- [ ] **File naming**: Lowercase, hyphens, descriptive
- [ ] **Links**: Descriptive link text, all links functional

### Content Completeness

- [ ] All PRD documentation requirements addressed
- [ ] Examples are valid YAML (copy-paste ready)
- [ ] All new spec fields documented in `fulldeviceconfig.rst`
- [ ] All sections have content (no TODOs left)
- [ ] Feature support matrix updated (if applicable)

### Build Verification

- [ ] Updated `docs/sphinx/_toc.yml` with new files
- [ ] Sphinx build succeeds (no errors)
- [ ] Generated HTML renders correctly
- [ ] No broken links (internal or external)
- [ ] Admonitions render properly
- [ ] Code blocks have correct syntax highlighting

### Cross-References

- [ ] Links to related documentation
- [ ] Links to configuration examples
- [ ] Links to knowledge base (for internal context)
- [ ] Links to troubleshooting (where applicable)

### Release Readiness

- [ ] Release notes entry complete
- [ ] Known limitations documented
- [ ] Troubleshooting entries added
- [ ] Migration notes (if breaking changes)

---

## Common Mistakes to Avoid

### ❌ Mistake 1: Wrong File Location

```
❌ docs/myfeature/README.md
✅ docs/component/my-feature.md
```

### ❌ Mistake 2: Forgetting TOC Update

Creating `docs/component/new-feature.md` but not adding to `_toc.yml` → Won't appear in generated docs!

### ❌ Mistake 3: Wrong File Format for CRD Reference

```
❌ docs/fulldeviceconfig.md
✅ docs/fulldeviceconfig.rst
```

### ❌ Mistake 4: Wrong Terminology

```
❌ "The K8s GPU operator will install..."
✅ "The AMD GPU Operator installs..."
```

### ❌ Mistake 5: Missing Language in Code Blocks

```markdown
❌ ```
   kubectl apply -f config.yaml
   ```

✅ ```bash
   kubectl apply -f config.yaml
   ```
```

### ❌ Mistake 6: Generic Link Text

```markdown
❌ For more information, click [here](feature.md).
✅ See the [Feature Configuration Guide](feature.md) for details.
```

### ❌ Mistake 7: Inconsistent List Punctuation

```markdown
❌ - Item one
   - Item two.
   - Item three

✅ - Item one
   - Item two
   - Item three
```

---

## Summary

### Critical Files That Must Be Updated

1. **docs/sphinx/_toc.yml** - Add new files or they won't appear
2. **docs/fulldeviceconfig.rst** - New spec fields MUST be documented here
3. **docs/releasenotes.md** - Every release includes release notes
4. **docs/contributing/documentation-standards.md** - Follow ALL standards here

### Documentation Update Flow

```
PRD → Read Knowledge Base → Read Standards → Write Docs → Update TOC → Build → Validate → Release Notes
```

### Quality Gates

1. **Standards compliance** - Voice, tone, terminology, formatting
2. **Build success** - Sphinx build with no errors
3. **Link validation** - All links work
4. **Content completeness** - All requirements addressed
5. **TOC presence** - All new files in TOC

### Resources

- **Standards**: `docs/contributing/documentation-standards.md`
- **Build Guide**: `docs/contributing/documentation-build-guide.md`
- **Developer Guide**: `docs/contributing/developer-guide.md`
- **Knowledge Base**: `knowledge/` directory
- **Examples**: Existing docs for similar components
