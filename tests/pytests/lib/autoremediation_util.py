#!/usr/bin/python3

'''
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
'''

import time
import logging
import subprocess
import requests
import yaml
from datetime import datetime
from typing import Tuple, List, Optional
from kubernetes import client
from kubernetes.client.rest import ApiException
from kubernetes.utils import create_from_dict

Logger = logging.getLogger("lib.autoremediation_util")


def check_argo_crds_exist() -> Tuple[int, List[str]]:
    """
    Check if Argo Workflow CRDs exist in the cluster.

    Returns:
        Tuple[int, List[str]]: (return_code, list of missing CRDs)
            return_code: 0 if all CRDs exist, -1 otherwise
            missing_crds: List of CRD names that are missing
    """
    expected_crds = [
        'workflows.argoproj.io',
        'workflowtemplates.argoproj.io',
        'clusterworkflowtemplates.argoproj.io'
    ]

    api = client.ApiextensionsV1Api()
    missing_crds = []

    try:
        existing_crds = api.list_custom_resource_definition()
        existing_crd_names = {crd.metadata.name for crd in existing_crds.items}

        for crd_name in expected_crds:
            if crd_name not in existing_crd_names:
                missing_crds.append(crd_name)

        if missing_crds:
            Logger.warning(f"Missing Argo CRDs: {missing_crds}")
            return -1, missing_crds

        Logger.info("All required Argo Workflow CRDs are present")
        return 0, []

    except ApiException as e:
        Logger.error(f"Failed to list CRDs: {e}")
        return -1, expected_crds


def check_argo_controller_running(namespace: str = "argo-workflow") -> Tuple[int, str, str]:
    """
    Check if Argo Workflow controller is running in the specified namespace.

    Args:
        namespace: Namespace to check for workflow controller

    Returns:
        Tuple[int, str, str]: (return_code, info_message, error)
            return_code: 0 if controller is running and healthy, -1 otherwise
    """
    v1 = client.CoreV1Api()
    apps_v1 = client.AppsV1Api()

    try:
        # Check for workflow-controller deployment
        deployments = apps_v1.list_deployment_for_all_namespaces(
            label_selector="app.kubernetes.io/name=workflow-controller"
        )

        if not deployments.items:
            Logger.info("No Argo workflow controller deployment found")
            return -1, "", "Workflow controller deployment not found"

        # Check if any deployment is ready
        for deployment in deployments.items:
            ready_replicas = deployment.status.ready_replicas or 0
            desired_replicas = deployment.spec.replicas or 0

            if ready_replicas > 0 and ready_replicas == desired_replicas:
                Logger.info(f"Found healthy workflow controller in namespace {deployment.metadata.namespace}")
                return 0, f"Workflow controller running in {deployment.metadata.namespace}", ""

        # Deployments exist but none are healthy
        Logger.warning("Workflow controller deployments found but none are healthy")
        return -1, "", "Workflow controller exists but is not healthy"

    except ApiException as e:
        Logger.error(f"Failed to check workflow controller: {e}")
        return -1, "", str(e)


