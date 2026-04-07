# DRA Auto-Partitioning: Why Structured Parameters are Required

**Date**: 2026-02-27
**Critical Architectural Clarification**

## The Problem

**Question**: If a node only has `SPX_NPS1` GPUs in its ResourceSlice, how does a pod requesting `CPX_NPS1` even reach the DRA driver to trigger auto-partitioning?

**Answer**: It doesn't, if you use CEL-based matching. You **MUST** use structured parameters instead.

---

## Two DRA Modes

### Mode 1: CEL-Based Matching ❌ (Does NOT work for auto-partitioning)

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
              expression: device.attributes["partitionProfile"] == "CPX_NPS1"
```

**How scheduler works with CEL**:
1. Scheduler reads ResourceSlice from node
2. Scheduler **evaluates CEL expression** against each device in ResourceSlice
3. If CEL expression matches → node is candidate
4. If CEL expression fails → **node is filtered out immediately**
5. Scheduler only calls DRA driver for nodes that passed CEL filter

**Why auto-partition fails**:
```
Node has: SPX_NPS1 devices in ResourceSlice
Pod requests: device.attributes["partitionProfile"] == "CPX_NPS1"

Scheduler evaluation:
  - Check device 0: SPX_NPS1 == CPX_NPS1? FALSE
  - Check device 1: SPX_NPS1 == CPX_NPS1? FALSE
  - All devices fail CEL → NODE REJECTED

Result: Scheduler NEVER calls DRA driver for this node
        DRA driver has NO opportunity to partition
        Pod stays PENDING forever ❌
```

---

### Mode 2: Structured Parameters ✅ (Works for auto-partitioning)

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
spec:
  devices:
    requests:
      - name: gpu
        deviceClassName: gpu.amd.com
    config:  # Structured parameters - NOT evaluated by scheduler!
      - requests:
          - partitionProfile: CPX_NPS1
            model: MI300X
            memory: "12Gi"
```

**How scheduler works with structured parameters**:
1. Scheduler reads structured parameters from ResourceClaim
2. Scheduler does **NOT** evaluate anything against ResourceSlice
3. For **EACH candidate node**, scheduler calls:
   `NodeListAndPrepare(claim, nodeName)` → DRA driver gRPC method
4. Driver responds: "yes I can satisfy this" or "no I cannot"
5. Scheduler collects responses from all nodes and picks best one

**Why auto-partition works**:
```
Node has: SPX_NPS1 devices in ResourceSlice
Pod requests: partitionProfile=CPX_NPS1 (structured parameter)

Scheduler calls DRA driver:
  NodeListAndPrepare(claim, node-123)

DRA driver logic:
  1. Parse structured param: requested = CPX_NPS1
  2. Check ResourceSlice for node-123:
     - currentPartitionProfile: SPX_NPS1
     - supportedPartitionProfiles: [SPX_NPS1, CPX_NPS1, DPX_NPS1, ...]
  3. Is CPX_NPS1 in supportedProfiles? YES ✓
  4. Is GPU idle? YES ✓
  5. PARTITION NOW (synchronously):
     - amd-smi set --gpu 0 --compute-partition CPX
     - Update ResourceSlice (SPX → 8 CPX partitions)
  6. Return to scheduler: "Here are 8 CPX_NPS1 devices ready for allocation"

Result: Auto-partition SUCCESS ✅
        Pod gets allocated to newly created partition
```

---

## Architecture Flow Comparison

### CEL-Based (Broken)

```
┌─────────────┐
│ Pod Created │ requests CPX_NPS1 via CEL
└──────┬──────┘
       │
       ▼
┌──────────────────────┐
│ Scheduler            │
│ 1. Read ResourceSlice│ → Contains only SPX_NPS1
│ 2. Eval CEL          │ → SPX_NPS1 == CPX_NPS1? NO
│ 3. Filter node OUT   │ → Node rejected ❌
└──────────────────────┘
       │
       ▼
   DRA driver NEVER called
   Pod stays PENDING
```

### Structured Parameters (Correct)

```
┌─────────────┐
│ Pod Created │ requests partitionProfile=CPX_NPS1 (structured)
└──────┬──────┘
       │
       ▼
┌────────────────────────────┐
│ Scheduler                  │
│ 1. Read structured params  │ → partitionProfile=CPX_NPS1
│ 2. Call DRA driver for     │ → NodeListAndPrepare(claim, node-123)
│    EACH candidate node     │
└──────────────┬─────────────┘
               │
               ▼
┌──────────────────────────────────────┐
│ DRA Driver: NodeListAndPrepare()    │
│ 1. Parse: need CPX_NPS1              │
│ 2. Check ResourceSlice:              │
│    - supportedProfiles has CPX_NPS1? │ ✓ YES
│    - GPU idle?                       │ ✓ YES
│ 3. PARTITION (amd-smi set...)        │ ✓ Done
│ 4. Update ResourceSlice              │ ✓ SPX → 8×CPX
│ 5. Return devices to scheduler       │ → [gpu-0-part-0, gpu-0-part-1, ...]
└──────────────┬───────────────────────┘
               │
               ▼
┌──────────────────────┐
│ Scheduler            │
│ 1. Receives devices  │ → 8 CPX_NPS1 partitions
│ 2. Picks one         │ → gpu-0-part-0
│ 3. Calls Allocate()  │ → Finalize allocation
└──────────────────────┘
       │
       ▼
   Pod RUNNING ✅
```

