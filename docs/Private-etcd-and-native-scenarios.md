# Private offline etcd and native R/F definitions

Task `ir3` adds controller plan/PKI/request preparation, guest-local offline
staging and owned-process helpers, mTLS/RBAC bootstrap, and declarative R01–R10
and F01–F05 definitions. No SSH, guest installation, etcd service, VM action,
ZFS/PF/FUSE action or native scenario was run while implementing it. Local tests
use fake executors, generated test certificates and an owned synthetic sleep
process. These checks establish no FreeBSD etcd feasibility or HFS recovery.

Read [lab operations](Lab-operations.md), [approval and automation](Test-automation.md),
[native topology](FreeBSD-test-lab.md), [replication ownership](Lab-replication.md),
and [fault primitives](Native-fault-runner.md) before preparing any later run.
The f2 received sink, production data/credentials/consumers, Rocky/9p and physical
host faults are outside this profile. Every actual run requires fresh explicit
approval of the exact complete plan and disruptive lifecycle window.

## Plan and offline preparation

`Plan.PrivateEtcd` is optional and Stage 1 only. Its complete typed inputs enter
the existing canonical plan hash and approval boundary. It pins the separate
controller driver, native helper and `pkg-static` artifacts, helper/pkg-static
byte sizes, every package archive's exact version/bytes/SHA256 and dependency
graph, extracted etcd/etcdctl digests, FreeBSD `15.1-RELEASE` and
`FreeBSD:15:amd64`, per-process limits, deadlines, and the explicit
`term-then-kill-owned-child-v1` stop policy. No package version or checksum is
invented by this implementation. A real offline bundle must be supplied before
the plan can be reviewed.

Create a fresh identity with `NewPrivateEtcdIdentity`. UUIDs beginning
`68667374-` (the `hfst` marker plus 96 random bits) are reserved for this lab and
must never be admitted into production. The token includes the run ID and UUID.
No production key namespace or trust root is accepted. Even etcd's mandatory
reserved bootstrap user `root` has a fresh test CA/certificate; its credential
stays on the controller, separate from the HFS operator/controller/daemon roles.

The three members use only `hfs-test0`/`.11`, `hfs-test1`/`.12`, and
`hfs-test2`/`.13` on `172.31.253.0/24`, with distinct run/member names. Client
and peer ports are **32379** and **32380**. HFS replication reserves **32443**.
Standard etcd 2379/2380, SSH, production and physical-host control remain outside
fault recipes. Existing native PF enrollment still requires exact run anchors,
labels, peer/port/direction and exclusive pipes; the port choices do not relax
those checks or authorize root PF changes.

Each guest requires separate explicit directory resources:

- `/var/tmp/hfs-run-RUN/input`: bounded transferred immutable artifacts.
- `/var/tmp/hfs-run-RUN/etcd`: bounded package rootfs, test TLS, control, logs and
  member data. Data is `/var/tmp/hfs-run-RUN/etcd/data` on the guest's independent
  UFS OS disk. It never bootstraps from an HFS backing/mount dataset.

Bootstrap runs on a temporary **f3-host controller**, which can reach the
otherwise unrouted guest subnet. `PrivateEtcd.Controller` explicitly pins
`target:"f3"`, the exact observed `hostname` (`f3` or `f3.lan.buetow.org`),
`interface:"vm-hfslab"`, `source_address:"172.31.253.1"`, and one resource name.
The sole admitted f3 resource is a bounded mode-0700 directory
`/var/tmp/hfs-run-RUN/etcd-controller`, containing `hfs-lab`, `pki/`, and its
durable controller enrollment. Its reservation includes the native helper,
PKI/request bytes and metadata (at least helper bytes plus 2 MiB, at most
128 MiB). This grants no authority over another f3 path, dataset, service or VM.
The five-VM inventory remains unchanged. The bridge/interface/address are from
[the existing preparation inventory](FreeBSD-lab-preparation.md); their presence
must be revalidated during the separately approved run.

