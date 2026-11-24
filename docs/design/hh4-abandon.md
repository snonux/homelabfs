# hh4: a blocked session abandons its stranded claims (design)

This is part 2 of a84. Part 1 added the refusal trace. Both are described in
[internal/guestbridge/README.md](../../internal/guestbridge/README.md),
section "Swallowed refusals and stranded RESERVED claims (a84)".

Status: both sides are implemented. The host side (Go) came first; the guest
kernel patches (0002, 0003, 0005 under `tools/integration/linux9p/patches`)
and the lib9p forwarder followed, as described in "The guest change" below.
No guest kernel was booted: the guest side is proven by a local object build
and the component harnesses only.

## Problem

After Reserve, every uncertain execution error answers CALL ADMITTED/OK and
leaves the claim RESERVED/UNPREPARED. The guest polls QUERY for 30 s, then
blocks the session (`hfs_session_block`) and keeps the claim retained. From
then on:

- the claim keeps its retained and source credit (`ClaimCreditBytes`,
  `SourceCreditBytes`) until a RETIRE recycles its number;
- the guest never retires past it, because it has no terminal result or gap
  proof for it;
- SESSION_RETIRE refuses, because `terminalEmpty` needs every number at or
  below the floor.

The provider is per session, and a blocked session still admits reserved
control work: drains, releases, FORGET, GAP_RESOLVE, retirement. That work
needs claim credit. Stranded claims can starve it, and they make a clean
SESSION_RETIRE impossible.

Releasing such a claim is easy, and a84 already sketched it: decline it as a
gap, return its credit, zero the entry's credit. The missing piece is a
trigger the host can prove. None of these is one:

- `hfs_session_block` is guest-local. The host sees nothing.
- A host timeout cannot rule out a QUERY still on its way. That QUERY would
  resume the claim, and the guest still takes TERMINAL after its deadline.
- A transport failure is the host's own fence. It says nothing about what
  the guest gave up.
- SESSION_RETIRE needs `terminalEmpty`, which the stranded claim prevents.

## Options

**A. CANCEL each uncertain claim on block.** CANCEL already crosses every
leg. The guest names each claim, and the provider's `Cancel` already declines
an ALLOCATED or RESERVED claim as a gap. Its weaknesses:

- CANCEL's contract is conservative. The spec says it "detaches waiting
  only ... it never turns unknown into NO_EFFECT", and its reply has the
  QUERY shape, which never says DECLINED on 1.2/1.3. So the guest learns
  nothing definite. It would need a GAP_RESOLVE per claim to get a gap proof,
  and a gap QUERY after that: three exchanges per claim, each one a new
  chance to be lost on a blocked session.
- Making CANCEL return credit changes the meaning of an existing operation.
  An older host would accept the same CANCEL, decline the claim without
  returning credit, and answer INDETERMINATE. The guest could not tell
  whether the release happened.
- `Cancel` checks no session binding beyond the provider's scope check. It
  also declines a non-original RESERVED claim in stage PREPARING. That is
  harmless for controlled traffic (every controlled claim is an original),
  but it is the wrong predicate for a credit release.

**B. ABANDON: a session-bound command naming an exact claim list.** This is
a new controlled operation. The guest sends it once, after it blocks. It
lists the claims it gives up on and promises never to send another CALL,
QUERY or GAP_RESOLVE for them. The host answers each claim definitely, and
gives a gap proof for every one it declined.

**Choice: B.** It is the safer option:

1. It has one exact predicate (`abandonable`, below) and one transaction.
   No existing operation changes its meaning. CANCEL, QUERY and GAP_RESOLVE
   behave exactly as before.
2. The reply is definite per claim and carries the proof RETIRE needs. The
   guest needs no follow-up exchange. A blocked session should send as few
   exchanges as possible: each one can hit the 30 s deadline and set
   `transport_failed`.