---

## ResourceSlice Requirements

To enable auto-partitioning with structured parameters, ResourceSlice **must** publish:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
spec:
  nodeName: node-123
  devices:
    - name: gpu-0
      basic:
        attributes:
          # What the GPU IS now
          currentPartitionProfile: {string: "SPX_NPS1"}

          # What the GPU CAN BECOME (critical!)
          supportedPartitionProfiles: {stringSlice: ["SPX_NPS1", "CPX_NPS1", "DPX_NPS1", "QPX_NPS1"]}

          # Can it be partitioned right now?
          isIdle: {bool: true}

          model: {string: "MI300X"}
          memory: {quantity: "96Gi"}
```

**Without `supportedPartitionProfiles`**, the driver cannot determine if partition is possible!

---

## DRA Driver Implementation

**Key gRPC method**: `NodeListAndPrepare()`

Called by K8s scheduler for **each candidate node** during pod scheduling.

```go
func (d *DRADriver) NodeListAndPrepare(
    ctx context.Context,
    req *drapb.NodeListAndPrepareRequest,
) (*drapb.NodeListAndPrepareResponse, error) {
    // 1. Parse structured parameters from claim
    requestedProfile := parseStructuredParams(req.Claim) // "CPX_NPS1"

    // 2. Get ResourceSlice for this node
    resourceSlice := d.getResourceSlice(req.NodeName)

    // 3. Check if exact match exists
    if device := findExactMatch(resourceSlice, requestedProfile); device != nil {
        return &drapb.NodeListAndPrepareResponse{
            Devices: []*drapb.Device{device},
        }, nil
    }

    // 4. Check if we can auto-partition
    gpu := findConvertibleGPU(resourceSlice, requestedProfile)
    if gpu == nil {
        return nil, status.Error(codes.ResourceExhausted,
            "no convertible GPU found")
    }

    // Check supportedPartitionProfiles attribute
    supported := gpu.Attributes["supportedPartitionProfiles"].([]string)
    if !contains(supported, requestedProfile) {
        return nil, status.Error(codes.ResourceExhausted,
            "partition profile not supported")
    }

    // Check if GPU is idle
    isIdle := gpu.Attributes["isIdle"].(bool)
    if !isIdle {
        return nil, status.Error(codes.ResourceExhausted,
            "GPU not idle")
    }

    // 5. PARTITION NOW (synchronously during this call)
    if err := d.partitionGPU(gpu.ID, requestedProfile); err != nil {
        return nil, status.Error(codes.Internal,
            fmt.Sprintf("partition failed: %v", err))
    }

    // 6. UPDATE RESOURCESLICE (before returning)
    if err := d.updateResourceSlice(req.NodeName, gpu.ID, requestedProfile); err != nil {
        return nil, status.Error(codes.Internal,
            fmt.Sprintf("ResourceSlice update failed: %v", err))
    }

    // 7. Return newly created devices
    devices := d.getDevicesAfterPartition(gpu.ID, requestedProfile)

    return &drapb.NodeListAndPrepareResponse{
        Devices: devices,
    }, nil
}
```

**Critical points**:
- Partition happens **synchronously** (scheduler waits)
- ResourceSlice updated **before** returning to scheduler
- Fast compute-only partition: 1-5 seconds
- NPS partition with reload: 15-30 seconds (scheduler waits!)

---

## Summary

| Question | CEL-Based | Structured Parameters |
|----------|-----------|----------------------|
| Does scheduler evaluate against ResourceSlice? | ✅ YES (filters nodes) | ❌ NO (calls driver) |
| Does scheduler call driver for non-matching nodes? | ❌ NO | ✅ YES (all candidates) |
| Can driver partition during scheduling? | ❌ NO (not called) | ✅ YES (in NodeListAndPrepare) |
| Can auto-partition work? | ❌ NO | ✅ YES |

**Bottom line**: **MUST use structured parameters** for auto-partitioning. CEL-based matching fundamentally cannot work because the scheduler filters out nodes before the driver ever sees them.

---

## User-Facing Example

**Wrong** (will fail):
```yaml
# ❌ This will NOT auto-partition
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
spec:
  spec:
    devices:
      requests:
        - name: gpu
          deviceClassName: gpu.amd.com
          selectors:
            - cel:
                expression: device.attributes["partitionProfile"] == "CPX_NPS1"
```

**Correct** (will auto-partition):
```yaml
# ✅ This WILL auto-partition
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
spec:
  spec:
    devices:
      requests:
        - name: gpu
          deviceClassName: gpu.amd.com
      config:
        - requests:
            - partitionProfile: CPX_NPS1
              model: MI300X
              memory: "12Gi"
```

---

## References

- [Kubernetes DRA Structured Parameters KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation)
- [DRA API Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [AMD DRA Driver](https://github.com/ROCm/k8s-gpu-dra-driver)
