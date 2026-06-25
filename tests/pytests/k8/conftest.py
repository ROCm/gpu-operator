#!/usr/bin/python3

'''
 Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

 Licensed under the Apache License, Version 2.0 (the \"License\");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an \"AS IS\" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
'''

import pdb
import pytest
import os
import re
import logging
import time
from lib import common
import lib.helm_util as helm_util
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.npd_util as npd_util
from lib.util import K8Helper

Logger = logging.getLogger("k8.conftest")

@pytest.fixture(scope="session")
def inbox_driver_skip(environment):
    if environment.amdgpu_driver_spec["driver-deployment"] == "inbox":
        pytest.skip("Using inbox amdgpu driver - skip")
    return

@pytest.fixture(scope="session")
def gpu_operator_release_name(environment):
    return "gpu-operator"

@pytest.fixture(scope="session")
def exporter_release_name():
    return "device-metrics-exporter"

@pytest.fixture(scope="session", autouse=True)
def init_testbed(request, gpu_cluster, gpu_operator_release_name, exporter_release_name, environment):
    global Logger
    all_namespaces = []
    if hasattr(environment, "gpu_operator_namespace"):
        all_namespaces.append(environment.gpu_operator_namespace)
    if hasattr(environment, "exporter_namespace"):
        all_namespaces.append(environment.exporter_namespace)

    def _cleanup_steps():
        # cleanup
        K8Helper.delete_debug_pods(all_namespaces + ["default"])

        # remove gpu-operator helm-chart
        if hasattr(environment, "gpu_operator_namespace"):
            # remove any deviceconfig instances
            device_cfg_info = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace, None)
            for devcfg_name, _ in device_cfg_info.items():
                k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)

            if helm_util.is_helm_chart_deployed(gpu_cluster, gpu_operator_release_name, environment.gpu_operator_namespace):
                Logger.warn(f"helm {gpu_operator_release_name} is already deployed - cleanup")
                ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(gpu_cluster, gpu_operator_release_name,
                                                                          environment.gpu_operator_namespace)
                if ret_code != 0:
                    helm_util.helm_cleanup(gpu_cluster, gpu_operator_release_name, environment.gpu_operator_namespace)

        if hasattr(environment, "exporter_namespace"):
            if helm_util.is_helm_chart_deployed(gpu_cluster, exporter_release_name, environment.exporter_namespace):
                Logger.warn(f"helm {exporter_release_name} is already deployed - cleanup")
                ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(gpu_cluster, exporter_release_name,
                                                                          environment.exporter_namespace)
                if ret_code != 0:
                    helm_util.helm_cleanup(gpu_cluster, exporter_release_name, environment.exporter_namespace)

    Logger.info("Cleanup before starting test session")
    _cleanup_steps()

    # Init k8 cluster
    k8_util.k8_init_cluster(gpu_cluster, all_namespaces)
    yield
    Logger.info("Cleanup after starting test session")
    _cleanup_steps()
    return