On a separately authorized FreeBSD 15.1 amd64 package builder, obtain the exact
`coreos-etcd36` package and complete dependency closure, plus the matching
offline `pkg-static` executable. That future preparation may use package
repositories; guest staging never does. Record repository/snapshot provenance,
versions and all artifact hashes in the plan. Do not reuse the older Python PoC
archive or silently substitute a package from the workstation/host ABI. No
package refresh, fetch or installation is performed by the planning commands.

`VerifyEtcdBundle` checks all local pins and package sizes and inspects the Zstd
package tar streams before approval. Compact/full manifests must agree with
the exact package/ABI/dependency-version pins. Traversal, symlinks, hardlinks,
special files, set-ID modes, duplicate paths and payloads outside `usr/local`
are rejected. Compressed packages and the aggregate extracted archive payload
each consume at most a quarter of the member's directory reservation. The
guest repeats these checks after transfer. Dependency archives retain their
normal `NAME-VERSION.pkg` names for offline dependency resolution.

The strict no-link policy may reject real library packages. It is deliberately
fail closed; no supported package-library relocation is claimed. ELF admission
allows static FreeBSD amd64 binaries or only the FreeBSD base loader with
`libc.so.7`, `libthr.so.3` and `libm.so.5`; other imported libraries, RPATH and
RUNPATH are rejected. The actual bundle's loader requirements and native startup
remain feasibility gates.
Do not bypass these checks or add repository/host-library fallback to make a
bundle install. Guest installation uses the pinned `pkg-static`, an isolated
`-r .../rootfs`, empty config/repository directory, disabled plugins/rc handling,
and `add --no-scripts` against exact local files. It does not update the global
guest package database, run package scripts, enable an rc service or bootstrap
`/usr/sbin/pkg`.

The controller-only commands are:

```text
hfs-lab private-etcd-plan --input COMPLETE_PLAN.json --out NEW_REVIEW.json
hfs-lab private-etcd-pki --input COMPLETE_PLAN.json --out NEW_PRIVATE_DIRECTORY
hfs-lab private-etcd-request --plan PLAN.json --approval OPERATOR_APPROVAL.json \
  --step EXACT_STEP_ID --out NEW_REQUEST.json
hfs-lab private-etcd-request --plan PLAN.json --approval OPERATOR_APPROVAL.json \
  --step etcd-rbac --tls WORKSTATION_PRIVATE_PKI_DIRECTORY --out NEW_RBAC_REQUEST.json
```

`tools/lab/private-etcd-plan.sh` additionally pins the workstation executable
checksum and only admits these offline commands. It never approves a plan.
The reviewed source plan must already contain its complete lifecycle and
`PrivateEtcdSteps` schedule; review output is explanatory and does not replace
the canonical plan/approval. The review operations are a catalog; the actual
ordered schedule is `PrivateEtcdSteps`, with one controller-wide bootstrap.
PKI output is private material, never evidence to
commit or attach publicly. Its directory is new mode 0700; files are mode 0600.

## Concrete helpers and ownership

Insert `PrivateEtcdSteps` setup after the existing approved lab startup, followed
by the selected test drivers, collection, private teardown, normal lab shutdown
and required restoration. The exact schedule stages all three guests, installs
their separate TLS material, launches their foreground supervisors, and then
performs controller RBAC bootstrap. Each mutation has paired idempotent teardown.
Changing targets, artifacts, steps, disruption scope or budgets changes the
plan hash and requires a new approval.

The existing pinned-driver boundary supplies transport through f3 with the
pinned SSH host keys, creates the real private run/input directories, transfers
only exact verified inputs, keeps the foreground serve streams alive, and
persists receipts/evidence outside the guests. This change supplies no SSH
transport or background remote service installer. Neither a shell exit status
nor a lost transport proves an operation is terminal. `RunEtcdStep` requires
the exact bounded plan/step/target/timestamp receipt and treats missing/foreign,
late or nonterminal receipts as uncertain.