def check_argo_installation_openshift() -> Tuple[int, str, str]:
    """
    Check if Argo Workflows is fully installed and operational on OpenShift.

    This function verifies both:
    1. Required CRDs are installed
    2. Workflow controller is running and healthy

    For OpenShift deployments, Argo may be installed via:
    - OpenShift AI Operator with DataScienceCluster CRD
    - Manual Helm installation

    Returns:
        Tuple[int, str, str]: (return_code, info_message, error)
            return_code: 0 if Argo is fully operational, -1 otherwise
    """
    api = client.ApiextensionsV1Api()

    try:
        # Check for DataScienceCluster CRD (OpenShift AI)
        crds = api.list_custom_resource_definition()
        has_dsc = any(crd.metadata.name == "datascienceclusters.datasciencecluster.opendatahub.io"
                     for crd in crds.items)

        if has_dsc:
            Logger.info("Found DataScienceCluster CRD - OpenShift AI Operator may have installed Argo")

        # Step 1: Check for Argo Workflow CRDs
        argo_crd_names = [
            'workflows.argoproj.io',
            'workflowtemplates.argoproj.io',
            'clusterworkflowtemplates.argoproj.io'
        ]

        existing_crd_names = {crd.metadata.name for crd in crds.items}
        missing_crds = [name for name in argo_crd_names if name not in existing_crd_names]

        if missing_crds:
            Logger.warning(f"Missing Argo CRDs: {missing_crds}")
            return -1, "", f"Argo Workflows not installed. Missing CRDs: {missing_crds}"

        Logger.info("Argo Workflows CRDs are present")

        # Step 2: Check for workflow controller
        ret_code, info, error = check_argo_controller_running()
        if ret_code != 0:
            Logger.warning(f"Argo CRDs found but controller is not healthy: {error}")
            return -1, "", f"Argo CRDs present but controller not running: {error}"

        # Both CRDs and controller are present and healthy
        Logger.info("Argo Workflows is fully installed and operational")
        return 0, f"Argo Workflows operational. {info}", ""

    except ApiException as e:
        Logger.error(f"Failed to check Argo installation: {e}")
        return -1, "", str(e)


def install_argo_crds(version: str = "v4.0.3") -> Tuple[int, str, str]:
    """
    Install Argo Workflows CRDs using Python K8s API.

    Downloads CRD manifests from GitHub and applies them using the API client.
    This is the Python equivalent of:
        kubectl apply --server-side --force-conflicts -k "https://github.com/argoproj/argo-workflows/manifests/base/crds/full?ref=v4.0.3"

    Args:
        version: Argo Workflows version (e.g., "v4.0.3")

    Returns:
        Tuple[int, str, str]: (return_code, message, error)
    """
    # Map of CRD files to download from Argo Workflows repository
    crd_files = [
        "argoproj.io_clusterworkflowtemplates.yaml",
        "argoproj.io_cronworkflows.yaml",
        "argoproj.io_workflowartifactgctasks.yaml",
        "argoproj.io_workfloweventbindings.yaml",
        "argoproj.io_workflows.yaml",
        "argoproj.io_workflowtaskresults.yaml",
        "argoproj.io_workflowtasksets.yaml",
        "argoproj.io_workflowtemplates.yaml"
    ]

    base_url = f"https://raw.githubusercontent.com/argoproj/argo-workflows/{version}/manifests/base/crds/full"
    api_client = client.ApiClient()
    installed_crds = []
    errors = []

    try:
        for crd_file in crd_files:
            url = f"{base_url}/{crd_file}"
            Logger.info(f"Downloading CRD from {url}")
            crd_manifest = None

            try:
                response = requests.get(url, timeout=30)
                response.raise_for_status()

                # Parse YAML manifest
                crd_manifest = yaml.safe_load(response.text)
                crd_name = crd_manifest['metadata']['name']

                # Apply CRD using create_from_dict
                Logger.info(f"Applying CRD: {crd_name}")
                create_from_dict(api_client, crd_manifest, verbose=True)
                installed_crds.append(crd_name)

            except requests.RequestException as e:
                error_msg = f"Failed to download {crd_file}: {e}"
                Logger.error(error_msg)
                errors.append(error_msg)
            except ApiException as e:
                # If CRD already exists (409 Conflict), that's okay
                if e.status == 409:
                    # crd_manifest is available here since parsing succeeded
                    crd_name = crd_manifest['metadata']['name'] if crd_manifest else crd_file
                    Logger.info(f"CRD {crd_name} already exists, skipping")
                    installed_crds.append(crd_name)
                else:
                    error_msg = f"Failed to apply {crd_file}: {e}"
                    Logger.error(error_msg)
                    errors.append(error_msg)
            except Exception as e:
                error_msg = f"Unexpected error with {crd_file}: {e}"
                Logger.error(error_msg)
                errors.append(error_msg)

        if errors:
            error_summary = f"Installed {len(installed_crds)} CRDs with {len(errors)} errors: {'; '.join(errors[:3])}"
            Logger.warning(error_summary)
            return -1, f"Partial installation: {installed_crds}", error_summary

        success_msg = f"Successfully installed {len(installed_crds)} Argo Workflow CRDs"
        Logger.info(success_msg)
        return 0, success_msg, ""

    except Exception as e:
        error_msg = f"Failed to install Argo CRDs: {e}"
        Logger.error(error_msg)
        return -1, "", error_msg


