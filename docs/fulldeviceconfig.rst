======================
Full Reference Config
======================

.. _full_device_config:

Full DeviceConfig
==================

Below is an example of a full DeviceConfig CR that can be used to install the AMD GPU Operator and its components. This example includes all the available fields and their default values.

.. raw:: html

    <style>
    span.c1 { 
        color: #349934 !important; /* Color of comments in rendered yaml code block; */
    }

    .bd-main .bd-content .bd-article-container {
        max-width: 100%; /* Override the page width to 100%; */
    }

    .bd-sidebar-secondary {
        display: none; /* Disable the secondary sidebar from displaying; */
    }
    </style>

.. code-block:: yaml
  
    apiVersion: amd.com/v1alpha1 
    kind: DeviceConfig #New Custom Resource Definition used by the GPU Operator
    metadata:
      # Name of the DeviceConfig CR. Note that the name of device plugin, node-labeller and metric-explorter pods will be prefixed with 
      name: gpu-operator 
      namespace: kube-amd-gpu # Namespace for the GPU Operator and it's components
    spec: 
      ## AMD GPU Driver Configuration ##
      driver:
        # Set to false to skip driver installation to use inbox or pre-installed driver on worker nodes
        # Set to true to enable operator to install out-of-tree amdgpu kernel module
        enable: false 
        blacklist: false # Set to true to blacklist the amdgpu kernel module which is required for installing out-of-tree driver
        # Specify your repository to host driver image
        # DO NOT include the image tag as AMD GPU Operator will automatically manage the image tag for you
        image: docker.io/username/repo
        # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
        # you can create the docker-registry type secret by running command like:
        # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
        # Make sure you created the secret within the namespace that KMM operator is running
        imageRegistrySecret:
          name: mysecret
      imageRegistryTLS: 
        insecure: false # If true, check for the container image using plain HTTP
        InsecureSkipTLSVerify: false # If true, skip any TLS server certificate validation (useful for self-signed certificates)
      version: "6.3" # Specify the driver version you would like to be installed that coincides with a ROCm version number
      upgradePolicy:
        enable: true
        maxParallelUpgrades: 3 # (Optional) Number of nodes that will be upgraded in parallel. Default is 1
      ## AMD K8s Device Plugin Configuration ##
      commonConfig:
        # (Optional) Specify common values used by all components. 
        initContainerImage: busybox:1.36 # Specify the InitContainerImage to use for all component pods
        utilsContainer: 
          image: docker.io/amdpsdo/gpu-operator-utils:latest # Image to use for the utils container
          imagePullPolicy: IfNotPresent # Image pull policy for the utils container. Either `Always`, `IfNotPresent` or `Never`
          # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
          # you can create the docker-registry type secret by running command like:
          # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
          # Make sure you created the secret within the namespace that KMM operator is running
          imageRegistrySecret:
            name: mysecret
      devicePlugin: 
        enableNodeLabeller: true # enable or disable the node labeller
        # (Optional) Specifying image names are optional. Default image names for shown here if not specified.
        devicePluginImage: rocm/k8s-device-plugin:latest # Change this to trigger metrics exporter upgrade on CR update
        devicePluginImagePullPolicy: IfNotPresent # Image pull policy for the device plugin. Either `Always`, `IfNotPresent` or `Never`
        # devicePluginImagePullPolicy default value is "IfNotPresent" for valid tags, "Always" for no tag or "latest" tag
        devicePluginTolerations:
          key: "key1" # Key is the taint key that the toleration applies to. Empty means match all taint keys. If the key is empty,
          # operator must be "Exists"; this combination means to match all values and all keys.
          operator: "Equal" # Operator represents a key's relationship to the value. Valid operators are Exists and Equal. 
          # Defaults to Equal. Exists is equivalent to wildcard for value, so that a pod can tolerate all taints of a particular category.
          value: "value1" # Value is the taint value the toleration matches to. If the operator is Exists, the value should be empty,
          # otherwise just a regular string.
          effect: "NoSchedule" # Effect indicates the taint effect to match. Empty means match all taint effects. When specified, allowed 
          # values are "NoSchedule", "PreferNoSchedule" and "NoExecute".
          tolerationSeconds: [Expected Int value, not set by default] #Seconds represents the period of time the toleration tolerates the taint. 
          # By default, it is not set, which means tolerate the taint forever (do not evict). Effect needs to be NoExecute for this, 
          # otherwise this field is ignored. Zero and negative values will be treated as 0 (evict immediately) by the system.
        nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest # Change this to trigger metrics exporter upgrade on CR update
        nodeLabellerImagePullPolicy: IfNotPresent # Image pull policy for the node labeller. Either `Always`, `IfNotPresent` or `Never`
        # nodeLabellerImagePullPolicy default value is "IfNotPresent" for valid tags, "Always" for no tag or "latest" tag
        nodeLabellerTolerations:
          key: "key1" # Key is the taint key that the toleration applies to. Empty means match all taint keys. If the key is empty,
          # operator must be "Exists"; this combination means to match all values and all keys.
          operator: "Equal" # Operator represents a key's relationship to the value. Valid operators are Exists and Equal. 
          # Defaults to Equal. Exists is equivalent to wildcard for value, so that a pod can tolerate all taints of a particular category.
          value: "value1" # Value is the taint value the toleration matches to. If the operator is Exists, the value should be empty,
          # otherwise just a regular string.
          effect: "NoSchedule" # Effect indicates the taint effect to match. Empty means match all taint effects. When specified, allowed 
          # values are "NoSchedule", "PreferNoSchedule" and "NoExecute".
          tolerationSeconds: [Expected Int value, not set by default] #Seconds represents the period of time the toleration tolerates the taint. 
          # By default, it is not set, which means tolerate the taint forever (do not evict). Effect needs to be NoExecute for this, 
          # otherwise this field is ignored. Zero and negative values will be treated as 0 (evict immediately) by the system.
        imageRegistrySecret:
          # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
          # you can create the docker-registry type secret by running command like:
          # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
          # Make sure you created the secret within the namespace that KMM operator is running
          name: mysecret
        upgradePolicy:
          #(Optional) If no UpgradePolicy is mentioned for any of the components but their image is changed, the daemonset will
          # get upgraded according to the defaults, which is `upgradeStrategy` set to `RollingUpdate` and `maxUnavailable` set to 1. 
          upgradeStrategy: RollingUpdate, # (Optional) Can be either `RollingUpdate` or `OnDelete`
          maxUnavailable: 1 # (Optional) Number of pods that can be unavailable during the upgrade process. 1 is the default value
      ## AMD GPU Metrics Exporter Configuration ##
      metricsExporter: 
        enable: false # false by Default. Set to true to enable the Metrics Exporter 
        serviceType: ClusterIP # ServiceType used to expose the Metrics Exporter endpoint. Can be either `ClusterIp` or `NodePort`.
        port: 5000 # Note if specifying NodePort as the serviceType use `32500` as the port number must be between 30000-32767
        # (Optional) Specifying metrics exporter image is optional. Default imagename shown here if not specified.
        image: rocm/device-metrics-exporter:v1.2.0 # Change this to trigger metrics exporter upgrade on CR update
        imagePullPolicy: "IfNotPresent" # image pull policy for the metrics exporter container. Either `Always`, `IfNotPresent` or `Never`
        # imagePullPolicy default value is "IfNotPresent" for valid tags, "Always" for no tag or "latest" tag
        config:
          # Name of the ConfigMap that contains the metrics exporter configuration.
          name: gpu-config # (Optional) If the configmap does not exist the DeviceConfig will show a validation error and not start any plugin pods
        upgradePolicy:
          #(Optional) If no UpgradePolicy is mentioned for any of the components but their image is changed, the daemonset will
          # get upgraded according to the defaults, which is `upgradeStrategy` set to `RollingUpdate` and `maxUnavailable` set to 1.
          upgradeStrategy: RollingUpdate, # (Optional) Can be either `RollingUpdate` or `OnDelete`
          maxUnavailable: 1 # (Optional) Number of pods that can be unavailable during the upgrade process. 1 is the default value
        # If specifying a node selector here, the metrics exporter will only be deployed on nodes that match the selector
        # See Item #6 on https://instinct.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html for example usage
        tolerations:
          key: "key1" # Key is the taint key that the toleration applies to. Empty means match all taint keys. If the key is empty,
          # operator must be "Exists"; this combination means to match all values and all keys.
          operator: "Equal" # Operator represents a key's relationship to the value. Valid operators are Exists and Equal. 
          # Defaults to Equal. Exists is equivalent to wildcard for value, so that a pod can tolerate all taints of a particular category.
          value: "value1" # Value is the taint value the toleration matches to. If the operator is Exists, the value should be empty,
          # otherwise just a regular string.
          effect: "NoSchedule" # Effect indicates the taint effect to match. Empty means match all taint effects. When specified, allowed 
          # values are "NoSchedule", "PreferNoSchedule" and "NoExecute".
          tolerationSeconds: [Expected Int value, not set by default] #Seconds represents the period of time the toleration tolerates the taint. 
          # By default, it is not set, which means tolerate the taint forever (do not evict). Effect needs to be NoExecute for this, 
          # otherwise this field is ignored. Zero and negative values will be treated as 0 (evict immediately) by the system.
        imageRegistrySecret:
          # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
          # you can create the docker-registry type secret by running command like:
          # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
          # Make sure you created the secret within the namespace that KMM operator is running
          name: mysecret
        selector:   
          feature.node.kubernetes.io/amd-gpu: "true" # You must include this again as this selector will overwrite the global selector
          amd.com/device-metrics-exporter: "true" # Helpful for when you want to disable the metrics exporter on specific nodes
      ## AMD GPU Device Test Runner Configuration ##
      testRunner: 
        enable: true # false by Default. Set to true to enable the Metrics Exporter 
        serviceType: ClusterIP # ServiceType used to expose the Metrics Exporter endpoint. Can be either `ClusterIp` or `NodePort`.
        port: 5000 # Note if specifying NodePort as the serviceType use `32500` as the port number must be between 30000-32767
        # (Optional) Specifying metrics exporter image is optional. Default imagename shown here if not specified.
        image: docker.io/rocm/test-runner:v1.2.0-beta.0 # Change this to trigger metrics exporter upgrade on CR update
        imagePullPolicy: "IfNotPresent" # image pull policy for the test runner container. Either `Always`, `IfNotPresent` or `Never`
        # imagePullPolicy default value is "IfNotPresent" for valid tags, "Always" for no tag or "latest" tag
        config:
          # Name of the configmap to customize the config for test runner. If not specified default test config will be aplied
          name: test-config # (Optional) If the configmap does not exist the DeviceConfig will show a validation error and not start any plugin pods
        logsLocation:
          mountPath: "/var/log/amd-test-runner" # mount path inside test runner container for log files
          hostPath: "/var/log/amd-test-runner" # host path to be mounted into test runner container for log files
        upgradePolicy:
          #(Optional) If no UpgradePolicy is mentioned for any of the components but their image is changed, the daemonset will
          # get upgraded according to the defaults, which is `upgradeStrategy` set to `RollingUpdate` and `maxUnavailable` set to 1.
          upgradeStrategy: RollingUpdate, # (Optional) Can be either `RollingUpdate` or `OnDelete`
          maxUnavailable: 1 # (Optional) Number of pods that can be unavailable during the upgrade process. 1 is the default value
        # If specifying a node selector here, the metrics exporter will only be deployed on nodes that match the selector
        # See Item #6 on https://instinct.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html for example usage
        tolerations:
          key: "key1" # Key is the taint key that the toleration applies to. Empty means match all taint keys. If the key is empty,
          # operator must be "Exists"; this combination means to match all values and all keys.
          operator: "Equal" # Operator represents a key's relationship to the value. Valid operators are Exists and Equal. 
          # Defaults to Equal. Exists is equivalent to wildcard for value, so that a pod can tolerate all taints of a particular category.
          value: "value1" # Value is the taint value the toleration matches to. If the operator is Exists, the value should be empty,
          # otherwise just a regular string.
          effect: "NoSchedule" # Effect indicates the taint effect to match. Empty means match all taint effects. When specified, allowed 
          # values are "NoSchedule", "PreferNoSchedule" and "NoExecute".
          tolerationSeconds: [Expected Int value, not set by default] #Seconds represents the period of time the toleration tolerates the taint. 
          # By default, it is not set, which means tolerate the taint forever (do not evict). Effect needs to be NoExecute for this, 
          # otherwise this field is ignored. Zero and negative values will be treated as 0 (evict immediately) by the system.
        imageRegistrySecret:
          # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
          # you can create the docker-registry type secret by running command like:
          # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
          # Make sure you created the secret within the namespace that KMM operator is running
          name: mysecret
        selector:   
          feature.node.kubernetes.io/amd-gpu: "true" # You must include this again as this selector will overwrite the global selector
          amd.com/device-test-runner: "true" # Helpful for when you want to disable the test runner on specific nodes 
      selector: 
      # Specify the nodes to be managed by this DeviceConfig Custom Resource.  This will be applied to all components unless a selector 
      # is specified in the component configuration. The node labeller will automatically find nodes with AMD GPUs and apply the label 
      # `feature.node.kubernetes.io/amd-gpu: "true"` to them for you
        feature.node.kubernetes.io/amd-gpu: "true" 


