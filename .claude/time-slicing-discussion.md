# AMD GPU Time-Slicing and Process Throttling Discussion

**Date**: February 27, 2026
**Topic**: AMD Instinct GPU Time-Slicing, CU% Quota Enforcement, and Process Throttling

---

## Background Context

Initial quote from AMD documentation:
> "Time slicing is not officially supported on AMD Instinct GPUs. It may work but it's not validated."

Key AMD SMI APIs for monitoring:
- `amdsmi_get_gpu_process_list` - Provides instant CU occupancy values per process
- `amdsmi_get_gpu_asic_info` - Fetches total number of CUs for calculating CU% usage

---

## Research Question

**Can we enforce CU% quotas per process and throttle processes externally without their cooperation?**

---

## Key Findings

### 1. Time-Slicing Support
- ❌ **NOT officially supported** on AMD Instinct GPUs
- May work but not validated by AMD
- No production-ready time-slicing mechanism like NVIDIA MPS

### 2. Quota Enforcement Options

#### A. Spatial Partitioning ✅ (Available)
- **What**: Divide GPU into separate partitions with dedicated CUs
- **Enforcement**: Driver-level, processes confined to partition resources
- **Limitation**: Static allocation, not dynamic quota management
- **Use Case**: Multi-tenant environments with fixed resource allocation

#### B. CU Mask Setting ⚠️ (Limited)
- **API**: https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html
- **Method**: Environment variables `ROC_GLOBAL_CU_MASK` and `HSA_CU_MASK`
- **Limitations**:
  - Must be called from inside the process
  - Dynamic changes may not take effect
  - Requires process cooperation
  - No external enforcement

#### C. Process Monitoring + Killing ❌ (Not Throttling)
- **Approach**: Monitor via `amd-smi process`, kill violators with `kill -9 <PID>`
- **Limitations**:
  - Reactive, not proactive
  - Disrupts workloads completely
  - No graceful degradation
  - No actual throttling

---

## Process Isolation (Serialization)

### AMDGPU Process Isolation Feature

**Location**: `/sys/class/drm/card0/device/enforce_isolation`

**How It Works**:
- Forces GPU access serialization between processes
- Prevents concurrent execution (time-slicing effect)
- Runs cleaner shader between jobs (clears LDS and GPRs)

**Control**:
```bash
# Enable isolation (serialize all processes)
echo 1 > /sys/class/drm/card0/device/enforce_isolation

# Disable isolation
echo 0 > /sys/class/drm/card0/device/enforce_isolation
```

**Module Parameter**: `enforce_process_isolation`
- -1 = auto
- 0 = disable
- 1 = enable
- 2 = enable legacy mode
- 3 = enable without cleaner shader

**Limitations**:
- All-or-nothing per GPU (affects ALL processes)
- Not fine-grained per-process control
- No quota enforcement, just serialization
- Performance impact from preventing concurrent execution

**Sources**:
- https://docs.kernel.org/gpu/amdgpu/process-isolation.html
- https://www.phoronix.com/news/AMDGPU-Process-Isolation

---

## DRM CGroup Controller (Future Solution)

### Status: In Development (RFC v8 as of 2024-2026)

**Concept**: Weight-based hierarchical GPU usage budget system, similar to CPU cgroups

**How It Works**:
1. GPU time budget split based on relative group weights
2. Controller notifies drivers when clients exceed budget
3. Drivers enforce based on scheduling capabilities

**Control Interface**:
```bash
# Create cgroup for background tasks
mkdir /sys/fs/cgroup/background_gpu

# Add process to cgroup
echo $PID > /sys/fs/cgroup/background_gpu/cgroup.procs

# Set weight (default: 100, setting 50 = 50% GPU bandwidth)
echo 50 > /sys/fs/cgroup/background_gpu/drm.weight
```

**Capabilities**:
- ✅ External control without process cooperation
- ✅ Hierarchical control via cgroup hierarchy
- ✅ Weight-based time allocation
- ✅ Per-process or per-cgroup limits

**Use Cases**:
- Background computational tasks competing less with foreground
- Window manager integration (focused/unfocused windows)
- Multi-tenant GPU sharing in data centers

