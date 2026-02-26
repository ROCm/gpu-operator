GPU Partitioning via DCM
========================

**OpenShift / Red Hat OCP:** On OpenShift, the GPU Operator is typically deployed in the ``openshift-amd-gpu`` namespace (not ``kube-amd-gpu``). Use ``openshift-amd-gpu`` in the examples below when you are on OpenShift. Essential OpenShift cluster components (DNS, ingress, network, etc.) may run on GPU nodes; applying a ``NoExecute`` taint will evict any pod that does not tolerate it. Ensure you add the ``amd-dcm`` toleration to required workloads in ``kube-system`` and, on OpenShift, in relevant ``openshift-*`` namespaces (e.g. ``openshift-dns``, ``openshift-ingress``) that schedule onto GPU nodes before tainting.

- GPU on the node cannot be partitioned on the go, we need to bring down all daemonsets using the GPU resource before partitioning. Hence we need to taint the node and the partition.
- DCM pod comes with a toleration
    - `key: amd-dcm , value: up , Operator: Equal, effect: NoExecute `
    - User can specify additional tolerations if required
- Avoid adding the `amd-dcm` toleration to the operands (`device plugin`, `node labeller`, `metrics exporter`, and `test runner`) daemonsets via the `DeviceConfig` spec. 
    - This ensures operands restart automatically after partitioning completes, allowing them to detect updated GPU resources.
    - If operands do not restart automatically, manually restart them after partitioning is complete.

GPU Partitioning Workflow
-------------------------

1. Add tolerations to the required system pods to prevent them from being evicted during partitioning process
2. Deploy the DCM pod by applying/updating the DeviceConfig
3. Taint the node to evict all workloads and prevent scheduling on new workloads on the node
4. Label the node to indicate what paritioning profile will be used
5. DCM will partition the node accordingly
6. Once partition is done, un-taint the node to add it back so workloads can be scheduled on the cluster

Setting GPU Partitioning
-------------------------

1. Add tolerations to all deployments and daemonsets in kube-system namespace
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Since tainting a node will bring down all pods/daemonsets, we need to add toleration to the Kubernetes system pods to prevent them from getting evicted. Pods in the system namespace are responsible for things like DNS, networking, proxy and the overall proper functioning of your node.

.. note::
   **OpenShift:** On OpenShift, essential cluster DaemonSets and Deployments (e.g. in ``openshift-dns``, ``openshift-ingress``, ``openshift-network-operator``) may run on GPU nodes. Adding a ``NoExecute`` taint without first adding the ``amd-dcm`` toleration to those workloads can evict critical components. After patching ``kube-system`` as below, add the same toleration to any ``openshift-*`` namespaces that have workloads on your GPU nodes. You can list pods on the node and add tolerations to their controllers as needed.

