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
import pprint
import pytest
import sys
import os
import time
import json
import logging
import random
import datetime
import yaml
import copy
import functools
import lib.common as common
import lib.helm_util as helm_util
import lib.k8_util as k8_util
import lib.spec_util as spec_util
import lib.metric_util as metric_util
import lib.amdgpu as amdgpu_util
from lib.util import K8Helper
from kubernetes import client, config, utils
from test_test_runner import update_test_runner_configmap, create_configmap, update_test_runner_image, metrics_fields

Logger = logging.getLogger("k8.test_config_manager")
LogPrettyPrinter = pprint.PrettyPrinter(indent = 2)

debug_on_failure = K8Helper.triage

@pytest.fixture(autouse=True, scope="module")
def skip_module(environment):
    if environment.gpu_operator_version in ["v1.0.0", "v1.1.0", "v1.2.0", "v1.2.1"]:
        pytest.skip(f"Skipping config-manager for current version {environment.gpu_operator_version}")
    return

@pytest.fixture(scope="module")
def add_tolerations(environment, effect="NoSchedule"):
    toleration_to_add = {
        "key": "amd-dcm",
        "operator": "Equal",
        "value": "up",
        "effect": effect
    }

    for ns in {"kube-system", "cert-manager", "kube-flannel"}:
        k8_util.k8_patch_tolerations(ns, toleration_to_add, tolerate_add=True)
    yield

    for ns in {"kube-system", "cert-manager", "kube-flannel"}:
        k8_util.k8_patch_tolerations(ns, toleration_to_add, tolerate_add=False)

def verify_events(gpu_cluster, environment, profile, before, after):
    global Logger

    gpu_series = get_gpu_series(gpu_cluster, environment)
    dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
    if not gpu_series or 'MI2' in gpu_series:
        pytest.skip(f"testcase not supported")
    file_path = os.path.join("lib", "files", f"partitioning_check_{gpu_series}_{dut_node.num_gpus}.json")
    with open(file_path) as fp:
        profiles = json.load(fp)
        if not profiles.get("gpu-config-profiles"):
            pytest.fail(f"check {file_path}, something wrong with the configmap")
        elif not profiles["gpu-config-profiles"].get(profile, False):
            pytest.skip(f"testcase not supported")

    before_events = before[1].items
    after_events = after[1].items

    # 1. Extract the unique UIDs from the 'before' list and put them in a set
    before_uids = {event.metadata.uid for event in before_events}

    # 2. Iterate through the 'after' list and find any event whose UID is NOT in the 'before' set
    Logger.info("Following are the events observed during this testcase:-")
    new_events = []
    flag = False
    for event in after_events:
        if event.metadata.uid not in before_uids:
            Logger.info(f"=============={event.metadata.uid}=================")
            Logger.info(f"{pprint.pformat(event.reason)}")
            Logger.info(f"{pprint.pformat(event.type)}")
            Logger.info(f"{pprint.pformat(event.involved_object.name)}")
            Logger.info(f"{pprint.pformat(event.message)}")
            if profile in event.message and "Success" in event.message:
                flag = True
    debug_on_failure(environment, "ail" not in event.message, f"Fail found in {event.message}")
    debug_on_failure(environment, flag,
                     f"Successful profile change has not happened with {profile}")

@pytest.fixture(scope="module")
def deviceconfig_install(gpu_cluster, images, gpu_operator_install, create_dcm_configmap, add_tolerations, environment):
    global Logger

    # cleanup - remove any deviceconfigs and then gpu-operator helm-chart
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)
        if ret_code != 0:
            Logger.error(f"Failed to delete deviceconfig name: {devcfg_name}, error : {ret_stderr}")
    time.sleep(10)

    class DeviceConfigCRInfo(object):
        pass

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")
    configmap = "config-map-config-manager"

    test_config = {
            'metadata.namespace' : environment.gpu_operator_namespace,
            'driver.enable' : True,
            'devicePlugin.enableNodeLabeller' : True,
            'metricsExporter.enable' : True,
            'metricsExporter.serviceType' : 'NodePort',
            'testRunner.enable' : False,
            'configManager.enable' : True,
            'configManager.config' : configmap,
        }
    test_config.update(images)
    test_cfg_map = spec_util.build_deviceconfig_cr_template(test_config, gpu_nodes, 'config-manager', environment.amdgpu_driver_spec)
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
        debug_on_failure(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")
        devicecfg_list.append(tcfg['metadata.name'])

    # Check for corresponding deviceconfig created
    K8Helper.check_deviceconfig_status(environment, devicecfg_list)
    for devcfg in devicecfg_list:
        K8Helper.wait_kmm_worker_completion(environment, devcfg)
    K8Helper.update_node_driver_version(gpu_cluster, environment)

    devcfg_info = DeviceConfigCRInfo()
    setattr(devcfg_info, "test_cfg_map", test_cfg_map)
    setattr(devcfg_info, "exporter_port_map", exporter_port_map)
    setattr(devcfg_info, "devicecfg_list", devicecfg_list)

    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    yield devcfg_info

    device_cfg_info = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace, None)
    for devcfg_name, _ in device_cfg_info.items():
        k8_util.k8_delete_deviceconfig_cr(environment.gpu_operator_namespace, devcfg_name)
    return

def get_gpu_series(gpu_cluster, environment):
    gpu_variants = gpu_cluster.get_gpu_variants()
    if gpu_variants:
        Logger.info(f"found following gpu_series in the cluster {gpu_variants}, using {gpu_variants[0]}")
        return gpu_variants[0]
    debug_on_failure(environment, gpu_series, f"didn't find gpu_series from cluster")

@pytest.fixture(scope="module")
def create_dcm_configmap(gpu_cluster, environment):
    namespace = environment.gpu_operator_namespace
    configmap = "config-map-config-manager"

    gpu_series = get_gpu_series(gpu_cluster, environment)
    dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
    num_gpus_on_dut = dut_node.num_gpus
    debug_on_failure(environment, gpu_series != None, f"Missing gpu-series information - collect tech-support to debug cluster")

    file_path = os.path.join("lib", "files", f"partitioning_check_{gpu_series}_{dut_node.num_gpus}.json")
    if os.path.exists(file_path):
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_configmap(namespace, configmap)
        k8_util.k8_create_configmap(namespace, configmap, file_path)
    else:
        # Lets create empty configmap to keep DCM happy!!
        file_path = os.path.join("lib", "files", "partitioning_no_profiles.json")
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_configmap(namespace, configmap)
        k8_util.k8_create_configmap(namespace, configmap, file_path)
    yield configmap
    ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_configmap(namespace, configmap)

