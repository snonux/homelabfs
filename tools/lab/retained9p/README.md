# Real retained-9p base gate preparation

These tools prepare `zq3` against the actual controlled daemon and admitted full
custom kernel/exporter. They do not reuse the loopback PoC. Current local drafts
have **no deployed acceptance result**. No installation, kernel boot, exporter,
VM, mount, device or live workload action is implied by their presence.

**The old xz3 candidate is quarantined.** Its CGO-disabled FreeBSD observer
returns Unsupported after exporter launch, so it cannot reach native READY.
The controller refuses its exact manifest before any SSH. See
[`quarantine.json`](quarantine.json). Tasks r24/s24 and reopened xz3 must provide
qualified corrected artifacts. Existing offline build proofs remain preserved;
neither a successful old metadata admission nor a different digest establishes
native eligibility. These preparation commands never grant deployment eligibility.

**The controller pins exactly one candidate:** the 2026-11-08 xz3 rebuild,
manifest `3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1`
(source `e0cca0c`, CGO daemon `53598737…`; see its
[evidence](../../integration/artifacts/evidence/xz3-20261108/README.md)). Any
other digest refuses before reading inputs or contacting the lab. This pin is a
dispatch precondition only; installed/booted/native acceptance remains zq3.

`guest.py` runs real Linux syscalls only after matching the actual boot/provider,
complete two-domain inventory, installed manifest digest, SELinux enforcing and
both exact `9P2000.L.hfs1.4,cache=mmap,msize=65536` mount IDs. It retains directory
descriptors and verifies independent `st_dev` domains. Every operation stays in
a newly exclusively created case directory. JSONL invocation/reply/errno evidence
leaves through stdout; a controller must preserve it outside the tested volume.

**Successful cases retain their payload** until host correlation is captured.
(The earlier probe deleted it immediately, so the host could never correlate
journal/source state against the files the guest had just written.) The case
records the exact payload inventory (names, types, inodes, ownership, modes,
sizes, links, leaf listing) and the SHA256 of a fresh 256-bit release token in
`/var/tmp/hfs-zq3-RUN/release-CASE.json`: exclusive, `0600`, fsynced, in the
root-private local run directory that holds both mount points and is checked to
be on neither tested device. The token itself appears only in the controller's
private stdout log. A separate `homelabfs-retained9p-release/1` request passes
the same full admission, then must present that token, the unchanged inventory
digest and the host correlation digest. Only then is the record consumed
(renamed to `released-CASE.json`, so a replay refuses) and the exact known
entries unlinked; the journal is preserved. A wrong token, changed inventory,
changed boot/provider, insecure run directory or existing record refuses and
preserves everything. Failed cases preserve their files and write no record.
One release before the next case keeps the full 4MiB logical canary free of
earlier payloads.

Implemented probe cases are credentials, sync, metadata, namespace refusal and
full mapped pressure. The namespace refusal case exercises an actual new user
namespace, and requires the operation to refuse. Failure to create a namespace
is not a pass. This establishes a refusal boundary only: **it does not satisfy
the original namespace/pod compatibility requirement**. The C6 implementation
currently supports the initial user namespace and its numeric idmap. Ordinary
mount/PID/network namespaces may share that user namespace; they need separate
actual fixtures. The accepted lock concession is only same-guest/same-mount
advisory locks, including bind-shared pods; use the existing actual
[`guest-lock-probe.py`](../guest-lock-probe.py), never independent-mount shared
lock assertions. Each guest case has at most60 seconds,256 recorded operations,
12MiB issued writes,128 descriptors and one concurrently active credential child.
The full mapped case dirties all4MiB; SIGBUS is a failure.

The guest terminal record always says `acceptance:false` and requires host
correlation. Syscall success cannot certify covering journal barriers, actual
source ownership, funded drain progress, PTE/TLB exclusion or recovery. Missing
correlations, pod cases or other required matrix entries keep `zq3` incomplete.

## Controller evidence and uncertain dispatch

`check-retained9p.sh check --plan ABS` validates an exact syscall batch and emits
its byte digest without contacting the lab. The batch then runs one case at a
time against already installed/admitted mounts:

1. `probe --plan ABS --sha256 SHA --output FRESH_ABS` creates the run and
   dispatches the first case, ending at `payload-retained-awaiting-correlation`.
2. Root captures host correlation for that case while its payload is present and
   writes a `homelabfs-retained9p-host-correlation/1` record: closed keys
   `version, run_id, case, case_stdout_sha256, inventory_sha256, host_observe,
   acceptance:false`, bound to the case's retained stdout and inventory digests;
   `host_observe` is the nonempty raw host evidence (for example the lifecycle
   endpoint OBSERVE reply). The controller binds, but does not interpret, it.
3. `release --output ABS --sha256 SHA --correlation ABS --correlation-sha256 SHA`
   checks that binding and the retained stdout digest, persists the record and
   dispatches the guest release.
4. `next --output ABS --sha256 SHA` dispatches the following case only after the
   previous payload is released. `syscall_batch_success` needs all five released.

`status --output ABS` reads retained state. Refusals before a dispatch change no
state; a failed or uncertain dispatch records `error`, after which every later
command refuses (inspect, never replay). Commands refuse 3600 seconds after the
run started, leaving any retained payload for inspection. Each reopen verifies
the owned lock, exact plan digest and every staged input snapshot. These commands
provide no VM lifecycle, package installation, mounting, fencing or normal
publication; the host correlator that produces step 2's record does not exist yet.
Root must complete the separately approved critical setup before a probe batch.
The batch's SSH executable, host/guest public pin files and probe bytes are pinned
and checked before dispatch. The guest connection goes through f3 with strict
explicit host/guest pin policy and no agent forwarding/control master/key update.

