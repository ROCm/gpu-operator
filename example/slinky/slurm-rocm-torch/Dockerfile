# syntax=docker/dockerfile:1
# SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
# SPDX-License-Identifier: Apache-2.0
################################################################################

ARG SLURM_VERSION=24.05.7

################################################################################
ARG PARENT_IMAGE=rocm/pytorch-training:v25.4
FROM ${PARENT_IMAGE} AS build

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

ARG SLURM_VERSION
ENV SLURM_VERSION=${SLURM_VERSION}

USER root
WORKDIR /tmp/

#COPY patches/ patches/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#debuild
RUN <<EOR
# Download and Build Slurm
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --no-install-recommends \
  build-essential fakeroot devscripts equivs curl
## Download
SLURM_DIR="slurm-${SLURM_VERSION}"
mkdir -p $SLURM_DIR
if [ $(curl -s -Iw '%{http_code}' -o /dev/null https://download.schedmd.com/slurm/${SLURM_DIR}.tar.bz2) -eq 200 ]; then
  curl -s -O https://download.schedmd.com/slurm/${SLURM_DIR}.tar.bz2
  tar --strip-components=1 -jxvf ${SLURM_DIR}.tar.bz2 -C $SLURM_DIR
else
  curl -s -L -H "Accept: application/vnd.github+json" -O https://github.com/SchedMD/slurm/archive/${SLURM_VERSION}.tar.gz
  tar --strip-components=1 -zxvf ${SLURM_VERSION}.tar.gz -C $SLURM_DIR
fi
## Patch
find $(pwd)/patches/ -type f -name "*.patch" -print0 | sort -z | xargs -t0r -n1 patch -p1 -d $SLURM_DIR -i
## Build
mk-build-deps -ir --tool='apt-get -qq -y -o Debug::pkgProblemResolver=yes --no-install-recommends' ${SLURM_DIR}/debian/control
( cd $SLURM_DIR && debuild -b -uc -us >/dev/null )
## Cleanup
apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

################################################################################
FROM ${PARENT_IMAGE} AS base

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

ARG SLURM_VERSION
ENV SLURM_VERSION=${SLURM_VERSION}

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.authors="slinky@schedmd.com" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/documentation.html" \
  org.opencontainers.image.license="GPL-2.0-or-later WITH openssl-exception" \
  org.opencontainers.image.vendor="SchedMD LLC." \
  org.opencontainers.image.version="${SLURM_VERSION}" \
  org.opencontainers.image.source="https://github.com/SlinkyProject/containers"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  vendor="SchedMD LLC." \
  version="${SLURM_VERSION}" \
  release="https://github.com/SlinkyProject/containers"

USER root
WORKDIR /tmp/

ARG SLURM_USER=slurm
ARG SLURM_USER_UID=401
ARG SLURM_USER_GID=401

RUN <<EOR
# Create SlurmUser
set -xeuo pipefail
groupadd --system --gid=${SLURM_USER_GID} ${SLURM_USER}
useradd --system --no-log-init --uid=${SLURM_USER_UID} --gid=${SLURM_USER_GID} --shell=/usr/sbin/nologin ${SLURM_USER}
EOR

#COPY --from=build /tmp/*.deb /tmp/

RUN <<EOR
# Install Dependencies
set -xeuo pipefail
apt-get -qq update
# For: Helm Chart
apt-get -qq -y install --no-install-recommends \
  rsync gettext-base iputils-ping
# Init System
apt-get -qq -y install --no-install-recommends tini
apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

ENV KUBECTL_VERSION=1.32

# Ref: https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/#install-using-native-package-management
RUN <<EOR
# Install kubectl
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --no-install-recommends \
  apt-transport-https ca-certificates curl gnupg
mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://pkgs.k8s.io/core:/stable:/v${KUBECTL_VERSION}/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
chmod 644 /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${KUBECTL_VERSION}/deb/ /" | tee /etc/apt/sources.list.d/kubernetes.list
chmod 644 /etc/apt/sources.list.d/kubernetes.list
apt-get -qq update
apt-get -qq -y install --no-install-recommends kubectl
apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

## START DEBUG SECTION ###
ARG DEBUG=0
RUN <<EOR
# For: Development and Debugging
set -xeuo pipefail
if [ "$DEBUG" = true ] || [ "$DEBUG" = 1 ]; then
  apt-get -qq update
  apt-get -qq -y install --no-install-recommends sudo
  echo "${SLURM_USER} ALL=(ALL) NOPASSWD: ALL" >/etc/sudoers.d/slurm
  apt-get clean && rm -rf /var/lib/apt/lists/*
fi
EOR
### END DEBUG SECTION ###

################################################################################
# SLURM: slurmctld
# BUILD: `docker build --target=slurmctld -t [<registry>/]slurmctld:<tag> .`
################################################################################
FROM base AS slurmctld

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.title="Slurm Control Plane" \
  org.opencontainers.image.description="slurmctld - The central management daemon of Slurm" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/slurmctld.html"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  name="Slurm Control Plane" \
  summary="slurmctld - The central management daemon of Slurm" \
  description="slurmctld - The central management daemon of Slurm"

USER root
WORKDIR /tmp/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#pkg_install
RUN <<EOR
# Install Slurm Packages
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --fix-broken \
  ./slurm-smd-client_[0-9]*.deb \
  ./slurm-smd-dev_[0-9]*.deb \
  ./slurm-smd-doc_[0-9]*.deb \
  ./slurm-smd-slurmctld_[0-9]*.deb \
  ./slurm-smd_[0-9]*.deb
rm *.deb && apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

ENTRYPOINT ["tini", "-g", "--", "slurmctld", "-D"]

################################################################################
# SLURM: slurmd
# BUILD: `docker build --target=slurmd -t [<registry>/]slurmd:<tag> .`
################################################################################
FROM base AS slurmd

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.title="Slurm Worker Agent" \
  org.opencontainers.image.description="slurmd - The compute node daemon for Slurm" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/slurmd.html"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  name="Slurm Worker Agent" \
  summary="slurmd - The compute node daemon for Slurm" \
  description="slurmd - The compute node daemon for Slurm"

USER root
WORKDIR /tmp/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#pkg_install
RUN <<EOR
# Install Slurm Packages
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --fix-broken \
  ./slurm-smd-client_[0-9]*.deb \
  ./slurm-smd-dev_[0-9]*.deb \
  ./slurm-smd-doc_[0-9]*.deb \
  ./slurm-smd-libnss-slurm_[0-9]*.deb \
  ./slurm-smd-libpam-slurm-adopt_[0-9]*.deb \
  ./slurm-smd-libpmi2-0_[0-9]*.deb \
  ./slurm-smd-slurmd_[0-9]*.deb \
  ./slurm-smd_[0-9]*.deb
rm *.deb && apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

ENTRYPOINT ["tini", "-g", "--", "slurmd", "-D"]

################################################################################
# SLURM: slurmdbd
# BUILD: `docker build --target=slurmdbd -t [<registry>/]slurmdbd:<tag> .`
################################################################################
FROM base AS slurmdbd

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.title="Slurm Database Agent" \
  org.opencontainers.image.description="slurmdbd - Slurm Database Daemon" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/slurmdbd.html"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  name="Slurm Database Agent" \
  summary="slurmdbd - Slurm Database Daemon" \
  description="slurmdbd - Slurm Database Daemon"

USER root
WORKDIR /tmp/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#pkg_install
RUN <<EOR
# Install Slurm Packages
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --fix-broken \
  ./slurm-smd-doc_[0-9]*.deb \
  ./slurm-smd-slurmdbd_[0-9]*.deb \
  ./slurm-smd_[0-9]*.deb
rm *.deb && apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

ENTRYPOINT ["tini", "-g", "--", "slurmdbd", "-D"]

################################################################################
# SLURM: slurmrestd
# BUILD: `docker build --target=slurmrestd -t [<registry>/]slurmrestd:<tag> .`
################################################################################
FROM base AS slurmrestd

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.title="Slurm REST API Agent" \
  org.opencontainers.image.description="slurmrestd - Interface to Slurm via REST API" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/slurmrestd.html"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  name="Slurm REST API Agent" \
  summary="slurmrestd - Interface to Slurm via REST API" \
  description="slurmrestd - Interface to Slurm via REST API"

USER root
WORKDIR /tmp/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#pkg_install
RUN <<EOR
# Install Slurm Packages
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --fix-broken \
  ./slurm-smd-doc_[0-9]*.deb \
  ./slurm-smd-slurmrestd_[0-9]*.deb \
  ./slurm-smd_[0-9]*.deb
rm *.deb && apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

ENTRYPOINT ["tini", "-g", "--", "slurmrestd"]
CMD ["0.0.0.0:6820"]

################################################################################
# SLURM: sackd
# BUILD: `docker build --target=sackd -t [<registry>/]sackd:<tag> .`
################################################################################
FROM base AS sackd

SHELL ["bash", "-c"]

ARG DEBIAN_FRONTEND=noninteractive

# OCI Annotations
# https://github.com/opencontainers/image-spec/blob/v1.0/annotations.md
LABEL \
  org.opencontainers.image.title="Slurm Auth/Cred Server" \
  org.opencontainers.image.description="sackd - Slurm Auth and Cred Kiosk Daemon" \
  org.opencontainers.image.documentation="https://slurm.schedmd.com/sackd.html"

# HasRequiredLabel requirement from Red Hat OpenShift Software Certification
# https://access.redhat.com/documentation/en-us/red_hat_software_certification/2024/html/red_hat_openshift_software_certification_policy_guide/assembly-requirements-for-container-images_openshift-sw-cert-policy-introduction#con-image-metadata-requirements_openshift-sw-cert-policy-container-images
LABEL \
  name="Slurm Auth/Cred Server" \
  summary="sackd - Slurm Auth and Cred Kiosk Daemon" \
  description="sackd - Slurm Auth and Cred Kiosk Daemon"

USER root
WORKDIR /tmp/

# Ref: https://slurm.schedmd.com/quickstart_admin.html#pkg_install
RUN <<EOR
# Install Slurm Packages
set -xeuo pipefail
apt-get -qq update
apt-get -qq -y install --fix-broken \
  ./slurm-smd-client_[0-9]*.deb \
  ./slurm-smd-doc_[0-9]*.deb \
  ./slurm-smd-sackd_[0-9]*.deb \
  ./slurm-smd_[0-9]*.deb
rm *.deb && apt-get clean && rm -rf /var/lib/apt/lists/*
EOR

RUN <<EOR
# Create sackd Entrypoint Wrapper
set -xeuo pipefail
cat <<EOF | tee /usr/local/bin/sackd.sh
#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
sackd "\$@" 1>/proc/1/fd/1 2>/proc/1/fd/2
exec sleep infinity
EOF
chmod +x /usr/local/bin/sackd.sh
EOR

RUN <<EOR
# Create SlurmUser with home directory and add to video and render groups
set -xeuo pipefail
groupadd --system --gid=${SLURM_USER_GID} ${SLURM_USER}
useradd --system --no-log-init --uid=${SLURM_USER_UID} --gid=${SLURM_USER_GID} \
  --create-home --home-dir /home/${SLURM_USER} --shell=/usr/sbin/nologin ${SLURM_USER}
usermod -aG video,render ${SLURM_USER}
EOR

ENTRYPOINT ["tini", "-g", "--", "sackd.sh"]
