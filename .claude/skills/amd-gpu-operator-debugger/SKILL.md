---
name: amd-gpu-operator-debugger
description: >
  Debug AMD GPU Operator failures on a Kubernetes cluster. Systematically diagnoses
  all known failure modes: workload scheduling failures, GPU resources not available,
  controller or operand pods stuck Pending (taints, missing tolerations), no operands
  deployed (DeviceConfig selector mismatch, NFD label issues, vGPU vs physical GPU
  mismatch, multiple conflicting DeviceConfigs), ConfigMap reference errors, KMM-managed
  driver failures (builder pod failed, DNS resolution, insecure registry, missing
  registry secrets), init container failures (amdgpu driver not loaded), kernel-level
  GPU crashes visible only in dmesg, metrics not appearing in Prometheus, DRA driver
  issues (ResourceSlices not published), Helm lifecycle issues (install/upgrade/uninstall
  hangs), DeviceConfig stuck Terminating. Supports two modes: (1) direct kubectl-based
  triage using kubeconfig, or (2) launching a privileged debug container on the target
  node for kernel-level inspection. Triggers on: "debug gpu operator", "amd gpu operator
  not working", "gpu pods pending", "workload cannot schedule", "cannot schedule gpu
  workload", "no gpu resources", "amd.com/gpu 0", "device plugin not working", "dra
  driver not working", "resourceslices missing", "kmm driver failed", "init pod stuck",
  "gpu metrics missing", "metrics not showing in prometheus", "gpu metrics not available",
  "helm install failed", "helm upgrade failed", or any mention of AMD GPU Operator
  failures on Kubernetes.
---

# AMD GPU Operator Debugger

Systematically diagnoses AMD GPU Operator failures on Kubernetes clusters.

**References:**

- `architecture.md` - Component model, pod taxonomy, driver modes
- `deviceconfig_fields.md` - DeviceConfig field reference
- `failure_patterns.md` - Symptom → root cause lookup table
- `diagnostic_commands.md` - All diagnostic commands
- `remediation_commands.md` - All fix/patch commands
- `dra_npd_anr.md` - DRA/NPD/ANR diagnostics
- `kubectl_syntax_guide.md` - Safe kubectl patterns
- `techsupport_bundle.md` - Support bundle collection

## CRITICAL: Command Safety Requirements

**IMPORTANT**: When generating kubectl commands during diagnosis, ALWAYS follow the safe patterns
documented in `references/kubectl_syntax_guide.md`:

- Use `2>/dev/null` to suppress kubectl errors
- Assign command output to variables before using them
- Check if variables are non-empty with `if [ -n "$VAR" ]` before using
- Provide helpful error messages when resources are not found

See `references/kubectl_syntax_guide.md` for detailed examples and anti-patterns to avoid.

---

## Architecture Overview

**Three operators work together:** AMD GPU Operator (manages DeviceConfig, creates DaemonSets),
KMM Operator (builds and loads amdgpu driver), NFD (labels nodes with GPU hardware).

**Key difference from NVIDIA:** AMD GPU Operator delegates driver management to KMM.
NO Driver DaemonSet, only `kmm-worker` pods is created by KMM after build, not by GPU Operator directly.

**Init container dependency:** All operand pods (device plugin, metrics exporter, etc.) wait
for `amdgpu` kernel module to load before starting. If driver never loads, pods stay at `Init 0/1`.

See `references/architecture.md` for complete details, pod taxonomy, and driver modes.

---

## Inputs

1. **Execution mode:** `local-kubectl`, `local-container`, or `remote-container`

2. If `remote-container`:
   - **SSH host** (user@hostname)
   - **SSH password** (if needed - will prompt interactively)
   - **Remote kubeconfig path** (on remote machine)

3. For all modes:
   - **KUBECONFIG** (default: ~/.kube/config)
   - **Namespace** (default: kube-amd-gpu)
   - **Target node** (optional)

**Launch:**

- Local: `scripts/launch_agent_container.sh <kubeconfig> <ns> local`
- Remote: `ssh <host> 'docker run -d ... bitnami/kubectl:latest sleep 3600'` (may prompt for password)
- Remote kubectl: `ssh <host> docker exec <container> kubectl <args>`

**Variables:** `export KUBECONFIG=~/.kube/config NS=kube-amd-gpu NODE=<node>`

---

## Triage Runbook

Work through phases in order. Stop at the phase where the root cause is found.

---

### Phase 1 — Cluster Snapshot

**Check:** Pods, DaemonSets, DeviceConfig status, node labels, GPU allocatable

**Decision:**

- All healthy → Done
- Controller Pending → Phase 2
- DaemonSets DESIRED=0 → Phase 4
- Pods Pending → Phase 5
- Pods Init 0/1 → Phase 6
- Running but no GPUs → Phase 8

**Commands:** `diagnostic_commands.md` Phase 1

---

### Phase 2 — Controller Manager Health

