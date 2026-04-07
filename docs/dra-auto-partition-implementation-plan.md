# Implementation Plan: Auto-Partitioning AMD GPUs with DRA Driver

## Context

### Problem Statement
The GPU operator currently uses Device Config Manager (DCM) with manual node labeling to partition AMD Instinct GPUs. Users must manually label nodes to trigger partitioning (e.g., SPX/NPS1 → CPX/NPS1), which requires:
1. Admin manually adding node label `dcm.amd.com/gpu-config-profile=cpx_nps1`
2. DCM detecting label and running partition commands
3. Driver reload terminating all GPU workloads
4. Device plugin re-discovering partitioned GPUs

This manual workflow doesn't support **pod-driven automatic partitioning** where a pod requesting `cpx/nps1` can automatically trigger GPU partition if only `spx/nps1` GPUs are available.

### Desired Behavior
When a pod requests a specific partition profile (e.g., `cpx/nps1`) via ResourceClaim:
1. DRA driver checks available GPUs via ResourceSlices
2. If exact match exists → allocate immediately
3. If convertible GPU exists (e.g., SPX that can become CPX) AND entire node is **idle**:
   - Automatically partition ALL GPUs on the node
   - Update ResourceSlices with new partition topology
   - Allocate to requesting pod
4. If no idle node → pod waits in pending state

### Key Clarifications from User
1. **AMD DRA Driver Ready**: Available at https://github.com/ROCm/k8s-gpu-dra-driver (alpha, K8s 1.34+)
2. **Partition Only Idle Nodes**: No pod draining needed - only partition nodes with zero GPU allocations
3. **ResourceSlice Updates**: Yes, after partition, ResourceSlices must be updated to reflect new topology
4. **K8s Version**: 1.34+ (DRA GA with resource.k8s.io/v1 API)
5. **Un-partitioning**: Return to default state when partitions no longer needed

### Why DRA Over Device Plugin
**DRA Advantages**:
- ✅ Attribute-aware resource management (model, partition, memory, PCIe topology)
- ✅ ResourceClaims declaratively match GPU requirements via **structured parameters**
- ✅ Scheduler topology awareness for distributed workloads
- ✅ Designed for dynamic resource reconfiguration
- ✅ Pod-driven allocation without manual intervention
- ✅ **Driver called for each candidate node** - enables auto-partitioning during scheduling

**Device Plugin Limitations**:
- ❌ Static resource model (count-based, no attributes)
- ❌ Blind scheduling (no topology awareness)
- ❌ Requires manual node labeling for partitioning
- ❌ Cannot react to pod requirements dynamically
- ❌ No opportunity to partition during scheduling (device plugin reports fixed capacity)

### AMD GPU Partitioning Capabilities (MI300X)
**Partition Modes**:
- **Compute**: SPX (1 GPU), DPX (2 GPUs), QPX (4 GPUs), CPX (8 GPUs)
- **Memory**: NPS1 (unified), NPS2 (2 NUMA domains), NPS4 (4 NUMA domains)

**Runtime Partitioning**:
```bash
# Compute partition: PER-GPU operation (NO driver reload needed)
amd-smi set --gpu <id> --compute-partition CPX
# No driver reload required! Takes effect immediately.

# Memory partition: WHOLE NODE operation (REQUIRES driver reload)
amd-smi set --gpu all --memory-partition NPS4
amd-smi reset -r  # Driver reload ONLY needed for NPS changes
```

**Key Constraints**:
- ✅ No system reboot required
- **⚠️ CRITICAL: Driver reload ONLY needed for NPS (memory partition) changes**
  - Compute partition changes (SPX/CPX/DPX/QPX) take effect immediately, NO reload
  - NPS changes require driver reload (terminates all GPU workloads)
- ❌ GPU reverts to SPX on driver reload/reboot (not persistent)
- ⚠️ NPS4 only compatible with CPX (not SPX)
- **⚠️ Memory partition (NPS) is WHOLE NODE operation**
  - Changing NPS affects ALL GPUs on the node
  - Cannot have mixed NPS modes (e.g., GPU0=NPS1, GPU1=NPS4)
  - Must coordinate NPS changes across all GPUs on node
  - **Entire node must be idle for NPS changes** (driver reload disrupts all)
- **✅ Compute partition is PER-GPU operation**
  - Can partition individual GPUs independently
  - No driver reload, no disruption to other GPUs
  - Can have mixed compute modes (e.g., GPU0=CPX, GPU1=DPX) on same node
  - **Only the specific GPU needs to be idle for compute partition changes**
- **⚠️ Un-partitioning flexibility:**
  - Compute-only: Can un-partition single GPU (CPX→SPX) without affecting others
  - Memory partition: Must coordinate across entire node (requires driver reload)

**Sources**:
- [AMD MI300X GPU Partitioning Overview](https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/mi300x/overview.html)
- [Quick Start Guide to Partitioning MI300X GPUs](https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/mi300x/quick-start-guide.html)

---

## Critical Architectural Decision: Structured Parameters vs CEL

### The Problem with CEL-Based Matching

**Initial Incorrect Approach** (using CEL):
```yaml
spec:
  devices:
    requests:
      - name: gpu
        deviceClassName: gpu.amd.com
        selectors:
          - cel:
              expression: device.attributes["partitionProfile"] == "CPX_NPS1"
```

**Why this FAILS for auto-partitioning:**
1. Scheduler evaluates CEL expression **directly against ResourceSlices**
2. If no `CPX_NPS1` device exists in ResourceSlice → CEL fails → **node filtered out**
3. Scheduler **never calls** the DRA driver for this node
4. Driver has **no opportunity** to partition the GPU
5. Result: Pod stays pending forever, even though GPU could be partitioned

**Flow diagram**:
```
ResourceSlice has: SPX_NPS1
Pod requests: CPX_NPS1 (via CEL)
→ Scheduler: CEL expression fails (SPX_NPS1 ≠ CPX_NPS1)
→ Node REJECTED
→ DRA driver NOT CALLED
→ Auto-partition IMPOSSIBLE ❌
```

### The Solution: Structured Parameters

**Correct Approach** (using structured parameters):
```yaml
spec:
  devices:
    requests:
      - name: gpu
        deviceClassName: gpu.amd.com
    config:  # Structured parameters, not CEL!
      - requests:
          - partitionProfile: CPX_NPS1
            model: MI300X
```

**Why this WORKS for auto-partitioning:**
1. Scheduler reads structured parameters (does **not** evaluate against ResourceSlice)
2. Scheduler calls DRA driver's `NodeListAndPrepare()` for **each candidate node**
3. Driver checks: "Can I satisfy partitionProfile=CPX_NPS1?"
   - Exact match exists? → Return immediately
   - Can partition SPX to CPX? → **Partition now**, then return
4. Driver partitions **synchronously** during scheduler's call
5. Driver updates ResourceSlices **before** returning
6. Scheduler receives updated device list

**Flow diagram**:
```
ResourceSlice has: SPX_NPS1 with supportedPartitionProfiles=["SPX_NPS1", "CPX_NPS1", ...]
Pod requests: partitionProfile=CPX_NPS1 (structured param)
→ Scheduler: Call NodeListAndPrepare() for this node
→ DRA driver: "CPX_NPS1 in supportedPartitionProfiles? YES"
→ DRA driver: "GPU idle? YES"
→ DRA driver: Partition SPX → CPX (synchronously)
→ DRA driver: Update ResourceSlice
→ DRA driver: Return CPX devices to scheduler
→ Auto-partition SUCCESS ✅
```

### Key Differences

| Aspect | CEL-Based | Structured Parameters |
|--------|-----------|----------------------|
| Scheduler behavior | Filters nodes via CEL | Calls driver for all nodes |
| Driver involvement | After node selected | During node selection |
| Can auto-partition? | ❌ NO | ✅ YES |
| When partition happens | N/A | In `NodeListAndPrepare()` |
| ResourceSlice requirements | Must have exact match | Must list supported profiles |

### Implementation Impact

**ResourceSlice must publish capabilities**:
```yaml
devices:
  - name: gpu-0
    basic:
      attributes:
        currentPartitionProfile: {string: "SPX_NPS1"}  # What it IS now
        supportedPartitionProfiles: {stringSlice: ["SPX_NPS1", "CPX_NPS1", "DPX_NPS1", "QPX_NPS1"]}  # What it CAN become
        isIdle: {bool: true}  # Can it be partitioned right now?
```

**DRA driver logic in NodeListAndPrepare()**:
```go
func (d *DRADriver) NodeListAndPrepare(...) {
    requested := parseStructuredParams(claim) // "CPX_NPS1"

    // Check if requested in supportedPartitionProfiles
    if canSatisfy(resourceSlice, requested) {
        if exactMatchExists() {
            return devices  // Already have it
        }

        if gpuIdle() {
            partition(gpu, requested)  // Do it now!
            updateResourceSlice()
            return newDevices
        }
    }

    return error("cannot satisfy")
}
```

---

## Recommended Approach: **DRA with Auto-Partitioning Extension**

### Critical Design Considerations

#### Key Architectural Insight: Driver Reload Only for NPS

**CRITICAL**: Driver reload is **ONLY required for NPS (memory partition) changes**, NOT for compute partition changes.

This fundamentally changes the architecture and enables much more flexible partitioning:

| Operation | Scope | Driver Reload? | Disruption | Flexibility |
|-----------|-------|----------------|------------|-------------|
| **Compute partition** (SPX/CPX/DPX/QPX) | Per-GPU | ❌ NO | None | ✅ High - can partition individual idle GPUs |
| **Memory partition** (NPS1/NPS2/NPS4) | Node-wide | ✅ YES | All GPUs on node | ⚠️ Low - requires entire node idle |

**Implications**:

1. **Per-GPU Partitioning for Compute**:
   - Can change GPU 0 from SPX to CPX while GPU 1 stays DPX
   - No disruption to other GPUs on same node
   - Only the target GPU needs to be idle
   - Immediate effect (no reload delay)

2. **Node-Wide Coordination for NPS**:
   - Changing NPS1 → NPS4 requires driver reload
   - All GPUs on node must be idle
   - All workloads terminated during reload
   - More conservative partitioning

3. **Mixed Compute Modes Allowed**:
   - Same node can have GPUs in different compute modes
   - Example: GPU0=CPX, GPU1=DPX, GPU2=SPX (all with same NPS)
   - Enables heterogeneous workload placement

4. **Re-Partitioning Flexibility**:
   - Compute-only re-partition: Very flexible, per-GPU
   - NPS re-partition: Conservative, node-wide

#### Memory Partition is Node-Wide
**Impact on Auto-Partitioning**:
- Changing NPS affects **all GPUs on the node simultaneously**
- Cannot satisfy conflicting NPS requests on same node (e.g., Pod A wants NPS1, Pod B wants NPS4)
- Must track node-level NPS state, not per-GPU

**Partition Decision Logic**:
```
Request: CPX_NPS4
Current Node State: All GPUs in SPX_NPS1

Decision:
1. Check if ANY GPU on node has active allocations
   - If yes → REJECT (cannot change NPS, would disrupt workloads)
2. Check if all GPUs are idle
   - If yes → Partition ENTIRE NODE (all GPUs) to CPX_NPS4
3. Update ResourceSlices for ALL GPUs on node
```

