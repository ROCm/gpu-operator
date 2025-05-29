GPU Partitioning via DCM
========================

-  GPU on the node cannot be partitioned on the go, we need to bring down all daemonsets using the GPU resource before partitioning. Hence we need to taint the node and the partition.
- DCM pod comes with a toleration
    - `key: amd-dcm , value: up , Operator: Equal, effect: NoExecute `
    - User can specify additional tolerations if required

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

1. Add tolerations to all deployments in kube-system namespace
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Since tainting a node will bring down all pods/daemonsets, we need to add toleration to the Kubernetes system pods to prevent them from getting evicted. Pods in the system namespace are responsible for things like DNS, networking, proxy and the overall proper functioning of your node.

Here we are patching all the deployments in the `kube-system` namespace with the key `amd-dcm` which is used during the tainting process to evict all non-essential pods:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

         kubectl get deployments -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch deployment {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..          oc get deployments -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch deployment {} -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/tolerations", "value": [{"key": "amd-dcm", "operator": "Equal", "value": "up", "effect": "NoExecute"}]}]'
             

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

Below is an example of how to create the `config-manager-config.yaml` file that has the following 2 profiles:

- **cpx-profile**: CPX+NPS4 (64 GPU partitions)
- **spx-profile**: SPX+NPS1 (no GPU partitions)

.. code-block:: yaml
    
    cat <<EOF > config-manager-config.yaml
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
        }
        }
    EOF


Now apply the DCM ConfigMap to your cluster

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl apply -f config-manager-config.yaml

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc apply -f config-manager-config.yaml

3. Add Taint to node
~~~~~~~~~~~~~~~~~~~~

In order to ensure there are no workloads on the node that are using the GPUs we taint the node to evict any non-essential workloads. To do this taint the node with the `amd-dcm=up:NoExecute` toleration. This ensures that only workloads and daemonsets with this specific tolerations will remain on the node. All others will terminate. This can be done as follows:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl taint nodes [nodename] amd-dcm=up:NoExecute

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc taint nodes [nodename] amd-dcm=up:NoExecute

4. Label the node with the CPX profile
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Monitor the pods on the node to ensure that all non-essential workloads are being terminated. Wait for a short amount of time to ensure the pods have terminated. Once done we need to label the node with the parition profile we want DCM to apply. In this case we will apply the `cpx-profile` label as follows ensure we also pass the --overwrite flag to account for any existing `gpu-config-profile` label:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile=cpx-profile --overwrite

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc label node [nodename] dcm.amd.com/gpu-config-profile=cpx-profile --overwrite

You can also confirm that the label got applied by checking the node:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl describe node [nodename] | grep gpu-config-profile

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc describe node [nodename] | grep gpu-config-profile

5. Verify GPU partitioning
~~~~~~~~~~~~~~~~~~~~~~~~~~

Connect to the node in your cluster via SSH and run amd-smi to confirm you now see the new partitions:

.. code-block:: bash

    amd-smi list

6. Remove Taint from the node
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Remove the taint from the node to restart all previous workloads and allow the node to be used again for scheduling workloads:

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl taint nodes [nodename] amd-dcm=up:NoExecute-

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc taint nodes [nodename] amd-dcm=up:NoExecute-

Reverting back to SPX (no partitions)
-------------------------------------

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile=spx-profile --overwrite

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc label node [nodename] dcm.amd.com/gpu-config-profile=spx-profile --overwrite

Removing Partition Profile label
--------------------------------

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl label node [nodename] dcm.amd.com/gpu-config-profile-

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc label node [nodename] dcm.amd.com/gpu-config-profile-

Removing DCM tolerations from all daemonsets in kube-system namespace
---------------------------------------------------------------------

.. tab-set::

   .. tab-item:: Kubernetes

      .. code-block:: bash

            kubectl get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch daemonset {} -n kube-system --type='json' -p='[{"op": "remove", "path": "/spec/template/spec/tolerations/0"}]'

..    .. tab-item:: OpenShift

..       .. code-block:: bash

..             oc get daemonsets -n kube-system -o json | jq -r '.items[] | .metadata.name' | xargs -I {} kubectl patch daemonset {} -n kube-system --type='json' -p='[{"op": "remove", "path": "/spec/template/spec/tolerations/0"}]'