For the `etcd-rbac` step, whose exact target is **f3**, workstation preparation
verifies original artifact bytes and hashes the exact controller-only PKI file
set into the request. These are controller-generated request bindings under the
approved plan, not newly operator-approved hashes. The pinned SSH transport
stages the native helper at `etcd-controller/hfs-lab`, only the requested client
PKI files at `etcd-controller/pki/`, and the request on f3. No root/client key is
sent to a guest; selected-member files remain separate. The workstation does
not dial the private guest IPs directly, and no SSH forwarding, host routing or
firewall change is assumed or installed by this helper.

On an approved exact guest the driver invokes:

```text
STAGED_HFS_LAB private-etcd-native --input REQUEST.json
STAGED_HFS_LAB private-etcd-native --input TLS_REQUEST.json --tls SELECTED_MEMBER.json
```

The operation is selected solely by the approved step contract, not arbitrary
argv. The helper verifies its own staged executable digest/byte size, exact
guest hostname, native amd64 FreeBSD 15.1 kernel/userland, UFS placement, private
real paths and durable run/plan/cluster/device/inode enrollment. A run-local
lifecycle lock prevents competing stage/launch/teardown actions. Partial setup
leaves an outstanding intent and preserves everything for inspection; there is
no blind retry or automatic reconciliation CLI.
Guest hostname admission requires exactly the prepared `hfs-testN.test` name;
bare aliases or arbitrary suffixes are not adopted.

PKI has fresh independent peer and client CAs, separate member peer/server keys,
IP SANs for only that member, and EKUs for their precise uses. CA signing keys
are discarded after generation and never transferred. Selected-member files
contain no other member or controller client keys. The controller retains
separate certificate identities for all three daemon users, the controller,
the operator and the bootstrap administrator. Certificates last at most 24
hours. No production credentials or system-CA fallback are read.

The `serve` helper owns one actual foreground etcd child for its whole lifetime.
It emits a startup receipt with the actual launch identity, then continues to
run until the approved stop/limit policy completes. The driver must retain that
stream. Startup acknowledges an enrolled child only; mTLS status and membership
are checked separately during bootstrap. `FaultSupervisor`'s explicit
`private-etcd` role has its own private process-control socket/profile and
rejects `BindWorkspace`. Its launch nonce is never an HFS workspace generation.
The HFS supervisor's existing readiness gate remains intact.

Signals use the mutex-protected, unreaped direct-child lifetime. The durable
TERM intent binds the exact launch; the owner allows two seconds for TERM,
then uses the same owned-child gate for the explicitly approved KILL escalation.
Admission closes before controls drain and Wait reaps. A PID read from a file,
`ps`, wildcard kill or detached replacement can never be adopted. A dead/lost
supervisor is uncertain; its child is never rediscovered by PID. A child stuck
in an uninterruptible kernel wait can prevent reap; preserve the owning helper
and report uncertainty rather than claiming completed shutdown.

Controller bootstrap is explicit, executed by the existing pinned transport
**on f3**, never directly by the workstation:

```text
STAGED_F3_HFS_LAB private-etcd-bootstrap --input RBAC_REQUEST.json \
  --tls /var/tmp/hfs-run-RUN/etcd-controller/pki
```

Before any connection, it requires native FreeBSD amd64, the exact approved f3
hostname, an up `vm-hfslab` with `172.31.253.1/24`, its own staged helper
digest/size, private bounded resource paths, and every exact request-bound PKI
file hash/size. It enrolls the observed directory device/inode and all those
bindings durably. Missing placement/route, a workstation/guest host, foreign
files or a duplicate enrollment reject before the RPC seam. Remote validation
never tries to open workstation artifact paths. The gRPC dialer binds
`172.31.253.1` and only admits the three literal private client endpoints on
32379; connection failure does not fall back to another source or endpoint.
The exact f3 receipt includes the resource/host/route/helper/PKI enrollment.