**Implications**:
- More conservative partitioning (entire node must be idle)
- Better utilization if all pods request same NPS mode
- Node-level partition "affinity" - once NPS set, prefer matching requests

#### Un-Partitioning Strategy

There are **three triggers** for un-partitioning:

---

### 1. Timer-Based Un-Partitioning (Idle GPUs)

**Trigger**: All GPUs on node become idle (no allocations)

**Flow**:
```
1. Detect last partition allocation released (per GPU or per node)
2. Start grace period timer (configurable, default 5 min)
   - Avoids thrashing if new partition pod scheduled soon
   - Timer specific to current partition profile
   - Can be per-GPU (compute-only) or per-node (NPS)
3. During grace period:
   - Monitor for new allocation requests
   - If new allocation: cancel timer, keep current partition
   - If timer expires: proceed to step 4
4. Double-check GPU/node still idle (race condition protection)
5. Execute un-partition to default profile:

   **Case A: Compute-Only Un-Partition (Single GPU)**
   a. Mark GPU as "resetting" (blocks new allocations on this GPU)
   b. Run: amd-smi set --gpu <id> --compute-partition SPX
   c. NO driver reload needed!
   d. Update ResourceSlices for this GPU only:
      - Remove partition devices for this GPU
      - Add single GPU device (SPX, same NPS)
   e. Other GPUs on node: UNAFFECTED

   **Case B: NPS Un-Partition (Entire Node)**
   a. Mark node as "resetting" (blocks new allocations)
   b. For each GPU:
      - amd-smi set --gpu <id> --compute-partition SPX
   c. For node (all GPUs):
      - amd-smi set --gpu all --memory-partition NPS1
   d. Reload driver:
      - amd-smi reset -r
   e. Wait for driver readiness
   f. Verify un-partition success
   g. Update ResourceSlices for ALL GPUs on node:
      - Remove all partition devices
      - Add single GPU devices (SPX_NPS1)
6. Mark GPU/node ready
7. Emit event: UnpartitionCompleted
```

**Configuration**:
```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
spec:
  draDriver:
    unpartitionPolicy:
      enableTimerBased: true
      gracePeriod: 300  # seconds (5 min default)
      defaultProfile: SPX_NPS1  # profile to return to
```

**Challenges**:
- Determining "default" profile per node (may vary by hardware/workload)
- Balancing responsiveness vs thrashing (grace period tuning)
- Handling re-allocation during grace period
- Different grace periods per partition type?

---

### 2. Admin-Forced Un-Partitioning

**Trigger**: Admin manually requests un-partition via node annotation

**Flow**:
```
1. Admin adds annotation to node:
   kubectl annotate node <node-name> \
     amd.com/force-unpartition=true \
     amd.com/target-profile=SPX_NPS1

2. DRA driver detects annotation change
3. Check if node is idle:
   - If idle: proceed immediately to un-partition
   - If busy: emit warning event, wait for idle OR admin override
4. Admin override (optional):
   kubectl annotate node <node-name> \
     amd.com/force-unpartition-override=true \
     --overwrite
   - This will EVICT all pods using GPU partitions
   - Dangerous operation, requires confirmation
5. Execute un-partition to target profile
6. Update ResourceSlices
7. Remove annotations:
   - amd.com/force-unpartition
   - amd.com/force-unpartition-override
   - amd.com/target-profile
8. Emit event: AdminForcedUnpartition
```

**Use Cases**:
- Node maintenance requiring default partition state
- Troubleshooting partition issues
- Reverting failed/stuck partition state
- Manual optimization of cluster resources

**Safety Mechanisms**:
- Warn if node has active allocations
- Require explicit override annotation to evict pods
- Emit events for audit trail
- Verify target profile is valid for node hardware

**Example**:
```bash
# Force un-partition idle node
kubectl annotate node gpu-node-123 amd.com/force-unpartition=true

# Force un-partition busy node (evicts pods)
kubectl annotate node gpu-node-123 \
  amd.com/force-unpartition=true \
  amd.com/force-unpartition-override=true \
  amd.com/target-profile=SPX_NPS1

# Check events
kubectl get events --field-selector involvedObject.name=gpu-node-123
```

---

### 3. Re-Partitioning for Different Profile (Idle GPU Optimization)

**Trigger**: New allocation request needs different partition profile, and current partitioned GPU is idle

**Scenario Example**:
```
Current State:
- Node: gpu-node-123
- GPU 0: CPX_NPS1 (8 partitions)
  - partition-0: ALLOCATED to pod-A
  - partitions 1-7: IDLE
- GPU 1: CPX_NPS1 (8 partitions)
  - All partitions: IDLE ✓

New Request:
- Pod-B requests: DPX_NPS4 (2 partitions with NPS4)

Decision:
- GPU 0: Busy (has allocation) → Skip
- GPU 1: Idle (no allocations) → Can re-partition
- Target: CPX_NPS1 → DPX_NPS4
- Challenge: NPS change affects ENTIRE NODE (both GPUs)
  → Cannot re-partition GPU 1 alone (NPS is node-wide)
  → Must wait until GPU 0 also idle
  → Pod-B remains PENDING
```

**Flow for Re-Partitioning**:
```
1. Allocation request with partition profile X arrives
2. Check existing ResourceSlices:
   - Exact match found? → Allocate immediately
3. Check for re-partition opportunity:
   a. Find nodes with idle GPUs
   b. Check if different partition profile Y exists
   c. Validate conversion: Y → X
   d. Check if ALL GPUs on node are idle:
      - If NPS change needed (Y.NPS ≠ X.NPS):
        * ENTIRE NODE must be idle
        * If not: skip this node, try others
      - If only compute partition change (Y.NPS == X.NPS):
        * Only idle GPUs get re-partitioned
        * Other GPUs keep current compute partition
4. If re-partition possible:
   a. Cancel any pending un-partition timer
   b. Execute partition to new profile X
   c. Update ResourceSlices
   d. Allocate from newly partitioned GPU
5. If not possible:
   a. Pod remains PENDING
   b. Emit event: AwaitingIdleNode
   c. When node becomes idle, timer-based un-partition starts
   d. After un-partition, re-evaluate pending pods
```

**Example Scenarios**:

**Scenario A: Compute-Only Change (NPS Same) - NOW WORKS!**
```
Current: CPX_NPS1 (8 compute partitions, NPS1)
Request: DPX_NPS1 (2 compute partitions, NPS1)
Node Status:
- GPU 0: 2/8 partitions allocated (BUSY)
- GPU 1: 0/8 partitions allocated (IDLE)

Decision:
✓ Re-partition GPU 1 only: CPX → DPX
✗ Cannot re-partition GPU 0 (busy)
✓ No NPS change, NO driver reload needed!
✓ GPU 0 stays CPX_NPS1, GPU 1 becomes DPX_NPS1

Execution:
1. Check GPU 1 is idle: TRUE
2. Run: amd-smi set --gpu 1 --compute-partition DPX
3. NO driver reload (NPS unchanged)
4. Update ResourceSlices:
   - GPU 0: Keep 8 CPX_NPS1 partitions (unchanged)
   - GPU 1: Replace with 2 DPX_NPS1 partitions
5. Allocate DPX partition to requesting pod

Result:
✓ Pod gets allocation immediately
✓ No disruption to GPU 0 workloads
✓ Mixed compute modes on same node
```

**Scenario B: NPS Change Required**
```
Current: CPX_NPS1
Request: CPX_NPS4 (NPS change)
Node Status:
- GPU 0: 1/8 partitions allocated
- GPU 1: 0/8 partitions allocated (IDLE)

Decision:
✗ NPS change affects ENTIRE NODE
✗ GPU 0 has allocation
→ Cannot re-partition
→ Pod remains PENDING
→ Emit event: AwaitingNodeIdle
```

**Scenario C: Different Compute + Different NPS**
```
Current: CPX_NPS1
Request: DPX_NPS4
Node Status:
- All GPUs idle

Decision:
✓ Node is idle
✓ Can convert CPX_NPS1 → DPX_NPS4
✓ Execute re-partition:
   1. Un-partition to SPX_NPS1 (reset)
   2. Partition to DPX_NPS4 (target)
   3. Update ResourceSlices
   4. Allocate
```

**CRITICAL INSIGHT - CORRECTED**:
**Re-partitioning flexibility depends on whether NPS changes**:

1. **Compute-only change (same NPS)**:
   - Can re-partition individual idle GPUs
   - No driver reload needed
   - No disruption to other GPUs
   - Much more flexible!

2. **NPS change required**:
   - Must wait for entire node to be idle
   - Driver reload affects all GPUs
   - More conservative approach

**Optimized Flow for Compute-Only Re-Partitioning**:
```
1. Allocation request arrives (e.g., DPX_NPS1)
2. Check exact match → Not found
3. Check for re-partition opportunity:
   a. Find GPUs with different compute partition (e.g., CPX_NPS1)
   b. Check if NPS matches (NPS1 == NPS1) → YES
   c. Check if specific GPU is idle → YES
4. Decision:
   ✓ Compute-only change possible
   ✓ No driver reload needed
   ✓ Can proceed even if other GPUs busy
5. Execute compute partition change:
   a. amd-smi set --gpu <id> --compute-partition DPX
   b. NO driver reload!
6. Update ResourceSlices (only for this GPU)
7. Allocate to requesting pod
```

**Optimized Flow for NPS Change Re-Partitioning**:
```
1. Allocation request arrives (e.g., CPX_NPS4)
2. Check exact match → Not found
3. Check for re-partition opportunity:
   a. Find node with different NPS (e.g., NPS1)
   b. NPS change required (NPS1 → NPS4)
   c. Check if ENTIRE NODE is idle → Must be YES
4. Decision:
   ✗ If any GPU busy → Pod remains PENDING
   ✓ If all idle → IMMEDIATE re-partition
5. Execute NPS partition change:
   a. Set compute partition for all GPUs
   b. Set memory partition (node-wide)
   c. Driver reload (affects entire node)
6. Update ResourceSlices (all GPUs on node)
7. Allocate to requesting pod
```

**Configuration**:
```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
spec:
  draDriver:
    unpartitionPolicy:
      enableRepartition: true  # Allow re-partition for pending pods
      preferRepartition: true  # Skip default profile, go direct
      maxRepartitionDelay: 60  # Max wait for idle (seconds)
```

---

### Un-Partitioning Priority

When multiple triggers conflict, priority order:

1. **Admin-forced** (highest priority)
   - Overrides timers and re-partition requests
   - Can evict pods with override flag

2. **Re-partitioning for pending allocation**
   - Overrides timer-based un-partition
   - Serves immediate workload need
   - Skips default profile

3. **Timer-based** (lowest priority)
   - Only executes if no pending allocations
   - Can be cancelled by new requests

**Example Timeline**:
```
T=0:    Last allocation released, timer starts (5 min)
T=2min: New allocation request (DPX_NPS1) arrives
        → Cancel timer
        → Check if node idle
        → Re-partition to DPX_NPS1
        → Allocate

T=10min: Allocation released again
         → Timer starts again
T=15min: Timer expires, no new requests
         → Un-partition to SPX_NPS1 (default)
```

---

### Handling Un-Partition Failures

**Failure Scenarios**:
1. amd-smi command fails
2. Driver reload fails or hangs
3. Post-partition verification fails
4. ResourceSlice update fails

