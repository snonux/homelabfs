# Controlled guest component harness IPC

This harness runs actual owned guest provider/transport/receipt/physical helpers
with the actual owned C carrier and real Go Transport/Producer/controller/source.
Kernel credentials, virtio devices, sysfs, native FUSE mount/headers and MM/page
primitives are explicit substituted OS boundaries. No installed kernel, native
PTE/TLB execution, deployment, positive terminal session handoff or runtime
readiness is established. Component passes apply only to their recorded input
hashes; current C7 acceptance is tracked separately from C6 evidence.

The host builds the consumer after constructing its real fixture:

```
python3 build.py --source OWNED_LINUX --output FRESH_DIRECTORY --operator-key HEX32
```

The executable is `FRESH_DIRECTORY/consumer`. The public key is compiled into
the actual provider configuration; there is no runtime public-key setter.
`HFS_C6_LEDGER` names an exclusively created private directory for the actual C
carrier ledger. The enclosing runner supplies CPU/RSS/allocated-disk/available-memory/time guards. Swap usage and page-out
counters are recorded observations; they do not admit or stop a stage.

The first stdin line is canonical JSON with exactly
`{base,binds,credentials,os,provision}`. `base` is the actual host native-cell
highwater. `binds` and `provision` are the reviewed signed artifact bytes as JSON
objects. `credentials` contains one complete original Credentials per local
mount in registry order. `os` is the explicitly substituted OS boundary with
`{boot_id,provider_nonce,capture_tuples}`; the last field contains one or two
predeclared complete credential-capture tuples, with the first equal to each
mount's initial credentials. It is independent of the artifact fields so signed
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
* `END` closes the bounded fixture process without claiming terminal session
  retirement.

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
funded dirty runtime. The C7 commands below separately exercise actual DIRTY_RESERVE/FROZEN/nonempty
drain and protected claim/view floors. Positive terminal closure remains614.

C7 adds these test-only commands, all invoking the maintained production helpers:

* `REQUEST ID SESSION OPCODE ARGSHEX` calls actual `hfs_call`, including protected
  pre-send allocation. Returned real result references remain held until explicit
  disposal; the helper does not invent request state.
* `ADOPT_ROOT_PREPARE` validates/adopts the real ROOT/DOMAIN grants before a view
  exists; `VIEW_NEW` later calls actual `hfs_view_new_unbound`. Both refuse an
  existing view/root. This permits disposable file preparation before physical
  rights, without relaxing `hfs_call`'s UNBOUND snapshot refusal. `ADOPT_RESULT`
  validates and retains additional real ROOT/DOMAIN grants for issuer tests.
* `ISSUER ID SESSION INDEX` selects only a startup-declared capture tuple. The
  host constructor independently freezes that finite exact credential allowlist
  at its explicit native-attestation primitive boundary. Runtime JSON cannot
  add a tuple or change production verification.
* `DIRTY` obtains a real exclusive page lease, prepares/attaches a primitive page,
  obtains actual DIRTY_RESERVE and adopts its complete preowned obligation.
  STOPPED runs actual freeze and manifest sealing. `PIN` changes only the
  explicitly substituted DMA-pin primitive. `DRAIN` invokes actual dirty drain;
  later phases use real cache invalidation and closed-drain cleanup helpers.
* `PRESSURE ... COUNT` holds real registered-provider admissibility cookies;
  count0 fills ordinary reservation slots. Two sessions contribute pressure.
  `ORDINARY_REFUSES` requires actual `hfs_call` refusal without number/cell
  issuance. `UNPRESSURE` disposes only those exact cookie owners.
* `CLOSE_PENDING` invokes actual provider STOP/retain while dirty pins, results
  and cookies remain. It asserts refusal preserves every owner; subsequent
  funded controls must use the typed closing path. It is no native unmount.
* `RELEASE_DONE`, `RETIRE` and `VIEW_RETIRE` run the actual full release and
  retirement state machines. `DROP_RESULTS` disposes held result references.
  The original session0 floor/retention-before-session1-release ordering is
  preserved as the saved-release continuation regression.