def reset_dcm_profile(gpu_cluster, environment, skip_reboot = True):
    global Logger
    namespace = environment.gpu_operator_namespace
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    def _any_gpu_partitioned(gpu_nodes):
        partitioned = []
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            pod_name = k8_util.k8_get_pod_name("config-manager", namespace, node_name)
            # reset the device first
            ret_code, output, resp_stderr = k8_util.exec_command_in_pod(namespace, ["amd-smi", "partition", "-c", "--json"], pod_name)
            if ret_code != 0:
                Logger.error(f"Failed to collect current partition information for node {node_name}, {pod_name}, error : {resp_stderr}")
                continue

            partition_status = extract_partition_info(environment, output)
            Logger.debug(f"Current Partition Status: {LogPrettyPrinter.pformat(output)}")
            for gpu_id, profile in partition_status.items():
                if profile != "SPX_NPS1":
                    partitioned.append(True)
        return any(partitioned)

    gpu_series = get_gpu_series(gpu_cluster, environment)
    dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
    debug_on_failure(environment, gpu_series != None, f"Missing gpu-series information - collect tech-support to debug cluster")
    if gpu_series and 'MI3' in gpu_series:
        if _any_gpu_partitioned(gpu_nodes):
            patch_body = {
                "spec": {
                    "configManager": {
                        "configManagerTolerations": [
                            {
                                "effect": "NoExecute",
                                "key": "amd-dcm",
                                "operator": "Equal",
                                "value": "up"
                            }
                        ]
                    }
                }
            }

            api_client = client.ApiClient()
            custom_objects_api = client.CustomObjectsApi(api_client)
            devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
            for devcfg_name, _ in devcfg_map.items():
                try:
                    custom_objects_api.patch_namespaced_custom_object(
                        group="amd.com",
                        version='v1alpha1',
                        name=devcfg_name,
                        namespace=namespace,
                        plural='deviceconfigs',
                        body=patch_body
                    )
                    Logger.info(f"Successfully patched with {patch_body}")
                except client.ApiException as e:
                    Logger.error(f"Failed to patch custom object: {e}")

            # Watch for all pod creation (node is tainted, no device-plugin)
            devicecfg_pods = [
                common.PodInfo('config-manager', len(gpu_nodes), 1),
            ]
            failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
            if not failed_pods:
                labels_dict = {"dcm.amd.com/gpu-config-profile" : "SPX_NPS1"}
                for node in gpu_nodes:
                    node_name = node['metadata']['labels']['kubernetes.io/hostname']
                    k8_util.k8_taint_node(node_name, taint_add=True)
                    k8_util.k8_label_node(node_name, labels_dict, overwrite=True)

                time.sleep(30)

            if _any_gpu_partitioned(gpu_nodes):
                Logger.error(f"Failed to restore DCM profile to default")
            else:
                Logger.info(f"GPUs are restored to default partition status!!")
        else:
            Logger.info(f"GPUs are in default partition status!!")

    # Now cleanup from dcm pod and node label/taint perspective
    labels_dict = {
                      "dcm.amd.com/gpu-config-profile" : None,
                      "dcm.amd.com/gpu-config-profile-state" : None
                  }
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
        k8_util.k8_untaint_node(node_name)
    # Watch for all pod creation

    '''
    FIXME: No specific need to remove config-map for the DCM
    patch_body = {
        "spec": {
            "configManager": {
                "config": None
            }
        }
    }

    api_client = client.ApiClient()
    custom_objects_api = client.CustomObjectsApi(api_client)
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        try:
            custom_objects_api.patch_namespaced_custom_object(
                group="amd.com",
                version='v1alpha1',
                name=devcfg_name,
                namespace=environment.gpu_operator_namespace,
                plural='deviceconfigs',
                body=patch_body
            )
            print(f"Successfully patched")
        except client.ApiException as e:
            pytest.fail(f"Failed to patch custom object: {e}")
    '''

    # Watch for all pod creation
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    if failed_pods:
        Logger.error(f"One or more pods are not ready - {failed_pods}")

    if not skip_reboot:
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            ret_code = k8_util.reboot_node(gpu_cluster, node_name)
            if ret_code != 0:
                Logger.error(f"Failed to reboot node {node_name}")
 
def extract_partition_info(environment, amd_smi_partition_json):
    try:
        amd_smi_partition_info = json.loads(amd_smi_partition_json.replace("'", "\""))
    except Exception as je:
        Logger.error(f"Failed to parse amd_smi_partition JSON document, error : {je}")
        Logger.debug(f"JSON : {amd_smi_partition_json}")
        K8Helper.triage(environment, False, f"Failed to parse amd-smi-partition JSON")

    current_partitions = amd_smi_partition_info.get("current_partition", [])
    main_gpu_entries = [item for item in current_partitions if item["memory"] != "N/A" and item["accelerator_type"] != "N/A"]
    partition_status = {}
    for entry in main_gpu_entries:
        partition_status[entry["gpu_id"]] = f"{entry['accelerator_type']}_{entry['memory']}"
    return partition_status

# Dead code - parse_amd_smi_json() is not called anywhere in the test suite
# Removing to avoid confusion. If needed in the future, the validation logic
# can be restored from git history.

@pytest.mark.level11
def test_deviceconfig_config_manager_deploy(deviceconfig_install, gpu_cluster, environment):
    global Logger
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    # Watch for all pod creation
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")
    reset_dcm_profile(gpu_cluster, environment)

