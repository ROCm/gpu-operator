# Developer Guide

This guide provides information for developers who want to contribute to or modify the AMD GPU Operator.

```{warning}
This project is not ready yet to accept the external developers commits.
```

## Prerequisites

- Docker
- Kubernetes cluster (v1.29.0+) or OpenShift (4.16+)
- `kubectl` or `oc` CLI tool configured to access your cluster

## Development Environment Setup

- Install Helm:

```bash
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 get_helm.sh
./get_helm.sh
```

For alternative installation methods, refer to the [Helm Official Website](https://helm.sh/docs/intro/install/).

- Clone the repository:

```bash
git clone https://github.com/ROCm/gpu-operator.git
cd gpu-operator
```

- (Optional) Set up a local Docker registry. If you want to build and host container images locally, you can set up a local Docker registry:

```bash
docker run -d -p 5000:5000 --name registry registry:latest
```

- Modify the registry-related variables in the `Makefile`:
  - `DOCKER_REGISTRY`: Set to `localhost:5000` for local development, or your preferred registry
  - `IMAGE_NAME`: Set to `rocm/gpu-operator`
  - `IMAGE_TAG`: Set as needed (e.g., `v1.0.0` or `latest`)

- Compile the project:

 ```bash
 make
 ```

This will generate the basic YAML files for CRD, build controller images, build Helm charts and build OpenShift OLM bundle.

- (Optional) Run specific make target:
  - Run `make docker/shell` to build and attach to a container with build environment configured
  - Run `make <specific target>` within the container to execute specific make target.

- Build and push the AMD GPU Operator image:

```bash
make docker-build
make docker-push
```

> Note: If you're using a remote registry that requires authentication, ensure you've logged in using `docker login` before pushing.

- Generate Helm charts:
  - For vanilla Kubernetes: `make helm`
  - For OpenShift: `OPENSHIFT=1 make helm`

- Check `Makefile` help message for more options:

```bash
make help
```

## Running Tests

Running e2e requires a Kubernetes cluster, please prepare your Kubernetes cluster ready for running the e2e tests, as well as configure the kubeconfig file at ```~/.kube/config``` for kubectl and helm toolkits to get access to your cluster. The e2e test cases will deploy the Operator to your cluster and run the test cases.

To run the e2e tests:

```bash
make e2e
```

To run e2e tests with a specific Helm chart:

```bash
make e2e GPU_OPERATOR_CHART="path to helm chart"
```

To run e2e test only:

```bash
make -C tests/e2e # run e2e tests only
```

## Creating a Pull Request

1. Fork the repository on GitHub.
2. Create a new branch for your changes.
3. Make your changes and commit them with clear, descriptive commit messages.
4. Push your changes to your fork.
5. Create a pull request against the main repository.

Please ensure your code follows our coding standards and includes appropriate tests.

## Build Documentation Website Locally

- Download mkdocs utilities

```bash
python3 -m pip install mkdocs
```

- Build the website

```bash
cd docs
python3 -m mkdocs build
```

- Deploy the website

```bash
python3 -m mkdocs serve --dev-addr localhost:2345
```

- The local docs website will dynmically update as changes are made to markdown docs.