**Recovery Strategy**:
```
1. Mark node as "error" state
2. Emit event: UnpartitionFailed (with error details)
3. Retry with exponential backoff:
   - Attempt 1: Immediate
   - Attempt 2: 30 seconds
   - Attempt 3: 1 minute
   - Attempt 4: 2 minutes
   - Max attempts: 5
4. If all attempts fail:
   - Mark node as "degraded"
   - Alert admin via event
   - Node remains in current partition state
   - Block new allocations until resolved
5. Admin intervention:
   - Manual troubleshooting
   - Force reset via annotation
   - Drain node and reboot if necessary
```

**Monitoring**:
```
# Metrics
amd_gpu_unpartition_failures_total{node, reason}
amd_gpu_node_degraded_total{node}

# Events
kubectl get events --field-selector reason=UnpartitionFailed
```

### Architecture Overview

**CRITICAL**: Uses **Structured Parameters** mode (NOT CEL-based) to enable auto-partitioning.

**Why Structured Parameters?**
- ❌ **CEL-based mode**: Scheduler filters nodes via CEL against ResourceSlices → only calls driver if match found → **cannot auto-partition** (no match = no driver call)
- ✅ **Structured Parameters mode**: Scheduler calls driver's `NodeListAndPrepare()` for **each candidate node** → driver can partition during scheduling → **auto-partition works!**

```
┌─────────────────────────────────────────────────────────────────┐
│ Pod Creation with ResourceClaim (Structured Parameters)         │
│   apiVersion: resource.k8s.io/v1                                │
│   kind: ResourceClaim                                           │
│   spec:                                                         │
│     devices:                                                    │
│       requests:                                                 │
│         - name: gpu                                             │
│           deviceClassName: gpu.amd.com                          │
│       config:  # Structured parameters (NOT CEL!)               │
│         - requests:                                             │
│             - partitionProfile: CPX_NPS1                        │
│               model: MI300X                                     │
│               memory: "12Gi"                                    │
└───────────────────────────────────┬─────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ K8s Scheduler (with DRA support)                                │
│  - Reads ResourceClaim structured parameters                    │
│  - For EACH candidate node, calls DRA driver:                  │
│    → NodeListAndPrepare(claim, node) gRPC method              │
│  - Driver returns: "can satisfy" or "cannot satisfy"           │
│  - Scheduler selects best node from driver responses           │
└───────────────────────────────────┬─────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ AMD GPU DRA Driver: NodeListAndPrepare() gRPC Method           │
│ (Called by scheduler for EACH candidate node)                  │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 1. Parse Requested Partition from Structured Params     │   │
│  │    - Extract: partitionProfile = CPX_NPS1               │   │
│  │    - Extract: model = MI300X, memory = 12Gi             │   │
│  └─────────────────────────────────────────────────────────┘   │
│                     │                                           │
│                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 2. Check Exact Match                                    │   │
│  │    - Query ResourceSlices for this node                 │   │
│  │    - Find devices with currentPartitionProfile=CPX_NPS1 │   │
│  │    - If found AND idle → Return immediately             │   │
│  └─────────────────────────────────────────────────────────┘   │
│                     │ No exact match                            │
│                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 3. Check Convertible GPUs (AUTO-PARTITION LOGIC)        │   │
│  │    - Check ResourceSlice supportedPartitionProfiles     │   │
│  │    - Find GPU where CPX_NPS1 in supportedProfiles       │   │
│  │    - Check allocation status:                           │   │
│  │      * NPS changing? → ENTIRE NODE must be idle         │   │
│  │      * Compute only? → Only target GPU must be idle     │   │
│  │    - Validate conversion compatibility                  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                     │ Found convertible idle GPU(s)             │
│                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 4. Execute Partition (SYNCHRONOUSLY during scheduling)  │   │
│  │    a. Lock GPU/node to prevent concurrent partition     │   │
│  │    b. Determine partition path:                         │   │
│  │       - Compute-only? → Per-GPU, no reload (~1-5s)      │   │
│  │       - NPS change? → Node-wide, reload needed (~30s)   │   │
│  │                                                          │   │
│  │    PATH 1 - COMPUTE-ONLY (per-GPU):                     │   │
│  │      - amd-smi set --gpu X --compute-partition CPX      │   │
│  │      - NO driver reload!                                │   │
│  │      - Other GPUs UNAFFECTED                            │   │
│  │                                                          │   │
│  │    PATH 2 - NPS CHANGE (node-wide):                     │   │
│  │      - For each GPU: set compute partition              │   │
│  │      - amd-smi set --gpu all --memory-partition NPS1    │   │
│  │      - amd-smi reset -r (driver reload, all GPUs)       │   │
│  │                                                          │   │
│  │    c. Wait for operation completion                     │   │
│  │    d. Verify partition success                          │   │
│  └─────────────────────────────────────────────────────────┘   │
│                     │ Success                                   │
│                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 5. Update ResourceSlices (BEFORE returning to scheduler)│   │
│  │    For compute-only change (per-GPU):                   │   │
│  │      - Update GPU 0: SPX → CPX (8 partitions)           │   │
│  │      - GPU 1,2,3... unchanged                           │   │
│  │    For NPS change (node-wide):                          │   │
│  │      - Update ALL GPUs with new profile                 │   │
│  │    - Update currentPartitionProfile attribute           │   │
│  │    - Publish updated ResourceSlice to API server        │   │
│  └─────────────────────────────────────────────────────────┘   │
│                     │                                           │
│                     ▼                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 6. Return Success to Scheduler                          │   │
│  │    - Return list of available devices (partitions)      │   │
│  │    - Scheduler selects one partition                    │   │
│  │    - Scheduler calls Allocate() to finalize            │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ Background: Monitor Allocation Releases (UN-PARTITION)  │   │
│  │    - Separate goroutine watches ResourceClaim releases  │   │
│  │    - Detect when last partition released                │   │
│  │    - Start grace period timer (default 5 min)           │   │
│  │    - If no new allocations → un-partition GPU/node      │   │
│  │    - Return to default SPX_NPS1                         │   │
│  │    - Update ResourceSlices                              │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Key Components

#### 1. AMD DRA Driver Extensions
**Base**: https://github.com/ROCm/k8s-gpu-dra-driver

**Key Architectural Point**: Partition logic runs **synchronously inside `NodeListAndPrepare()` gRPC method**, NOT as a separate controller watching ResourceClaims.

**Extensions Needed**:

- **NodeListAndPrepare() Implementation**: Core method where auto-partitioning happens:
  - Called by K8s scheduler for **each candidate node** during pod scheduling
  - Parses structured parameters from ResourceClaim config (partitionProfile, model, memory)
  - Checks if exact match exists in ResourceSlice → return immediately
  - Checks if GPU can be converted (via `supportedPartitionProfiles` attribute)
  - **Partitions GPU synchronously** during this call (scheduler waits)
  - Updates ResourceSlices **before** returning to scheduler
  - Returns list of available devices or error if cannot satisfy
  - **This is where the magic happens** - scheduler calls us before it has decided, giving us a chance to partition

- **Node Partition State Tracker**: Track node-level state:
  ```go
  type NodePartitionState struct {
      NodeName        string
      CurrentNPS      NPSMode // NPS1, NPS2, NPS4
      GPUStates       map[string]GPUState // GPU ID -> state
      AllGPUsIdle     bool
      LastAllocation  time.Time
      UnpartitionTimer *time.Timer
  }
  ```

- **Un-Partition Manager**: Handle returning to default state:
  - Monitor allocation releases
  - Trigger un-partition after grace period
  - Handle rollback on un-partition failure

- **Partition Compatibility Matrix**: Logic to determine valid conversions:
  ```go
  // Example: Can SPX_NPS1 convert to CPX_NPS1?
  func canConvert(current, requested PartitionProfile) bool {
      // NPS4 only works with CPX
      if requested.Memory == NPS4 && requested.Compute != CPX {
          return false
      }
      // Check if GPU is in default state (SPX)
      if current.Compute == SPX {
          return true // SPX can convert to any mode
      }
      // Already partitioned, cannot re-partition without reset
      return false
  }
  ```

- **Allocation Tracker**: Track which GPUs/partitions are allocated vs idle:
  ```go
  type GPUAllocationStatus struct {
      GPUID       string
      Partitions  map[string]AllocationState // partition ID -> state
      IsIdle      bool // true if all partitions unallocated
  }

  type NodeAllocationStatus struct {
      NodeName    string
      GPUs        map[string]*GPUAllocationStatus
      AllGPUsIdle bool // true if ALL GPUs on node are idle
  }
  ```

### Detailed Scenario: NPS4 Partitioning

#### Background on NPS Modes
**NPS (NUMA Per Socket)** determines how memory is exposed to the system:
- **NPS1**: All HBM stacks (96GB on MI300X) appear as single unified memory domain
- **NPS2**: Memory divided into 2 NUMA domains (2 × 48GB)
- **NPS4**: Memory divided into 4 NUMA domains (4 × 24GB)

**Critical Constraint**: NPS4 **requires** CPX mode (8 compute partitions)
- Each XCD (compute die) has direct access to its local memory quadrant
- SPX_NPS4 is **invalid** - would create 1 compute partition with 4 memory domains (nonsensical)
- Valid combinations: SPX_NPS1, CPX_NPS1, CPX_NPS2, CPX_NPS4

#### Example: Node with 2× MI300X GPUs, NPS4 Partitioning

**Initial State**:
- Node: `gpu-node-123`
- 2× AMD MI300X GPUs (gpu-0, gpu-1)
- Current mode: SPX_NPS1
- Total allocatable resources: 2 GPUs

**Pod Request**:
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
spec:
  devices:
    requests:
      - name: gpu
        deviceClassName: gpu.amd.com
        selectors:
          - cel:
              expression: |
                device.attributes["partitionProfile"] == "CPX_NPS4" &&
                device.attributes["memory"].asQuantity() >= quantity("20Gi")
```

**DRA Driver Decision Flow**:
```
1. Check exact match: No CPX_NPS4 GPUs available
2. Check convertible nodes:
   - gpu-node-123 has SPX_NPS1 GPUs
   - Can convert SPX_NPS1 → CPX_NPS4? YES (valid combination)
   - Is entire node idle? Check all GPUs:
     * gpu-0: No allocations ✓
     * gpu-1: No allocations ✓
   - All GPUs idle: TRUE
3. Execute partition:
   - Lock node gpu-node-123
   - Mark both GPUs as "partitioning"
   - Run: amd-smi set --gpu 0 --compute-partition CPX
   - Run: amd-smi set --gpu 1 --compute-partition CPX
   - Run: amd-smi set --gpu all --memory-partition NPS4  ← NODE-WIDE
   - Run: amd-smi reset -r  ← Reload driver, affects entire node
   - Wait for driver ready
   - Verify partition success for both GPUs
4. Update ResourceSlices:
   - Remove: 2 SPX_NPS1 devices (gpu-0, gpu-1)
   - Add: 64 CPX_NPS4 partitions (2 GPUs × 8 XCDs × 4 NPS domains)
     * Wait, that's not how it works...
```

**IMPORTANT CLARIFICATION**: How CPX_NPS4 Actually Works

Each MI300X in CPX mode exposes **8 compute partitions** (one per XCD).
With NPS4, each partition has access to **its local memory quadrant** (24GB).