def exporter_nodeport_exp_config(request, gpu_cluster, deviceconfig_install, environment):
    global Logger
    # Generate set of config-maps in the k8 cluster with different set of labels and metrics
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    # Restore default mode (non-rbac) for this testcase
    dme_version = None
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['metricsExporter.enable'] = True
        tcfg['metricsExporter.serviceType'] = 'NodePort'
        tcfg['metricsExporter.rbacConfig.enable'] = False
        tcfg['metricsExporter.rbacConfig.disableHttps'] = False
        dme_version = tcfg['metricsExporter.image.version']

        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        K8Helper.triage(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")

    exporter_config_defn = {}
    label_support_info = metric_util.get_label_details(environment.gpu_operator_version)
    non_mandatory_labels = list(filter(lambda x: label_support_info[x] == "no", label_support_info.keys()))
    mandatory_labels = list(filter(lambda x: label_support_info[x] == "yes", label_support_info.keys()))

    # Build common list of metrics across all nodes in the cluster (if different gpu-series are part of cluster)
    list_of_metrics_set = []
    for node in gpu_nodes:
        node_ip = k8_util.k8_get_node_address(node)
        cluster_node = gpu_cluster.get_worker_node(node_ip)
        if not cluster_node:
            pytest.fail(f"Unable to get worker node from cluster for ip: {node_ip}")
        metrics_data = metric_util.get_supported_metrics(gpu_series = cluster_node.gpu_series,
                                                         amdgpu_driver = cluster_node.amdgpu_driver_version, 
                                                         dme_version = dme_version)
        list_of_metrics_set.append(set(map(lambda x: x['name'].split(":")[0].lower(), metrics_data)))
    common_metrics = list(functools.reduce(lambda s1, s2: s1.intersection(s2), list_of_metrics_set))
    Logger.info(f"Using {common_metrics} for metrics-exporter configmap validation")

    for idx in range(2):
        label_subset = random.sample(non_mandatory_labels, 5)
        metric_subset = random.sample(common_metrics, 5)
        config_map = {
            "GPUConfig" : {
                "Labels" : label_subset,
                "Fields" : metric_subset,
            },
        }
        exp_config_name = f"exporter-config-{idx}"
        configmap_file = os.path.join(environment.logdir, f"{exp_config_name}.json")
        with open(configmap_file, "w") as fp:
            fp.write(json.dumps(config_map, indent=4))

        configmap_file = os.path.join(environment.logdir, f"config.json")
        with open(configmap_file, "w") as fp:
            fp.write(json.dumps(config_map, indent=4))

        # Delete if there is any previous instance with same name
        ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_configmap(environment.gpu_operator_namespace,
                                                                       exp_config_name)
        Logger.debug(f"Result of configmap delete operation, ret_code:{ret_code}, ret_stdout: {ret_stdout.strip()}, err: {ret_stderr.strip()}")
        # ignore ret_code
        ret_code, ret_stdout, ret_stderr = k8_util.k8_create_configmap(environment.gpu_operator_namespace,
                                                                       exp_config_name,
                                                                       configmap_file)
        K8Helper.triage(environment, ret_code == 0,
                        f"Failed to create configmap {exp_config_name} for {configmap_file}, err: {ret_stderr.strip()}")
        exporter_config_defn[exp_config_name] = (label_subset, metric_subset)
        Logger.info(f"Created configmap {exp_config_name} with labels: {label_subset} and metrics: {metric_subset}")

    def _cleanup_configmap():
        # Restore/Revert back test configuration
        for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
            if tcfg.get('metricsExporter.config'):
                del tcfg['metricsExporter.config']
            cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
            ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
            if ret_code != 0:
                Logger.warn(f"Failed to create deviceconfig, stderr: {ret_stderr}")

            # Check for corresponding deviceconfig updated
            K8Helper.check_deviceconfig_status(environment, deviceconfig_install.devicecfg_list)

        for exp_config, _ in exporter_config_defn.items():
            # Delete
            ret_code, ret_stdout, ret_stderr = k8_util.k8_delete_configmap(environment.gpu_operator_namespace,
                                                                           exp_config)
            if ret_code != 0:
                Logger.warn(f"Failed to delete metrics-exporter configmap {exp_config}")
        return

    request.addfinalizer(_cleanup_configmap)

    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('metrics-exporter', len(gpu_nodes), 1),
    ]
    failed_exp_config_metrics = []
    failed_exp_config_labels = []
    failed_endpoints = set()
    for exp_config, label_metrics_tuple in exporter_config_defn.items():
        Logger.info(f"Testing with exporter-config {exp_config}")
        for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
            tcfg['metricsExporter.config'] = exp_config
            cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
            ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
            K8Helper.triage(environment, (ret_code == 0), f"Failed to create deviceconfig, stderr: {ret_stderr}")

            # Check for corresponding deviceconfig created
            K8Helper.check_deviceconfig_status(environment, deviceconfig_install.devicecfg_list)

            failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods)
            K8Helper.triage(environment, not failed_pods, f"One or more pods are not ready - {failed_pods}")
            time.sleep(30) # Wait for config-map is read by exporter pod
            expected_metrics = set(label_metrics_tuple[1])
            expected_metrics.update(['promhttp_metric_handler_errors_total'])
            expected_labels = set(label_metrics_tuple[0])
            expected_labels.update(mandatory_labels)
            for node in gpu_nodes:
                node_ip = k8_util.k8_get_node_address(node)
                cluster_node = gpu_cluster.get_worker_node(node_ip)
                if not cluster_node:
                    pytest.fail(f"Unable to get worker node from cluster for ip: {node_ip}")
                node_hostname = k8_util.k8_get_node_hostname(node)
                node_port = deviceconfig_install.exporter_port_map[node_hostname]
                ret_code, resp, _ = cluster_node.http_get(node_port, "metrics")
                # Commenting out following as this rely on ssh access to each node
                #if ret_code != 0:
                #    # try from node itself
                #    ret_code, resp, _ = cluster_node.proxy_http_get(node_ip, node_port, "metrics", token = token)

                if ret_code != 0:
                    Logger.error(f"Failed to get metrics from nodeport endpoint for {node_ip}, stdout: {ret_stdout} stderr: {ret_stderr}")
                    failed_endpoints.add(node_ip)
                    continue
                metric_util.dump_metrics(resp, os.path.join(environment.logdir, f"{node_ip}_{exp_config}_metrics.txt"))
                obs_metric_info = metric_util.parse_metric_data(resp)
                obs_metrics = set(obs_metric_info.keys())

                # Check for metrics
                if obs_metrics != expected_metrics:
                    Logger.error(f"Mismatch in metrics Expected : {expected_metrics} vs Observed : {obs_metrics} config-map:{exp_config}")
                    if expected_metrics - obs_metrics:
                        Logger.error(f"Missing: {expected_metrics - obs_metrics}")
                    if obs_metrics - expected_metrics:
                        Logger.error(f"Unexpected: {obs_metrics - expected_metrics}")
                    failed_exp_config_metrics.append((exp_config, f"Expected:{expected_metrics}, Observed:{obs_metrics}"))

                # Check for labels associated with each exported metric
                for metric_name, metric_data_list in obs_metric_info.items():
                    if metric_name in {'promhttp_metric_handler_errors_total', 'gpu_nodes_total'}:
                        continue
                    label_check_failed = False
                    for metric_data in metric_data_list:
                        observed_labels = set(metric_data['labels'].keys())
                        if len(expected_labels - observed_labels) > 0:
                            Logger.error(f"Missing labels with config-map:{exp_config}, error: {expected_labels - observed_labels}")
                            label_check_failed = True
                    if label_check_failed and exp_config not in failed_exp_config_labels:
                        failed_exp_config_labels.append((exp_config, f"Expected:{expected_labels}, Observed:{observed_labels}"))

    # Do final verification
    K8Helper.triage(environment, len(failed_endpoints) == 0, f"One or more metric endpoints HTTP-GET failed, nodes: {failed_endpoints}")
    K8Helper.triage(environment, (len(failed_exp_config_metrics) == 0),
                    f"Export ConfigMap (Fields) failed for {failed_exp_config_metrics} cases")
    K8Helper.triage(environment, (len(failed_exp_config_labels) == 0),
                    f"Export ConfigMap (Labels) failed for {failed_exp_config_labels} cases")


