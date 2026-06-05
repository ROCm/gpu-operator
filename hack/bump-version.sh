#!/usr/bin/env bash
#
# bump-version.sh — render the codebase with a new project version.
# Standalone replacement for the Makefile `update-version` /
# `update-helm-metadata` / `update-version-in-ci` targets so infra can
# drive a version bump without invoking `make`.
#
# Usage:
#   hack/bump-version.sh <PROJECT_VERSION> [IMAGE_TAG]
#
# Arguments:
#   PROJECT_VERSION   Required. X.Y.Z or vX.Y.Z. Drives chart `version:`,
#                     Go operand-image defaults, CI PROJECT_VERSION env,
#                     Makefile `PROJECT_VERSION ?=` default, OLM CSV
#                     name+version, docs version, etc.
#   IMAGE_TAG         Optional. Defaults to PROJECT_VERSION. Drives
#                     Dockerfile `version=`/`release=` labels and helm
#                     `appVersion:`. For a dev artifact pass `dev`; for
#                     a release pass the release version (or just omit).
#
# Examples:
#   hack/bump-version.sh 1.6.0          # release bump: IMAGE_TAG = v1.6.0
#   hack/bump-version.sh 1.6.0 dev      # dev artifact of project v1.6.0
#   hack/bump-version.sh 1.6.0 1.6.0    # same as the first form

set -euo pipefail

usage() {
    cat <<EOF
Usage: $(basename "$0") <PROJECT_VERSION> [IMAGE_TAG]

  PROJECT_VERSION   Required (X.Y.Z or vX.Y.Z)
  IMAGE_TAG         Optional, defaults to PROJECT_VERSION

Examples:
  $(basename "$0") 1.6.0
  $(basename "$0") 1.6.0 dev
EOF
    exit 1
}

POSITIONAL=()
for arg in "$@"; do
    case "$arg" in
        -h|--help) usage ;;
        --*)
            echo "ERROR: unknown flag: $arg" >&2
            exit 1
            ;;
        *) POSITIONAL+=("$arg") ;;
    esac
done

