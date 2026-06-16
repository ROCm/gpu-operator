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
DRA (Dynamic Resource Allocation) utility functions for AMD GPU Kubernetes testing
"""

import os
import pdb
import time
import json
import logging
import pytest
import yaml
from typing import List, Dict, Optional, Tuple
from kubernetes import client, config
from kubernetes.client.rest import ApiException
import lib.k8_util as k8_util

Logger = logging.getLogger("lib.dra_util")

# DRA API Group
DRA_API_GROUP = "resource.k8s.io"

# We only support structured API: v1beta1 (K8s 1.32), v1beta2 (K8s 1.33+/OpenShift 4.20+), or v1 (K8s 1.34+)
# The older opaque API (v1alpha2, v1alpha3) is not supported
# API version is determined dynamically at runtime via check_dra_api_available()

# Module-level cache for DRA API version to avoid repeated detection
_DRA_API_VERSION_CACHE = ""


def check_feature_gate_enabled(
    component_names: List[str],
) -> Tuple[bool, Dict[str, bool], str]:
    """
    Check if DynamicResourceAllocation feature gate is enabled on Kubernetes components.

    This checks the command-line arguments of control plane components to verify
    that --feature-gates=DynamicResourceAllocation=true is set.

    Equivalent kubectl commands:
        kubectl get pod kube-apiserver-<node> -n kube-system -o yaml
        kubectl get pod kube-scheduler-<node> -n kube-system -o yaml
        kubectl get pod kube-controller-manager-<node> -n kube-system -o yaml

    Args:
        component_names: List of component names to check (e.g., ['kube-apiserver', 'kube-scheduler'])

    Returns:
        Tuple of (all_enabled, status_dict, error_message):
            - all_enabled: True if feature gate is enabled on all components
            - status_dict: Dict mapping component name to enabled status
            - error_message: Error message if any component is missing the feature gate
    """
    global Logger

    status = {}
    errors = []

    try:
        v1 = client.CoreV1Api()

        # Get all pods in kube-system namespace
        pods = v1.list_namespaced_pod(namespace="kube-system")

        for component in component_names:
            found = False
            enabled = False

            # Find pods matching the component name
            for pod in pods.items:
                if pod.metadata.name.startswith(component):
                    found = True

                    # Check command-line arguments
                    if pod.spec.containers:
                        container = pod.spec.containers[0]
                        command_args = container.command or []
                        command_args.extend(container.args or [])

                        # Look for --feature-gates argument
                        for arg in command_args:
                            if arg.startswith("--feature-gates="):
                                feature_gates_str = arg.split("=", 1)[1]
                                # Parse feature gates (format: "Gate1=true,Gate2=false,...")
                                feature_gates = {}
                                for gate in feature_gates_str.split(","):
                                    if "=" in gate:
                                        gate_name, gate_value = gate.split("=", 1)
                                        feature_gates[gate_name.strip()] = (
                                            gate_value.strip().lower() == "true"
                                        )

                                # Check if DynamicResourceAllocation is enabled
                                if feature_gates.get(
                                    "DynamicResourceAllocation", False
                                ):
                                    enabled = True
                                    Logger.info(
                                        f"Feature gate DynamicResourceAllocation is enabled on {component}"
                                    )
                                else:
                                    Logger.warning(
                                        f"Feature gate DynamicResourceAllocation not found or disabled on {component}"
                                    )
                                break
                    break

            if not found:
                Logger.warning(
                    f"Could not find {component} pod in kube-system namespace"
                )
                errors.append(f"{component} pod not found")
            elif not enabled:
                errors.append(
                    f"{component} missing --feature-gates=DynamicResourceAllocation=true"
                )

            status[component] = enabled

        all_enabled = all(status.values())
        error_msg = "; ".join(errors) if errors else ""

        return all_enabled, status, error_msg

    except ApiException as e:
        error_msg = f"Failed to check feature gates: {e}"
        Logger.error(error_msg)
        return False, {}, error_msg
    except Exception as e:
        error_msg = f"Unexpected error checking feature gates: {e}"
        Logger.error(error_msg)
        return False, {}, error_msg


def check_dra_api_available() -> Tuple[bool, str, str]:
    """
    Check if DRA structured API is available in the cluster and return the version.

    We only support the newer "structured API" DRA which went beta in K8s 1.32
    and GA in K8s 1.34. The older "opaque API" is not supported.

    This function queries the Kubernetes API server directly to determine which
    DRA API version is available, preferring v1 (GA) over v1beta1 (beta).

    Equivalent kubectl commands:
        # Check available API versions
        kubectl api-versions | grep resource.k8s.io

        # List API resources for the group
        kubectl api-resources --api-group=resource.k8s.io

        # List DeviceClasses to validate
        kubectl get deviceclasses.resource.k8s.io

    Returns:
        tuple: (bool, str, str) - (success, error_message, api_version)
            - success: True if DRA API is available, False otherwise
            - error_message: Error message if not available, empty string otherwise
            - api_version: DRA API version (v1beta1, v1beta2, or v1), empty string if not available
    """
    global Logger

    try:
        # Query available API groups and versions from the cluster
        # This is more robust than inferring from K8s version
        api_client = client.ApiClient()
        apis_api = client.ApisApi(api_client)
        api_groups = apis_api.get_api_versions()

        # Look for resource.k8s.io group and check available versions
        dra_versions = []
        for group in api_groups.groups:
            if group.name == DRA_API_GROUP:
                dra_versions = [v.version for v in group.versions]
                Logger.info(
                    f"Found DRA API group '{DRA_API_GROUP}' with versions: {dra_versions}"
                )
                break

        if not dra_versions:
            # Get K8s version to provide helpful error message
            ret_code, version_info = k8_util.k8_get_version()
            if ret_code == 0:
                major = version_info.get("major", "?")
                minor = version_info.get("minor", "?")
                error_msg = f"DRA API group '{DRA_API_GROUP}' not found in cluster (K8s {major}.{minor}). "

                # Provide version-specific guidance
                try:
                    if int(str(minor)) >= 32 and int(str(minor)) <= 33:
                        error_msg += "For K8s 1.32-1.33, ensure DynamicResourceAllocation feature gate is enabled on all components (kube-apiserver, kube-controller-manager, kube-scheduler, kubelet) with --feature-gates=DynamicResourceAllocation=true and --runtime-config=resource.k8s.io/v1beta1=true"
                    elif int(str(minor)) < 32:
                        error_msg += "DRA requires Kubernetes 1.32+ (currently using older version)"
                    else:
                        error_msg += "DRA should be available by default in this version. Check cluster configuration."
                except (ValueError, TypeError):
                    pass
            else:
                error_msg = f"DRA API group '{DRA_API_GROUP}' not found in cluster"

            Logger.error(error_msg)
            return False, error_msg, ""

        # Prefer v1 (GA) over v1beta2 over v1beta1 (beta)
        # Filter to only structured API versions we support
        if "v1" in dra_versions:
            dra_api_version = "v1"
            Logger.info("Using DRA v1 API (GA, enabled by default in K8s 1.34+)")
        elif "v1beta2" in dra_versions:
            dra_api_version = "v1beta2"
            Logger.info(
                "Using DRA v1beta2 API (Beta). Available in OpenShift 4.20+ and K8s 1.33+"
            )
        elif "v1beta1" in dra_versions:
            dra_api_version = "v1beta1"
            Logger.info(
                "Using DRA v1beta1 API (Beta, K8s 1.32). "
                "DynamicResourceAllocation feature gate is enabled."
            )
        else:
            # Check if only older opaque API versions are available
            unsupported_msg = f"Only unsupported DRA API versions found: {dra_versions}. Requires v1beta1 (K8s 1.32+), v1beta2 (K8s 1.33+/OpenShift 4.20+), or v1 (K8s 1.34+)"
            Logger.error(unsupported_msg)
            return False, unsupported_msg, ""

        Logger.info(f"Using DRA API version {dra_api_version}")

        # Validate by trying to list DeviceClasses
        # kubectl equivalent: kubectl get deviceclasses.resource.k8s.io
        ret_code, device_classes, err = k8_util.k8_get_custom_resource_objects(
            group=DRA_API_GROUP, version=dra_api_version, plural="deviceclasses"
        )

        if ret_code != 0:
            error_msg = f"Failed to list DeviceClasses with {dra_api_version}: {err}"
            Logger.error(error_msg)
            return False, error_msg, ""

        Logger.info(
            f"DRA API (DeviceClass) is available and validated with version {dra_api_version}"
        )
        return True, "", dra_api_version

    except Exception as e:
        error_msg = f"Failed to query DRA API availability: {e}"
        Logger.error(error_msg)
        return False, error_msg, ""


def get_dra_api_version(environment=None) -> str:
    """
    Determine the DRA structured API version available in the cluster.

    This function checks for a cached version (module-level or environment object)
    to avoid repeated API detection and logging.

    Args:
        environment: Optional test environment object that may have cached dra_api_version

    Returns: API version string (v1beta1 or v1), or empty string if not available
    """
    global _DRA_API_VERSION_CACHE

    # Check module-level cache first
    if _DRA_API_VERSION_CACHE:
        return _DRA_API_VERSION_CACHE

    # Check if version is cached in environment
    if environment and hasattr(environment, "dra_api_version"):
        _DRA_API_VERSION_CACHE = environment.dra_api_version
        return environment.dra_api_version

    # Otherwise detect it and cache
    _, _, api_version = check_dra_api_available()
    _DRA_API_VERSION_CACHE = api_version
    return api_version


# Note: ResourceClass creation/deletion functions removed (create_resource_class, delete_resource_class).
# DRA API evolved from ResourceClass (v1alpha2) to DeviceClass (v1alpha3/v1).
# Tests now use the default 'gpu.amd.com' DeviceClass created by Helm installation.
# If you need to create custom DeviceClass objects for testing, use kubectl directly
# or k8_util.k8_create_custom_resource() with the appropriate DeviceClass manifest.


def create_resource_claim(
    name: str,
    namespace: str,
    resource_class: str,
    device_count: int = 1,
) -> Tuple[int, str, str]:
    """
    Create a ResourceClaim

    Equivalent kubectl command (uses detected DRA API version):
        kubectl apply -f - <<EOF
        apiVersion: resource.k8s.io/<detected-version>  # v1beta1 or v1
        kind: ResourceClaim
        metadata:
          name: <name>
          namespace: <namespace>
        spec:
          devices:
            requests:
              - name: gpu-request
                exactly:
                  deviceClassName: <resource_class>
                  allocationMode: ExactCount
                  count: <device_count>
        EOF

    Note: API version is auto-detected via get_dra_api_version().
    In DRA v1, allocationMode values are:
    - ExactCount: Request exact count of devices
    - All: Request all available devices

    This is different from PV's WaitForFirstConsumer/Immediate modes.

    For node affinity, use Pod nodeSelector instead of ResourceClaim constraints.
    Pin the Pod to a specific node, and DRA will allocate from that node's devices.

    Args:
        name: Name of the ResourceClaim
        namespace: Namespace for the ResourceClaim
        resource_class: Name of the DeviceClass to use (e.g., 'gpu.amd.com')
        device_count: Number of GPU devices to request (default: 1)

    Returns:
        Tuple of (return_code, stdout, stderr)
    """
    global Logger

    # Build device requests according to the detected API version:
    # - v1beta1 (K8s 1.32): flat fields — deviceClassName/allocationMode/count directly on request
    # - v1 (K8s 1.33+): fields nested under 'exactly' (or 'firstAvailable') discriminated union
    api_version = get_dra_api_version()
    if api_version == "v1":
        device_requests = [
            {
                "name": "gpu-request",
                "exactly": {
                    "deviceClassName": resource_class,
                    "allocationMode": "ExactCount",
                    "count": device_count,
                },
            }
        ]
    else:
        # v1beta1
        device_requests = [
            {
                "name": "gpu-request",
                "deviceClassName": resource_class,
                "allocationMode": "ExactCount",
                "count": device_count,
            }
        ]

    resource_claim = {
        "apiVersion": f"{DRA_API_GROUP}/{get_dra_api_version()}",
        "kind": "ResourceClaim",
        "metadata": {"name": name, "namespace": namespace},
        "spec": {
            "devices": {
                "requests": device_requests,
            },
        },
    }

    # Log the actual spec for debugging
    Logger.debug(
        f"Creating ResourceClaim with spec:\n{json.dumps(resource_claim, indent=2)}"
    )

    # Use existing k8_util helper for creating custom resources
    ret_code, stdout, stderr = k8_util.k8_create_custom_resource(resource_claim)
    if ret_code == 0:
        Logger.info(
            f"Created ResourceClaim: {name} requesting {device_count} GPU(s) in namespace {namespace}"
        )
    else:
        Logger.error(f"Failed to create ResourceClaim {name}: {stderr}")
        Logger.error(f"ResourceClaim spec was:\n{json.dumps(resource_claim, indent=2)}")
    return ret_code, stdout, stderr


def get_resource_claim(name: str, namespace: str) -> Optional[Dict]:
    """
    Get ResourceClaim details

    Equivalent kubectl command:
        kubectl get resourceclaim <name> -n <namespace> -o yaml

    Args:
        name: Name of the ResourceClaim
        namespace: Namespace of the ResourceClaim

    Returns:
        ResourceClaim object or None
    """
    global Logger

    ret_code, result, err = k8_util.k8_get_namespaced_custom_resource(
        group=DRA_API_GROUP,
        version=get_dra_api_version(),
        namespace=namespace,
        plural="resourceclaims",
        name=name,
    )

    if ret_code == 0:
        return result
    else:
        Logger.error(f"Failed to get ResourceClaim {name}: {err}")
        return None


def delete_resource_claim(name: str, namespace: str) -> Tuple[int, str, str]:
    """
    Delete a ResourceClaim

    Equivalent kubectl command:
        kubectl delete resourceclaim <name> -n <namespace>

    Args:
        name: Name of the ResourceClaim
        namespace: Namespace of the ResourceClaim

    Returns:
        Tuple of (return_code, stdout, stderr)
    """
    global Logger

    # Use existing k8_util helper for deleting custom resources
    ret_code, stdout, stderr = k8_util.k8_delete_custom_resource(
        group=DRA_API_GROUP,
        version=get_dra_api_version(),
        plural="resourceclaims",
        namespace=namespace,
        name=name,
    )
    if ret_code == 0:
        Logger.info(f"Deleted ResourceClaim: {name} from namespace {namespace}")
    else:
        Logger.error(f"Failed to delete ResourceClaim {name}: {stderr}")
    return ret_code, stdout, stderr


def list_resource_claims(namespace: str = None) -> List[Dict]:
    """
    List ResourceClaims in a namespace or cluster-wide

    Equivalent kubectl command:
        kubectl get resourceclaims -n <namespace>          # for specific namespace
        kubectl get resourceclaims --all-namespaces        # for all namespaces

    Args:
        namespace: Namespace to list claims from (None for all namespaces)

    Returns:
        List of ResourceClaim objects
    """
    global Logger

    try:
        if namespace:
            # For namespaced resources, use direct API call
            custom_api = client.CustomObjectsApi()
            result = custom_api.list_namespaced_custom_object(
                group=DRA_API_GROUP,
                version=get_dra_api_version(),
                namespace=namespace,
                plural="resourceclaims",
            )
            return result.get("items", [])
        else:
            # For cluster-wide resources, use k8_util method
            ret_code, items, err = k8_util.k8_get_custom_resource_objects(
                group=DRA_API_GROUP,
                version=get_dra_api_version(),
                plural="resourceclaims",
            )
            if ret_code == 0:
                return items
            else:
                Logger.error(f"Failed to list ResourceClaims: {err}")
                return []
    except ApiException as e:
        Logger.error(f"Failed to list ResourceClaims: {e}")
        return []


def cleanup_resource_claims(namespace: str = None) -> None:
    """
    Clean up all ResourceClaims in a namespace

    Equivalent kubectl command:
        kubectl delete resourceclaims --all -n <namespace>           # for specific namespace
        kubectl delete resourceclaims --all --all-namespaces         # for all namespaces

    Args:
        namespace: Namespace to clean up (None for all namespaces)
    """
    global Logger

    claims = list_resource_claims(namespace)
    for claim in claims:
        claim_name = claim["metadata"]["name"]
        claim_namespace = claim["metadata"]["namespace"]
        delete_resource_claim(claim_name, claim_namespace)
        Logger.info(f"Cleaned up ResourceClaim: {claim_name} in {claim_namespace}")


def wait_for_resource_claim_allocation(
    name: str, namespace: str, timeout: int = 120
) -> bool:
    """
    Wait for a ResourceClaim to be allocated
    
    Equivalent kubectl command:
        kubectl wait --for=jsonpath='{.status.allocation}' \
            resourceclaim/<name> -n <namespace> --timeout=<timeout>s

    Args:
        name: Name of the ResourceClaim
        namespace: Namespace of the ResourceClaim
        timeout: Timeout in seconds

    Returns:
        True if allocated, False otherwise
    """
    global Logger

    start_time = time.time()
    while time.time() - start_time < timeout:
        claim = get_resource_claim(name, namespace)
        if claim and claim.get("status", {}).get("allocation"):
            Logger.info(f"ResourceClaim {name} is allocated")
            return True
        time.sleep(5)

    Logger.error(f"ResourceClaim {name} allocation timed out after {timeout}s")
    return False


def create_pod_with_resource_claim(
    pod_name: str,
    namespace: str,
    resource_claim_name: str,
    image: str = "rocm/pytorch:latest",
    command: Optional[List[str]] = None,
    wait_for_running: bool = False,
    node_selector: Optional[Dict[str, str]] = None,
) -> Tuple[int, str, str]:
    """
    Create a Pod that uses a ResourceClaim

    Equivalent kubectl command (example uses detected DRA API version):
        kubectl apply -f - <<EOF
        apiVersion: v1
        kind: Pod
        metadata:
          name: <pod_name>
          namespace: <namespace>
        spec:
          nodeSelector:
            kubernetes.io/hostname: <node-name>
          resourceClaims:
          - name: gpu-claim
            resourceClaimName: <resource_claim_name>  # Direct reference (v1/v1beta1)
          containers:
          - name: gpu-container
            image: <image>
            command: <command>
            resources:
              claims:
              - name: gpu-claim
        EOF

    Note: The resourceClaims format uses resourceClaimName directly (no 'source' wrapper)
    for both DRA v1 and v1beta1 APIs when referencing an existing claim.

    Args:
        pod_name: Name of the Pod
        namespace: Namespace for the Pod
        resource_claim_name: Name of the ResourceClaim to use
        image: Container image to use
        command: Command to run in the container
        wait_for_running: Wait for pod to reach Running state
        node_selector: Optional node selector dict (e.g., {"kubernetes.io/hostname": "node-name"})

    Returns:
        Tuple of (return_code, stdout, stderr)
    """
    global Logger

    if command is None:
        command = ["sleep", "infinity"]

    pod_spec = {
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {"name": pod_name, "namespace": namespace},
        "spec": {
            "restartPolicy": "Never",
            "resourceClaims": [
                {
                    "name": "gpu-claim",
                    "resourceClaimName": resource_claim_name,
                }
            ],
            "containers": [
                {
                    "name": "gpu-container",
                    "image": image,
                    "command": command,
                    "resources": {"claims": [{"name": "gpu-claim"}]},
                }
            ],
        },
    }

    # Add nodeSelector if provided
    if node_selector:
        pod_spec["spec"]["nodeSelector"] = node_selector

    # Log the actual spec for debugging
    Logger.debug(f"Creating Pod with spec:\n{json.dumps(pod_spec, indent=2)}")

    try:
        v1 = client.CoreV1Api()
        result = v1.create_namespaced_pod(namespace=namespace, body=pod_spec)
        Logger.info(f"Created Pod {pod_name} with ResourceClaim {resource_claim_name}")

        # Optionally wait for pod to be running
        if wait_for_running:
            ret_code = k8_util.k8_check_pod_running(
                namespace=namespace,
                pod_list=[pod_name],
                sleep_time=10,
                total_attempts=30,
            )
            if ret_code != 0:
                Logger.error(f"Pod {pod_name} failed to reach Running state")
                return ret_code, "", "Pod failed to reach Running state"

        return 0, json.dumps(result.to_dict(), default=str), ""

    except ApiException as e:
        Logger.error(f"Failed to create Pod {pod_name}: {e}")
        Logger.error(f"Pod spec was:\n{json.dumps(pod_spec, indent=2)}")
        return -1, "", str(e)


def generate_dra_driver_values(images: Dict, output_file: str) -> bool:
    """
    Generate Helm values.yaml for DRA driver

    Equivalent Helm command:
        helm install <release-name> <chart> --values <output_file>

    Note: This function generates the values file; no direct kubectl equivalent.

    Args:
        images: Image configuration dictionary
        output_file: Path to output values.yaml file

    Returns:
        True if successful, False otherwise
    """
    global Logger

    values = {}

    # Add image configuration
    # The key structure in images dict is based on the 'key' field in YAML
    # For dra-driver-image with key 'image.repository', it becomes 'image.repository.repository'
    # For dra-driver-image with key 'draDriver.image', it becomes 'draDriver.image.repository'
    image_repo = (
        images.get("image.repository.repository")
        or images.get("dra-driver-image.repository")
        or images.get("dra-driver.image.repository")
        or images.get("draDriver.image.repository")  # Support camelCase key from manifest
    )
    image_tag = (
        images.get("image.repository.version")
        or images.get("dra-driver-image.version")
        or images.get("dra-driver.image.version")
        or images.get("draDriver.image.version", "latest")  # Support camelCase key from manifest
    )

    if image_repo:
        values["image"] = {
            "repository": image_repo,
            "tag": image_tag,
            "pullPolicy": "IfNotPresent",
        }

    # Add image pull secret if specified
    image_secret = (
        images.get("image.repository.secret")
        or images.get("dra-driver-image.secret")
        or images.get("dra-driver.image.secret")
        or images.get("draDriver.image.secret")  # Support camelCase key from manifest
    )
    if image_secret:
        values["imagePullSecrets"] = [{"name": image_secret}]

    # Add other DRA driver specific configurations
    values["deviceClass"] = {"name": "gpu.amd.com"}

    try:
        # Ensure the directory exists
        output_dir = os.path.dirname(output_file)
        if output_dir:
            os.makedirs(output_dir, exist_ok=True)

        with open(output_file, "w") as f:
            yaml.dump(values, f, default_flow_style=False)
        Logger.info(f"Generated DRA driver values.yaml: {output_file}")
        return True
    except Exception as e:
        Logger.error(f"Failed to generate values.yaml: {e}")
        return False


def get_dra_device_allocations(namespace: str = None) -> Dict[str, List[str]]:
    """
    Get GPU device allocations from DRA ResourceClaims
    
    Equivalent kubectl command:
        kubectl get resourceclaims -n <namespace> \
            -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.allocation.devices}{"\n"}{end}'

    Args:
        namespace: Namespace to check (None for all namespaces)

    Returns:
        Dictionary mapping claim names to allocated device IDs
    """
    global Logger

    allocations = {}
    claims = list_resource_claims(namespace)

    for claim in claims:
        claim_name = claim["metadata"]["name"]
        if claim.get("status", {}).get("allocation"):
            # Extract device information from allocation
            allocation = claim["status"]["allocation"]
            devices = []
            # Parse allocation details (structure depends on DRA driver implementation)
            if "devices" in allocation:
                devices = allocation["devices"]
            allocations[claim_name] = devices

    return allocations


def verify_dra_driver_crds() -> Tuple[bool, List[str]]:
    """
    Verify that DRA resources are available (either as CRDs or built-in)

    Equivalent kubectl command (K8s 1.26-1.33 with CRDs):
        kubectl get crds | grep resource.k8s.io
        kubectl get crd resourceclaims.resource.k8s.io
        kubectl get crd resourceclasses.resource.k8s.io
        kubectl get crd resourceclaimtemplates.resource.k8s.io

    For K8s 1.34+ (built-in resources):
        kubectl api-resources | grep resource.k8s.io
        kubectl get resourceclaims --all-namespaces
        kubectl get deviceclasses  # Both v1beta1 and v1 use DeviceClass

    Returns:
        Tuple of (success, list of unavailable resources)
    """
    global Logger

    # In K8s 1.34+, these are built-in resources, not CRDs
    # We'll check if the API is available instead
    api_version = get_dra_api_version()

    unavailable = []

    # Check ResourceClaims - Use existing k8_util helper
    ret_code, items, err = k8_util.k8_get_custom_resource_objects(
        group=DRA_API_GROUP, version=api_version, plural="resourceclaims"
    )
    if ret_code == 0:
        Logger.info("ResourceClaim API is available")
    else:
        unavailable.append("resourceclaims.resource.k8s.io")
        Logger.error(f"ResourceClaim API not available: {err}")

    # Check DeviceClasses - Both v1beta1 and v1 use "deviceclasses"
    # (The older opaque API used "resourceclasses" but we don't support that)
    ret_code, items, err = k8_util.k8_get_custom_resource_objects(
        group=DRA_API_GROUP, version=api_version, plural="deviceclasses"
    )
    if ret_code == 0:
        Logger.info("DeviceClass API is available")
    else:
        unavailable.append("deviceclasses.resource.k8s.io")
        Logger.error(f"DeviceClass API not available: {err}")

    return len(unavailable) == 0, unavailable


# =============================================================================
# DRA Test Helper Functions
# Shared by k8/dra-driver/ and k8/gpu-operator/ test suites
# =============================================================================


def verify_dra_driver_pods_running(
    namespace: str,
    expected_pod_count: int,
    pod_name_pattern: str = "dra-driver",
    sleep_time: int = 20,
) -> Tuple[bool, str]:
    """
    Verify DRA driver pods are running.
    Shared helper for both standalone and operand DRA driver tests.

    Args:
        namespace: Namespace where DRA driver pods are deployed
        expected_pod_count: Expected number of DRA driver pods (usually number of GPU nodes)
        pod_name_pattern: Pattern to match pod names (default: "dra-driver")
        sleep_time: Time to wait for pods to stabilize

    Returns:
        Tuple of (success, error_message)

    Example:
        success, err = verify_dra_driver_pods_running("kube-amd-gpu-dra", 2)
        if not success:
            pytest.fail(f"DRA driver pods not ready: {err}")
    """
    global Logger
    import lib.common as common

    expected_pods = [common.PodInfo(pod_name_pattern, expected_pod_count, 1)]
    failed_pods = k8_util.k8_check_pod_running(
        namespace, expected_pods, sleep_time=sleep_time
    )

    if failed_pods:
        return False, f"DRA driver pods not ready: {failed_pods}"

    Logger.info(
        f"All {expected_pod_count} DRA driver pods are running in namespace {namespace}"
    )
    return True, ""


def verify_device_class_exists(
    api_version: str, device_class_name: str = "gpu.amd.com"
) -> Tuple[bool, str, Optional[Dict]]:
    """
    Verify that a DeviceClass exists.

    Args:
        api_version: DRA API version (v1 or v1beta1)
        device_class_name: Name of the DeviceClass to check

    Returns:
        Tuple of (exists, error_message, device_class_object)

    Example:
        exists, err, dc = verify_device_class_exists("v1", "gpu.amd.com")
        if not exists:
            pytest.fail(f"DeviceClass not found: {err}")
    """
    global Logger

    ret_code, device_classes, err = k8_util.k8_get_custom_resource_objects(
        group=DRA_API_GROUP, version=api_version, plural="deviceclasses"
    )

    if ret_code != 0:
        return False, f"Failed to get DeviceClasses: {err}", None

    # Find the specific DeviceClass
    for dc in device_classes:
        if dc.get("metadata", {}).get("name") == device_class_name:
            Logger.info(f"Found DeviceClass: {device_class_name}")
            return True, "", dc

    return False, f"DeviceClass '{device_class_name}' not found", None


def verify_resource_slices_exist(
    api_version: str, driver_name: str = "gpu.amd.com", min_count: int = 1
) -> Tuple[bool, str, List[Dict]]:
    """
    Verify that ResourceSlices exist for a given driver.

    Args:
        api_version: DRA API version (v1 or v1beta1)
        driver_name: Driver name to filter ResourceSlices (default: "gpu.amd.com")
        min_count: Minimum expected number of ResourceSlices

    Returns:
        Tuple of (success, error_message, resource_slices_list)

    Example:
        success, err, slices = verify_resource_slices_exist("v1", "gpu.amd.com", min_count=2)
        if not success:
            pytest.fail(f"ResourceSlices check failed: {err}")
    """
    global Logger

    ret_code, resource_slices, err = k8_util.k8_get_custom_resource_objects(
        group=DRA_API_GROUP, version=api_version, plural="resourceslices"
    )

    if ret_code != 0:
        return False, f"Failed to get ResourceSlices: {err}", []

    # Filter for specific driver
    driver_resource_slices = (
        [
            rs
            for rs in resource_slices
            if rs.get("spec", {}).get("driver") == driver_name
        ]
        if resource_slices
        else []
    )

    if len(driver_resource_slices) < min_count:
        return (
            False,
            f"Expected at least {min_count} ResourceSlices for driver '{driver_name}', found {len(driver_resource_slices)}",
            driver_resource_slices,
        )

    Logger.info(
        f"Found {len(driver_resource_slices)} ResourceSlices for driver '{driver_name}'"
    )
    return True, "", driver_resource_slices


def wait_for_resource_slices_deletion(
    api_version: str,
    driver_name: str = "gpu.amd.com",
    max_retries: int = 12,
    retry_interval: int = 5,
) -> Tuple[bool, str]:
    """
    Wait for ResourceSlices to be deleted (with retry).

    Args:
        api_version: DRA API version (v1 or v1beta1)
        driver_name: Driver name to filter ResourceSlices
        max_retries: Maximum number of retry attempts
        retry_interval: Seconds to wait between retries

    Returns:
        Tuple of (deleted, error_message)

    Example:
        deleted, err = wait_for_resource_slices_deletion("v1", "gpu.amd.com")
        if not deleted:
            pytest.fail(f"ResourceSlices not deleted: {err}")
    """
    global Logger

    for attempt in range(max_retries):
        ret_code, resource_slices, err = k8_util.k8_get_custom_resource_objects(
            group=DRA_API_GROUP, version=api_version, plural="resourceslices"
        )

        if ret_code != 0:
            Logger.warning(
                f"Failed to get ResourceSlices (attempt {attempt + 1}/{max_retries}): {err}"
            )
            time.sleep(retry_interval)
            continue

        # Filter for driver ResourceSlices
        driver_resource_slices = (
            [
                rs
                for rs in resource_slices
                if rs.get("spec", {}).get("driver") == driver_name
            ]
            if resource_slices
            else []
        )

        if len(driver_resource_slices) == 0:
            Logger.info(
                f"ResourceSlices deleted successfully (attempt {attempt + 1}/{max_retries})"
            )
            return True, ""
        else:
            Logger.info(
                f"Waiting for ResourceSlices deletion (attempt {attempt + 1}/{max_retries}): "
                f"{len(driver_resource_slices)} slices still exist"
            )
            time.sleep(retry_interval)

    return (
        False,
        f"ResourceSlices not deleted within {max_retries * retry_interval} seconds",
    )


def verify_dra_installation(
    namespace: str,
    api_version: str,
    expected_pod_count: int,
    device_class_name: str = "gpu.amd.com",
    driver_name: str = "gpu.amd.com",
) -> Tuple[bool, str]:
    """
    Comprehensive DRA driver installation verification.
    Checks: pods running, DeviceClass exists, ResourceSlices published.

    This is a convenience function that combines multiple verification steps
    commonly needed after DRA driver installation.

    Args:
        namespace: DRA driver namespace
        api_version: DRA API version (v1 or v1beta1)
        expected_pod_count: Expected number of DRA driver pods
        device_class_name: Expected DeviceClass name (default: "gpu.amd.com")
        driver_name: Expected driver name in ResourceSlices (default: "gpu.amd.com")

    Returns:
        Tuple of (success, error_message)

    Example:
        success, err = verify_dra_installation(
            namespace="kube-amd-gpu-dra",
            api_version="v1",
            expected_pod_count=2
        )
        if not success:
            pytest.fail(f"DRA installation verification failed: {err}")
    """
    global Logger

    # Check pods
    success, err = verify_dra_driver_pods_running(namespace, expected_pod_count)
    if not success:
        return False, f"Pod check failed: {err}"

    # Check DeviceClass
    exists, err, _ = verify_device_class_exists(api_version, device_class_name)
    if not exists:
        return False, f"DeviceClass check failed: {err}"

    # Check ResourceSlices
    success, err, _ = verify_resource_slices_exist(api_version, driver_name, min_count=1)
    if not success:
        return False, f"ResourceSlices check failed: {err}"

    Logger.info("DRA driver installation verified successfully")
    return True, ""


def verify_dra_uninstallation(
    namespace: str,
    api_version: str,
    driver_name: str = "gpu.amd.com",
    max_wait_pods: int = 60,
    max_wait_resources: int = 60,
) -> Tuple[bool, str]:
    """
    Verify DRA driver is fully uninstalled.
    Checks that pods are terminated and ResourceSlices are deleted.

    Args:
        namespace: DRA driver namespace
        api_version: DRA API version (v1 or v1beta1)
        driver_name: Driver name to check (default: "gpu.amd.com")
        max_wait_pods: Max seconds to wait for pods to terminate
        max_wait_resources: Max seconds to wait for ResourceSlices deletion

    Returns:
        Tuple of (success, error_message)

    Example:
        success, err = verify_dra_uninstallation(
            namespace="kube-amd-gpu-dra",
            api_version="v1"
        )
        if not success:
            pytest.fail(f"DRA uninstallation verification failed: {err}")
    """
    global Logger
    import lib.common as common

    # Check pods are terminated
    # k8_check_pod_terminated() has built-in retry logic with sleep_time and total_attempts
    # Calculate retry parameters from max_wait_pods (default: 60s)
    dra_pods = [common.PodInfo("dra-driver", 1, 1)]

    sleep_interval = 5  # seconds between retries
    total_attempts = max(1, max_wait_pods // sleep_interval)  # e.g., 60s / 5s = 12 attempts

    running_pods = k8_util.k8_check_pod_terminated(
        namespace, dra_pods, sleep_time=sleep_interval, total_attempts=total_attempts
    )

    if running_pods:
        return False, f"DRA driver pods still running after {max_wait_pods}s: {running_pods}"

    Logger.info("DRA driver pods terminated")

    # Check ResourceSlices are deleted
    # Calculate retry count from max_wait_resources, ensuring at least 1 retry
    retry_interval = 5
    max_retries = max(1, max_wait_resources // retry_interval)

    success, err = wait_for_resource_slices_deletion(
        api_version, driver_name, max_retries=max_retries, retry_interval=retry_interval
    )
    if not success:
        return False, f"ResourceSlices deletion failed: {err}"

    Logger.info("DRA driver uninstallation verified successfully")
    return True, ""
