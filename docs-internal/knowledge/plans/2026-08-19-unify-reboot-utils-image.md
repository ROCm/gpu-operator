# Unify the driver-upgrade reboot utils image (drop the OpenShift `rhubi-` variant)

## Context

The driver-upgrade reboot pod (`getRebootPod` in
`internal/controllers/upgrademgr.go`) selected a **different** utils image on
OpenShift than on vanilla Kubernetes:

- vanilla: `docker.io/rocm/gpu-operator-utils:latest`
- OpenShift: `docker.io/rocm/gpu-operator-utils:rhubi-latest`

The `rhubi-` (RHEL UBI) utils image is **not maintained** — published tags on
Docker Hub only show `rhubi-latest` and a stale `rhubi-v1.2.1`; there is no
per-release `rhubi-<version>` image. This makes the OpenShift reboot path
depend on an unmaintained image variant, while the rest of the operator's
utils usage (remediation reboot, worker manager) already uses the single
vanilla image on both platforms.

This surfaced during KUBE-46 (utils-container privileged SCC) analysis: the
reboot pod was the only place still branching the utils image by platform.

## Approach

Remove the OpenShift-specific utils image branch so the reboot pod uses the
same `defaultUtilsImage` on all platforms:

- Delete the `defaultOcUtilsImage` constant.
- Delete the `if h.isOpenShift { utilsImage = defaultOcUtilsImage }` branch in
  `getRebootPod`.

The `isOpenShift` flag was **only** consumed by that branch within
`upgrademgr.go`; with the branch gone it became dead plumbing, so it is removed
from the `upgradeMgr` helper:

- Drop the `isOpenShift` field from `upgradeMgrHelper`.
- Drop the `isOpenShift` parameter from `newUpgradeMgrHandler` and
  `newUpgradeMgrHelperHandler`.
- Update the single caller in `device_config_reconciler.go`.

Note: `isOpenShift` remains wired through the reconciler and other managers
(e.g. remediation); only the now-unused upgrade-manager plumbing is removed.

### Behavior note

In practice the default DeviceConfig always sets
`commonConfig.utilsContainer.image`, which overrides this Go default in
`getRebootPod`, so real deployments already used the CR-provided image. This
change only affects the fallback when the CR does not set a utils image — and
makes that fallback consistent (vanilla image) instead of pulling an
unmaintained `rhubi-latest`.

### Alternatives considered

- **Keep the branch, publish `rhubi-<version>` images** — rejected; adds a
  maintenance burden for an image variant that isn't being maintained, and the
  vanilla utils image already works on OpenShift (validated during KUBE-46:
  the reboot pod ran the real `nsenter … reboot` on an OCP MI350P node).

## Scope

**In:** `getRebootPod` utils image selection; removal of dead `isOpenShift`
plumbing in `upgrademgr.go` and its caller.

**Out:** `plugin.go` (`defaultUbiDevicePluginImage`) and `nodelabeller.go`
(`defaultUbiNodeLabellerImage`) also have `rhubi-`/`rhubi-latest` variants, but
those are device-plugin / node-labeller images (independently versioned,
out of scope for this change).

## Validation

- `go build ./internal/...` — passes.
- `go vet ./internal/controllers/` — clean.
- `go test ./internal/controllers/` — passes.
- KUBE-46 OCP validation already exercised the reboot pod with the vanilla
  utils image (`privileged: false` custom SCC) end-to-end: a real MI350P
  worker rebooted and recovered, confirming the vanilla image works on
  OpenShift for the reboot path.

## Risks / Rollback

- **Low risk.** Only changes the fallback utils image for the OpenShift reboot
  pod; default deployments already override it via the CR.
- **Rollback:** restore the `defaultOcUtilsImage` constant and the
  `if h.isOpenShift` branch (and the `isOpenShift` plumbing) in
  `upgrademgr.go`.
