# DRA Driver Test Suite

This directory contains pytest-based test suites for validating the AMD GPU DRA (Dynamic Resource Allocation) driver on Kubernetes.

## Overview

The DRA driver test suite validates:

- DRA driver installation via Helm chart
- Device discovery and advertisement
- GPU attributes and metadata
- Hardware correlation and accuracy
- Cleanup and uninstallation

## Prerequisites

- Kubernetes 1.32+ (DRA API v1beta1 with feature gate enabled)
- Kubernetes 1.34+ (DRA API v1 - GA, enabled by default)
- GPU operator installed and running
- AMDGPU driver loaded on cluster nodes
- AMD GPUs available on at least one node

## Test Files

### test_dra_driver_install.py

Installation, validation, and cleanup tests.

**Tests (3):**

1. **test_dra_driver_install**
   - **Purpose**: Validate DRA driver Helm chart installation
   - **Validates**:
     - Helm release is deployed successfully
     - DRA driver namespace exists
     - DRA driver pods are running
     - Pod logs have no critical errors
   - **Fixtures**: `dra_driver_install`
   - **kubectl equivalents**:
     - `kubectl get namespaces`
     - `kubectl get pods -n kube-amd-gpu-dra`
     - `kubectl logs <pod-name> -n kube-amd-gpu-dra`

2. **test_dra_driver_gpu_node_labels**
   - **Purpose**: Validate GPU nodes have proper DRA-related labels
   - **Validates**:
     - Nodes have `feature.node.kubernetes.io/amd-gpu=true` label
   - **Fixtures**: `dra_driver_install`
   - **kubectl equivalents**:
     - `kubectl get nodes -l feature.node.kubernetes.io/amd-gpu=true`

3. **test_dra_driver_uninstall**
   - **Purpose**: Test DRA driver uninstallation and verify cleanup
   - **Validates**:
     - Helm chart can be uninstalled successfully
     - All DRA driver pods are terminated
     - DeviceClass `gpu.amd.com` is deleted
     - **Then reinstalls DRA driver** for test isolation
   - **Fixtures**: `dra_driver_install`, `images`, `dra_api_version`
   - **kubectl equivalents**:
     - `helm uninstall <release> -n <namespace>`
     - `kubectl get pods -n kube-amd-gpu-dra`
     - `kubectl get deviceclasses.resource.k8s.io`
   - **Note**: This test ensures other tests can run in any order by reinstalling after validation

### test_dra_driver_attributes.py

Device attribute validation and hardware correlation tests.

**Tests (3):**

1. **test_dra_driver_device_attributes**
   - **Purpose**: Validate DRA driver advertises all required device attributes
   - **Validates**:
     - ResourceSlices are created and populated
     - Each GPU device has required attributes:
       - `type` (e.g., "amdgpu")
       - `pciAddr` (PCI address)
       - `drmCardIndex` (DRM card index)
       - `numCUs` (compute units)
       - `numSIMDs` (SIMD units)
       - `devName` (device name/codename)
       - `uuid` (unique device ID)
       - `vram` (VRAM size in bytes)
       - Additional model-specific attributes
   - **Reference**: [DRA Driver Attributes Documentation](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/docs/driver-attributes.md)
   - **Fixtures**: `dra_driver_install`
   - **kubectl equivalents**:
     - `kubectl get resourceslices.resource.k8s.io`
     - `kubectl get resourceslices -o yaml`

2. **test_dra_gpu_count_matches_hardware**
   - **Purpose**: Verify DRA advertises the same number of GPUs as detected by hardware
   - **Validates**:
     - DRA device count matches hardware lspci/sysfs detection
     - Per-node validation (all nodes checked)
     - Type filtering (only "amdgpu" type devices counted)
   - **Fixtures**: `dra_driver_install`, `gpu_hardware_info`
   - **Hardware detection**: Uses pod to run `lspci` and read `/sys/class/drm`

3. **test_dra_devices_match_hardware**
   - **Purpose**: Deep validation that DRA attributes match actual hardware per GPU
   - **Validates** (per GPU, using PCI address as key):
     - `pciAddr` matches hardware PCI address
     - `drmCardIndex` matches hardware DRM card index
     - `numCUs` matches hardware compute units
     - `devName` matches hardware device name
     - `uuid` matches hardware UUID
     - `vram` matches hardware VRAM size
     - Partition attributes match (if partitioned)
   - **Fixtures**: `dra_driver_install`, `gpu_hardware_info`
   - **Approach**: Detailed per-GPU comparison, reports exact mismatches
   - **Hardware detection**: Collects data from sysfs, DRM, and lspci

## Fixtures

### Session-Scoped Fixtures

Located in `conftest.py`:

- **`dra_api_version`**: Detects and validates DRA API version (v1beta1 or v1)
- **`dra_driver_release_name`**: Returns DRA driver Helm release name
- **`dra_driver_namespace`**: Returns DRA driver namespace
- **`init_dra_testbed`**: Session setup (cleanup before tests, create namespace, validate DRA API)

### Module-Scoped Fixtures

Located in `conftest.py`:

- **`amdgpu_driver_install`**:
  - Installs AMDGPU driver via DeviceConfig CR
  - Depends on `gpu_operator_install` (from `k8/conftest.py`)
  - Waits for KMM module build/load to complete

