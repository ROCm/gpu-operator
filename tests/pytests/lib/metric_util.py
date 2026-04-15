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
import logging
import json
import re
import os
import pprint
import time
import threading
from packaging import version
from collections import defaultdict
from prometheus_client.parser import text_string_to_metric_families
import lib.k8_util as k8_util
from lib.util import K8Helper

Logger = logging.getLogger("lib.metricutil")
LogPrettyPrinter = pprint.PrettyPrinter(indent = 2)

def get_label_details(version_string):
    global Logger
    with open('lib/files/label-support-matrix.json', 'r') as fp:
        label_data = json.load(fp)

    if 'main' in version_string or 'exporter' in version_string or 'collab-7.12' in version_string:
        sw_version = version.Version("v99.99.99")
    else:
        sw_version = version.Version(version_string.split('-', 1)[0])

    label_support_info = {}
    for label, info in label_data.items():
        min_version = version.Version(info['min-version'])
        if min_version > sw_version:
            Logger.debug(f"skipping label : {label} with info: {info} for current-version : {sw_version}")
            continue
        if info.get("eos-version", None) != None:
            eos_version = version.Version(info["eos-version"])
            if sw_version > eos_version:
                Logger.debug(f"skipping label : {label} with info: {info} for current-version : {sw_version}")
                continue

        label_support_info[label] = info["mandatory"].get(f"v{str(sw_version)}", "no")
    return label_support_info

def dump_metrics(http_response, out_file):
    metric_data = str(http_response)
    with open(out_file, "w") as fp:
        for line in metric_data.split('\\n'):
            fp.write(line.strip())
            fp.write("\n")
    return

def dump_all_samples(all_metrics, file_prefix):
    for idx, sample in enumerate(all_metrics):
        out_file = f"{file_prefix}_{idx}.output"
        dump_metrics(sample, out_file)
    return

def dump_json_samples(all_json_samples, file_prefix):
    global Logger
    pattern = r'("[^"]+")\s*:\s*"(\[.*?\])"'
    replacement = r'\1: \2'
    for idx, sample in enumerate(all_json_samples):
        out_file = f"{file_prefix}_{idx}.json"
        try:
            new_sample = re.sub(pattern, replacement, sample.replace("'", "\""))
            with open(out_file, "w") as fp:
                json.dump(json.loads(new_sample), fp, indent=4)
        except:
            try:
                # Write as-is so that we can debug json parsing issue offline
                with open(out_file, "w") as fp:
                    fp.write(sample)
            except:
                # finally redirect to logging so that we have data to analyze failure
                Logger.debug(f"Failed to write json sample to file {out_file}")
                Logger.debug(f"{LogPrettyPrinter.pformat(sample)}")
    return

def parse_metric_data(http_response):
    global Logger
    metrics_content = http_response.decode('utf-8')
    metrics = defaultdict(list)
    for metrics_family in text_string_to_metric_families(metrics_content):
        for entry in metrics_family.samples:
            metrics[entry.name].append({
                'type' : metrics_family.type,
                'value' : entry.value,
                'labels' : entry.labels
            })
    return metrics

def get_supported_metrics(gpu_series = None, skip_profiler_metrics = True, amdgpu_driver = None, dme_version = None):
    global Logger
    with open('lib/files/metrics-support.json', 'r') as fp:
        data = json.load(fp)

    metrics = data['metrics']
    # Remove profiler-metrics if enabled
    if skip_profiler_metrics:
        metrics = list(filter(lambda entry: '_PROF_' not in entry['name'], metrics))

    # Filter by gpu-series if defined
    if gpu_series:
        supported_metrics = []
        for entry in metrics:
            for support in entry['gpu-support']:
                if gpu_series in support.get('gpu', []):
                    supported_metrics.append(entry)
                    break
        metrics = supported_metrics

    # check deprecation-matrix
    if amdgpu_driver:
        amdgpu_driver_version = version.Version(amdgpu_driver)
        supported_metrics = []
        for entry in metrics:
            if entry.get("driver-support", None):
                min_version = version.Version(entry["driver-support"].get("ini", "0.0.0"))
                max_version = version.Version(entry["driver-support"].get("fini", "99.99.99"))
                if min_version <= amdgpu_driver_version and amdgpu_driver_version <= max_version:
                    supported_metrics.append(entry)
            else:
                supported_metrics.append(entry)
        metrics = supported_metrics

    # Filter based on supported device-metrics-exporter version
    if dme_version:
        if "exporter-0.0.1" in dme_version or "collab-7.12" in dme_version:
            exporter_version = version.Version("v99.99.99") # TODO: Hack till CI/CD versioning is fixed
            #Logger.debug(f"Running latest/main DME {exporter_version}")
        else:
            exporter_version = version.Version(dme_version.split("-")[0])
            #Logger.debug(f"Running DME {exporter_version}")
        supported_metrics = []
        for entry in metrics:
            if entry.get("exporter-support", None):
                min_version = version.Version(entry["exporter-support"].get("ini", "v1.0.0"))
                max_version = version.Version(entry["exporter-support"].get("fini", "v99.99.99"))
                if min_version <= exporter_version and exporter_version <= max_version:
                    supported_metrics.append(entry)
            else:
                supported_metrics.append(entry)
        metrics = supported_metrics
    return metrics