def verify_gpu_capacity_status(environment, worker):
    i = 0
    while i < 10:
        cap, alloc = k8_util.k8_get_node_gpu_capacity(worker)
        if cap == alloc:
            return
        time.sleep(10)
        i = i + 1
    debug_on_failure(environment, i < 10,
                     f"capacity = allocatable {cap} != {alloc}")

def verify_no_label(environment, profile):
    i = 0
    while i < 30:
        ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
        if gpu_nodes and gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile', 'NA') == profile and \
                gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state', 'NA') == "failure":
            break
        i += 1
        time.sleep(2)
    debug_on_failure(environment, i < 30,
            f"didn't find {profile} or state=failure in labels:\n{pprint.pprint(gpu_nodes[0]['metadata']['labels'])}")

 
def verify_label(environment, profile):
    i = 0
    prof = "NA"
    stat = "unknown"
    while i < 30:
        ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
        if gpu_nodes:
            prof = gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile', 'NA')
            stat = gpu_nodes[0]['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state', 'unknown')
            if prof == profile and stat == "success":
                break
        i += 1
        time.sleep(10)
    debug_on_failure(environment, i < 30,
                     f"Didn't find gpu-config-profile-state=success, found {prof} or\
                       Didn't find gpu-config-profile={profile}, found {stat}")


def verify_logs(environment, log_msg_list, pod_str="config-manager", since="1800s", container=None, optional=False):
    global Logger
    global LogPrettyPrinter
    namespace = environment.gpu_operator_namespace

    i = 0
    ret_code, stdout, stderr = k8_util.k8_get_pod_logs(pod_str, namespace, since, container)

    flag = False
    for log_msg in log_msg_list:
        while log_msg not in stdout and i < 30:
            time.sleep(30)
            i = i + 1
            ret_code, stdout, stderr = k8_util.k8_get_pod_logs(pod_str, namespace, since, container)
        if optional and log_msg in stdout:
            flag = True
            break
        debug_on_failure(environment, log_msg in stdout,
                         f"didn't find {log_msg} in\n" + LogPrettyPrinter.pformat(stdout.split('\n')))
        if optional:
            debug_on_failure(environment, flag,
                             f"didn't find one of {log_msg_list} in\n" + LogPrettyPrinter.pformat(stdout.split('\n')))

def wait_for_pods(environment, local_workload_ctxts):
    workload_pods = []
    status_info = None
    for ctxt in local_workload_ctxts:
        workload_pods.append(common.PodInfo(ctxt['pod_name'], 1, 1))

    for _ in range(5):
        status_info = k8_util.k8_check_pod_status("default", workload_pods)
        statuses = [status for name, (status, full_pod_info) in status_info.items()]
        if "Pending" in statuses:
            time.sleep(5)
        else:
            break
    K8Helper.collect_unhealthy_pods(environment, workload_pods)
    return status_info

#Unsupported compute partition combination
@pytest.mark.level12
@pytest.mark.parametrize("profile", ["invalidgpucount",
                                     "invalmemorytype",
                                     "invalcomputetype",
                                     "invalidmissingfields-numGPUs",
                                     "invalidmissingfields-memoryPartition",
                                     "invalidmissingfields-computePartition",
                                     "highgpucount_mostly_invalid"])
