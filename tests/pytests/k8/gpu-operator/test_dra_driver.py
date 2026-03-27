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

import pdb
import pprint
import pytest
import sys
import os
import re
import time
import json
import logging
import lib.common as common
import lib.helm_util as helm_util
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.dra_util as dra_util
from lib.util import K8Helper

Logger = logging.getLogger("k8.test_dra_driver")
LogPrettyPrinter = pprint.PrettyPrinter(indent=2)

debug_on_failure = K8Helper.triage


@pytest.fixture(autouse=True, scope="module")
def skip_module(environment):
    """
    Skip module if DRA is not supported.

    This module only checks Kubernetes version / API availability, but it doesn't
    verify that the GPU Operator version under test supports the spec.draDriver operand.
    For operator versions whose DeviceConfig template doesn't include draDriver
    (pre-v1.5.0 per spec_util.py), generate_k8_deviceconfig_cr() will drop the field
    and the tests will fail waiting for DRA pods/DeviceClass.

    Skips when:
    - Kubernetes version < 1.32 (DRA not supported)
    - DRA API not available
    - GPU Operator version < v1.5.0 (draDriver field not in DeviceConfig template)
    """
    global Logger

    # Get Kubernetes version
    ret_code, version_info = k8_util.k8_get_version()
    if ret_code != 0:
        pytest.skip("Failed to get Kubernetes version")

    # Parse version carefully - version_info['minor'] from Kubernetes VersionApi is commonly
    # a string like "32+" (e.g., on GKE/EKS), so int() can raise ValueError.
    # Parse only the numeric prefix (e.g., re.match(r'\d+', minor)) before converting to int.
    try:
        major_str = str(version_info.get("major", "0"))
        minor_str = str(version_info.get("minor", "0"))

        # Extract numeric prefix from version strings
        major_match = re.match(r"(\d+)", major_str)
        minor_match = re.match(r"(\d+)", minor_str)

        major = int(major_match.group(1)) if major_match else 0
        minor = int(minor_match.group(1)) if minor_match else 0

        Logger.info(
            f"Parsed Kubernetes version: {major}.{minor} (raw: {major_str}.{minor_str})"
        )
    except (ValueError, AttributeError) as e:
        Logger.error(f"Failed to parse Kubernetes version from {version_info}: {e}")
        pytest.skip(f"Failed to parse Kubernetes version: {e}")

    # DRA requires K8s 1.32+
    if major < 1 or (major == 1 and minor < 32):
        pytest.skip(
            f"DRA requires Kubernetes 1.32+, but cluster is running {major}.{minor}"
        )

    # Check DRA API availability
    dra_available, error_msg, api_version = dra_util.check_dra_api_available()
    if not dra_available:
        pytest.skip(f"DRA API not available: {error_msg}")

    Logger.info(f"DRA API version detected: {api_version}")

    # Check GPU Operator version supports draDriver field
    # draDriver was added in v1.5.0 (see spec_util.py device_config_template_v1_5_0)
    gpu_operator_version = getattr(environment, "gpu_operator_version", None)
    if gpu_operator_version:
        # Parse version string (e.g., "v1.5.0" -> (1, 5, 0))
        from packaging import version

        try:
            # Remove 'v' prefix if present
            version_str = gpu_operator_version.lstrip("v")
            parsed_version = version.parse(version_str)
            min_version = version.parse("1.5.0")

            if parsed_version < min_version:
                pytest.skip(
                    f"DRA driver operand requires GPU Operator v1.5.0+, "
                    f"but testing with {gpu_operator_version}. "
                    f"DeviceConfig template for this version does not include spec.draDriver field."
                )
            Logger.info(
                f"GPU Operator version {gpu_operator_version} supports DRA driver operand"
            )
        except Exception as e:
            Logger.warning(
                f"Failed to parse GPU Operator version '{gpu_operator_version}': {e}. "
                f"Proceeding with test (may fail if version < v1.5.0)"
            )
    else:
        Logger.warning(
            "GPU Operator version not found in environment. "
            "Proceeding with test (may fail if version < v1.5.0)"
        )

    return


@pytest.fixture(scope="session")
def dra_api_version(environment):
    """Detect and cache DRA API version"""
    global Logger

    # Check if already cached in environment
    if hasattr(environment, "dra_api_version"):
        Logger.debug(f"Using cached DRA API version: {environment.dra_api_version}")
        return environment.dra_api_version

    # Detect DRA API version
    dra_available, error_msg, api_version = dra_util.check_dra_api_available()
    if not dra_available:
        pytest.fail(f"DRA API not available: {error_msg}")

    # Cache in environment for reuse
    setattr(environment, "dra_api_version", api_version)
    Logger.info(f"DRA API version validated and cached: {api_version}")

    return api_version