def is_metric_supported(metric_to_test, gpu_series, amdgpu_driver, dme_version):
    global Logger

    supported_metrics = get_supported_metrics(gpu_series = gpu_series,
                                              skip_profiler_metrics = False,
                                              amdgpu_driver = amdgpu_driver, dme_version = dme_version)

    for entry in supported_metrics:
        metric_name = entry['name']
        if metric_name.lower() == metric_to_test.lower():
            return True
    return False

def get_metric_metadata(metric_to_test):
    global Logger

    all_metrics = get_supported_metrics(skip_profiler_metrics = False)
    for entry in all_metrics:
        metric_name = entry['name']
        if metric_name.lower() == metric_to_test.lower():
            return entry
    return None

def is_metric_contingent(metric_to_test):
    global Logger

    metric_metadata = get_metric_metadata(metric_to_test)
    if metric_metadata:
        metric_types = metric_metadata.get("type", {})
        if metric_types.get("contingent", "no") == "yes":
            return True
    return False

def get_metric_support_info(metric_metadata, gpu_series):
    global Logger

    for support in metric_metadata['gpu-support']:
        if gpu_series in support['gpu']:
            return support
    return None

def health(port, node):
    ret_code, _, _ = node.http_get(port, "metrics")
    assert ret_code == 0, f"Failed to get metrics for {node.ip_address}"

def service_start(node):
    node.run_command("sudo systemctl start amd-metrics-exporter")
    
def service_stop(node):
    node.run_command("sudo systemctl stop amd-metrics-exporter")

def cleanup_cfg(node):
    node.run_command("sudo rm -rf /etc/metrics/")

