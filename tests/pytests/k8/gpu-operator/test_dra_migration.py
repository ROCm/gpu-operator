#!/usr/bin/python3

"""
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
"""

"""
TC-MIGRATE-001: Device Plugin ↔ DRA live migration.

Starting state: DeviceConfig with Device Plugin enabled (operator default), DRA disabled.
Tests the full forward (DP → DRA) and reverse (DRA → DP) migration path via DeviceConfig patch.
"""

import re
import time
import types
import logging
import contextlib
import pytest
import lib.common as common
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.dra_util as dra_util
from lib.util import K8Helper
from packaging import version

Logger = logging.getLogger("k8.gpu-operator.test_dra_migration")

debug_on_failure = K8Helper.triage


@pytest.fixture(autouse=True, scope="module")
def skip_module(environment):
    """Skip if K8s version < 1.32, DRA API unavailable, or operator < v1.5.0."""
    ret_code, version_info = k8_util.k8_get_version()
    if ret_code != 0:
        pytest.skip("Failed to get Kubernetes version")

    major_match = re.match(r"(\d+)", str(version_info.get("major", "0")))
    minor_match = re.match(r"(\d+)", str(version_info.get("minor", "0")))
    major = int(major_match.group(1)) if major_match else 0
    minor = int(minor_match.group(1)) if minor_match else 0

    if major < 1 or (major == 1 and minor < 32):
        pytest.skip(
            f"DRA requires Kubernetes 1.32+, but cluster is running {major}.{minor}"
        )

    dra_available, error_msg, _ = dra_util.check_dra_api_available()
    if not dra_available:
        pytest.skip(f"DRA API not available: {error_msg}")

    gpu_operator_version = getattr(environment, "gpu_operator_version", None)
    if gpu_operator_version:
        with contextlib.suppress(version.InvalidVersion):
            if version.parse(gpu_operator_version.lstrip("v")) < version.parse("1.5.0"):
                pytest.skip(
                    f"draDriver operand requires GPU Operator v1.5.0+, got {gpu_operator_version}"
                )

    return


@pytest.fixture(scope="module")
def deviceconfig_install(gpu_cluster, images, gpu_operator_install, environment, dra_api_version):
    """
    Install DeviceConfig with Device Plugin enabled (default state), DRA disabled.
    Starting state for DP → DRA migration tests.
    """
    global Logger

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, ret_code == 0, "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, len(gpu_nodes) > 0, "No nodes with AMD/GPU found in the cluster")

    # Starting state: DP enabled (operator defaults), DRA disabled
    test_config = {
        "metadata.namespace": environment.gpu_operator_namespace,
        "driver.enable": True,
        "devicePlugin.enableNodeLabeller": False,
        "metricsExporter.enable": False,
        "testRunner.enable": False,
    }
    test_config.update(images)

    test_cfg_map = spec_util.build_deviceconfig_cr_template(
        test_config, gpu_nodes, "dra_migration", environment.amdgpu_driver_spec
    )
    devicecfg_list = []
    for spec_name, tcfg in test_cfg_map.items():
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_create_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment, ret_code == 0,
            f"Failed to create deviceconfig '{spec_name}': {ret_stderr}",
        )
        devicecfg_list.append(tcfg["metadata.name"])

    K8Helper.check_deviceconfig_status(environment, devicecfg_list)

    driver_deployment = environment.amdgpu_driver_spec.get("driver-deployment", "deviceconfig")
    if driver_deployment != "inbox":
        for devcfg in devicecfg_list:
            try:
                K8Helper.wait_kmm_worker_completion(environment, devcfg)
            except Exception as e:
                Logger.warning(f"KMM completion check failed for {devcfg}: {e}")

    K8Helper.update_node_driver_version(gpu_cluster, environment)

    yield types.SimpleNamespace(test_cfg_map=test_cfg_map)

    # Teardown — delete only DeviceConfigs created by this fixture
    for devcfg_name in devicecfg_list:
        k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)