So the math is:
- 2 GPUs × 8 partitions/GPU = **16 total allocatable partitions**
- Each partition: 24GB memory (1 NPS domain), 38 CUs

**NOT** 64 partitions - NPS doesn't multiply partitions, it changes memory topology.

#### Partition Execution Commands

**Scenario 1: Compute-Only Change (SPX_NPS1 → CPX_NPS1)**
```bash
# Set compute partition to CPX for target GPU
amd-smi set --gpu 0 --compute-partition CPX

# NO DRIVER RELOAD NEEDED! Change takes effect immediately.
# Other GPUs (gpu-1, gpu-2, etc.) are not affected.

# Verification (immediate)
amd-smi list
# Output:
# GPU: 0 - CPX mode, 8 partitions, NPS1
# GPU: 1 - SPX mode (unchanged), NPS1
```

**Scenario 2: Memory Partition Change (SPX_NPS1 → CPX_NPS4)**
```bash
# Step 1: Set compute partition to CPX for EACH GPU on node
amd-smi set --gpu 0 --compute-partition CPX
amd-smi set --gpu 1 --compute-partition CPX
# No reload yet, changes buffered

# Step 2: Set memory partition to NPS4 (affects ENTIRE NODE, all GPUs)
amd-smi set --gpu all --memory-partition NPS4

# Step 3: Reload driver (REQUIRED for NPS change)
amd-smi reset -r
# This terminates ALL GPU workloads on the node

# Verification (after driver reload)
amd-smi list
# Output:
# GPU: 0 - CPX mode, 8 partitions
#   Partition: 0, Memory: 24GB (NPS domain 0)
#   Partition: 1, Memory: 24GB (NPS domain 1)
#   Partition: 2, Memory: 24GB (NPS domain 2)
#   Partition: 3, Memory: 24GB (NPS domain 3)
#   Partition: 4, Memory: 24GB (NPS domain 0) - shared
#   Partition: 5, Memory: 24GB (NPS domain 1) - shared
#   Partition: 6, Memory: 24GB (NPS domain 2) - shared
#   Partition: 7, Memory: 24GB (NPS domain 3) - shared
# GPU: 1 - CPX mode, 8 partitions
#   [Same structure - 8 partitions × 4 NPS domains]
```

**Scenario 3: Mixed Compute Modes on Same Node**
```bash
# Possible! Each GPU can have different compute partition
amd-smi set --gpu 0 --compute-partition CPX  # 8 partitions
amd-smi set --gpu 1 --compute-partition DPX  # 2 partitions
amd-smi set --gpu 2 --compute-partition SPX  # 1 GPU

# No driver reload needed (same NPS)
# Result: Node has CPX, DPX, and SPX GPUs simultaneously
```

**Note**: Each XCD partition accesses one NPS domain, so there's memory locality awareness.

#### 2. ResourceSlice Management

**CRITICAL**: ResourceSlice must include `supportedPartitionProfiles` attribute to enable auto-partitioning!

**Current State** (before partition):
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: node-123-gpu-slice
  annotations:
    amd.com/partition-profile: "SPX_NPS1"
    amd.com/nps-mode: "NPS1"
spec:
  nodeName: node-123
  pool:
    name: gpu.amd.com
    resourceSliceCount: 1
  devices:
    - name: gpu-0
      basic:
        attributes:
          model: {string: "MI300X"}
          currentPartitionProfile: {string: "SPX_NPS1"}  # Current state
          supportedPartitionProfiles: {stringSlice: ["SPX_NPS1", "CPX_NPS1", "DPX_NPS1", "QPX_NPS1"]}  # What CAN be provided
          memory: {quantity: "96Gi"}
          computeUnits: {int: 304}
          isIdle: {bool: true}  # Critical for auto-partition decision
    - name: gpu-1
      basic:
        attributes:
          model: {string: "MI300X"}
          currentPartitionProfile: {string: "SPX_NPS1"}
          supportedPartitionProfiles: {stringSlice: ["SPX_NPS1", "CPX_NPS1", "DPX_NPS1", "QPX_NPS1"]}
          memory: {quantity: "96Gi"}
          computeUnits: {int: 304}
          isIdle: {bool: true}
```

**Why `supportedPartitionProfiles` is critical**:
- Scheduler calls `NodeListAndPrepare()` for each node
- Driver checks if requested profile is in `supportedPartitionProfiles`
- If yes + GPU idle → driver partitions
- If no → driver returns "cannot satisfy", scheduler tries next node

**After Partition - Example 1: CPX_NPS1** (SPX → CPX with NPS1):
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: node-123-gpu-slice
  annotations:
    amd.com/partition-profile: "CPX_NPS1"
    amd.com/nps-mode: "NPS1"
    amd.com/last-partition-time: "2026-02-27T10:30:00Z"
    amd.com/total-partitions: "16"  # 2 GPUs × 8 partitions
spec:
  nodeName: node-123
  pool:
    name: gpu.amd.com
    resourceSliceCount: 1
  devices:
    # GPU 0 - 8 partitions (one per XCD)
    - name: gpu-0-partition-0
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS1"}
          memory: {quantity: "12Gi"}  # 96GB / 8 XCDs
          computeUnits: {int: 38}      # 304 CUs / 8 XCDs
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 0}
          npsMode: {string: "NPS1"}
          npsDomain: {int: 0}  # All partitions access all memory (NPS1)
    - name: gpu-0-partition-1
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS1"}
          memory: {quantity: "12Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 1}
          npsMode: {string: "NPS1"}
          npsDomain: {int: 0}
    # ... (6 more partitions for gpu-0, partitions 2-7)

    # GPU 1 - 8 partitions
    - name: gpu-1-partition-0
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS1"}
          memory: {quantity: "12Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-1"}
          xcdIndex: {int: 0}
          npsMode: {string: "NPS1"}
          npsDomain: {int: 0}
    # ... (7 more partitions for gpu-1, partitions 1-7)
```
**Total allocatable resources**: 16 partitions (2 GPUs × 8 partitions/GPU)

---

**After Partition - Example 2: CPX_NPS4** (SPX → CPX with NPS4):
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: node-123-gpu-slice
  annotations:
    amd.com/partition-profile: "CPX_NPS4"
    amd.com/nps-mode: "NPS4"
    amd.com/last-partition-time: "2026-02-27T10:35:00Z"
    amd.com/total-partitions: "16"  # 2 GPUs × 8 partitions
spec:
  nodeName: node-123
  pool:
    name: gpu.amd.com
    resourceSliceCount: 1
  devices:
    # GPU 0 - 8 partitions, each with affinity to NPS domain
    - name: gpu-0-partition-0
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}  # 96GB / 4 NPS domains
          memoryAccessible: {quantity: "24Gi"}  # Only local domain
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 0}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 0}  # Affinity to NPS domain 0
          numaNode: {int: 0}   # NUMA node ID
    - name: gpu-0-partition-1
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 1}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 1}  # Affinity to NPS domain 1
          numaNode: {int: 1}
    - name: gpu-0-partition-2
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 2}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 2}  # Affinity to NPS domain 2
          numaNode: {int: 2}
    - name: gpu-0-partition-3
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 3}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 3}  # Affinity to NPS domain 3
          numaNode: {int: 3}
    - name: gpu-0-partition-4
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 4}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 0}  # XCD 4 maps to domain 0 (locality)
          numaNode: {int: 0}
    - name: gpu-0-partition-5
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 5}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 1}  # XCD 5 maps to domain 1
          numaNode: {int: 1}
    - name: gpu-0-partition-6
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 6}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 2}  # XCD 6 maps to domain 2
          numaNode: {int: 2}
    - name: gpu-0-partition-7
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-0"}
          xcdIndex: {int: 7}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 3}  # XCD 7 maps to domain 3
          numaNode: {int: 3}

    # GPU 1 - 8 partitions (same structure as GPU 0)
    - name: gpu-1-partition-0
      basic:
        attributes:
          model: {string: "MI300X"}
          partitionProfile: {string: "CPX_NPS4"}
          memory: {quantity: "24Gi"}
          memoryAccessible: {quantity: "24Gi"}
          computeUnits: {int: 38}
          parentGPU: {string: "gpu-1"}
          xcdIndex: {int: 0}
          npsMode: {string: "NPS4"}
          npsDomain: {int: 4}  # GPU 1 gets domains 4-7 (global)
          numaNode: {int: 4}
    # ... (7 more partitions for gpu-1)
```

**Key Differences with NPS4**:
1. **Memory per partition**: 24GB (96GB / 4 NPS domains) vs 12GB (96GB / 8 XCDs)
2. **NUMA affinity**: Each partition has `npsDomain` and `numaNode` attributes
3. **Memory locality**: Partitions access their local NPS domain with lower latency
4. **Total partitions**: Still 16 (2 GPUs × 8 partitions), NPS doesn't change partition count
5. **Topology awareness**: Scheduler can consider NUMA locality for workload placement

**Total allocatable resources**: 16 partitions (2 GPUs × 8 partitions/GPU)

---

### How Pods Match NPS4 Partitions

**Pod requesting ≥20GB memory**:
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
spec:
  devices:
    requests:
      - name: gpu
        deviceClassName: gpu.amd.com
        selectors:
          - cel:
              expression: |
                device.attributes["partitionProfile"] == "CPX_NPS4" &&
                device.attributes["memory"].asQuantity() >= quantity("20Gi")
```

**Scheduler evaluation**:
```
Check node-123 ResourceSlice:
- gpu-0-partition-0: memory=24Gi, partitionProfile=CPX_NPS4 ✓ MATCH
- gpu-0-partition-1: memory=24Gi, partitionProfile=CPX_NPS4 ✓ MATCH
- ... (all 16 partitions match)

Pick partition with best NUMA affinity (if specified in pod)
Allocate: gpu-0-partition-0 (NUMA node 0)
```

**Why NPS4 for LLM Inference**:
- Better memory bandwidth (each XCD accesses local memory)
- NUMA locality reduces latency
- Useful for large model partitioning across multiple partitions

---

### Memory Topology Visualization

**SPX_NPS1** (Before Partition):
```
┌─────────────────────────────────────────────┐
│         GPU 0 (SPX_NPS1)                    │
│  ┌─────────────────────────────────────┐    │
│  │  8 XCDs (304 CUs total)             │    │
│  │  Unified Memory View: 96GB          │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘

Allocatable: 1 GPU (96GB, 304 CUs)
```

**CPX_NPS1** (After Partition):
```
┌─────────────────────────────────────────────┐
│         GPU 0 (CPX_NPS1)                    │
│  ┌──────┐┌──────┐┌──────┐┌──────┐          │
│  │XCD 0 ││XCD 1 ││XCD 2 ││XCD 3 │          │
│  │12GB  ││12GB  ││12GB  ││12GB  │          │
│  │38 CUs││38 CUs││38 CUs││38 CUs│          │
│  └──────┘└──────┘└──────┘└──────┘          │
│  ┌──────┐┌──────┐┌──────┐┌──────┐          │
│  │XCD 4 ││XCD 5 ││XCD 6 ││XCD 7 │          │
│  │12GB  ││12GB  ││12GB  ││12GB  │          │
│  │38 CUs││38 CUs││38 CUs││38 CUs│          │
│  └──────┘└──────┘└──────┘└──────┘          │
│  Unified Memory: 96GB (all XCDs access)    │
└─────────────────────────────────────────────┘

Allocatable: 8 partitions (12GB, 38 CUs each)
```

