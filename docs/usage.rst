Quick Start Guide
===================

Getting up and running with the AMD GPU Operator and Device Metrics Exporter on Kubernetes is quick and easy. Below is a short guide on how to get started using the helm installation method on a standard Kubernetes install. Note that more detailed instructions on the different installation methods can be found on this site: 

`GPU Operator Kubernetes Helm Install <./installation/kubernetes-helm.md>`_

`GPU Operator Red Hat OpenShift Install <./installation/openshift-olm.md>`_

Installing the GPU Operator
---------------------------

1. The GPU Operator uses `cert-manager <https://cert-manager.io/>`_ to manage certificates for MTLS communication between services. If you haven't already installed ``cert-manager`` as a prerequisite on your Kubernetes cluster, you'll need to install it as follows:

.. code-block:: bash
    
      # Add and update the cert-manager repository
      helm repo add jetstack https://charts.jetstack.io --force-update

      # Install cert-manager
      helm install cert-manager jetstack/cert-manager \
        --namespace cert-manager \
        --create-namespace \
        --version v1.15.1 \
        --set crds.enabled=true


2. Once ``cert-manager`` is installed, you're just a few commands away from installing the GPU Operating and having a fully managed GPU infrastructure, add the helm repository and fetch the latest helm charts:

.. code-block:: bash
    
      # Add the Helm repository
      helm repo add rocm https://rocm.github.io/gpu-operator
      helm repo update

3. Install the GPU Operator

By using ``helm install`` command you can install the AMD GPU Operator helm charts. 

.. code-block:: bash
      :substitutions:

      helm install amd-gpu-operator rocm/gpu-operator-charts \
            --namespace kube-amd-gpu --create-namespace \
            --version=|version|

.. tip::

      1. Before v1.3.0 the gpu operator helm chart won't provide a default ``DeviceConfig``, you need to take extra step to create a ``DeviceConfig``.
      2. Starting from v1.3.0 the ``helm install`` command would support one-step installation + configuration, which would create a default ``DeviceConfig`` with default values and may not work for all the users with different the deployment scenarios, please refer to :ref:`typical-deployment-scenarios`  for more information and get corresponding ``helm install`` commands. 
      3. The ``--version`` flag is optional, if not specified, the latest version of the GPU Operator will be installed.
      4. The namespace ``kube-amd-gpu`` is the default namespace for GPU Operator, you can change it by using the ``--namespace`` flag.

.. _typical-deployment-scenarios:
Typical Deployment Scenarios
--------------------------------

1. Use VM worker node with VF-Passthrough GPU

If you are using VM based GPU worker node with Virtual Function (VF) Passthrough powered by `AMD MxGPU GIM driver <https://github.com/amd/MxGPU-Virtualization>`_, the VF device would show up in the guest VM. 

You need to adjust the default node selector to ``"feature.node.kubernetes.io/amd-vgpu":"true"`` to make the ``DeviceConfig`` work for your VM based cluster.

.. code-block:: bash
      :substitutions:

      helm install amd-gpu-operator rocm/gpu-operator-charts \
            --namespace kube-amd-gpu --create-namespace \
            --version=|version| \
            --set-json 'deviceConfig.spec.selector={"feature.node.kubernetes.io/amd-gpu":null,"feature.node.kubernetes.io/amd-vgpu":"true"}'

2. Use GPU worker node without inbox / pre-installed driver

If your worker node doesn't have inbox / pre-installed AMD GPU driver loaded, the operand (e.g. deivce plugin, metrics exporter) would stuck at ``Init 0/1`` pod state.

If you plan to use GPU Operator to install out-of-tree driver on your worker nodes, please refer to `Driver Installation Guide <./drivers/installation.html>`_ to configure the default ``DeviceConfig``. Here are example commands:

.. code-block:: bash
      :substitutions:

      # 1. prepare image registry to store driver image (e.g. dockerHub)
      # 2. setup image registry secret: 
      # kubectl create secret docker-registry mySecret -n kube-amd-gpu --docker-username=xxx --docker-password=xxx --docker-server=index.docker.io
      helm install amd-gpu-operator rocm/gpu-operator-charts \
            --namespace kube-amd-gpu --create-namespace \
            --version=|version| \
            --set deviceConfig.spec.driver.enable=true \
            --set deviceConfig.spec.driver.blacklist=true \
            --set deviceConfig.spec.driver.version=6.4 \
            --set deviceConfig.spec.driver.image=docker.io/myUserName/amd-driver-image \
            --set deviceConfig.spec.driver.imageRegistrySecret.name=mySecret

3. Deploy ``DeviceConfig`` separately without using the default one during helm charts installation

You can use the option ``--set crds.defaultCR.install=false`` to disable the deployment of the default ``DeviceConfig`` then deploy it later in a separate step with your desired configuration.


Verify Installation
---------------------------

After running ``helm install`` commands with proper configurations in ``values.yaml``. You should now see the GPU Operator pods starting up in the namespace you specified above, ``kube-amd-gpu``. Here is an example of one control plane node and one GPU worker node:

