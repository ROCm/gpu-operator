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
import sys
import os
import copy
import logging
import shutil
import json
import io
from packaging import version
from ruamel.yaml import YAML
from ruamel.yaml import comments
from ruamel.yaml import scalarstring
from pathlib import Path
from collections import defaultdict

Logger = logging.getLogger("lib.specutil")
yaml = YAML()
yaml.preserve_quotes = True
yaml.indent(sequence=4, offset=2)
DQ = scalarstring.DoubleQuotedScalarString

device_config_template_v1_0_0 = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
        },
        'devicePlugin' : {
            'enableNodeLabeller' : False,
        },
        'metricsExporter' : {
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
    },
}

device_config_template_v1_2_0 = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'commonConfig' : {
        },
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
            'upgradePolicy' : {
                'enable' : False,
                'maxParallelUpgrades' : 1,
                'maxUnavailableNodes' : '25%',
                'nodeDrainPolicy' : {
                    'force' : False,
                    'timeoutSeconds' : 300,
                },
                'rebootRequired' : False,
            },
            'imageBuild' : {
                'baseImageRegistry' : 'docker.io',
            },
        },
        'devicePlugin' : {
            'devicePluginImagePullPolicy' : 'Always',
            'enableNodeLabeller' : False,
            'nodeLabellerImagePullPolicy' : 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'metricsExporter' : {
            'imagePullPolicy'   : 'Always',
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'testRunner' : {
            'enable' : False,
            'config' : None,
            'imagePullPolicy': 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
    },
}

device_config_template_v1_3_0 = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'commonConfig' : {
        },
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
            'upgradePolicy' : {
                'enable' : False,
                'maxParallelUpgrades' : 1,
                'maxUnavailableNodes' : '25%',
                'nodeDrainPolicy' : {
                    'force' : False,
                    'timeoutSeconds' : 300,
                },
                'rebootRequired' : True,
            },
            'imageBuild' : {
                'baseImageRegistry' : 'docker.io',
            },
        },
        'devicePlugin' : {
            'devicePluginImagePullPolicy' : 'Always',
            'enableNodeLabeller' : False,
            'nodeLabellerImagePullPolicy' : 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'metricsExporter' : {
            'imagePullPolicy'   : 'Always',
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
            'prometheus' : {
                'serviceMonitor' : {
                    'enable': False,
                    'honorLabels': False,
                    'honorTimestamps': False,
                    'interval': '30s',
                    'attachMetadata' : {
                        'node': False,
                    },
                    'relabelings': [
                        {
                            'sourceLabels': ['pod'],
                            'targetLabel': 'exporter_pod',
                            'action': 'replace',
                            'regex': '(.*)',
                            'replacement': '$1',
                        },
                        {
                            'action': 'labeldrop',
                            'regex': 'pod',
                        },
                    ],
                },
            },
        },
        'testRunner' : {
            'enable' : False,
            'config' : None,
            'imagePullPolicy': 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'configManager' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
    },
}

device_config_template_v1_4_1 = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'commonConfig' : {
        },
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
            'upgradePolicy' : {
                'enable' : False,
                'maxParallelUpgrades' : 1,
                'maxUnavailableNodes' : '25%',
                'nodeDrainPolicy' : {
                    'force' : False,
                    'timeoutSeconds' : 300,
                },
                'rebootRequired' : True,
            },
            'imageBuild' : {
                'baseImageRegistry' : 'docker.io',
            },
        },
        'devicePlugin' : {
            'devicePluginImagePullPolicy' : 'Always',
            'enableNodeLabeller' : False,
            'nodeLabellerImagePullPolicy' : 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'metricsExporter' : {
            'imagePullPolicy'   : 'Always',
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
            'podAnnotations' : {},
            'serviceAnnotations' : {},
            'prometheus' : {
                'serviceMonitor' : {
                    'enable': False,
                    'honorLabels': False,
                    'honorTimestamps': False,
                    'interval': '30s',
                    'attachMetadata' : {
                        'node': False,
                    },
                    'relabelings': [
                        {
                            'sourceLabels': ['pod'],
                            'targetLabel': 'exporter_pod',
                            'action': 'replace',
                            'regex': '(.*)',
                            'replacement': '$1',
                        },
                        {
                            'action': 'labeldrop',
                            'regex': 'pod',
                        },
                    ],
                },
            },
        },
        'testRunner' : {
            'enable' : False,
            'config' : None,
            'imagePullPolicy': 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'configManager' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
    },
}

device_config_template_v1_5_0 = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'commonConfig' : {
        },
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
            'upgradePolicy' : {
                'enable' : False,
                'maxParallelUpgrades' : 1,
                'maxUnavailableNodes' : '25%',
                'nodeDrainPolicy' : {
                    'force' : False,
                    'timeoutSeconds' : 300,
                },
                'rebootRequired' : True,
            },
            'imageBuild' : {
                'baseImageRegistry' : 'docker.io',
            },
        },
        'devicePlugin' : {
            'devicePluginImagePullPolicy' : 'Always',
            'enableNodeLabeller' : False,
            'nodeLabellerImagePullPolicy' : 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'metricsExporter' : {
            'imagePullPolicy'   : 'Always',
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
            'podAnnotations' : {},
            'serviceAnnotations' : {},
            'prometheus' : {
                'serviceMonitor' : {
                    'enable': False,
                    'honorLabels': False,
                    'honorTimestamps': False,
                    'interval': '30s',
                    'attachMetadata' : {
                        'node': False,
                    },
                    'relabelings': [
                        {
                            'sourceLabels': ['pod'],
                            'targetLabel': 'exporter_pod',
                            'action': 'replace',
                            'regex': '(.*)',
                            'replacement': '$1',
                        },
                        {
                            'action': 'labeldrop',
                            'regex': 'pod',
                        },
                    ],
                },
            },
        },
        'testRunner' : {
            'enable' : False,
            'config' : None,
            'imagePullPolicy': 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'configManager' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'draDriver' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
        'remediationWorkflow': {
            'autoStartWorkflow': True,
            'enable': False,
            'ttlForFailedWorkflows': '24h'
        },
    },
}