**CPX_NPS4** (After Partition with NPS4):
```
┌─────────────────────────────────────────────┐
│         GPU 0 (CPX_NPS4)                    │
│  NPS Domain 0   NPS Domain 1                │
│  ┌──────┐      ┌──────┐                     │
│  │XCD 0 │      │XCD 1 │                     │
│  │24GB* │      │24GB* │                     │
│  │38 CUs│      │38 CUs│                     │
│  └──────┘      └──────┘                     │
│  ┌──────┐      ┌──────┐                     │
│  │XCD 4 │      │XCD 5 │                     │
│  │24GB* │      │24GB* │                     │
│  │38 CUs│      │38 CUs│                     │
│  └──────┘      └──────┘                     │
│  NPS Domain 2   NPS Domain 3                │
│  ┌──────┐      ┌──────┐                     │
│  │XCD 2 │      │XCD 3 │                     │
│  │24GB* │      │24GB* │                     │
│  │38 CUs│      │38 CUs│                     │
│  └──────┘      └──────┘                     │
│  ┌──────┐      ┌──────┐                     │
│  │XCD 6 │      │XCD 7 │                     │
│  │24GB* │      │24GB* │                     │
│  │38 CUs│      │38 CUs│                     │
│  └──────┘      └──────┘                     │
│                                             │
│  *Each XCD has local access to its NPS     │
│   domain (24GB), remote access to others   │
│  Total Memory: 96GB (4 × 24GB domains)     │
└─────────────────────────────────────────────┘

Allocatable: 8 partitions (24GB local, 38 CUs each)
              with NUMA locality
```

#### 3. Integration with GPU Operator
**Current GPU Operator Components**:
- ✅ Device Config Manager (DCM) - has partition execution logic
- ✅ Node Labeller - labels nodes with GPU info
- ✅ Device Plugin - discovers GPUs (legacy path)

**New Integration Points**:
1. **Deploy DRA Driver alongside Device Plugin**:
   - GPU Operator Helm chart installs DRA driver DaemonSet
   - DRA driver runs on all GPU nodes
   - Device plugin remains for backward compatibility

2. **Reuse DCM Partition Logic**:
   - DRA driver calls DCM partition functions
   - Avoid code duplication
   - Consistent partition behavior

3. **Node Labeller Integration**:
   - Node labeller detects partition state changes
   - Updates node labels for visibility
   - Useful for monitoring/debugging

---

## Implementation Plan

### Phase 1: Evaluate AMD DRA Driver (1 week)
**Goal**: Understand current capabilities and gaps

**Tasks**:
1. Clone and review https://github.com/ROCm/k8s-gpu-dra-driver
2. Identify existing ResourceSlice publishing logic
3. Check for partition awareness in current implementation
4. Review allocation/deallocation lifecycle hooks
5. Test basic DRA functionality without partitioning:
   - Deploy DRA driver
   - Create ResourceClaim for default SPX GPU
   - Verify allocation works

**Deliverables**:
- Gap analysis document
- Test results showing basic DRA functionality
- List of required extensions

---

### Phase 2: Design Auto-Partition Extension (1 week)
**Goal**: Design architecture for auto-partitioning logic

**Tasks**:
1. Design partition controller component:
   - When to trigger partition (allocation request with mismatch)
   - How to check ENTIRE NODE idle state
   - Locking mechanism to prevent concurrent partitions
   - Error handling and rollback

2. Design ResourceSlice update strategy:
   - How to atomically replace SPX with CPX partitions for all GPUs
   - Handle race conditions with scheduler
   - Versioning to prevent stale reads

3. Design partition compatibility matrix:
   - Valid conversion paths (SPX→CPX, SPX→DPX, etc.)
   - Memory/compute constraints (NPS4 requires CPX)
   - Per-GPU-model capabilities

4. Design allocation tracking:
   - Data structure to track allocated partitions per node
   - Persistence (in-memory vs etcd)
   - Sync with ResourceSlice state

5. Design un-partition logic:
   - Grace period management
   - Default partition profile per node
   - Rollback on failure

**Deliverables**:
- Architecture design document
- Sequence diagrams for partition and un-partition flows
- API specifications for new components
- Edge case analysis

---

### Phase 3: Implement NodeListAndPrepare Method (2-3 weeks)
**Goal**: Implement DRA driver's `NodeListAndPrepare()` gRPC method with auto-partitioning logic

**CRITICAL**: This is NOT a separate controller. This is the core gRPC method called by K8s scheduler during pod scheduling.

**Tasks**:
1. **Implement NodeListAndPrepare() with auto-partitioning**:
   ```go
   // pkg/dradriver/node_list_and_prepare.go
   // This is the gRPC method called by K8s scheduler for EACH candidate node

   func (d *DRADriver) NodeListAndPrepare(
       ctx context.Context,
       req *drapb.NodeListAndPrepareRequest,
   ) (*drapb.NodeListAndPrepareResponse, error) {
       // Step 1: Parse structured parameters from ResourceClaim
       requestedProfile, err := d.parsePartitionProfile(req.Claim)
       if err != nil {
           return nil, status.Errorf(codes.InvalidArgument, "invalid partition profile: %v", err)
       }

       nodeName := req.NodeName

       // Step 2: Check for exact match in ResourceSlice
       devices, err := d.findExactMatch(nodeName, requestedProfile)
       if err == nil && len(devices) > 0 {
           // Found exact match - return immediately, no partition needed
           return &drapb.NodeListAndPrepareResponse{
               Devices: devices,
           }, nil
       }

       // Step 3: No exact match - check if we can auto-partition

       // Get node state
       nodeState := d.getNodePartitionState(nodeName)

       // Determine if NPS change is needed
       npsChangeNeeded := nodeState.CurrentNPS != requestedProfile.NPS

       if npsChangeNeeded {
           // PATH 2: NPS change (node-wide operation, requires driver reload)

           // Check if ENTIRE NODE is idle (all GPUs)
           if !nodeState.AllGPUsIdle {
               return nil, status.Errorf(codes.ResourceExhausted,
                   "NPS change requires entire node idle, but %d GPUs have allocations",
                   nodeState.ActiveGPUCount)
           }

           // Partition entire node (SYNCHRONOUSLY during scheduling)
           if err := d.partitionEntireNode(nodeName, requestedProfile); err != nil {
               return nil, status.Errorf(codes.Internal, "partition failed: %v", err)
           }

           // Update ResourceSlices for ALL GPUs
           if err := d.updateResourceSlicesForNode(nodeName, requestedProfile); err != nil {
               return nil, status.Errorf(codes.Internal, "ResourceSlice update failed: %v", err)
           }

           // Return newly created devices
           devices, err := d.getDevicesAfterPartition(nodeName, requestedProfile)
           if err != nil {
               return nil, status.Errorf(codes.Internal, "failed to get devices: %v", err)
           }

           return &drapb.NodeListAndPrepareResponse{
               Devices: devices,
           }, nil

       } else {
           // PATH 1: Compute-only change (per-GPU operation, NO driver reload)

           // Find an idle GPU that can be converted
           gpu, err := d.findIdleConvertibleGPU(nodeName, requestedProfile)
           if err != nil {
               return nil, status.Errorf(codes.ResourceExhausted,
                   "no idle convertible GPU: %v", err)
           }

           // Check if this specific GPU is idle
           if !d.isGPUIdle(gpu.ID) {
               return nil, status.Errorf(codes.ResourceExhausted,
                   "GPU %s is not idle", gpu.ID)
           }

           // Partition ONLY this GPU (SYNCHRONOUSLY, no reload, ~1-5s)
           if err := d.partitionSingleGPU(gpu, requestedProfile); err != nil {
               return nil, status.Errorf(codes.Internal, "partition failed: %v", err)
           }

           // Update ResourceSlices for THIS GPU only
           if err := d.updateResourceSlicesForGPU(gpu, requestedProfile); err != nil {
               return nil, status.Errorf(codes.Internal, "ResourceSlice update failed: %v", err)
           }

           // Return newly created partitions for this GPU
           devices, err := d.getDevicesForGPU(gpu.ID, requestedProfile)
           if err != nil {
               return nil, status.Errorf(codes.Internal, "failed to get devices: %v", err)
           }

           return &drapb.NodeListAndPrepareResponse{
               Devices: devices,
           }, nil
       }
   }

   // Helper: Parse partition profile from structured parameters
   func (d *DRADriver) parsePartitionProfile(claim *ResourceClaim) (PartitionProfile, error) {
       // Extract from claim.Config (structured parameters)
       config := claim.Spec.Devices.Config
       if len(config) == 0 {
           return PartitionProfile{}, fmt.Errorf("no config in claim")
       }

       // Example: partitionProfile: "CPX_NPS1"
       profileStr := config[0].Requests["partitionProfile"]
       return ParsePartitionProfile(profileStr)
   }

   // Partition entire node (for NPS changes)
   func (pc *PartitionController) partitionEntireNode(
       node string,
       profile PartitionProfile,
   ) error {
       gpus := pc.getGPUsOnNode(node)

       // Lock entire node
       if err := pc.lockNode(node); err != nil {
           return err
       }
       defer pc.unlockNode(node)

       // Mark all GPUs as transitioning
       for _, gpu := range gpus {
           pc.setGPUState(gpu.ID, StatePartitioning)
       }

       // Get old profile for comparison
       oldProfile := pc.getNodePartitionState(node).CurrentProfile

       // Execute commands (includes driver reload for NPS change)
       if err := pc.executePartitionCommands(node, gpus, oldProfile, profile); err != nil {
           for _, gpu := range gpus {
               pc.setGPUState(gpu.ID, StateError)
           }
           return err
       }

       // Wait for driver ready (after reload)
       if err := pc.waitForDriverReady(node); err != nil {
           return err
       }

       // Verify all GPUs
       for _, gpu := range gpus {
           if err := pc.verifyPartition(gpu.ID, profile); err != nil {
               return err
           }
           pc.setGPUState(gpu.ID, StateReady)
       }

       pc.updateNodePartitionState(node, profile)
       return nil
   }

   // Partition single GPU (compute-only, no NPS change)
   func (pc *PartitionController) partitionSingleGPU(
       gpu *GPU,
       profile PartitionProfile,
   ) error {
       // Lock only this GPU
       if err := pc.lockGPU(gpu.ID); err != nil {
           return err
       }
       defer pc.unlockGPU(gpu.ID)

       // Mark as transitioning
       pc.setGPUState(gpu.ID, StatePartitioning)

       // Get old compute mode
       oldComputeMode := pc.getGPUState(gpu.ID).ComputeMode

       // Execute compute partition change (NO driver reload)
       if err := pc.executeComputePartitionSingleGPU(gpu.NodeName, gpu, profile.Compute); err != nil {
           pc.setGPUState(gpu.ID, StateError)
           return err
       }

       // Verify immediately (no driver reload delay)
       if err := pc.verifyGPUPartition(gpu.ID, profile); err != nil {
           return err
       }

       pc.setGPUState(gpu.ID, StateReady)
       log.Infof("Successfully partitioned GPU %s: %s -> %s (no disruption)",
                 gpu.ID, oldComputeMode, profile.Compute)
       return nil
   }

   // Handle allocation release - trigger un-partition if needed
   func (pc *PartitionController) HandleAllocationRelease(
       allocation *Allocation,
   ) error {
       node := allocation.NodeName
       nodeState := pc.getNodePartitionState(node)

       // Check if this was the last allocation on node
       if pc.getActiveAllocations(node) == 0 {
           // Start un-partition grace period
           pc.scheduleUnpartition(node, nodeState.UnpartitionGracePeriod)
       }

       return nil
   }

   // Un-partition node back to default state
   func (pc *PartitionController) unpartitionNode(node string) error {
       nodeState := pc.getNodePartitionState(node)

       // Double-check node is still idle
       if !nodeState.AllGPUsIdle {
           return nil // New allocation arrived, cancel un-partition
       }

       // Lock node
       if err := pc.lockNode(node); err != nil {
           return err
       }
       defer pc.unlockNode(node)

       // Execute un-partition to default SPX_NPS1
       defaultProfile := PartitionProfile{Compute: SPX, Memory: NPS1}
       if err := pc.partitionNode(node, defaultProfile); err != nil {
           return err
       }

       // Update ResourceSlices
       return pc.updateResourceSlicesForNode(node, defaultProfile)
   }
   ```