def install_argo_workflows_helm(namespace: str = "argo-workflow",
                                argo_git_tag: str = "v4.0.3",
                                chart_version: str = "1.0.5",
                                install_crds: bool = True) -> Tuple[int, str, str]:
    """
    Install Argo Workflows using Helm for OpenShift.

    This function handles the complete installation:
    1. Optionally installs CRDs using Python K8s API
    2. Installs Argo Workflows controller via Helm

    Args:
        namespace: Namespace to install Argo Workflows
        argo_git_tag: Git tag for downloading CRDs from GitHub (e.g., "v4.0.3")
        chart_version: Helm chart version for argo/argo-workflows (e.g., "1.0.5")
        install_crds: If True, install CRDs before Helm chart

    Returns:
        Tuple[int, str, str]: (return_code, stdout, stderr)

    Note:
        The argo_git_tag and chart_version use different versioning schemes:
        - argo_git_tag: Application/Git version (e.g., v4.0.3)
        - chart_version: Helm chart version (e.g., 1.0.5 for Argo v4.x)
        See https://github.com/argoproj/argo-helm/releases for chart versions
    """
    try:
        # Step 1: Install CRDs if requested
        # Note: On OpenShift/K8s 1.25+, let Helm manage CRDs instead of manual installation
        # to avoid CRD validation issues. Manual CRD installation is skipped when install_crds=True
        # because we set crds.install=true in Helm command below.
        if install_crds:
            Logger.info(f"CRDs will be installed by Helm chart (crds.install=true) to ensure version compatibility")
            # Skip manual CRD installation - let Helm handle it

        # Step 2: Add Helm repository
        Logger.info("Adding Argo Helm repository")
        helm_repo_cmd = [
            "helm", "repo", "add",
            "argo", "https://argoproj.github.io/argo-helm",
            "--force-update"
        ]
        result = subprocess.run(helm_repo_cmd, capture_output=True, text=True, timeout=60)
        if result.returncode != 0:
            Logger.warning(f"Helm repo add warning: {result.stderr}")

        # Update Helm repo
        helm_update_cmd = ["helm", "repo", "update"]
        subprocess.run(helm_update_cmd, capture_output=True, text=True, timeout=60)

        # Step 3: Install/Upgrade Argo Workflows controller using Helm chart version
        # Use 'upgrade --install' to handle both new installations and updates
        # Let Helm manage CRDs when install_crds=True to ensure version compatibility
        Logger.info(f"Installing/Upgrading Argo Workflows controller chart v{chart_version} in namespace {namespace}")
        helm_install_cmd = [
            "helm", "upgrade", "--install", "argo-workflow",
            "argo/argo-workflows",
            "-n", namespace,
            "--create-namespace",
            "--version", chart_version,
            "--set", f"crds.install={str(install_crds).lower()}",  # Let Helm manage CRDs if requested
            "--wait",
            "--timeout", "5m"
        ]

        result = subprocess.run(helm_install_cmd, capture_output=True, text=True, timeout=360)
        if result.returncode != 0:
            Logger.error(f"Failed to install Argo Workflows: {result.stderr}")
            return -1, result.stdout, result.stderr

        Logger.info("Argo Workflows installed successfully via Helm")
        return 0, result.stdout, ""

    except subprocess.TimeoutExpired as e:
        error_msg = f"Helm command timeout: {e}"
        Logger.error(error_msg)
        return -1, "", error_msg
    except Exception as e:
        error_msg = f"Failed to install Argo Workflows: {e}"
        Logger.error(error_msg)
        return -1, "", error_msg


