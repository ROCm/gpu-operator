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
import yaml
import pytest
import os
import time
import logging
from datetime import datetime
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.anr_util as anr_util
import lib.amdgpu as amdgpu_util
from lib.util import K8Helper

Logger = logging.getLogger("k8.test_auto_node_remediation")

# On OpenShift, KMM runs in 'openshift-kmm' (managed by OLM), which is outside the
# operator namespace. Without ignoring it during drain, the KMM webhook pod gets
# evicted on single-node clusters, breaking the operator's reconciler.
OPENSHIFT_KMM_NAMESPACE = 'openshift-kmm'

# Workaround for GPUOP-663: ANR workflow pods need amd-dcm toleration on DCM-managed nodes.
_DCM_TOLERATIONS = [
    {"key": "amd-dcm", "value": "up", "effect": "NoSchedule"},
    {"key": "amd-dcm", "value": "up", "effect": "NoExecute"},
]


def _add_dcm_workflow_tolerations(tcfg):
    """Append amd-dcm tolerations to nodeRemediationTaints when DCM is enabled.

    GPUOP-663: The operator does not auto-add amd-dcm tolerations to workflow pods.
    On DCM-managed nodes the amd-dcm taint blocks workflow scheduling. This workaround
    injects the tolerations via nodeRemediationTaints until the operator is fixed.

    Note: when nodeRemediationTaints is set, the operator uses it *instead of* the
    default amd-gpu-unhealthy taint. So if the list was empty we must also include
    the default remediation taint to preserve existing behavior.
    """
    if not tcfg.get('configManager.enable'):
        return
    taints = list(tcfg.get('remediationWorkflow.nodeRemediationTaints') or [])
    existing_keys = {(t['key'], t.get('effect', '')) for t in taints}
    if ("amd-gpu-unhealthy", "NoSchedule") not in existing_keys:
        taints.append({"key": "amd-gpu-unhealthy", "effect": "NoSchedule"})
    for dcm_t in _DCM_TOLERATIONS:
        if (dcm_t['key'], dcm_t['effect']) not in existing_keys:
            taints.append(dcm_t)
    tcfg['remediationWorkflow.nodeRemediationTaints'] = taints


def _drain_ignore_namespaces(environment):
    ns_list = ['kube-system', 'cert-manager', environment.gpu_operator_namespace]
    if environment.deployment_mode == 'openshift':
        ns_list.append(OPENSHIFT_KMM_NAMESPACE)
        ns_list.append('argo-workflow')
    return ns_list



@pytest.fixture(scope="module")
def deviceconfig_install(gpu_cluster, images, gpu_operator_install,
                        argo_workflow_setup, environment, request):
    global Logger

    # Argo Workflows is managed differently on vanilla K8s vs OpenShift
    # - Vanilla K8s: GPU Operator installs Argo automatically (dummy fixture)
    # - OpenShift: argo_workflow_setup fixture installs and manages Argo
    argo_info = argo_workflow_setup
    Logger.info(f"Using Argo Workflows - managed by: {argo_info.get('managed_by', 'fixture')}")

    # cleanup - remove any deviceconfigs
    def _deviceconfig_cleanup():
        devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
        for devcfg_name, _ in devcfg_map.items():
            ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)
            if ret_code != 0:
                Logger.error(f"Failed to delete deviceconfig name: {devcfg_name}, error : {ret_stderr}")
        time.sleep(10)

    _deviceconfig_cleanup()
    request.addfinalizer(_deviceconfig_cleanup)

    class DeviceConfigCRInfo(object):
        pass

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    test_config = {
    'metadata.namespace' : environment.gpu_operator_namespace,
    'driver.enable' : True,
    'remediationWorkflow.nodeDrainPolicy.ignoreNamespaces': _drain_ignore_namespaces(environment),
    # Workaround (GPUOP-975): Explicitly set testerImage from the manifest so all ANR tests use a valid image.
    'remediationWorkflow.testerImage.repository': images.get('testRunner.image.repository'),
    'remediationWorkflow.testerImage.version': images.get('testRunner.image.version'),
    }
    test_config.update(images)

    test_cfg_map = spec_util.build_deviceconfig_cr_template(test_config, gpu_nodes, 'auto_node_remediation', environment.amdgpu_driver_spec)
    exporter_port_map = {}
    devicecfg_list = []
    if len(test_cfg_map) > 1:
        # Assign unique NodePorts for each deviceconfig instance
        for idx, cfg_name in enumerate(test_cfg_map.keys()):
            cfg = test_cfg_map[cfg_name]
            cfg['metricsExporter.nodePort'] = 32500 + idx * 100
            exporter_port_map[cfg['selector.value']] = cfg['metricsExporter.nodePort']
    else:
        for node in gpu_nodes:
            node_hostname = k8_util.k8_get_node_hostname(node)
            exporter_port_map[node_hostname] = 32500
 
    for spec_name, tcfg in test_cfg_map.items():
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_create_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")
        devicecfg_list.append(tcfg['metadata.name'])

    # Check for corresponding deviceconfig created
    K8Helper.check_deviceconfig_status(environment, devicecfg_list)
    for devcfg in devicecfg_list:
        K8Helper.wait_kmm_worker_completion(environment, devcfg)

    devcfg_info = DeviceConfigCRInfo()
    setattr(devcfg_info, "test_cfg_map", test_cfg_map)
    setattr(devcfg_info, "exporter_port_map", exporter_port_map)
    setattr(devcfg_info, "devicecfg_list", devicecfg_list)
    yield devcfg_info
  
@pytest.fixture(autouse=True)
def collect_logs_on_failure(request, environment):
    """Collect workflow details and controller logs whenever a remediation TC fails."""
    failures_before = request.session.testsfailed
    test_start_time = datetime.utcnow()
    yield
    if request.session.testsfailed <= failures_before:
        return
    node_names = []
    try:
        ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
        if ret_code == 0:
            node_names = [
                n['metadata']['labels']['kubernetes.io/hostname']
                for n in gpu_nodes
            ]
    except Exception:
        pass
    anr_util.collect_workflow_logs(environment, node_names=node_names or None, since_time=test_start_time)


