# Controlled guest admission artifacts

Task `s04` fixes this local provisioning contract. It changes no network wire,
normal publication or distributed authority. Implementation and deployment
status must be reported separately; this contract is not runtime readiness.

The controlled provider is explicitly selected, default off, built into the
complete custom kernel with built-in 9p and virtio-9p, SYSFS and SYSCTL. Its
operator Ed25519 public key is pinned in the admitted kernel configuration.
An empty or invalid key refuses admission. GCC-specific wrapper compilation,
including `-fwhole-program`, is part of the pinned build inputs; other toolchains
are not implicitly supported.

## Provisioning

The root initial-user-namespace administrator writes one complete bounded
artifact to `provision_v1`. Fragmented assembly, replacement and reset are not
supported. The canonical JSON wrapper has exactly:

```
{payload,operator_signature,group_signatures}
```

Its canonical payload has exactly:

```
{version:"homelabfs-guest-provision/1",boot_id,provider_nonce,groups,budget}
```

`boot_id` is the actual kernel random/boot_id, obtained from its shared kernel
getter. `provider_nonce` is generated once by this boot-owned provider and
cannot be restored or replaced. All UUIDs are nonzero lowercase standard UUIDs.
There are one through64 ordered groups. Each group has exactly:

```
{enrollment_bytes,enrollment_operator_signature,
 enrollment_domain_signatures,local_mounts}
```

`enrollment_bytes` is lowercase hexadecimal of the **original complete** Go
`json.Marshal(viewcontrol.Enrollment)` bytes. Preserve their actual struct field
order and every original domain, including remote guests. Do not substitute a
canonical reserialization. A private bounded enrollment parser accepts the exact
known Go representation, refuses duplicate/unknown fields and alternative field
order, and does not weaken the canonical network parser. Existing embedded
`[]byte` public keys retain canonical standard base64 JSON encoding and decode
to exactly32 bytes. Original signatures are exactly64 bytes encoded in lowercase
hexadecimal. The original operator signature and ordered array of `{host,guest}`
domain signatures verify the exact original enrollment bytes against the pinned
operator key and each corresponding original domain's host and guest keys.

The admitted local `Profile.Name` is1..255 printable ASCII bytes excluding
`<`, `>` and `&`. Quote/backslash retain their exact Go JSON escaping. Unicode,
control and HTML-containing profile names refuse this local implementation;
the canonical network tokenizer remains unchanged.

The outer operator signature verifies the complete canonical **payload** bytes.
`group_signatures` aligns one-for-one with groups. Every entry is an array of
`{host,guest}` signatures aligned exactly with **all** original group Domains,
including remote domains. Each signs that same complete canonical payload.
Missing, extra or reordered signatures refuse installation.

`local_mounts` contains exactly the original domain indices whose signed
`Scope.guest_boot` matches this actual boot, in strictly increasing index order.
Each group contributes at least one local domain. Every local row must share
this kernel's signed VM identity; remote domains are preserved and verified but
are never mounted or physically attested by this kernel. A mount row has exactly:

```
{domain_index,session,device_tag,device_name,user_namespace,idmap_hash,
 realm,subject,anchor,nonce,view_control_nonce}
```

The session equals its indexed complete scope. Device names/tags are1..255
ASCII bytes without slash, with no truncation. Subject is lowercase hexadecimal
encoding1..1024 bytes. Namespace UUID is a signed protocol identity; the kernel
independently captures and checks actual `init_user_ns`, its uid/gid map, mount
superblock and live client/virtio device. Other namespaces/maps refuse.

Group enrollment IDs are unique. Local sessions, device names and device tags
are globally unique, with no ambiguous reuse. Total local rows cannot exceed64.
Ordinary principal family fields remain separate from the exact signed
acquisition/control anchor+nonce lanes in `TicketFamilies`. Distinctness concerns
the full lane identities; anchors need not individually differ. The signed
`view_control_nonce` must equal its exact control lane nonce. Internal1.4 fields
retain both view anchors and the acquisition nonce rather than relabeling the
ordinary family. Closed older protocol bytes and behavior remain unchanged.

Budget has exactly `{ordinary_bytes,control_bytes,reservations,cookies,received}`.
Each byte capacity is1..1GiB and their checked sum is at most1GiB. Reservation
slots are1..8192; cookie and received-owner slots are each1..4096. No zero,
overflow, duplicate fields or defaults are accepted. Charge actual bookkeeping
or explicitly bounded fixed storage. Ordinary resources never borrow control.
These caps establish finite component ownership, not C7 funded-progress or full
4MiB canary acceptance.

Both the complete provision and the worst-case complete observer serialization
must fit128KiB. Before installing any state, preflight all domains with full
non-LIVE phase/hash fields and maximum-width counters. Refuse overflow rather
than admit an inventory that cannot later publish STOP evidence. Count maxima
do not promise that every maximum-size enrollment fits. Verify all signatures,
shape, capacities and index mappings before installation. Once installed, local
identities remain present despite subsequent failures or uncertainty.

## Retained allocation accounting

Before publishing the registry, C6 computes an immutable `control_base_bytes`.
It includes each retained registry/mount/enrollment structure, each owned JSON
structure, raw bytes including its terminator and compacted token array, all
four128KiB snapshot packages, one128KiB+1 artifact input buffer, accepted-open
bookkeeping and a permanent8MiB accepted-operation staging reserve. If that base
exhausts the signed control budget, installation refuses transactionally. Every
later control reservation includes the base when checking its limit; unreserve
cannot return base ownership. Ordinary allocations cannot borrow it.