Minimal DeviceConfig
==================
The below is an example of the minimal DeviceConfig CR that can be used to install the AMD GPU Operator and its components. All fields not listed below will revert to their default values. See the above `Full DeviceConfig`_ for all available fields and their default values.

.. code-block:: yaml

  apiVersion: amd.com/v1alpha1
  kind: DeviceConfig
  metadata:
    name: gpu-operator
    namespace: kube-amd-gpu
  spec:
    driver:
      enable: false # Set to false to skip driver installation to use inbox or pre-installed driver on worker nodes
    devicePlugin:
      enableNodeLabeller: true
    metricsExporter:
      enable: true # To enable/disable the metrics exporter, disabled by default
      serviceType: "NodePort" # Node port for metrics exporter service
      nodePort: 32500
      testRunner:
        enable: true
        logsLocation:
          mountPath: "/var/log/amd-test-runner" # mount path inside test runner container for logs
          hostPath: "/var/log/amd-test-runner" # host path to be mounted into test runner container for logs
    selector:
      feature.node.kubernetes.io/amd-gpu: "true"

Metrics Exporter ConfigMap
==========================

.. code-block:: yaml

  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: exporter-configmap
    namespace: kube-amd-gpu
  data:
    config.json: |
      {
        "GPUConfig": {
          "Labels": [
            "GPU_UUID",
            "SERIAL_NUMBER",
            "GPU_ID",
            "POD",
            "NAMESPACE",
            "CONTAINER",
            "JOB_ID",
            "JOB_USER",
            "JOB_PARTITION",
            "CLUSTER_NAME",
            "CARD_SERIES",
            "CARD_MODEL",
            "CARD_VENDOR",
            "DRIVER_VERSION",
            "VBIOS_VERSION",
            "HOSTNAME"
          ]
        }
      }