.. code-block:: bash

  $ kubectl get deviceconfigs -n kube-amd-gpu
  NAME      AGE
  default   10m

  $ kubectl get pods -n kube-amd-gpu
  NAME                                                              READY   STATUS     AGE
  amd-gpu-operator-gpu-operator-charts-controller-manager-74nm5wt   1/1     Running    10m
  amd-gpu-operator-kmm-controller-5c895cd594-h65nm                  1/1     Running    10m
  amd-gpu-operator-kmm-webhook-server-76d6765d5b-g5g74              1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-gc-64c9b7dcd9-gz4g4       1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-master-7d69c9b6f9-hcrxm   1/1     Running    10m
  amd-gpu-operator-node-feature-discovery-worker-jlzbs              1/1     Running    10m
  default-device-plugin-9r9bh                                       1/1     Running    10m
  default-metrics-exporter-6c7z5                                    1/1     Running    10m
  default-node-labeller-xtwbm                                       1/1     Running    10m

* Controller components: ``gpu-operator-charts-controller-manager``, ``kmm-controller`` and ``kmm-webhook-server``

* Operands: ``default-device-plugin``, ``default-node-labeller`` and ``default-metrics-exporter``

Please refer to `TroubleShooting <./troubleshooting.html>`_ if any issue happened during the installation and configuration.

For a full list of ``DeviceConfig`` configurable options refer to the `Full Reference Config <https://instinct.docs.amd.com/projects/gpu-operator/en/latest/fulldeviceconfig.html>`_ documentation. An example DeviceConfig is supplied in the ROCm/gpu-operator repository:     
      .. code-block:: bash
            
            kubectl apply -f https://raw.githubusercontent.com/ROCm/gpu-operator/refs/heads/release-v1.3.1/example/deviceconfig_example.yaml

That's it! The GPU Operator components should now all be running. You can verify this by checking the namespace where the gpu-operator components are installed (default: ``kube-amd-gpu``):

.. code-block:: bash
      
      kubectl get pods -n kube-amd-gpu

Creating a GPU-enabled Pod
--------------------------

To create a pod that uses a GPU, specify the GPU resource in your pod specification:

.. code-block:: yaml

      apiVersion: v1
      kind: Pod
      metadata:
        name: gpu-pod
      spec:
        containers:
          - name: gpu-container
            image: rocm/rocm-terminal:latest
            resources:
              limits:
                amd.com/gpu: 1 # requesting 1 GPU

Save this YAML to a file (e.g., ``gpu-pod.yaml``) and create the pod:

.. code-block:: bash

      kubectl apply -f gpu-pod.yaml

Checking GPU Status
-------------------

To check the status of GPUs in your cluster:

.. code-block:: bash

      kubectl get nodes -o custom-columns=NAME:.metadata.name,GPUs:.status.capacity.'amd\.com/gpu'

Using amd-smi
-------------

To run ``amd-smi`` in a pod:

- Create a YAML file named ``amd-smi.yaml``:

.. code-block:: yaml

      apiVersion: v1
      kind: Pod
      metadata:
        name: amd-smi
      spec:
        containers:
        - image: docker.io/rocm/rocm-terminal:latest
          name: amd-smi
          command: ["/bin/bash"]
          args: ["-c","amd-smi version && amd-smi monitor -ptum"]
          resources:
            limits:
              amd.com/gpu: 1
            requests:
              amd.com/gpu: 1
        restartPolicy: Never

- Create the pod:

.. code-block:: bash

      kubectl create -f amd-smi.yaml

- Check the logs and verify the output ``amd-smi`` reflects the expected ROCm version and GPU presence:

.. code-block:: bash

      kubectl logs amd-smi

      AMDSMI Tool: 24.6.2+2b02a07 | AMDSMI Library version: 24.6.2.0 | ROCm version: 6.2.2
      GPU  POWER  GPU_TEMP  MEM_TEMP  GFX_UTIL  GFX_CLOCK  MEM_UTIL  MEM_CLOCK
        0  126 W     40 °C     32 °C       1 %    182 MHz       0 %    900 MHz

Using rocminfo
--------------

To run ``rocminfo`` in a pod:

- Create a YAML file named ``rocminfo.yaml``:

.. code-block:: yaml

      apiVersion: v1
      kind: Pod
      metadata:
        name: rocminfo
      spec:
        containers:
        - image: docker.io/rocm/rocm-terminal:latest
          name: rocminfo
          command: ["/bin/sh","-c"]
          args: ["rocminfo"]
          securityContext:
            runAsUser: 0
          resources:
            limits:
              amd.com/gpu: 1
        restartPolicy: Never

- Create the pod:

.. code-block:: bash

      kubectl create -f rocminfo.yaml

- Check the logs and verify the output:

.. code-block:: bash

      kubectl logs rocminfo


Configuring GPU Resources
-------------------------

Configuration parameters are documented in the `Custom Resource Installation Guide <./drivers/installation.html>`_