def collect_metrics_samples(gpu_cluster, gpu_nodes, exporter_port_map, environment, ctxt_name):
    global Logger
    Logger.info(f"Collecting metrics-exporter curl output, amd-smi metrics and gpuctl metrics snapshot")

    def _collect_amd_smi_output(cmd_responses, exporter_pod_name, num_samples = 10, interval = 1):
        cmd = ["amd-smi", "metric", "--json"]
        for _ in range(num_samples):
            ret_code, resp_stdout, resp_stderr = k8_util.exec_command_in_pod(environment.gpu_operator_namespace,
                                                                             cmd, exporter_pod_name,
                                                                             "metrics-exporter-container")
            if ret_code != 0:
                Logger.error(f"Cmd {cmd} failed on {exporter_pod_name}, error : {resp_stderr}")
            else:
                cmd_responses.append(resp_stdout.replace("'", "\"").replace("True", "\"True\"").replace("False", "\"False\""))
            time.sleep(interval)
        return

    def _collect_gpuctl_output(cmd_responses, exporter_pod_name, num_samples = 10, interval = 1):
        cmd = ["gpuctl", "show", "gpu", "--json"]
        for _ in range(num_samples):
            ret_code, resp_stdout, resp_stderr = k8_util.exec_command_in_pod(environment.gpu_operator_namespace,
                                                                             cmd, exporter_pod_name,
                                                                             "metrics-exporter-container")
            if ret_code != 0:
                Logger.error(f"Cmd {cmd} failed on {exporter_pod_name}, error : {resp_stderr}")
            else:
                cmd_responses.append(resp_stdout.replace("'", "\"").replace("True", "\"True\"").replace("False", "\"False\""))
            time.sleep(interval)
        return

    def _collect_exporter_metrics(cmd_responses, cluster_node, num_samples = 10, interval = 1):
        for _ in range(num_samples):
            # Collect 10 exporter_metrics
            ret_code, ret_stdout, ret_stderr = cluster_node.http_get(node_port, "metrics")
            #if ret_code != 0:
            #    # try from node itself
            #    ret_code, ret_stdout, ret_stderr = cluster_node.proxy_http_get(node_ip, node_port, "metrics")

            if ret_code != 0:
                Logger.error(f"Failed to get metrics from nodeport endpoint for {node_ip}, stdout: {ret_stdout} stderr: {ret_stderr}")
            else:
                cmd_responses.append(ret_stdout)
            time.sleep(interval)
        return

    num_samples = 10
    interval = 15
    collected_metrics = {}
    for node in gpu_nodes:
        node_ip = k8_util.k8_get_node_address(node)
        cluster_node = gpu_cluster.find_node_by_ip(node_ip)
        if not cluster_node:
            pytest.fail(f"Unable to get worker node from cluster for ip: {node_ip}")
        node_name = k8_util.k8_get_node_hostname(node)
        node_port = exporter_port_map.get(node_name, 32500)
        exporter_pod_name = k8_util.k8_get_pod_name("metrics-exporter", environment.gpu_operator_namespace, node_name)
        # Collect gpu information from the node
        cmd = ["amd-smi", "static", "--json"]
        ret_code, amd_smi_info, resp_stderr = k8_util.exec_command_in_pod(environment.gpu_operator_namespace,
                                                                          cmd, exporter_pod_name,
                                                                          "metrics-exporter-container")
        K8Helper.triage(environment, (ret_code == 0 and len(amd_smi_info) > 0),
                        f"Unable to collect amd-smi static information from node {node_name}, error : {resp_stderr}")

        threads = []
        exporter_metrics = []
        smi_metrics = []
        gpuctl_metrics = []

        threads.append(threading.Thread(target = _collect_amd_smi_output, args=(smi_metrics, exporter_pod_name, num_samples, interval)))
        threads.append(threading.Thread(target = _collect_exporter_metrics, args=(exporter_metrics, cluster_node, num_samples, interval)))
        if environment.builtin_gpuctl_support:
            threads.append(threading.Thread(target = _collect_gpuctl_output, args=(gpuctl_metrics, exporter_pod_name, num_samples, interval)))

        # Start all the threads
        for thr in threads:
            thr.start()

        time.sleep(num_samples * interval)

        # Wait for all threads to complete
        for thr in threads:
            thr.join()

        collected_metrics[node_name] = {}
        collected_metrics[node_name]['title'] = f"Metrics for {node_name} under {ctxt_name} conditions"
        collected_metrics[node_name]['num-samples'] = num_samples
        collected_metrics[node_name]['gpu-series'] = cluster_node.gpu_series
        collected_metrics[node_name]['gpu-info'] = amd_smi_info
        collected_metrics[node_name]['exporter'] = exporter_metrics
        collected_metrics[node_name]['amd-smi'] = smi_metrics
        collected_metrics[node_name]['gpuctl'] = gpuctl_metrics
        dump_json_samples(smi_metrics, os.path.join(environment.logdir, f"{ctxt_name}_{cluster_node.gpu_series}_smi_metrics"))
        dump_json_samples([amd_smi_info], os.path.join(environment.logdir, f"{ctxt_name}_{cluster_node.gpu_series}_smi_info"))
        dump_all_samples(exporter_metrics, os.path.join(environment.logdir, f"{ctxt_name}_{node_name}_curl"))
        dump_json_samples(gpuctl_metrics, os.path.join(environment.logdir, f"{ctxt_name}_{cluster_node.gpu_series}_gpuctl"))
        K8Helper.triage(environment, (len(smi_metrics) == num_samples),
                        f"Failed to collect all required number of amd-smi-metrics samples for node {node_name}")
        K8Helper.triage(environment, (len(exporter_metrics) == num_samples),
                        f"Failed to collect all required number of metrics-exporter samples for node {node_name}")
        if environment.builtin_gpuctl_support:
            K8Helper.triage(environment, (len(gpuctl_metrics) == num_samples),
                            f"Failed to collect all required number of gpuctl-metrics samples for node {node_name}")
    return collected_metrics