def uninstall_argo_crds() -> Tuple[int, str, str]:
    """
    Uninstall Argo Workflows CRDs using Python K8s API.

    Returns:
        Tuple[int, str, str]: (return_code, message, error)
    """
    crd_names = [
        'clusterworkflowtemplates.argoproj.io',
        'cronworkflows.argoproj.io',
        'workflowartifactgctasks.argoproj.io',
        'workfloweventbindings.argoproj.io',
        'workflows.argoproj.io',
        'workflowtaskresults.argoproj.io',
        'workflowtasksets.argoproj.io',
        'workflowtemplates.argoproj.io'
    ]

    api = client.ApiextensionsV1Api()
    deleted_crds = []
    errors = []

    for crd_name in crd_names:
        try:
            Logger.info(f"Deleting CRD: {crd_name}")
            api.delete_custom_resource_definition(name=crd_name)
            deleted_crds.append(crd_name)
        except ApiException as e:
            if e.status == 404:
                Logger.info(f"CRD {crd_name} not found, skipping")
            else:
                error_msg = f"Failed to delete {crd_name}: {e}"
                Logger.error(error_msg)
                errors.append(error_msg)

    if errors:
        error_summary = f"Deleted {len(deleted_crds)} CRDs with {len(errors)} errors"
        return -1, error_summary, "; ".join(errors)

    success_msg = f"Successfully deleted {len(deleted_crds)} Argo Workflow CRDs"
    Logger.info(success_msg)
    return 0, success_msg, ""


def uninstall_argo_workflows_helm(namespace: str = "argo-workflow", remove_crds: bool = False) -> Tuple[int, str, str]:
    """
    Uninstall Argo Workflows using Helm.

    Args:
        namespace: Namespace where Argo Workflows is installed
        remove_crds: If True, also remove Argo CRDs after uninstalling

    Returns:
        Tuple[int, str, str]: (return_code, stdout, stderr)
    """
    try:
        # Step 1: Uninstall Helm release
        Logger.info(f"Uninstalling Argo Workflows from namespace {namespace}")
        helm_cmd = [
            "helm", "uninstall", "argo-workflow",
            "-n", namespace
        ]

        result = subprocess.run(helm_cmd, capture_output=True, text=True, timeout=120)
        if result.returncode != 0:
            Logger.warning(f"Helm uninstall warning: {result.stderr}")

        # Step 2: Delete namespace using K8s API
        v1 = client.CoreV1Api()
        try:
            Logger.info(f"Deleting namespace {namespace}")
            v1.delete_namespace(name=namespace)
            time.sleep(5)  # Give namespace time to terminate
        except ApiException as e:
            if e.status == 404:
                Logger.info(f"Namespace {namespace} already deleted")
            else:
                Logger.error(f"Failed to delete namespace: {e}")
                return -1, result.stdout, str(e)

        # Step 3: Remove CRDs if requested
        if remove_crds:
            Logger.info("Removing Argo Workflow CRDs")
            ret_code, msg, err = uninstall_argo_crds()
            if ret_code != 0:
                Logger.warning(f"CRD removal had issues: {err}")

        Logger.info("Argo Workflows uninstalled successfully")
        return 0, result.stdout, ""

    except Exception as e:
        error_msg = f"Failed to uninstall Argo Workflows: {e}"
        Logger.error(error_msg)
        return -1, "", error_msg