def test_anr_workflow(gpu_cluster, images, deviceconfig_install, environment, request):
    global Logger
    condition_type = "AMDGPUHwsHang"
    clean_params = {
        'remediationWorkflow.enable' : False,
    }
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, condition_type, clean_params))

    ret_code, pods = k8_util.k8_get_pods(environment.gpu_operator_namespace)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to fetch GPU Operator pods in namespace {environment.gpu_operator_namespace}")

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    remediation_label_key = "amd.com/gpu.remediating"
    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] =  images['testRunnerAgfhc.image.version']
        tcfg['remediationWorkflow.nodeRemediationLabels'] = {remediation_label_key: "true"}

        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    # Wait for ConfigMap to be created from configMapImage
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")
    # Wait for WorkflowTemplate — operator creates it lazily on first reconcile
    # after remediationWorkflow.enable is set
    target_template = "default-template"
    template_found = False
    for attempt in range(12):
        ret_code, workflowtemplates, err = k8_util.k8_get_custom_resource_objects(group="argoproj.io", version="v1alpha1", plural="workflowtemplates")
        if ret_code == 0 and workflowtemplates:
            template_names = [t['metadata']['name'] for t in workflowtemplates]
            if target_template in template_names:
                template_found = True
                break
        Logger.info(f"Attempt {attempt+1}/12: WorkflowTemplate '{target_template}' not yet available, waiting...")
        time.sleep(5)
    K8Helper.triage(environment, template_found,
        f"Required WorkflowTemplate '{target_template}' not found in the cluster after 60s")

    # patch configmap recipe.
    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    patch_body = {
        "nodeCondition": condition_type,
        "physicalActionNeeded": False,
        "skipRebootStep": False,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    }
    ret_code, resp, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, patch_body)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to modify configmap : {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    hang_status = any(
        c.get('type') == condition_type and c.get('status') == "True"
        for c in target_node.get('status', {}).get('conditions', [])
    )
    if not hang_status:
        anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)
    else:
        Logger.info(f"Node {node_name} already has {condition_type}=True. Skipping patch.")

    # Stream workflow events and verify node state at each step:
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout, with_reboot=True)
    ret_code, stdout, stderr = anr_util.monitor_and_patch_remediation(environment, node_name, condition_type=condition_type, timeout=anr_timeout)
    K8Helper.triage(environment, (ret_code == 0), f"workflow status - {stderr}")

