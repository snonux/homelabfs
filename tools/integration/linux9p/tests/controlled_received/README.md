# C6 component harness IPC

This harness runs actual owned guest provider/transport/receipt/physical helpers
with the actual owned C carrier and real Go Transport/Producer/controller/source.
Kernel credentials, virtio devices, sysfs, native FUSE mount/headers and MM/page
primitives are explicit substituted OS boundaries. No installed kernel, native
PTE/TLB execution, deployment, C7 retirement or runtime readiness is established.

The host builds the consumer after constructing its real fixture:

```
python3 build.py --source OWNED_LINUX --output FRESH_DIRECTORY --operator-key HEX32
```

The executable is `FRESH_DIRECTORY/consumer`. The public key is compiled into
the actual provider configuration; there is no runtime public-key setter.
`HFS_C6_LEDGER` names an exclusively created private directory for the actual C
carrier ledger. The enclosing runner supplies CPU/RSS/disk/swap/time guards.

The first stdin line is canonical JSON with exactly
`{base,binds,credentials,os,provision}`. `base` is the actual host native-cell
highwater. `binds` and `provision` are the reviewed signed artifact bytes as JSON
objects. `credentials` contains one complete original Credentials per local
mount in registry order. `os` is the explicitly substituted OS boundary with
`{boot_id,provider_nonce}`; it is independent of the artifact fields so signed
wrong-boot/nonce artifacts cannot redefine that boundary. The consumer returns
`READY` after actual registration, provisioning/binding, enrolled client/device,
exact HELLO and superblock binding. Only HELLO may emit bytes before sb binding.

All later host commands have an integer ID. Responses are
`D ID ERRNO BODYHEX`, using `-` for no body. Errnos are negative Linux values.

* `RUN ID SESSION RESERVED OP PAYLOADHEX` calls actual `hfs_control` and keeps
  that session's latest actual privately received response. OP is a fixed
  protocol operation, not an interpreted shell command.
* `ADOPT_ROOT ID SESSION` consumes only the latest actual QUERY TERMINAL result,
  runs the actual ROOT result/capability validation, retains its real ROOT and
  DOMAIN capabilities and creates the actual empty unbound physical view.
  Response is the actual DOMAIN capability JSON.
* `ADOPT_DESCRIPTION ID SESSION OPCODE` validates the latest actual QUERY result
  for CREATE or OPEN and retains its real DESCRIPTION capability. It cannot
  install a capability supplied in the command. Response is that capability.
* `ACQUIRE ID SESSION` calls actual `hfs_lease_acquire` with the captured original
  credential and DOMAIN access, then `hfs_view_bind_initial`. It verifies the exact private SOURCE before binding, rejects detached/wrong
  spans and stale capture after initial binding, and retains the actual
  lease/receipt owner. Response is the actual Lease JSON.
* `RELEASE_START ID SESSION` activates the lease's actual preowned control ticket
  and sends its exact VIEW_RELEASE once through actual `hfs_control`, retaining
  its original bytes and response. It does not fabricate RELEASED or retirement.
* `PHASE ID SESSION PHASE` consumes the latest actual privately received EVENT.
  STOPPED calls actual `hfs_view_stop` and `hfs_view_seal`; INSTALLED_DRAIN and
  INSTALLED_FINAL call actual `hfs_mmap_install`; DRAINED calls actual
  `hfs_mmap_finish_drained`; REOPENED calls actual `hfs_view_reopen`. Each then
  uses actual `hfs_mmap_ack`, including before/after provider callbacks. No
  command sets a physical phase, revision or ACK hash directly. Response has no
  body; the independently emitted observation supplies the resulting record.
* `CHECK ID SESSION CASE` exercises the actual stored-object callbacks. Cases
  are `target`, `event`, `publication`, `cap`, `cookie_balance`,
  `forged_target`, `forged_event`, `forged_publication`, `bad_device`, `bad_map`,
  `retirement_refuses`, `nonnull_publication`, `wrong_channel`, `stale_target`. Each CHECK returns0 only after asserting the intended
  acceptance or refusal; unexpected acceptance returns a failure. Forged cases parse detached copies, even with public
  diagnostic flags set, and must receive no private authority. `cap` uses the
  retained DESCRIPTION when available and otherwise the actual ROOT grant.
* `END` closes the bounded fixture process without claiming protected retirement.

Native I/O is the existing mapped-test protocol: `W OFFSET FRAMEHEX` requests a
real host Transport write and receives `OK N` or `ERROR`; `R FD OFFSET COUNT`
receives `DATA HEX` or `AGAIN`. FD11 selects result, FD12 recall.

Before native I/O the consumer emits `S ADMISSIONHEX` from the actual C snapshot
serializer. The host replaces only the existing observer fixture's complete
Admission record with those bytes, preserving its independently measured static
artifact fields. It never calls `sourceRig.observeACK` or synthesizes observations.

When actual transport waits on a real backend EAGAIN, its OS sleep primitive
emits `Y OUTER_ID`. The host replies `CONTINUE`, or issues nested commands for the
other session before CONTINUE. The backend has returned and released locks
before this scheduling boundary. Nested responses use their own IDs. This permits
real RELEASE/physical progress during a pending WAIT without fabricated results.
An initial WAIT can let the actual backend deadline elapse and return EMPTY; the
next WAIT reclaims that real control reply, retaining its nonnull original Ack
across actual internal polls until the real controller EVENT arrives.

The startup also exercises exact control-budget boundary arithmetic through the
actual reserve/unreserve callbacks: the permanently charged admission base cannot
be spent, refusal leaves counters unchanged, and ordinary capacity cannot borrow
control. This checks finite accounting, not C7 physically funded progress.
A disposable child forces the first actual HELLO allocation to fail after real
transport binding, then executes the actual mount failure decision and proves the
consumed session/client/device remain retained. The parent runs its independent
successful fixture; the failed child emits no native command.

`policy_run.py` covers the inert complete ACQUIRE/RENEW/RESERVE/FROZEN policy with
four positives and34 semantic/schema/original-byte negatives. Containing hashes
are recomputed so the intended comparisons are exercised. These unsigned codec
vectors never call the private factory and prove no FROZEN receipt adoption or
funded dirty runtime. Actual DIRTY_RESERVE/FROZEN/nonempty drain and protected
closure remain C7/later acceptance.