Bootstrap then checks the exact three voter names/addresses/URLs and matching actual etcd
cluster ID, requires fresh auth-disabled state, creates certificate-CN users,
grants precisely `coord.Permissions`, enables auth last, checks exact resulting
ranges, and tests permitted policy reads plus denied foreign-namespace reads.
Partial provisioning stops without retries. HFS roles receive no root role.
The root and role client credentials remain in the enrolled private f3
controller directory, with the workstation source retained separately;
credential retirement/rotation is a separate approved operation. It is not
silently revoked or distributed to workloads. No per-operation HFS I/O or
serving authority is inferred from this bootstrap.

## Resource and evidence limits

Only the dedicated native CLI helper applies inherited hard CPU-time,
virtual-address-space, per-file-size and descriptor rlimits. It does not alter
the controller or unit-test process. The CPU soft/hard seconds are distinct
explicit plan inputs; 5–10 CPU seconds of hard-limit headroom let SIGXCPU route
the owning helper through child cleanup before its hard limit. These limits
are per process, not an
aggregate guest reservation. `GOMAXPROCS=1` bounds the Go scheduler; the 128 MiB
`GOMEMLIMIT` is a soft heap target. `RLIMIT_AS` bounds virtual address space,
not resident memory. Go/etcd may reserve over 1 GiB at startup and fail with
low accepted AS limits. These settings have not been calibrated on the guests;
native resource feasibility is an explicit later approved gate.

The child log has an exact byte cap. The aggregate run-root guard scans every
250 ms, with a one-second/65,536-entry scan bound, and stops the owned child on
a scan/deadline/disk-budget failure using the approved TERM/KILL policy. Files
are retained. **This is not a hard aggregate UFS quota.** Scan time and the
stop grace permit byte overshoot; a native write-rate/overshoot bound is not
established. Capacity headroom must account for the input copies, isolated
rootfs/package database, WAL/snapshots, logs, and that uncalibrated overshoot.
`RLIMIT_FSIZE` is per file. Do not describe a passing monitor as proof of a hard
aggregate cap, guest RSS cap or three-guest capacity admission. Existing host
RAM/Rocky admission remains a separate preflight requirement.

Guest collection implements bounded enrollment, current directory bytes,
private supervisor status/explicit observation error, bounded daemon log and
conservative remaining-resource inventory. It reads no TLS contents. It does
not claim to collect etcd quorum/auth health; the controller's mTLS bootstrap
checks are separate and need their own persisted receipt. The transport driver
must retain all infrastructure receipts and version/hash provenance outside
the guests. Unavailable status is recorded as unavailable, never healthy.

Teardown stops and reaps only the positively enrolled process, then removes
only its individually checked private member key files. All package, data,
rootfs and evidence directories remain for inspection/retention, including
successful runs. No parent directory, VM disk, dataset, production service or
received sink is removed. Data removal after bounded retention requires a
separate reviewed cleanup action; it is not an unimplemented wildcard delete
hidden behind this teardown command. Failed/uncertain resources always survive.
The bounded f3 controller directory and its receipt/enrollment are also retained
for explicit credential retirement and retention cleanup; guest teardown never
deletes or adopts a host path implicitly.

## R/F definitions and acceptance boundary

`NativeScenarios` defines every R01–R10 and F01–F05 row from the
[integration matrix](Integration-tests.md), role rotations and explicit
fault boundaries/assertions. F03 splits authority-only 32379/32380 partitions
from replication-only 32443 partitions. F02/F04 preserve blocked promotion when
positive fencing cannot complete; pause duration and etcd TTL are never proof.
R08 requires genuinely disposable disk-loss support; F05 explicitly requires a
fourth-node profile and is unsupported by these three prepared guests.

`PrepareNativeScenario` requires actual capability observations from the pinned
candidate binary on each of the three guests. A missing probe, missing feature
or foreign binary digest returns unsupported. Even a complete capability report
remains blocked by **fq3 checked commit-model evidence**, **zq3 real-daemon
native compatibility evidence**, and the unimplemented normal replication,
fencing/takeover/recovery engines. Task `4r3` retains these prerequisites.
There is no scenario pass-producing executor in this change. Command exit 0,
private-etcd readiness and fake receipts never establish replicated history,
fencing or recovery success. Future drivers must persist real external client
histories and evaluate them with the independent oracle plus native assertions.