def test_custom_configmap(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify that a user-supplied configmap is picked up and drives the workflow correctly.
    Checks: custom condition mapping, nodeRemediationLabels applied, drain and suspend steps complete."""
    global Logger
    condition_type = "AMDGPUBootFailed"
    remediation_label_key = "amd.com/remediating"
    configmap_name = "custom-remediation-config"
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, condition_type, {'remediationWorkflow.enable': False}))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    configmap_file = os.path.join(environment.logdir, f"{configmap_name}.yaml")
    custom_mapping = [{
        "nodeCondition": condition_type,
        "notifyRemediationMessage": "Rerun the known failing workload.",
        "notifyTestFailureMessage": "Remove the failing OAM (see OAM Removal and Installation)",
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "iterations": 1,
            "recipe": recipe,
            "stopOnFailure": True,
            "timeoutSeconds": recipe_timeout,
        },
        "workflowTemplate": "default-template"
    }]
    with open(configmap_file, "w") as fp:
        yaml.dump(custom_mapping, fp, default_flow_style=False)

    k8_util.k8_delete_configmap(environment.gpu_operator_namespace, configmap_name)
    ret_code, _, ret_stderr = k8_util.k8_create_configmap(environment.gpu_operator_namespace, configmap_name, configmap_file, "workflow")
    K8Helper.triage(environment, (ret_code == 0), f"Failed to create configmap {configmap_name}: {ret_stderr.strip()}")

    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.config'] = configmap_name
        tcfg['remediationWorkflow.nodeRemediationLabels'] = {remediation_label_key: "true"}
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to modify deviceconfig: {ret_stderr}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout)
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)
    applylabels_ok = anr_util._wait_for_step(node_name, 'applylabels', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, applylabels_ok,
        f"applylabels step did not succeed on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")
    label_present = anr_util._verify_node_labels(node_name, [f"{remediation_label_key}=true"], tag="applylabels", expect_present=True)
    K8Helper.triage(environment, label_present,
        f"nodeRemediationLabels '{remediation_label_key}' not found on '{node_name}' after applylabels")

    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"drain step did not succeed on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")

    suspend_ok = anr_util._wait_for_step(node_name, 'suspend', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, suspend_ok,
        f"suspend did not auto-resume on '{node_name}' (physicalActionNeeded=False) — "
        f"step phases: {anr_util._get_step_phases(node_name)}")

    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=False)


def test_disable_autoStartWorkflow(gpu_cluster, images, deviceconfig_install, environment, request):
    global Logger
    clean_params = {
        'remediationWorkflow.enable' : False,
        'remediationWorkflow.autoStartWorkflow':  True,
    }
    condition_type = "AMDGPUHwsHang"
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, condition_type, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.autoStartWorkflow'] = False
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] =  images['testRunnerAgfhc.image.version']

        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    # Wait for ConfigMap to be created from configMapImage
    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    patch_body = {
        "nodeCondition": condition_type,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    }
    ret_code, resp, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, patch_body)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to modify configmap : {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']

    # Remove any leftover resume label before injecting the condition
    k8_util.k8_label_node(node_name, {"operator.amd.com/gpu-force-resume-workflow": None})

    # Inject the node condition to trigger the workflow controller
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)
    # With autoStartWorkflow=False the workflow must be created but suspended at the awaitapproval gate
    suspended = anr_util._wait_for_step(node_name, 'awaitapproval', 'Running', timeout=120)
    K8Helper.triage(environment, suspended,
        f"Workflow did not reach suspended state on '{node_name}' — "
        f"expected 'awaitapproval' step Running with autoStartWorkflow=False. "
        f"Step phases: {anr_util._get_step_phases(node_name)}")

    # Apply the resume label to manually trigger workflow start
    k8_util.k8_label_node(node_name, {"operator.amd.com/gpu-force-resume-workflow": "true"}, overwrite=True)

    # Confirm the gate unblocks — awaitapproval must move to Succeeded
    resumed = anr_util._wait_for_step(node_name, 'awaitapproval', 'Succeeded', timeout=60)
    K8Helper.triage(environment, resumed,
        f"Workflow did not resume on '{node_name}' after applying resume label — "
        f"awaitapproval step still not Succeeded. Step phases: {anr_util._get_step_phases(node_name)}")

    Logger.info(f"autoStartWorkflow=false verified on '{node_name}': "
                f"workflow suspended at awaitapproval, resumed after label applied")

    # Abort — no need to run the full workflow, gate behaviour is confirmed
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=False)


def test_max_parallel_workflows(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify maxParallelWorkflows limits the number of concurrently Running workflows.
    Requires at least 2 GPU worker nodes — skipped on single-node clusters.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    MAX_PARALLEL = 1
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    target_nodes = anr_util.get_worker_nodes(gpu_nodes, all=True)
    if len(target_nodes) < 2:
        pytest.skip("test_max_parallel_workflows requires at least 2 GPU nodes")

    framework, recipe, _ = anr_util.get_framework_and_recipe(gpu_cluster, target_nodes[0])

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.maxParallelWorkflows'] = MAX_PARALLEL
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] = images['testRunnerAgfhc.image.version']
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, err = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {err}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    node_names = [n['metadata']['labels']['kubernetes.io/hostname'] for n in target_nodes]

    # Inject condition on all worker nodes simultaneously
    for node_name in node_names:
        anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)

    # Poll until the controller has created a remediation workflow for every targeted node,
    # or until timeout.  A fixed sleep is flaky — workflows may not yet exist when we sample.
    WORKFLOW_APPEAR_TIMEOUT = 60
    deadline = time.time() + WORKFLOW_APPEAR_TIMEOUT
    node_workflows = []
    while time.time() < deadline:
        ret_code, workflows, err = k8_util.k8_get_custom_resource_objects(
            group="argoproj.io", version="v1alpha1", plural="workflows")
        K8Helper.triage(environment, (ret_code == 0), f"Failed to list workflows: {err}")
        # Keep only remediation workflows belonging to our target nodes in the operator namespace.
        node_workflows = [
            w for w in (workflows or [])
            if w.get('metadata', {}).get('namespace') == environment.gpu_operator_namespace
            and any(anr_util._is_node_workflow(w, n) for n in node_names)
        ]
        if len(node_workflows) >= len(node_names):
            break
        time.sleep(5)

    K8Helper.triage(environment, len(node_workflows) >= len(node_names),
        f"Only {len(node_workflows)}/{len(node_names)} remediation workflows appeared within "
        f"{WORKFLOW_APPEAR_TIMEOUT}s of condition injection")

    running = [w for w in node_workflows if w.get('status', {}).get('phase') == 'Running']
    pending = [w for w in node_workflows if w.get('status', {}).get('phase') == 'Pending']
    Logger.info(f"maxParallelWorkflows={MAX_PARALLEL}: Running={len(running)}, Pending={len(pending)}")

    K8Helper.triage(environment, len(running) <= MAX_PARALLEL,
        f"maxParallelWorkflows={MAX_PARALLEL} violated: {len(running)} Running workflows found")
    K8Helper.triage(environment, len(pending) >= len(node_names) - MAX_PARALLEL,
        f"Expected {len(node_names) - MAX_PARALLEL} Pending workflow(s), got {len(pending)}")

    for node_name in node_names:
        anr_util._abort_workflow(node_name)
        anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

def test_skip_reboot_step(gpu_cluster, deviceconfig_install, environment, request):
    """Verify skipRebootStep=true causes the reboot step to be Skipped in the workflow."""
    global Logger
    CONDITION = "AMDGPUHwsHang"
    TEST_RUNNER_SA = "amd-gpu-operator-test-runner"
    PULL_SECRET = "amdpsdo-secret"
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))

    anr_util._patch_sa_image_pull_secret(environment.gpu_operator_namespace, TEST_RUNNER_SA, PULL_SECRET)

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Failed to get GPU nodes")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout=recipe_timeout)
    request.node.add_marker(pytest.mark.timeout(anr_timeout))

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, err = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {err}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "timeoutSeconds": recipe_timeout,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    # Wait for drain to complete (confirms workflow started and progressed)
    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"drain step did not succeed on {node_name} — step phases: {anr_util._get_step_phases(node_name)}")

    # Wait for suspend to auto-resume (physicalActionNeeded=false)
    suspend_ok = anr_util._wait_for_step(node_name, 'suspend', 'Succeeded', timeout=120)
    K8Helper.triage(environment, suspend_ok,
        f"suspend step did not auto-resume on {node_name} — step phases: {anr_util._get_step_phases(node_name)}")

    # Core assertion: reboot step must be Skipped (skipRebootStep=true)
    reboot_skipped = anr_util._wait_for_step(node_name, 'reboot', 'Skipped', timeout=60)
    K8Helper.triage(environment, reboot_skipped,
        f"reboot step is not Skipped on {node_name} — skipRebootStep=true was not respected. "
        f"Step phases: {anr_util._get_step_phases(node_name)}")

    Logger.info(f"skipRebootStep verified: reboot step is Skipped on {node_name}")

    # Abort — no need to wait for full AGFHC run
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)


def test_physical_action_suspend_and_resume(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify physicalActionNeeded=true workflow behaviour:

    1. The notifybeforesuspend step emits a k8s Warning event on the node before blocking.
    2. The workflow suspends at the suspend gate and does NOT auto-resume (no resume label applied).
    3. Applying the resume label unblocks the workflow so it can continue.

    The test aborts the workflow once step 3 is confirmed — no full AGFHC run required.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Failed to get GPU nodes")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, err = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {err}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": True,
        "skipRebootStep": True,
        "notifyRemediationMessage": "Physical action required: inspect and repair the GPU node.",
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "timeoutSeconds": recipe_timeout,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout)
    #  Stream workflow events until the suspend gate is reached (suspend Running).
    ret_code, stdout, stderr = anr_util.monitor_and_patch_remediation(
        environment, node_name, condition_type=CONDITION,
        stop_at_step=('suspend', 'Running'), timeout=anr_timeout)
    K8Helper.triage(environment, ret_code == 0,
        f"Workflow did not reach physical action suspend gate on '{node_name}': {stderr}")

    # Explicitly verify the Warning event emitted by the notifybeforesuspend step.
    event_ok = anr_util._verify_notifybeforesuspend(
        environment.gpu_operator_namespace, node_name)
    K8Helper.triage(environment, event_ok,
        f"notifybeforesuspend Warning event not found on '{node_name}' — "
        f"step may have succeeded without emitting the expected k8s event")

    Logger.info(f"[physical-action] Workflow suspended at gate on '{node_name}' — notify event verified.")

    # Apply the resume label and confirm the gate unblocks.
    resume_label = "operator.amd.com/gpu-force-resume-workflow"
    k8_util.k8_label_node(node_name, {resume_label: "true"}, overwrite=True)
    Logger.info(f"[physical-action] Applied resume label '{resume_label}' on '{node_name}'.")

    suspend_resumed = anr_util._wait_for_step(node_name, 'suspend', 'Succeeded', timeout=120)
    K8Helper.triage(environment, suspend_resumed,
        f"Workflow did not resume on '{node_name}' after applying label '{resume_label}' — "
        f"suspend step still not Succeeded. Step phases: {anr_util._get_step_phases(node_name)}")

    Logger.info(f"[physical-action] Workflow resumed successfully on '{node_name}' after label applied.")

    # Abort — no need to wait for full test-runner completion.
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

def test_pre_condition_workflow(gpu_cluster, images, deviceconfig_install, environment, request):
    global Logger
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, "AMDGPUHwsHang", {'remediationWorkflow.enable': False}))

    condition_type = "AMDGPUHwsHang"
    configmap_name = "custom-remediation-config"
    configmap_file = os.path.join(environment.logdir, f"{configmap_name}.yaml")

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    target_node = anr_util.get_worker_nodes(gpu_nodes)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']

    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    custom_mapping = [{
        "nodeCondition": condition_type,
        "notifyRemediationMessage": "Rerun the known failing workload.",
        "notifyTestFailureMessage": 'Remove the failing OAM (see OAM Removal and Installation)',
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "iterations": 1,
            "recipe": recipe,
            "stopOnFailure": True,
            "timeoutSeconds": recipe_timeout,
        },
        "workflowTemplate": "default-template"
    }]

    with open(configmap_file, "w") as fp:
        yaml.dump(custom_mapping, fp, default_flow_style=False)

    k8_util.k8_delete_configmap(environment.gpu_operator_namespace, configmap_name)
    ret_code, ret_stdout, ret_stderr = k8_util.k8_create_configmap(environment.gpu_operator_namespace, configmap_name, configmap_file, "workflow")
    K8Helper.triage(environment, (ret_code == 0), f"Failed to create configmap {configmap_name} for {configmap_file}, err: {ret_stderr.strip()}")

    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)

    # Enable remediation with the custom configmap
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.config'] = configmap_name
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to modify deviceconfig, stderr: {ret_stderr}")

    # Verify workflow is created when remediation is enabled
    time.sleep(30)
    _, wfs, _ = k8_util.k8_get_custom_resource_objects(group="argoproj.io", version="v1alpha1", plural="workflows")
    wf_created = any(anr_util._is_node_workflow(w, node_name) for w in (wfs or []))
    K8Helper.triage(environment, wf_created,
        f"Workflow did not appear for node '{node_name}' after enabling remediation")

    # Disable remediation
    for _, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = False
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to modify deviceconfig, stderr: {ret_stderr}")

    time.sleep(15)

    # Verify in-flight workflow is NOT interrupted when remediation is disabled
    _, workflows_after, _ = k8_util.k8_get_custom_resource_objects(
        group="argoproj.io", version="v1alpha1", plural="workflows"
    )
    still_running = any(
        anr_util._is_node_workflow(w, node_name) and w.get('status', {}).get('phase') == 'Running'
        for w in (workflows_after or [])
    )
    K8Helper.triage(environment, still_running,
        f"Workflow for node '{node_name}' is no longer Running after disabling remediation — "
        f"in-flight workflows must not be interrupted.")

    # Abort — no need to wait for full completion
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=False)
    
@pytest.mark.parametrize("framework", ["RVS", "AGFHC"])
def test_tester_image_frameworks(gpu_cluster, images, deviceconfig_install, environment, framework, request):
    """Verify that the remediation workflow runs to completion with each tester image framework."""
    global Logger
    condition_type = "AMDGPUHwsHang"

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)

    node_ip = k8_util.k8_get_node_address(target_node)
    cluster_node = gpu_cluster.find_node_by_ip(node_ip)
    tr_support = amdgpu_util.get_test_runner_support(cluster_node.device_id)

    if framework == "RVS":
        if not tr_support.get("rvs", False):
            pytest.skip(f"RVS not supported on {cluster_node.gpu_series}")
        recipes = tr_support.get("rvs_recipes", [])
        recipe = recipes[0] if recipes else pytest.skip(f"No RVS recipes defined for {cluster_node.gpu_series}")
    else:
        if not tr_support.get("agfhc", False):
            pytest.skip(f"AGFHC not supported on {cluster_node.gpu_series}")
        recipes = tr_support.get("agfhc_recipes", [])
        recipe = recipes[0] if recipes else pytest.skip(f"No AGFHC recipes defined for {cluster_node.gpu_series}")
    recipe_timeout = anr_util.get_recipe_timeout(framework, recipe)

    clean_params = {'remediationWorkflow.enable': False}
    if framework == "AGFHC":
        clean_params['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
        clean_params['remediationWorkflow.testerImage.version']    = images['testRunnerAgfhc.image.version']
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, condition_type, clean_params))

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version']    = images['testRunnerAgfhc.image.version']
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to modify deviceconfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    patch_body = {
        "nodeCondition": condition_type,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    }
    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, patch_body)
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)

    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout, with_reboot=False)
    ret_code, _, stderr = anr_util.monitor_and_patch_remediation(environment, node_name, condition_type=condition_type, timeout=anr_timeout)
    K8Helper.triage(environment, (ret_code == 0), f"[{framework}] Workflow failed on '{node_name}': {stderr}")


def test_recovery_policy(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify recoveryPolicy.maxAllowedRunsPerWindow prevents new workflows once the limit is exhausted.

    When maxAllowedRunsPerWindow is exhausted within the configured window, the operator does NOT
    create any new workflow (not even a Suspended one). This is intentional: the policy exists to
    prevent repeated remediation on nodes with persistent hardware failures. No workflow at all is
    the correct outcome.

    Scenario 1 — First run is allowed:
        Configure maxAllowedRunsPerWindow=1, windowSize=5m. Inject the condition and confirm a
        workflow starts. Abort it (consuming the one allowed run).

    Scenario 2 — Policy blocks further runs within the window:
        Clear the condition, re-inject within the same 5m window, wait two reconcile cycles and
        confirm no new workflow is created for the node.
    """
    global Logger
    condition_type = "AMDGPUHwsHang"
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, condition_type, {'remediationWorkflow.enable': False}))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to modify deviceconfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": condition_type,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "recoveryPolicy": {
            "maxAllowedRunsPerWindow": 1,
            "windowSize": "5m",
        },
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']

    # Trigger and consume the one allowed run for this window
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)
    time.sleep(30)
    _, wfs, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    wf_started = any(anr_util._is_node_workflow(w, node_name) for w in (wfs or []))
    K8Helper.triage(environment, wf_started,
        f"First workflow did not start on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=False)

    # --- Scenario 2: re-trigger within the same window — operator must create no new workflow ---
    _, workflows_before, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    wf_names_before = {w['metadata']['name'] for w in (workflows_before or []) if anr_util._is_node_workflow(w, node_name)}

    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=True)
    time.sleep(45)  # two reconcile cycles (20s each) + buffer

    _, workflows_after, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    new_workflows = [
        w['metadata']['name'] for w in (workflows_after or [])
        if anr_util._is_node_workflow(w, node_name)
        and w['metadata']['name'] not in wf_names_before
    ]
    K8Helper.triage(environment, len(new_workflows) == 0,
        f"recoveryPolicy not enforced: {len(new_workflows)} workflow(s) created for '{node_name}' "
        f"after maxAllowedRunsPerWindow=1 was exhausted: {new_workflows}")
    Logger.info(f"[recovery-policy] Scenario 2 passed: no workflow created after limit exhausted on '{node_name}'.")

    anr_util.patch_node_condition(environment, node_name, condition_type=condition_type, condition_status=False)