3. It fails safe on an old host. The first refusal is not the Go closed
   schema but the lib9p exporter: its command classifier (`_command`,
   `tools/integration/lib9p/src/hfs9p.c`, the final `else` that returns
   EINVAL for an unknown op) refuses the cell when the guest's last chunk
   completes it, so the guest's send fails (ERROR INVALID, -EPROTO in the
   guest) and nothing reaches the host. That is what a guest meets behind a
   forwarder older than step 7 below. A Go host reached directly refuses
   ABANDON in its closed schema. Either way the guest keeps its claims
   retained, exactly as today. With option A the guest could not tell
   whether the release had happened.
4. It is bound to the session and the claim: the session's own provider
   route, the claim's protected original context, and its exact original
   hash. A claim bound to another session, or a wrong hash, refuses the whole
   command, and nothing is written.
5. It needs no credentials per claim. GAP_RESOLVE needs the full original
   Call (up to 64 KiB each). ABANDON only gives up authority the session
   already held, and the trusted guest kernel is the adapter for the whole
   session anyway (spec: guest root and kernel can forge in-export
   identities).

The cost is a new operation in the guest codec and in the lib9p forwarder.
That is the same cost READDIR_CLEANUP had.

## Wire (docs/spec/Retained-9p-controlled.md)

ABANDON is accepted under every controlled tuple (frame minor 2, 3 and 4).
Closed 1.0 and 1.1 refuse it.

```
payload  {realm:uuid, subject:bytes(1024), anchor:uuid, nonce:uuid,
          claims:[{number:n64, original_hash:hash}]}       1..64, ascending
response {claims:[{number:n64, status:ABANDONED|KEPT|RETIRED_ID|UNKNOWN,
                   proof_hash:hash|null}]}                  request order
```

- `verification` is null.
- It is reserved control work: `controlCommand` is true, so it uses the
  STOP/drain lane.
- Like GAP_RESOLVE and READDIR_CLEANUP, it is claim-ledger work, not a view
  operation. It skips the view provider's `CheckRequest`, and the claims
  provider authenticates it itself (`AbandonOriginal`: the observed command,
  its payload hash, the enrolled protocol and endpoint, and an independently
  decoded closed union).

## Host transaction (internal/controlledvalidation/abandon.go)

`Provider.abandon` holds `p.execution` and runs one bbolt update. For each
named number, in order:

| State | Answer | Ledger |
| --- | --- | --- |
| number <= floor (its entry may be recycled) | RETIRED_ID | none |
| no entry, only a dirty earmark (allocated by EarmarkNumber, earmark credit) | KEPT | none (closed by the controller's CANCELLED-ticket path) |
| neither entry nor earmark | UNKNOWN | none |
| entry claim differs (original hash) | whole command Stale | rolled back |
| protected original context is not this scope | whole command Permission | rolled back |
| entry bound to an open dirty earmark | KEPT | none (a DRAIN_WRITE byte obligation, dh4) |
| proven gap (GAP_RESOLVE, Cancel, an earlier ABANDON): DECLINED, nothing planned, applied or answered, stage empty or UNPREPARED | ABANDONED, current proof | none, never rewritten |
| `abandonable` | ABANDONED, new proof | Gap, DECLINED, Abandoned; both credits released and zeroed |
| anything else (PREPARING, PENDING, TERMINAL, ADMISSION_REFUSED, a gap whose preparation began) | KEPT | none |

The rows run in this order. For a number with an entry, its identity (exact
original hash, then the owning scope) is checked before any state is read,
so a wrong or foreign claim refuses the whole command even when it would
otherwise be KEPT. The stage check on a proven gap makes ABANDONED always
mean "provably never prepared": a legacy Cancel can decline a non-original
claim in stage PREPARING, and such a gap is KEPT.

Two states are `abandonable`:

- an AllocateOriginal entry whose CALL has not reserved it: ALLOCATED, stage
  empty or UNPREPARED, nothing planned, applied or answered;
- `neverPrepared`: RESERVED/UNPREPARED with no plan, planned outcome,
  outcome, result, bridge snapshot or snapshot version.

The proof is `hash(json(entry))` after the rewrite. That is exactly what
RETIRE coverage (`coverageHash`) and `checkEntryOwner` compare. The entry
keeps its a84 trace and its l64 Opened time. A later GAP_RESOLVE rewrites
identical bytes and returns the same proof.

The same transaction adds the release to the lifetime counters (root key
`abandoned`). OBSERVE reports them in each provider's `ClaimEvidence` as
`Abandoned` (claims) and `AbandonedCredit` (retained plus source bytes).
Both are omitempty and saturate. With them at their maximum, the worst-case
summary is 4510 bytes, under `MaxClaimEvidenceBytes` (4608).

A84 asked to "zero the entry credit so a later recycle returns nothing". The
entry stays until RETIRE recycles it, and the recycle then returns 0. The
gap entry's own bytes stay bounded by `MaxClaims`, because the entry still
counts as an identity until it is recycled. Nothing rewrites a gap or makes
it grow (a gap has no plan, so `checkPlanCredit` never runs for it). So the
credit had nothing left to pay for. Between the ABANDON and the RETIRE, the
ledger can exceed `MaxRetainedBytes` by at most the bytes of the abandoned
entries. The guest retires them right away with the proofs it got.

## Linearizability and replay

The rule: a QUERY, PREPARE or CALL that arrives after the abandon is refused
or has no effect.

Only one path can prepare a claim: `executeAdmitted`, which writes
PREPARING. A CALL and a QUERY resume both go through it (the resume via
`ResumeValidation` and the QUERY continuation). Every writer that moves a
never-prepared claim on holds `p.execution` and rechecks the claim inside
its own transaction:

- `executeAdmitted` reloads with `loadExact`, which refuses a gap (Stale),
  before it writes PREPARING;
- `refuseUnprepared` (j64) and `protectRefusal` recheck `!e.Gap &&
  neverPrepared`;
- `noteRefusal` (a84) skips gaps.

The abandon holds `p.execution` too, so it is totally ordered against each
of them:

- **Abandon after PREPARING.** The claim is no longer `abandonable`. It is
  KEPT, and its credit and entry are unchanged. That is right: the preparer
  may have run, and a84 says PREPARING and later stay for reconciliation.
- **Abandon before PREPARING.** The preparer's reload finds the gap and
  refuses. No preparer runs, so no effect exists. This holds even for a
  QUERY whose `Resolve` saw RESERVED before the abandon, and for an
  admission that was granted before it. Both end in `executeAdmitted`'s
  locked reload.

`Reserve` and `Allocate` take no mutex, but they are bbolt writers, and
bbolt writers are serialized:

- a CALL that reserves after the abandon finds `e.Gap` and gets Permission.
  A CALL whose Reserve won keeps a RESERVED/UNPREPARED claim, which the
  abandon then declines;
- a replayed ALLOCATE of an abandoned number finds the identical entry and
  repeats its acknowledgement without writing (as after GAP_RESOLVE). Its
  CALL is then refused.

After the abandon, the answers are:

- `Resolve` answers the gap as INDETERMINATE on 1.2/1.3. A mapped 1.4 QUERY
  gets DECLINED (`protectedGapQuery`), the definite protected no-effect.
- `recoverValidation` resumes only a RESERVED state, and `loadExact` refuses
  a gap. So the gap is protected twice.
- A replayed CALL of a known claim repeats ADMITTED/OK (ownership was
  transferred before) and executes nothing.

The tests check all of this (`TestAbandonedClaimRefusesLaterCallQueryAndGap`):
the gap's bytes stay identical, the file is never created, and GAP_RESOLVE
returns the abandon's proof.

**Replay of the ABANDON itself.** A listed claim that is already a proven
gap is answered from its current bytes and never rewritten. So an exact
replay rewrites nothing, answers the same outcomes and proofs, releases no
credit twice and does not count again. On 1.4 the transport also returns
the protected base of a retried cell (`retainedMappedBase`). On 1.2/1.3 a
resend in a new cell is idempotent at the ledger.

**What ABANDON never touches:**

- UNKNOWN numbers. Because of the guest's x84 allocation fence, at most the
  one ALLOCATE in flight can be unknown. If a delayed ALLOCATE lands after
  the ABANDON, it charges credit as it does today, and the guest keeps that
  claim retained. This is a residual, not a regression.
- KEPT claims stay retained for normal reconciliation.

## Crash replay of the ledgers

- **The claims ledger** (the provider's bbolt file). The abandon is one
  update transaction, so a crash leaves either nothing or every listed
  rewrite, the credit release and the counters together.
  `TestAbandonCrashReplay` aborts the transaction at the
  `abandon-before-commit` failpoint, after every claim was rewritten. It
  finds the entry and credit unchanged, reopens the ledger and commits, then
  reopens again and replays. The replay gives the same proof, the credit is
  released once and the counter is 1.
- **The facts (viewcontrol) ledger.** ABANDON writes nothing there. The
  runtime Facts' validation of the view ledger after a crash (fences,
  retirement tombstones, schema) is unaffected. A claim bound to an open
  earmark is KEPT, so a view controller's crash recovery, which re-closes
  only unclosed earmarks, never meets an abandoned claim.
- **The restart carrier.** The digest covers the whole ledger, so the new
  entry field (`Abandoned`, omitempty) and the `abandoned` root key are
  carried by the existing snapshot and compare. Entries written before hh4
  encode the same as before.

## Interactions

**0a4 participant fence.** A view-fenced session gets ErrSessionFenced for
every request that passes the view provider's `CheckRequest`. ABANDON does
not pass there, exactly as GAP_RESOLVE does not, so a fenced session can
still abandon. That is safe:

- never-prepared claims never took a cut, so the abandon installs nothing,
  admits nothing and cannot resume a stale view;
- it only gives up authority the session held, with the same effect class
  GAP_RESOLVE already grants a fenced session.

This matters, because a fenced guest is usually blocked too, and its
stranded claims could otherwise never be released.

**fh4 operator retirement.** `RetireFenced` releases the fenced session's
view-ledger rows. It does not touch the claims ledger. ABANDON does the
equivalent for the claims ledger, on the guest's word rather than the
operator's, and only for provably never-prepared claims. Neither one
depends on the other. Once a provider is sealed (SESSION_RETIRE), the
provider refuses ABANDON (`updateOpen`: Permission).

**dh4 blocked-session halt.** dh4 exempted DRAIN_WRITE, GAP_RESOLVE and the
gap QUERY from the halt that stops a blocked session's retries and polls.
ABANDON must be exempt in the same way, or it would die at its first FETCH
AGAIN, by timing alone (the s54 failure mode). Its other properties:

- like the dh4 exemptions, it is reserved control work and stays bounded: it
  runs to its reply or its 30 s deadline, and `transport_failed` still halts
  it at its first retry;
- the host never abandons a claim bound to an open earmark (KEPT), so the
  dh4 drain obligation is never cut short;
- the guest does not list gap requests, which it resolves through the
  exempt GAP_RESOLVE path.

**SESSION_RETIRE.** After ABANDON and the RETIRE with its proofs, only KEPT
claims can still hold `terminalEmpty` back. That is correct: they may have
had effects.

## The guest change

All of it is in the patch series under `tools/integration/linux9p/patches`,
regenerated with `export.py --task hh4`; `fs/9p` and `net/9p` build without
warnings with `CONFIG_9P_FS_HOMELABFS_CONTROLLED` on and off. The steps below
are the specification; where the build differs, the difference is noted at
the end of this section ("As built").

1. **0002 `hfs_policy.c`**: add `"ABANDON"` to `hfs_after_block_operation`'s
   exempt list, next to DRAIN_WRITE and GAP_RESOLVE. Extend the dh4 comment
   there and in `hfs_policy.h`: ABANDON is the blocked session's own
   reconciliation work, bounded by its deadline and `transport_failed`.
2. **0003 `hfs_session.c`**: add `"ABANDON"` to `hfs_operation`'s
   `commands[]`. Add the response check: `hfs_json_exact(reply, payload,
   "claims")`, then for each outcome `hfs_json_exact(..., "number,proof_hash,status")`,
   numbers in request order, `proof_hash` a 32-byte hex exactly on
   ABANDONED.
3. **0003, the trigger.** `hfs_session_block` must not send anything itself:
   it is called with locks held, from atomic-ish contexts and from inside
   exchanges. Instead:
   - add `struct work_struct abandon_work` to `struct hfs_session`, and
     `INIT_WORK` it in session setup;
   - in `hfs_session_block`, after `WRITE_ONCE(session->blocked, true)`, do
     `if (session->bridge_minor >= 2 && !test_and_set_bit(HFS_ABANDON_QUEUED,
     &session->block_reported)) queue_work(session->control,
     &session->abandon_work);`. `hfs_session_fence` reaches the same code
     when not unmounting;
   - `kill_sb` calls `flush_work(&session->abandon_work)` before its final
     `hfs_retire` and SESSION_RETIRE (its existing
     `flush_workqueue(session->control)` after `kill_anon_super` already
     does that), and queues the work itself if the session blocked silently
     (unmounting);
   - the work is queued once per session, so a request skipped at block
     time (`held`, or `users > 0` because its caller was still inside it) is
     never revisited by it. `kill_sb` therefore runs **one final collection
     pass** after that flush, synchronously and without the once-bit: by
     then every caller has returned (`kill_anon_super` evicted the inodes),
     so the requests skipped earlier qualify. The pass is the same worker
     body (step 4), called directly; it skips every request already
     `abandoning`.
4. **0003, the worker `hfs_session_abandon_work`:**
   - under `session->lock`, collect at most 64 requests from
     `session->requests` in ascending claim number with all of these: `!terminal`,
     `!gap`, `!abandoning`, `allocated` (the ALLOCATE was acknowledged),
     `!earmarked` (a drain is never abandoned), `!control`, `!held` and
     `users == 0` (no caller still waits on it);
   - still under the lock, set `request->abandoning = true` on each and
     **copy** its claim number and original hash into the worker's own
     payload buffer. The worker keeps no `struct hfs_request` pointer
     across the unlocked send;
   - build `{anchor, claims:[{number, original_hash}], nonce, realm,
     subject}` from `session->enrollment` and those copies, then drop the
     lock;
   - send it with `hfs_control_flagged(session, "ABANDON", &payload, true,
     hfs_after_block_operation("ABANDON"), &reply)`;
   - after the send, take `session->lock` again and look each outcome's
     request up **by claim number** in `session->requests`. A request that
     is gone meanwhile (retired or freed) is skipped. A request that turned
     TERMINAL meanwhile is left as it is (see the in-flight CALL below);
   - repeat until no request is left. A request that is still `abandoning`
     is never listed again in this session.
   - **The send needs no `session->control` slot.** I checked the patches:
     `hfs_control_flagged` runs the exchange synchronously in the calling
     thread (its FETCH/poll loop and waits), the same way
     `hfs_process_exit_work` and the FORGET work already call `hfs_call`
     from items on that queue. Nothing in the exchange path queues work on
     `session->control`; the only items queued there are the PROCESS_EXIT
     and FORGET works (0003). So the worker cannot wait on an item queued
     behind itself. It holds one of the queue's two slots (`max_active` 2)
     for at most one exchange deadline (30 s) per batch, which can delay a
     queued FORGET or PROCESS_EXIT by that much; on a blocked session those
     are exempt cleanup that tolerates the delay.
   - **A CALL already in flight can still finish.** A request may be listed
     while its own CALL or QUERY exchange is still on the wire (for example a
     reply in flight when the session blocked; `users == 0` does not rule
     out a worker-owned exchange). The host serializes the two: if that
     CALL's preparation began first, the host answers the claim KEPT, and
     the in-flight exchange may then deliver TERMINAL. The guest accepts that
     TERMINAL as usual (the request becomes terminal and is retired with its
     result). `abandoning` only forbids **new** CALL or QUERY sends (step 6);
     it never discards a reply. If the ABANDON won, the in-flight exchange
     can only get the no-effect answers listed above.
5. **0003, the outcomes, under `session->lock`:**
   - **ABANDONED**: set `request->gap = true`. `hfs_retire` then takes its
     existing `!terminal && gap` path (`provider->resolve_gap`). Its
     GAP_RESOLVE returns the same proof by construction (host test
     `TestAbandonedClaimRefusesLaterCallQueryAndGap`), and dh4 already
     exempts both the GAP_RESOLVE and its gap QUERY. Optionally, store the
     ABANDON proof in a new `request->abandon_proof[32]` and let
     `hfs_controlled_request_hash` (0005) use it, which saves one exchange
     per claim at retirement;
   - **KEPT, UNKNOWN or RETIRED_ID**: leave the request retained, as today.
     Keep `abandoning` set: the session is blocked and the guest promised
     never to poll it again.
   - **A failed or uncertain ABANDON exchange**: keep every listed request
     retained, as today. Resend the identical payload at most once more,
     only if `!transport_failed`; the host is idempotent. Never fall back
     to QUERY.
6. **0003, the promise.** `hfs_request_run`'s CALL->QUERY loop and the
   `hfs_retire` gap path must not send a CALL or QUERY for a request with
   `abandoning && !gap`. The blocked halt already stops ordinary polls; this
   makes the promise independent of timing.
7. **lib9p `hfs9p.c` (host-side C forwarder, also a local build):** in the
   command classifier, accept `ABANDON` for `minor >= 2`. Check
   `{anchor,claims,nonce,realm,subject}`, the claim list bound (1..64,
   ascending, number and 32-byte original_hash), and set `e->control =
   true`, like GAP_RESOLVE.

Tests to add with the guest change:

- `transport`: `hfs_after_block_operation("ABANDON")` is true.
- `controlled_received`: a blocked session with one stranded claim sends one
  ABANDON, maps ABANDONED to `gap`, and then retires the number.
- `controlled`: an old host's refusal keeps the claim retained and the
  session blocked, with no `transport_failed`. The refusal must come through
  the real first refusal path: the exporter's `_command` answering EINVAL
  for the unknown op on the guest's completing chunk (an unpatched
  `hfs9p.c`), not a host-side Go schema error.
