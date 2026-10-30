# Canary kernel and artifact policy

[Architecture](../Architecture.md) · [Packaging and rollout](../Packaging-and-rollout.md) · [Deployment compatibility](../Deployment-tests.md) · [Approval contract](../Test-automation.md)

## Scope and admission status

This policy defines admission of kernel, module and daemon combinations for
disposable HomelabFS lab and canary volumes containing synthetic data. It does
not select a new canary target, install software, authorize a boot or test, or
publish packages. Production workload migration, production PV changes and NFS
retirement belong to another project. Existing NFS, stunnel, CARP, mount monitors,
package-service storage and production power automation remain in place.

**No future canary artifact set is admitted by this document.** Exact target
identities, complete manifests, trusted signer fingerprints, validation evidence
and rollback records must be supplied and reviewed before admission. Missing
fields mean refused, not an invitation to substitute a plausible version or key.
The existing f3 preparation and completed PoCs establish reusable test assets and
historical evidence only. They grant no new execution approval.

The following matrix records dated evidence and candidate choices. It is not a
current VM inventory. Reinspect actual state before an approved operation.

| Target or candidate | Kernel/module/daemon set | Evidence and admission decision |
|---|---|---|
| r0–r2, inspected 2026-10-28 | Rocky 9.8 x86_64, `5.14.0-687.60.1.el9_8.x86_64`; `9pnet_virtio` absent | [Dated baseline](../Deployment-readiness.md#observed-baseline-2026-10-28). Unsupported for the proposed 9p canary path. This does not authorize changing their kernels. |
| f3/rocky transport PoC, 2026-10-24 and regression 2026-10-25 | ELRepo `kernel-lt`, `kernel-lt-core`, `kernel-lt-modules`, `kernel-lt-modules-extra`, all `6.1.190-1.el9.elrepo` x86_64; guest `9p`, `9pnet`, `9pnet_virtio`; f3 host FreeBSD FUSE passthrough probe | [PoC](../PoC-f3-rocky.md), [runbook](../PoC-runbook.md) and [lab results](../Lab-operations.md). Historical functional evidence only; not a supported HomelabFS release or signed canary manifest. Secure Boot was disabled; SELinux was enforcing. Stock kernel retained as normal default. |
| Native f3 lab, initial run 2026-10-25 | hfs-test0–hfs-test2: FreeBSD 15.1-RELEASE amd64, each with its own kernel `fusefs`, ZFS pool and Go FUSE passthrough probe; initial build used Go 1.26.8 and go-fuse v2.9.0 | [Lab operations](../Lab-operations.md). Direct guest FUSE evidence; no Rocky or 9p in this native path. No distributed daemon validated. Do not infer live power state from the recorded final state. |
| Future isolated Rocky guest candidate | Exact imported full-kernel RPM set, or centrally built kernel/modules, plus exact FreeBSD exporter and real HomelabFS daemon set | Not selected or admitted. ELRepo is one candidate; the PoC version is not automatically the candidate. Must meet every gate below. |
| Future shared-host canary candidate | Same complete pinned guest/host/daemon contract, bound to explicitly named approved guests and hosts | Not selected or admitted. Requires separate installation and maintenance approval, capacity/isolation admission and matching compatibility evidence. Existing r-nodes are not implicitly eligible. |

The stock-source external-module attempt against
`5.14.0-687.59.1.el9_8.x86_64` failed because the disabled 9p sources did not match
the distribution's backported interfaces. It supplies no installable supported
module set. Installing a generic extra-modules package or force-loading modules
is not an accepted workaround. Neither upstream kernel version nor a general
distribution kABI promise replaces validation of the exact selected set.

## Complete artifact manifest

Assign an immutable artifact-set ID to each candidate. Preserve a signed manifest
with content digests and lengths for every artifact and an explicit dependency
closure. Build/import once for each supported OS release, architecture and full
kernel release/ABI; reuse those exact bytes on admitted targets. Never rebuild
per consumer or overwrite a released version in place.

The manifest must include:

| Component | Required identity and retained material |
|---|---|
| Rocky guest kernel | OS release and architecture; full `uname -r`; RPM name/epoch/version/release/architecture for the kernel, core, modules, extra modules and all runtime dependencies. For an ELRepo candidate retain all four `kernel-lt` packages, not just the meta-package. Retain boot image, kernel config, initramfs generation inputs and generated-image digest, boot entry and boot arguments. |
| 9p modules | Exact `9p`, `9pnet`, `9pnet_virtio` files and their dependency closure; package ownership, digests, vermagic/symbol compatibility and signer identities. Record `CONFIG_NET_9P`, `CONFIG_NET_9P_VIRTIO` and `CONFIG_9P_FS`. A built-in implementation must be identified in the exact kernel image/config instead. |
| FreeBSD serving environment | Base/userland and kernel versions, architecture, build/update identity, matching `fusefs.ko` and ZFS implementation, bhyve/lib9p build identity for the 9p path, runtime libraries and relevant configuration digests. Native lab FUSE runs inside each FreeBSD guest; the independent Rocky probe uses f3 host FUSE. |
| HomelabFS daemon | Native FreeBSD package version/digest and complete runtime dependencies; source revision, Go toolchain, `go.mod`/`go.sum`, FUSE adapter/library revision, build flags and service/configuration schema. Record journal/control format and protocol compatibility needed for rollback. The passthrough probe is never interchangeable with the real daemon. |
| Guest/pod access configuration | 9p device/export identity, protocol, exact mount options including cache mode and `msize`, export root, mountpoint, service ordering/readiness rules, SELinux policy packages/local policy digests, and the credential/namespace configuration used in validation. Record Kubernetes/CSI versions and manifests when that publication path is claimed. |
| Trust and recovery | Signed release manifest, RPM/pkg repository metadata and signatures, public trust roots/fingerprints, verification-tool dependencies, revocation information, installation lock policy, and the independently available complete rollback set and recovery procedure. |

The f3s 9p path has a FreeBSD HomelabFS daemon and a Rocky kernel client. It does
not deploy a HomelabFS Linux FUSE daemon in Rocky. A future direct Linux-FUSE
target needs its own manifest and validation; it cannot inherit this admission.
Build headers, source and development packages stay on designated builders
unless a recorded runtime dependency needs them.

For imported RPMs retain upstream URLs, original bytes, upstream signatures and
verification results. For custom builds retain source/SRPM checksums, patches,
full kernel configuration, builder/toolchain identity, dependency locks, build
commands/logs and output checksums. Preserve the corresponding FreeBSD base and
module provenance. Record who imported/built, reviewed and promoted the set,
when, and which evidence identifies its exact inputs. A source checksum alone
does not authenticate a finished kernel or certify a release.

## Enforced trust

Before admission, the release owner must specify actual public signer
fingerprints and an independently authenticated trust-bootstrap procedure. Key
names, HTTPS transport and a checksum downloaded beside a package are not trust
anchors. No fingerprint or HomelabFS signing identity is established here.

The intended f3s repository may distribute admitted immutable artifacts, but its
recorded Rocky examples use `gpgcheck=0`/`repo_gpgcheck=0` and its FreeBSD example
uses `signature_type: "NONE"`. Those examples do not meet this policy. Signing
and client enforcement are prerequisites, not assumed existing capabilities;
see [packaging status](../Packaging-and-rollout.md#fit-with-the-existing-f3s-repository).

Admission requires these enforcement paths to be implemented and evidenced:

- Rocky verifies RPM signatures against the pinned allowed signer set, with
  package and repository-metadata signature checking enabled. It also verifies
  the signed immutable release manifest and exact artifact digests. Neither
  `--nogpgcheck` nor automatic acceptance of an unknown key is permitted.
- FreeBSD verifies a signed repository/catalogue against pinned trust and checks
  package digests against the authenticated release manifest. Offline package
  installation must perform equivalent authenticated verification; a direct
  unsigned `pkg add` is not a bypass. Base/kernel/module verification has an
  explicitly recorded trust path as well as the daemon package verification.
- Package authentication and boot/module authentication are recorded separately.
  A valid RPM signature does not establish that firmware booted that image or
  that the kernel enforces module signatures. Record Secure Boot state, firmware
  trust, bootloader/kernel signature verification, module-signature enforcement
  and accepted module signer identities for each Rocky candidate. The target
  record must state which boot/module controls its trust policy requires and
  evidence their enforcement, including rejection tests for enabled controls.
  This policy requires authenticated artifact verification before installation;
  it does not impose universal Secure Boot enablement. A target without enforced
  boot authentication must record that limitation and must not claim protection
  against replacement after installation. The historical Secure-Boot-disabled
  PoC demonstrates no boot-authentication guarantee.
- The owner records how the selected FreeBSD environment authenticates the
  installed base/kernel/modules and protects their staged files, and the limits
  of its boot trust. Do not claim a Linux Secure Boot result covers FreeBSD.
  Privileged host compromise is not prevented by a daemon's digest check.
- Keep signing private keys outside Git, nginx, consumers and the package PV.
  Use the separately maintained f3s signing/custody procedure. Key rotation or
  revocation changes the admission record, offline trust bundle and affected
  artifact sets; expired, revoked or unknown trust blocks new admission/serving.

On isolated approved targets, evidence must include refusal of unsigned,
wrong-signer, corrupted and wrong-version artifacts/metadata; rejection of
untrusted modules/boot images applies where the declared target controls enforce
that boundary. Documentation or merely
printing a signature is insufficient. Trust configuration changes and these
negative checks require their own approved execution scope.

## Target records and serving refusal

An operator must complete a separate admission record for each target. Required
fields are: target/VM and physical-host identity; isolated or shared-host class;
artifact-set ID and signed-manifest digest; observed installed and running
versions; trust evidence; synthetic volume IDs, unique export/mount paths and
run-owned identities; resource budget; permitted publication profile;
compatibility evidence/run IDs and limitations; rollback location and boot
entry; release/update operator and reviewer; installation approval reference;
and admission/revalidation date. No target or concrete new path is allocated
by this policy. Unfilled fields keep its status `not admitted`.

Before any canary publication or daemon serving readiness, the future admission
implementation must compare the running guest kernel, module/dependency files,
FreeBSD environment, daemon and configuration against that record, including
trust status. Installed RPM versions alone do not identify the running kernel.
Pinning and startup enforcement are required implementation work, not features
that this document adds to the current probe.

An unknown kernel after an automatic or fallback boot must leave HomelabFS
canary access gated: do not mount/publish the canary, announce serving readiness,
or let a consumer write through the plain directory underneath a missing mount.
Record a diagnostic naming the mismatch and retain management/recovery access.
Do not block unrelated production services or force-load incompatible modules.
The retained stock kernel may restore OS administration while lacking 9p; that
is an acceptable recovery boot with HomelabFS unavailable, never a serving
rollback. Loss of a required verified mount or exporter invalidates readiness;
consumer revocation/cache fencing follows the separate fencing contract, not
an assumption that changing a readiness flag stops existing descriptors.

## Pinning, updates and validation ownership

The release owner named in the admission record tracks upstream kernel, 9p,
FreeBSD, FUSE-library, daemon and signing-security advisories. The operator
controls maintenance/boot selection; the reviewer checks provenance and
evidence. Paul approves the concrete installation/lifecycle window and each
test run. An unassigned owner blocks admission; naming these roles does not
appoint a new person or authorize a change.

Pin exact kernel/package versions and configuration under the approved
configuration workflow. Protect the active and rollback kernel entries from
automatic removal, and prevent unattended kernel, module or daemon changes or
general upgrades from advancing the admitted storage set. Record the actual
DNF lock/exclusion/install-only policy, FreeBSD package locks and boot defaults
and verify their effect on the approved target. Locks alone do not protect a
boot entry, and they do not excuse missed security updates.

Every change to a manifest component, patch/configuration, mount/cache policy,
credential/SELinux policy, boot trust, signing key or daemon format/protocol
creates a new candidate or explicitly invalidates the relevant admission.
Revalidation is also required when observed state drifts, evidence cannot be
bound to exact bytes, or an advisory invalidates the accepted security posture.
An urgent fix does not inherit compatibility from a package name: suspend
canary serving if necessary, prepare a new pinned candidate and approved plan,
then validate and review it before promotion. Do not silently extend a revoked
set's approval to get a canary running.

Validation has separate evidence scopes:

1. Native FreeBSD integration validates the real daemon on the selected guest
   kernel/FUSE/ZFS set. It has no 9p dependency.
2. Task `zq3` validates the exact real-daemon FreeBSD FUSE → bhyve/lib9p → Rocky
   kernel → pod path before replication implementation. Include SELinux
   enforcing, UID/GID, supplementary groups, non-root denial, pod security
   contexts and supported namespace mappings; do not disable SELinux or replace
   these checks with a root-only world-writable probe. Apply the
   [2026-10-30 single-mount advisory lock scope](../Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30):
   prove POSIX/flock contention and cleanup within one guest 9p mount/kernel
   inode domain, including pods sharing it. Independent/duplicate mounts,
   other guests and host access have no shared-lock guarantee; record those
   cases as unsupported and separately prove exclusive writable-client
   publication and old-client cached-access fencing. This concession does not
   waive any other gate or admit a target. Check mmap/cache,
   write/fsync/flush propagation against [durability](Durability.md), restart,
   mount disappearance and the smallest safe recovery path. A syscall returning
   success or SQLite process-crash check is not power-loss evidence.
3. Routine live smoke and isolated K01–K05, fencing, lifecycle, physical-host and
   restore profiles retain their separate approval and evidence boundaries.
   Unsupported or unavailable cases are recorded as not run, with corresponding
   claims withheld. Nothing here adds 9p to the native integration matrix.

Candidate validation installation in an isolated environment is a separately
approved provisional operation, not serving admission or shared-host promotion.
Preserve approved plans, immutable manifests, raw results and cleanup/rollback
outcomes outside the test cluster. Reuse applicable evidence only when its exact
artifact set and profile still match; the reviewer records what changed and
which gates must run again. No test is triggered by reading this policy,
building, publishing, admission or completion of another stage.

## Placement and offline rollback

Prefer separately approved isolated guests for evaluating candidates: they
contain kernel changes and allow bounded reboot/failure exercises without
granting production fencing authority. The prepared f3 lab is not permission
to create or overwrite disks. Respect the existing memory/lifecycle constraints:
the three 2 GiB FreeBSD guests must not run alongside the 12 GiB Rocky VM on f3.
Any lifecycle window needs fresh approval and state inspection.

Shared-host installation needs a separate plan naming the guest and physical
host, capacity limits, service impact, management/console path, permitted
maintenance, restoration steps and quorum constraints. An isolated pass is
necessary evidence for its matching profile, not permission for an r0–r2 kernel
rollout or for disruptive shared-host testing. No canary credentials may fence
production guests or hosts.

Before installation, retain both the complete candidate and a validated
known-good rollback set on an independent administrative/offline recovery store,
with a local staged copy reachable without HomelabFS, guest networking, k3s or
the package service. Store the manifest, public trust roots/revocation state,
verification tools, dependencies, boot/recovery instructions and credentials
needed for authorized console access. Private signing keys are not required on
the target. Identify and verify the exact recovery locations in its admission
record; no such new store is provisioned here.

The approved plan must describe both rollback paths:

- **OS recovery:** preserve the prior kernel image, matching modules,
  initramfs/boot entry, default/saved boot state and original VM/export/service
  configuration. Use a controlled one-time candidate boot first, with an
  independent console and recorded fallback entry. On failure, cleanly return
  to the retained boot/configuration without requiring a working canary mount.
  A stock no-9p fallback keeps HomelabFS serving disabled.
- **Serving recovery:** restore an entire previously admitted kernel, modules,
  FreeBSD exporter, daemon and configuration combination. Check daemon protocol
  and journal/control format compatibility before downgrade. If compatibility
  is unproved, keep the canary unavailable and preserve its failed resources;
  use an approved synthetic-data restore into a separate writable destination
  rather than attempting an in-place format downgrade. A retained kernel alone
  is not a complete serving rollback.

Normal unmount and bounded cleanup precede path removal; preserve busy/failed
resources and evidence for inspection. Never force-kill Rocky, recursively
delete through a live mount, overwrite VM disks or modify a received backup
sink to make rollback pass. Recovery involving replicated lab copies must first
follow [lab replication](../Lab-replication.md); never boot or modify the received
sink directly. Rollback rehearsal needs its own approved isolated plan and must
record offline retrieval, signature verification, recovery boot and serving
gates. If that evidence or the complete recovery set is missing, admission is
refused.

## Review outcome required before promotion

Promotion requires a complete authenticated manifest and target record, enforced
trust and pinning evidence, matching native/guest compatibility results,
resource/isolation admission, and an independently recoverable tested rollback
path. Publication, canary installation and every test/lifecycle window still
have separate approval gates. At this policy's creation all future target,
signature-enforcement and real-daemon compatibility gates remain open. Historical
PoCs do not close them, and this document makes no current-runtime claim.