def test_custom_taint(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify nodeRemediationTaints: custom taint key is applied during remediation.

    Configures a non-default taint key (gpu-remediating=true:NoSchedule) via
    nodeRemediationTaints and verifies it appears on the node after the taint step
    succeeds. Confirms the default 'amd-gpu-unhealthy' taint is not used.

    Note: taint removal verification (on successful workflow) is blocked by GPUOP-614
    (xargs not found in test-runner image). The workflow is aborted after the taint
    step to verify taint application only.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    CUSTOM_TAINT_KEY = "gpu-remediating"
    CUSTOM_TAINT_VALUE = "true"
    CUSTOM_TAINT_EFFECT = "NoSchedule"

    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, CONDITION, {'remediationWorkflow.enable': False}))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes, strict=True)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] = images['testRunnerAgfhc.image.version']
        tcfg['remediationWorkflow.nodeRemediationTaints'] = [{
            "key": CUSTOM_TAINT_KEY,
            "value": CUSTOM_TAINT_VALUE,
            "effect": CUSTOM_TAINT_EFFECT,
        }]
        _add_dcm_workflow_tolerations(tcfg)
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    # Wait for taint step to succeed — confirms custom taint was applied
    taint_ok = anr_util._wait_for_step(node_name, 'taint', 'Succeeded', timeout=180)
    K8Helper.triage(environment, taint_ok,
        f"taint step did not succeed on '{node_name}' within timeout — "
        f"step phases: {anr_util._get_step_phases(node_name)}")

    # Verify custom taint is present and default taint is absent
    ret_code, refreshed_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Failed to refresh GPU nodes")
    refreshed = next((n for n in (refreshed_nodes or [])
                      if n['metadata']['labels']['kubernetes.io/hostname'] == node_name), None)
    K8Helper.triage(environment, refreshed is not None, f"Node '{node_name}' not found after refresh")
    taints = refreshed.get('spec', {}).get('taints') or []
    taint_keys = [t.get('key') for t in taints]

    K8Helper.triage(environment, CUSTOM_TAINT_KEY in taint_keys,
        f"Custom taint '{CUSTOM_TAINT_KEY}' not found on '{node_name}' after taint step. Taints: {taints}")
    K8Helper.triage(environment, 'amd-gpu-unhealthy' not in taint_keys,
        f"Default taint 'amd-gpu-unhealthy' unexpectedly present when custom taint is configured. Taints: {taints}")

    Logger.info(f"Custom taint '{CUSTOM_TAINT_KEY}={CUSTOM_TAINT_VALUE}:{CUSTOM_TAINT_EFFECT}' "
                f"verified on '{node_name}'")

    # Abort — taint-removal verification blocked by GPUOP-614 (xargs not found in test-runner image)
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)


