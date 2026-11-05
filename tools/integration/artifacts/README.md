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
their narrower component scope.

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

`daemon.py --package ABSOLUTE_PACKAGE` cross-builds the actual
`cmd/hfs-controlled-runtime` for FreeBSD/amd64 with cgo disabled and records
its Go source/dependency/license closure and Go build ID. Set an explicit
private `GOMODCACHE`; `--prepare` downloads only the selected target dependencies
there before the offline build. Other projects’ module caches are not inputs.
A Go build ID is not assumed to be a GNU note. The binary is never started by the builder.

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
symbols; and the actual FreeBSD Go runtime. It distinguishes source-owned
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
records. It never substitutes successful object tests for full builds. The
recorded repository revision is the actual Go builder's baseline; the owned
source changes and new recipes have separate hashes and retained bytes. The
baseline alone does not claim to contain those changes.

`check_candidate.py` exercises the real assembled package and mutations of a
private copy, including source/patch disagreement, old features, foreign ELF,
false rebuild evidence and authority claims. It restores the copy and rechecks
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
