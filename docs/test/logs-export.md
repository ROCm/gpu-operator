# Exporting Test Runner Logs to External Storage

The Test Runner component of the AMD GPU Operator now supports exporting logs to external storage solutions, including AWS S3, Azure Blob Storage, and MinIO.

> Note: Support for additional object storage providers is planned for future releases.

## Overview

The Test runner logs export to external storage buckets feature facilitates centralized logging for audit trails, troubleshooting, and long-term log retention. Exported logs are automatically organized hierarchically into subdirectories based on trigger type, job name, node name, and timestamp, enhancing searchability and traceability.
By default, this feature is disabled and requires explicit configuration to activate.

Following section describes the steps to enable the functionality.

## Configuration Guide

### 1. Create Kubernetes Secrets for External Storage Access

Credentials required to access the external storage must be provided as a Kubernetes [Secret](https://kubernetes.io/docs/concepts/configuration/secret). This captured connectivity information as Kubernetes Secret must be within the same namespace as the AMD GPU Operator/Test Runner. This Secret should be mounted as a volume in the Test Runner container. The required information/keys captured vary depending on the storage provider.

> Note: The values in the secret yaml file must be base64 encoded.

Alternatively secrets can be created using kubectl CLI command without base64 encoding:

`kubectl create secret generic aws-secret --from-literal=aws_access_key_id='your access key id' --from-literal=aws_secret_access_key='your secret access key' --from-literal=aws_region='aws region'`

### a. AWS S3 Secret

For AWS S3, the secret captures user [access key](https://aws.amazon.com/blogs/security/wheres-my-secret-access-key) information and AWS region of bucket.
The secret should include the following keys:​
- `aws_access_key_id`: Your AWS access key ID​
- `aws_secret_access_key`: Your AWS secret access key​
- `aws_region`: The AWS region where your S3 bucket resides

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-secret
  namespace: default
type: Opaque
data:
  aws_access_key_id: <your-access-key-id>
  aws_region: <sample-aws-region>
  aws_secret_access_key: <your-secret-key>
```

### b. Azure Blob Storage

For Azure Blob Storage, the secret captures storage account name and key info.
The secret should include the following keys:​

- `azure_storage_account` - Your Azure storage account name
- `azure_storage_key` - Your Azure storage account key

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: azure-secret
  namespace: default
type: Opaque
data:
  azure_storage_account: <sample_azure_storage_account>
  azure_storage_key: <sample_azure_storage_key>
```

### c. MinIO

Minio supports S3 compatible APIs for object storage. So for Minio, we can create AWS secret with extra field to capture Minio S3 endpoint URL.
The secret should include the following keys:​

- `aws_access_key_id` - Your MinIO access key
- `aws_secret_access_key` - Your MinIO secret key
- `aws_region` - In MinIO, `us-east-1` can be used as default aws region
- `aws_endpoint_url` - Your MinIO server's S3-compatible endpoint URL

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: minio-secret
  namespace: default
type: Opaque
data:
  aws_access_key_id: <your-minio-access-id>
  aws_region: us-east-1
  aws_secret_access_key: <your-minio-secret-key>
  aws_endpoint_url: <your-minio-s3-endpoint>
```

### 2. Update the Test Runner ConfigMap

Define the storage provider and bucket information in the Test Runner's ConfigMap. This configuration specifies where and how logs should be exported.

Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-runner-config
  namespace: kube-amd-gpu
data:
  config.json: |
    {
      "TestConfig": {
        "GPU_HEALTH_CHECK": {
          "TestLocationTrigger": {
            "global": {
              "TestParameters": {
                "AUTO_UNHEALTHY_GPU_WATCH": {
                  "TestCases": [
                    {
                      "Recipe": "gst_single",
                      "Iterations": 1,
                      "StopOnFailure": true,
                      "TimeoutSeconds": 600
                    }
                  ],
                  "LogsExportConfig": [
                    {
                      "Provider": "aws",
                      "BucketName": "aws-bucket-name",
                      "SecretName": "aws-secret"
                    },
                    {
                      "Provider": "azure",
                      "BucketName": "azure-bucket-name",
                      "SecretName": "azure-secret"
                    },
                    {
                      "Provider": "aws",
                      "BucketName": "minio-bucket-name",
                      "SecretName": "minio-secret"
                    }
                  ]
                }
              }
            }
          }
        }
      }
    }
```

> Note: Replace `aws-bucket-name`, `azure-bucket-name`, and `minio-bucket-name` with your actual bucket names.

### 3. Configure the DeviceConfig Custom Resource (CR)

In scenarios like the Auto Unhealthy GPU Watch, specify the secrets in the testRunner section of the DeviceConfig CR. These secrets are then mounted into the container for runtime use. This ensures that the Test Runner has access to the necessary credentials for log export.
We can export logs to multiple external services. We can specify multiple secrets in device config Custom Resource(CR) and associate each to a particular external storage service.

Example:

```yaml
  # Specify the testrunner config
  testRunner:
    # To enable/disable the testrunner, disabled by default
    enable: True

    # testrunner image
    image: docker.io/rocm/test-runner:v1.4.0

    # image pull policy for the testrunner
    # default value is IfNotPresent for valid tags, Always for no tag or "latest" tag
    imagePullPolicy: "Always"

    # test runner config map
    config:
      # example config map can be found under examples/testrunner/configmap.json
      name: sample-configmap

    # specify the mount for test logs
    logsLocation:
      # mount path inside test runner container
      mountPath: "/var/log/amd-test-runner"

      # host path to be mounted into test runner container
      hostPath: "/var/log/amd-test-runner"

      # list of secrets that contain connectivity info to cloud providers
      logsExportSecrets:
      # the secrets mentioned below are associated with the test runner via config map. Refer examples/testrunner/configmap.json
      - name: azure-secret
      - name: aws-secret
```

```{note}
Note: Ensure that the `logsExportSecrets` list includes all the secrets corresponding to the external storage services you intend to use.
```

### Additional Notes

- For manual, pre-start and cron jobs, the secret information should be mounted explicitly in their respective job yamls.
- Refer to [examples](https://github.com/ROCm/gpu-operator/tree/main/example/testrunner) folder for sample job yamls and config map.
- Ensure secrets and config maps are created in the appropriate namespace.