def set_node_condition(node_name: str, condition_type: str, status: str = "True",
                      reason: str = "GPUError", message: str = "") -> Tuple[int, str, str]:
    """
    Set a custom node condition for testing ANR workflows.

    Uses the k8_patch_node_status API from k8_util.

    Args:
        node_name: Name of the node to update
        condition_type: Type of condition (e.g., "AMDGPUHwsHang", "AMDGPUXgmi")
        status: Condition status - "True" or "False"
        reason: Reason for the condition
        message: Descriptive message for the condition

    Returns:
        Tuple[int, str, str]: (return_code, response, error)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    condition_body = {
        "status": {
            "conditions": [
                {
                    "type": condition_type,
                    "lastTransitionTime": datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%SZ'),
                    "message": message or f"Simulated {condition_type}",
                    "reason": reason,
                    "status": status,
                }
            ]
        }
    }

    try:
        ret_code, response, error = k8_util.k8_patch_node_status(node_name, condition_body)
        if ret_code == 0:
            Logger.info(f"Node condition set: {node_name} - {condition_type}={status}")
        else:
            Logger.error(f"Failed to set node condition: {error}")
        return ret_code, response, error

    except Exception as e:
        Logger.error(f"Failed to set node condition: {e}")
        return -1, "", str(e)


def clear_node_condition(node_name: str, condition_type: str) -> Tuple[int, str, str]:
    """
    Clear a node condition by setting its status to False.

    Args:
        node_name: Name of the node to update
        condition_type: Type of condition to clear

    Returns:
        Tuple[int, str, str]: (return_code, response, error)
    """
    return set_node_condition(node_name, condition_type, status="False", reason="Resolved", message="")


def check_node_has_condition(node_name: str, condition_type: str, expected_status: str = "True") -> bool:
    """
    Check if a node has a specific condition with the expected status.

    Args:
        node_name: Name of the node
        condition_type: Type of condition to check
        expected_status: Expected status of the condition

    Returns:
        bool: True if condition exists with expected status, False otherwise
    """
    v1 = client.CoreV1Api()

    try:
        node = v1.read_node(name=node_name)
        conditions = node.status.conditions or []

        for condition in conditions:
            if condition.type == condition_type and condition.status == expected_status:
                return True

        return False

    except ApiException as e:
        Logger.error(f"Failed to check node condition: {e}")
        return False


def resume_workflow_by_label(node_name: str) -> Tuple[int, str, str]:
    """
    Resume a suspended workflow by adding the resume label to the node.

    Per documentation: Apply label 'operator.amd.com/gpu-force-resume-workflow=true'
    to resume a paused workflow.

    Args:
        node_name: Name of the node

    Returns:
        Tuple[int, str, str]: (return_code, response, error)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    label_key = "operator.amd.com/gpu-force-resume-workflow"
    labels_dict = {label_key: "true"}

    success = k8_util.k8_label_node(node_name, labels_dict=labels_dict, overwrite=True)
    if success:
        Logger.info(f"Resume label added to node {node_name}")
        return 0, f"Resume label added to {node_name}", ""
    else:
        error_msg = f"Failed to add resume label to {node_name}"
        Logger.error(error_msg)
        return -1, "", error_msg


