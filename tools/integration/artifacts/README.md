# Owned controlled artifact builder (xz3)

This is a build and offline verification layer for disposable retained-9p
validation. Its selected tuple is terminal 1.4, body schema 5, features 32767,
msize 65536 and normal protocol 3.0. It includes the actual C1–C10 sources;
`Publication` remains null. Full source/build/package receipts and an inert
installation proposal are separate from installed-byte admission and native
acceptance. These tools contain no VM, mount, module-load, installation or
publication action.

Development status: the complete Linux kernel, image and configured modules
have passed two clean builds with byte-identical outputs, including actual
module CRC/vermagic checks. The full native bhyve exporter, matching owned
lib9p/libvmmapi archives and FreeBSD Go runtime also reproduce byte-for-byte
across clean builds. The candidate passes complete offline component admission.
These results establish build reproducibility; kernel boot and filesystem
behavior remain unverified. Older object receipts retain
their narrower component scope. Those historical daemon results describe the
quarantined CGO_ENABLED=0 daemon, which current admission refuses. The dynamic
CGO daemon must be rebuilt, reproduced and admitted on its own.

## Kernel and daemon recipes

`kernel.py` has four explicit stages: `configure`, `vmlinux`, `image-modules`,
and `collect`. Every invocation takes the same absolute `--source`, `--output`,
`--package` and `--operator-public`. The initial output/package roots must be
fresh and disjoint from the verified source tree. Each stage must run under the
coordinator's reviewed resource guard: two CPUs, 2 GiB descendant RSS,
20 minutes per stage, at least max(8 GiB,25% physical) MemAvailable and 8 GiB
aggregate allocated disk across **all** old/new roots, archives, caches,
temporaries and remote artifacts. Swap is telemetry only. Retain failed outputs.

`kernel.config` is an unbooted x86_64 candidate resolved from allnoconfig. The
owned provider, guest 9p and virtio-9p are built into the complete custom kernel;
virtio-net is a matching in-tree module. Generic MM/GUP/page flag/THP and boot
identity changes are part of this full kernel ABI. A stock/ELRepo kernel plus
an external module cannot satisfy it. The recipe refuses disabled mandatory
symbols and retains the exact resolved configuration, compiler binaries and
loader dependencies, source inventory, timestamps, kernel/module outputs and
GNU build IDs. It does not assert that candidate firmware/storage/rootfs/security
choices match a currently running target.

SELinux must be an active LSM, not only built. `kernel.py configure` appends
the fragment to an allnoconfig `.config`; allnoconfig has already written
`CONFIG_LSM` with its DAC-only default (no `selinux`), and olddefconfig keeps an
already-set string, so candidates up to `e01fab21` had `SECURITY_SELINUX=y`
but booted without SELinux (zq3-r6 preparation, c64). The fragment now assigns
`CONFIG_LSM="lockdown,yama,integrity,selinux,bpf,landlock"`, the RHEL 9 order
(Rocky's stock config excerpt in the 9p-boundary inventory has no `CONFIG_LSM`
line). Listed LSMs that are not built (lockdown, yama, landlock, bpf) are
skipped, so the active set is capability, integrity and selinux. The other
SELinux values (`BOOTPARAM`, `DEVELOP`, `AVC_STATS`, no runtime `DISABLE`,
`CHECKREQPROT_VALUE=0`, sidtab/sid2str sizes) are pinned to Rocky's stock values;
`DEVELOP` boots permissive until systemd loads the policy and applies
`/etc/selinux/config`. `assert_config` (shared by `kernel.py`, `abi.py` and
`admit.py`) refuses a resolved config whose quoted `CONFIG_LSM` lacks
`selinux`. It runs after the operator-key check, so the abi-negative
operator refusal text is unchanged. A `configure` run on the pinned source
(`/home/paul/tmp/claude/c64-config`, operator `2f6d909c…`) resolved a `.config`
that differs from candidate `e01fab21`'s `linux/config` only in the
`CONFIG_LSM` line. Only `security/security.c` (built in) reads `CONFIG_LSM`,
so the kernel image changes; the in-tree modules and `Module.symvers` should
not. The c64+d64 rebuild (candidate `66b5bb2b…`,
[evidence](evidence/xz3-20261110g/README.md)) confirmed both: the resolved
config is that same `28fe4a8e…`, and the five modules, `Module.symvers` and
`abi-negative.json` came out byte-identical to `e01fab21`. The y64 rebuild
(candidate `867ede36…`, [evidence](evidence/xz3-20261110h/README.md)) kept
that config; its g64+m64 patch series changed the image and the CRCs of the
hfs exports in `Module.symvers`, while the five modules and
`abi-negative.json` again came out byte-identical.