2. **Implement NODE partition execution** (reuse DCM logic):
   ```go
   func (pc *PartitionController) partitionNode(
       node string,
       profile PartitionProfile,
   ) error {
       // Lock entire NODE to prevent concurrent operations
       if err := pc.lockNode(node); err != nil {
           return err
       }
       defer pc.unlockNode(node)

       // Mark all GPUs as transitioning
       gpus := pc.getGPUsOnNode(node)
       for _, gpu := range gpus {
           pc.setGPUState(gpu.ID, StatePartitioning)
       }

       // Execute amd-smi commands
       // Note: NPS is node-wide, compute partition per-GPU
       if err := pc.executePartitionCommands(node, gpus, profile); err != nil {
           for _, gpu := range gpus {
               pc.setGPUState(gpu.ID, StateError)
           }
           return err
       }

       // Wait for driver reload completion
       if err := pc.waitForDriverReady(node); err != nil {
           return err
       }

       // Verify partition for all GPUs
       for _, gpu := range gpus {
           if err := pc.verifyPartition(gpu.ID, profile); err != nil {
               return err
           }
           pc.setGPUState(gpu.ID, StateReady)
       }

       // Update node partition state
       pc.updateNodePartitionState(node, profile)
       return nil
   }

   func (pc *PartitionController) executePartitionCommands(
       node string,
       gpus []*GPU,
       oldProfile, newProfile PartitionProfile,
   ) error {
       // Determine if NPS change is needed
       npsChanged := oldProfile.Memory != newProfile.Memory

       // Step 1: Set compute partition (per GPU)
       for _, gpu := range gpus {
           cmd := fmt.Sprintf("amd-smi set --gpu %s --compute-partition %s",
                             gpu.ID, newProfile.Compute)
           if err := pc.runCommand(node, cmd); err != nil {
               return fmt.Errorf("failed to set compute partition for GPU %s: %w",
                                 gpu.ID, err)
           }
       }

       // Step 2: If NPS changed, set memory partition and reload driver
       if npsChanged {
           // Set memory partition (affects all GPUs on node)
           cmd := fmt.Sprintf("amd-smi set --gpu all --memory-partition %s",
                             newProfile.Memory)
           if err := pc.runCommand(node, cmd); err != nil {
               return fmt.Errorf("failed to set memory partition: %w", err)
           }

           // Reload driver (ONLY for NPS changes)
           cmd = "amd-smi reset -r"
           if err := pc.runCommand(node, cmd); err != nil {
               return fmt.Errorf("failed to reload driver: %w", err)
           }

           log.Infof("Driver reload completed for NPS change: %s -> %s",
                     oldProfile.Memory, newProfile.Memory)
       } else {
           // Compute-only change, NO driver reload needed
           log.Infof("Compute-only partition change, no driver reload: %s -> %s",
                     oldProfile.Compute, newProfile.Compute)
       }

       return nil
   }

   // Execute partition for single GPU (compute-only, no NPS change)
   func (pc *PartitionController) executeComputePartitionSingleGPU(
       node string,
       gpu *GPU,
       newComputeMode ComputeMode,
   ) error {
       cmd := fmt.Sprintf("amd-smi set --gpu %s --compute-partition %s",
                         gpu.ID, newComputeMode)
       if err := pc.runCommand(node, cmd); err != nil {
           return fmt.Errorf("failed to set compute partition for GPU %s: %w",
                             gpu.ID, err)
       }

       // NO driver reload needed!
       log.Infof("Compute partition changed for GPU %s: %s (no driver reload)",
                 gpu.ID, newComputeMode)
       return nil
   }
   ```

3. **Implement ResourceSlice update for entire node**:
   ```go
   func (rsm *ResourceSliceManager) UpdateAfterPartition(
       node string,
       gpus []*GPU,
       oldProfile, newProfile PartitionProfile,
   ) error {
       // Get current ResourceSlice for node
       slice := rsm.getResourceSlice(node)

       // Remove ALL old device entries for this node
       for _, gpu := range gpus {
           rsm.removeDevice(slice, gpu.ID)
       }

       // Add new partition entries for ALL GPUs
       for _, gpu := range gpus {
           for i := 0; i < newProfile.PartitionCount(); i++ {
               partition := gpu.CreatePartition(i, newProfile)
               rsm.addDevice(slice, partition)
           }
       }

       // Update node-level metadata
       slice.Metadata.Annotations["amd.com/partition-profile"] = newProfile.String()
       slice.Metadata.Annotations["amd.com/nps-mode"] = string(newProfile.Memory)

       // Atomically update via K8s API
       return rsm.client.Update(context.TODO(), slice)
   }
   ```

4. **Implement allocation tracking**:
   ```go
   type AllocationTracker struct {
       mu sync.RWMutex
       nodeAllocations map[string]*NodeAllocationStatus
   }

   func (at *AllocationTracker) IsNodeIdle(nodeName string) bool {
       at.mu.RLock()
       defer at.mu.RUnlock()

       status, exists := at.nodeAllocations[nodeName]
       if !exists {
           return true // Not allocated
       }

       return status.AllGPUsIdle
   }

   func (at *AllocationTracker) IsGPUIdle(gpuID string) bool {
       at.mu.RLock()
       defer at.mu.RUnlock()

       // Find GPU across all nodes
       for _, nodeStatus := range at.nodeAllocations {
           if gpuStatus, exists := nodeStatus.GPUs[gpuID]; exists {
               return gpuStatus.IsIdle
           }
       }

       return true // GPU not found, assume idle
   }
   ```

5. **Implement un-partition manager**:
   ```go
   type UnpartitionManager struct {
       pc *PartitionController
       timers map[string]*time.Timer // node -> timer
       gracePeriod time.Duration
   }

   func (um *UnpartitionManager) ScheduleUnpartition(node string) {
       // Cancel existing timer if any
       if timer, exists := um.timers[node]; exists {
           timer.Stop()
       }

       // Schedule new timer
       timer := time.AfterFunc(um.gracePeriod, func() {
           um.executeUnpartition(node)
       })
       um.timers[node] = timer
   }

   func (um *UnpartitionManager) CancelUnpartition(node string) {
       if timer, exists := um.timers[node]; exists {
           timer.Stop()
           delete(um.timers, node)
       }
   }

   func (um *UnpartitionManager) executeUnpartition(node string) {
       if err := um.pc.unpartitionNode(node); err != nil {
           // Log error, emit event
           log.Errorf("Failed to un-partition node %s: %v", node, err)
       }
   }
   ```

**Deliverables**:
- Partition controller implementation
- Node partition execution logic (wrapping amd-smi)
- ResourceSlice update mechanism for entire node
- Allocation tracker with node-level idle detection
- Un-partition manager with grace period
- Unit tests for each component

---

### Phase 4: Integration with GPU Operator (1-2 weeks)
**Goal**: Deploy DRA driver as part of GPU Operator

**Tasks**:
1. **Update GPU Operator Helm Chart**:
   - Add DRA driver DaemonSet deployment
   - Configure RBAC for ResourceSlice access
   - Add ConfigMap for partition compatibility matrix
   - Add feature gate for DRA auto-partition

2. **Update DeviceConfig CRD**:
   ```yaml
   apiVersion: amd.com/v1alpha1
   kind: DeviceConfig
   spec:
     draDriver:
       enable: true
       autoPartition: true
       partitionPolicy:
         allowedConversions:
           - from: SPX_NPS1
             to: [CPX_NPS1, DPX_NPS1, QPX_NPS1]
           - from: SPX_NPS2
             to: [CPX_NPS2]
         idleTimeout: 300 # Only partition if idle for 5 min
         unpartitionGracePeriod: 300 # Wait 5 min before un-partitioning
         defaultProfile: SPX_NPS1 # Default state to return to
   ```

3. **Reuse DCM Partition Functions**:
   - Extract partition logic into shared library
   - Both DCM and DRA driver import library
   - Consistent amd-smi command execution

4. **Update Node Labeller**:
   - Detect partition changes via DRA driver
   - Add labels: `amd.com/gpu-partition-profile=CPX_NPS1`
   - Update counts: `amd.com/gpu-partitions=16` (for 2 GPUs × 8 partitions)

**Deliverables**:
- Updated Helm chart with DRA driver
- Updated DeviceConfig CRD
- Shared partition library
- Node labeller updates
- Integration tests

---

### Phase 5: Testing & Validation (2 weeks)
**Goal**: Validate auto-partitioning end-to-end with structured parameters and NodeListAndPrepare

**Test Cases**:

**Critical Test: Verify Scheduler Calls NodeListAndPrepare**:

0. **Structured Parameters Flow Validation**:
   - Create ResourceClaim with structured parameters (NOT CEL):
     ```yaml
     config:
       - requests:
           - partitionProfile: CPX_NPS1
     ```
   - Mock scheduler calling `NodeListAndPrepare()` for multiple nodes
   - Node A: Has exact CPX_NPS1 match → verify immediate return
   - Node B: Has SPX_NPS1, idle → verify partition happens IN this call
   - Node C: Has SPX_NPS1, busy → verify error returned
   - Verify scheduler receives correct responses from each node
   - **This is THE critical test** - proves auto-partition works

**Compute-Only Partition Tests** (No Driver Reload):

1. **Single GPU Compute Partition (No Disruption)**:
   - Node with 1 GPU in SPX_NPS1 (idle), ResourceSlice has `supportedPartitionProfiles: ["SPX_NPS1", "CPX_NPS1", ...]`
   - Deploy pod with CPX_NPS1 ResourceClaim (structured parameters)
   - Scheduler calls `NodeListAndPrepare()` → driver partitions GPU synchronously
   - Verify GPU auto-partitions during scheduling
   - Verify ResourceSlices updated BEFORE driver returns to scheduler
   - Verify pod gets allocated partition

2. **Single GPU Compute Partition with Other GPUs Busy**:
   - Node with 2 GPUs: GPU0=CPX_NPS1 (has allocations), GPU1=SPX_NPS1 (idle)
   - Deploy pod requesting DPX_NPS1
   - Verify ONLY GPU1 partitions (SPX → DPX)
   - Verify NO driver reload
   - Verify GPU0 allocations UNAFFECTED
   - Verify ResourceSlices: GPU0 unchanged, GPU1 updated
   - Verify pod gets allocated from GPU1

