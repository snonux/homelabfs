# Persistent transaction ledger

This package is the pure, bounded coordinator/participant protocol core for
[`Transactions.md`](../../docs/spec/Transactions.md). It uses the existing version
1.0 record codec and segment writer/reader over `vfs`. It does not implement a
network replication service, takeover controller, namespace index, cache fence,
FUSE adapter or serving publication. It grants no production capability.

`Open` verifies retained journal streams and reconstructs logical state. A fresh
instance with semantic history requires `Reconcile` before ordinary proposals or
terminal status. Neither a complete local record nor successful local sync is
treated as a chosen decision. Missing, contradictory, corrupt or over-budget
history stops progress. The package never repairs a tail or invents an abort.
Any uncertain journal failure requires closing and reopening the instance;
transport uncertainty requires reconciling the exact existing candidate.

`Propose` takes an existing typed journal payload, validates its protocol
transition, appends/syncs through `segment.Writer`, and requires current partner
protection. It is deliberately record-oriented: the service resolves paths,
captures authorization/revision checks, supplies exact local redo and owns the
closure gates. Anchored SESSION and REQUEST_CLAIM precede resolution. Bindings
include the immutable anchor and remain unchanged when later affinities grow.
Retries compare complete original documents and immutable record contents,
reuse stored frame identities, and consult retirement floors/tombstones first.
Fresh bindings include the transitive member union of every discovered complete
or partial affinity join. They wait for full fan-out of a manifest descending
all known manifests in that component. Retained earlier bindings and terminal
results keep their original participants and references during retry/replay.

The ledger enforces full affinity fan-out before binding, one active operation
per overlapping member set, full participant anchors before prepares/commit,
complete prepares for commit, one candidate per term, exact decision installation,
complete applied/cleanup vectors, barrier records, COMPLETE and anchored terminal
results. An explicit `Cancel` can propose abort before a decision exists. A lost
reply, canceled wait, missing partner or failed apply/barrier cannot reverse a
commit candidate or prove abort. GetStatus does not execute unknown identities.
If COMMIT wins while Cancel is proposing an abort, Cancel can return ErrConflict;
retry Cancel or GetStatus with the same identity to obtain the actual outcome.

The injected `Protector` is mandatory and has no local-only implementation.
It validates installed exclusive authority, complete anchor recovery and the
current authenticated session before and after blocking retention. It must own
the complete ordered prefixes, referenced metadata/payload/artifacts, reporter
stream prefixes and retryable asynchronous catch-up obligations. A plain receipt
cannot choose a decision: the engine also validates the exact partner-authored
typed ACCEPT, its candidate, request, term, authority, incarnation and dependency
coverage. The external transport must authenticate that assertion and prove its
reporter prefix; matching bytes alone cannot do so.

`ProtectionRequest.Required` is the current owner vector, with one position per
volume. `Histories` separately retains exact prefix boundaries by full owner or
reporter stream identity, including predecessor configurations and incarnations.
The engine never compares sequence numbers from different histories. A current
partner may retain and forward immutable historical bytes, but a current receipt
does not become a fresh old-term ACCEPT. Historical decisions remain linked to
their original candidate and acceptance evidence.

`ProposeRecovery` is separate from ordinary serving admission. It persists
PROMISE and TAIL_REPORT in explicitly configured reporter streams, and FENCE and
RECOVERY_SELECT in the current coordinator's owner stream. A mandatory
`RecoveryVerifier` first authorizes the exact recovery-only stream/authority scope
independently of normal pair readiness, then the exact evidence and original
stream on admission
and replay. It must independently validate control allocation, artifact contents,
authenticated reporter continuity/accepted history, the complete intersecting
storage majorities, positive old execution/cache fencing and successor authority.
The engine additionally checks stream/hash/lineage identities, per-group promise
and report majorities, durable term floors, immutable binding/coordinator,
selected prepares/results and known accepted/installed constraints. Absence abort
requires the common partner's continuous complete accepted tail for every member.
The verifier is a trusted integration capability, not a boolean flag or a timeout.

A successor DECISION has a higher authorized ballot linked through the exact
RECOVERY_SELECT. It preserves the original operation/request identity, digest,
coordinator UUID, prepares and provisional result. The original candidate bytes
remain pinned. Its newly authored ballot needs the current partner's typed ACCEPT;
old acceptance evidence cannot substitute for that new acceptance.
Every selection, including absence abort, and every normal or recovered ballot
retains BEGIN's stable operation identity.

Each selected ballot records its own INSTALL in every bound participant before
COMPLETE. INSTALL retries are immutable per request and ballot; old INSTALLs stay
pinned. Recovery after a partial old installation may temporarily have mixed
ballot installs behind the gates, but COMPLETE rejects that mixture. Later-ballot
INSTALLs reuse the original prepared redo, so apply deduplication remains keyed
to the immutable prepared operation/applied cut rather than the decision ballot.

`Applier` operates only on private state behind closed closure gates. Exact
prepare/decision entries identify the participant and applied cut. Apply must
durably deduplicate operations across subsequent changes: repeating an old
set-A after a newer set-B must leave B in place. Repeating a raw syscall is not
sufficient. Abort cleanup must also be idempotent. Barrier receives the exact
install and cache/drain attestation frames and must validate their actual effects.
It must neither publish nor release the gates. Protecting COMPLETE makes the
visibility generation ready for the service's atomic publication; this package
never publishes a mount or a partially applied namespace.

All complete source records and referenced artifacts remain nonexpiring pins.
Retirement changes retry admission, not data, snapshot, foreign-home or catch-up
retention. Default bounds are 64 current groups, 256 total streams, 4096 frames
and 32 MiB of raw retained frame bytes, including cached ACCEPTs. Admission has
at most 64 waiting callers; contexts bound queued waits without changing an
admitted operation's outcome. Segment scan limits and codec allocation limits
also apply. Capacity exhaustion backpressures
admission and preserves pending decisions. Checkpoint replacement, prefix trim,
GC, anchor retirement, loss recovery/new generations, membership reconfiguration
and closure splitting are unsupported and fail closed. Initial membership is
exactly three replicas; fenced recovery preserves that membership and generation.
Historical owner streams must be complete, untrimmed predecessor histories.

The local tests use disposable simulator storage, real versioned owner/reporter
journals, durable private apply markers, recovery authorization/fence artifacts
and injected receipt faults. They exercise phase/partial-fanout crashes,
idempotency across later writes, missing/contradictory graphs, session floors,
ballot recovery and same-volume histories. These results do not establish real
transport, controller, physical fencing, kernel cache or namespace publication
correctness. Those capabilities and separately approved infrastructure tests
remain integration work.