- `controlled_received`: a request skipped at block time (`users > 0`) is
  abandoned by `kill_sb`'s final pass; a request whose in-flight CALL
  delivers TERMINAL after a KEPT answer is retired with its result.

### As built

- `hfs_session_abandon_queue` (called first thing after
  `hfs_session_mark_blocked` in `hfs_session_block`) also requires
  `session->control`: a session blocked before `v9fs_hfs_mount` allocated
  the queue queues nothing, and kill_sb's pass still runs.
- `kill_sb` does not queue the work for a silent block; it calls the pass
  (`hfs_session_abandon`) directly after `flush_workqueue(session->control)`,
  which covers a silent block, a request skipped by the worker and a request
  that became eligible after the worker ran. The pass needs
  `session->blocked` (kill_sb sets it) and minor >= 2.
- The original hash in the payload is the hash of the request's intent,
  which is how `hfs_call_prepare` made the claim's `original_hash`.
- Resend (step 5): one resend of the identical payload only for an
  uncertain failure. A definite refusal is not resent: `-EPROTO` (an
  exporter ERROR INVALID/SCOPE, which is how an exporter without ABANDON
  refuses it, or a malformed reply), `-ESTALE`, and the local refusals
  `-EOPNOTSUPP`, `-EINVAL`, `-EACCES`. Resending those would only park a
  second control cell in `session->uncertain`. A failed batch ends the pass
  with one `pr_warn` line ("ABANDON of N claims failed (err); they stay
  retained").
- The reply check also requires `verification` null. A malformed reply
  applies nothing.
- The abandon_proof shortcut (step 5, optional) is not built: an ABANDONED
  request becomes `gap` and `hfs_retire` resolves it with GAP_RESOLVE and
  its gap QUERY, which return the same proof. That QUERY expects DECLINED,
  which only a mapped 1.4 session gets (the host answers INDETERMINATE on
  1.2/1.3), so on 1.2/1.3 the gap is not retired by `hfs_retire`. That is
  unchanged from every other gap on those minors.
- The promise (step 6) is enforced at admission, not by a check before the
  send: `hfs_request_run` sends its CALL and every QUERY through
  `hfs_control_request` with the request, which refuses an `abandoning`
  request with `-EHOSTDOWN` under `session->lock` before a cell is numbered.
  An admitted exchange counts in `request->exchanges` until it ends, and the
  collection skips such a request. Both happen under `session->lock`, so
  once a request is listed no CALL or QUERY for it is admitted, and none
  admitted earlier is still being sent or retried. (The writer alone would
  not order them: `hfs_transport_send` yields it between CAPACITY retries.)
  This replaces design step 4's "A CALL already in flight can still finish":
  a request is never listed while one is in flight; it is left to a later
  pass (kill_sb's). A TERMINAL that already arrived is still kept (see
  "TERMINAL after KEPT" below). `hfs_request_resolve_gap` refuses `abandoning && !gap`.
  The REJECTED-CALL path now sets `gap` under `session->lock`, which the
  collection reads it under.
- Patch 0005: ABANDON is a cleanup operation for the controlled pre-send
  gate (`hfs_controlled_cleanup_operation`), because kill_sb's pass runs on a
  closing session.
- Eligibility keeps the design's `!control`: a cleanup request (FORGET,
  DESCRIPTION_RELEASE/CLOSE, PROCESS_EXIT, all sent with `control` true) that
  strands is not abandoned. The host predicate would allow it; widening the
  guest filter is a possible follow-up.