**Current Status**:
- ❌ Not yet merged into mainline Linux kernel
- ⚠️ No production-ready release
- 📅 No official timeline announced

**Sources**:
- https://blogs.igalia.com/tursulin/drm-scheduling-cgroup-controller/
- https://lwn.net/Articles/948716/
- https://www.mail-archive.com/amd-gfx@lists.freedesktop.org/msg128324.html

---

## GPU Queue Priorities

### Question: Can we set priorities for kernel queues?

**Answer**: YES, but with limitations on external control.

### 1. HSA Queue Priority (ROCm/HSA Runtime)

**Priority Levels**:
```c
HSA_AMD_QUEUE_PRIORITY_LOW
HSA_AMD_QUEUE_PRIORITY_NORMAL  // default
HSA_AMD_QUEUE_PRIORITY_HIGH
```

**API**:
```c
// During queue creation
hsa_queue_create(agent, size, type, callback, data,
                 private_segment_size, group_segment_size, &queue);

// Dynamic priority change
queue->SetPriority(HSA_AMD_QUEUE_PRIORITY_HIGH);
```

**Requirements**:
- ROCm 1.9 or higher
- Defined in `hsa_ext_amd.h` (AMD extension header)

**External Control**: ❌ NO - Requires application cooperation

**Sources**:
- https://github.com/RadeonOpenCompute/ROCR-Runtime/blob/master/src/core/inc/host_queue.h
- https://rocm.docs.amd.com/projects/HIP/en/docs-6.1.2/doxygen/html/hsa__ext__amd_8h.html

---

### 2. AMDGPU Context Priority (Kernel Driver)

**Priority Levels**:
```c
AMDGPU_CTX_PRIORITY_UNSET
AMDGPU_CTX_PRIORITY_VERY_LOW
AMDGPU_CTX_PRIORITY_LOW
AMDGPU_CTX_PRIORITY_NORMAL
AMDGPU_CTX_PRIORITY_HIGH       // 512
AMDGPU_CTX_PRIORITY_VERY_HIGH  // 1023
```

**API**:
```c
// Create context with priority
amdgpu_cs_ctx_create2(dev, AMDGPU_CTX_PRIORITY_HIGH, &ctx_id);

// Uses DRM_AMDGPU_CTX ioctl internally
```

**Available Since**: Linux 4.15 (introduced for VR workloads)

**Permission Requirements**:
- Normal/Low: All users
- High/Very High: Root/admin only
- User processes attempting high priority fall back to normal

**Priority Override** (Linux 5.1+):
- Interface: `AMDGPU_SCHED_OP_CONTEXT_PRIORITY_OVERRIDE`
- Allows overriding single context instead of entire process
- Still requires access to process's file descriptor

**External Control**: ❌ NO - Per-file-descriptor, no sysfs interface

**Sources**:
- https://www.mail-archive.com/amd-gfx@lists.freedesktop.org/msg81186.html
- https://www.phoronix.com/news/AMDGPU-4.15-Ctx-TTM-PP
- https://www.phoronix.com/news/AMDGPU-Linux-5.1-Initial-Fixes

---

### 3. User Mode Queue Priority (Mesa 25.2+)

**Priority Mapping to MES (Micro Engine Scheduler)**:
- **Level 0 (normal low)**: Most apps → `AMD_PRIORITY_LEVEL_NORMAL`
- **Level 1 (low)**: Background jobs → `AMD_PRIORITY_LEVEL_LOW`
- **Level 2 (normal high)**: High priority apps → `AMD_PRIORITY_LEVEL_MEDIUM`
- **Level 3 (high)**: Admin only, compositors → `AMD_PRIORITY_LEVEL_HIGH`

**Features**:
- Kernel queues always mapped and take priority
- Scheduling firmware dynamically maps/unmaps queues based on priority
- Priority affects scheduling order when queues exceed hardware slots

**Available Since**:
- Linux kernel 6.16+
- Mesa 25.2+

**Permission Requirements**:
- Level 3 requires root/admin
- User processes fall back to normal level

**External Control**: ❌ NO - Set during queue creation by application

**Sources**:
- https://www.phoronix.com/news/Mesa-25.2-AMDGPU-Queue-Priority
- https://docs.kernel.org/gpu/amdgpu/userq.html

