# Deterministic transaction simulation

Task `7w3` connects the real `internal/transactions` engine, versioned
owner/reporter journals and simulator storage to an independent external oracle.
Run `GOMAXPROCS=2 mage simTransactions`. The default seeded suite has eight
seeds starting at one, with four namespace operations each. Replay one seed:

```sh
HOMELABFS_SIM_SEED=42 HOMELABFS_SIM_CASES=1 GOMAXPROCS=2 mage simTransactions
```

The target is bounded to 120 seconds. Count accepts 1..32; seed accepts an
unsigned 64-bit integer. A race check uses the same test pattern and a 600-second
bound, without coverage. The final race profile runs every named control and
reproducibility check with two four-action seeds (`HOMELABFS_SIM_CASES=2`),
separately from the normal eight-seed profile. These are local synthetic checks under the standing
local-test authorization. No VM, SSH, ZFS, mount, production data or external
service is accessed. Lab runs retain their separate exact-run approval rules.

## Actual implementation and synthetic capabilities

Three real simulator `Node` actors have separate crash-fenced storage roots.
The engine writes real owner streams for three groups. Each bound operation has
at least two groups, whose positions, prepares and installs remain distinct.
The third group permits a later closure merge to change the current coordinator
minimum without changing an old binding. Calls enter the owner's controlled
`Node.Call`; expected operation errors are retained outside the scheduler callback
so an injected I/O error does not become an unrelated scheduler failure.

The fixture supplies mandatory trusted `Protector`, `RecoveryVerifier` and
`Applier` capabilities. This is a **direct deterministic capability/message
adapter**, not transaction replication through `transport.Endpoint`. The existing
network simulator's delayed packets and unobserved RAM-loss behavior are unchanged.
The fixture captures complete actual source journal/private-state bytes in the
partner's owned RAM. Typed ACCEPTs are written through the actual segment writer
in a distinct, explicitly volatile partner root. A simulated RAM-loss event
crashes the node, discards that root and owned retention, and keeps already
serialized replies. It does not invalidate a surviving owner's session knowledge
by magic. Subsequent sends fail until reauthentication; the queued old response
can still arrive.

Private namespace apply consumes `PREPARED.Redo`: exact entries, object metadata,
reference counts and directory parents. It checks old revisions and persists a
snapshot plus applied-operation markers through sync, rename and directory sync.
Replay is keyed to the immutable request/participant, independent of the ballot,
and preserves later writes when earlier installs replay. Publication persists the actual complete namespace, metadata and object bytes
in a separately synced snapshot; it does not persist only an oracle projection.
On reopen the adapter discards volatile publication, visibility and gate state,
reloads this durable source, then reconciles actual private data and journal state.
A pending partial install retains the previous whole durable generation behind
the gate. A completed recovered generation reads freshly recovered private bytes;
an old in-memory hash snapshot cannot conceal corruption. Publication is a separate
explicit adapter step after protected COMPLETE. Every partial apply exposes only
the previous complete generation. A synthetic barrier checks actual apply markers;
it proves no kernel invalidation or guest/cache fence.

Ordinary phase-reopen tests retain the fixture's installed synthetic authority
while replay stays private. They do not implement real process-incarnation
reauthentication or authorize an old daemon to serve after restart. The explicit
successor test uses new configuration/epoch/term and actual actor incarnations.
Its trusted recovery adapter verifies scope and retained synthetic authorization
and positive-fence artifact bytes. Actual engine checks enforce promises, reports,
acceptance constraints, immutable binding and source identities. Neither these
artifacts nor the adapter implement a control-plane allocator or physical fence.

## Independent outcome oracle

`CheckTransactions` receives a literal declared baseline, caller intents, original
canonical documents/digests, client outcomes and actual observations. Its expected
namespace uses flat volume-relative paths; actual apply uses parent/object entry
indexes and typed participant redo. They share no namespace mutation helper.
Neither a recovered namespace snapshot nor replica agreement supplies expectations.

The external recorder retains each invocation and each client terminal receipt,
including successful retries after lost replies. Completion ordinals distinguish
older observations from later acknowledgements. Every completion is checked;
expected effects are deduplicated once by anchored identity. Missing acknowledged
claims, COMPLETE, result or publication fail. Protected abort receipts and preflight
rejections require their recorded anchored terminal outcomes and no namespace effect.
Uncertain replies do not assert rollback.