- `users > 0` is in practice only a result borrower, and such a request is
  already terminal. A request the worker misses is more often one that
  became eligible after the worker ran (an exchange in flight at block time
  that failed later); kill_sb's pass lists it.
- TERMINAL after KEPT: no listed request has an exchange in flight, but a
  request whose last QUERY already returned TERMINAL can still be listed
  before `hfs_request_run` records it (the exchange has ended; `terminal`
  and `users` are set only afterwards), when another exchange's failure
  blocked the session in that window. The host then answers KEPT and the
  guest keeps the result: `hfs_abandon_apply` never makes a terminal request
  a gap. That window is not exercised by a harness; the branch is
  source-reviewed.
- Resend after `-EPROTO`: the guest cannot tell an exporter ERROR INVALID
  from a malformed host reply by errno (both are `-EPROTO`, and
  `exporter_error` is sticky), so neither is resent. After a malformed
  reply the listed requests stay `abandoning` and retained, like KEPT ones.

Tests as built (all local, no guest boot):

- `transport`: `hfs_after_block_operation("ABANDON")`; red on the
  unpatched series.
- `mount_hello`: the first block of a minor-4 session with a control queue
  queues `abandon_work` once (`HFS_ABANDON_QUEUED`); a second block, a
  minor-1 session and a session without the queue do not; `hfs_session_new`
  prepares the item; every kill_sb runs exactly one pass, after the control
  flush and before the settle and stop.