---

## Hardware Scheduling Limitations

### Critical Constraint:
> **The AMD GPU Hardware Scheduler (HWS) has no inherent prioritization mechanism to favor "active" queues.**

**Implications**:
- Priority enforcement is **software-based** (driver/firmware level)
- Not enforced by GPU hardware itself
- Up to 32 concurrent HSA queues (8 per ACE × 4 ACEs)
- Scheduling firmware manages queue mapping based on priority and time quanta

**What This Means**:
- Queue priorities affect **scheduling order** and **time-slicing**
- They do **NOT** enforce CU% resource limits
- Software scheduler decides which queues get hardware queue slots

**Source**:
- https://dl.acm.org/doi/fullHtml/10.1145/3453417.3453432

---

## Can Queue Priorities Be Changed Externally?

### Answer: NO (Currently) - Requires Process Cooperation

**Internal Control Only** (Current State):

1. **HSA Queue Priority**:
   - Set via application API (`hsa_queue_create`, `SetPriority`)
   - ❌ No external control mechanism
   - ❌ Cannot be modified by another process

2. **AMDGPU Context Priority**:
   - Set via IOCTL with file descriptor
   - ❌ No sysfs or external interface
   - ❌ Priority override requires FD access

**External Control** (Future):

**DRM Cgroup Controller** - The ONLY mechanism for true external control:
- ✅ Weight-based GPU time allocation via cgroups
- ✅ No process cooperation required
- ✅ Runtime changes supported
- ❌ **Still in development** (not merged)

**Control Method**:
```bash
# External process control via cgroups
echo $PID > /sys/fs/cgroup/background_gpu/cgroup.procs
echo 50 > /sys/fs/cgroup/background_gpu/drm.weight  # 50% GPU bandwidth
```

---

## Summary: Throttling Capabilities

| Mechanism | Throttling Type | External Control | Production Ready | Granularity |
|-----------|----------------|------------------|------------------|-------------|
| Process Isolation | Serialization only | ✅ Yes (sysfs) | ✅ Yes | Per-GPU |
| DRM CGroup | Time budget/weight | ✅ Yes (cgroup) | ❌ No (in dev) | Per-cgroup |
| Spatial Partition | Fixed CU allocation | ✅ Yes (driver) | ✅ Yes | Per-partition |
| HSA Queue Priority | Scheduling order | ❌ No (app-only) | ✅ Yes | Per-queue |
| AMDGPU Context Priority | Scheduling order | ❌ No (FD-based) | ✅ Yes | Per-context |
| Device CGroups | Access control | ✅ Yes (cgroup) | ✅ Yes | Per-device |
| CU Mask | CU availability | ❌ No (env vars) | ✅ Yes | Per-process |
| Kill Process | Hard termination | ✅ Yes (signal) | ✅ Yes | Per-process |

---

## Queue Priority Comparison

| Method | API Level | Priority Levels | Root Required | Available Since | External Control |
|--------|-----------|----------------|---------------|-----------------|------------------|
| HSA Queue Priority | HSA Runtime | 3 (LOW/NORMAL/HIGH) | For HIGH only | ROCm 1.9+ | ❌ NO |
| AMDGPU Context | Kernel IOCTL | 5 levels | For HIGH/VERY_HIGH | Linux 4.15+ | ❌ NO |
| User Mode Queue | Mesa/Kernel | 4 levels (MES) | For level 3 | Linux 6.16+, Mesa 25.2+ | ❌ NO |
| DRM Cgroup | Cgroup | Weight-based | For high weights | In Development | ✅ YES |

---

## Bottom Line

### Current Situation (2026):

**❌ NO, you CANNOT externally throttle AMD GPU processes at the CU% level** because:

1. **Time-slicing**: Not officially supported on AMD Instinct GPUs
2. **Quota enforcement**: Not supported at driver level (except spatial partitioning)
3. **Queue priorities**: Exist but require process cooperation
4. **External control**: No production mechanism available

### Available Options TODAY:

1. **Process Isolation/Serialization**:
   - Forces sequential execution (prevents concurrency)
   - All-or-nothing per GPU
   - `/sys/class/drm/card0/device/enforce_isolation`

