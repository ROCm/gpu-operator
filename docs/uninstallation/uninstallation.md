# Uninstall

To remove the operator and related resources, you need to follow specific sequence to remove them.

1. `DeviceConfig` custom resources
2. Helm Charts or OLM bundle
3. Custom resource definition

## Uninstall Custom Resource

To delete the `Deviceconfig`, you can use either one of the methods:

* use existing YAML file: ```kubectl delete -f deviceconfigs.yaml```
* query the cluster and delete by resource metadata:
  ```kubectl delete deviceconfigs <your-deviceconfig-name> -n kube-amd-gpu```
* simply remove all deviceconfigs in the namespace:
  ```kubectl delete deviceconfigs --all -n kube-amd-gpu```

Once the deletion was triggered, if out-of-tree driver was previously installed by AMD GPU operator, it will trigger KMM to send worker pods to selected nodes and start to unload the `amdgpu` kernel module.

The delete request won't finish immediately, instead it will wait for the unload confirmation from all selected worker nodes, then finish the deletion of the `DeviceConfig` resource.

If delete request gets stuck for too long, you may need to check the status of KMM worker pods, if any error happened please check the worker pod error logs:

```kubectl logs kmm-worker -n kube-amd-gpu```

or refer to [Troubleshooting](../troubleshooting) document to find the solution.

## Uninstall Helm Charts

```bash
helm uninstall amd-gpu-operator -n kube-amd-gpu
```

By default the helm uninstall command will call a pre-delete hook to check if there is any `DeviceConfig` custom resources existing in the cluster. If you forget to remove all the `DeviceConfig` custom resources, the pre-delete hook will stop the helm uninstall process. In that situation, please delete all existing `DeviceConfig`.

```{note}
The pre-delete hook is using the operator controller image to run kubectl for checking existing `DeviceConfig`, if you want to skip the pre-delete hook, you can run helm uninstall command with ```--no-hooks``` option, in that way the Helm Charts will be immediately uninstalled but may have risk that some `DeviceConfig` resources still remain in the cluster.
```

## Uninstall Custom Resource Definition

By default Helm Charts won't uninstall the CRDs for users, so you may need to manually clean up CRDs after uninstalling the Helm Charts. To list all existing CRDs, run this command:

```bash
$ kubectl get crds
NAME                                     CREATED AT
certificaterequests.cert-manager.io      2024-08-20T00:29:12Z
certificates.cert-manager.io             2024-08-20T00:29:12Z
challenges.acme.cert-manager.io          2024-08-20T00:29:12Z
clusterissuers.cert-manager.io           2024-08-20T00:29:12Z
deviceconfigs.amd.com                    2024-10-29T19:39:37Z
issuers.cert-manager.io                  2024-08-20T00:29:12Z
modules.kmm.sigs.x-k8s.io                2024-10-29T19:39:37Z
nodefeaturegroups.nfd.k8s-sigs.io        2024-10-29T19:39:37Z
nodefeaturerules.nfd.k8s-sigs.io         2024-10-29T19:39:37Z
nodefeatures.nfd.k8s-sigs.io             2024-10-29T19:39:37Z
nodemodulesconfigs.kmm.sigs.x-k8s.io     2024-10-29T19:39:37Z
orders.acme.cert-manager.io              2024-08-20T00:29:12Z
preflightvalidations.kmm.sigs.x-k8s.io   2024-10-29T22:53:39Z
```

then use kubectl to delete CRDs that need to be deleted:

```bash
kubectl delete crds deviceconfigs.amd.com
```

```{warning}
Carefully evaluate the impact of removing all CRDs. If the CRDs of cert-manager, NFD or KMM are being used by operators other than AMD GPU operator, deleting those CRDs may affect other operators.
```