- `controlled` (real host Transport/Service/provider): `abandon` blocks a
  session whose one claim is ALLOCATED and never called, and the pass sends
  exactly the expected bytes once (control class), the host stores the
  claim DECLINED/Gap/Abandoned with zero credit, the request becomes a gap,
  its GAP_RESOLVE succeeds, `hfs_request_run` sends no CALL even with the
  blocked flag lifted, and a second pass sends nothing. `abandon-old` runs
  the same guest against the exporter without ABANDON (`build.py
  --old-exporter` removes the classifier branch): the exporter refuses the
  SEND, nothing reaches the host (claim still ALLOCATED), the guest gets
  `-EPROTO`, does not resend, logs once, keeps the claim and stays blocked
  without `transport_failed`. `abandon-query` runs the actual collection
  at two points of one `hfs_request_run` on an unblocked session: while the
  admitted CALL's reply is fetched it lists nothing (in flight), and at the
  QUERY's admission it lists the request; no QUERY is sent, and the ABANDON
  then gets KEPT. It failed with the QUERY-side check removed (review 1,
  red first), and kills the mutants "no admission check" and "no in-flight
  check".
- `controlled_received --scenario abandon`
  (`TestControlledReceivedActualAbandon`, mapped 1.4, real
  Producer/runtime): the block queues the worker once; the worker skips a
  request a caller holds and sends nothing; the final pass sends one ABANDON
  with the exact bytes; ABANDONED becomes `gap`; RETIRE moves the floor to 2
  and the GAP_RESOLVE proof equals the ABANDON proof; a further pass sends
  nothing. A claim whose CALL ran (reply lost) is answered KEPT and stays
  retained; a rerun sends nothing for it (its CALL is refused at admission,
  so this does not exercise the QUERY side; `abandon-query` does).