@pytest.fixture(scope="module")
def gpu_operator_install(gpu_cluster, gpu_operator_release_name, images, environment):
    global Logger

    # cleanup
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)
        if ret_code != 0:
            Logger.error(f"Failed to delete deviceconfig name: {devcfg_name}, error : {ret_stderr}")
    time.sleep(10)

    if helm_util.is_helm_chart_deployed(gpu_cluster, gpu_operator_release_name, environment.gpu_operator_namespace):
        Logger.warn(f"helm {gpu_operator_release_name} is already deployed - cleanup")
        ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(gpu_cluster, gpu_operator_release_name,
                                                                    environment.gpu_operator_namespace)
        if ret_code != 0:
            helm_util.helm_cleanup(gpu_cluster, gpu_operator_release_name, environment.gpu_operator_namespace)
        #k8_util.k8_delete_namespace(environment.gpu_operator_namespace)

    if images.get("gpu-operator.repo", None):
        helm_util.helm_add_repo(gpu_cluster, images.get("gpu-operator.repo-name"), images.get("gpu-operator.repo"))

    for api_version in ("v1", "v1beta2", "v1beta1"):
        ret_code, _, _ = k8_util.k8_delete_custom_resource("resource.k8s.io", api_version, "deviceclasses", "", "gpu.amd.com")
        if ret_code == 0:
            Logger.warning("Deleted pre-existing DeviceClass 'gpu.amd.com' before helm install")
        break

    secret_list = []
    for entry in gpu_cluster.k8_secrets["secrets"]:
        secret_list.append(entry["name"])
    values_yaml = os.path.join(environment.logdir, f"values_{environment.gpu_operator_version}.yaml")
    if spec_util.generate_helmchart_deployment_config(environment.gpu_operator_version, images, secret_list, values_yaml):
        Logger.debug(f"Generated values.yaml for helm-chart install command, {values_yaml}")
    else:
        values_yaml = None

    options = {
        "crds.defaultCR.install" : "false",
    }

    ret_code, ret_stdout, ret_stderr = helm_util.helm_install(gpu_cluster, gpu_operator_release_name,
                                                              environment.gpu_operator_namespace,
                                                              images.get('gpu-operator.helm-chart', None),
                                                              environment.gpu_operator_version, values_yaml, **options)
    if ret_code != 0:
        Logger.error(f"Failed to install helm chart for {gpu_operator_release_name}")
        Logger.error(f"Stdout: {ret_stdout}")
        Logger.error(f"Stderr: {ret_stderr}")
    K8Helper.triage(environment, (ret_code == 0), f"Failed to install {gpu_operator_release_name}")
    K8Helper.watch_for_daemon_rollout(environment, environment.gpu_operator_namespace, len(gpu_cluster.cluster_nodes))
    time.sleep(30)
    yield
    # cleanup - remove any deviceconfigs and then gpu-operator helm-chart
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)
        if ret_code != 0:
            Logger.error(f"Failed to delete deviceconfig name: {devcfg_name}, error : {ret_stderr}")
    time.sleep(10)

    ret_code, ret_stdout, ret_stderr = helm_util.helm_uninstall(gpu_cluster, gpu_operator_release_name, environment.gpu_operator_namespace)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to uninstall {gpu_operator_release_name} helm-chart, error: {ret_stderr}")
    return

@pytest.fixture(scope="module")
def deploy_npd_daemonset(gpu_cluster, environment):
    global Logger
    Logger.info("Deploy node-problem-detector with default configuration")
    ret_code, ret_stdout, ret_stderr = npd_util.init_npd_k8(gpu_cluster, environment)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to deploy npd daemon-set, error: {ret_stderr}")
    yield
    Logger.info("Cleanup node-problem-detector")
    npd_util.fini_npd_k8(gpu_cluster, environment)
    return

@pytest.fixture(scope="module")
def argo_workflow_setup(environment):
    """
    Dummy Argo Workflows fixture for vanilla Kubernetes.

    On vanilla Kubernetes, the GPU Operator installs Argo Workflows automatically
    when ANR (Auto Node Remediation) is enabled via Helm chart installation.

    This fixture exists as a no-op placeholder to maintain test compatibility
    between vanilla Kubernetes and OpenShift environments.

    For OpenShift (which requires manual Argo installation), the real implementation
    is in tests/pytests/openshift/conftest.py.

    Returns:
        dict: Information indicating Argo is managed by GPU Operator
            - namespace: GPU Operator namespace (where Argo controller runs)
            - installed_by_fixture: False (GPU Operator manages it)
            - managed_by: "gpu-operator"
    """
    global Logger

    Logger.info("Using dummy argo_workflow_setup fixture - GPU Operator manages Argo on vanilla K8s")

    # Return info matching the OpenShift fixture interface
    argo_info = {
        "namespace": environment.gpu_operator_namespace,
        "installed_by_fixture": False,
        "managed_by": "gpu-operator",
        "platform": "vanilla-kubernetes"
    }

    yield argo_info

    # No cleanup needed - GPU Operator manages Argo lifecycle
    Logger.debug("No Argo cleanup needed - managed by GPU Operator")
    return