**Symptom:** Controller pod Pending
**Cause:** Node taint without matching toleration, or insufficient resources
**Fix:** Add toleration to controller deployment or remove node taint

**Commands:** `diagnostic_commands.md` Phase 2

---

### Phase 3 — Operand DaemonSet Check

**Decision:**

- DESIRED=0 → Phase 4 (selector/NFD)
- DESIRED>0, pods Pending → Phase 5
- DESIRED>0, pods Init 0/1 → Phase 6

---

### Phase 4 — Selector / NFD Label Check

**Symptom:** DaemonSets DESIRED=0

**Patterns:** A) Selector mismatch, B) vGPU vs physical, C) New GPU model, D) Overlapping selectors, E) Per-component mismatch

**Fix:** See `remediation_commands.md` Phase 4

---

### Phase 5 — Operand Pods Pending

**Patterns:**

- **Taint:** Event `untolerated taint` → Add tolerations to `spec.devicePlugin.tolerations`, `spec.metricsExporter.tolerations`, `spec.driver.tolerations`
- **ConfigMap:** Event `configmap "X" not found` → Create ConfigMap or fix `spec.<component>.config.name`

**Commands:** `diagnostic_commands.md` Phase 5

---

### Phase 6 — Init Container Failures (amdgpu Driver Not Loaded)

**Symptom:** Pods at Init 0/1

**Root cause:** `amdgpu` kernel module not loaded on node

**Sub-phases:**

- **6a:** `spec.driver.enable: true` → Check KMM Module, builder pod, kmm-worker pods
  - Builder Failed → Phase 7
  - kmm-worker CrashLoopBackOff + "Device or resource busy" → Process using GPU (check with Phase 9 debug container)
- **6b:** `spec.driver.enable: false` → Check if inbox driver is loaded on node (Phase 9)

**Commands:** `diagnostic_commands.md` Phase 6

---

### Phase 7 — KMM Driver Build / Pull Failures

Fired when: `spec.driver.enable: true` but no kmm-worker DaemonSet pods exist, or
builder pod exists in Failed state.

```bash
# Builder pod status and logs
kubectl --kubeconfig=$KUBECONFIG get pods -n $NS | grep build
kubectl --kubeconfig=$KUBECONFIG logs -n $NS <builder-pod-name> --tail=80

# KMM Module status and conditions
kubectl --kubeconfig=$KUBECONFIG get module -n $NS -o yaml

# KMM controller logs
KMM_POD=$(kubectl --kubeconfig=$KUBECONFIG get pods -n $NS | grep kmm-operator | \
  awk '{print $1}' | head -1)
if [ -n "$KMM_POD" ]; then
  kubectl --kubeconfig=$KUBECONFIG logs -n $NS $KMM_POD --tail=50
else
  echo "No KMM operator pod found"
fi

# Events in operator namespace
kubectl --kubeconfig=$KUBECONFIG get events -n $NS --sort-by='.lastTimestamp' | tail -30
```

**Pattern A:** Kernel incompatibility → Check kernel version, use compatible driver version

**Pattern B:** DNS resolution failure → Test DNS with busybox pod, fix CoreDNS or network policy

**Pattern C:** HTTP registry → Set `spec.driver.imageRegistryTLS.insecure: true`

**Pattern D:** Secret name typo → Fix `spec.driver.imageRegistrySecret.name`

**Pattern E:** Secret missing → Create docker-registry secret

See `references/diagnostic_commands.md` Phase 7 for full commands.

---

### Phase 8 — Zero GPU Resources (Kernel Crash)

**Symptom:** Device plugin Running, but `amd.com/gpu: 0`

**Root cause:** `amdgpu` module crashed after loading (hardware/firmware/PCIe fault)

**Must check dmesg** for kernel messages → Use Phase 9 debug container

**Key dmesg signatures:** VM_L2_PROTECTION_FAULT, GPU reset failed, probe failed -110

**Commands:** `diagnostic_commands.md` Phase 8

---

### Phase 9 — Debug Container (Kernel-Level Checks)

**Use when:** Need dmesg, lsmod, or device file inspection (requires debug mode consent)

**Launch:** `scripts/launch_debug_container.sh $NODE $NS`

**Checks inside pod (use chroot /host):**

- Driver: `chroot /host lsmod | grep amdgpu`
- Devices: `chroot /host ls /dev/kfd /dev/dri/`
- dmesg: `chroot /host dmesg | grep -E "amdgpu|GPU|FAULT" | tail -50`
- Firmware: `chroot /host ls /lib/firmware/amdgpu/`
- Processes: `chroot /host lsof | grep -E "/dev/kfd|/dev/dri"`

**Cleanup:** `kubectl delete pod gpu-debug-$NODE -n $NS`

**Manual YAML:** See `diagnostic_commands.md` Debug Container section

---

### Phase 10 — Operational Issues (Uninstall / Finalizers)

#### Helm uninstall hangs / times out

