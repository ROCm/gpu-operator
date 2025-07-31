# Documentation Standards

## Voice and Tone

### Writing Style

- Use active voice
- Write in second person ("you") for instructions
- Maintain professional, technical tone
- Be concise and direct
- Use present tense

Examples:

```diff
- The configuration file will be created by the operator
+ The operator creates the configuration file

- One should ensure that all prerequisites are met
+ Ensure all prerequisites are met
```

### Terminology Standards

#### Product Names

- "AMD GPU Operator" (not "GPU operator" or "gpu-operator")
- "Kubernetes" (not "kubernetes" or "K8s")
- "OpenShift" (not "Openshift" or "openshift")
- "AMD ROCm™" (not "ROCM" or "rocm")

#### Technical Terms

| Term | Usage Notes |
|------|-------------|
| AMD GPU driver | Standard term for the driver. Don't use "AMDGPU driver" or "GPU driver" alone |
| worker node | Standard term for cluster nodes. Don't use "worker" or "node" alone |
| DeviceConfig | One word, capital 'D' and 'C' when referring to the resource |
| container image | Use instead of just "image" |
| pod | Lowercase unless starting a sentence |
| namespace | Lowercase unless starting a sentence |

#### Acronym Usage

Always expand acronyms on first use in each document:

- NFD (Node Feature Discovery)
- KMM (Kernel Module Management)
- CRD (Custom Resource Definition)
- CR (Custom Resource)

## Formatting Standards

### Headers

- Use title case for all headers
- Add blank line before and after headers

```markdown
# Main Title

## Section Title

### Subsection Title
```

### Code Blocks

- Always specify language for syntax highlighting
- Use inline code format (`code`) for:
  - Command names
  - File names
  - Variable names
  - Resource names
- Use block code format (```) for:
  - Command examples
  - YAML/JSON examples
  - Configuration files
  - Output examples

Examples:

````markdown
Install using `helm`:

```bash
helm install amd-gpu-operator rocm/gpu-operator-helm
```

Create a configuration:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: example
```
````

### Lists

- Maintain consistent indentation (2 spaces)
- End each list item with punctuation
- Add blank line between list items if they contain multiple sentences or code blocks

### Admonitions

Use consistent formatting for notes, warnings, and tips:

```markdown
```{note}
Important supplementary information.
```

```{warning}
Critical information about potential problems.
```

```{tip}
Helpful advice for better usage.
```

```text

### Tables

- Use tables for structured information
- Include header row
- Align columns consistently
- Add blank lines before and after tables

Example:

```markdown
| Parameter | Description | Default |
|-----------|-------------|---------|
| `image` | Container image path | `rocm/gpu-operator:latest` |
| `version` | Driver version | `6.2.0` |
```

## Document Structure

### Standard Sections

Every document should include these sections in order:

1. Title (H1)
2. Brief overview/introduction
3. Prerequisites (if applicable)
4. Main content
5. Verification steps (if applicable)
6. Troubleshooting (if applicable)

### Example Template

```markdown
# Feature Title

Brief description of the feature or component.

## Prerequisites

- Required components
- Required permissions
- Required resources

## Overview

Detailed description of the feature.

## Configuration

Configuration steps and examples.

## Verification

Steps to verify successful implementation.

## Troubleshooting

Common issues and solutions.
```

## File Naming

- Use lowercase
- Use hyphens for spaces
- Be descriptive but concise
- Include category prefix when applicable

Examples:

- `install-kubernetes.md`
- `upgrade-operator.md`

## Links and References

- Use relative links for internal documentation
- Use absolute links for external references
- Include link text that makes sense out of context

Examples:

```markdown
[Installation Guide](../install/kubernetes)
[Kubernetes Documentation](https://kubernetes.io/docs)
```