The staging calculation conservatively permits two overlapping parser packages,
each containing a JSON structure,128KiB+1 raw bytes and two initial/compacted token
arenas of `(128KiB/2+1)*sizeof(hfs_token)`, plus three128KiB work buffers and128
extra canonical-profile bytes. It is under5MiB of requested allocation bytes on
the pinned target; an actual compile-time assertion requires it to fit the8MiB
reserve. Retained group/registry allocations are counted separately. The accepted
open allowance is8192bytes per observer/artifact open; a compile-time assertion
covers the actual kernfs-open and seq-file structures. A separate4096byte fixed
allowance covers provider/observer globals and their small boot bookkeeping.
These are requested-allocation accounting bounds and conservative reserves,
not measurements or guarantees of kernel RSS, allocator slab residency or
physically prefunded progress. C7 and deployment sizing remain separate work.

Provision and bind share one accepted administrative open slot. Their existing
kernfs `prealloc=true` buffer and `prealloc_mutex` serialize same-fd input copying
and the callback; `of->mutex` alone starts too late to bound concurrent copies.
The underlying Linux6.1 kernfs allocates generic open/prealloc/seq-file objects
before `ops.open`. Rejected/in-flight pre-admission VFS/kernfs syscall allocations
and allocator rounding are outside the signed retained/live-provider budget.
This contract does not claim a global bound on privileged root syscalls.

The unchanged reviewed214 example artifact signs only1MiB of control budget and
therefore refuses this implementation's staging base. Tests preserve its bytes
and hash as an insufficient-budget negative and derive a separately signed16MiB
positive, preserving every original enrollment byte and original signature.

## One-time controller binding

One group is bound by a separate root-only complete write of this canonical
wrapper:

```
{payload,operator_signature,domain_signatures}
```

Payload has exactly:

```
{version:"homelabfs-guest-bind/1",boot_id,provider_nonce,
 provision_hash,group_id,controller}
```

`provision_hash` hashes the installed complete canonical provision payload.
`group_id` is the existing enrollment.ID. Domain signatures cover every original
domain of that group, in its original order, using the same hex encodings and
exact cardinality rules. Operator and domain keys sign the canonical bind payload.
Endpoint, serving-generation and key tuples remain immutable from provision.
Bind a group successfully exactly once. Replays, replacement and controllers
reused across distinct groups initially refuse.

## Bootstrap and retained ownership

1. Install complete signed local inventories with no source or workload rights.
2. C5 observes that inventory and the host constructs the actual controller.
3. Install the corresponding signed group binding.
4. Enroll an exact device tag once, then create the actual 9p client.
5. Bind the real in-use virtio channel/client/device and copied bounded name/tag.
6. Permit only fixed typed HELLO before superblock binding. HELLO grants no
   source, capability, map or ordinary-allocation authority.
7. `hfs_fill_super` captures the actual superblock pointer, `s_dev` and namespace
   before credential capture, ALLOCATE and ROOT. Ordinary, capability and source
   use requires the full signed group, controller, client/device and sb binding.

The transport getter checks live channel membership and the actual in-use
client/trans/vdev relationship under its existing lock. It retains a device
reference and copies names/tags while protected; no channel-owned string escapes.
A device reference pins identity, not continued transport liveness. Admission and
received verification recheck that actual lifetime. Caller JSON, booleans, parsed
receipts, hashes and diagnostic transported flags cannot establish it.

Prepared/unbound sessions refuse source access. Failed or uncertain ownership
retains consumed identities and resources. Source-byte verification remains the
real host C5 journal/catalog/payload verifier. The kernel additionally requires
private actual received provenance, exact original request/response/target and
physical-state validation. It cannot infer those duties from signature success.
The read-only SOURCE verifier additionally checks the exact privately selected
lease/reservation/entry span and strict capture revision/phase at consumption.
Initial leases consume it before physical binding; copied historical bytes do
not remain fresh grants after a phase change. FROZEN adoption retains the actual
received entry after exact expected-byte comparison, and rechecks the quiescent
physical capture under the view lock. No parsed expected entry creates authority.
Existing `json->owner` continues to own request lifetime, independently of private
received provenance and exact descendant spans.

The observer freezes one complete immutable snapshot per open across chunked
reads/seeks, with four128KiB slots and exact EOF. It preserves all original local
stopped/uncertain domains and the exact C5 phases/event/ACK-payload hashes. Publish
a phase only after actual STOP/adoption/PTE/source duties and before its ACK.
There is no boot-local reset/unload/reconstruction interface. C6 conservative
retention releases no uncertain ownership; C7 must implement protected retirement,
gaps, floors and session release before those operations can succeed.

## Cryptographic boundary

The owned wrapper uses unchanged pinned Monocypher4.0.3 SHA512 Ed25519 verification.
It requires canonical prime-subgroup A and R and rejects identity A. Identity R
requires the real signature equation to verify. This deliberately refuses mixed
order inputs accepted by more permissive verifiers. It exposes no signing or
encryption API. Generic local-artifact messages are bounded to128KiB; V2 receipt
unsigned messages retain their separate16KiB bound. Cryptographic primitive and
compile-only evidence creates no provider, source or physical authority.
