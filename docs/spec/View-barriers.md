# Stopped domains, reserved drains and view installation

Task `qz3`: generic semantic design and bounded model. This is **not implemented**
and supplies no kernel, deployment, normal-publication or arbitrary-bound proof.
It refines the positive barrier *interface* assumed in the separate
[all-origin model](../../spec/tla/README.md#successor-all-origin-commitment-model-dz3).
It is not a machine-checked refinement mapping or a composition proof with that
model. Its existing modules, configurations and recorded results remain unchanged.

The [normal replication contract](Replication-protocol.md) owns origin durability,
selection and protected results. [Durability](Durability.md#mmap-and-guest-buffering)
owns syscall promises. This model refines their frontend sequencing independently
of guest credential trust. The credential delegation and maintained patch
proposal in [retained 9p compatibility](../lab/Retained-9p-compatibility.md) remains
unselected pending the separate user decision (`jz3`). No policy, installation,
mount, VM, lab operation or publication permission follows from this document.

## Interface and sequencing

All admitted native and guest reader/cache domains belong to the barrier inventory.
A native root read participates just like a guest read. Coarse per-volume tokens
allow several committed readers or one exclusive dirtying domain. Exclusivity is
transferred by recall; it is not a permanent writable-client restriction. The
finite graph chooses either modeled domain as the dirtying owner. It checks one
recall and a conflicting final operation, not repeated token transfers.

1. Reserve a bounded package for each potentially dirty page **before** installing
   its writable PTE: dirty bytes/provenance, origin grant/journal/result, memory
   retention and retry capacity. Each package remains owned until its obligation
   is resolved. Exhaustion blocks new writable permission, before CPU stores.
2. Close new conflicting operations and token acquisition before reserving the
   final conflicting sequencer slot. The caller waits without a vnode lock,
   sequencer slot, worker or credit that a drain needs. Independent bounded
   control/drain worker capacity remains available.
3. Revoke writable access and complete physical observation quiescence, including
   preexisting reads and busy/pinned aliases. Only then freeze the exact dirty
   manifest and acknowledge **STOPPED**. A failed alias revocation leaves the
   domain recalling. Connection loss, timeout and a fresh connection are not
   positive STOPPED acknowledgements.
4. Submit every frozen dirty description through the reserved origin-operation
   path. It retains the ordinary durable-origin/distinct-memory/result contract.
   Each serial drain installs its protected intermediate cut in all stopped
   domains while user observations remain closed. Its result and page cleanup
   do **not** wait for DRAINED. Otherwise DRAINED would wait for cleanup, cleanup
   for the result, and the result for DRAINED.
5. **DRAINED** certifies that the frozen manifest is resolved, all intermediate
   cuts are installed and aliases are quiescent. Require exact STOPPED and
   DRAINED acknowledgements from every inventoried domain before the final grant.
6. Protect/select the final operation, install its exact cut and generation in
   every domain, receive matching installation acknowledgements, then reopen and
   return success. Empty caches refill from the owner's logical RAM view. Backing
   apply is unnecessary for this visibility boundary and is outside this model.

Every acknowledgement binds domain, authority epoch, recall ID, live connection
session, exact frozen manifest and the phase's cut. Messages remain replayable;
receiving a duplicate does not create another operation. Recall and installation command delivery require a live connection. After delivery,
local PTE revocation, STOPPED and installation may finish offline and queue their
old-session acknowledgement. Coordinator recall intent does not itself notify
a disconnected domain; preexisting cached observations and stores remain possible
until the corresponding local gates close. Disconnect clears learned
acknowledgements for that connection without dropping the frozen obligations or
pretending physical mappings were revoked. Reconnect preserves the stopped state
and manifest, installs a new session and requires fresh exact acknowledgements.
The model covers connection loss with a continuously surviving domain ledger;
daemon/kernel crashes and ledger reconstruction require the separate recovery
protocol, not this reconnect transition.

## Client stores and reads

A CPU mmap store or guest-buffered write dirties client memory. It is not yet a
delivered daemon mutation, selected journal record or durable write. The model's
`Store`, `Deliver`, grant/prepare/durable/select stages and `Protect` are separate.
A store can race with recall until writable access is actually revoked. STOPPED
then freezes all those stores. Delivery may occur before recall or through the
reserved path after STOPPED; neither delivery nor an empty queue proves durability.
The finite page identity stands for immutable bytes plus its description grant;
actual byte merging, overlapping-page stores and exact credential provenance are
component obligations rather than modeled codecs.

A conflicting committed observation must acquire the token through recall/drain
before using the owner's cut. The model's Read guard is an abstract coherence/token
admission precondition, not knowledge that a remote CPU has dirtied a page. An
implementation must coordinate from known writable-token ownership and recall
before conflicting reads; it must not assume remotely observable dirty flags.
Read-token acquisition messages and how domains learn that state are unmodeled
platform/integration obligations. This includes root/native observations across a
guest dirty token, even though the owner's current selected cut has not yet
included those client bytes. The model conservatively gates cross-domain committed
reads while dirty bytes await drain. After final installation, either domain can
observe that cut. It does not call earlier client-buffered completion the
per-daemon durability contract. Actual guest ordinary-write, MS_SYNC, fsync,
fdatasync and directory-fsync boundaries still need the verified platform contract
required by Durability.md. Application synchronization defines races with future
stores; private copied user memory does not become a shared file-backed alias.

## Finite model and verification contract

[ViewBarrier.tla](../../spec/tla/ViewBarrier.tla) has two domains (`native`, `guest`),
one arbitrary dirty-token owner, at most two once-dirtied pages, one serial drain
slot/worker and one final operation. Each page package is reserved independently. Credits are not recycled within
this one-barrier graph; sustained admission and credit recycling need separate
proof. The capacity profile admits two potential pages with only one package, checking
backpressure before dirtying. Connection profiles allow at most one disconnect
and reconnect **per domain**, at any pre-completion phase. Fixed epoch 7/recall 11
are compared with delayed old-context acknowledgements; this is not a model of
multiple authority transitions. No domain is removed from the inventory.

The normal-operation pipeline abstracts the verified all-origin component guards:
grant, preparation, origin durable choice, selection and protected result. It does
not reproduce remote ACK timing, RAM loss, origin disk loss or complete source
coverage; those remain the separate all-origin/recovery obligations. Safety checks
pre-PTE capacity, frozen manifests, alias quiescence, exact acknowledgement contexts,
no early final grant or reopening, selected/protected exact-once cuts, read gating
and the absence of drain-needed resources held by the conflicting waiter.

The positive `Progress` profile adds weak fairness for begin, alias release,
revocation, stop, delivery, reserved workers, intermediate/final installation,
cleanup, reconnect and exact phase-message emission/receipt. It requires eventual
successful reply for this finite workload. Admission and dirtying are not fair:
completion also covers zero admitted dirty pages. The separate witnesses require
actual dirty workload and post-reply reads, avoiding that vacuity. Progress assumes
finite connection losses, a surviving ledger/authority, available reserved storage
and worker capacity, eventual alias release and working physical barriers. Pinned
aliases or a dead disconnected domain can block forever when those assumptions
are removed. No finite latency, partition availability or physical revocation
progress follows from the temporal check.

Run selected local profiles, sequentially, only when the coordinating run is
approved:

```sh
python -B spec/tla/check_view_barrier.py ViewBarrierSafety ViewBarrierCapacity
python -B spec/tla/check_view_barrier.py --group negative
python -B spec/tla/check_view_barrier.py --group witness
python -B spec/tla/check_view_barrier.py ViewBarrierReconnect
python -B spec/tla/check_view_barrier.py --seconds 900 ViewBarrierProgress
```

The runner uses existing TLC 1.8.0, SHA-256
`fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`,
one worker, one active processor and 1 GiB JVM heap. There are no depth/state
cutoffs, state constraints or symmetry reductions. The 180-second default wall
limit fails incomplete checks. Evidence limits are 2 GiB per profile and 4 GiB
aggregate across retained ViewBarrier attempts, checked during execution;
exhaustion stops the checker and preserves evidence. Exact source/configuration,
runner/tool hashes, commands, actual exits and logs are retained below
`.cache/tla/ViewBarrier-*`. Positive success requires exit zero and an empty queue.
Safety negative controls require a named Safety counterexample/exit 12;
reachability controls require WitnessAbsent/exit 12. The recursive-barrier control
requires an actual temporal counterexample/exit 13, not a timeout.

The deliberate mutations remove pre-PTE credit reservation, alias exclusion,
exact context checks, resource release by the final waiter, pre-grant drain,
closed intermediate installation or cross-domain dirty-read coordination. The
recursive control makes intermediate installation wait for DRAINED and must
expose a fair non-completing execution. Controls are sensitivity checks, not
findings in the proposed sequence. Witnesses require two dirty pages completing,
an intermediate cut installed while stopped, dirty workload surviving reconnect and explicit rejection of a real queued
old-session acknowledgement, local STOPPED/queued acknowledgement after a
delivered recall loses its connection, and both domains reading after dirty-workload completion. The offline-STOPPED
observer records only an actual local Stop action while disconnected and is
enabled only in that witness profile; it stays empty in all other profiles.
No protocol guard depends on that observation. This prevents an online STOPPED
followed by disconnect from satisfying the offline-execution witness.

[The recorded final checks](../../spec/tla/ViewBarrier-results.md) contain all
seventeen exact-source outcomes: four complete positive graphs, seven expected
Safety counterexamples, one fair recursive-barrier temporal counterexample and
five reachability witnesses. Complete finite exploration remains conditional on
TLC fingerprints not hiding a distinct state. No local model result grants
runtime readiness.

## Remaining platform obligations

Actual native FUSE and guest 9p implementations must prove PTE/TLB/MMU quiescence,
fault-around/page-mkwrite/map/unmap/fork races, writable/exec/open-unlinked/hard-link
aliases, splice, GUP/DMA pins and mlock behavior. Long-term pins must be rejected
before admission or positively revoked. Implementations must bind frozen dirty
provenance, immutable claims, credential snapshots, exact results and installed
cuts through cancellation, uncertainty and reconnect. Review real vnode/MM lock
order, reserved queues/tags/workers and journal/memory capacities; exercise their
exhaustion before stores. Daemon-self-mmap/GC deadlock remains a separate known
platform hazard. Validate that no cache-hit or native/root path bypasses the token.

Those tests need approved disposable integration runs with actual installed/source/
artifact bindings. Stock invalidation calls, zero TTL, advisory locks, historical
SQLite success and this model establish none of those physical facts. Failed
recall preserves dirty resources and gates publication; it cannot be converted to
an acknowledgement by a timeout or silent inventory removal.
