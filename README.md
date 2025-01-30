# AMD GPU Operator

AMD GPU Opertaor Helm Chart Repository.

## Quick Start
```bash
# Install Helm
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 get_helm.sh
./get_helm.sh

# Install Cert Manager
helm repo add jetstack https://charts.jetstack.io --force-update

helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.15.1 \
  --set crds.enabled=true

# Install AMD GPU Operator
helm repo add rocm https://rocm.github.io/gpu-operator
helm repo update
helm install amd-gpu-operator rocm/gpu-operator-charts --namespace kube-amd-gpu --create-namespace
```

## Install Custom Resource
Please refer to [official documentation](https://instinct.docs.amd.com/projects/gpu-operator/en/latest/installation/kubernetes-helm.html#install-custom-resource) for more details about installing a custom resource.
