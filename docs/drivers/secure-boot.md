# Secure Boot Configuration Guide

This guide explains how to configure the AMD GPU Operator for systems with Secure Boot enabled.

## Overview

Secure Boot is a security feature that helps protect a system against malicious code being loaded during the boot process. When enabled, it requires kernel modules to be signed with a valid key pair and the public key must be registered in the Machine Owner Key (MOK) database.

## Prerequisites

Before proceeding, ensure you have:

- A Kubernetes cluster with worker nodes that have Secure Boot enabled
- Administrative access to your cluster
- Understanding of basic cryptographic concepts
- Access to the worker nodes' MOK database

## Configuration Methods

There are two approaches to handling Secure Boot requirements:

### Method 1: Pre-signed Driver Images

Users prepare and sign their own driver images before deployment.

- Create signed kernel modules following your OS vendor's guidelines:
  - [Ubuntu Signing Guide](https://ubuntu.com/blog/how-to-sign-things-for-secure-boot)
- Package the signed modules into a container image
- Configure the operator to use your pre-signed image:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: amdgpu-config
  namespace: kube-amd-gpu
spec:
  driver:
    image: registry.example.com/signed-amdgpu:v1.2.3
```

### Method 2: Operator-managed Signing

Let the AMD GPU Operator handle the signing process.

- Generate signing keys:

```bash
openssl req -x509 -new -nodes -utf8 -sha256 -days 36500 -batch \
  -outform DER -out my_signing_key_pub.der \
  -keyout my_signing_key.priv
```

- Encode the keys with base64 encoding:

```bash
cat my_signing_key.priv | base64 -w 0 > my_signing_key.base64
cat my_signing_key_pub.der | base64 -w 0 > my_signing_key_pub.base64
```

- Create Kubernetes secrets:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key-pub
  namespace: kube-amd-gpu
type: Opaque
data:
  cert: <base64 encoded public key>
---
apiVersion: v1
kind: Secret
metadata:
  name: my-signing-key
  namespace: kube-amd-gpu
type: Opaque
data:
  key: <base64 encoded private key>
```

1. Configure DeviceConfig to use the signing keys:

```yaml
apiVersion: amd.com/v1alpha1
kind: DeviceConfig
metadata:
  name: amdgpu-config
  namespace: kube-amd-gpu
spec:
  driver:
    imageSign:
      keySecret:
        name: my-signing-key
      certSecret:
        name: my-signing-key-pub
```

## Troubleshooting

- Module Loading Failures

If you see errors like:

```bash
modprobe: ERROR: could not insert 'amdgpu': Required key not available
```

or

```bash
modprobe: ERROR: could not insert 'amdgpu': Operation not permitted
```

Check:

- Module signing status
- Public key registration in MOK
- Secure Boot status on the node

### Verification Steps

- Check Secure Boot status:

```bash
mokutil --sb-state
```

- Verify MOK enrollment:

```bash
mokutil --list-enrolled
```

- Check module signature:

```bash
modinfo -F signer amdgpu
```

## Best Practices

1. Key Management
   - Store signing keys securely
   - Use different keys for different environments
   - Implement key rotation procedures

2. Testing
   - Validate signed modules in a test environment
   - Verify module loading on all kernel versions
   - Test driver functionality after signing

3. Documentation
   - Document key generation process
   - Maintain signing procedure documentation
   - Record MOK enrollment steps

## Additional Resources

- [Linux Kernel Module Signing Documentation](https://www.kernel.org/doc/html/latest/admin-guide/module-signing.html)
- [UEFI Secure Boot Guide](https://wiki.debian.org/SecureBoot)
- [AMD GPU Driver Documentation](https://rocm.docs.amd.com/)