device_config_template_main = {
    'apiVersion'    : 'amd.com/v1alpha1',
    'kind'          : 'DeviceConfig',
    'metadata'      : {
        'name'      : 'test-deviceconfig',
        'namespace' : 'default',
    },
    'spec'          : {
        'commonConfig' : {
        },
        'driver'    : {
            'enable': False,
            'blacklist' : True,
            'imageRegistryTLS' : {
                'insecure'                  : True,
                'insecureSkipTLSVerify'     : True,
            },
            'upgradePolicy' : {
                'enable' : False,
                'maxParallelUpgrades' : 1,
                'maxUnavailableNodes' : '25%',
                'nodeDrainPolicy' : {
                    'force' : False,
                    'timeoutSeconds' : 300,
                },
                'rebootRequired' : True,
            },
            'imageBuild' : {
                'baseImageRegistry' : 'docker.io',
            },
        },
        'devicePlugin' : {
            'devicePluginImagePullPolicy' : 'Always',
            'enableNodeLabeller' : False,
            'nodeLabellerImagePullPolicy' : 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'metricsExporter' : {
            'imagePullPolicy'   : 'Always',
            'enable'            : False,
            'nodePort'          : 32500,
            'port'              : 5000,
            'serviceType'       : DQ('ClusterIP'),
            'rbacConfig' : {
                'enable'        : False,
                'disableHttps'  : True,
            },
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
            'podAnnotations' : {},
            'serviceAnnotations' : {},
            'prometheus' : {
                'serviceMonitor' : {
                    'enable': False,
                    'honorLabels': False,
                    'honorTimestamps': False,
                    'interval': '30s',
                    'attachMetadata' : {
                        'node': False,
                    },
                    'relabelings': [
                        {
                            'sourceLabels': ['pod'],
                            'targetLabel': 'exporter_pod',
                            'action': 'replace',
                            'regex': '(.*)',
                            'replacement': '$1',
                        },
                        {
                            'action': 'labeldrop',
                            'regex': 'pod',
                        },
                    ],
                },
            },
        },
        'testRunner' : {
            'enable' : False,
            'config' : None,
            'imagePullPolicy': 'Always',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'configManager' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'draDriver' : {
            'enable' : False,
            'imagePullPolicy' : 'IfNotPresent',
            'upgradePolicy' : {
                'maxUnavailable' : 1,
                'upgradeStrategy' : 'RollingUpdate',
            },
        },
        'selector' : {
            'feature.node.kubernetes.io/amd-gpu' : DQ('true'),
        },
        'remediationWorkflow': {
            'autoStartWorkflow': True,
            'enable': False,
            'ttlForFailedWorkflows': '24h'
        },
    },
}

device_config_templates = {
    'v1.0.0'    : device_config_template_v1_0_0,
    'v1.1.0'    : device_config_template_v1_0_0,
    'v1.2.0'    : device_config_template_v1_2_0,
    'v1.2.1'    : device_config_template_v1_2_0,
    'v1.2.2'    : device_config_template_v1_2_0,
    'v1.3.0'    : device_config_template_v1_3_0,
    'v1.4.0'    : device_config_template_v1_3_0,
    'v1.4.1'    : device_config_template_v1_4_1,
    'v1.5.0'    : device_config_template_v1_5_0,
    'v99.99.99' : device_config_template_main,
}

device_config_template_default = device_config_template_main

gpu_operator_helm_deployment_template_0 = {
    'node-feature-discovery' : {
        'enabled': DQ('true'),
    },
    'installdefaultNFDRule': DQ('true'),
    'upgradeCRD': DQ('true'),
    'kmm' : {
        'enabled' : DQ('true'),
        'controller': {
            'manager': {
                'env': {
                    'relatedImageSign': 'rocm/kernel-module-management-signimage',
                    'relatedImageWorker': 'rocm/kernel-module-management-worker',
                },
                'image': {
                    'repository': 'rocm/kernel-module-management-operator',
                },
            }
        },
        'webhookServer': {
            'webhookServer': {
                'image': {
                    'repository': 'rocm/kernel-module-management-webhook-server',
                },
            }
        }
    },
    # AMD GPU operator controller related configs
    'controllerManager': {
        'manager': {
            'args': [
                '--config=controller_manager_config.yaml',
            ],
            'containerSecurityContext': {
                'allowPrivilegeEscalation': False,
            },
            'image': {
              # -- AMD GPU operator controller manager image repository
                'repository': 'rocm/gpu-operator',
              # -- AMD GPU operator controller manager image tag
                #'tag': '',
            },
            # -- Image pull policy for AMD GPU operator controller manager pod
            'imagePullPolicy': 'Always',
            # -- Image pull secret name for pulling AMD GPU operator controller manager image if registry needs credential to pull image
            #'imagePullSecrets': '',
            'tolerations': [
                {
                    'key': "node-role.kubernetes.io/master",
                    'operator': "Equal", 
                    'value': "",
                    'effect': "NoSchedule",
                },
                {
                    'key': "node-role.kubernetes.io/control-plane",
                    'operator': "Equal",
                    'value' : "",
                    'effect': "NoSchedule",
                }
            ],
        },
        # -- Node selector for AMD GPU operator controller manager deployment
        'nodeSelector': {},
        # -- Deployment affinity configs for controller manager
        'affinity': {
            'nodeAffinity': {
                'preferredDuringSchedulingIgnoredDuringExecution': [
                    {
                        'weight': 1,
                        'preference': {
                            'matchExpressions': [
                                {
                                    'key': 'node-role.kubernetes.io/control-plane',
                                    'operator': 'Exists',
                                }
                            ]
                        }
                    }
                ]
            }
        },
        'replicas': 1,
    }
}

exporter_helm_deployment_template_0 = {
    'platform': 'k8s',
    'nodeSelector': {}, # Optional: Add custom nodeSelector
    'tolerations': [],  # Optional: Add custom tolerations
    'kubelet': {
        'podResourceAPISocketPath': '/var/lib/kubelet/pod-resources',
    },
    'image': {
        'repository': 'rocm/device-metrics-exporter',
        'tag': '',
        'pullPolicy': 'Always',
    },
    'configMap': "", # Optional: Add custom configuration
    'service': {
        'type': 'ClusterIP',  # or NodePort
        'ClusterIP': {
            'port': 5000,
        },
        'NodePort' : {
            'nodePort' : 32500,
            'port' : 5000,
        },
    },
    # ServiceMonitor configuration for Prometheus Operator integration
    'serviceMonitor': {
        'enabled': False,
        'interval': '30s',
        'honorLabels': True,
        'honorTimestamps': True,
        'labels': {},
        'relabelings': [],
    }
}

def dump_yaml(file_name, data):
    with open(file_name, 'w') as fp:
        yaml.dump(data, fp)
    return data

def get_yaml(data):
    str_stream = io.StringIO()
    yaml.dump(data, str_stream)
    yaml_data = str_stream.getvalue()
    return yaml_data

def generate_k8_deviceconfig_cr(gpu_operator_version, spec = {}, skip_sections = {}):
    global Logger

    if "main" in gpu_operator_version or "collab-7.12" in gpu_operator_version:
        gpu_op_version = version.Version("v99.99.99")
    else:
        gpu_op_version = version.Version(gpu_operator_version.split("-")[0])

    device_config = copy.deepcopy(device_config_templates.get(f"v{str(gpu_op_version)}", device_config_template_default))
    device_config['metadata']['name'] = spec.get('metadata.name', 'deviceconfig')
    device_config['metadata']['namespace'] = spec.get('metadata.namespace', 'default')

    # commonConfig
    if not skip_sections.get('commonConfig', False):
        if spec.get('commonConfig.initContainerImage.repository', None) and spec.get('commonConfig.initContainerImage.version', None):
            img = f"{spec['commonConfig.initContainerImage.repository']}:{spec['commonConfig.initContainerImage.version']}"
            device_config['spec']['commonConfig']['initContainerImage'] = img
        if gpu_op_version >= version.Version('v1.5.0'):
            if spec.get('commonConfig.utilsContainer.repository', None) and spec.get('commonConfig.utilsContainer.version', None):
                img = f"{spec['commonConfig.utilsContainer.repository']}:{spec['commonConfig.utilsContainer.version']}"
                utils_container_cfg = device_config['spec']['commonConfig'].setdefault('utilsContainer', {})
                utils_container_cfg['image'] = img
                if spec.get('commonConfig.utilsContainer.secret', None):
                    utils_container_cfg['imageRegistrySecret'] = {
                        'name' : spec.get('commonConfig.utilsContainer.secret')
                    }
    else:
        del device_config['spec']['commonConfig']

    # driver
    if not skip_sections.get('driver', False):
        if spec.get('driver.image.repository', None):
            device_config['spec']['driver']['image'] = spec.get('driver.image.repository')
        device_config['spec']['driver']['version'] = DQ(spec.get('driver.version', '6.2.2'))
        device_config['spec']['driver']['enable'] = spec.get('driver.enable', False)
        device_config['spec']['driver']['blacklist'] = spec.get('driver.blacklist', True)
        if gpu_op_version >= version.Version('v1.2.0'):
            rebootReqDefault = True
            if gpu_op_version == version.Version('v1.2.0'):
                rebootReqDefault = False
            device_config['spec']['driver']['upgradePolicy']['enable'] = spec.get('driver.upgradePolicy.enable', False)
            device_config['spec']['driver']['upgradePolicy']['rebootRequired'] = spec.get('driver.upgradePolicy.rebootRequired', rebootReqDefault)
            device_config['spec']['driver']['upgradePolicy']['maxParallelUpgrades'] = spec.get('driver.upgradePolicy.maxParallelUpgrades', 1)
            device_config['spec']['driver']['upgradePolicy']['maxUnavailableNodes'] = spec.get('driver.upgradePolicy.maxUnavailableNodes', "25%")
            device_config['spec']['driver']['upgradePolicy']['nodeDrainPolicy']['force'] = spec.get('driver.upgradePolicy.nodeDrainPolicy.force', False)
            device_config['spec']['driver']['upgradePolicy']['nodeDrainPolicy']['timeoutSeconds'] = spec.get('driver.upgradePolicy.nodeDrainPolicy.timeoutSeconds', 300)
            if spec.get('driver.imageBuild.baseImageRegistry', None):
                device_config['spec']['driver']['imageBuild']['baseImageRegistry'] = spec.get('driver.imageBuild.baseImageRegistry')
                device_config['spec']['driver']['imageBuild']['baseImageRegistryTLS'] = {
                    'insecure' : True,
                    'insecureSkipTLSVerify' : True,
                }
    else:
        del device_config['spec']['driver']

    # device-plugin
    if not skip_sections.get('devicePlugin', False):
        if spec.get('devicePlugin.devicePluginImage.repository', None):
            img = f"{spec.get('devicePlugin.devicePluginImage.repository')}:{spec.get('devicePlugin.devicePluginImage.version')}"
            device_config['spec']['devicePlugin']['devicePluginImage'] = img

        # node-labeller
        if spec.get('devicePlugin.nodeLabellerImage.repository', None):
            img = f"{spec.get('devicePlugin.nodeLabellerImage.repository')}:{spec.get('devicePlugin.nodeLabellerImage.version')}"
            device_config['spec']['devicePlugin']['nodeLabellerImage'] = img
        device_config['spec']['devicePlugin']['enableNodeLabeller'] = spec.get('devicePlugin.enableNodeLabeller', False)
        # enableDevicePlugin field (for DRA driver compatibility)
        if spec.get('devicePlugin.enableDevicePlugin', None) is not None:
            device_config['spec']['devicePlugin']['enableDevicePlugin'] = spec.get('devicePlugin.enableDevicePlugin', True)
        if gpu_op_version >= version.Version("v1.2.0"):
            device_config['spec']['devicePlugin']['upgradePolicy']['maxUnavailable'] = spec.get('devicePlugin.upgradePolicy.maxUnavailable', 1)
            device_config['spec']['devicePlugin']['upgradePolicy']['upgradeStrategy'] = spec.get('devicePlugin.upgradePolicy.upgradeStrategy', 'RollingUpdate')
        if spec.get('devicePlugin.devicePluginImage.secret', None):
            device_config['spec']['devicePlugin']['imageRegistrySecret'] = {
                    'name' : spec.get('devicePlugin.devicePluginImage.secret')
            }
    else:
        del device_config['spec']['devicePlugin']

    # metrics-exporter
    if not skip_sections.get('metricsExporter', False):
        if spec.get('metricsExporter.image.repository', None):
            img = f"{spec['metricsExporter.image.repository']}:{spec['metricsExporter.image.version']}"
            device_config['spec']['metricsExporter']['image'] = img
        if spec.get('metricsExporter.enable', False):
            device_config['spec']['metricsExporter']['enable'] = spec.get('metricsExporter.enable', False)
        if spec.get('metricsExporter.serviceType', None):
            device_config['spec']['metricsExporter']['serviceType'] = DQ(spec.get('metricsExporter.serviceType'))
            device_config['spec']['metricsExporter']['nodePort'] = spec.get('metricsExporter.nodePort', 32500)
            device_config['spec']['metricsExporter']['port'] = spec.get('metricsExporter.port', 5000)
        if spec.get('metricsExporter.config', None):
            device_config['spec']['metricsExporter']['config'] = {
                    'name' : spec.get('metricsExporter.config')
            }
        else:
            if 'config' in device_config['spec']['metricsExporter']:
                del device_config['spec']['metricsExporter']['config']
        device_config['spec']['metricsExporter']['rbacConfig']['enable'] = spec.get('metricsExporter.rbacConfig.enable', False)
        device_config['spec']['metricsExporter']['rbacConfig']['disableHttps'] = spec.get('metricsExporter.rbacConfig.disableHttps', True)
        if spec.get('metricsExporter.rbacConfig.secret.name' ,None):
                device_config['spec']['metricsExporter']['rbacConfig'].setdefault('secret', {})
                device_config['spec']['metricsExporter']['rbacConfig']['secret']['name'] = spec.get('metricsExporter.rbacConfig.secret.name', None)
        if spec.get('metricsExporter.image.secret', None):
            device_config['spec']['metricsExporter']['imageRegistrySecret'] = {
                    'name' : spec.get('metricsExporter.image.secret')
            }
        if gpu_op_version >= version.Version('v1.2.0'):
            device_config['spec']['metricsExporter']['upgradePolicy']['maxUnavailable'] = spec.get('metricsExporter.upgradePolicy.maxUnavailable', 1)
            device_config['spec']['metricsExporter']['upgradePolicy']['upgradeStrategy'] = spec.get('metricsExporter.upgradePolicy.upgradeStrategy', 'RollingUpdate')
        if gpu_op_version >= version.Version('v1.2.2'):
            if spec.get('prometheus.serviceMonitor.enable', False):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['enable'] = True
            if spec.get('prometheus.serviceMonitor.honorLabels', False):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['honorLabels'] = True
            if spec.get('prometheus.serviceMonitor.honorTimestamps', False):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['honorTimestamps'] = True
            if spec.get('prometheus.serviceMonitor.interval', None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['interval'] = spec.get('prometheus.serviceMonitor.interval', '30s')
            if spec.get('prometheus.serviceMonitor.attachMetadata.node', False):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['attachMetadata']['node'] = spec.get('prometheus.serviceMonitor.attachMetadata.node', True)
            if spec.get('prometheus.serviceMonitor.relabelings',None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['relabelings'] = spec.get('prometheus.serviceMonitor.relabelings', [])
            if spec.get('prometheus.serviceMonitor.labels',None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['labels'] = spec.get('prometheus.serviceMonitor.labels', {})
            if spec.get('prometheus.serviceMonitor.tlsConfig.ca.configMap', None) :
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor'].setdefault('tlsConfig', {}).setdefault('ca', {})
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['tlsConfig']['ca']['configMap'] = spec.get('prometheus.serviceMonitor.tlsConfig.ca.configMap', {})
            if spec.get('prometheus.serviceMonitor.tlsConfig.cert.secret', None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor'].setdefault('tlsConfig', {}).setdefault('cert', {})
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['tlsConfig']['cert']['secret'] = spec.get('prometheus.serviceMonitor.tlsConfig.cert.secret',{})
            if spec.get('prometheus.serviceMonitor.tlsConfig.keySecret', None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor'].setdefault('tlsConfig', {})
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['tlsConfig']['keySecret'] = spec.get('prometheus.serviceMonitor.tlsConfig.keySecret',"")
            if spec.get('prometheus.serviceMonitor.tlsConfig.serverName', None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor'].setdefault('tlsConfig', {})
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['tlsConfig']['serverName'] = spec.get('prometheus.serviceMonitor.tlsConfig.serverName', None)
            if spec.get('prometheus.serviceMonitor.tlsConfig.insecureSkipVerify', None):
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor'].setdefault('tlsConfig', {})
                device_config['spec']['metricsExporter']['prometheus']['serviceMonitor']['tlsConfig']['insecureSkipVerify'] = spec.get('prometheus.serviceMonitor.tlsConfig.insecureSkipVerify', False)
            if spec.get('metricsExporter.rbacConfig.clientCAConfigMap.name' ,None):
                device_config['spec']['metricsExporter']['rbacConfig'].setdefault('clientCAConfigMap', {})
                device_config['spec']['metricsExporter']['rbacConfig']['clientCAConfigMap']['name'] = spec.get('metricsExporter.rbacConfig.clientCAConfigMap.name', None)
        if gpu_op_version >= version.Version('v1.4.0'):
            device_config['spec']['metricsExporter']['podAnnotations'] = spec.get('metricsExporter.podAnnotations', {})
            device_config['spec']['metricsExporter']['serviceAnnotations'] = spec.get('metricsExporter.serviceAnnotations', {})
    else:
        del device_config['spec']['metricsExporter']

    # test-runner 
    if 'testRunner' in device_config['spec']:
        if not skip_sections.get('testRunner', False):
            if spec.get('testRunner.image.repository', None):
                img = f"{spec.get('testRunner.image.repository')}:{spec.get('testRunner.image.version')}"
                device_config['spec']['testRunner']['image'] = img
            device_config['spec']['testRunner']['enable'] = spec.get('testRunner.enable', False)
            device_config['spec']['testRunner']['imagePullPolicy'] = spec.get('testRunner.imagePullPolicy', 'IfNotPresent')
            if spec.get('testRunner.config', None):
                device_config['spec']['testRunner']['config'] = {
                        'name' : spec.get('testRunner.config'),
                }
            if spec.get('testRunner.image.secret', None):
                device_config['spec']['testRunner']['imageRegistrySecret'] = {
                        'name' : spec.get('testRunner.image.secret')
                }
            if gpu_op_version >= version.Version('v1.2.0'):
                device_config['spec']['testRunner']['upgradePolicy']['maxUnavailable'] = spec.get('testRunner.upgradePolicy.maxUnavailable', 1)
                device_config['spec']['testRunner']['upgradePolicy']['upgradeStrategy'] = spec.get('testRunner.upgradePolicy.upgradeStrategy', 'RollingUpdate')
        elif not skip_sections.get('testRunnerAgfhc', False):
            if spec.get('testRunnerAgfhc.image.repository', None):
                img = f"{spec.get('testRunnerAgfhc.image.repository')}:{spec.get('testRunnerAgfhc.image.version')}"
                device_config['spec']['testRunner']['image'] = img
            device_config['spec']['testRunner']['enable'] = spec.get('testRunnerAgfhc.enable', False)
            device_config['spec']['testRunner']['imagePullPolicy'] = spec.get('testRunnerAgfhc.imagePullPolicy', 'IfNotPresent')
            if spec.get('testRunnerAgfhc.config', None):
                device_config['spec']['testRunner']['config'] = {
                        'name' : spec.get('testRunnerAgfhc.config'),
                }
            if spec.get('testRunnerAgfhc.image.secret', None):
                device_config['spec']['testRunner']['imageRegistrySecret'] = {
                        'name' : spec.get('testRunnerAgfhc.image.secret')
                }
            device_config['spec']['testRunner']['upgradePolicy']['maxUnavailable'] = spec.get('testRunnerAgfhc.upgradePolicy.maxUnavailable', 1)
            device_config['spec']['testRunner']['upgradePolicy']['upgradeStrategy'] = spec.get('testRunnerAgfhc.upgradePolicy.upgradeStrategy', 'RollingUpdate')
        else:
            del device_config['spec']['testRunner']

    # config-manager
    if 'configManager' in device_config['spec']:
        if not skip_sections.get('configManager', False):
            if spec.get('configManager.image.repository', None):
                img = f"{spec.get('configManager.image.repository')}:{spec.get('configManager.image.version')}"
                device_config['spec']['configManager']['image'] = img
            device_config['spec']['configManager']['enable'] = spec.get('configManager.enable', False)
            device_config['spec']['configManager']['imagePullPolicy'] = spec.get('configManager.imagePullPolicy', 'IfNotPresent')
            if spec.get('configManager.config', None):
                device_config['spec']['configManager']['config'] = {
                        'name' : spec.get('configManager.config'),
                }
            if spec.get('configManager.image.secret', None):
                device_config['spec']['configManager']['imageRegistrySecret'] = {
                        'name' : spec.get('configManager.image.secret')
                }
            device_config['spec']['configManager']['upgradePolicy']['maxUnavailable'] = spec.get('configManager.upgradePolicy.maxUnavailable', 1)
            device_config['spec']['configManager']['upgradePolicy']['upgradeStrategy'] = spec.get('configManager.upgradePolicy.upgradeStrategy', 'RollingUpdate')

    # dra-driver
    if 'draDriver' in device_config['spec']:
        if not skip_sections.get('draDriver', False):
            if spec.get('draDriver.image.repository', None):
                img = f"{spec.get('draDriver.image.repository')}:{spec.get('draDriver.image.version')}"
                device_config['spec']['draDriver']['image'] = img
            device_config['spec']['draDriver']['enable'] = spec.get('draDriver.enable', False)
            device_config['spec']['draDriver']['imagePullPolicy'] = spec.get('draDriver.imagePullPolicy', 'IfNotPresent')
            if spec.get('draDriver.image.secret', None):
                device_config['spec']['draDriver']['imageRegistrySecret'] = {
                        'name' : spec.get('draDriver.image.secret')
                }
            device_config['spec']['draDriver']['upgradePolicy']['maxUnavailable'] = spec.get('draDriver.upgradePolicy.maxUnavailable', 1)
            device_config['spec']['draDriver']['upgradePolicy']['upgradeStrategy'] = spec.get('draDriver.upgradePolicy.upgradeStrategy', 'RollingUpdate')
        else:
            del device_config['spec']['draDriver']

    # selector
    if spec.get('selector.field', None) and spec.get('selector.value', None):
        device_config['spec']['selector'] = {
            spec.get('selector.field', 'feature.node.kubernetes.io/amd-gpu') : spec.get('selector.value', DQ('true')),
        }
    # Remediation Workflow:
    if 'remediationWorkflow' in device_config['spec']:
        device_config['spec']['remediationWorkflow'] = {}
        if not skip_sections.get('remediationWorkflow', False):
            if spec.get('remediationWorkflow.enable', None):
                device_config['spec']['remediationWorkflow']['enable'] = spec.get('remediationWorkflow.enable', False)
            if spec.get('remediationWorkflow.autoStartWorkflow', None):
                device_config['spec']['remediationWorkflow']['autoStartWorkflow'] = spec.get('remediationWorkflow.autoStartWorkflow', True)
            if spec.get('remediationWorkflow.ttlForFailedWorkflows', None):
                device_config['spec']['remediationWorkflow']['ttlForFailedWorkflows'] = spec.get('remediationWorkflow.ttlForFailedWorkflows', '24h')
            if spec.get('remediationWorkflow.config', None):
                device_config['spec']['remediationWorkflow']['config'] = {
                    'name' : spec.get('remediationWorkflow.config', None),
                    }
            if spec.get("remediationWorkflow.nodeRemediationLabels", None) is not None:
                device_config["spec"]["remediationWorkflow"]["nodeRemediationLabels"] = spec.get("remediationWorkflow.nodeRemediationLabels")
            if spec.get("remediationWorkflow.maxParallelWorkflows", None) is not None:
                device_config["spec"]["remediationWorkflow"]["maxParallelWorkflows"] = spec.get("remediationWorkflow.maxParallelWorkflows")
            if spec.get('remediationWorkflow.testerImage.repository', None) and spec.get('remediationWorkflow.testerImage.version', None):
                img = f"{spec.get('remediationWorkflow.testerImage.repository')}:{spec.get('remediationWorkflow.testerImage.version')}"
                device_config['spec']['remediationWorkflow']['testerImage'] = img      
        else:
            del device_config['spec']['remediationWorkflow']

    return device_config

def generate_helmchart_deployment_config(gpu_operator_version, images, secret_list, file_name):
    '''
    Generate values.yaml used to install gpu-operator helm-chart
    '''

    modifed = False
    helmchart_values = copy.deepcopy(gpu_operator_helm_deployment_template_0)

    if secret_list:
        helmchart_values['global'] = {
                'imagePullSecrets' : [],
        }
        for secret in secret_list:
            helmchart_values['global']['imagePullSecrets'].append({'name' : secret})

    # kmm controller manager image-sign
    kmm_sign_prefix = 'kmm.controller.manager.env.relatedImageSign'
    if images.get(f'{kmm_sign_prefix}.repository', None):
        modifed = True
        img = images.get(f'{kmm_sign_prefix}.repository') + ":" + images.get(f'{kmm_sign_prefix}.version', 'latest')
        helmchart_values['kmm']['controller']['manager']['env']['relatedImageSign'] = img
    if images.get(f"{kmm_sign_prefix}.secret", None):
        helmchart_values['kmm']['controller']['manager']['env']['relatedImageSignPullSecret'] = images.get(f'{kmm_sign_prefix}.secret')

    # kmm controller manager image-worker
    kmm_worker_prefix = 'kmm.controller.manager.env.relatedImageWorker'
    if images.get(f"{kmm_worker_prefix}.repository", None):
        modifed = True
        img = images.get(f'{kmm_worker_prefix}.repository') + ":" + images.get(f'{kmm_worker_prefix}.version', 'latest')
        helmchart_values['kmm']['controller']['manager']['env']['relatedImageWorker'] = img
    if images.get(f"{kmm_worker_prefix}.secret", None):
        helmchart_values['kmm']['controller']['manager']['env']['relatedImageWorkerPullSecret'] = images.get(f'{kmm_worker_prefix}.secret')

    # kmm controller manager
    kmm_manager_prefix = 'kmm.controller.manager.image'
    if images.get(f'{kmm_manager_prefix}.repository', None):
        modifed = True
        helmchart_values['kmm']['controller']['manager']['image']['repository'] = images.get(f'{kmm_manager_prefix}.repository')
    if images.get(f'{kmm_manager_prefix}.version', None):
        modifed = True
        helmchart_values['kmm']['controller']['manager']['image']['tag'] = images.get(f'{kmm_manager_prefix}.version')
    if images.get(f'{kmm_manager_prefix}.secret', None):
        modifed = True
        helmchart_values['kmm']['controller']['manager']['imagePullSecrets'] = images.get(f'{kmm_manager_prefix}.secret')

    # kmm webhook-server
    kmm_webhook_prefix = 'kmm.webhookServer.webhookServer.image'
    if images.get(f'{kmm_webhook_prefix}.repository', None):
        modifed = True
        helmchart_values['kmm']['webhookServer']['webhookServer']['image']['repository'] = images.get(f'{kmm_webhook_prefix}.repository')
    if images.get(f'{kmm_webhook_prefix}.version', None):
        modifed = True
        helmchart_values['kmm']['webhookServer']['webhookServer']['image']['tag'] = images.get(f'{kmm_webhook_prefix}.version')
    if images.get(f'{kmm_webhook_prefix}.secret', None):
        modifed = True
        helmchart_values['kmm']['webhookServer']['webhookServer']['imagePullSecrets'] = images.get(f'{kmm_webhook_prefix}.secret')

    # AMD GPU operator controller related configs
    ctrl_manager_prefix = 'controllerManager.manager.image'
    if images.get(f'{ctrl_manager_prefix}.repository', None):
        modifed = True
        helmchart_values['controllerManager']['manager']['image']['repository'] = images.get(f'{ctrl_manager_prefix}.repository')
    if images.get(f'{ctrl_manager_prefix}.version', None):
        modifed = True
        helmchart_values['controllerManager']['manager']['image']['tag'] = images.get(f'{ctrl_manager_prefix}.version')
    if images.get(f'{ctrl_manager_prefix}.secret', None):
        modifed = True
        helmchart_values['controllerManager']['manager']['imagePullSecrets'] = images.get(f'{ctrl_manager_prefix}.secret')

    if modifed:
        return dump_yaml(file_name, helmchart_values)
    return modifed

def generate_exporter_helmchart_deployment_config(dme_version, images, file_name, **kwargs):
    '''
    Generate values.yaml used to install device-metrics-exporter helm-chart
    '''
    modifed = False
    helmchart_values = copy.deepcopy(exporter_helm_deployment_template_0)

    if images.get(f'metricsExporter.image.repository', None):
        img = f"{images['metricsExporter.image.repository']}"
        tag = f"{images['metricsExporter.image.version']}"
        helmchart_values['image']['repository'] = img
        helmchart_values['image']['tag'] = tag
        if images.get('metricsExporter.image.secret', None):
            helmchart_values['image']['pullSecrets'] = images['metricsExporter.image.secret']
        modifed = True

    if 'nodeSelector' in kwargs:
        helmchart_values['nodeSelector'] = kwargs['nodeSelector']

    if 'configMap' in kwargs:
        helmchart_values['configMap'] = kwargs['configMap']
    if 'service.type' in kwargs:
        helmchart_values['service']['type'] = kwargs['service.type']
        modifed = True
    if 'service.ClusterIP.port' in kwargs:
        helmchart_values['service']['ClusterIP']['port'] = kwargs['service.ClusterIP.port']
        modifed = True
    if 'service.NodePort.nodePort' in kwargs:
        helmchart_values['service']['NodePort']['nodePort'] = kwargs['service.NodePort.nodePort']
        modifed = True
    if 'service.NodePort.port' in kwargs:
        helmchart_values['service']['NodePort']['port'] = kwargs['service.NodePort.port']
        modifed = True

    if 'exporter-0.0.1' in dme_version or 'collab-7.12' in dme_version:
        exporter_version = version.Version('v99.99.99') # main
    else:
        exporter_version = version.Version(dme_version.split("-")[0])

    if exporter_version >= version.Version('v1.4.1'):
        if 'podAnnotations' in kwargs:
            helmchart_values['podAnnotations'] = kwargs['podAnnotations']
        if 'service.annotations' in kwargs:
            helmchart_values['service']['annotations'] = kwargs['service.annotations']

    if 'serviceMonitor.enabled' in kwargs:
        helmchart_values['serviceMonitor']['enabled'] = kwargs['serviceMonitor.enabled']

    if modifed:
        return dump_yaml(file_name, helmchart_values)
    return modifed

def generate_k8_workload_template(wl_template, workload_config, wl_cr_file):
    """
    Generates a workload yaml file
    """
    global Logger
    wl_cr = copy.deepcopy(wl_template)
    wl_cr['metadata']['namespace'] = workload_config.get('namespace', 'default')
    wl_cr['metadata']['name'] = workload_config.get('pod_name', 'pytorch-gpu-pod-1')
    wl_cr['spec']['containers'][0]['resources']['limits']['amd.com/gpu'] = workload_config.get('num_gpu_reqd', 1)
    wl_cr['spec']['nodeSelector']['kubernetes.io/hostname'] = workload_config.get('nodeSelector')
    for cntr in wl_cr['spec']['containers']:
        if len(cntr['args']) > 1:
            full_args = "".join(cntr['args'])
            cntr['args'].clear()
            cntr['args'].append(full_args)

    return dump_yaml(wl_cr_file, wl_cr)

def generate_service_account_yaml(file_name, namespace, sa_name):
    """

    Example:
    service-account.yaml
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: exporter-client
      namespace : default
    """

    global Logger
    sa_spec = {
        'apiVersion' : 'v1',
        'kind' : 'ServiceAccount',
        'metadata' : {
            'name' : sa_name,
            'namespace' : namespace,
        }
    }
    return dump_yaml(file_name, sa_spec)

def generate_cluster_role_spec(file_name, cluster_role_name, endpoint_verbs):
    global Logger

    rules = []
    for endpoint in endpoint_verbs:
        rules.append({
            'nonResourceURLs' : [endpoint[0]],
            'verbs' : [endpoint[1]],
        })
    cluster_role_spec = {
        'apiVersion' : 'rbac.authorization.k8s.io/v1',
        'kind' : 'ClusterRole',
        'metadata' : {
            'name' : cluster_role_name,
        },
        'rules' : rules,
    }

    return dump_yaml(file_name, cluster_role_spec)

def generate_clusterrolebinding_yaml(file_name, crb_name,cluster_role, subject_kind='ServiceAccount', subject_name=None, namespace=None):    
    """
    Example:
        apiVersion: rbac.authorization.k8s.io/v1
        kind: ClusterRoleBinding
        metadata:
          name: metrics
        roleRef:
          apiGroup: rbac.authorization.k8s.io
          kind: ClusterRole
          name: metrics
        subjects:
        - kind: ServiceAccount
          name: exporter-client
          namespace: default   # Updated namespace to metrics-reader
    """

    global Logger
    crb_spec = {
        'apiVersion' : 'rbac.authorization.k8s.io/v1',
        'kind' : 'ClusterRoleBinding',
        'metadata' : {
            'name' : crb_name,
        },
        'roleRef' : {
            'apiGroup' : 'rbac.authorization.k8s.io',
            'kind' : 'ClusterRole',
            'name' : cluster_role,
        },
        'subjects' : [
            {
                'kind': subject_kind,
                'name': subject_name,
                **({'namespace': namespace} if subject_kind == 'ServiceAccount' else {})
            }
        ]
    }
    return dump_yaml(file_name, crb_spec)

def build_deviceconfigs_by_hostname(init_test_config, gpu_nodes, ctxt_name, amdgpu_driver_spec):
    global Logger
    test_configs = {}
    for idx, node in enumerate(gpu_nodes):
        local_test_config = copy.deepcopy(init_test_config)
        if amdgpu_driver_spec["driver-deployment"] == "inbox":
            local_test_config['driver.enable'] = False
            local_test_config['driver.version'] = "0.0"
            local_test_config['driver.blacklist'] = False
        else:
            local_test_config['driver.version'] = amdgpu_driver_spec["default-version"]
            local_test_config['driver.blacklist'] = True
        local_test_config['metadata.name'] = f'deviceconfig-{idx}'
        local_test_config['selector.field'] = 'kubernetes.io/hostname'
        local_test_config['selector.value'] = node['metadata']['labels'].get('kubernetes.io/hostname')
        test_configs[f"{ctxt_name}_{idx}"] = local_test_config
    return test_configs

def build_deviceconfig_cr_template(init_test_config, gpu_nodes, ctxt_name, amdgpu_driver_spec):
    global Logger

    selectors = set()
    for idx, node in enumerate(gpu_nodes):
        if 'feature.node.kubernetes.io/amd-vgpu' in node['metadata']['labels']:
            selectors.add('vgpu')
        elif 'feature.node.kubernetes.io/amd-gpu' in node['metadata']['labels']:
            selectors.add('gpu')

    test_configs = {}
    for sel in selectors:
        local_test_config = copy.deepcopy(init_test_config)
        if amdgpu_driver_spec["driver-deployment"] == "inbox":
            local_test_config['driver.enable'] = False
            local_test_config['driver.version'] = "0.0"
            local_test_config['driver.blacklist'] = False
        else:
            local_test_config['driver.version'] = amdgpu_driver_spec["default-version"]
            local_test_config['driver.blacklist'] = True
        if sel == 'vgpu':
            local_test_config['selector.field'] = 'feature.node.kubernetes.io/amd-vgpu'
            local_test_config['selector.value'] = DQ('true')
        else:
            local_test_config['selector.field'] = 'feature.node.kubernetes.io/amd-gpu'
            local_test_config['selector.value'] = DQ('true')
        local_test_config['metadata.name'] = f'devcfg-clusterwide-{sel}'
        test_configs[ctxt_name] = local_test_config
    return test_configs