def test_negative_partitioning(request, gpu_cluster, deviceconfig_install, environment, profile):
    global Logger
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if 'MI2' in gpu_series:
        pytest.skip(f"skipping tests for gpu_series = {gpu_series}")

    DCM_LOG_MATCH = {
        "invalmemorytype" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
        ],
        "invalcomputetype" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
        ],
        "invalidgpucount" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
            "does not equal the total number of GPUs available on this node",
        ],
        "invalidmissingfields-numGPUs" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
            "does not equal the total number of GPUs available on this node",
        ],
        "invalidmissingfields-memoryPartition" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
        ],
        "invalidmissingfields-computePartition" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
        ],
        "highgpucount_mostly_invalid" : [
            f"Selected Profile {profile} found in the configmap",
            "Profile validation failed. Could not partition",
            "does not equal the total number of GPUs available on this node",
        ]
    }
    local_workload_ctxts = []
    namespace = environment.gpu_operator_namespace
    dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
    file_path = os.path.join("lib", "files", f"partitioning_check_{gpu_series}_{dut_node.num_gpus}.json")
    with open(file_path) as fp:
        profiles = json.load(fp)
        if not profiles.get("gpu-config-profiles"):
            pytest.fail(f"check {file_path}, something wrong with the configmap")
        elif not profiles["gpu-config-profiles"].get(profile, False):
            pytest.skip(f"Profile {profile} is not supported for {gpu_series}. Refer {file_path}")

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    for node in gpu_nodes:
        worker = k8_util.k8_get_node_hostname(node)
        if node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile'):
            Logger.info(f"Record existing profile = {node['metadata']['labels']['dcm.amd.com/gpu-config-profile']}")
        else:
            Logger.info("Didn't find any existing profile")
        if node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state'):
            Logger.info(f"Record existing state = {node['metadata']['labels']['dcm.amd.com/gpu-config-profile-state']}")
        else:
            Logger.info("Didn't find any existing profile state")
    Logger.info(f"to be changed to profile: {profile}")

    def _cleanup_after():
        reset_dcm_profile(gpu_cluster, environment)
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            k8_util.k8_untaint_node(node_name)

    request.addfinalizer(_cleanup_after)

    patch_body = {
        "spec": {
            "configManager": {
                "configManagerTolerations": [
                    {
                        "effect": "NoSchedule",
                        "key": "amd-dcm",
                        "operator": "Equal",
                        "value": "up"
                    }
                ]
            }
        }
    }

    api_client = client.ApiClient()
    custom_objects_api = client.CustomObjectsApi(api_client)
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        try:
            custom_objects_api.patch_namespaced_custom_object(
                group="amd.com",
                version='v1alpha1',
                name=devcfg_name,
                namespace=namespace,
                plural='deviceconfigs',
                body=patch_body
            )
            Logger.info(f"Successfully patched with {patch_body}")
        except client.ApiException as e:
            debug_on_failure(environment, False, f"Failed to patch custom object: {e}")

    # Watch for all pod creation
    time.sleep(20)
    devicecfg_pods = [
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    pre_partition_status = {}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        pod_name = k8_util.k8_get_pod_name("config-manager", namespace, node_name)
        ret_code, output, resp_stderr = k8_util.exec_command_in_pod(namespace, ["amd-smi", "partition", "-c", "--json"], pod_name)
        debug_on_failure(environment, (ret_code == 0), f"Failed to collect current partition information, error : {resp_stderr}")
        Logger.debug(f"Current Partition Status: {LogPrettyPrinter.pformat(output)}")
        pre_partition_status[node_name] = extract_partition_info(environment, output)

    labels_dict = {"dcm.amd.com/gpu-config-profile" : profile}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        k8_util.k8_taint_node(node_name, taint_add=True)
        k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
        verify_no_label(environment, profile)

    time.sleep(20) # Time to reinit
    verify_logs(environment, DCM_LOG_MATCH.get(profile, []))

    post_partition_status = {}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        pod_name = k8_util.k8_get_pod_name("config-manager", namespace, node_name)
        ret_code, output, resp_stderr = k8_util.exec_command_in_pod(namespace, ["amd-smi", "partition", "-c", "--json"], pod_name)
        debug_on_failure(environment, (ret_code == 0), f"Failed to collect current partition information, error : {resp_stderr}")
        Logger.debug(f"Current Partition Status: {LogPrettyPrinter.pformat(output)}")
        post_partition_status[node_name] = extract_partition_info(environment, output)
    debug_on_failure(environment, (pre_partition_status == post_partition_status),
                     f"GPU Partition unexpectedly changed, pre: {pre_partition_status}, post: {post_partition_status}")


def run_partition_test_scenario(gpu_cluster, environment, request, profile, workload):
    global Logger
    gpu_series = get_gpu_series(gpu_cluster, environment)
    dut_node = gpu_cluster.find_node_by_gpu_series(gpu_series)
    local_workload_ctxts = []
    namespace = environment.gpu_operator_namespace
    file_path = os.path.join("lib", "files", f"partitioning_check_{gpu_series}_{dut_node.num_gpus}.json")
    before_events = k8_util.k8_get_events(namespace=environment.gpu_operator_namespace)

    with open(file_path) as fp:
        profiles = json.load(fp)
        if not profiles.get("gpu-config-profiles"):
            pytest.fail(f"check {file_path}, something wrong with the configmap")
        elif not profiles["gpu-config-profiles"].get(profile, False):
            pytest.skip(f"Profile {profile} is not supported for {gpu_series}. Refer {file_path}")
        else:
            memory = profiles["gpu-config-profiles"][profile]["profiles"][0]["memoryPartition"]
            partition = profiles["gpu-config-profiles"][profile]["profiles"][0]["computePartition"]
            GPUs = profiles["gpu-config-profiles"][profile]["profiles"][0]["numGPUsAssigned"]

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    for node in gpu_nodes:
        worker = k8_util.k8_get_node_hostname(node)
        if node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile'):
            Logger.info(f"Record existing profile = {node['metadata']['labels']['dcm.amd.com/gpu-config-profile']}")
        else:
            Logger.info("Didn't find any existing profile")
        if node['metadata']['labels'].get('dcm.amd.com/gpu-config-profile-state'):
            Logger.info(f"Record existing state = {node['metadata']['labels']['dcm.amd.com/gpu-config-profile-state']}")
        else:
            Logger.info("Didn't find any existing profile state")
    Logger.info(f"to be changed to profile: {profile}")

    #add_tolerations(environment)
    def _untaint_all_nodes():
        for node in gpu_nodes:
            node_name = node['metadata']['labels']['kubernetes.io/hostname']
            k8_util.k8_untaint_node(node_name)
    request.addfinalizer(_untaint_all_nodes)

    if workload:
        def _start_workload():
            params = {
                "node_name" : worker,
                "num_gpu_reqd" : 1,
                "workload_selection" : "busybox-workload",
            }
            wl_ctxt = K8Helper.workload_operation(environment, K8Helper.WorkloadOp.START_WORKLOAD, **params)
            local_workload_ctxts.append(wl_ctxt)
        def _cleanup_workload():
            for ctxt in local_workload_ctxts:
                K8Helper.workload_operation(environment, K8Helper.WorkloadOp.STOP_WORKLOAD, **ctxt)
            return

        request.addfinalizer(_cleanup_workload)
        _untaint_all_nodes()
        _start_workload()
        status_info = wait_for_pods(environment, local_workload_ctxts)
        for name, (status, full_pod_info) in status_info.items():
            debug_on_failure(environment, status == 'Running',
                             f"Workload not in RUNNING state, {pprint.pformat(status_info)}")

    # Watch for all pod creation
    time.sleep(20)
    devicecfg_pods = [
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    pre_partition_status = {}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        pod_name = k8_util.k8_get_pod_name("config-manager", namespace, node_name)
        ret_code, output, resp_stderr = k8_util.exec_command_in_pod(namespace, ["amd-smi", "partition", "-c", "--json"], pod_name)
        debug_on_failure(environment, (ret_code == 0), f"Failed to collect current partition information, error : {resp_stderr}")
        Logger.debug(f"Current Partition Status: {LogPrettyPrinter.pformat(output)}")
        pre_partition_status[node_name] = extract_partition_info(environment, output)

    patch_body = {
        "spec": {
            "configManager": {
                "configManagerTolerations": [
                    {
                        "effect": "NoSchedule",
                        "key": "amd-dcm",
                        "operator": "Equal",
                        "value": "up"
                    }
                ]
            }
        }
    }

    api_client = client.ApiClient()
    custom_objects_api = client.CustomObjectsApi(api_client)
    devcfg_map = k8_util.k8_get_deviceconfigs_info(environment.gpu_operator_namespace)
    for devcfg_name, _ in devcfg_map.items():
        try:
            custom_objects_api.patch_namespaced_custom_object(
                group="amd.com",
                version='v1alpha1',
                name=devcfg_name,
                namespace=namespace,
                plural='deviceconfigs',
                body=patch_body
            )
            Logger.info(f"Successfully patched with {patch_body}")
        except client.ApiException as e:
            debug_on_failure(environment, False, f"Failed to patch custom object: {e}")

    labels_dict = {"dcm.amd.com/gpu-config-profile" : profile}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        k8_util.k8_taint_node(node_name, taint_add=True)
        k8_util.k8_label_node(node_name, labels_dict, overwrite=True)
        verify_label(environment, profile)
    time.sleep(30)

    if workload:
        # Since earlier workload would have evicted, recreate and check it lands in PENDING state
        _start_workload()
        status_info = wait_for_pods(environment, local_workload_ctxts)
        debug_on_failure(environment, status_info[local_workload_ctxts[-1]['pod_name']] == 'Pending',
                         f"Workload not in PENDING state, {pprint.pformat(local_workload_ctxts[-1])}")
        debug_on_failure(environment, status_info[local_workload_ctxts[0]['pod_name']] == 'Running',
                         f"Workload not in RUNNING state, {pprint.pformat(local_workload_ctxts[0])}")

    _untaint_all_nodes()

    # Watch for all pod creation
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    match_logs = [
            f"Requested compute partition {partition}",
            f"Requested memory partition {memory}",
            f"Selected Profile {profile} found in the configmap",
            #"Gpu-config-profile-state label added successfully",
            "AMD SMI shutdown successfully",
            "ServicesList"]

    for gpu in range(GPUs):
        match_logs.append(f"GPU ID {gpu}")

    verify_logs(environment, match_logs)

    time.sleep(20)
    post_partition_status = {}
    for node in gpu_nodes:
        node_name = node['metadata']['labels']['kubernetes.io/hostname']
        pod_name = k8_util.k8_get_pod_name("config-manager", namespace, node_name)
        ret_code, output, resp_stderr = k8_util.exec_command_in_pod(namespace, ["amd-smi", "partition", "-c", "--json"], pod_name)
        debug_on_failure(environment, (ret_code == 0), f"Failed to collect current partition information, error : {resp_stderr}")
        Logger.debug(f"Current Partition Status: {LogPrettyPrinter.pformat(output)}")
        post_partition_status[node_name] = extract_partition_info(environment, output)
    # TODO : Check applied partition in post_partition_status
    # If pre is SPX_NPS1 and requested profile is SPX_NPS1, the no change will be observed
    debug_on_failure(environment, (pre_partition_status != post_partition_status),
                     f"No change in partition-status, pre: {pre_partition_status}, post: {post_partition_status}",
                     expected_to_fail = True)
    after_events = k8_util.k8_get_events(namespace=environment.gpu_operator_namespace)

    if workload:
        status_info = wait_for_pods(environment, local_workload_ctxts)
        workload_status = []
        for status in status_info.values():
            workload_status.append(status == 'Running')
        debug_on_failure(environment, all(workload_status), 
                         f"Some of the workloads not in Running state: {pprint.pformat(local_workload_ctxts)}")
    Logger.info("verify events in the testcase")
    verify_events(gpu_cluster, environment, profile, before_events, after_events)
    worker = k8_util.k8_get_node_hostname(gpu_nodes[0])
    verify_gpu_capacity_status(environment, worker)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS2", "DPX_NPS1", "CPX_NPS1", "CPX_NPS2", "SPX_NPS1"])
def test_partitioning_no_workload_MI350X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI350X':
        pytest.skip(f"Testcases specifically designed for MI350X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS2", "DPX_NPS1", "CPX_NPS1", "CPX_NPS2", "SPX_NPS1"])
def test_partitioning_workload_MI350X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI350X':
        pytest.skip(f"Testcases specifically designed for MI350X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = True)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS2", "DPX_NPS1", "CPX_NPS1", "CPX_NPS2", "SPX_NPS1"])