def test_multiple_node_conditions_sequential(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify that multiple NodeConditions on the same node are handled sequentially.

    Injects two different conditions on the same node. While the first workflow is running,
    confirms no second workflow is created (sequential guard validated). Then aborts WF1
    — while CONDITION_1 is still True so the operator can process the abort label — clears
    CONDITION_1 post-abort, removes any residual taint, and confirms WF2 starts for
    CONDITION_2.

    The amd-gpu-unhealthy taint acts as the sequencing guard: a second workflow is not
    started while the taint is present. Aborting WF1 (rather than waiting for its GPU test
    step to complete naturally) avoids a recipe-duration-dependent timeout on hardware where
    the test step takes longer than the WF2 detection window.
    """
    global Logger
    CONDITION_1 = "AMDGPUHwsHang"
    CONDITION_2 = "AMDGPUHplFailure"

    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, [CONDITION_1, CONDITION_2], {'remediationWorkflow.enable': False}))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes, strict=True)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] = images['testRunnerAgfhc.image.version']
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    for condition in (CONDITION_1, CONDITION_2):
        ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
            "nodeCondition": condition,
            "physicalActionNeeded": False,
            "skipRebootStep": True,
            "validationTestsProfile": {
                "framework": framework,
                "recipe": recipe,
                "iterations": 1,
                "timeoutSeconds": recipe_timeout,
            },
        })
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap for {condition}: {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout, with_reboot=False)

    # Step 1: inject first condition, wait for workflow 1 to reach drain step
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION_1, condition_status=True)
    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"First workflow did not reach drain step on '{node_name}' — "
        f"step phases: {anr_util._get_step_phases(node_name)}")

    # Step 2: inject second condition while workflow 1 is running — no second workflow must start
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION_2, condition_status=True)
    time.sleep(30)  # two reconcile cycles

    _, workflows, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    node_workflows = [w for w in (workflows or []) if anr_util._is_node_workflow(w, node_name)]
    K8Helper.triage(environment, len(node_workflows) == 1,
        f"Expected 1 workflow while first is running, got {len(node_workflows)} on '{node_name}'")
    Logger.info(f"Verified: no second workflow started while first is running on '{node_name}'")

    # Capture wf1 names before deletion so we can distinguish WF2 later.
    _, wfs_before_delete, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    wf1_names = {w['metadata']['name'] for w in (wfs_before_delete or []) if anr_util._is_node_workflow(w, node_name)}

    # Step 3: directly delete WF1 — the operator's abort-label mechanism only works when a
    # Suspend step is active, but with physicalActionNeeded=False the workflow skips awaitapproval
    # and may be mid-test-step here, so the abort label would be silently ignored.
    for wf_name in wf1_names:
        ret_code, _, err = k8_util.k8_delete_custom_resource(
            group="argoproj.io", version="v1alpha1", plural="workflows",
            namespace=environment.gpu_operator_namespace, name=wf_name)
        K8Helper.triage(environment, ret_code == 0,
            f"Failed to delete WF1 '{wf_name}' on '{node_name}': {err}")
        Logger.info(f"Deleted WF1 '{wf_name}' on '{node_name}'")

    # Clear CONDITION_1 and remove the taint so the operator can create WF2.
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION_1, condition_status=False)

    ret_code, nodes = k8_util.k8_get_nodes()
    if ret_code == 0:
        node_info = next((n for n in nodes if n['metadata']['name'] == node_name), None)
        if node_info:
            applied_taints = node_info.get('spec', {}).get('taints') or []
            for taint in applied_taints:
                if taint.get('key') == 'amd-gpu-unhealthy':
                    k8_util.k8_untaint_node(node_name, effects=[taint['effect']],
                                            taint_key=taint['key'], taint_value=taint.get('value', ''))
                    Logger.info(f"Removed taint '{taint['key']}' from '{node_name}'")

    # Step 4: wait for a *new* workflow for CONDITION_2 (distinct from workflow-1).
    wf2_found = False
    for attempt in range(9):  # 180s
        _, wfs_after, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
        new_wfs = [w for w in (wfs_after or [])
                   if anr_util._is_node_workflow(w, node_name) and w['metadata']['name'] not in wf1_names]
        if new_wfs:
            wf2_found = True
            Logger.info(f"New workflow detected for CONDITION_2: {new_wfs[0]['metadata']['name']}")
            break
        time.sleep(20)
    K8Helper.triage(environment, wf2_found,
        f"Second workflow for CONDITION_2 was not created on '{node_name}' after workflow 1 aborted")

    drain2_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain2_ok,
        f"Second workflow did not reach drain step on '{node_name}' after first completed — "
        f"CONDITION_2 ({CONDITION_2}) was not picked up. Step phases: {anr_util._get_step_phases(node_name)}")

    Logger.info(f"Sequential handling verified on '{node_name}': "
                f"workflow 2 started for {CONDITION_2} after workflow 1 completed")

    # Abort workflow 2 and clear both conditions
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION_2, condition_status=False)


def test_node_label_removal(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify nodeRemediationLabels are removed from the node after workflow completion.

    Configures a remediation label and runs the full workflow end-to-end via
    monitor_and_patch_remediation, which verifies both label application (applylabels step)
    and label removal (removelabels step) as part of its step dispatch.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    REMEDIATION_LABEL_KEY = "amd.com/gpu.remediating"
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, recipe_timeout = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        tcfg['remediationWorkflow.nodeRemediationLabels'] = {REMEDIATION_LABEL_KEY: "true"}
        if framework == "AGFHC":
            tcfg['remediationWorkflow.testerImage.repository'] = images['testRunnerAgfhc.image.repository']
            tcfg['remediationWorkflow.testerImage.version'] = images['testRunnerAgfhc.image.version']
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": recipe_timeout,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)

    # Run full workflow — monitor_and_patch_remediation verifies both applylabels and removelabels steps
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout, with_reboot=False)
    ret_code, _, stderr = anr_util.monitor_and_patch_remediation(environment, node_name, condition_type=CONDITION, timeout=anr_timeout)
    K8Helper.triage(environment, (ret_code == 0), f"Workflow failed on '{node_name}': {stderr}")

    # Explicitly confirm the label is absent after workflow completes
    label_removed = anr_util._verify_node_labels(
        node_name, [f"{REMEDIATION_LABEL_KEY}=true"], tag="removelabels", expect_present=False)
    K8Helper.triage(environment, label_removed,
        f"Remediation label '{REMEDIATION_LABEL_KEY}' still present on '{node_name}' after workflow completion")
    Logger.info(f"nodeRemediationLabels removal verified on '{node_name}' after workflow completion")


def test_failed_workflow_ttl(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify ttlForFailedWorkflows automatically deletes failed workflow objects after the TTL expires.

    Uses an intentionally invalid testerImage to force the workflow to fail, then waits for the
    TTL window to pass and confirms the workflow CR has been garbage collected by the operator.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    TTL = "2m"
    TTL_SECONDS = 120
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, _ = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.ttlForFailedWorkflows'] = TTL
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        # Use an invalid testerImage to force the workflow to fail at the test step
        tcfg['remediationWorkflow.testerImage.repository'] = "invalid-registry.local/nonexistent"
        tcfg['remediationWorkflow.testerImage.version'] = "invalid"
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "timeoutSeconds": 60,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)

    # Wait for workflow to reach a terminal failed state (invalid image → ImagePullBackOff → Failed)
    # recipe_timeout=60: configmap sets validationTestsProfile.timeoutSeconds=60 as the Argo step deadline
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout=60)
    terminal = anr_util._wait_for_workflow_terminal(node_name, timeout=anr_timeout)
    K8Helper.triage(environment, terminal in ('Failed', 'Error'),
        f"Workflow did not reach Failed/Error state on '{node_name}' — terminal: {terminal}. "
        f"Step phases: {anr_util._get_step_phases(node_name)}")
    Logger.info(f"Workflow reached terminal phase '{terminal}' on '{node_name}' — TTL window starts now")

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

    # Wait for TTL + buffer then confirm the workflow CR is gone
    time.sleep(TTL_SECONDS + 30)

    _, workflows, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    remaining = [w for w in (workflows or []) if anr_util._is_node_workflow(w, node_name)]
    K8Helper.triage(environment, len(remaining) == 0,
        f"Expected workflow to be garbage collected after TTL '{TTL}' + 30s buffer, "
        f"but {len(remaining)} workflow(s) still present on '{node_name}': "
        f"{[w['metadata']['name'] for w in remaining]}")
    Logger.info(f"ttlForFailedWorkflows='{TTL}' verified: workflow CR deleted after TTL on '{node_name}'")

def test_configmap_required_for_remediation(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify that remediationWorkflow.enable=true alone is not sufficient to trigger remediation.

    The operator requires exactly one of:
      - remediationWorkflow.config.name  (user-provided ConfigMap), OR
      - remediationWorkflow.configMapImage (operator-managed ConfigMap via Job)

    Scenario 1 — enable=true with neither configmap source set:
        Manually delete any existing ConfigMap. Verify that the operator does not re-create the
        default ConfigMap and does not start any workflow for an injected node condition.

    Scenario 2 — add configMapImage, verify ConfigMap is created and workflow starts:
        Set configMapImage on the same DeviceConfig. The operator creates the default ConfigMap
        via a Job. Confirm the ConfigMap appears and a workflow is created for the pending
        node condition, then abort it.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']

    # --- Scenario 1: enable=true, no configmap source ---
    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        if 'remediationWorkflow.config' in tcfg:
            del tcfg['remediationWorkflow.config']
        if 'remediationWorkflow.configMapImage.repository' in tcfg:
            del tcfg['remediationWorkflow.configMapImage.repository']
        if 'remediationWorkflow.configMapImage.version' in tcfg:
            del tcfg['remediationWorkflow.configMapImage.version']
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"

    # Manually delete any pre-existing ConfigMap. 
    k8_util.k8_delete_configmap(environment.gpu_operator_namespace, configmap_name)
    Logger.info(f"[configmap-required] Pre-existing ConfigMap '{configmap_name}' deleted (if any) before Scenario 1")

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    # Wait two reconcile cycles (20s) for the operator to react
    time.sleep(20)

    # Verify default ConfigMap was NOT created (no image → operator skips createConfigMapFromImage)
    ret_code_cm, config_map, _ = k8_util.k8_get_configmap(environment.gpu_operator_namespace, configmap_name)
    K8Helper.triage(environment, ret_code_cm != 0 or config_map is None,
        f"Default ConfigMap '{configmap_name}' created without a configmap source — "
        f"operator must not create it when neither configMapImage nor config.name is set")
    Logger.info(f"[configmap-required] Scenario 1: ConfigMap not created as expected")

    # Verify no workflow was created for the pending condition
    _, wfs, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    node_workflows = [w for w in (wfs or []) if anr_util._is_node_workflow(w, node_name)]
    K8Helper.triage(environment, len(node_workflows) == 0,
        f"Workflow(s) started without a configmap source: "
        f"{[w['metadata']['name'] for w in node_workflows]}")
    Logger.info(f"[configmap-required] Scenario 1 passed: no workflow on '{node_name}' without configmap source")

    # --- Scenario 2: add configMapImage, ConfigMap must be created and workflow must start ---
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig with configMapImage: {ret_stderr}")

    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap '{configmap_name}' was not created from configMapImage within timeout")
    Logger.info(f"[configmap-required] Scenario 2: ConfigMap created after configMapImage set")

    # The node condition is still present — a workflow must now be created
    time.sleep(30)
    _, wfs, _ = k8_util.k8_get_custom_resource_objects("argoproj.io", "v1alpha1", "workflows")
    wf_created = any(anr_util._is_node_workflow(w, node_name) for w in (wfs or []))
    K8Helper.triage(environment, wf_created,
        f"No workflow created on '{node_name}' after configMapImage was provided")
    Logger.info(f"[configmap-required] Scenario 2 passed: workflow created on '{node_name}' "
                f"after configMapImage was provided")

    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)


def test_validation_failure_notification(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify notifyTestFailureMessage is emitted as a k8s Warning event when the test step fails.

    Uses an intentionally invalid testerImage so the workflow's test step fails with
    ImagePullBackOff. The operator must emit a Warning event on the node with the configured
    notifyTestFailureMessage content before the workflow reaches Failed state.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    FAILURE_MESSAGE = "GPU validation failed: remove the failing OAM and contact support."
    clean_params = {
        'remediationWorkflow.enable': False,
        'remediationWorkflow.testerImage.repository': None,
        'remediationWorkflow.testerImage.version': None,
    }
    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes)
    framework, recipe, _ = anr_util.get_framework_and_recipe(gpu_cluster, target_node)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        # Invalid testerImage forces ImagePullBackOff → test step fails → operator emits notifyTestFailureMessage
        tcfg['remediationWorkflow.testerImage.repository'] = "invalid-registry.local/nonexistent"
        tcfg['remediationWorkflow.testerImage.version'] = "invalid"
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
        "notifyTestFailureMessage": FAILURE_MESSAGE,
        "validationTestsProfile": {
            "framework": framework,
            "recipe": recipe,
            "iterations": 1,
            "timeoutSeconds": 60,
        },
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)

    # Wait for the workflow to fail — invalid image causes ImagePullBackOff at the test step
    # recipe_timeout=60: configmap sets validationTestsProfile.timeoutSeconds=60 as the Argo step deadline
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node, recipe_timeout=60)
    terminal = anr_util._wait_for_workflow_terminal(node_name, timeout=anr_timeout)
    K8Helper.triage(environment, terminal in ('Failed', 'Error'),
        f"Workflow did not reach Failed/Error on '{node_name}' — terminal: {terminal}. "
        f"Step phases: {anr_util._get_step_phases(node_name)}")
    Logger.info(f"Workflow reached terminal phase '{terminal}' on '{node_name}'")

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

    # Verify the operator emitted a Warning event on the node containing notifyTestFailureMessage
    ret_code, events, _ = k8_util.k8_get_events(namespace=environment.gpu_operator_namespace)
    K8Helper.triage(environment, ret_code == 0,
        f"Failed to fetch events from namespace '{environment.gpu_operator_namespace}'")

    event_found = any(
        ev.involved_object.kind == "Node"
        and ev.involved_object.name == node_name
        and ev.type == "Warning"
        and FAILURE_MESSAGE in (ev.message or "")
        for ev in events.items
    )
    K8Helper.triage(environment, event_found,
        f"notifyTestFailureMessage Warning event not found on node '{node_name}'. "
        f"Expected message substring: '{FAILURE_MESSAGE}'")
    Logger.info(f"notifyTestFailureMessage event verified on '{node_name}': '{FAILURE_MESSAGE}'")


def test_drain_default_policy(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify the drain step evicts the mock pod under default nodeDrainPolicy settings.

    No nodeDrainPolicy is set — operator uses defaults (force=false, timeoutSeconds=300,
    ignoreDaemonSets=true).  Asserts:
    - mock evictable pod is gone after drain
    - _verify_drain confirms no evictable pods remain
    - DaemonSet pods present before drain are still present after (ignoreDaemonSets=true)
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    POD_NAME = "drain-test-pod"
    POD_NS = "default"
    
    # Register cleanup_workflow first — pytest runs finalizers LIFO so it runs last,
    # after the mock pod is deleted.
    clean_params = {'remediationWorkflow.enable': False}
    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))
    request.addfinalizer(lambda: anr_util._delete_pod_safe(POD_NAME, POD_NS))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes, strict=True)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    anr_timeout = anr_util.get_anr_monitor_timeout(gpu_cluster, target_node)
    request.node.add_marker(pytest.mark.timeout(anr_timeout))

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    # Snapshot DaemonSet pods before drain to verify they survive
    ds_pods_before = set(anr_util._get_daemonset_pod_uids(node_name).keys())
    Logger.info(f"DaemonSet pods on '{node_name}' before drain: {ds_pods_before}")

    # Deploy mock evictable pod — gives the drain step real work to do
    anr_util._create_evictable_pod(node_name, POD_NS, POD_NAME)

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)

    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"drain step did not succeed on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")

    # Abort immediately after drain — eviction already issued, pod termination is async.
    # _verify_drain checks live k8s state so works correctly post-abort.
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

    # Mock pod must be gone — drain actually evicted something.
    # Poll up to 60s: grace_period_seconds=30 + API latency.
    K8Helper.triage(environment, not anr_util._pod_on_node(node_name, POD_NS, POD_NAME, timeout=60),
        f"Mock pod '{POD_NAME}' still present on '{node_name}' after drain step succeeded")

    # No evictable pods remain (uses default drain policy — ignoreDaemonSets=true).
    # Pass the operator's default ignored namespaces so _verify_drain doesn't flag
    # system pods in those namespaces as evictable.
    K8Helper.triage(environment, anr_util._verify_drain(node_name, {'ignoreNamespaces': _drain_ignore_namespaces(environment)}),
        f"_verify_drain failed: evictable pods remain on '{node_name}' after drain")

    # All DaemonSet pods present before drain must still be present (ignoreDaemonSets=true)
    ds_pods_after = set(anr_util._get_daemonset_pod_uids(node_name).keys())
    evicted_ds_pods = ds_pods_before - ds_pods_after
    K8Helper.triage(environment, len(evicted_ds_pods) == 0,
        f"DaemonSet pod(s) were evicted on '{node_name}' despite ignoreDaemonSets=true: {evicted_ds_pods}")

    Logger.info(f"test_drain_default_policy passed on '{node_name}': "
                f"mock pod evicted, {len(ds_pods_before)} DaemonSet pod(s) preserved")


def test_drain_ignore_daemonsets_false(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify ignoreDaemonSets=false causes DaemonSet pods to be evicted along with the mock pod."""
    global Logger
    CONDITION = "AMDGPUHwsHang"
    POD_NAME = "drain-test-pod"
    POD_NS = "default"
    clean_params = {'remediationWorkflow.enable': False}

    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))
    request.addfinalizer(lambda: anr_util._delete_pod_safe(POD_NAME, POD_NS))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes, strict=True)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    drain_timeout_seconds = 300
    anr_timeout = anr_util.get_anr_monitor_timeout(
        gpu_cluster, target_node,
        tcfg={'remediationWorkflow.nodeDrainPolicy.timeoutSeconds': drain_timeout_seconds},
    )
    request.node.add_marker(pytest.mark.timeout(anr_timeout))

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        tcfg['remediationWorkflow.nodeDrainPolicy.ignoreDaemonSets'] = False
        tcfg['remediationWorkflow.nodeDrainPolicy.timeoutSeconds'] = drain_timeout_seconds
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    # Snapshot DS pod UIDs before drain — DaemonSet controllers recreate evicted pods with
    # different names, so we track by UID (not name) to detect eviction.
    ds_uids_before = anr_util._get_daemonset_pod_uids(node_name)
    Logger.info(f"DaemonSet pods on '{node_name}' before drain: {list(ds_uids_before)}")

    anr_util._create_evictable_pod(node_name, POD_NS, POD_NAME)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"drain step did not succeed on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")

    # Abort immediately after drain — eviction already issued, pod termination is async.
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

    # Mock pod must be gone.
    K8Helper.triage(environment, not anr_util._pod_on_node(node_name, POD_NS, POD_NAME, timeout=60),
        f"Mock pod '{POD_NAME}' still present on '{node_name}' after drain step succeeded")

    # With ignoreDaemonSets=false, DS pods are included in the drain scope and evicted.
    # Poll until all old UIDs are gone — new pods come back with different names and UIDs.
    evicted_uids = anr_util._wait_for_daemonset_pods_evicted(node_name, ds_uids_before, timeout=120)
    K8Helper.triage(environment, len(evicted_uids) > 0,
        f"No DaemonSet pods were evicted on '{node_name}' despite ignoreDaemonSets=false — "
        f"old UIDs still present: {set(ds_uids_before.values()) - evicted_uids}")
    Logger.info(f"DaemonSet eviction confirmed: {len(evicted_uids)}/{len(ds_uids_before)} pod(s) evicted (UIDs gone)")