- lib9p `backend_test`: `_abandon` accepts the closed shape (1 and 64
  claims) and refuses no claim, 65 claims, unordered or duplicate numbers,
  number 0, missing or short hashes, extra fields, an empty or odd subject,
  a missing field and a non-array `claims`; `_command` routes ABANDON on
  minor 2 and 3 as control work.

## Tests (host side)

- `internal/controlledvalidation/abandon_test.go`:
  - `TestAbandonReturnsCreditOfNeverPreparedClaims`: credit returned, entry
    credit zero, proofs are the entry hashes, OBSERVE counters, RETIRE with
    the proofs succeeds and recycle releases nothing twice;
  - `TestAbandonedClaimRefusesLaterCallQueryAndGap`: a later QUERY, CALL or
    ALLOCATE (and its CALL) is refused or has no effect; GAP_RESOLVE returns
    the same proof;
  - `TestAbandonKeepsPreparedClaims`: PREPARING (the preparer ran) and
    PENDING are KEPT, with entry, credit and counters unchanged;
  - `TestAbandonIsBoundToSession`: another scope is refused by the bridge
    and by the provider; a claim homed in another session, or a wrong
    original hash, refuses the whole command with nothing written;
  - `TestAbandonCrashReplay`: an aborted transaction leaves nothing; reopen,
    commit and replay are idempotent.
- `internal/controlledvalidation/abandon_states_test.go`:
  - `TestAbandonBetweenAdmissionAndPreparation`: an ABANDON run inside
    Admit, between execute's first `loadExact` and `executeAdmitted`'s
    locked reload, declines the claim; no preparer runs and no file exists;
  - `TestAbandonKeepsEarmarkedAndRefusedClaims`: a claim bound to an open
    earmark, an earmark-only number and a definite admission refusal
    (TERMINAL) are KEPT with nothing written;
  - `TestAbandonExistingGapAndRetired`: a GAP_RESOLVE gap answers ABANDONED
    with its own proof and no credit or counter change; retired numbers
    answer RETIRED_ID.
- `internal/guestbridge/abandon_test.go`: the closed codec (minor 2/3/4
  only, list bounds and order, extra fields) and the reply shape check.
- `internal/controlledruntime/abandon_test.go`: the real runtime path (the
  router, the Producer's preparation and Facts authorization, and the 1.4
  mapped transport). Another session reaches only its own ledger (UNKNOWN);
  the proof retires the number.
- `TestClaimEvidenceWorstCaseBound` now includes the counters at their
  maximum, and the router guard test includes `AbandonOriginal`.