* `READDIR_CLEAN` invokes existing parent-funded cleanup while closing, including
  refusal of a detached generic control request.
* `FAULT ... OP MODE` substitutes one transport error: mode1 rejects the actual
  selected SEND before forwarding; mode2 loses a real fetched reply after the
  actual carrier/host protected it. Mode3 is limited to RETIRE/VIEW_RETIRE:
  after fetching the real reply it fails the next comparison scratch allocation
  of that operation's actual size. The complete reply and exact old cut remain
  owned while a later real request or acquisition arrives. Deadlines are unchanged.
  Lost candidates and their exact credits remain in the actual uncertain list.
* `LATE_BORROW` schedules a real production result borrow at the mutex primitive
  boundary after the actual provider accepted its floor. The first reap refuses
  that live owner; freeing the result permits exact old-cut revalidation at the
  already accepted floor. It does not fabricate a proof or set lifecycle flags.
* `RESOLVE_GAP` invokes the actual mandatory provider. `GAP_NEGATIVES` and
  `FLOOR_NEGATIVES` exercise wrong original bytes/type/credentials/proof and
  detached-object refusal against real held replies. Hash decoding alone is
  explicitly distinguished from authority. `STATE` is read-only diagnostics.
* `RELEASE_NEGATIVES` uses the actual known cell/hash to force a real ACK JSON
  allocation failure after carrier funding and an oversized closed ACK refusal.
  Neither may change cells, send bytes or retain a reservation.
* `RETAIN_VERIFY` checks that final known/unknown cells, device, session, credits
  and ingress fencing remain owned when terminal retirement refuses.

The funded dirty case uses one actual4096-byte page, a nonempty exact manifest
and real DRAIN_WRITE through Service/Transport/Controller/Producer/source and
private guest Received provenance. It is not the later full4MiB canary/platform
acceptance. GAP cases retain actual DECLINED/result:null and protected proof_hash
rather than fabricating a Result. Issuer cases retire separate A/B/A contiguous
prefixes of one shared family floor with their original complete credentials.
Task614 owns positive final-session handoff; C7 never changes RELEASE_CELL into
that proof. Artifact/resource/installed-kernel gates remain separate.

R1 completion-ownership regressions

`--scenario allocate` uses the existing one-shot ALLOCATE SEND and fetched-reply
loss primitives, then actual same-request/same-cell allocation retry. It checks
saved channel/kind/body profile and unchanged local cell/claim counts before real
CALL/QUERY/adoption/floor completion. `--scenario dirty` runs both the original
cross-session scenario and retained-result pressure: real ordinary result borrowers
and ordinary VIEW_QUERY descendants saturate actual ordinary capacity without
control-class growth. Four separately transported/private EVENT owners per session
model the actual pending[3]+last-event lifetimes. Actual production reservation and
Received-prepare primitives model the parked WAIT and two floor prefetch capacities;
they create no Received association, receipt or physical state. Remaining shared
control quota is held, then the real Go/C nonempty FROZEN/drain/ACK/release/floor
path progresses through independently preowned lanes. Ordinary parent retirement
refuses until real borrowers are freed. Native scheduling, signatures, source and
physical predicates remain unchanged; the scheduling/resource substitution is not
installed-kernel or native concurrency proof.

HELLO startup observes actual frees and verifies its carrier remains charged until
token/data/JSON destruction and only then releases its actual candidate. The
monitor regression invokes actual maintained runner functions with real child
process groups, disappearance races, monitor exceptions and bounded termination.
It includes a TERM-responsive leader with a TERM-resistant descendant. The
shared helper observes exit with `waitid(WNOWAIT)`, retaining the leader's PID
until group cleanup finishes, then reaps it. This also cleans remaining group
members when the leader exits normally. The regression uses process-local
subreaping solely to observe and reap its synthetic grandchildren:
`TMPDIR=<owned> python3 tools/integration/linux9p/tests/common/runner_monitor_test.py -v`.