def abort_workflow_by_label(node_name: str) -> Tuple[int, str, str]:
    """
    Abort a workflow by adding the abort label to the node.

    Per documentation: Apply label 'operator.amd.com/gpu-abort-workflow=true'
    to abort the workflow while keeping the node in tainted state.

    Args:
        node_name: Name of the node

    Returns:
        Tuple[int, str, str]: (return_code, response, error)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    label_key = "operator.amd.com/gpu-abort-workflow"
    labels_dict = {label_key: "true"}

    success = k8_util.k8_label_node(node_name, labels_dict=labels_dict, overwrite=True)
    if success:
        Logger.info(f"Abort label added to node {node_name}")
        return 0, f"Abort label added to {node_name}", ""
    else:
        error_msg = f"Failed to add abort label to {node_name}"
        Logger.error(error_msg)
        return -1, "", error_msg


def get_node_taints(node_name: str) -> Tuple[int, List, str]:
    """
    Get all taints from a node.

    Args:
        node_name: Name of the node

    Returns:
        Tuple[int, List, str]: (return_code, list of taints, error)
    """
    v1 = client.CoreV1Api()

    try:
        node = v1.read_node(name=node_name)
        taints = node.spec.taints or []
        return 0, taints, ""

    except ApiException as e:
        Logger.error(f"Failed to get node taints: {e}")
        return -1, [], str(e)


def check_node_has_taint(node_name: str, taint_key: str) -> bool:
    """
    Check if a node has a specific taint key.

    Args:
        node_name: Name of the node
        taint_key: Taint key to check for

    Returns:
        bool: True if taint exists, False otherwise
    """
    ret_code, taints, _ = get_node_taints(node_name)
    if ret_code != 0:
        return False

    for taint in taints:
        if taint.key == taint_key:
            return True

    return False


def verify_remediation_configmap(namespace: str, deviceconfig_name: str) -> Tuple[int, bool, str]:
    """
    Verify that the default ANR ConfigMap exists for a DeviceConfig.

    The ConfigMap name follows the pattern: {deviceconfig_name}-default-conditional-workflow-mappings
    Uses k8_get_configmap from k8_util.

    Args:
        namespace: Namespace where the ConfigMap should exist
        deviceconfig_name: Name of the DeviceConfig

    Returns:
        Tuple[int, bool, str]: (return_code, configmap_exists, error)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    configmap_name = f"{deviceconfig_name}-default-conditional-workflow-mappings"

    ret_code, config_map, error = k8_util.k8_get_configmap(namespace, configmap_name)

    if ret_code == 0 and config_map:
        Logger.info(f"ConfigMap {configmap_name} exists in {namespace}")
        return 0, True, ""
    else:
        Logger.warning(f"ConfigMap {configmap_name} not found in {namespace}")
        return ret_code, False, error


def get_workflow_status(namespace: str, workflow_name: str) -> Tuple[int, Optional[str], str]:
    """
    Get the status/phase of an Argo Workflow.

    Args:
        namespace: Namespace where the workflow exists
        workflow_name: Name of the workflow

    Returns:
        Tuple[int, Optional[str], str]: (return_code, phase, error)
            phase: One of "Pending", "Running", "Succeeded", "Failed", "Error"
    """
    custom_api = client.CustomObjectsApi()

    try:
        workflow = custom_api.get_namespaced_custom_object(
            group="argoproj.io",
            version="v1alpha1",
            namespace=namespace,
            plural="workflows",
            name=workflow_name
        )

        phase = workflow.get("status", {}).get("phase", "Unknown")
        Logger.info(f"Workflow {workflow_name} status: {phase}")
        return 0, phase, ""

    except ApiException as e:
        if e.status == 404:
            Logger.info(f"Workflow {workflow_name} not found")
            return -1, None, "Workflow not found"
        Logger.error(f"Failed to get workflow status: {e}")
        return -1, None, str(e)


