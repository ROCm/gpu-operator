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

import pytest
import os
import re
import logging
import time
from lib import common
import lib.helm_util as helm_util
import lib.k8_util as k8_util
import lib.dra_util as dra_util
from lib.util import K8Helper

Logger = logging.getLogger("k8.dra-driver.conftest")


def pytest_collection_modifyitems(session, config, items):
    """
    Reorder DRA driver tests to ensure proper execution order:
    1. Install tests run first (setup)
    2. Other tests run in the middle
    3. Uninstall tests run last (cleanup)
    """
    # Separate tests into categories
    install_tests = []
    uninstall_tests = []
    other_tests = []

    for item in items:
        # Check if this is a DRA driver test (only reorder tests in this directory)
        if "dra-driver" not in item.nodeid:
            other_tests.append(item)
            continue

        # Install test should run first
        if "test_dra_driver_install" in item.nodeid and "uninstall" not in item.nodeid:
            install_tests.append(item)
        # Uninstall test should run last
        elif "test_dra_driver_uninstall" in item.nodeid:
            uninstall_tests.append(item)
        # All other tests in the middle
        else:
            other_tests.append(item)

    # Reorder: install first, then others, then uninstall last
    items[:] = install_tests + other_tests + uninstall_tests

    # Log the reordering for debugging
    if install_tests or uninstall_tests:
        Logger.info("=" * 70)
        Logger.info("DRA driver test execution order:")
        Logger.info("=" * 70)
        if install_tests:
            Logger.info(f"  FIRST: {[item.name for item in install_tests]}")
        if other_tests:
            Logger.info(
                f"  MIDDLE: {len([i for i in other_tests if 'dra-driver' in i.nodeid])} other DRA tests"
            )
        if uninstall_tests:
            Logger.info(f"  LAST: {[item.name for item in uninstall_tests]}")
        Logger.info("=" * 70)


