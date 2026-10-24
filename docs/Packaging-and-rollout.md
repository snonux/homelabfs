# Kernel, module, and daemon distribution

[Architecture](Architecture.md) · [PoC runbook](PoC-runbook.md)

## Agreed direction

Serve the required validated kernel or kernel-module RPMs through **pkgrepo.f3s.buetow.org**. Build once per supported target, test once as a release candidate, then install the same immutable artifacts on multiple nodes. Do not compile kernels or DKMS modules independently on every f3s workload node.

The current PoC uses prebuilt ELRepo kernel-lt RPMs. No custom kernel/module build has passed: the exact Rocky stock-source 9p build failed. For an ELRepo-based release, obtain and verify upstream RPMs once and publish the validated artifacts internally; recompilation is unnecessary. If we later port 9p modules or build a kernel, produce versioned RPMs centrally and use the same distribution flow. The production choice is still open.

## Fit with the existing f3s repository

The f3s skill describes an nginx service in the k3s `infra` namespace, serving package files from the current NFS-backed PV on f0. Rocky clients use:

```text
https://pkgrepo.f3s.buetow.org/rockylinux/9/x86_64/
```

The corresponding current backing path is `/data/nfs/k3svolumes/pkgrepo/rockylinux/9/x86_64/`, containing RPMs and DNF `repodata`. FreeBSD packages use the existing `/freebsd/FreeBSD:15:amd64/latest/` repository. Reuse the f3s packaging orchestration in `conf/packages/Makefile` and the established repository layout, adding a kernel/module packaging workflow rather than treating these as single-binary Go RPMs.

These details come from the f3s skill's package-repository architecture and client-setup references. They describe the existing infrastructure, not a HomelabFS publication already performed. Current Rocky custom-repository examples disable RPM signature checking; signing and client trust enforcement are pending in the wider f3s package plan. Kernel distribution should integrate with that work and must not silently claim the current repository already enforces signatures.

## Artifact and compatibility contract

- Identify each release by OS major version, architecture, complete kernel release/ABI, configuration, patches, and package version. "Compile once" means once for each supported combination, not one module binary for every future kernel.
- For a full kernel, retain all matching runtime packages and dependencies: the tested ELRepo set includes `kernel-lt`, `kernel-lt-core`, `kernel-lt-modules`, and `kernel-lt-modules-extra`. Build/development packages belong on builders when needed, not every consumer.
- For a module release, package the required 9p modules and enforce the tested kernel relationship. Do not assume the distribution's stable kABI covers all internal filesystem interfaces. Fail closed on unsupported kernels; do not force-load modules with mismatched symbols or versions.
- Record source URLs/checksums, source RPMs, patches, build environment, Go dependency locks where relevant, config, build logs, package checksums/signatures, and test results. Preserve upstream signatures when mirroring; sign custom RPMs with the established package-signing workflow. Private keys do not belong in Git, nginx, or the package PV.
- Also distribute the eventual FreeBSD HomelabFS daemon as a native package. The current passthrough binary is a PoC, not a production daemon release.

## Release and rollout procedure to implement

1. Build or import one candidate artifact set on a designated builder, verify provenance, and retain it in staging.
2. Test boot, module load, 9p/FUSE semantics, workload behavior, and recovery on f3/rocky. Re-run relevant tests for every kernel/library update; an unchanged package name is not compatibility evidence.
3. Publish immutable artifacts with generated DNF metadata and checksums through the existing package service. Promote only validated versions; retain a known-good rollback set. Verify client download and signature behavior before rollout.
4. Install the pinned validated version on one eligible guest, retain its old kernel, and select a controlled test boot. Confirm health and rollback before updating additional nodes. Do not reboot enough nodes together to violate storage or Kubernetes quorum.
5. Automate this through the f3s configuration repository after the package and signing flow is ready. Avoid silently advancing the storage kernel through an untested general upgrade. Track upstream security fixes and rebuild/revalidate centrally when required.

## Bootstrap and recovery

The package service currently depends on Kubernetes and NFS; eventually it may depend on HomelabFS itself. Installation and recovery must not require a healthy HomelabFS cluster to retrieve its own kernel, modules, or daemon. Retain the validated RPM/package set, public trust material, and recovery instructions on an independent administrative host or offline recovery store. Pre-stage artifacts before node maintenance. Migrating the package-service PV is part of NFS retirement and requires a separate tested cutover.

## Status

Planning only: no kernel/module RPMs were published to pkgrepo, no repository metadata or signing configuration was changed, and no r0/r1/r2 kernel rollout was performed. f3/rocky remains the validation environment.