Here we are patching all the deployments in the `kube-system` namespace with the key `amd-dcm` which is used during the tainting process to evict all non-essential pods:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

         kubectl get deployments -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch deployment {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'

   .. tab-item:: OpenShift

      .. code-block:: bash

         oc get deployments -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} oc patch deployment {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'

We also need to patch all the daemonsets in the `kube-system` namespace to prevent CNI (e.g., Cilium) malfunction:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

         kubectl get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch daemonsets {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'

   .. tab-item:: OpenShift

      .. code-block:: bash

         oc get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} oc patch daemonset {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'


The above command is convenient as it adds the required tolerations all with a single command. However, you can also manually edit any required deployments or pods yourself and add this toleration to any other required pods in your cluster as follows:

.. code-block:: yaml

    #Add this under the spec.template.spec.tolerations object
    tolerations:
        - key: "amd-dcm"
            operator: "Equal"
            value: "up"
            effect: "NoExecute"


2. Create DCM Profile ConfigMap
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Next you will need to create the Device Config Mangaer ConfigMap that specifies the different partitioning profiles you would like to set. Refer to the [Device Config Mangaer ConfigMap](../dcm/device-config-manager-configmap.html#configmap) page for more details on how to create the DCM ConfigMap.

Before creating your partition profiles, ensure you use the correct compute and memory partition combinations for your GPU model. For detailed information on supported partition profiles by GPU model, refer to the `AMD GPU Partitioning documentation <https://instinct.docs.amd.com/projects/amdgpu-docs/en/latest/gpu-partitioning/index.html>`_.

**Checking Supported Partitions on Your System**

You can verify the supported compute and memory partition modes directly on your GPU node by checking the sysfs files. SSH into your node and run the following commands:

.. code-block:: bash

   # Check available compute partitions (e.g., SPX, DPX, QPX, CPX)
   cat /sys/module/amdgpu/drivers/pci\:amdgpu/<bdf>/available_compute_partition

   # Check available memory partitions (e.g., NPS1, NPS2, NPS4, NPS8)
   cat /sys/module/amdgpu/drivers/pci\:amdgpu/<bdf>/available_memory_partition

Replace ``<bdf>`` with your GPU's PCI bus/device/function identifier (e.g., ``0000:87:00.0``). You can find the available BDFs by listing the directory contents:

.. code-block:: bash

   ls /sys/module/amdgpu/drivers/pci\:amdgpu/

Example output:

.. code-block:: bash

   $ cat /sys/module/amdgpu/drivers/pci\:amdgpu/0000\:87\:00.0/available_compute_partition
   SPX, DPX, QPX, CPX
   
   $ cat /sys/module/amdgpu/drivers/pci\:amdgpu/0000\:87\:00.0/available_memory_partition
   NPS1, NPS4, NPS8

Below is an example of how to create the `config-manager-config.yaml` file that has the following 2 profiles:

- **cpx-profile**: CPX+NPS4 (64 GPU partitions)
- **spx-profile**: SPX+NPS1 (no GPU partitions)

Use namespace ``kube-amd-gpu`` for Kubernetes; on OpenShift use ``openshift-amd-gpu``.

.. code-block:: yaml
    
    apiVersion: v1
    kind: ConfigMap
    metadata:
        name: config-manager-config
        namespace: kube-amd-gpu
    data:
        config.json: |
        {
            "gpu-config-profiles":
            {
                "cpx-profile":
                {
                    "skippedGPUs": {
                        "ids": []
                    },
                    "profiles": [
                        {
                            "computePartition": "CPX",
                            "memoryPartition": "NPS4",
                            "numGPUsAssigned": 8
                        }
                    ]
                },
                "spx-profile":
                {
                    "skippedGPUs": {
                        "ids": []
                    },
                    "profiles": [
                        {
                            "computePartition": "SPX",
                            "memoryPartition": "NPS1",
                            "numGPUsAssigned": 8
                        }
                    ]
                }
            },
            "gpuClientSystemdServices": {
                "names": ["amd-metrics-exporter", "gpuagent"]
            }
        }


Now apply the DCM ConfigMap to your cluster

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl apply -f config-manager-config.yaml

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc apply -f config-manager-config.yaml

After creating the ConfigMap, you need to associate it with the Device Config Manager by updating the DeviceConfig Custom Resource (CR)

.. code-block:: yaml

    configManager:
      # To enable/disable the config manager, enable to partition
      enable: True

      # image for the device-config-manager container
      image: "rocm/device-config-manager:v1.4.0"

      # image pull policy for config manager. Accepted values are Always, IfNotPresent, Never
      imagePullPolicy: IfNotPresent

      # specify configmap name which stores profile config info
      config: 
        name: "config-manager-config"

      # OPTIONAL
      # toleration field for dcm pod to bypass nodes with specific taints
      configManagerTolerations:
        - key: "key1"
          operator: "Equal" 
          value: "value1"
          effect: "NoExecute"

.. note::
   The ConfigMap name is of type ``string``. Ensure you change the ``spec/configManager/config/name`` to match the name of the config map you created (in this example, ``config-manager-config``). The Device-Config-Manager pod needs a ConfigMap to be present or else the pod does not come up.

3. Add Taint to node
~~~~~~~~~~~~~~~~~~~~

In order to ensure there are no workloads on the node that are using the GPUs we taint the node to evict any non-essential workloads. To do this taint the node with the `amd-dcm=up:NoExecute` toleration. This ensures that only workloads and daemonsets with this specific tolerations will remain on the node. All others will terminate. This can be done as follows:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl taint nodes [nodename] amd-dcm=up:NoExecute

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc taint nodes [nodename] amd-dcm=up:NoExecute

4. Label the node with the CPX profile
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Monitor the pods on the node to ensure that all non-essential workloads are being terminated. Wait for a short amount of time to ensure the pods have terminated. Once done we need to label the node with the parition profile we want DCM to apply. In this case we will apply the `cpx-profile` label as follows ensure we also pass the --overwrite flag to account for any existing `gpu-config-profile` label:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile=cpx-profile --overwrite

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc label node [nodename] dcm.amd.com/gpu-config-profile=cpx-profile --overwrite

You can also confirm that the label got applied by checking the node:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl describe node [nodename] | grep gpu-config-profile

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc describe node [nodename] | grep gpu-config-profile

5. Verify GPU partitioning
~~~~~~~~~~~~~~~~~~~~~~~~~~

Use kubectl exec to run amd-smi inside the Device Config Manager pod to confirm you now see the new partitions:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl exec -n kube-amd-gpu -it [dcm-pod-name] -- amd-smi list

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc exec -n openshift-amd-gpu -it [dcm-pod-name] -- amd-smi list

Replace ``[dcm-pod-name]`` with the actual name of your Device Config Manager pod (e.g., ``gpu-operator-device-config-manager-hn9rb``). On Kubernetes the DCM pod runs in ``kube-amd-gpu``; on OpenShift it runs in ``openshift-amd-gpu`` (use ``-n openshift-amd-gpu`` in the OpenShift tab above).

6. Remove Taint from the node
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Remove the taint from the node to restart all previous workloads and allow the node to be used again for scheduling workloads:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl taint nodes [nodename] amd-dcm=up:NoExecute-

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc taint nodes [nodename] amd-dcm=up:NoExecute-

Reverting back to SPX (no partitions)
-------------------------------------

To revert a node back to SPX mode (no partitions), apply the ``spx-profile`` label to the node:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile=spx-profile --overwrite

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc label node [nodename] dcm.amd.com/gpu-config-profile=spx-profile --overwrite

Removing Partition Profile label
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

To completely remove the partition profile label from a node:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile-

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc label node [nodename] dcm.amd.com/gpu-config-profile-

Removing DCM tolerations from all daemonsets in kube-system namespace
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

After completing partitioning operations, you can remove the DCM tolerations that were added to the kube-system namespace:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch daemonset {} -n kube-system --type='json' -p='[{"op": "remove", "path": "/spec/template/spec/tolerations/0"}]'

   .. tab-item:: OpenShift

      .. code-block:: bash

            oc get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} oc patch daemonset {} -n kube-system --type='json' -p='[{"op": "remove", "path": "/spec/template/spec/tolerations/0"}]'