@pytest.fixture(scope="session")
def dra_api_version(environment):
    """
    Detect and validate DRA API version based on Kubernetes version.

    This fixture runs once per test session and enforces version-specific requirements:
    - K8s 1.34+: Requires DRA v1 API (GA, enabled by default)
    - K8s 1.32-1.33: Requires DRA v1beta1 API with DynamicResourceAllocation feature gate enabled
      * On standard K8s: Validates feature gate in control plane component pods
      * On OpenShift: Skips feature gate check (control plane managed differently)
    - K8s < 1.32: Skips tests (DRA not supported)

    Returns:
        str: DRA API version (v1beta1 or v1)
    """
    global Logger

    # Check if already cached in environment
    if hasattr(environment, "dra_api_version"):
        Logger.debug(f"Using cached DRA API version: {environment.dra_api_version}")
        return environment.dra_api_version

    # Get Kubernetes version
    Logger.info("Detecting Kubernetes version and validating DRA requirements")
    ret_code, version_info = k8_util.k8_get_version()
    if ret_code != 0:
        pytest.fail("Failed to get Kubernetes version")

    major = int(version_info.get("major", 0))
    minor = int(version_info.get("minor", 0))
    Logger.info(f"Kubernetes version: {major}.{minor}")

    # Detect DRA API availability and version
    dra_available, error_msg, api_version = dra_util.check_dra_api_available()

    # K8s 1.34+: Must have v1 API (GA)
    if major > 1 or (major == 1 and minor >= 34):
        if not dra_available:
            pytest.fail(
                f"K8s {major}.{minor} requires DRA v1 API, but DRA is not available: {error_msg}"
            )
        if api_version != "v1":
            pytest.fail(
                f"K8s {major}.{minor} requires DRA v1 API (GA), but found {api_version}. "
                f"DRA should be enabled by default in K8s 1.34+. Check cluster configuration."
            )
        Logger.info(f"✓ K8s {major}.{minor} has DRA v1 API (GA) as expected")

    # K8s 1.32-1.33: Must have v1beta1 API with feature gate enabled
    elif major == 1 and minor >= 32:
        if not dra_available:
            pytest.fail(
                f"K8s {major}.{minor} requires DRA v1beta1 API with feature gate enabled, "
                f"but DRA is not available: {error_msg}"
            )
        if api_version != "v1beta1":
            pytest.fail(
                f"K8s {major}.{minor} requires DRA v1beta1 API (beta), but found {api_version}"
            )

        # Verify feature gate is enabled on control plane components
        # Skip this check on OpenShift since control plane pods are not visible in kube-system
        if environment.deployment_mode == "openshift":
            Logger.info(
                f"✓ K8s {major}.{minor} (OpenShift) has DRA v1beta1 API available. "
                f"Skipping feature gate check (control plane managed by OpenShift)."
            )
        else:
            Logger.info("Verifying DynamicResourceAllocation feature gate is enabled...")
            components = ["kube-apiserver", "kube-scheduler", "kube-controller-manager"]
            all_enabled, status, gate_error = dra_util.check_feature_gate_enabled(
                components
            )

            if not all_enabled:
                pytest.fail(
                    f"K8s {major}.{minor} with DRA v1beta1 requires DynamicResourceAllocation feature gate enabled. "
                    f"Feature gate check failed: {gate_error}. Component status: {status}. "
                    f"Enable with --feature-gates=DynamicResourceAllocation=true on kube-apiserver, "
                    f"kube-scheduler, kube-controller-manager, and kubelet. "
                    f"Also ensure --runtime-config=resource.k8s.io/v1beta1=true on kube-apiserver."
                )

            Logger.info(
                f"✓ K8s {major}.{minor} has DRA v1beta1 API with feature gate enabled on: {list(status.keys())}"
            )

    # K8s < 1.32: Skip tests (DRA not supported)
    else:
        pytest.skip(
            f"DRA requires Kubernetes 1.32+ (structured API), but cluster is running {major}.{minor}"
        )

    # Cache in environment for reuse
    setattr(environment, "dra_api_version", api_version)
    Logger.info(f"DRA API version validated and cached: {api_version}")

    return api_version


@pytest.fixture(scope="session")
def dra_driver_release_name(environment):
    return "amd-gpu-dra-driver"


@pytest.fixture(scope="session")
def dra_driver_namespace(environment):
    """DRA driver namespace"""
    return os.getenv("DRA_DRIVER_NAMESPACE", "kube-amd-gpu-dra")


@pytest.fixture(scope="session", autouse=True)
def init_dra_testbed(
    request,
    gpu_cluster,
    dra_driver_release_name,
    dra_driver_namespace,
    environment,
    dra_api_version,
):
    """Initialize DRA test environment"""
    global Logger

    def _cleanup_steps():
        # cleanup
        K8Helper.delete_debug_pods([dra_driver_namespace, "default"])

        # remove dra-driver helm-chart
        if helm_util.is_helm_chart_deployed(
            gpu_cluster, dra_driver_release_name, dra_driver_namespace
        ):
            Logger.warning(
                f"helm {dra_driver_release_name} is already deployed - cleanup"
            )
            ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(
                gpu_cluster, dra_driver_release_name, dra_driver_namespace
            )
            if ret_code != 0:
                helm_util.helm_cleanup(
                    gpu_cluster, dra_driver_release_name, dra_driver_namespace
                )

        # Clean up any remaining ResourceClaims
        dra_util.cleanup_resource_claims(dra_driver_namespace)

    # Check if user wants to skip cleanup (useful when running individual tests against existing installation)
    skip_cleanup = os.getenv("DRA_SKIP_CLEANUP", "false").lower() in (
        "true",
        "1",
        "yes",
    )

    if skip_cleanup:
        Logger.info(
            "DRA_SKIP_CLEANUP=true - Skipping cleanup, will use existing DRA driver installation"
        )
    else:
        Logger.info("Cleanup before starting DRA test session")
        _cleanup_steps()

    # Init k8 cluster for DRA testing
    k8_util.k8_init_cluster(gpu_cluster, [dra_driver_namespace])

    # DRA API version is already checked and cached by dra_api_version fixture
    Logger.info(f"DRA API version validated: {dra_api_version}")

    yield
    # NOTE: Session teardown cleanup is intentionally NOT done here to allow manual
    # inspection after running individual tests. Cleanup will happen via:
    # 1. Session setup (above) - cleans before each test session starts
    # 2. test_dra_driver_uninstall test - explicit cleanup when that test runs
    Logger.info(
        "DRA test session complete (no auto-cleanup, run test_dra_driver_uninstall to cleanup)"
    )
    return