Symptom: `helm uninstall` produces `timed out waiting for the condition`.

Cause: the pre-delete hook Job uses the operator manager image from `helm install`.
If that image can't be pulled (bad tag, missing registry credentials, or wrong image was set during install),
the hook Job stays in `ImagePullBackOff` forever.

```bash
# Check the hook Job
kubectl --kubeconfig=$KUBECONFIG get jobs -n $NS
kubectl --kubeconfig=$KUBECONFIG describe job -n $NS <hook-job-name>
```

**Common scenario:** User accidentally set wrong controller manager image during `helm install`,
causing the operator to malfunction. Later attempts to uninstall fail because the hook Job can't pull
the same wrong image.

Fix — skip the hook:

```bash
helm uninstall -n $NS amd-gpu-operator --no-hooks
```

Then manually clean up DeviceConfig resources (see next section) and orphaned
DaemonSets/KMM Modules:

```bash
kubectl --kubeconfig=$KUBECONFIG get daemonsets -n $NS
kubectl --kubeconfig=$KUBECONFIG get modules -n $NS
```

#### DeviceConfig stuck Terminating

Symptom: `kubectl delete deviceconfigs --all -A` hangs indefinitely.

Cause: the operator adds finalizer `amd.node.kubernetes.io/deviceconfig-finalizer`.
If the operator is not running, nothing removes it.

```bash
# Remove finalizer from a single DeviceConfig
kubectl --kubeconfig=$KUBECONFIG patch deviceconfig <name> -n $NS --type=json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'

# Remove from all DeviceConfigs at once
kubectl --kubeconfig=$KUBECONFIG get deviceconfigs.amd.com -A -o name | \
  xargs -I{} kubectl --kubeconfig=$KUBECONFIG patch {} -n $NS --type=json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```

**Warning:** removing the finalizer skips the operator's cleanup logic. Verify no
orphaned resources remain afterwards:

```bash
kubectl --kubeconfig=$KUBECONFIG get daemonsets -n $NS
kubectl --kubeconfig=$KUBECONFIG get modules -n $NS
```

#### Complete cleanup after bad install (wrong controller image)

If the operator was installed with a wrong controller manager image and needs to be completely
removed and reinstalled, follow this complete cleanup procedure:

```bash
# Step 1: Uninstall with --no-hooks (hook will fail due to bad image)
helm uninstall -n $NS amd-gpu-operator --no-hooks

# Step 2: Remove finalizers from all DeviceConfigs
kubectl --kubeconfig=$KUBECONFIG get deviceconfigs.amd.com -A -o name | \
  xargs -I{} kubectl --kubeconfig=$KUBECONFIG patch {} --type=json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'

# Step 3: Delete all DeviceConfigs
kubectl --kubeconfig=$KUBECONFIG delete deviceconfigs.amd.com --all -A

# Step 4: Delete GPU Operator CRDs manually
kubectl --kubeconfig=$KUBECONFIG delete crd deviceconfigs.amd.com
kubectl --kubeconfig=$KUBECONFIG delete crd $(kubectl get crd | grep amd.com | awk '{print $1}')

# Step 5: Verify cleanup
kubectl --kubeconfig=$KUBECONFIG get all -n $NS
kubectl --kubeconfig=$KUBECONFIG get crd | grep amd.com

# Step 6: Reinstall with correct image

```bash
helm install amd-gpu-operator rocm/gpu-operator-charts -n $NS --create-namespace \
  --set controller.manager.image.repository=<correct-image> \
  --set controller.manager.image.tag=<correct-tag>
```

**Reference:** [AMD GPU Operator Troubleshooting - DeviceConfig stuck in Terminating state](https://instinct.docs.amd.com/projects/gpu-operator/en/main/troubleshooting.html#deviceconfig-stuck-in-terminating-state)

---

### Phase 11 — Helm Install Failures

**Check:** kubectl access, user is cluster-admin, cert-manager running, webhook CA bundle populated

**Common causes:** No admin permissions, cert-manager not installed, webhook CA bundle empty

**Commands:** `diagnostic_commands.md` Phase 11

---

### Phase 12 — Helm Upgrade Failures

**Check:** Pre-upgrade hook jobs, CRD upgrade status

**Common causes:** Hook ImagePullBackOff, hook validation failed, CRD webhook down

**Fix:** Delete failed hooks, retry with `--force`, or rollback

**Commands:** `diagnostic_commands.md` Phase 12

---

### Phase 13 — Techsupport Bundle

**Use when:** Root cause unclear after manual triage

See `references/techsupport_bundle.md`

---

## Final Answer Template

Report findings using this format:

**Node:** `node-name` | **Namespace:** `kube-amd-gpu` | **Driver mode:** `true/false`

**Symptom:** Brief description of observed issue

**Root Cause:** Specific cause identified

**Evidence:** Key command outputs that prove the cause

**Remediation:** Fix commands

**Verification:** Command to confirm fix worked
