# Key Insights: AMD GPU Auto-Partitioning with DRA

## Critical Architectural Requirement: Structured Parameters

**THE KEY QUESTION**: If a node only has `SPX_NPS1` GPUs in its ResourceSlice, how does a pod requesting `CPX_NPS1` even reach the DRA driver to trigger auto-partitioning?

**THE ANSWER**: Must use **Structured Parameters** mode, NOT CEL-based matching.

### Why CEL-Based Matching FAILS ❌

```yaml
selectors:
  - cel:
      expression: device.attributes["partitionProfile"] == "CPX_NPS1"
```

- Scheduler evaluates CEL **directly against ResourceSlice**
- ResourceSlice only has `SPX_NPS1` → CEL fails → Node **rejected**
- DRA driver **NEVER CALLED** → Auto-partition **IMPOSSIBLE**

### Why Structured Parameters WORKS ✅

```yaml
config:
  - requests:
      - partitionProfile: CPX_NPS1
```

- Scheduler calls `NodeListAndPrepare()` for **EACH candidate node**
- DRA driver checks `supportedPartitionProfiles` in ResourceSlice
- If supported + GPU idle → **partition synchronously during call**
- Auto-partition **SUCCESS**

**See [dra-structured-parameters-architecture.md](dra-structured-parameters-architecture.md) for detailed explanation.**

---

## Critical Discovery: Driver Reload Only for NPS Changes

### The Game Changer

**AMD GPU partitioning has TWO fundamentally different behaviors**:

1. **Compute Partition** (SPX/CPX/DPX/QPX) → **NO driver reload needed**
2. **Memory Partition** (NPS1/NPS2/NPS4) → **Requires driver reload**

This enables **per-GPU partitioning** for compute changes with zero disruption to other GPUs!

---

## Architecture: Two Partition Paths

### Path 1: Fast Compute-Only Partition ⚡

**Characteristics**:
- ✅ Per-GPU operation
- ✅ NO driver reload
- ✅ Immediate effect (~1 second)
- ✅ Zero disruption to other GPUs
- ✅ Can run while other GPUs are busy

**Example**:
```bash
# Partition GPU 1 from SPX to CPX (GPU 0 stays unchanged)
amd-smi set --gpu 1 --compute-partition CPX
# Done! No driver reload. GPU 0 workloads continue running.
```

**Use Cases**:
- High-frequency partition changes
- Heterogeneous workload scheduling
- Minimal disruption requirements
- Multi-tenant environments

---

### Path 2: Slow NPS Partition 🐢

**Characteristics**:
- ⚠️ Node-wide operation
- ❌ Requires driver reload
- ⏱️ Takes 15-30 seconds
- ❌ Terminates all GPU workloads on node
- ❌ Entire node must be idle

**Example**:
```bash
# Change from NPS1 to NPS4 (affects ALL GPUs on node)
amd-smi set --gpu all --memory-partition NPS4
amd-smi reset -r  # Driver reload - kills all workloads
```

**Use Cases**:
- LLM inference (need NPS4 for memory bandwidth)
- Large model training (NUMA locality)
- Less frequent, planned partition changes

---

## Key Implications

### 1. Mixed Compute Modes Allowed

**Same node can have**:
- GPU 0: CPX (8 partitions)
- GPU 1: DPX (2 partitions)
- GPU 2: SPX (1 whole GPU)

All with the same NPS mode (e.g., NPS1).

### 2. Re-Partitioning is Much More Flexible

**Before (incorrect understanding)**:
- Any partition requires entire node idle
- Driver reload always needed
- Very conservative

**After (correct understanding)**:
- Compute-only: Can re-partition individual idle GPUs immediately
- NPS change: Still requires entire node idle
- Much more responsive!

### 3. Un-Partitioning Can Be Per-GPU

**Compute-only un-partition**:
```
GPU 0: CPX (has allocations) → stays CPX
GPU 1: CPX (idle for 5 min) → un-partitions to SPX
No driver reload, GPU 0 unaffected
```

**NPS un-partition**:
```
All GPUs idle → un-partition entire node to SPX_NPS1
Driver reload required
```

---

## Decision Matrix