- **`dra_driver_install`**:
  - Installs DRA driver Helm chart
  - Depends on `amdgpu_driver_install`
  - Waits for DRA driver pods to be ready
  - No cleanup (by design - allows manual inspection)

### Test-Specific Fixtures

Located in test files:

- **`gpu_hardware_info`** (in `test_dra_driver_attributes.py`):
  - Collects hardware info for all GPU nodes once per test module
  - Spawns debug pods to run `lspci` and read sysfs
  - Returns dict mapping `node_name -> hardware_data`
  - Reduces pod creation from 3N to N (where N = number of nodes)

## Test Execution

### Run All DRA Tests

```bash
pytest tests/pytests/k8/dra-driver
```

### Run Specific Test File

```bash
pytest tests/pytests/k8/dra-driver/test_dra_driver_install.py
pytest tests/pytests/k8/dra-driver/test_dra_driver_attributes.py
```

### Run Specific Test

```bash
pytest tests/pytests/k8/dra-driver/test_dra_driver_install.py::test_dra_driver_install
pytest tests/pytests/k8/dra-driver/test_dra_driver_attributes.py::test_dra_driver_device_attributes
```

### Run with Verbose Output

```bash
pytest tests/pytests/k8/dra-driver -v --log-cli-level=INFO
```

## jobd Integration

The DRA driver tests are integrated with jobd CI system via:

**Job**: `dra-driver-pytest-sanity` (in `tests/jobs/sanity/.job.yml`)

**Command**:

```bash
/gpu-operator/ci-internal/run_sanity.sh --deployment k8 --app dra-driver \
  --testbed /warmd.json --amdgpu-driver default-deviceconfig
```

**Build Dependencies**:

- `build-gpu-operator`
- `build-gpu-operator-k8s`
- `build-external-kernel-module-manager`
- `build-external-kernel-module-signimage`
- `build-external-kernel-module-webhook-server`
- `build-external-kernel-module-worker`

**Test Execution Flow**:

1. GPU operator installed
2. AMDGPU driver loaded via DeviceConfig
3. DRA driver Helm chart deployed
4. All 6 tests run (both test files)

## Fixture Dependency Chain

```text
Session Setup:
  init_dra_testbed
    ↓
  dra_api_version (validates K8s version and DRA API)

Module-Level (per test file):
  gpu_operator_install (from k8/conftest.py)
    ↓
  amdgpu_driver_install (creates DeviceConfig CR)
    ↓
  dra_driver_install (installs DRA driver Helm chart)
    ↓
  Tests execute
    ↓
  Cleanup (DeviceConfig deleted, GPU operator uninstalled)
```

## Test Design Principles

1. **Module-scoped fixtures**: Each test file gets fresh GPU operator + driver install
2. **No auto-cleanup**: Fixtures don't cleanup to allow manual inspection after test failure
3. **Test isolation**: `test_dra_driver_uninstall` reinstalls after testing uninstall
4. **Order independence**: Tests can run in any order (via reinstall in uninstall test)
5. **Hardware correlation**: Attributes validated against actual hardware, not just existence checks

## Debugging

### View DRA Driver Logs

```bash
kubectl logs -n kube-amd-gpu-dra -l app.kubernetes.io/name=k8s-gpu-dra-driver
```

### Check ResourceSlices

```bash
kubectl get resourceslices.resource.k8s.io
kubectl get resourceslices -o yaml
```

### Check DeviceClass

```bash
kubectl get deviceclasses.resource.k8s.io
kubectl describe deviceclass gpu.amd.com
```

### Verify DRA API Version

```bash
# For K8s 1.32-1.33 (v1beta1)
kubectl api-resources | grep resource.k8s.io/v1beta1

# For K8s 1.34+ (v1)
kubectl api-resources | grep resource.k8s.io/v1
```

### Manual Cleanup

If tests fail and leave resources:

```bash
# Uninstall DRA driver
helm uninstall amd-gpu-dra-driver -n kube-amd-gpu-dra

# Delete DeviceConfig
kubectl delete deviceconfig -n kube-amd-gpu --all

# Uninstall GPU operator
helm uninstall gpu-operator -n kube-amd-gpu
```

## Known Issues / Limitations

1. **Test ordering**: While tests maintain isolation via reinstall, pytest default ordering works. For guaranteed order, `pytest_collection_modifyitems` could be added.

2. **Cleanup philosophy**: Fixtures intentionally don't cleanup automatically. This allows manual inspection but means failed tests leave resources.

3. **Hardware info collection**: Uses privileged debug pods to collect hardware info. Requires cluster permissions to create pods.

4. **K8s version dependency**: Tests require K8s 1.32+ with DRA API enabled. Automatically skipped on older versions.

## Contributing

When adding new DRA driver tests:

1. Follow the existing fixture pattern
2. Use `K8Helper.triage()` for validation assertions
3. Add detailed docstrings explaining what's being validated
4. Include kubectl equivalent commands in comments
5. Update this README with new test descriptions
6. Ensure tests maintain independence (can run in any order)

## References

- [DRA Driver Attributes Documentation](https://github.com/ROCm/k8s-gpu-dra-driver/blob/main/docs/driver-attributes.md)
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [DRA API Reference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourceslice-v1beta1-resource-k8s-io)
