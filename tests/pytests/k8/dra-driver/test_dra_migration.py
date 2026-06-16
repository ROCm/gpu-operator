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
TC-DP-DRA-HELM-MIGRATION: Operator Device Plugin ↔ Standalone DRA Helm Chart migration.

Starting state: GPU Operator + DeviceConfig with Device Plugin enabled.
Tests the coexistence and bidirectional migration between the operator-managed
Device Plugin and a standalone DRA driver helm chart installation.
"""

import re
import os
import time
import types
import logging
import pytest
import lib.common as common
import lib.helm_util as helm_util
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.dra_util as dra_util
from lib.util import K8Helper

Logger = logging.getLogger("k8.dra-driver.test_dra_migration")

debug_on_failure = K8Helper.triage


@pytest.fixture(autouse=True, scope="module")
def skip_module(environment):
    """
    Skip if:
    - K8s version < 1.32 (DRA not supported)
    - DRA API unavailable in the cluster

    Note: inbox driver mode with operator present is fully supported —
    the operator manages the Device Plugin lifecycle regardless of how
    the AMDGPU kernel driver was loaded. Operator version is not checked
    here — DRA API availability in the cluster is the authoritative gate.
    """
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

    return


@pytest.fixture(scope="module")
def deviceconfig_install(gpu_cluster, images, gpu_operator_install, environment):
    """
    Install DeviceConfig with Device Plugin ENABLED (migration starting state).

    Creates a DeviceConfig with Device Plugin as the primary GPU allocator.
    The GPU Operator is brought in via gpu_operator_install.
    """
    global Logger

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, ret_code == 0, "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, len(gpu_nodes) > 0, "No AMD GPU nodes found in cluster")

    # Starting state: DP enabled, DRA disabled
    # In inbox mode the host already carries the driver — do not enable KMM-managed driver.
    driver_deployment = environment.amdgpu_driver_spec.get("driver-deployment", "deviceconfig")
    test_config = {
        "metadata.namespace": environment.gpu_operator_namespace,
        "driver.enable": driver_deployment != "inbox",
        "devicePlugin.enableDevicePlugin": True,
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
    if driver_deployment != "inbox":
        for devcfg in devicecfg_list:
            try:
                K8Helper.wait_kmm_worker_completion(environment, devcfg)
            except Exception as e:
                Logger.warning(f"KMM completion check failed for {devcfg}: {e}")

    K8Helper.update_node_driver_version(gpu_cluster, environment)

    yield types.SimpleNamespace(test_cfg_map=test_cfg_map)

    # Teardown — delete DeviceConfigs created by this fixture
    for devcfg_name in devicecfg_list:
        k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)


@pytest.mark.level1
def test_dp_dra_helm_migration(
    deviceconfig_install,
    gpu_cluster,
    environment,
    dra_api_version,
    dra_driver_release_name,
    dra_driver_namespace,
    images,
):
    """
    TC-DP-DRA-HELM-MIGRATION: Operator Device Plugin ↔ Standalone DRA Helm Chart migration.

    Forward (DP enabled → install DRA helm chart → disable DP):
      1. Verify DP baseline: pods running, amd.com/gpu capacity > 0 on all GPU nodes.
      2. Install standalone DRA helm chart alongside DP.
      3. Verify coexistence: DRA pods running, DeviceClass exists, ResourceSlices published,
         DP pods still running.
      4. Disable Device Plugin via DeviceConfig patch.
      5. Verify clean handoff: DP pods terminated, DRA still operational, DRA allocates GPU
         (ResourceClaim probe: create claim + pod, verify pod runs, proves DRA kubelet plugin
         is the sole active GPU allocator).

    Reverse (DRA only → re-enable DP → uninstall DRA):
      6. Re-enable Device Plugin via DeviceConfig patch.
      7. Verify coexistence: DP pods running, capacity restored, DRA still running.
      8. Uninstall DRA helm chart.
      9. Verify clean handoff: DRA pods terminated, ResourceSlices removed, DP still running.
    """
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, ret_code == 0, "Failed to get GPU nodes")
    debug_on_failure(environment, len(gpu_nodes) > 0, "No AMD GPU nodes found in cluster")

    n = len(gpu_nodes)
    node_names = [node.get("metadata", {}).get("name") for node in gpu_nodes]
    dra_pod_prefix = f"{dra_driver_release_name}-k8s-gpu-dra-driver-kubeletplugin"
    op_ns = environment.gpu_operator_namespace

    def assert_dp_running(step):
        failed = k8_util.k8_check_pod_running(op_ns, [common.PodInfo("device-plugin", n, 1)], sleep_time=20)
        debug_on_failure(environment, not failed, f"[Step {step}] DP pods not running: {failed}")

    def assert_dp_terminated(step):
        running = k8_util.k8_check_pod_terminated(op_ns, [common.PodInfo("device-plugin", n, 1)])
        debug_on_failure(environment, not running, f"[Step {step}] DP pods still running: {running}")

    def assert_dra_running(step):
        failed = k8_util.k8_check_pod_running(dra_driver_namespace, [common.PodInfo(dra_pod_prefix, n, 1)], sleep_time=10)
        debug_on_failure(environment, not failed, f"[Step {step}] DRA driver pods not running: {failed}")

    def assert_slices_exist(step):
        ok, err, _ = dra_util.verify_resource_slices_exist(dra_api_version, "gpu.amd.com", min_count=n)
        debug_on_failure(environment, ok, f"[Step {step}] ResourceSlices not published: {err}")

    def patch_dp(enabled, step):
        for _, tcfg in deviceconfig_install.test_cfg_map.items():
            tcfg["devicePlugin.enableDevicePlugin"] = enabled
            cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
            ret_code, _, stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
            debug_on_failure(environment, ret_code == 0, f"[Step {step}] DeviceConfig patch failed: {stderr}")

    # ------------------------------------------------------------------
    # Step 1: Verify Device Plugin baseline
    # ------------------------------------------------------------------
    Logger.info("Step 1: Verify Device Plugin baseline")
    assert_dp_running(1)
    for node in node_names:  # kubelet capacity registration is async after DP pod starts — poll up to 90s
        capacity = None
        for _ in range(18):
            capacity, _ = k8_util.k8_get_node_gpu_capacity(node)
            if capacity and capacity > 0:
                break
            time.sleep(5)
        debug_on_failure(environment, capacity and capacity > 0, f"[Step 1] Node '{node}': amd.com/gpu capacity={capacity}, expected >0")

    # ------------------------------------------------------------------
    # Step 2: Install standalone DRA helm chart
    # ------------------------------------------------------------------
    Logger.info("Step 2: Install standalone DRA helm chart")
    dra_chart = images.get("dra-driver.helm-chart")
    dra_version = images.get("dra-driver.version")
    debug_on_failure(environment, dra_chart is not None, "[Step 2] DRA helm chart not found in image manifest")

    if images.get("dra-driver.repo-name") and images.get("dra-driver.repo"):
        helm_util.helm_add_repo(gpu_cluster, images["dra-driver.repo-name"], images["dra-driver.repo"])

    if dra_chart and dra_chart.startswith("oci://") and images.get("dra-driver.secret"):
        for entry in getattr(gpu_cluster, "k8_secrets", {}).get("secrets", []):
            if entry.get("name") == images["dra-driver.secret"]:
                registry = re.match(r"oci://([a-zA-Z0-9.-]+(?::\d+)?)", dra_chart)
                if registry:
                    helm_util.helm_registry_login(gpu_cluster, registry.group(1), entry["username"], entry["password"])
                break

    values_yaml = None
    if images.get("image.repository.repository") or images.get("dra-driver-image.repository") or images.get("draDriver.image.repository"):
        values_yaml = os.path.join(environment.logdir, f"dra_migration_values_{dra_version}.yaml")
        dra_util.generate_dra_driver_values(images, values_yaml)

    # Operator chart creates gpu.amd.com DeviceClass by default; skip creation in DRA chart
    # to avoid helm ownership conflict on the same cluster-scoped resource.
    install_kwargs = {}
    if dra_util.verify_device_class_exists(dra_api_version, "gpu.amd.com")[0]:
        install_kwargs["deviceClass.create"] = "false"

    ret_code, _, stderr = helm_util.helm_install(gpu_cluster, dra_driver_release_name, dra_driver_namespace, dra_chart, dra_version, values_yaml, **install_kwargs)
    debug_on_failure(environment, ret_code == 0, f"[Step 2] Helm install failed: {stderr}")

    # ------------------------------------------------------------------
    # Step 3: Verify coexistence — both DP and DRA running
    # ------------------------------------------------------------------
    Logger.info("Step 3: Verify DP + DRA coexistence")
    assert_dra_running(3)
    exists, err, _ = dra_util.verify_device_class_exists(dra_api_version, "gpu.amd.com")
    debug_on_failure(environment, exists, f"[Step 3] DeviceClass not found: {err}")
    assert_slices_exist(3)
    assert_dp_running(3)

    # ------------------------------------------------------------------
    # Step 4: Disable Device Plugin
    # ------------------------------------------------------------------
    Logger.info("Step 4: Disable Device Plugin")
    patch_dp(False, 4)
    time.sleep(30)

    # ------------------------------------------------------------------
    # Step 5: DP terminated; DRA allocates GPU end-to-end
    # ------------------------------------------------------------------
    Logger.info("Step 5: Verify DP terminated, DRA is sole GPU allocator")
    assert_dp_terminated(5)
    assert_dra_running(5)
    assert_slices_exist(5)

    # DRA allocation probe: ResourceClaim + pod must reach Running with DP gone
    ret_code, _, stderr = dra_util.create_resource_claim("dp-dra-helm-probe-claim", dra_driver_namespace, "gpu.amd.com")
    debug_on_failure(environment, ret_code == 0, f"[Step 5] Failed to create ResourceClaim: {stderr}")
    init_image_repo = images.get("commonConfig.initContainerImage.repository")
    init_image_version = images.get("commonConfig.initContainerImage.version")
    debug_on_failure(environment, init_image_repo is not None and init_image_version is not None,
                     "[Step 5] commonConfig.initContainerImage not found in image manifest")
    ret_code, _, stderr = dra_util.create_pod_with_resource_claim("dp-dra-helm-probe-pod", dra_driver_namespace, "dp-dra-helm-probe-claim", image=f"{init_image_repo}:{init_image_version}", command=["sh", "-c", "sleep 60"])
    debug_on_failure(environment, ret_code == 0, f"[Step 5] Failed to create probe pod: {stderr}")
    allocated = dra_util.wait_for_resource_claim_allocation("dp-dra-helm-probe-claim", dra_driver_namespace, timeout=120)
    debug_on_failure(environment, allocated, "[Step 5] ResourceClaim not allocated — DRA kubelet plugin did not respond")
    failed = k8_util.k8_check_pod_running(dra_driver_namespace, [common.PodInfo("dp-dra-helm-probe-pod", 1, 1)], sleep_time=10)
    debug_on_failure(environment, not failed, f"[Step 5] DRA probe pod did not reach Running: {failed}")
    k8_util.k8_delete_pod("dp-dra-helm-probe-pod", dra_driver_namespace)
    dra_util.delete_resource_claim("dp-dra-helm-probe-claim", dra_driver_namespace)

    # ------------------------------------------------------------------
    # Step 6: Re-enable Device Plugin
    # ------------------------------------------------------------------
    Logger.info("Step 6: Re-enable Device Plugin")
    patch_dp(True, 6)

    # ------------------------------------------------------------------
    # Step 7: Verify DP restored, DRA still running
    # ------------------------------------------------------------------
    Logger.info("Step 7: Verify DP + DRA coexistence (DP restored)")
    assert_dp_running(7)
    for node in node_names:  # kubelet capacity is asynchronous — poll up to 90s
        capacity = None
        for _ in range(18):
            capacity, _ = k8_util.k8_get_node_gpu_capacity(node)
            if capacity and capacity > 0:
                break
            time.sleep(5)
        debug_on_failure(environment, capacity and capacity > 0, f"[Step 7] Node '{node}': amd.com/gpu capacity={capacity}, expected >0")
    assert_dra_running(7)
    assert_slices_exist(7)

    # ------------------------------------------------------------------
    # Step 8: Uninstall DRA helm chart
    # ------------------------------------------------------------------
    Logger.info("Step 8: Uninstall standalone DRA helm chart")
    ret_code, _, stderr = helm_util.helm_uninstall(gpu_cluster, dra_driver_release_name, dra_driver_namespace)
    if ret_code != 0:
        helm_util.helm_cleanup(gpu_cluster, dra_driver_release_name, dra_driver_namespace)
    time.sleep(30)

    # ------------------------------------------------------------------
    # Step 9: DRA terminated, ResourceSlices removed, DP still running
    # Note: DeviceClass may persist (cluster-scoped, not always GC'd by helm)
    # ------------------------------------------------------------------
    Logger.info("Step 9: Verify DRA terminated, ResourceSlices removed, DP still running")
    running = k8_util.k8_check_pod_terminated(dra_driver_namespace, [common.PodInfo(dra_pod_prefix, n, 1)])
    debug_on_failure(environment, not running, f"[Step 9] DRA pods still running after uninstall: {running}")
    deleted, err = dra_util.wait_for_resource_slices_deletion(dra_api_version, "gpu.amd.com")
    debug_on_failure(environment, deleted, f"[Step 9] ResourceSlices not deleted: {err}")
    assert_dp_running(9)