@pytest.mark.level1
def test_dra_migration(deviceconfig_install, gpu_cluster, environment, dra_api_version):
    """
    TC-MIGRATE-001: Device Plugin ↔ DRA live migration via DeviceConfig patch.

    Forward (DP → DRA):
      1. Verify DP pods running and amd.com/gpu capacity > 0 on GPU nodes.
      2. Patch DeviceConfig: enableDevicePlugin=False.
      3. Assert DP pods terminated.
      4. Patch DeviceConfig: draDriver.enable=True (DP stays disabled).
      5. Assert DRA pods running, DeviceClass 'gpu.amd.com' exists, ResourceSlices published.

    Reverse (DRA → DP):
      6. Patch DeviceConfig: draDriver.enable=False.
      7. Assert DRA pods terminated and ResourceSlices removed.
      8. Patch DeviceConfig: enableDevicePlugin=True (DRA stays disabled).
      9. Assert DP pods running, amd.com/gpu capacity restored on all GPU nodes.
    """
    global Logger

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, ret_code == 0, "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, len(gpu_nodes) > 0, "No nodes with AMD/GPU found in the cluster")

    gpu_node_count = len(gpu_nodes)
    node_names = [n.get("metadata", {}).get("name") for n in gpu_nodes]

    # ------------------------------------------------------------------
    # Step 1: Verify initial DP state
    # ------------------------------------------------------------------
    Logger.info("Step 1: Verify initial Device Plugin state")
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace,
        [common.PodInfo("device-plugin", gpu_node_count, 1)],
        sleep_time=20,
    )
    debug_on_failure(environment, not failed_pods, f"[Step 1] DP pods not running: {failed_pods}")

    for node_name in node_names:
        capacity, _ = k8_util.k8_get_node_gpu_capacity(node_name)
        debug_on_failure(
            environment, capacity > 0,
            f"[Step 1] Node '{node_name}': amd.com/gpu capacity={capacity}, expected >0",
        )
    Logger.info(f"[Step 1] Device plugin running, amd.com/gpu capacity confirmed on {gpu_node_count} node(s)")

    # ------------------------------------------------------------------
    # Step 2: Disable Device Plugin
    # ------------------------------------------------------------------
    Logger.info("Step 2: Disable Device Plugin (enableDevicePlugin=False)")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["devicePlugin.enableDevicePlugin"] = False
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment, ret_code == 0,
            f"[Step 2] Failed to patch deviceconfig to disable DP: {ret_stderr}",
        )

    # Wait for operator to reconcile before polling pod state
    time.sleep(30)

    # ------------------------------------------------------------------
    # Step 3: Assert DP pods terminated
    # ------------------------------------------------------------------
    # Note: kubelet retains amd.com/gpu capacity in node status even after the device plugin
    # pod is gone — the capacity only clears on kubelet restart or if the plugin re-registers
    # with count=0. We only assert pod termination here, not capacity.
    Logger.info("Step 3: Assert DP pods terminated")
    running_pods = k8_util.k8_check_pod_terminated(
        environment.gpu_operator_namespace,
        [common.PodInfo("device-plugin", gpu_node_count, 1)],
    )
    debug_on_failure(
        environment, not running_pods,
        f"[Step 3] DP pods still running after disabling: {running_pods}",
    )
    Logger.info("[Step 3] Device plugin pods terminated")

    # ------------------------------------------------------------------
    # Step 4: Enable DRA driver (DP stays disabled)
    # ------------------------------------------------------------------
    Logger.info("Step 4: Enable DRA driver (draDriver.enable=True, DP stays disabled)")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["draDriver.enable"] = True
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment, ret_code == 0,
            f"[Step 4] Failed to patch deviceconfig to enable DRA: {ret_stderr}",
        )

    # ------------------------------------------------------------------
    # Step 5: Assert DRA operational — pods, DeviceClass, ResourceSlices
    # ------------------------------------------------------------------
    Logger.info("Step 5: Assert DRA driver is operational")
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace,
        [common.PodInfo("dra-driver", gpu_node_count, 1)],
        sleep_time=20,
    )
    debug_on_failure(
        environment, not failed_pods,
        f"[Step 5] DRA driver pods not ready: {failed_pods}",
    )

    exists, err, _ = dra_util.verify_device_class_exists(dra_api_version, "gpu.amd.com")
    debug_on_failure(environment, exists, f"[Step 5] DeviceClass 'gpu.amd.com' not found: {err}")

    slices_ok, err, _ = dra_util.verify_resource_slices_exist(
        dra_api_version, "gpu.amd.com", min_count=gpu_node_count
    )
    debug_on_failure(
        environment, slices_ok,
        f"[Step 5] ResourceSlices for 'gpu.amd.com' not published (expected >={gpu_node_count}): {err}",
    )
    Logger.info("[Step 5] DRA driver operational: pods running, DeviceClass exists, ResourceSlices published")

    # ------------------------------------------------------------------
    # Step 6: Disable DRA driver
    # ------------------------------------------------------------------
    Logger.info("Step 6: Disable DRA driver (draDriver.enable=False)")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["draDriver.enable"] = False
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment, ret_code == 0,
            f"[Step 6] Failed to patch deviceconfig to disable DRA: {ret_stderr}",
        )

    # Wait for operator to reconcile before polling pod state
    time.sleep(30)

    # ------------------------------------------------------------------
    # Step 7: Assert DRA pods terminated and ResourceSlices removed
    # ------------------------------------------------------------------
    # Note: The DeviceClass 'gpu.amd.com' is created by the DRA driver pod at startup but
    # is NOT cleaned up by the pod on shutdown (it is a persistent cluster-scoped resource).
    # On K8s (non-OpenShift), the operator also does not manage the DeviceClass lifecycle.
    # We validate pod termination and ResourceSlice removal — ResourceSlices ARE cleaned up
    # when the driver pod stops publishing them.
    Logger.info("Step 7: Assert DRA driver pods terminated and ResourceSlices removed")
    running_pods = k8_util.k8_check_pod_terminated(
        environment.gpu_operator_namespace,
        [common.PodInfo("dra-driver", gpu_node_count, 1)],
    )
    debug_on_failure(
        environment, not running_pods,
        f"[Step 7] DRA pods still running after disabling: {running_pods}",
    )

    deleted, err = dra_util.wait_for_resource_slices_deletion(dra_api_version, "gpu.amd.com")
    debug_on_failure(
        environment, deleted,
        f"[Step 7] ResourceSlices not deleted after DRA driver disabled: {err}",
    )
    Logger.info("[Step 7] DRA driver removed: pods terminated, ResourceSlices removed")

    # ------------------------------------------------------------------
    # Step 8: Re-enable Device Plugin (DRA stays disabled)
    # ------------------------------------------------------------------
    Logger.info("Step 8: Re-enable Device Plugin (enableDevicePlugin=True, DRA stays disabled)")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["devicePlugin.enableDevicePlugin"] = True
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment, ret_code == 0,
            f"[Step 8] Failed to patch deviceconfig to re-enable DP: {ret_stderr}",
        )

    # ------------------------------------------------------------------
    # Step 9: Assert DP restored — pods running, GPU capacity back
    # ------------------------------------------------------------------
    Logger.info("Step 9: Assert Device Plugin restored")
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace,
        [common.PodInfo("device-plugin", gpu_node_count, 1)],
        sleep_time=20,
    )
    debug_on_failure(
        environment, not failed_pods,
        f"[Step 9] DP pods not running after re-enable: {failed_pods}",
    )

    # Wait for kubelet to reflect capacity restoration — asynchronous after pod startup
    for node_name in node_names:
        capacity = None
        for _ in range(18):  # up to 90s (18 × 5s)
            capacity, _ = k8_util.k8_get_node_gpu_capacity(node_name)
            if capacity is not None and capacity > 0:
                break
            time.sleep(5)
        debug_on_failure(
            environment, capacity is not None and capacity > 0,
            f"[Step 9] Node '{node_name}': amd.com/gpu capacity={capacity} after DP re-enabled, expected >0",
        )

    Logger.info("TC-MIGRATE-001 PASSED: DP → DRA → DP migration completed successfully")