def verify_workflow_controller_deployment(namespace: str,
                                         controller_name: str = "amd-gpu-operator-workflow-controller") -> bool:
    """
    Verify that the Argo Workflow controller deployment exists and is healthy.

    Uses k8_get_deployment from k8_util.

    Args:
        namespace: Namespace where the controller should be deployed
        controller_name: Name of the workflow controller deployment

    Returns:
        bool: True if controller is healthy, False otherwise
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    controller_deployment = k8_util.k8_get_deployment(namespace, controller_name)

    if controller_deployment is None:
        Logger.warning(f"Workflow controller '{controller_name}' not found in {namespace}")
        return False

    ready_replicas = controller_deployment.status.ready_replicas or 0
    available_replicas = controller_deployment.status.replicas or 0

    is_healthy = (ready_replicas == available_replicas and ready_replicas > 0)

    if is_healthy:
        Logger.info(f"Workflow controller is healthy: {ready_replicas}/{available_replicas} replicas ready")
    else:
        Logger.warning(f"Workflow controller is not healthy: {ready_replicas}/{available_replicas} replicas ready")

    return is_healthy


def enable_remediation_in_deviceconfig(namespace: str, deviceconfig_name: str, enable: bool = True) -> Tuple[int, str, str]:
    """
    Enable or disable remediation workflow in a DeviceConfig.

    Uses k8_modify_deviceconfig_cr from k8_util along with spec_util.

    Args:
        namespace: Namespace where the DeviceConfig exists
        deviceconfig_name: Name of the DeviceConfig
        enable: True to enable remediation, False to disable

    Returns:
        Tuple[int, str, str]: (return_code, stdout, stderr)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    # Get existing DeviceConfig
    ret_code, device_config, error = k8_util.k8_get_namespaced_custom_resource(
        group="amd.com",
        version="v1alpha1",
        plural="deviceconfigs",
        namespace=namespace,
        name=deviceconfig_name
    )

    if ret_code != 0:
        Logger.error(f"Failed to get DeviceConfig {deviceconfig_name}: {error}")
        return ret_code, "", error

    # Update remediationWorkflow.enable field
    if 'spec' not in device_config:
        device_config['spec'] = {}
    if 'remediationWorkflow' not in device_config['spec']:
        device_config['spec']['remediationWorkflow'] = {}

    device_config['spec']['remediationWorkflow']['enable'] = enable

    # Apply the updated DeviceConfig
    ret_code, stdout, stderr = k8_util.k8_modify_deviceconfig_cr(device_config)

    if ret_code == 0:
        Logger.info(f"Remediation {'enabled' if enable else 'disabled'} in DeviceConfig {deviceconfig_name}")
    else:
        Logger.error(f"Failed to modify DeviceConfig: {stderr}")

    return ret_code, stdout, stderr


def cleanup_node_for_anr_test(node_name: str, namespace: str) -> Tuple[int, str, str]:
    """
    Cleanup a node after ANR test by:
    1. Clearing any test node conditions
    2. Removing taints
    3. Removing workflow-related labels

    Args:
        node_name: Name of the node to cleanup
        namespace: Namespace where workflows exist

    Returns:
        Tuple[int, str, str]: (return_code, message, error)
    """
    # Import here to avoid circular dependency
    import lib.k8_util as k8_util

    errors = []

    # Clear common test conditions
    test_conditions = ["AMDGPUHwsHang", "AMDGPUXgmi", "AMDGPUMemoryError"]
    for condition_type in test_conditions:
        if check_node_has_condition(node_name, condition_type, "True"):
            ret_code, _, err = clear_node_condition(node_name, condition_type)
            if ret_code != 0:
                errors.append(f"Failed to clear condition {condition_type}: {err}")

    # Remove taints
    k8_util.k8_untaint_node(node_name)

    # Delete workflows for the node in the specified namespace only
    # This prevents accidentally deleting workflows from other namespaces
    custom_api = client.CustomObjectsApi()

    try:
        workflows = custom_api.list_namespaced_custom_object(
            group="argoproj.io",
            version="v1alpha1",
            namespace=namespace,
            plural="workflows"
        )

        for wf in workflows.get("items", []):
            wf_metadata = wf.get('metadata', {})
            wf_name = wf_metadata.get('name', '')
            wf_namespace = wf_metadata.get('namespace', '')

            # Safety check: Only delete if workflow is in the correct namespace
            # and the node name is in the workflow name
            if wf_namespace == namespace and node_name in wf_name:
                Logger.info(f"Deleting workflow {wf_name} in namespace {namespace}")
                ret_code, _, err = k8_util.k8_delete_custom_resource(
                    group="argoproj.io",
                    version="v1alpha1",
                    plural="workflows",
                    namespace=namespace,
                    name=wf_name
                )
                if ret_code != 0:
                    errors.append(f"Failed to delete workflow {wf_name}: {err}")

    except ApiException as e:
        if e.status != 404:  # Ignore if workflows don't exist
            errors.append(f"Failed to list workflows in {namespace}: {e}")

    if errors:
        error_msg = "; ".join(errors)
        Logger.warning(f"Cleanup completed with some errors: {error_msg}")
        return -1, "Cleanup completed with errors", error_msg

    Logger.info(f"Successfully cleaned up node {node_name}")
    return 0, f"Node {node_name} cleaned up successfully", ""
