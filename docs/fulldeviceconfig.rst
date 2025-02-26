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
        # Set to True to enable operator to install out-of-tree amdgpu kernel module
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
        insecure: False # If True, check for the container image using plain HTTP
        InsecureSkipTLSVerify: False # If True, skip any TLS server certificate validation (useful for self-signed certificates)
      version: "6.3" # Specify the driver version you would like to be installed that coincides with a ROCm version number
      upgradePolicy:
        enable: true
        maxParallelUpgrades: 3 # (Optional) Number of nodes that will be upgraded in parallel. Default is 1
      ## AMD K8s Device Plugin Configuration ##
      devicePlugin: 
        # (Optional) Specifying image names are optional. Default image names for shown here if not specified.
        devicePluginImage: rocm/k8s-device-plugin:latest # Change this to trigger metrics exporter upgrade on CR update
        nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest # Change this to trigger metrics exporter upgrade on CR update
        upgradePolicy:
          #(Optional) If no UpgradePolicy is mentioned for any of the components but their image is changed, the daemonset will
          # get upgraded according to the defaults, which is `upgradeStrategy` set to `RollingUpdate` and `maxUnavailable` set to 1. 
          upgradeStrategy: RollingUpdate, # (Optional) Can be either `RollingUpdate` or `OnDelete`
          maxUnavailable: 1 # (Optional) Number of pods that can be unavailable during the upgrade process. 1 is the default value
      ## AMD GPU Metrics Exporter Configuration ##
      metricsExporter: 
        enable: False # False by Default. Set to True to enable the Metrics Exporter 
        serviceType: ClusterIP # ServiceType used to expose the Metrics Exporter endpoint. Can be either `ClusterIp` or `NodePort`.
        port: 5000 # Note if specifying NodePort as the serviceType use `32500` as the port number must be between 30000-32767
        # (Optional) Specifying metrics exporter image is optional. Default imagename shown here if not specified.
        image: rocm/device-metrics-exporter:v1.2.0 # Change this to trigger metrics exporter upgrade on CR update
        upgradePolicy:
          #(Optional) If no UpgradePolicy is mentioned for any of the components but their image is changed, the daemonset will
          # get upgraded according to the defaults, which is `upgradeStrategy` set to `RollingUpdate` and `maxUnavailable` set to 1.
          upgradeStrategy: RollingUpdate, # (Optional) Can be either `RollingUpdate` or `OnDelete`
          maxUnavailable: 1 # (Optional) Number of pods that can be unavailable during the upgrade process. 1 is the default value
        # If specifying a node selector here, the metrics exporter will only be deployed on nodes that match the selector
        # See Item #6 on https://dcgpu.docs.amd.com/projects/gpu-operator/en/latest/knownlimitations.html for example usage
        selector:   
          feature.node.kubernetes.io/amd-gpu: "true" # You must include this again as this selector will overwrite the global selector
          amd.com/device-metrics-exporter: "true" # Helpful for when you want to disable the metrics exporter on specific nodes 
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
      enable: False # Set to False to skip driver installation to use inbox or pre-installed driver on worker nodes
    devicePlugin:
      enableNodeLabeller: True
    metricsExporter:
      enable: True # To enable/disable the metrics exporter, disabled by default
      serviceType: "NodePort" # Node port for metrics exporter service
      nodePort: 32500
    selector:
      feature.node.kubernetes.io/amd-gpu: "true"