Observations retain exact engine source frames (including hash and full stream
identity), published visibility, stable object IDs, home data hashes/sizes,
link counts, directory parents, synthetic handle pins, and durable per-participant
apply counts. The oracle decodes the actual frame bytes, compares canonical
originals/results and replays independent caller inputs. Every typed FrameRef and
nonzero owner dependency position must resolve to its exact retained identity;
COMPLETE/INSTALL/prepare/decision/ACCEPT/barrier links must agree. Untrimmed owner
streams must contain every frame from their initial OPEN through the observed
cut. Cached ACCEPTs need not include a reporter OPEN prefix delegated to Protector.
Previously observed frames remain nonexpiring pins even while a prepare is unresolved. All source records and
home dependencies remain pinned; no checkpoint replacement, trim or real GC is
implemented. Struct-keyed maps have explicit JSON encodings so counterexamples
retain complete machine-readable history, namespace and frame evidence.

## Coverage and boundaries

| Failure/interleaving | Local evidence |
| --- | --- |
| Same ID, disjoint endpoints/nonowner | Conflicting submissions before bind meet one protected claim; nonanchor claim and split-owner manifest fail without effects. This adapter provides the synthetic routing boundary; there is no Unix API server. |
| Source gone, lost reply, later lower coordinator | Exact claim/bind/result retries after rename, owner reopen and affinity growth retain the original coordinator and one effect. Retirement/closed session reject reuse while pins remain. |
| Partial affinity or BEGIN/prepare/install | Restart after each of fifteen boundaries; partial affinity survives reopen and blocks binding until full fan-out. Every partial state is checked against the previous complete namespace. |
| One prepare, timeout/cancel | No terminal result or visible effect from timeout. A protected ABORT cleans up without publication; its anchored result survives reopen. |
| Accepted COMMIT reply lost | Typed partner acceptance survives transport uncertainty; reconcile preserves COMMIT and completes all installs. |
| Owner disk lost, partner tail retained | Delete the actual owner's root; import only partner-owned bytes, then persist B/C promises/reports/fence/selection and protect a new term-2 decision, install and complete. The independent oracle checks recovered namespace/result and immutable operation identity. |
| Barrier failure, COMPLETE/reply loss | COMMIT remains immutable; private state stays behind the gate until the synthetic barrier succeeds. Retry/reopen returns the original outcome. |
| Delayed old term | Actual successor engine rejects the old decision after durable higher promises/selection. |
| Unobserved partner RAM loss then owner disk loss | Serialize old ACCEPT, crash B before its reply arrives, deliver it without a perfect detector, then delete A's root. Empty B/C sources cannot resolve the identity; no abort or partial publication is invented. New-generation accepted-loss recovery remains unsupported. |
| Directory and authorization checks | Opposing directory moves reject cycles; directory hard links and synthetic denied policy fail preflight. Exact old entry revisions reject stale redo after prepare. These are fixture checks, not full Unix/sticky/view policy or concurrent syscall conformance. |
| Foreign aliases and lifetime | Link, home write through foreign alias, unlink both names and replay retain the original home/data and an open-handle pin. Symlink moves preserve text. Moving the only subtree empties the old namespace while its home remains required. No mmap/cache/locking behavior is claimed. |
| Capacity/GC/checkpoint races | Post-COMMIT frame capacity backpressures without abort. Home reclaim is refused while references, handles or nonexpiring engine evidence remain. Checkpoint replacement, snapshot administration, actual pruning and closure splitting are unsupported. |

The seeded generator varies link, rename, write and unlink, chosen files/volumes,
payloads, reply loss, acceptance receipt loss, barrier failure and phase crashes.
It uses a separately labelled random stream. Final scenarios reopen actual private
state and journals before checking the last observation. Same-seed tests compare
complete scheduler traces, source frames and namespace outcomes.

On invariant failure, reduction removes action chunks using fresh executions with
at most sixteen attempts. A candidate must reproduce the original invariant
class; invalid dependent scripts do not count as counterexamples.
Evidence includes the original seed/script, reduced script, attempt count, full
external history, observations and trace. This is bounded reduction, not global
minimality. Negative controls exercise actual broken private publication, repeat
apply, premature home deletion, recovered-byte corruption, missing pinned
INSTALL/barrier/ACCEPT/session/prepare evidence and lost acknowledged histories; they must produce
an oracle `CheckFailure`, not a setup error. A dependent-write reduction test
retains the target visibility violation rather than an absent-source error.

Per scenario limits are four generated actions, 32 recorded invocations, 4 KiB
write payloads, 256 names/objects, 128 observations, 1,024 engine frames and 4 MiB
retained frame/image bytes. Private snapshots are capped at 1 MiB. Each node keeps
the simulator's 100,000-call, 8 MiB cumulative-write and sixteen-lifetime bounds;
fixtures have a ninety-second context for race instrumentation overhead. The
initial thirty-second fixture profile failed its race run on context deadlines;
that failed run is not a race pass. Resource exhaustion preserves evidence and
stops admission. These finite checks complement the `5w3` TLA projections and
`6w3` engine tests; they do not establish production or native filesystem support.