2. **Spatial Partitioning**:
   - Static CU allocation per partition
   - Driver-level enforcement
   - Not dynamic quota management

3. **Monitoring + Killing**:
   - Reactive, not proactive
   - Disrupts workloads
   - No graceful throttling

### Future Solution:

**DRM Scheduling Cgroup Controller** will enable:
- ✅ External process control without cooperation
- ✅ Weight-based GPU time allocation
- ✅ Dynamic runtime changes
- ✅ Per-process or per-cgroup throttling
- ✅ Hierarchical cgroup support

**Timeline**: No official date, currently RFC v8 (2024-2026)

**Workaround**: Build custom kernel with DRM cgroup patches

---

## Technical Details

### AMD GPU Architecture (Scheduling)

**Hardware Components**:
- 4 ACEs (Asynchronous Compute Engines)
- 8 HSA queues per ACE
- Total: 32 concurrent HSA queues maximum

**Software Stack**:
1. Application uses HIP API (`hipLaunchKernelGGL`)
2. HIP runtime → ROCclr software queue
3. ROCclr → HSA queue (AQL packets)
4. Hardware scheduler assigns queues to ACEs
5. ACEs distribute workgroups to CUs

**Scheduling Limitation**:
- HWS has no inherent prioritization for active queues
- Firmware/driver implements software-based priority scheduling
- Priority affects which queues get hardware slots, not CU% enforcement

### AMD SMI Monitoring APIs

**Process List**:
```c
amdsmi_get_gpu_process_list(gpu, num_items, &list)
// Returns instant CU occupancy per process
```

**ASIC Info**:
```c
amdsmi_get_gpu_asic_info(gpu, &asic_info)
// Returns total CU count for calculating CU% usage
```

**Violation Status** (MI300+ only):
```c
amdsmi_get_violation_status(gpu, &status)
// CLI: amd-smi metric --throttle
//      amd-smi monitor --violation
```

---

## References

### Documentation
- [AMD ROCm Documentation](https://rocm.docs.amd.com/)
- [AMDGPU Process Isolation](https://docs.kernel.org/gpu/amdgpu/process-isolation.html)
- [AMD SMI Documentation](https://rocm.docs.amd.com/projects/amdsmi/en/latest/)
- [Setting CUs in ROCm](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html)

### Kernel Development
- [DRM Scheduling Cgroup Controller](https://blogs.igalia.com/tursulin/drm-scheduling-cgroup-controller/)
- [DRM CGroup LWN Article](https://lwn.net/Articles/948716/)
- [AMDGPU User Mode Queues](https://docs.kernel.org/gpu/amdgpu/userq.html)

### Research Papers
- [Exploring AMD GPU Scheduling Details](https://dl.acm.org/doi/fullHtml/10.1145/3453417.3453432)

### News/Articles
- [AMD Process Isolation Support](https://www.phoronix.com/news/AMDGPU-Process-Isolation)
- [AMD DRM CGroup Controller RFC](https://www.phoronix.com/news/AMD-DRM-Cgroup-Controller-RFC)
- [Mesa 25.2 Queue Priority Support](https://www.phoronix.com/news/Mesa-25.2-AMDGPU-Queue-Priority)
- [AMDGPU Linux 4.15 Context Priority](https://www.phoronix.com/news/AMDGPU-4.15-Ctx-TTM-PP)

---

## Conclusion

**For CU% quota enforcement and external throttling on AMD Instinct GPUs:**

1. **Not possible today** without process cooperation
2. **Spatial partitioning** provides fixed resource allocation but not dynamic quotas
3. **Process isolation** can serialize execution but not enforce CU% limits
4. **Queue priorities** exist but cannot be changed externally
5. **DRM cgroup controller** is the future solution but not yet available

**Recommendation**:
- For immediate needs: Use spatial partitioning with fixed allocations
- For future: Monitor DRM cgroup controller development
- For custom needs: Consider patching kernel with RFC patches (unsupported)

**Key Difference from NVIDIA**:
- NVIDIA has MPS (Multi-Process Service) for resource limiting
- NVIDIA has MIG (Multi-Instance GPU) for hardware partitioning
- AMD lacks equivalent production-ready solutions as of 2026
