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
    kind: DeviceConfig # New Custom Resource Definition used by the GPU Operator
    metadata:
      # Name that will prefix device plugin, node-labeller and metrics-exporter pods
      name: gpu-operator
      # Namespace where the GPU Operator and its components will run
      namespace: kube-amd-gpu
    spec: 
      ## AMD GPU Driver Configuration ##
      driver:
        # Set to false to use existing in-tree/pre-installed driver
        # Set to true to install out-of-tree amdgpu kernel module
        # Default: true
``
        enable: false 
        # Set blacklist to true to blacklist the inbox / pre-installed amdgpu kernel module
        # Required when spec.driver.enable is true
        # GPU Worker node reboot is required to apply the blacklist
        blacklist: false
        # Specify the out-of-tree amdgpu driver version you want to install that coincides with a ROCm version number
        version: "6.3"
        # Specify your repository URL to host driver image for out-of-tree amdgpu kernel module
        # DO NOT include the image tag as AMD GPU Operator will automatically manage the image tag for you
        image: docker.io/username/repo
        # (Optional) Specify the credential for your private registry if it requires credential to get pull/push access
        # you can create the docker-registry type secret by running command like:
        # kubectl create secret docker-registry mysecret -n kmm-namespace --docker-username=xxx --docker-password=xxx
        # Make sure you created the secret within the namespace that KMM operator is running
        imageRegistrySecret:
          name: mysecret
        # (Optional) Specify your image registry's TLS config
        imageRegistryTLS: 
          insecure: False # If True, check for the container image using plain HTTP
          insecureSkipTLSVerify: False # If True, skip any TLS server certificate validation (useful for self-signed certificates)
        # (Optional) Specify configuration to sign the driver image
        # Will be used when there is no pre-compiled driver image 
        # and operator is building + signing driver image in one shot within cluster
        # necessary for secure boot enabled system
        imageSign:
          # the private key used to sign kernel modules within image
          keySecret:
            name: my-key-secret
          # the public key used to sign kernel modules within image
          certSecret:
            name: my-cert-secret
      ## AMD K8s Device Plugin Configuration ##
      devicePlugin: 
        # (Optional) Specifying image names are optional. Default image names for shown here if not specified.
        devicePluginImage: rocm/k8s-device-plugin:latest # Change this to trigger metrics exporter upgrade on CR update
        nodeLabellerImage: rocm/k8s-device-plugin:labeller-latest # Change this to trigger metrics exporter upgrade on CR update
        # (Optional) Specify image registry secret to pull device plugin and node labeller images if needed. 
        imageRegistrySecret:
          name: my-deviceplugin-image-secret
        # (Optional) Enable or disable node labeller, default value is true
        enableNodeLabeller: true
      ## AMD GPU Metrics Exporter Configuration ##
      metricsExporter: 
        enable: False # False by Default. Set to True to enable the Metrics Exporter 
        serviceType: ClusterIP # ServiceType used to expose the Metrics Exporter endpoint. Can be either `ClusterIp` or `NodePort`.
        port: 5000 # Used to specify Port the Metrics Exporter service is exposed on when using ClusterIP serviceType
        nodePort: 32500 # Used instead of `port` when using NodePort as the serviceType. The port number must be between 30000-32767
        # (Optional) Specifying metrics exporter image is optional. Default imagename shown here if not specified.
        image: rocm/device-metrics-exporter:latest # Change this to trigger metrics exporter upgrade on CR update
        # (Optional) Specify image registry secret to pull metrics exporter image if needed. 
        # Private registry credentials (optional)
        imageRegistrySecret:
          name: exporter-image-pull-secret
        # Custom metrics exporter configuration (optional)
        config:
          name: exporter-configmap
        # RBAC Proxy Configuration for secure metrics endpoint access (optional)
        rbacConfig:
          # Enable RBAC authentication proxy (Default: false)
          # When enabled, provides authentication and authorization for metrics endpoint        
          enable: false
          # RBAC proxy container image
          # Default: quay.io/brancz/kube-rbac-proxy:v0.18.1          
          image: "quay.io/brancz/kube-rbac-proxy:v0.18.1"
          # TLS configuration for metrics endpoint
          # Set true to disable HTTPS          
          disableHttps: false
          # TLS certificate configuration
          # Default: Auto-generated self-signed certificates         
          secret:
            name: my-kube-rbac-proxy-cert
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