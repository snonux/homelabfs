# External history oracle and bounded scenarios

`internal/sim` now supplies an external history recorder, independent state
checker, seeded script generator and bounded deletion minimizer. The executable
adapter uses the existing real `fsservice.Service`, journal codecs/recovery,
storage and crashable VFS fixture. This is disposable **local validation**.
Normal replication, partner readiness, distributed commit and takeover remain
unimplemented; these results satisfy none of their acceptance gates. No normal
write acknowledgement or replica receipt is fabricated to run the scenarios.

## External evidence and supported workload

The scenario owns `History` in process memory outside actor storage, so a node
crash cannot remove it. Each invocation records a unique logical operation ID,
volume UUID, client identity, owned input, SHA-256 payload checksum and virtual
timestamp. Completion records its ordinal, timestamp, outcome and result text.
Ordinals distinguish events at the same virtual timestamp. A successful reply
is `Acknowledged`; a timeout, lost reply or post-admission error is `Uncertain`.
An invocation still pending at collection is also uncertain. `Rejected` means
proven pre-admission rejection only. A lost reply cannot later be rewritten as
an acknowledgement or definite rollback.

This version deliberately supports serial calls per volume. Overlapping
invocation/completion intervals are rejected, rather than interpreted as a
linearizable concurrent history. Several client identities can issue successive
calls. The recorder and runner are single-scenario owned, without concurrent
Go callers. Multi-client overlapping histories need a separate extension with
real-time partial-order validation when the distributed engine exists.

The independent workload model covers exclusive empty regular-file creation,
append, chmod, rename to an absent name, and unlink in the volume root. It
compares the complete observed namespace, bytes, file type, mode, UID/GID,
link count and logical size, including stable root metadata. This is an explicit
projection: native inode/allocation identities, internal revisions, timestamps,
hard links, directories below root, permission authorization and concurrent
syscall semantics are outside this oracle's current coverage. The smoke driver
uses UID/GID 1000; append models that unprivileged caller clearing set-ID bits.
The declared baseline is fixed fixture input, never exported from the recovered
service to manufacture an expected answer.

## Complete-history acceptance at an exact cut

`CheckHistory` takes a volume, declared baseline, external history and one or
more recovered observations. Every observation must identify the **same exact
committed owner-stream frame**: volume, generation, configuration, epoch,
sequence and SHA-256, with the same ordered recovered logical effects.
Sequence equality alone is insufficient. Each effect also supplies its real
journal operation UUID and exact committed source frame. UUIDs cannot repeat;
sources must belong to the common history, increase strictly and lie within
the cut. Every append also retains the actual selected `record.Write.Offset`,
including explicit zero offsets; missing offset evidence fails closed. Before
each append, the checker compares this observed offset against the current file
length computed from independent input replay. Later overwrite, rename or unlink
cannot mask an earlier positional write that violated append semantics. Replica
history equality includes these selected offsets.
The local adapter verifies actual COMMIT-to-MUTATION references with
the real reader, decodes mutation arguments, and correlates actual mutation
UUIDs to the external invocation while each controlled call is isolated.
It does not create a recovered effect from a successful client reply.

The checker replays **external input**, comparing recovered intent against that
input before applying it to the independent model. Every acknowledged mutation
must occur exactly once. Uncertain mutations may occur once or be absent, as
long as the resulting history is valid. Rejected/uninvoked mutations, changed
payloads, duplicates and reordered serial effects fail. The chosen observed
uncertain subset is verified directly; no exponential subset search or inferred
replica-majority truth is used. All observations are compared against the
independent result, so replicas agreeing on missing data or wrong metadata fail.

This is final complete-history acceptance, with **every acknowledgement in the
supplied volume history covered by the cut**. Selecting an earlier common cut
cannot omit a later acknowledged operation to obtain a pass. No prefix-check
mode is exposed. The adapter must collect an immutable/quiescent cut; passing a
claimed frame/hash does not by itself establish distributed commit authority.