The ordinary namespace fixture selects a rootful OCI runtime in the initial
user namespace, VFS image storage and a newly created private network namespace
with loopback only. Mount, PID, network, UTS and IPC namespaces must actually
differ from the guest's initial namespaces; host-network or host-only probes do
not satisfy it. `kernel.py` and component admission share the same resolved
config checks, including namespace, numeric multiuser credentials, local locks,
memory/PID cgroups, proc/sys/tmpfs/devpts, seccomp and SELinux/audit primitives.
In selected Linux `init/Kconfig`, NAMESPACES depends on MULTIUSER; IPC_NS depends
on SYSVIPC or POSIX_MQUEUE; NET_NS depends on NET; CGROUP_PIDS and MEMCG live
under CGROUPS. There is no separate MNT_NS switch. TMPFS_XATTR (selected by the
maintained TMPFS_POSIX_ACL) supports security labels on runtime tmpfs mounts.
UTS_NS and CGROUP_PIDS repair previously disabled prerequisites; IPC_NS is now
explicit instead of relying on its default. Resolved configs reject missing,
disabled, modular or duplicate required entries. The maintained fragment alone
does not establish Kconfig resolution.

Overlay, veth, bridge, NAT/CNI and Kubernetes scheduling are outside this
fixture's capability claim; they are not prerequisites for VFS/private-loopback
OCI namespaces. This choice preserves the mandatory namespace/pod filesystem
gate. The [fixture requirements](../../../docs/lab/Retained-9p-fixtures.md#ordinary-pod-namespace-profile)
still demand a pinned isolated runtime/image, actual namespace observations,
numeric IDs/full groups, SELinux enforcing and shared-bind lock evidence.
USER_NS enables a foreign-userns refusal control, never foreign-userns guest
attestation. Actual full image/modules rebuild and clean reproduction belong
to xz3 after config and runtime repairs; installed boot and actual pod acceptance
remain zq3. Historical candidate reproducibility does not cover this config.

`daemon.py --package ABS --cc-wrapper ABS --sysroot ABS --clang ABS --lld ABS`
cross-builds the actual `cmd/hfs-controlled-runtime` for FreeBSD/amd64 with
**cgo enabled**. CGO is mandatory: with `CGO_ENABLED=0` the FreeBSD build
compiles `process_freebsd_nocgo.go`, whose `observeProcess` always returns
`Unsupported`. The runtime starts bhyve before observing it, so such a daemon
can never reach native READY. The cgo build compiles `process_freebsd.go` and
`process_freebsd.h` against the FreeBSD libc ABI.

The wrapper, clang, `ld.lld` and sysroot are explicit absolute arguments;
nothing defaults to a local path. The wrapper must name exactly the given
clang, `ld.lld` and sysroot, use `--no-default-config` and target
`x86_64-unknown-freebsd15.1`. The sysroot is a closed capture: `capture.json`
must list exactly its files and their hashes. Some host inputs lie outside the
sysroot and are derived rather than passed: clang's resource directory (from
the wrapper's `-print-resource-dir`; it is searched before the sysroot, so
e.g. `stddef.h` comes from there) and the host shared libraries loaded by
clang, `ld.lld` and the wrapper's `#!` interpreter (via `ldd`). Every resource
file is hashed. The wrapper's effective header search list (`-E -v`) must stay
inside that resource directory and the sysroot. All of this is measured before
and after the build and must be unchanged. Admission cannot re-hash these
unpackaged host files; it only requires that they were recorded. The record
keeps wrapper bytes, clang/lld hashes, versions and library closures, the
resource tree hashes, the header search list, the capture manifest hash and
platform, explicit
`CGO_CFLAGS`/`CGO_LDFLAGS` and Go's embedded build settings. It also records
the binary's runtime closure: PT_INTERP `/libexec/ld-elf.so.1`, exact DT_NEEDED
order and every transitive base library, with sysroot path and hash. These
libraries stay external and are not redistributed. A sysroot prefix map keeps
capture paths out of C debug info.

`-trimpath -buildvcs=false`, a forced `-a` rebuild, an explicit private
`GOMODCACHE` and `GOPROXY=off` remain. `--prepare` downloads only the selected
target dependencies there first, using the same cgo environment, so the source
closure matches. Other projects’ module caches are not inputs. The record
keeps the Go source/dependency/license closure, including runtime/cgo C files,
and the Go build ID. A Go build ID is not assumed to be a GNU note. The builder
parses the binary and never starts it.

The external operator's fresh disposable **public** key is a kernel input.
The private key stays outside the repository, package and runtime process.
A candidate public pin does not constitute external consent, endpoint
provisioning, guest attestation, a release signer or normal authority.

## Package and component checks

`package.py seal` writes one fresh immutable manifest from actual package files
and a path-to-kind JSON inventory. `package.py check` requires an independently
supplied manifest SHA256, operator public key and repository source revision;
it checks the closed tuple/policy, exact file set, bytes and digests. It rejects
extra files, symlinks, path escapes, duplicate JSON keys and mismatched identity.
This is a container check, not proof that arbitrary labelled files form a
working filesystem.