def test_partitioning_no_workload_MI350P(gpu_cluster, deviceconfig_install, environment, request,
                                         create_dcm_configmap, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI350P':
        pytest.skip(f"Testcases specifically designed for MI350P")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS2", "DPX_NPS1", "CPX_NPS1", "CPX_NPS2", "SPX_NPS1"])
def test_partitioning_workload_MI350P(gpu_cluster, deviceconfig_install, environment, request,
                                         create_dcm_configmap, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI350P':
        pytest.skip(f"Testcases specifically designed for MI350P")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = True)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS1", "QPX_NPS4", "CPX_NPS1", "CPX_NPS4", "SPX_NPS1"])
def test_partitioning_no_workload_MI300X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI300X':
        pytest.skip(f"Testcases specifically designed for MI300X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS1", "QPX_NPS4", "CPX_NPS1", "CPX_NPS4", "SPX_NPS1"])
def test_partitioning_workload_MI300X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI300X':
        pytest.skip(f"Testcases specifically designed for MI300X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = True)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS4", "DPX_NPS1", "CPX_NPS1", "CPX_NPS4", "SPX_NPS1"])
def test_partitioning_no_workload_MI325X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI325X':
        pytest.skip(f"Testcases specifically designed for MI325X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)

@pytest.mark.level2
@pytest.mark.parametrize("profile", ["QPX_NPS1", "DPX_NPS2", "QPX_NPS4", "DPX_NPS1", "CPX_NPS1", "CPX_NPS4", "SPX_NPS1"])
def test_partitioning_workload_MI325X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if gpu_series != 'MI325X':
        pytest.skip(f"Testcases specifically designed for MI325X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = True)


@pytest.mark.level23
@pytest.mark.parametrize("profile", ["CPX_NPS1"])
def test_partitioning_63_workloads_MI350X(gpu_cluster, deviceconfig_install, environment, request, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if 'MI350X' not in gpu_series:
        pytest.skip(f"Testcases specifically designed for MI350X")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)
    def _cleanup_workload():
        k8_util.k8_delete_all_pods("default")

    request.addfinalizer(_cleanup_workload)
    _cleanup_workload()
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    for node in gpu_nodes:
        worker = k8_util.k8_get_node_hostname(node)
        for i in range(63):
            params = {
                "node_name" : worker,
                "num_gpu_reqd" : 1,
                "workload_selection" : "alexnet-tf-gpu"
            }
            wl_ctxt = K8Helper.workload_operation(environment, K8Helper.WorkloadOp.START_WORKLOAD, **params)
    time.sleep(60)
    ret_code, list_of_pods = k8_util.k8_get_pods("default")

    debug_on_failure(environment, len(list_of_pods) == 63,
                     f"found no running workloads in {pprint.pformat(list_of_pods)}")

    exporter_nodeport_exp_config(request, gpu_cluster, deviceconfig_install, environment)

@pytest.mark.level23
@pytest.mark.parametrize("profile", ["CPX_NPS1"])
def test_partitioning_63_workloads_MI350P(gpu_cluster, deviceconfig_install, environment, request,
                                          create_dcm_configmap, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    if 'MI350P' not in gpu_series:
        pytest.skip(f"Testcases specifically designed for MI350P")
    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)
    def _cleanup_workload():
        k8_util.k8_delete_all_pods("default")

    request.addfinalizer(_cleanup_workload)
    _cleanup_workload()
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    for node in gpu_nodes:
        worker = k8_util.k8_get_node_hostname(node)
        for i in range(63):
            params = {
                "node_name" : worker,
                "num_gpu_reqd" : 1,
                "workload_selection" : "alexnet-tf-gpu"
            }
            wl_ctxt = K8Helper.workload_operation(environment, K8Helper.WorkloadOp.START_WORKLOAD, **params)
    time.sleep(60)
    ret_code, list_of_pods = k8_util.k8_get_pods("default")

    debug_on_failure(environment, len(list_of_pods) == 63,
                     f"found no running workloads in {pprint.pformat(list_of_pods)}")

    exporter_nodeport_exp_config(request, gpu_cluster, deviceconfig_install, environment)

@pytest.mark.level23
@pytest.mark.parametrize("profile", ["CPX_NPS4", "DPX_NPS2"])
def test_partitioning_test_runner(gpu_cluster, deviceconfig_install, environment, request, images, profile):
    gpu_series = get_gpu_series(gpu_cluster, environment)
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    if 'MI3' not in gpu_series:
        pytest.skip(f"Testcases specifically designed for MI350X")

    framework = "RVS"
    recipe = "gst_single"
    configmap = {}
    for node in gpu_nodes:
        worker = k8_util.k8_get_node_hostname(node)
        update_test_runner_configmap(recipe, worker, configmap, framework)

    configmap_name = create_configmap(request, deviceconfig_install, environment, framework, configmap)
    update_test_runner_image(deviceconfig_install, environment, framework, configmap_name)

    run_partition_test_scenario(gpu_cluster, environment, request, profile, workload = False)

    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('metrics-exporter', len(gpu_nodes), 1),
        common.PodInfo('test-runner', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    job_name = "test-runner-manual-trigger"
    namespace = environment.gpu_operator_namespace
    sa_name = "test-run"
    cluster_role_name = "test-run-cluster-role"
    crb_name = 'test-run-rb'

    def _cleanup_jobs():
        k8_util.k8_delete_job(namespace, job_name)
        k8_util.k8_delete_cluster_role_binding(crb_name)
        k8_util.k8_delete_cluster_role(cluster_role_name)
        k8_util.k8_delete_service_account(sa_name, namespace)
    request.addfinalizer(_cleanup_jobs)
    _cleanup_jobs()

        # Create ServiceAccount
    ret_code, ret_stdout, ret_stderr = k8_util.k8_create_service_account(sa_name, namespace)
    debug_on_failure(environment, (ret_code == 0),
                     f"Failed to create service-account, error:{ret_stderr}")

    # Define ClusterRole: verb=get

    #rules = k8_util.k8_create_rules_from_endpoint_list([("/test_runner", "get")])
    rules = list()
    rules.append(
        k8_util.k8_create_rules_from_verbs(
            resources=["events"],
            verbs=["get", "list", "watch", "create", "update"],
            api_groups=[""]
        )
    )
    rules.append(
        k8_util.k8_create_rules_from_verbs(
            resources=["nodes"],
            verbs=["patch"],
            api_groups=[""]
        )
    )
    # Define ClusterRole: verb=get
    ret_code, ret_stdout, ret_stderr = k8_util.k8_create_cluster_role(cluster_role_name, rules)
    debug_on_failure(environment, (ret_code == 0),
                     f"Failed to create test_runner clusterrole with GET, error:{ret_stderr}")

    ret_code, ret_stdout, ret_stderr = k8_util.k8_create_role_binding(crb_name, namespace, cluster_role_name, sa_name)
    debug_on_failure(environment, (ret_code == 0),
                              f"Failed to create test_runner clusterrole with verbs, error:{ret_stderr}")
    # Create token for ServiceAccount
    token = k8_util.k8_create_token(namespace, sa_name, "1h")
    debug_on_failure(environment, token != None,
                     f"Failed to create token for the service-account : {sa_name}")
    Logger.info(f"TOKEN={token}")

    time.sleep(30) # Wait for exporter to start working
    # Get endpoint for each node

    # Create Job
    k8_util.k8_create_test_runner_job(namespace,
                                      images,
                                      worker,
                                      sa_name,
                                      job_name,
                                      framework,
                                      True,
                                      False,
                                      datetime.datetime.utcnow().minute + 2)
    job_status = k8_util.k8_get_job_status(namespace, job_name)
    debug_on_failure(environment, job_status == "Running",
                     f"job should be in Running state")
    #no need to wait for completion GPUOP-520
    verify_logs(environment, [f'Starting iteration 1 of 1 for test: {recipe}'], 'test-runner-manual')
    k8_util.k8_delete_job(namespace, job_name)

@pytest.mark.parametrize("upgrade_policy", ["RollingUpdate", "OnDelete"])
def test_config_manager_operand_upgrade(deviceconfig_install, environment, alternative_images, upgrade_policy):
    global Logger
    if environment.gpu_operator_version < "v1.3.0":
        pytest.skip(f"DCM Operand upgrade feature is not available in release before v1.2.0")

    if (images['configManager.image.version'] == alternative_images['configManager.image.version']):
        pytest.fail("Invalid input for operand upgrade testcase - both version same")

    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    K8Helper.triage(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    K8Helper.triage(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    # Check current version of the configManager from deviceconfig-CR
    def _modify_config_manager_version(repo, version):
        for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
            tcfg['configManager.image.repository'] = repo
            tcfg['configManager.image.version'] = version
            tcfg['configManager.upgradePolicy.upgradeStrategy'] = upgrade_policy
            cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
            ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
            K8Helper.triage(environment, (ret_code == 0), "Failed to modify deviceconfig CR")

    def _restore_config_manager():
        _modify_config_manager_version(images['configManager.image.repository'],
                                       images['configManager.image.version'])
        devicecfg_pods = [
            common.PodInfo('config-manager', len(gpu_nodes), 1),
        ]
        failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods)
        K8Helper.triage(environment, not failed_pods, f"One or more pods are not ready - {failed_pods}")

    request.addfinalizer(_restore_config_manager)

    K8Helper.check_deviceconfig_status(environment, deviceconfig_install.devicecfg_list)
    ret_code, orig_dcm_pods = k8_util.k8_get_pods(environment.gpu_operator_namespace, pod_name_pattern = "config-manager")
    K8Helper.triage(environment, (ret_code == 0 and len(orig_dcm_pods) > 0), f"Missing config-manager pods or error")
    _modify_config_manager_version(alternative_images['configManager.image.repository'],
                                     alternative_images['configManager.image.version'])

    if upgrade_policy == "RollingUpdate":
        Logger.debug("Wait until upgrade is complete...")
    elif upgrade_policy == "OnDelete":
        # Check no upgrade is kicked in
        time.sleep(20)
        ret_code, precheck_dcm_pods = k8_util.k8_get_pods(environment.gpu_operator_namespace, pod_name_pattern = "config-manager")
        for old_pod, new_pod in zip(orig_dcm_pods, precheck_dcm_pods):
            for o_s_info, n_s_info in zip(old_pod['status']['container_statuses'], new_pod['status']['container_statuses']):
                if o_s_info['name'] == 'config-manager-container' and n_s_info['name'] == 'config-manager-container':
                    K8Helper.triage(environment, (o_s_info['image'] == n_s_info['image']),
                                    f"Version mismatch before pod-deletion with policy: {upgrade_policy}, {o_s_info}, {n_s_info}")
        # explicitly delete the pod
        k8_util.k8_delete_all_pods_with_name_pattern(environment.gpu_operator_namespace, 'config-manager')

    time.sleep(20)
    devicecfg_pods = [
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods)
    K8Helper.triage(environment, not failed_pods, f"One or more pods are not ready - {failed_pods}")
    ret_code, new_dcm_pods = k8_util.k8_get_pods(environment.gpu_operator_namespace, pod_name_pattern = "config-manager")
    K8Helper.triage(environment, (ret_code == 0 and len(new_dcm_pods) > 0), f"Missing config-manager pods or error")
    K8Helper.check_deviceconfig_status(environment, deviceconfig_install.devicecfg_list)

    # Check latest version of the configManager from deviceconfig-CR and match to alternative_images
    for pod in new_dcm_pods:
        for s_info in pod['status']['container_statuses']:
            if s_info['name'] == 'config-manager-container':
                K8Helper.triage(environment, (alternative_images['configManager.image.version'] in s_info['image']),
                                f"Unexpected version found in the config-manager-container image post upgrade, {s_info}")
                K8Helper.triage(environment, (alternative_images['configManager.image.repository'] in s_info['image']),
                                f"Unexpected version found in the config-manager-container image post upgrade, {s_info}")

@pytest.mark.level1
def test_deviceconfig_config_manager_disable(gpu_cluster, deviceconfig_install, environment):
    global Logger
    ret_code, gpu_nodes = k8_util.k8_get_gpu_nodes()
    debug_on_failure(environment, (ret_code == 0), "Error while getting gpu-nodes from k8-cluster")
    debug_on_failure(environment, (len(gpu_nodes) > 0), "No nodes with AMD/GPU found in the cluster")

    # reset and reload driver via reboot - to handle inbox driver case as well
    gpu_series = get_gpu_series(gpu_cluster, environment)
    skip_reboot = (gpu_series not in ["MI325X"])
    reset_dcm_profile(gpu_cluster, environment, skip_reboot = skip_reboot)
    # disable config-manager
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['configManager.enable'] = False
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(environment, (ret_code == 0), "Failed to modify deviceconfig CR")

    export_pods = [
        common.PodInfo('config-manager', 1, 1),
    ]
    running_pods = k8_util.k8_check_pod_terminated(environment.gpu_operator_namespace, export_pods)
    debug_on_failure(environment, not running_pods,
                              f"Some of the pods are still running post uninstallation - {running_pods}")
    # Watch for all pod creation
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

    # re-enable config-manager
    for spec_name, tcfg in deviceconfig_install.test_cfg_map.items():
        tcfg['configManager.enable'] = True
        cr_spec = spec_util.generate_k8_deviceconfig_cr(environment.gpu_operator_version, tcfg)
        ret_code, ret_stdout, ret_stderr = k8_util.k8_modify_deviceconfig_cr(cr_spec)
        debug_on_failure(environment, (ret_code == 0), "Failed to modify deviceconfig CR")

    # Watch for all pod creation
    devicecfg_pods = [
        common.PodInfo('device-plugin', len(gpu_nodes), 1),
        common.PodInfo('config-manager', len(gpu_nodes), 1),
    ]
    failed_pods = k8_util.k8_check_pod_running(environment.gpu_operator_namespace, devicecfg_pods, sleep_time = 20)
    debug_on_failure(environment, (not failed_pods), f"One or more pods are not ready - {failed_pods}")