@pytest.fixture(scope="module")
def dra_driver_install(
    gpu_cluster, dra_driver_release_name, dra_driver_namespace, images, environment, dra_api_version
):
    """Install DRA driver using Helm chart"""
    global Logger

    # Use cached DRA API version from session fixture
    Logger.info(f"Using DRA API version: {dra_api_version}")

    # Check for and clean up any existing DRA installations
    Logger.info("Checking for existing DRA driver installations")
    if helm_util.is_helm_chart_deployed(
        gpu_cluster, dra_driver_release_name, dra_driver_namespace
    ):
        Logger.info(
            f"Found existing helm release '{dra_driver_release_name}' in namespace '{dra_driver_namespace}', uninstalling"
        )
        ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(
            gpu_cluster, dra_driver_release_name, dra_driver_namespace
        )
        if ret_code != 0:
            Logger.warning(f"Helm uninstall failed, attempting cleanup: {ret_stderr}")
            helm_util.helm_cleanup(
                gpu_cluster, dra_driver_release_name, dra_driver_namespace
            )
        time.sleep(10)  # Wait for resources to be cleaned up

    # Check for orphaned DeviceClass resources from previous installations
    # kubectl equivalent: kubectl get deviceclasses.resource.k8s.io
    ret_code, device_classes, err = k8_util.k8_get_custom_resource_objects(
        group="resource.k8s.io", version=dra_api_version, plural="deviceclasses"
    )

    if ret_code != 0:
        Logger.warning(f"Failed to get DeviceClasses: {err}")
    elif device_classes:
        for dc in device_classes:
            dc_name = dc["metadata"]["name"]
            annotations = dc["metadata"].get("annotations", {})
            # Check if it's from a different helm release
            helm_release = annotations.get("meta.helm.sh/release-name", "")
            helm_namespace = annotations.get("meta.helm.sh/release-namespace", "")
            if helm_release and (
                helm_release != dra_driver_release_name
                or helm_namespace != dra_driver_namespace
            ):
                Logger.warning(
                    f"Found orphaned DeviceClass '{dc_name}' from release '{helm_release}' in namespace '{helm_namespace}', deleting"
                )
                # kubectl equivalent: kubectl delete deviceclass <dc_name>
                ret_code, stdout, stderr = k8_util.k8_delete_custom_resource(
                    group="resource.k8s.io",
                    version=dra_api_version,
                    plural="deviceclasses",
                    namespace=None,  # DeviceClass is cluster-scoped
                    name=dc_name,
                )
                if ret_code != 0:
                    Logger.warning(f"Failed to delete DeviceClass {dc_name}: {stderr}")
                time.sleep(2)

    # Install DRA driver helm chart
    dra_chart = images.get("dra-driver.helm-chart", None)
    dra_version = images.get("dra-driver.version")

    # Add helm repo if using repo:// scheme
    if images.get("dra-driver.repo-name") and images.get("dra-driver.repo"):
        helm_util.helm_add_repo(gpu_cluster, images.get("dra-driver.repo-name"), images.get("dra-driver.repo"))

    # Login to OCI registry if using oci:// scheme with a private repo
    if dra_chart and dra_chart.startswith("oci://") and images.get("dra-driver.secret"):
        secret_name = images.get("dra-driver.secret")
        # Look up credentials from k8_cluster.k8_secrets
        for entry in getattr(gpu_cluster, 'k8_secrets', {}).get("secrets", []):
            if entry.get("name") == secret_name:
                # Extract registry hostname from oci:// URL
                registry = re.match(r"oci://([a-zA-Z0-9.-]+(?::\d+)?)", dra_chart)
                if registry:
                    Logger.info(f"Logging into OCI registry: {registry.group(1)}")
                    helm_util.helm_registry_login(
                        gpu_cluster, registry.group(1),
                        entry.get("username"), entry.get("password")
                    )
                break

    Logger.info(f"DRA driver installation details:")
    Logger.info(f"  Helm chart: {dra_chart}")
    Logger.info(f"  Version: {dra_version}")
    Logger.info(f"  Release name: {dra_driver_release_name}")
    Logger.info(f"  Namespace: {dra_driver_namespace}")

    K8Helper.triage(
        environment,
        dra_chart is not None,
        "DRA driver helm chart not found in image manifest",
    )

    # Generate values.yaml for DRA driver if needed
    values_yaml = None
    # The image key is based on the 'key' field in the YAML, not the artifact name
    if images.get("image.repository.repository") or images.get(
        "dra-driver-image.repository"
    ) or images.get("draDriver.image.repository"):
        values_yaml = os.path.join(
            environment.logdir, f"dra_driver_values_{dra_version}.yaml"
        )
        dra_util.generate_dra_driver_values(images, values_yaml)
        Logger.info(f"  Values file: {values_yaml}")
        # Log the values file content
        if os.path.exists(values_yaml):
            with open(values_yaml, "r") as f:
                Logger.info(f"  Values file content:\n{f.read()}")
    else:
        Logger.info(f"  Using default values (no custom values.yaml)")

    Logger.info(f"Installing DRA driver helm chart from: {dra_chart}")
    ret_code, ret_stdout, ret_stderr = helm_util.helm_install(
        gpu_cluster,
        dra_driver_release_name,
        dra_driver_namespace,
        dra_chart,
        dra_version,
        values_yaml,
    )

    if ret_code != 0:
        Logger.error(f"Helm install failed with return code {ret_code}")
        Logger.error(f"stdout: {ret_stdout}")
        Logger.error(f"stderr: {ret_stderr}")
    K8Helper.triage(
        environment,
        ret_code == 0,
        f"Failed to install DRA driver helm chart: {ret_stderr}",
    )

    # Pod name format: {release-name}-{chart-name}-kubeletplugin
    pod_name_prefix = f"{dra_driver_release_name}-k8s-gpu-dra-driver-kubeletplugin"

    # Count GPU nodes by hardware detection (lspci via node_gpu_collector) —
    # same as the original k8_get_gpu_nodes() intent but without label dependency.
    expected_count = sum(1 for node in gpu_cluster.cluster_nodes if node.is_gpu_node())
    K8Helper.triage(
        environment,
        expected_count > 0,
        "No GPU nodes found in cluster — no AMD GPU hardware detected",
    )

    expected_pods = [
        common.PodInfo(pod_name_prefix, expected_count, 1),
    ]

    failed_pods = k8_util.k8_check_pod_running(dra_driver_namespace, expected_pods)
    K8Helper.triage(
        environment, not failed_pods, f"DRA driver pods are not ready - {failed_pods}"
    )

    yield dra_version

    # NOTE: Cleanup is intentionally NOT done here to allow manual inspection
    # after running individual tests. Cleanup will happen via:
    # 1. init_dra_testbed session fixture (before/after full test session)
    # 2. test_dra_driver_uninstall test (when explicitly run)
    Logger.info("DRA driver installation fixture complete (no auto-cleanup)")
    return