if [[ ${#POSITIONAL[@]} -lt 1 || ${#POSITIONAL[@]} -gt 2 ]]; then
    usage
fi

RAW_PROJECT_VERSION="${POSITIONAL[0]}"
RAW_IMAGE_TAG="${POSITIONAL[1]:-${POSITIONAL[0]}}"

# Normalize PROJECT_VERSION: must be X.Y.Z, prefix v.
PV_NUM="${RAW_PROJECT_VERSION#v}"
if [[ ! "$PV_NUM" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "ERROR: PROJECT_VERSION must look like X.Y.Z or vX.Y.Z (got: $RAW_PROJECT_VERSION)" >&2
    exit 1
fi
PROJECT_VERSION="v${PV_NUM}"
VERSION="${PV_NUM}"

# Normalize IMAGE_TAG. If it looks like a version (X.Y.Z or vX.Y.Z),
# coerce to vX.Y.Z so the user can pass either form by mistake and
# still get the correct Dockerfile label / helm appVersion. Anything
# else (e.g. "dev", "latest", "rc1") is taken verbatim — Makefile's
# IMAGE_TAG default is the literal "dev", and we don't want to mangle
# free-form tags.
IT_NUM="${RAW_IMAGE_TAG#v}"
if [[ "$IT_NUM" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    IMAGE_TAG="v${IT_NUM}"
else
    IMAGE_TAG="${RAW_IMAGE_TAG}"
fi

# Helm chart version/appVersion mirror Makefile derivation:
#   HELM_CHART_VERSION ?= $(PROJECT_VERSION)
#   HELM_APP_VERSION   ?= $(IMAGE_TAG)
HELM_CHART_VERSION="${PROJECT_VERSION}"
HELM_APP_VERSION="${IMAGE_TAG}"

# Locate the repo root so the script can be run from anywhere.
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [[ ! -f Makefile || ! -d helm-charts-k8s ]]; then
    echo "ERROR: $REPO_ROOT does not look like the gpu-operator repo root" >&2
    exit 1
fi

echo ">>> PROJECT_VERSION=${PROJECT_VERSION}  IMAGE_TAG=${IMAGE_TAG}"

# --- Makefile default --------------------------------------------------------
sed -i "s|^PROJECT_VERSION ?=.*|PROJECT_VERSION ?= ${PROJECT_VERSION}|" Makefile

# --- Helm chart metadata (k8s + openshift + hack metadata-patch) ------------
for chart in hack/k8s-patch/metadata-patch/Chart.yaml \
             hack/openshift-patch/metadata-patch/Chart.yaml \
             helm-charts-k8s/Chart.yaml; do
    [[ -f "$chart" ]] || continue
    sed -i -e "s|^appVersion:.*|appVersion: \"${HELM_APP_VERSION}\"|" "$chart"
    sed -i "0,/^version:/s|^version:.*|version: ${HELM_CHART_VERSION}|" "$chart"
done

# --- Dockerfile labels ------------------------------------------------------
sed -i "s/release=\"[^\"]*\"/release=\"${IMAGE_TAG}\"/g" Dockerfile internal/utils_container/Dockerfile
sed -i "s/version=\"[^\"]*\"/version=\"${IMAGE_TAG}\"/g" Dockerfile internal/utils_container/Dockerfile

# --- Default operand image tags in Go source --------------------------------
sed -i "s|defaultConfigManagerImage.*=.*\"docker.io/rocm/device-config-manager:[^\"]*\"|defaultConfigManagerImage = \"docker.io/rocm/device-config-manager:${PROJECT_VERSION}\"|" internal/configmanager/configmanager.go
sed -i "s|defaultMetricsExporterImage.*=.*\"docker.io/rocm/device-metrics-exporter:[^\"]*\"|defaultMetricsExporterImage = \"docker.io/rocm/device-metrics-exporter:${PROJECT_VERSION}\"|" internal/metricsexporter/metricsexporter.go
sed -i "s|defaultTestRunnerImage.*=.*\"docker.io/rocm/test-runner:[^\"]*\"|defaultTestRunnerImage = \"docker.io/rocm/test-runner:${PROJECT_VERSION}\"|" internal/testrunner/testrunner.go

# --- CI job + asset-push script ---------------------------------------------
if [[ -f .job.yml ]]; then
    sed -i -e "s|PROJECT_VERSION=[^ ]*|PROJECT_VERSION=${PROJECT_VERSION}|" .job.yml
    sed -i "0,/HELM_CHARTS_VERSION=/s|HELM_CHARTS_VERSION=[^ ]*|HELM_CHARTS_VERSION=${PROJECT_VERSION}-\${RELEASE:-dev}|" .job.yml
fi
if [[ -f asset-build/gpuoperator-asset-push.sh ]]; then
    sed -i "s|PROJECT_VERSION:-.*$|PROJECT_VERSION:-${PROJECT_VERSION}\}|" asset-build/gpuoperator-asset-push.sh
fi

# --- Sphinx docs version -----------------------------------------------------
if [[ -f docs/conf.py ]]; then
    sed -i "s|^version = \"[^\"]*\"|version = \"${VERSION}\"|" docs/conf.py
fi

# --- OLM bundle CSV ----------------------------------------------------------
CSV=bundle/manifests/amd-gpu-operator.clusterserviceversion.yaml
if [[ -f "$CSV" ]]; then
    sed -i "s|^  name: amd-gpu-operator\.v[0-9].*|  name: amd-gpu-operator.${PROJECT_VERSION}|" "$CSV"
    sed -i "s|^  version: [0-9].*|  version: ${VERSION}|" "$CSV"
fi

# --- Generated helm-chart artifacts (CRD labels + README badges) -------------
for crd in helm-charts-k8s/crds/deviceconfig-crd.yaml \
           helm-charts-openshift/crds/deviceconfig-crd.yaml; do
    [[ -f "$crd" ]] || continue
    sed -i "s|helm.sh/chart: gpu-operator-charts-v[0-9][^[:space:]]*|helm.sh/chart: gpu-operator-charts-${HELM_CHART_VERSION}|" "$crd"
    sed -i "s|app.kubernetes.io/version: \"v[0-9][^\"]*\"|app.kubernetes.io/version: \"${HELM_CHART_VERSION}\"|" "$crd"
done

for readme in helm-charts-k8s/README.md helm-charts-openshift/README.md; do
    [[ -f "$readme" ]] || continue
    # Accept either vX.Y.Z or 'dev' in the existing badge text so the
    # rewrite is correct whether main has a release tag or the dev default.
    sed -i -E "s|Version-(v[0-9]+\.[0-9]+\.[0-9]+\|dev)-informational|Version-${HELM_CHART_VERSION}-informational|g" "$readme"
    sed -i -E "s|AppVersion-(v[0-9]+\.[0-9]+\.[0-9]+\|dev)-informational|AppVersion-${HELM_APP_VERSION}-informational|g" "$readme"
    sed -i -E "s|!\[Version: (v[0-9]+\.[0-9]+\.[0-9]+\|dev)\]|![Version: ${HELM_CHART_VERSION}]|g" "$readme"
    sed -i -E "s|!\[AppVersion: (v[0-9]+\.[0-9]+\.[0-9]+\|dev)\]|![AppVersion: ${HELM_APP_VERSION}]|g" "$readme"
done

echo
echo ">>> Done. Version rewrites applied (unstaged)."
echo ">>> Review with: git status && git diff"