`admit.py` additionally requires the actual full linked Linux kernel, complete
configured in-tree module inventory and matching source/config/build records;
the complete FreeBSD exporter with strong production controller/bootstrap
symbols; and the actual dynamic CGO FreeBSD Go runtime. Daemon admission
requires a `homelabfs-daemon-build/2` record with cgo enabled. Version 1
records describe the CGO0 daemon and are always refused. Go's embedded build
information must show `CGO_ENABLED=1`, freebsd/amd64, `-trimpath` and the
recorded Go version. The binary must define `x_cgo_init` and the Go and C cgo
trampolines for `C.hfs_process`. Build constraints make that observer and the
nocgo stub mutually exclusive, and the recorded source closure must contain
`process_freebsd.go`/`.h` but not `process_freebsd_nocgo.go`. PT_INTERP must be
`/libexec/ld-elf.so.1`. DT_NEEDED must equal the recorded list exactly. The
recorded transitive closure must be fully reachable, and every library and the
loader must match the packaged sysroot capture, whose hash is in the record. A
packaged copy of the cc wrapper must match its recorded hash. The admission
refuses a stripped binary, a CGO0 binary, a foreign loader, extra or missing
DT_NEEDED entries and any sysroot hash mismatch. It distinguishes source-owned
FreeBSD inputs from exactly hashed installed **binary sysroot** inputs. OS
version strings cannot supply invented source ancestry for installed libraries.
Neither successful verifier changes installed/booted/native acceptance to true.
The complete package must first exist before this verifier can pass.

`native_bundle.py` retains the entire selected native source and complete
upstream notices, including per-file license text. Admission expands this
bounded archive and checks every source hash against the actual native build.
The original archive remains pinned by URL, commit and captured hash; its first
capture was explicitly TOFU, not a previously published checksum. Installed
sysroot/runtime libraries are measured external inputs and are not bundled.

`assemble.py --package ABS --evidence-root ABS --operator-public HEX
--physical-bytes N` consumes completed component outputs and retained proof
records. It never substitutes successful object tests for full builds. It
copies the recorded cc wrapper to `inputs/build-recipes/freebsd-cc` and the
sysroot `capture.json` to `inputs/freebsd-cgo-sysroot-capture.json` after
checking their recorded hashes. Both daemon recipes (`daemon.py`,
`daemon_cgo.py`) must match the retained builder bindings. The
recorded repository revision is the actual Go builder's baseline; the owned
source changes and new recipes have separate hashes and retained bytes. The
baseline alone does not claim to contain those changes.

`check_candidate.py` exercises the real assembled package and mutations of a
private copy, including source/patch disagreement, old features, foreign ELF,
false rebuild evidence and authority claims. Daemon cases cover a real
`CGO_ENABLED=0` daemon supplied read-only via `--cgo0-daemon`, a CGO0 record,
the nocgo stub in the source closure, renamed observer symbols, and a foreign
loader. They also cover extra or missing DT_NEEDED, a sysroot library hash
mismatch, an unlinked capture, unrecorded clang resource headers or tool
library closure, and a foreign cc wrapper. It restores the copy and rechecks
both it and the immutable original. Small unit fixtures cover parser boundaries;
they are not component or runtime acceptance evidence.

The package includes every configured module. The later installed manifest must
separately select the exact actual loaded subset, with immutable absolute paths,
file hashes and GNU build IDs. The existing controlled runtime installed probe
requires equality with the complete live `/proc/modules` inventory and checks
kernel notes, boot/provider nonce and all-domain scope/protocol state. Packaging
all drivers is not permission to ignore unknown or mismatched loaded modules.

## Native build and placement boundaries

Native FreeBSD source preparation/full bhyve build must use one fresh private
Paul-owned `/var/tmp` root and the exact reviewed source/dependency closure.
No buildworld, global source tree, system package installation, privilege or VM
operation is needed or authorized. The native guard must constrain one CPU,
512 MiB **aggregate descendants**, 20 minutes per stage, the same global 8 GiB
local-plus-remote disk bound, and the exact current VM/process invariant. The
configured 12 GiB Rocky plus 3 GiB host reserve leaves only about 738 MiB on
f3; a 2 GiB compiler is inadmissible despite a transient free-page sample.
Exact native commands/path/guard need coordinator review before remote writes.

`proposal.py --physical-bytes N` emits inert memory arithmetic. Adding
`--package ABS --operator-public HEX` pins the actual component artifacts to an
inert installation envelope, with proposed time/disk limits and a private host
staging prefix. Its execution authority remains false. Its alternative
requires a separately approved clean shutdown/RAM change/boot window, with
native guests stopped. It retains the full 4 MiB canary and 2 GiB signed guest
ceiling, funds the separate 1 GiB C8 mmap/serializer ownership in addition to
652 MiB transport arenas, and includes service/recovery, actual provider copies,
controller/exporter and guest owners. Arithmetic fit never admits actual memory
pressure, allocator peaks, PTE/TLB behavior, source/drain progress or a VM plan.

C10 readiness still requires one actual VM/boot/process/endpoint, all pending
devices and exact signed scope inventory, fd6 HSB1/fd7 parent, strong startup
once, all HSM5 device claims, protected funded HELLO and fresh measurements.
C9 restoration remains limited to the same surviving owner/current cut;
replacement FUSE/exporter and guest RESUME remain blocked. `zq3` owns actual
single-node deployed acceptance; later `tr3` release packaging is independent.