@pytest.fixture(scope="module")
def deviceconfig_install(
    gpu_cluster, images, gpu_operator_install, environment, dra_api_version
):
    """
    Install DeviceConfig with DRA driver enabled.
    This fixture creates a DeviceConfig that enables DRA driver and disables device plugin.
    """
    global Logger

    # cleanup - remove any existing deviceconfigs
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_deviceconfig_cr(
            environment.gpu_operator_namespace, devcfg_name
        )
        if ret_code != 0:
            Logger.error(
                f"Failed to delete deviceconfig name: {devcfg_name}, error : {ret_stderr}"
            )
    time.sleep(10)

    class DeviceConfigCRInfo(object):
        pass

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(
        environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster"
    )
    debug_on_failure(
        environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster"
    )

    # Configure DeviceConfig with DRA driver enabled and device plugin disabled
    test_config = {
        "metadata.namespace": environment.gpu_operator_namespace,
        "driver.enable": True,
        "devicePlugin.enableDevicePlugin": False,  # Disable device plugin
        "draDriver.enable": True,  # Enable DRA driver
        "metricsExporter.enable": False,
        "testRunner.enable": False,
    }
    test_config.update(images)

    test_cfg_map = spec_util.build_deviceconfig_cr_template(
        test_config, gpu_nodes, "dra_driver", environment.amdgpu_driver_spec
    )
    devicecfg_list = []

    for spec_name, tcfg in test_cfg_map.items():
        cr_spec = spec_util.generate_k8_deviceconfig_cr(
            environment.gpu_operator_version, tcfg
        )
        ret_code, ret_stdout, ret_stderr = k8_util.k8_create_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment,
            (ret_code == 0),
            f"Failed to create deviceconfig, stderr: {ret_stderr}",
        )
        devicecfg_list.append(tcfg["metadata.name"])

    # Check for corresponding deviceconfig created
    K8Helper.check_deviceconfig_status(environment, devicecfg_list)
    for devcfg in devicecfg_list:
        K8Helper.wait_kmm_worker_completion(environment, devcfg)
    K8Helper.update_node_driver_version(gpu_cluster, environment)

    devcfg_info = DeviceConfigCRInfo()
    setattr(devcfg_info, "test_cfg_map", test_cfg_map)
    setattr(devcfg_info, "devicecfg_list", devicecfg_list)

    # Wait for DRA driver pods to be ready
    devicecfg_pods = [
        common.PodInfo("dra-driver", len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20
    )
    debug_on_failure(
        environment,
        (not failed_pods),
        f"One or more pods are not ready - {failed_pods}",
    )

    yield devcfg_info

    # Cleanup
    device_cfg_info = k8_util.k8_get_deviceconfigs_info(
        environment.gpu_operator_namespace, None
    )
    for devcfg_name, _ in device_cfg_info.items():
        k8_util.k8_delete_deviceconfig_cr(
            environment.gpu_operator_namespace, devcfg_name
        )

    # Clean up ResourceClaims if any
    dra_util.cleanup_resource_claims(environment.gpu_operator_namespace)
    return


@pytest.mark.level1
def test_deviceconfig_dra_driver_deploy(
    deviceconfig_install, gpu_cluster, environment, dra_api_version
):
    """
    Test DRA driver operand deployment.
    Verifies that DRA driver pods are running and DeviceClass/ResourceSlices are created.
    """
    global Logger

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(
        environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster"
    )
    debug_on_failure(
        environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster"
    )

    # Watch for DRA driver pod creation
    devicecfg_pods = [
        common.PodInfo("dra-driver", len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20
    )
    debug_on_failure(
        environment,
        (not failed_pods),
        f"One or more DRA driver pods are not ready - {failed_pods}",
    )

    Logger.info("DRA driver pods are running successfully")

    # Verify DeviceClass exists
    ret_code, device_classes, err = k8_util.k8_get_custom_resource_objects(
        group="resource.k8s.io", version=dra_api_version, plural="deviceclasses"
    )
    debug_on_failure(
        environment, (ret_code == 0), f"Failed to get DeviceClasses: {err}"
    )

    # Check for gpu.amd.com DeviceClass
    device_class_names = (
        [dc["metadata"]["name"] for dc in device_classes] if device_classes else []
    )
    debug_on_failure(
        environment,
        ("gpu.amd.com" in device_class_names),
        f"DeviceClass 'gpu.amd.com' not found. Available DeviceClasses: {device_class_names}",
    )

    Logger.info("DeviceClass 'gpu.amd.com' exists")

    # Verify ResourceSlices are published
    ret_code, resource_slices, err = k8_util.k8_get_custom_resource_objects(
        group="resource.k8s.io", version=dra_api_version, plural="resourceslices"
    )
    debug_on_failure(
        environment, (ret_code == 0), f"Failed to get ResourceSlices: {err}"
    )

    # Filter ResourceSlices for gpu.amd.com driver
    gpu_resource_slices = (
        [
            rs
            for rs in resource_slices
            if rs.get("spec", {}).get("driver") == "gpu.amd.com"
        ]
        if resource_slices
        else []
    )
    debug_on_failure(
        environment,
        (len(gpu_resource_slices) > 0),
        f"No ResourceSlices found for driver 'gpu.amd.com'. Total ResourceSlices: {len(resource_slices)}",
    )

    Logger.info(
        f"Found {len(gpu_resource_slices)} ResourceSlices published by DRA driver"
    )

    # Log ResourceSlice details for debugging
    for rs in gpu_resource_slices:
        rs_name = rs["metadata"]["name"]
        node_name = rs.get("spec", {}).get("nodeName", "N/A")
        pool = rs.get("spec", {}).get("pool", {})
        Logger.info(
            f"ResourceSlice: {rs_name}, Node: {node_name}, Pool: {pool.get('name', 'N/A')}"
        )


@pytest.mark.level1
def test_deviceconfig_dra_driver_disable(
    deviceconfig_install, environment, dra_api_version
):
    """
    Test disabling and re-enabling DRA driver operand.
    Verifies that DRA driver pods are terminated when disabled and restarted when enabled.
    """
    global Logger

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(
        environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster"
    )
    debug_on_failure(
        environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster"
    )

    # Disable DRA driver
    Logger.info("Disabling DRA driver via DeviceConfig")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["draDriver.enable"] = False
        cr_spec = spec_util.generate_k8_deviceconfig_cr(
            environment.gpu_operator_version, tcfg
        )
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment,
            (ret_code == 0),
            "Failed to modify deviceconfig CR to disable DRA driver",
        )

    # Verify DRA driver pods are terminated
    dra_pods = [
        common.PodInfo("dra-driver", 1, 1),
    ]
    running_pods = k8_util.k8_check_pod_terminated(
        environment.gpu_operator_namespace, dra_pods
    )
    debug_on_failure(
        environment,
        not running_pods,
        f"DRA driver pods are still running after disabling - {running_pods}",
    )

    Logger.info("DRA driver pods terminated successfully after disabling")

    # Verify ResourceSlices are deleted after disabling DRA driver
    # ResourceSlices may take some time to be cleaned up, so retry with timeout
    Logger.info("Verifying ResourceSlices are cleaned up after disabling DRA driver")
    max_retries = 12  # 12 retries * 5 seconds = 60 seconds max wait
    retry_interval = 5
    resource_slices_deleted = False

    for attempt in range(max_retries):
        ret_code, resource_slices, err = k8_util.k8_get_custom_resource_objects(
            group="resource.k8s.io", version=dra_api_version, plural="resourceslices"
        )

        if ret_code != 0:
            Logger.warning(
                f"Failed to get ResourceSlices (attempt {attempt + 1}/{max_retries}): {err}"
            )
            time.sleep(retry_interval)
            continue

        # Filter for AMD GPU ResourceSlices
        gpu_resource_slices = (
            [
                rs
                for rs in resource_slices
                if rs.get("spec", {}).get("driver") == "gpu.amd.com"
            ]
            if resource_slices
            else []
        )

        if len(gpu_resource_slices) == 0:
            Logger.info(
                f"ResourceSlices successfully deleted (attempt {attempt + 1}/{max_retries})"
            )
            resource_slices_deleted = True
            break
        else:
            Logger.info(
                f"Waiting for ResourceSlices deletion (attempt {attempt + 1}/{max_retries}): "
                f"{len(gpu_resource_slices)} AMD GPU ResourceSlices still exist"
            )
            time.sleep(retry_interval)

    debug_on_failure(
        environment,
        resource_slices_deleted,
        f"ResourceSlices were not deleted after disabling DRA driver within {max_retries * retry_interval} seconds",
    )

    # Re-enable DRA driver
    Logger.info("Re-enabling DRA driver via DeviceConfig")
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg["draDriver.enable"] = True
        cr_spec = spec_util.generate_k8_deviceconfig_cr(
            environment.gpu_operator_version, tcfg
        )
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(
            environment,
            (ret_code == 0),
            "Failed to modify deviceconfig CR to re-enable DRA driver",
        )

    # Verify DRA driver pods are running again
    devicecfg_pods = [
        common.PodInfo("dra-driver", len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(
        environment.gpu_operator_namespace, devicecfg_pods, sleep_time=20
    )
    debug_on_failure(
        environment,
        (not failed_pods),
        f"One or more DRA driver pods are not ready after re-enabling - {failed_pods}",
    )

    Logger.info("DRA driver pods restarted successfully after re-enabling")

    # Verify ResourceSlices are published again
    ret_code, resource_slices, err = k8_util.k8_get_custom_resource_objects(
        group="resource.k8s.io", version=dra_api_version, plural="resourceslices"
    )
    debug_on_failure(
        environment,
        (ret_code == 0),
        f"Failed to get ResourceSlices after re-enabling: {err}",
    )

    gpu_resource_slices = (
        [
            rs
            for rs in resource_slices
            if rs.get("spec", {}).get("driver") == "gpu.amd.com"
        ]
        if resource_slices
        else []
    )
    debug_on_failure(
        environment,
        (len(gpu_resource_slices) > 0),
        f"No ResourceSlices found after re-enabling DRA driver",
    )

    Logger.info(
        f"ResourceSlices restored after re-enabling: {len(gpu_resource_slices)} slices found"
    )