def test_drain_ignore_namespaces(gpu_cluster, images, deviceconfig_install, environment, request):
    """Verify ignoreNamespaces: pods in the ignored namespace survive drain; pods outside are evicted.

    Creates two pods on the same node:
      - IGNORED_POD in a dedicated namespace listed in ignoreNamespaces
      - EVICT_POD in the default namespace (not ignored)

    After drain succeeds:
      - EVICT_POD must be gone — confirms drain actually ran
      - IGNORED_POD must still be present — confirms ignoreNamespaces is honoured

    The ignored pod is then deleted explicitly; the namespace finalizer is a safety net.
    """
    global Logger
    CONDITION = "AMDGPUHwsHang"
    IGNORED_NS = "drain-test-ignore"
    IGNORED_POD = "drain-ignored-pod"
    EVICT_POD = "drain-evict-pod"
    EVICT_NS = "default"
    clean_params = {
        'remediationWorkflow.enable': False,
        'remediationWorkflow.nodeDrainPolicy.ignoreNamespaces': _drain_ignore_namespaces(environment),
    }

    request.addfinalizer(lambda: anr_util.cleanup_workflow(
        deviceconfig_install, environment, CONDITION, clean_params))
    request.addfinalizer(lambda: anr_util._delete_pod_safe(EVICT_POD, EVICT_NS))
    request.addfinalizer(lambda: k8_util.k8_delete_namespace(IGNORED_NS))

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    target_node = anr_util.get_worker_nodes(gpu_nodes, strict=True)
    node_name = target_node['metadata']['labels']['kubernetes.io/hostname']
    drain_timeout_seconds = 120
    anr_timeout = anr_util.get_anr_monitor_timeout(
        gpu_cluster, target_node,
        tcfg={'remediationWorkflow.nodeDrainPolicy.timeoutSeconds': drain_timeout_seconds},
    )
    request.node.add_marker(pytest.mark.timeout(anr_timeout))

    k8_util.k8_create_namespace(IGNORED_NS)
    anr_util._create_evictable_pod(node_name, IGNORED_NS, IGNORED_POD)
    anr_util._create_evictable_pod(node_name, EVICT_NS, EVICT_POD)

    devcfg_name = ''
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['remediationWorkflow.enable'] = True
        _add_dcm_workflow_tolerations(tcfg)
        tcfg['remediationWorkflow.configMapImage.repository'] = images.get('remediationWorkflow.configMapImage.repository')
        tcfg['remediationWorkflow.configMapImage.version'] = images.get('remediationWorkflow.configMapImage.version')
        tcfg['remediationWorkflow.nodeDrainPolicy.ignoreNamespaces'] = _drain_ignore_namespaces(environment) + [IGNORED_NS]
        tcfg['remediationWorkflow.nodeDrainPolicy.timeoutSeconds'] = drain_timeout_seconds
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, _, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to patch DeviceConfig: {ret_stderr}")
        devcfg_name = cr_spec['metadata']['name']

    configmap_name = f"{devcfg_name}-default-conditional-workflow-mappings"
    configmap_ready = anr_util.wait_for_configmap_from_image(environment, devcfg_name, timeout=120)
    K8Helper.triage(environment, configmap_ready,
        f"ConfigMap was not created from configMapImage within timeout")

    ret_code, _, err = k8_util.k8_patch_workflow_config(environment.gpu_operator_namespace, configmap_name, {
        "nodeCondition": CONDITION,
        "physicalActionNeeded": False,
        "skipRebootStep": True,
    })
    K8Helper.triage(environment, (ret_code == 0), f"Failed to patch workflow configmap: {err}")

    time.sleep(10)

    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=True)
    drain_ok = anr_util._wait_for_step(node_name, 'drain', 'Succeeded', timeout=anr_timeout)
    K8Helper.triage(environment, drain_ok,
        f"drain step did not succeed on '{node_name}' — step phases: {anr_util._get_step_phases(node_name)}")

    # Abort immediately after drain — evictions already issued, pod termination is async.
    anr_util._abort_workflow(node_name)
    anr_util.patch_node_condition(environment, node_name, condition_type=CONDITION, condition_status=False)

    # Pod outside the ignored namespace must be gone — confirms drain ran.
    K8Helper.triage(environment, not anr_util._pod_on_node(node_name, EVICT_NS, EVICT_POD, timeout=60),
        f"Pod '{EVICT_POD}' in '{EVICT_NS}' was not evicted — drain did not run or ignoreNamespaces bled over")

    # Pod inside the ignored namespace must still be present — drain must have skipped it.
    K8Helper.triage(environment, anr_util._pod_on_node(node_name, IGNORED_NS, IGNORED_POD, timeout=10),
        f"Pod '{IGNORED_POD}' in ignored ns '{IGNORED_NS}' was evicted — ignoreNamespaces not honoured")
    Logger.info(f"[ignore-namespaces] '{IGNORED_POD}' survived drain in '{IGNORED_NS}' — ignoreNamespaces confirmed")

    # Explicitly delete the ignored pod; namespace finalizer is only a safety net.
    anr_util._delete_pod_safe(IGNORED_POD, IGNORED_NS)
    Logger.info(f"[ignore-namespaces] Explicitly deleted '{IGNORED_POD}' from '{IGNORED_NS}'")