| Current State | Requested | NPS Change? | Scope | Reload? | Requires |
|---------------|-----------|-------------|-------|---------|----------|
| SPX_NPS1 | CPX_NPS1 | ❌ No | Per-GPU | ❌ No | Single GPU idle |
| CPX_NPS1 | DPX_NPS1 | ❌ No | Per-GPU | ❌ No | Single GPU idle |
| SPX_NPS1 | CPX_NPS4 | ✅ Yes | Node-wide | ✅ Yes | **All GPUs idle** |
| CPX_NPS1 | CPX_NPS4 | ✅ Yes | Node-wide | ✅ Yes | **All GPUs idle** |

---

## Performance Impact

| Operation | Latency | Disruption | Can Run While Others Busy? |
|-----------|---------|------------|----------------------------|
| Compute partition | ~1-5s | None | ✅ Yes |
| NPS partition | ~15-30s | All GPUs on node | ❌ No |

---

## ResourceSlice Updates

### Compute-Only (Per-GPU)

**Before**:
```yaml
devices:
  - name: gpu-0  # SPX
  - name: gpu-1  # SPX
```

**After** (only GPU 1 changed):
```yaml
devices:
  - name: gpu-0  # SPX (unchanged!)
  - name: gpu-1-partition-0  # CPX partition
  - name: gpu-1-partition-1  # CPX partition
  # ... 6 more partitions for gpu-1 only
```

### NPS Change (Node-Wide)

**Before**:
```yaml
devices:
  - name: gpu-0  # SPX_NPS1
  - name: gpu-1  # SPX_NPS1
```

**After** (all GPUs changed):
```yaml
devices:
  - name: gpu-0-partition-0  # CPX_NPS4
  # ... 7 more for gpu-0
  - name: gpu-1-partition-0  # CPX_NPS4
  # ... 7 more for gpu-1
```

---

## Implementation Priorities

### High Priority (Path 1 - Most Impact)

1. ✅ Implement per-GPU compute partition
2. ✅ Update ResourceSlices for single GPU only
3. ✅ No driver reload logic for compute changes
4. ✅ Mixed compute mode support

### Medium Priority (Path 2 - Less Frequent)

5. ⚠️ Implement node-wide NPS partition
6. ⚠️ Driver reload orchestration
7. ⚠️ Ensure entire node idle check

### Both Paths

8. 🔄 Re-partitioning logic (detect which path to use)
9. 🔄 Un-partitioning (per-GPU or node-wide)
10. 🔄 Grace period management

---

## Testing Strategy

### Critical Test: Verify No Disruption

```
Setup: Node with 2 GPUs, both SPX_NPS1
Step 1: Deploy pod-A requesting CPX_NPS1 → GPU 0 partitions
Step 2: Deploy long-running workload on GPU 0
Step 3: Deploy pod-B requesting DPX_NPS1 → GPU 1 should partition
VERIFY: GPU 0 workload NOT disrupted (no driver reload)
VERIFY: GPU 1 partitions successfully
VERIFY: Both pods running simultaneously
```

### Performance Test

```
Measure time:
- Compute partition (SPX→CPX): Should be < 5 seconds
- NPS partition (NPS1→NPS4): Allowed up to 30 seconds

Track metrics:
- Partition latency by type (compute vs NPS)
- Disruption events (should be 0 for compute-only)
```

---

## Monitoring Metrics

```prometheus
# Separate metrics for each path
amd_gpu_partition_compute_total{node, from, to, result}
amd_gpu_partition_nps_total{node, from, to, result}

# Latency by type
amd_gpu_partition_duration_seconds{node, type="compute"}
amd_gpu_partition_duration_seconds{node, type="nps"}

# Disruption tracking
amd_gpu_driver_reloads_total{node, reason}
```

---

## Summary

**The key breakthrough**: Separating compute and NPS partition paths enables:

1. **Fast, non-disruptive per-GPU partitioning** for majority use case
2. **Slower, coordinated node-wide partitioning** when truly needed (NPS)
3. **Mixed compute modes** on same node for heterogeneous workloads
4. **Much better utilization** (don't wait for entire node to be idle for compute changes)

This dramatically improves the user experience and system responsiveness!