The controller reads the plan once: those exact raw bytes supply parsing,
validation, the reported digest and the durable `plan.json`. Verified SSH,
public-pin and probe bytes are retained from their checked reads. Execution uses
the retained probe bytes and fresh nonwritable snapshots under the private run's
`inputs` directory, including the nested ProxyCommand executable and host pins.
Original input path replacements cannot change a dispatched batch. Snapshot
storage is bounded to16MiB SSH +512KiB probe +2×64KiB public pins. Existing output
ancestors must exclude untrusted replacement; only root-owned sticky `/tmp` and
`/var/tmp` may be writable temporary ancestors. Administrative modification of
the owned private run directory during execution is outside this contract.

Output paths must be canonical absolute ASCII paths containing only
`[A-Za-z0-9_./-]`. Percent tokens, dollar/environment syntax, tildes, whitespace,
quotes, backslashes and all other characters refuse before staging or process
creation. This restriction prevents OpenSSH option/token expansion from routing
the outer or nested command to a different snapshot path. Ordinary hyphens,
underscores and dots are allowed; parent ownership checks still apply.

The controller fsyncs the plan and per-case dispatch intent before starting SSH,
stores separate bounded stdout/stderr outside the lab, and requires a real final
guest terminal plus a zero transport exit. A transport timeout kills only the
local SSH child and retains the dispatch as uncertain; it never kills the VM or
infers remote quiescence from SSH exit. It refuses output-directory reuse and
does not replay a dispatch automatically. Stream overflow makes required evidence
incomplete, rather than silently truncating a receipt. The report separately
states syscall-batch success and `zq3_acceptance_complete:false`, with explicit
remaining original gates. A complete syscall batch is useful evidence, never a
substitute for host correlation or the full gate.

The new output directory is synced together with its parent before dispatch.
Each case directory and its empty log entries are durable before child startup;
final log data and the case directory are synced before recording completed
state. A parent/log/case durability failure prevents dispatch or retains its
uncertain intent, rather than publishing durable completion without reachable
evidence. Ordering/failure regressions exercise these local filesystem boundaries.

Each canonical request is capped at16KiB including its newline, matching the
guest wire bound. The controller rejects oversize input before process creation
and sends stdin nonblocking under the same per-case selector deadline while
draining stdout/stderr. A stalled SSH handshake or child that never reads cannot
hold the controller in a synchronous pipe write beyond that deadline. Local
regressions use an actual4KiB pipe/non-reading child and pre-spawn oversize denial;
these exercise controller transport safety only, not guest filesystem acceptance.

## External staged signer

`cmd/hfs-retained9p-operator` runs on the external controller. It never executes
SSH, installs a sysfs artifact, initializes a volume or starts the runtime. Its
signatures are inert administrative consent; actual runtime/Facts/guest checks
remain mandatory. The xz3 candidate's operator key stays outside repository,
runtime and guest. Do not read or use that actual private key during preparation.
Test with fresh throwaway keys only.

The two commands are:

```
hfs-retained9p-operator enroll --request /private/enroll-request.json --output /private/new-enrollment
hfs-retained9p-operator bind --request /private/bind-request.json --output /private/new-binding
```

Requests and proposal/key files must be clean absolute, bounded, singly linked,
private regular files owned by the calling UID. Keys accept one unencrypted
Ed25519 PKCS#8 PEM, or exact lowercase hex seed/private key without a newline.
No key bytes appear in output. Fresh0700 output directories contain0600 files;
failure preserves partial outputs and reuse refuses. Original proposal struct
bytes and domain order remain exact. Only an empty GuestBoot may be filled from
the actual observed boot. Signed provision validation checks original keys,
complete aggregate and per-domain signatures, inventory, protocol and budgets.

`EnrollmentRequest` uses exported Go JSON field names:

```
{
  "Proposal": "/private/copied-actual-proposal.json",
  "OperatorKey": "/private/operator.pem",
  "BootID": "actual-observed-guest-boot-UUID",
  "ProviderNonce": "actual-observed-provider-UUID",
  "Keys": [
    {"Session":"C0-UUID","Host":"/private/host0.pem","Guest":"/private/guest0.pem"},
    {"Session":"C1-UUID","Host":"/private/host1.pem","Guest":"/private/guest1.pem"}
  ],
  "Mounts": ["two exact controlledruntime.GuestArtifactMount records"],
  "Budget": "exact controlledruntime.GuestArtifactBudget",
  "Observer": "exact controlledruntime.GuestObserver"
}
```

The quoted record descriptions above are schema explanations, not runnable
request values. Populate actual exported structs and validate locally before
an approved bootstrap. The signer emits `enrollment.json` (the real
RuntimeEnrollment handoff) and `provision.json` (one complete canonical sysfs
write). Install the guest provision first, then atomically transfer enrollment
to the private host EnrollmentFile. At `AWAITING_GUEST_BIND`, the bind request
names that exact provision, original group ID, actual emitted controller and
same ordered key files. It emits `bind.json` for one complete guest sysfs write.
Neither file is a native readiness receipt.

## Required work before an executable exact plan

Read [the concrete gap and installation assessment](../../../docs/lab/Real-daemon-9p-base-gate.md).
The immutable195-file xz3 candidate remains unchanged. New operational helpers
need separately pinned source, executable and verification records. If the
runtime needs new control APIs, rebuild/reseal only under a separate explicit
artifact revision and retain the existing candidate/evidence unchanged.