3. **Mixed Compute Modes on Same Node**:
   - Start: All GPUs in SPX_NPS1
   - Deploy 3 pods requesting: CPX_NPS1, DPX_NPS1, QPX_NPS1
   - Verify final state: GPU0=CPX, GPU1=DPX, GPU2=QPX (all NPS1)
   - Verify no driver reloads occurred
   - Verify all pods allocated successfully

4. **Per-GPU Un-Partition (Compute-Only)**:
   - Node: GPU0=CPX_NPS1 (allocated), GPU1=CPX_NPS1 (idle)
   - GPU1 idle for grace period (5 min)
   - Verify GPU1 un-partitions to SPX_NPS1
   - Verify NO driver reload
   - Verify GPU0 stays CPX_NPS1 with active allocations
   - Verify ResourceSlices: GPU0 unchanged, GPU1 updated to SPX

**NPS Partition Tests** (With Driver Reload):

5. **Node-Wide NPS Partition (Multiple GPUs)**:
   - Node with 2 GPUs in SPX_NPS1 (all idle)
   - Deploy pod with CPX_NPS4 ResourceClaim
   - Verify BOTH GPUs partition (compute: SPX→CPX, memory: NPS1→NPS4)
   - Verify driver reload occurs
   - Verify ResourceSlices updated for both GPUs
   - Verify pod gets allocated partition from one GPU

6. **NPS Change Blocked by Busy GPU**:
   - Node: GPU0=SPX_NPS1 (has allocation), GPU1=SPX_NPS1 (idle)
   - Deploy pod requesting CPX_NPS4
   - Verify partition NOT triggered (NPS change requires all idle)
   - Verify pod remains PENDING
   - Verify event emitted: AwaitingNodeIdle

7. **NPS Change After Node Becomes Idle**:
   - Continue from test #6
   - Release allocation from GPU0
   - Verify both GPUs now idle
   - Verify automatic re-partition to CPX_NPS4
   - Verify driver reload occurs
   - Verify pending pod gets allocated

4. **Partition Compatibility**:
   - Request SPX_NPS4 (invalid: NPS4 requires CPX)
   - Verify request rejected with clear error

5. **Idle Detection - Partial Node Usage**:
   - Node with 2 GPUs, GPU0 has allocated partition
   - New pod requests different partition (e.g., DPX_NPS1)
   - Verify partition **not** triggered (node not idle)
   - Verify pod remains pending

6. **Un-Partitioning - Grace Period**:
   - Node partitioned to CPX_NPS1
   - Pod releases allocation (last allocation on node)
   - Verify grace period timer starts (5 min)
   - Wait for timer expiration
   - Verify node un-partitions to SPX_NPS1
   - Verify ResourceSlices updated

7. **Un-Partitioning - Cancelled by New Allocation**:
   - Node partitioned to CPX_NPS1
   - Pod releases allocation (last allocation)
   - Grace period starts
   - New pod scheduled before grace period expires
   - Verify un-partition cancelled
   - Verify node stays in CPX_NPS1

8. **Multi-Node Scheduling**:
   - Node A: SPX GPUs (idle)
   - Node B: CPX GPUs (has available partitions)
   - Pod requests CPX
   - Verify scheduler picks Node B (no partition needed)

9. **Concurrent Requests**:
   - Two pods request CPX simultaneously
   - Only one SPX node available (idle)
   - Verify only one partition operation
   - Verify both pods eventually get partitions

10. **Rollback on Failure**:
    - Trigger partition
    - Simulate amd-smi failure
    - Verify node state marked as error
    - Verify ResourceSlice not updated
    - Verify operator can retry

11. **Driver Reload Impact**:
    - Verify driver reload happens
    - Verify no other workloads disrupted (node idle)
    - Verify readiness checks pass before allocation

12. **Partition Mode Change (Requires Reset)**:
    - Node in CPX_NPS1
    - Pod requests DPX_NPS2
    - Verify un-partition to SPX first
    - Verify re-partition to DPX_NPS2
    - Verify ResourceSlices updated twice

**Deliverables**:
- E2E test suite
- Performance benchmarks (partition latency)
- Failure scenario tests
- Documentation with test results

---

### Phase 6: Documentation & Rollout (1 week)
**Goal**: Production-ready documentation and deployment guide

**Tasks**:
1. **User Documentation**:
   - How to enable DRA auto-partition in GPU Operator
   - ResourceClaim examples for different partitions
   - Troubleshooting guide
   - Un-partitioning behavior and grace period tuning

2. **Operator Documentation**:
   - Architecture diagram
   - Partition workflow sequence
   - Un-partition workflow sequence
   - Monitoring and metrics
   - Known limitations

3. **Migration Guide**:
   - Moving from DCM manual partitioning to DRA auto-partition
   - Backward compatibility considerations
   - Gradual rollout strategy

**Deliverables**:
- User guide
- Operator guide
- Migration documentation
- Release notes

---

## Critical Files to Modify/Create

### Existing GPU Operator Files

1. **DCM Partition Logic** (to extract and reuse):
   - `internal/configmanager/configmanager.go` - partition execution
   - `tests/e2e/dcm_e2e_test.go` - test patterns to replicate

2. **DeviceConfig CRD**:
   - `api/v1alpha1/deviceconfig_types.go` - add DRA spec

3. **Controller**:
   - `internal/controllers/deviceconfig_controller.go` - add DRA driver deployment

4. **Helm Chart**:
   - `deployments/gpu-operator/templates/` - add DRA DaemonSet

### New Files to Create

1. **DRA Driver Extensions** (fork of ROCm/k8s-gpu-dra-driver):
   ```
   pkg/
     controller/
       partition_controller.go      # Main auto-partition logic
       allocation_tracker.go         # Track GPU allocations (node-level)
       resource_slice_manager.go     # Update ResourceSlices
       unpartition_manager.go        # Un-partition orchestration
       node_state_tracker.go         # Track node partition state
     gpu/
       partition.go                  # Partition execution (wraps amd-smi)
       compatibility.go              # Partition conversion matrix
     config/
       partition_policy.go           # Policy configuration
   ```

2. **Shared Library** (for DCM and DRA):
   ```
   internal/partition/
     executor.go                     # amd-smi wrapper
     validator.go                    # Partition validation
   ```

3. **E2E Tests**:
   ```
   tests/e2e/
     dra_auto_partition_test.go     # Auto-partition test cases
     dra_unpartition_test.go        # Un-partition test cases
   ```

---

## Verification Plan

### How to Test End-to-End

1. **Deploy GPU Operator with DRA**:
   ```bash
   helm install amd-gpu-operator ./deployments/gpu-operator \
     --set draDriver.enabled=true \
     --set draDriver.autoPartition=true \
     --set draDriver.unpartitionGracePeriod=300
   ```

2. **Verify ResourceSlices Published**:
   ```bash
   kubectl get resourceslices -o yaml
   # Should show SPX_NPS1 GPUs
   ```

3. **Deploy Pod Requesting CPX** (using Structured Parameters):
   ```yaml
   apiVersion: v1
   kind: Pod
   metadata:
     name: test-cpx
   spec:
     containers:
       - name: workload
         image: rocm/pytorch
         resources:
           claims:
             - name: gpu
     resourceClaims:
       - name: gpu
         resourceClaimTemplateName: cpx-gpu-claim
   ---
   apiVersion: resource.k8s.io/v1
   kind: ResourceClaimTemplate
   metadata:
     name: cpx-gpu-claim
   spec:
     spec:
       devices:
         requests:
           - name: gpu
             deviceClassName: gpu.amd.com
         config:  # CRITICAL: Use structured parameters, NOT CEL selectors!
           - requests:
               - partitionProfile: CPX_NPS1
                 model: MI300X
                 memory: "12Gi"
   ```

   **Why structured parameters?**
   - ❌ CEL selectors: Scheduler filters nodes → only calls driver if match exists → cannot auto-partition
   - ✅ Structured parameters: Scheduler calls driver for ALL nodes → driver can partition on-the-fly

4. **Monitor Partition Operation**:
   ```bash
   # Watch DRA driver logs
   kubectl logs -f <dra-driver-pod> -c dra-driver

   # Watch events
   kubectl get events --watch | grep partition

   # Check ResourceSlice updates
   kubectl get resourceslices -o yaml --watch
   ```

5. **Verify Pod Allocation**:
   ```bash
   kubectl get pod test-cpx -o yaml
   # Check status.resourceClaimStatuses[0].allocation

   # Verify GPU partition allocated
   kubectl exec test-cpx -- rocm-smi
   # Should show CPX partition
   ```

6. **Verify GPU State on Node**:
   ```bash
   # SSH to node
   amd-smi list
   # Should show CPX mode with 8 partitions per GPU

   # Check node labels
   kubectl get node <node> -o yaml | grep partition
   # Should show CPX_NPS1 labels
   ```

7. **Test Un-Partitioning**:
   ```bash
   # Delete pod
   kubectl delete pod test-cpx

   # Wait for grace period (5 min)
   sleep 300

   # Verify node un-partitioned
   kubectl get resourceslices -o yaml
   # Should show SPX_NPS1 GPUs again

   # Verify on node
   amd-smi list
   # Should show SPX mode
   ```

---

## Known Limitations & Edge Cases

### 1. Driver Reload Disruption
**Issue**: Driver reload terminates all GPU workloads
**Mitigation**: Only partition nodes with ALL GPUs idle (no allocated partitions)
**Edge Case**: Pod scheduled right before partition starts
**Solution**:
- Atomic check-and-partition with locking
- Mark node as "partitioning" in ResourceSlice
- Scheduler skips nodes in transient state

### 2. Partition Not Persistent
**Issue**: GPU reverts to SPX on driver reload/reboot
**Impact**: Node reboot or driver crash loses partition config
**Mitigation**:
- DRA driver re-partitions on startup if needed
- DCM systemd integration can apply default partition at boot
**Future**: AMD working on persistent partition config

### 3. Incompatible Partition Requests
**Issue**: NPS4 requires CPX, user requests SPX_NPS4
**Solution**: Partition compatibility validator rejects invalid combos
**User Experience**: Clear error message in ResourceClaim status

### 4. Concurrent Partition Requests on Same Node
**Issue**: Two pods request CPX, target same SPX node
**Solution**:
- First request triggers partition (entire node)
- Second request waits for partition completion
- Both pods eventually allocated from same node (multiple partitions)

### 5. Partition Failure Handling
**Issue**: amd-smi fails mid-partition
**Solution**:
- Mark node as "error" state
- Emit Kubernetes event with failure details
- Retry logic with exponential backoff
- Admin notification via alerts

### 6. ResourceSlice Update Race
**Issue**: Scheduler reads stale ResourceSlice during partition
**Solution**:
- Optimistic locking with ResourceVersion
- Retry allocation if ResourceSlice changed
- Use "partitioning" transient state to block allocations

### 7. NPS Conflicts on Same Node
**Issue**: Pod A wants NPS1, Pod B wants NPS4 on same node
**Solution**:
- First pod sets node NPS mode
- Second pod with conflicting NPS **cannot** be scheduled on that node
- Scheduler finds different node or pod remains pending