## Controlled seeded execution and reduction

`GenerateScenario(seed, count)` uses an independent labelled deterministic
random stream to choose at most twelve actions, clients, payloads, explicit
time advances and named local faults. It never consumes transport/core random
streams. Faults are reply loss, crash after reply, reply loss followed by crash,
and the actual journal before-append error followed by recovery. Reply loss
deliberately hides the completed result from the client history; it does not
claim a transport-layer reply failure was exercised.

The adapter enters `Node.Call`, waits for actual request completion, records
the client outcome **before** any post-reply crash, and then executes explicit
lifecycle/time actions. Every final observation crashes/reconstructs the real
service and reads actual recovered data and attributes through FSAPI. It scans
the real recovered journal through the service's applied cut, after collecting
the read-side pins. No wall-clock sleep or goroutine arrival selects a fault.
Network fault scenarios still use the simulator's separate exact packet keys;
the local runner adds no automatic delivery loop.

The simulator's delayed precrash ACK packets and unobserved remote RAM loss
remain intact. This oracle neither revokes a surviving peer's knowledge when
another actor restarts nor imports the TLA model's observed-restart abstraction.
Connecting these schedules to real distributed acknowledgements requires the
pending normal replication engine and its readiness/commit prerequisites.

The recorder admits at most 256 calls, 4 KiB per payload and 1 MiB cumulative
payload; result text is at most 1 KiB. Checks admit at most sixteen observations.
Generated scripts contain at most twelve actions, fit the fixture's sixteen
lifetimes and retain the simulator's existing storage/event budgets. Each local
scenario fixture has a thirty-second wall-clock script bound, independent of
virtual time; existing smaller simulator fixtures retain their five-second bound.
These limits are admission controls, not performance guarantees.

`MinimizeScenario` removes contiguous chunks using fresh executions, with a
caller-selected maximum of 128 attempts and context cancellation. A callback
accepts only a reproduced checker failure; invalid reduced scripts do not count
as counterexamples. The local random test uses 24 attempts on invariant failure
and prints the original seed/script, reduced script, attempt count, original
external history, recovered observations and trace as JSON. This is bounded
reduction, without a claim of global minimality. Preserve that original evidence
alongside the reduced script. The seed regenerates the original; replay a reduced
script by passing the preserved `Scenario` directly to `RunScenario`.

## Local validation

Run the bounded target with `GOMAXPROCS=2 mage simHistory`. Defaults are eight
seeds starting at one, twelve actions each. Replay one seed with:

```sh
HOMELABFS_SIM_SEED=42 HOMELABFS_SIM_CASES=1 GOMAXPROCS=2 mage simHistory
```

`HOMELABFS_SIM_CASES` accepts 1..64; the Mage target has a 120-second suite bound.
Hand-built histories cover acknowledgement loss, both uncertain outcomes,
duplicate/reordered/uninvoked effects, changed payloads, wrong bytes/metadata,
divergent histories/cuts and replicas agreeing on a wrong answer. A deliberately
broken adapter executes the same logical append twice against the real service;
the checker rejects its recovered history and the minimizer retains the needed
create/append counterexample. Another real-service regression issues append A,
then a broken positional write B at offset zero, then unlink. Both correct and
broken executions finish with the same empty namespace, but the checker rejects
the broken execution at its selected append offset. Correct empty/nonempty-file
offsets and missing offset evidence are covered. A replay test compares complete
traces.

These commands use bounded local process memory, no host sockets, SSH, VM,
native FUSE mount, ZFS pool, production data or installation. Lab/native test
runs and disruptive lifecycle windows retain their separate explicit approval
requirements in `AGENTS.md` and the lab runbooks.

Cross-volume transactions use the separate [transaction simulation oracle](Transaction-simulation.md),
which checks the real record-oriented engine with synthetic private namespace and
recovery capabilities. The existing local-service oracle above remains unchanged.
