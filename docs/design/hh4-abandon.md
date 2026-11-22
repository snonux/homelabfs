# hh4: a blocked session abandons its stranded claims (design)

This is part 2 of a84. Part 1 added the refusal trace. Both are described in
[internal/guestbridge/README.md](../../internal/guestbridge/README.md),
section "Swallowed refusals and stranded RESERVED claims (a84)".

Status: the host side is implemented (Go, this task). The guest side is
specified below but not built: it needs a local kernel build, and the
patches under `tools/integration/linux9p/patches` were not edited.

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
3. It fails safe on an old host. An older host refuses ABANDON as an
   unsupported operation (the closed schema), and the guest keeps its claims
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
| number <= floor | RETIRED_ID | none |
| bound to an open dirty earmark | KEPT | none (a DRAIN_WRITE byte obligation, dh4) |
| no entry | UNKNOWN | none |
| entry claim differs (original hash) | whole command Stale | rolled back |
| protected original context is not this scope | whole command Permission | rolled back |
| proven gap (GAP_RESOLVE, Cancel, an earlier ABANDON) | ABANDONED, current proof | none, never rewritten |
| `abandonable` | ABANDONED, new proof | Gap, DECLINED, Abandoned; both credits released and zeroed |
| anything else (PREPARING, PENDING, TERMINAL, ADMISSION_REFUSED) | KEPT | none |

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

## The guest change still needed (not built)

All of it is in the patch series under `tools/integration/linux9p/patches`,
regenerated with `export.py --task hh4`. Then rebuild `fs/9p` and `net/9p`
with `CONFIG_9P_FS_HOMELABFS_CONTROLLED` on and off.

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
     `hfs_retire` and SESSION_RETIRE, and queues the work itself if the
     session blocked silently (unmounting).
4. **0003, the worker `hfs_session_abandon_work`:**
   - under `session->lock`, collect at most 64 requests from
     `session->requests` in ascending claim number with all of these: `!terminal`,
     `!gap`, `allocated` (the ALLOCATE was acknowledged), `!earmarked` (a
     drain is never abandoned), `!control`, `!held` and `users == 0` (no
     caller still waits on it);
   - set `request->abandoning = true` on each, still under the lock;
   - build `{anchor, claims:[{number, original_hash}], nonce, realm,
     subject}` from `session->enrollment` and each request's claim bytes;
   - send it with `hfs_control_flagged(session, "ABANDON", &payload, true,
     hfs_after_block_operation("ABANDON"), &reply)`;
   - repeat until no request is left. A request that is still `abandoning`
     is never listed again in this session.
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
  session blocked, with no `transport_failed`.

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
- `internal/guestbridge/abandon_test.go`: the closed codec (minor 2/3/4
  only, list bounds and order, extra fields) and the reply shape check.
- `internal/controlledruntime/abandon_test.go`: the real runtime path (the
  router, the Producer's preparation and Facts authorization, and the 1.4
  mapped transport). Another session reaches only its own ledger (UNKNOWN);
  the proof retires the number.
- `TestClaimEvidenceWorstCaseBound` now includes the counters at their
  maximum, and the router guard test includes `AbandonOriginal`.
