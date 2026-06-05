#!/usr/bin/env bash
# Build amdgpu driver source images for every (driver, OS-minor) combo
# that exists on repo.radeon.com. Tag scheme: coreos-<RHEL_MINOR>-<DRIVER>.
# Skips combos that 404 on the repo. Job succeeds if at least one combo
# builds (and pushes, unless --dry-run).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS_FILE="${SCRIPT_DIR}/versions.txt"
DOCKERFILE="${SCRIPT_DIR}/Dockerfile.coreos"
MANIFEST="${SCRIPT_DIR}/source-images-manifest.json"

REPO_URL="https://repo.radeon.com"
REGISTRY="docker.io"
IMAGE_NAME="amdpsdo/amdgpu-driver"
DRY_RUN=0
FORCE=0
VERSION_OVERRIDE=""
OS_VERSIONS_OVERRIDE=""
DEFAULT_OS_VERSIONS=(9 9.4 9.6 9.7 9.8 10 10.0 10.1 10.2)

log()  { echo "[INFO]  $*"; }
warn() { echo "[WARN]  $*" >&2; }
err()  { echo "[ERROR] $*" >&2; }

usage() {
    cat <<EOF
Usage: $0 [options]

  --registry <url>          Container registry (default: ${REGISTRY})
  --image-name <path>       Image repo path (default: ${IMAGE_NAME})
  --version <X>             Build only this driver version (override versions.txt)
  --os-versions <CSV>       Probe only these OS minors (override default list)
  --dry-run                 Build but do not push
  --force                   Rebuild even if target tag already exists in registry
  -h, --help                Show this help

Reads driver versions from ${VERSIONS_FILE} unless --version is given.
For each (driver, os) combo, HEAD-probes
  ${REPO_URL}/amdgpu/<driver>/el/<os>/main/x86_64/
and builds + pushes the image if the path exists.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --registry)     REGISTRY="$2"; shift 2 ;;
        --image-name)   IMAGE_NAME="$2"; shift 2 ;;
        --version)      VERSION_OVERRIDE="$2"; shift 2 ;;
        --os-versions)  OS_VERSIONS_OVERRIDE="$2"; shift 2 ;;
        --dry-run)      DRY_RUN=1; shift ;;
        --force)        FORCE=1; shift ;;
        -h|--help)      usage; exit 0 ;;
        *) err "unknown flag: $1"; usage; exit 2 ;;
    esac
done

# Resolve driver versions list
if [[ -n "${VERSION_OVERRIDE}" ]]; then
    DRIVER_VERSIONS=("${VERSION_OVERRIDE}")
elif [[ -f "${VERSIONS_FILE}" ]]; then
    mapfile -t DRIVER_VERSIONS < <(grep -vE '^\s*(#|$)' "${VERSIONS_FILE}")
else
    err "no versions: provide --version or create ${VERSIONS_FILE}"
    exit 2
fi

# Resolve OS versions list
if [[ -n "${OS_VERSIONS_OVERRIDE}" ]]; then
    IFS=',' read -ra OS_VERSIONS <<< "${OS_VERSIONS_OVERRIDE}"
else
    OS_VERSIONS=("${DEFAULT_OS_VERSIONS[@]}")
fi

log "Driver versions: ${DRIVER_VERSIONS[*]}"
log "OS versions:     ${OS_VERSIONS[*]}"
log "Registry:        ${REGISTRY}/${IMAGE_NAME}"
log "Dry run:         $([[ ${DRY_RUN} -eq 1 ]] && echo yes || echo no)"
log "Force rebuild:   $([[ ${FORCE} -eq 1 ]] && echo yes || echo no)"

declare -a BUILT_TAGS=()
declare -a FAILED_COMBOS=()
declare -a SKIPPED_COMBOS=()

for driver_ver in "${DRIVER_VERSIONS[@]}"; do
    for os_ver in "${OS_VERSIONS[@]}"; do
        probe_url="${REPO_URL}/amdgpu/${driver_ver}/el/${os_ver}/main/x86_64/"
        log "Probing ${probe_url}"
        http_code=$(curl -sSI -o /dev/null -w "%{http_code}" "${probe_url}" || echo "000")
        if [[ "${http_code}" != "200" ]]; then
            warn "skip: ${driver_ver} / el${os_ver} (HTTP ${http_code})"
            SKIPPED_COMBOS+=("${driver_ver}/el${os_ver}:${http_code}")
            continue
        fi

        tag="${REGISTRY}/${IMAGE_NAME}:coreos-${os_ver}-${driver_ver}"

        if [[ ${FORCE} -eq 0 ]] && docker manifest inspect "${tag}" >/dev/null 2>&1; then
            log "skip: ${tag} already published (use --force to rebuild)"
            SKIPPED_COMBOS+=("${tag}:exists")
            continue
        fi

        log "Building ${tag}"
        if ! docker build \
                --build-arg "REPO_URL=${REPO_URL}" \
                --build-arg "RHEL_VERSION=${os_ver}" \
                --build-arg "DRIVERS_VERSION=${driver_ver}" \
                -f "${DOCKERFILE}" \
                -t "${tag}" \
                "${SCRIPT_DIR}"; then
            err "build failed: ${tag}"
            FAILED_COMBOS+=("${tag}")
            continue
        fi

        if [[ ${DRY_RUN} -eq 1 ]]; then
            log "DRY-RUN: not pushing ${tag}"
        else
            log "Pushing ${tag}"
            if ! docker push "${tag}"; then
                err "push failed: ${tag}"
                FAILED_COMBOS+=("${tag}")
                continue
            fi
        fi
        BUILT_TAGS+=("${tag}")
        if ! docker image rm -f "${tag}" >/dev/null; then
            warn "cleanup failed: ${tag}"
        fi
    done
done

# Manifest
{
    printf '{\n  "registry": "%s",\n  "image": "%s",\n  "dry_run": %s,\n' \
        "${REGISTRY}" "${IMAGE_NAME}" "$([[ ${DRY_RUN} -eq 1 ]] && echo true || echo false)"
    printf '  "versions": ['
    for i in "${!BUILT_TAGS[@]}"; do
        [[ $i -gt 0 ]] && printf ','
        printf '\n    "%s"' "${BUILT_TAGS[$i]}"
    done
    printf '\n  ],\n  "skipped": ['
    for i in "${!SKIPPED_COMBOS[@]}"; do
        [[ $i -gt 0 ]] && printf ','
        printf '\n    "%s"' "${SKIPPED_COMBOS[$i]}"
    done
    printf '\n  ],\n  "failed": ['
    for i in "${!FAILED_COMBOS[@]}"; do
        [[ $i -gt 0 ]] && printf ','
        printf '\n    "%s"' "${FAILED_COMBOS[$i]}"
    done
    printf '\n  ]\n}\n'
} > "${MANIFEST}"

log "Summary: built=${#BUILT_TAGS[@]} skipped=${#SKIPPED_COMBOS[@]} failed=${#FAILED_COMBOS[@]}"
log "Successfully built:"
for t in "${BUILT_TAGS[@]}"; do log "  - ${t}"; done

if [[ ${#BUILT_TAGS[@]} -eq 0 ]]; then
    err "no images built"
    exit 1
fi
exit 0