### 8. Un-Partition Thrashing
**Issue**: Pod scheduled, un-partition scheduled, new pod arrives, cancel un-partition, repeat
**Solution**:
- Tunable grace period (default 5 min)
- Monitor partition frequency metrics
- Consider node affinity for partition profiles

### 9. Heterogeneous GPU Types on Same Node
**Issue**: Different GPU models may have different partition capabilities
**Solution**:
- Track capabilities per GPU model
- Validate partition request against GPU capabilities
- Reject if incompatible

---

## Monitoring & Observability

### Metrics to Add
```
# Partition operations
amd_gpu_partitions_total{node, from_profile, to_profile, result}
amd_gpu_partition_duration_seconds{node, profile}

# Un-partition operations
amd_gpu_unpartitions_total{node, profile, result}
amd_gpu_unpartition_cancelled_total{node}

# Allocation tracking
amd_gpu_allocations_total{node, partition_profile}
amd_gpu_idle_nodes_total{}
amd_gpu_idle_gpus_total{node}

# ResourceSlice updates
amd_gpu_resourceslice_updates_total{node, reason}

# Grace period tracking
amd_gpu_unpartition_timers_active{node}
```

### Events to Emit
```
# Partition events
- PartitionStarted: Node partition operation initiated
- PartitionCompleted: Node partition successful
- PartitionFailed: Node partition failed (with reason)

# Un-partition events
- UnpartitionScheduled: Un-partition grace period started
- UnpartitionCancelled: Un-partition cancelled (new allocation)
- UnpartitionCompleted: Node returned to default state
- UnpartitionFailed: Un-partition failed (with reason)

# ResourceSlice events
- ResourceSliceUpdated: ResourceSlice updated with new partitions

# Allocation events
- AllocationPending: No suitable node, waiting for partition
- AllocationBlocked: Node has conflicting NPS mode
```

### Logs to Add
```
# Partition logs
- "Starting partition for node %s: %s -> %s"
- "Partition completed for node %s in %v"
- "Updated ResourceSlice for node %s: removed %d devices, added %d"
- "Skipping partition for node %s: not idle (allocations: %d)"

# Un-partition logs
- "Scheduling un-partition for node %s (grace period: %v)"
- "Cancelling un-partition for node %s: new allocation detected"
- "Un-partitioning node %s: %s -> SPX_NPS1"
- "Un-partition completed for node %s in %v"

# NPS conflict logs
- "Cannot partition node %s to NPS4: current NPS1 with active allocations"
- "Rejecting allocation: node %s has incompatible NPS mode (current: %s, requested: %s)"
```

---

## Timeline Estimate

| Phase | Duration | Cumulative |
|-------|----------|------------|
| Phase 1: Evaluate AMD DRA Driver | 1 week | 1 week |
| Phase 2: Design Auto-Partition | 1 week | 2 weeks |
| Phase 3: Implement Controller | 2-3 weeks | 4-5 weeks |
| Phase 4: Integrate with GPU Operator | 1-2 weeks | 5-7 weeks |
| Phase 5: Testing & Validation | 2 weeks | 7-9 weeks |
| Phase 6: Documentation & Rollout | 1 week | 8-10 weeks |

**Total: 8-10 weeks** (2-2.5 months)

---

## Architecture Summary

### Two Partition Paths

The DRA driver implements **two distinct partition paths** based on whether NPS changes:

#### Path 1: Compute-Only Partition (Fast, Per-GPU)

**Characteristics**:
- ✅ No driver reload
- ✅ No disruption to other GPUs
- ✅ Immediate effect
- ✅ Can run while other GPUs are busy
- ✅ Per-GPU operation

**Workflow**:
```
Pod requests CPX_NPS1 → Check node
└─ Current: SPX_NPS1 (GPU0 busy, GPU1 idle)
   └─ NPS matches (NPS1 == NPS1) ✓
      └─ Find idle GPU: GPU1 ✓
         └─ Partition GPU1: SPX → CPX (no reload)
            └─ Update ResourceSlices (GPU1 only)
               └─ Allocate to pod

Timeline: ~1 second (immediate)
Other GPUs: Unaffected
```

**Use Cases**:
- High-frequency partition changes
- Heterogeneous workload mix (different compute needs)
- Multi-tenant with varying compute requirements
- Minimal disruption requirements

#### Path 2: NPS Partition (Slow, Node-Wide)

**Characteristics**:
- ❌ Requires driver reload
- ❌ Terminates all GPU workloads on node
- ⏱️ Takes 10-30 seconds
- ❌ Entire node must be idle
- ⚠️ Node-wide operation

**Workflow**:
```
Pod requests CPX_NPS4 → Check node
└─ Current: CPX_NPS1 (all GPUs must be idle)
   └─ NPS change needed (NPS1 → NPS4) ⚠️
      └─ Check all GPUs idle: YES ✓
         └─ Partition entire node:
            ├─ Set compute for all GPUs
            ├─ Set memory (NPS4)
            └─ Driver reload (disrupts all)
               └─ Update ResourceSlices (all GPUs)
                  └─ Allocate to pod

Timeline: ~15-30 seconds (driver reload)
Other GPUs: All affected (driver reload)
```

**Use Cases**:
- LLM inference (NPS4 for memory bandwidth)
- Large model training (NUMA locality)
- Less frequent partition changes
- Batch workload scheduling

### Decision Tree

```
New allocation request arrives
│
├─ Exact match exists?
│  └─ YES → Allocate immediately ✅
│
└─ NO → Check for partition opportunity
   │
   ├─ NPS change needed?
   │  │
   │  ├─ NO (compute-only) → Path 1: Fast Per-GPU
   │  │  │
   │  │  ├─ Find idle GPU with different compute mode
   │  │  ├─ Partition ONLY that GPU (no reload)
   │  │  └─ Allocate ✅
   │  │
   │  └─ YES (NPS change) → Path 2: Slow Node-Wide
   │     │
   │     ├─ Check if ENTIRE node is idle
   │     │  │
   │     │  ├─ YES → Partition entire node (with reload)
   │     │  │        └─ Allocate ✅
   │     │  │
   │     │  └─ NO → Pod remains PENDING ⏳
   │     │          Emit: AwaitingNodeIdle
   │     │
   │     └─ When node becomes idle:
   │        └─ Trigger partition automatically
   │           └─ Allocate ✅
```

### Performance Implications

| Scenario | Partition Path | Latency | Disruption | Frequency |
|----------|---------------|---------|------------|-----------|
| CPX_NPS1 → DPX_NPS1 | Path 1 | ~1s | None | High |
| SPX_NPS1 → CPX_NPS1 | Path 1 | ~1s | None | High |
| CPX_NPS1 → CPX_NPS4 | Path 2 | ~20s | All GPUs | Low |
| SPX_NPS1 → DPX_NPS4 | Path 2 | ~25s | All GPUs | Low |

### Resource Slices Management

**Compute-Only Partition** (Per-GPU):
```yaml
# Before: GPU0=SPX_NPS1, GPU1=SPX_NPS1
devices:
  - name: gpu-0  # Unchanged
  - name: gpu-1  # Updated

# After: GPU0=SPX_NPS1 (unchanged), GPU1=CPX_NPS1 (partitioned)
devices:
  - name: gpu-0  # Same
  - name: gpu-1-partition-0  # New
  - name: gpu-1-partition-1  # New
  # ... 6 more partitions for gpu-1
```

**NPS Partition** (Node-Wide):
```yaml
# Before: All GPUs SPX_NPS1
devices:
  - name: gpu-0
  - name: gpu-1

# After: All GPUs CPX_NPS4 (all updated)
devices:
  - name: gpu-0-partition-0  # New, NPS domain 0
  - name: gpu-0-partition-1  # New, NPS domain 1
  # ... 6 more for gpu-0
  - name: gpu-1-partition-0  # New, NPS domain 4
  # ... 7 more for gpu-1
```

---

## Success Criteria

**Compute-Only Partitioning** (Path 1 - Per-GPU, No Reload):
1. ✅ Pod requesting `cpx/nps1` automatically triggers partition of single idle `spx/nps1` GPU
2. ✅ Compute-only partition completes in < 5 seconds (no driver reload)
3. ✅ **Zero disruption** to other GPUs on same node during partition
4. ✅ ResourceSlices correctly updated for partitioned GPU only (others unchanged)
5. ✅ Pod allocated one of the new CPX partitions
6. ✅ Mixed compute modes supported on same node (GPU0=CPX, GPU1=DPX, GPU2=SPX)

**NPS Partitioning** (Path 2 - Node-Wide, With Reload):
7. ✅ Pod requesting `cpx/nps4` triggers partition when entire node idle
8. ✅ NPS partition completes in < 30 seconds (includes driver reload)
9. ✅ All GPUs on node partitioned atomically (compute + memory)
10. ✅ ResourceSlices correctly updated for all GPUs on node
11. ✅ NPS conflicts properly handled (reject if node not fully idle)
12. ✅ Pod remains pending if node has active allocations, allocates when idle

**Un-Partitioning**:
13. ✅ Compute-only un-partition (per-GPU) completes without driver reload or disruption
14. ✅ NPS un-partition (node-wide) returns all GPUs to default state with reload
15. ✅ Grace period timer works correctly (cancelled by new allocation)
16. ✅ Admin-forced un-partition works with safety mechanisms and override

**Reliability & Performance**:
17. ✅ Failed partitions properly handled with retries and error reporting
18. ✅ Concurrent partition requests handled safely (locking, optimistic updates)
19. ✅ Multiple pods can share partitioned GPUs (8 partitions per CPX GPU)
20. ✅ Performance: < 5s for compute-only, < 30s for NPS changes

**Testing & Documentation**:
21. ✅ E2E tests validate all scenarios (compute, NPS, mixed, un-partition)
22. ✅ Performance benchmarks demonstrate latency targets
23. ✅ Documentation clearly explains both partition paths and when each applies
24. ✅ Monitoring metrics track both partition types separately

---

## Alternative Considered: Device Plugin + Admission Webhook

**Approach**: Admission webhook auto-labels nodes based on pod requests

**Pros**:
- Simpler implementation
- Reuses existing DCM completely

**Cons**:
- Hacky workaround, not standard K8s API
- Race conditions with multiple pods
- No attribute-based matching
- Manual cleanup of labels needed
- No un-partitioning logic

**Verdict**: **Rejected** - DRA is the proper Kubernetes-native solution and is now GA in K8s 1.34

---

## References

### Documentation
- [AMD GPU DRA Driver](https://github.com/ROCm/k8s-gpu-dra-driver)
- [AMD MI300X GPU Partitioning Overview](https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/mi300x/overview.html)
- [K8s Dynamic Resource Allocation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [AMD GPU Operator DCM Guide](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/dcm/applying-partition-profiles.html)

### Blog Posts
- [Reimagining GPU Allocation with DRA](https://rocm.blogs.amd.com/software-tools-optimization/dra-gpu/README.html)
- [GPU Partitioning Made Easy with GPU Operator](https://rocm.blogs.amd.com/software-tools-optimization/gpu-operator-partitioning/README.html)
- [Deep Dive into MI300 Partition Modes](https://rocm.blogs.amd.com/software-tools-optimization/compute-memory-modes/README.html)

### Research
- [Exploring AMD GPU Scheduling](https://dl.acm.org/doi/fullHtml/10.1145/3453417.3453432)
