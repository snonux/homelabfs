# zq3-r17: READY and Phase 9 again; m0 is admitted, but m1's root fstat blocks its session when a recall-delayed grant arrives before the guest has settled (EKEYREJECTED) (2026-11-20)

Run sheet: [filled plan](../20261120-zq3-r17-filled-plan/run-sheet.md)
(the run log records no run-time revision).
Candidate r17 `f614d52a…` ([xz3-20261120-r17](../../../../tools/integration/artifacts/evidence/xz3-20261120-r17/README.md)):
the re4 guest kernel (build ID `360fbc6d…`) and the qe4/te4 daemon
`85900893…`, both running for the first time, with the exporter rebuilt
natively on f3 (byte-identical `2fd7e32f…`) and the runtime spec's
`ControlTickets` raised from 8 to 24. T5 stayed deferred. Times are UTC.
Private evidence: `/home/paul/tmp/claude/hfs-zq3-r17-fill/evidence` (run log
`RUN-LOG.md`), controller output
`/home/paul/tmp/claude/hfs-zq3-r17-fill/ctrl-run/credentials/`, bootstrap
journal `/home/paul/tmp/claude/hfs-zq3-r17-fill/bootstrap-run/`. The
controller, transport and provider ledgers of the owner snapshots
(`credentials-owner-live.tar`, taken right after T1,
`credentials-owner-after.tar` and the copy in `f3-evidence.tar`) and the
forwarding ledger were decoded offline from copies with the r15 decoders
and a read-only bbolt dump.

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed during guest admission
(new finding R17-1), so T2-T11 did not run (the sheet allows no retry).
Phase 11 took the guard-SIGTERM branch, and restoration was clean.

**What held:**
- Phase 1a unloaded `fusefs` (id 34) and `fdescfs` (id 33) again (Refs 1/1,
  no mounts, both `rc 0`). Phase 4 step 1a qualified the **new daemon
  `85900893…`** (uid/gid 0, groups `[0,0,5]`, pid 42431).
- Phase 3: kernel BLS `c211d6d2…`; the T7 pod rootfs manifest is
  `34a71569…`, as expected.
- Phase 5: the active-based estimate left a margin of only 17 MB
  (16,986,624 bytes) and recommended the UMA drain; it freed 106,164 pages
  (about 0.43 GB; `uma_free_bytes` 469 MB → 41 MB). Rocky stopped at
  15:34:11Z. Phase 6 passed with a 0.38 GB margin (reclaimable
  15,146,696,520 bytes against a bar of 14,763,950,080).
- Phase 8: the watcher made both operator signatures (enroll, then bind
  2.9 s later). mount-0 (mountinfo 391) and mount-1 (408) both came up with
  the exact g64 tuple. READY came 31 s after launch (15:34:25Z →
  15:34:56Z). The `usable` gate passed (claims 2.59 s after the final call,
  `observe_sequence_last` L=2).
- Phase 9 (budget GO at 15:35:52Z, 3512 s left): three SSH no-ops, OBSERVE
  at L+1=3 passed (both domains `REOPENED`, fence 3, claims `Allocated 6,
  Open 0`), mountinfo had exactly the two tuples, `Enforcing`, the SELinux
  `Initializing` line, no hfs block line.
- 984 timing (final): SSH no-op median 0.215 s (3 samples), OBSERVE net
  median 0.832 s, max 1.155 s (2 samples), below the 2.4 s bound.
- **The R16-1 shortage is gone:** no `VIEW_ACQUIRE` was answered BLOCKED
  in T1, so re4's back-off never ran. m0's root OPEN and `os.fstat` both
  succeeded and m0 was admitted (`root-admitted[0]` 11.8 s after
  `identity-checked`). m1's root OPEN also succeeded this time (its re-check
  logged the ie4 line and acquired again). No transport fence
  (`fenced: false`), `runtime.stderr` empty.
- When the guard stopped the runtime, m0 blocked at uptime 151.6 s with
  `VIEW_WAIT (-112), exporter ERROR 5`, and both `umount`s returned `rc 0`.
- No AVC records, no relabel. U2 removed the test kernel.

**New finding:**
- **R17-1:** T1's m1 root `fstat` (`stat-root[1]`, 25.2 s after
  `identity-checked`, 12.5 s after `open-root[1]`) failed 5.9 s later with
  `OSError` errno 129 (`EKEYREJECTED`) on `/var/tmp/hfs-zq3-zq3-r17/m1`.
  The guest blocked m1's session at uptime 129.94 s: `blocked by uncertain
  view ticket on VIEW_QUERY (-129), exporter ERROR -`. The probe wrote no
  `terminal` line (its own traceback ends in `PermissionError` errno 13,
  most likely a later step on the blocked mount, as the block line
  predicts), and the controller recorded `dispatching-uncertain`
  (`Refused: missing complete guest terminal; remote state uncertain`).
  See the analysis below.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (new daemon `85900893…` qualified, nightly gate GO 15:31Z and 15:33Z, UMA drain run, Phase 6 margin 0.38 GB) |
| 8 | passed (mount-0 and mount-1 ok, READY 31 s after launch, `usable` L=2) |
| 9 | passed (OBSERVE L+1=3; 984 timing) |
| T1 | **fail** (R17-1) |
| T2-T11 | unrun (no retry after T1; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

T1 started at 15:36:09Z with the guard budget GO (3495 s left). The
baseline OBSERVE at L+2=4 passed (both domains `REOPENED`, revision 10,
fence 3). The probe started at 15:36:10.6Z and ran 32.8 s. Phase 11 ran
the guard-SIGTERM branch: the final 984 sample, then one SIGTERM to guard
43890 at 15:37:03Z. The guard was gone by 15:37:22Z. Receipt:
`runtime_exit 0`, only the `guard-interrupted SIGTERM` breach (158.3 s),
runtime not running at guard exit, exporter (pid 43901) still alive. Then:
evidence tarballs, the fence record (`fenced: false`), the unmounts
(above; no new block lines), observer removed (`authorized_keys` back to
`e08542dc…`, `restored_pre_run: true`), `next_entry` empty, no AVC, full
dmesg (460 lines), poweroff, the bhyve child gone at 15:37:35Z, host cleanup
(tap and vmm destroyed, UEFI vars `49f5c851…` unchanged), native FUSE
unmount, the observer key deleted on f3 and the window restore
(`mount_restored: true`, `fusefs.ko` kept loaded by the reviewed plan).
Phase 12: `start-rocky` at 15:37:52Z, SSH at 15:38:12Z, `check --expect
stock` ok, no `/.autorelabel`.

## R17-1: a verified GRANTED reply is refused because the guest's view had not settled

### What the evidence shows

1. **Guest timeline** (`stdout.log`, guest monotonic): `identity-checked`
   98.82 s, `open-root` m0 99.35 s, `stat-root` m0 109.89 s,
   `root-admitted` m0 110.64 s, `open-root` m1 111.53 s, `stat-root` m1
   124.04 s, `failure` (`[Errno 129] Key was rejected by service`)
   129.90 s. Guest kernel lines in T1 (uptime): 107.63 s ie4 re-check line
   for m0 (`8cc39c17…`), 117.67 s the same for m1 (`b3e81660…`), 129.94 s
   m1 `blocked by uncertain view ticket on VIEW_QUERY (-129)`, 129.95 s
   m1's view worker stopped (`-112`, view phase `LIVE`). No BLOCKED,
   back-off, fence or exhaustion line.
2. **Three all-domain recalls in T1:** both domains went from fence 3
   (baseline OBSERVE) to fence 6 in the controller ledger (`Fences`), both
   ended at event sequence 20 with ACKs archived for every phase of the last
   recall (sequences 17-20), and `Recall` was null at the end.
3. **m1's tickets** (controller ledger, live and after snapshots, which
   are identical): one acquisition family (High 5, Floor 0) and one release
   family (High 4, Floor 0):

   | Ticket | Initial | Current |
   |---|---|---|
   | acquire #1 | GRANTED lease `425e9755…` fence 4 | same |
   | acquire #2 | PENDING | GRANTED lease `e718286a…` fence 5 |
   | acquire #3, #4 | GRANTED `e718286a…` (shared) | same |
   | **acquire #5** | **PENDING** | **GRANTED lease `6d07798f…` fence 6** (still `Active`, release credit reserved) |
   | release #1 (`425e9755…`) | PENDING | RELEASED |
   | release #2, #3 (`e718286a…`) | RELEASED | same |
   | release #4 (`e718286a…`, last holder) | PENDING | RELEASED |

   All five acquisitions name the same target (journal position 2); only
   the fence differs. No ticket was BLOCKED, none is retired, and none
   answered `RETIRED_ID`.
4. **The failing exchange** (forwarding ledger): m1 guest cell 333
   (native 1907), `VIEW_QUERY` for acquisition #5, answered `GRANTED` with
   lease `6d07798f…` fence 6 and a verified `SOURCE` receipt
   (`verification.kind` `SOURCE`, the expected target). Guest cell 334
   (native 1908), the poll of release #4, got `RELEASED` right after it:
   the recall that release #4 started had ended. The host does not grant a
   PENDING ticket when a recall ends (`finishRecall`/`advanceFences`,
   `internal/viewcontrol/fence.go`, only raise the fences); it re-checks
   the ticket lazily on the next `VIEW_QUERY` (`queryOrCancelTicket` →
   `readmit`, `tickets.go`). So cell 333 itself produced the grant, after
   the host had accepted m1's REOPENED ACK and raised the fence. m1's `VIEW_WAIT` with `after_sequence`
   20 (cell 329) was sent before cell 333, so m1 had already received the
   REOPEN event of that recall when it sent the query.
5. **Where -129 comes from:** `hfs_view_exchange` (patch 0004) calls
   `hfs_received_source` (patch 0005) on every hfs1.4 `GRANTED`/`FROZEN`
   reply, and any error there goes to `invalid:`, which marks the ticket
   uncertain and blocks the session ("uncertain view ticket").
   `-EKEYREJECTED` from `hfs_received_new` (receipt verification) would
   fail the exchange earlier and not print this line. Inside
   `hfs_received_source`, `-EKEYREJECTED` has several exits: no owner found
   for the reply, a session, channel or frame mismatch, a token span
   mismatch, or an owner that is not of kind SOURCE. The last is the most
   likely: grant #2 passed the same path with the same reply shape, so the
   lookup, channel, frame and span checks work. `hfs_received_new`
   gives that kind only if `hfs_received_source_current` holds **at
   receipt**: the lease fence equals the mount's `current_fence`, the view
   revision and phase equal the ones captured when the command was sent,
   the view is `LIVE` (or `UNBOUND`) and the lease target equals the view
   target. Otherwise it keeps the verified reply as a raw outcome
   (`/* Raw outcome, no stale adoption. */`), and the later consumption
   check reports it as if it were unverified.
6. **A failed REOPENED ACK is ruled out:** `hfs_controlled_after_ack`
   failing would stop m1's view worker at the ACK step. The worker-stop
   line at 129.95 s, after the block, shows `at VIEW_WAIT` with `ack -`
   and view phase `LIVE`: the worker stopped only because the session was
   already blocked.

### Why the grant was not current (inference)

The guest moves its view to `LIVE` with a new revision on the REOPEN
event (`hfs_view_reopen`), then sends the REOPENED `VIEW_ACK`, and advances
`current_fence` only after that ACK's reply (`hfs_controlled_after_ack`).
The host raises both domains' fences (`advanceFences`,
`internal/viewcontrol/fence.go`) when the last REOPENED ACK completes the
recall, and the next `VIEW_QUERY` of the PENDING acquisition re-admits it
at fence N+1. The guest re-queries a PENDING ticket every 10 ms, so a
query is very likely to reach the host right after it accepted m1's ACK,
while the guest is still between "view reopened" and "own ACK reply
processed". That query comes back with a lease at fence 6 to a mount
still at fence 5, or with a revision captured before the reopen. Either way it is
not current at receipt, and the host's correct, verified grant becomes a
fatal `-EKEYREJECTED`. Acquisition #2 took the same PENDING → GRANTED
path one recall earlier and succeeded (as #2 did in zq3-r16), so this is
a timing race, not a deterministic refusal. The ledgers do not show
whether the fence or the revision differed: the guest keeps no record of
its state at that receipt.

### Where the 10-12 s per root open went (inference)

Each observation's lease is released when its packet ends; the last
holder's release starts a recall, and here every recall involved both
domains (both fences 3 → 6). A recall needs four events per domain, each
acknowledged over a control round trip (OBSERVE net 0.51-1.16 s), so one
recall costs several seconds: `stat-root[1]` → failure, which is exactly
release #4's recall plus #5's grant, took 5.9 s. An open whose observation
starts while such a recall runs waits PENDING for it, and ie4 then
re-acquires after the moved view. That matches m0's open (10.5 s, ie4 line
8.3 s after `open-root[0]`) and m1's (12.5 s, ie4 line 6.1 s after
`open-root[1]`; release #1's recall, fence 4 → 5). The stdout markers and
the dmesg stamps are both since guest boot but come from different clocks
(the guest marked its TSC unstable at 13.5 s), so these splits are only
approximate.

### Did re4 cause it?

No. re4 changed the BLOCKED path only: back-off after a BLOCKED
`VIEW_ACQUIRE`, release of the held lease on BLOCKED, and the re-sent
`VIEW_RETIRE`. No acquisition was BLOCKED and no back-off line was logged,
so the first two never ran. m1's families stayed at floor 0 (nothing
retired), ticket #5 is still present with Current GRANTED, and the failing
reply was GRANTED, not `RETIRED_ID`, so no retired or unknown ticket was
involved. m1's only recorded `VIEW_RETIRE` (cell 337, answered BLOCKED)
was sent after the failing query and touched no acquisition ticket. re4 and
qe4 did remove R16-1, which let m1 get past its open
and into this older race in `hfs_received_source_current`, which T1 had
not reached before.

**Confidence:**
- high: the `EKEYREJECTED` is `hfs_received_source` refusing the GRANTED
  `VIEW_QUERY` reply for m1's acquisition #5 (lease `6d07798f…`, fence 6;
  guest cell 333), through `hfs_view_exchange`'s `invalid:` path;
- high: the host's reply was correct and verified (SOURCE receipt,
  expected target, ticket Current GRANTED, fence 6 = the domain fence);
- high: re4's new paths did not run, and no retired or unknown ticket was
  involved;
- medium: the reply was not current because it crossed m1's own REOPEN →
  REOPENED ACK window (fence 5 vs 6, or a revision captured before the
  reopen); the guest left no record of which;
- medium: the 10-12 s per open are whole release recalls waited out by
  PENDING acquisitions.

### Recommended fix

1. **Guest (patch 0005 `hfs_received_new`/`hfs_received_source`):** keep a
   verified SOURCE that is merely not current as its own state (for
   example a `stale` flag on the owner) and return `-ESTALE` for it, so
   `-EKEYREJECTED` again means only "not verified". `hfs_received_source`
   already returns `-ESTALE` for a source that was current at receipt but
   is not at consumption (the `hfs_received_source_current` re-check),
   and that also blocks the session today. The new handling must cover
   both cases, and the other `hfs_received_source` callers in patch 0004
   (the reservation and frozen-entry paths) must be checked for the same
   treatment.
2. **Guest (patch 0004 `hfs_view_exchange`), lease `VIEW_ACQUIRE` replies
   only:** on `-ESTALE` from a GRANTED lease reply, do not block the
   session or mark the ticket uncertain. Wait on the REOPENED ACK path
   (bounded by the exchange's 30 s deadline, stopping on a blocked session
   or failed transport) until the view is `LIVE` and the mount's
   `current_fence` has reached the lease fence, then send `VIEW_QUERY`
   again. That is safe: a re-sent `VIEW_QUERY` on a GRANTED ticket returns
   the stored reply and creates no new ticket, identity or lease, and a
   fresh capture validates it. If the lease fence is behind
   `current_fence` (a later recall passed it), give the lease to the
   observation's packet and return `-EXDEV`, so ie4 acquires again and the
   packet releases it (the host still counts the lease as active).
   `-EXDEV` fits only the ie4 observation path (`hfs_mmap_observe`). The
   same exchange also serves `DIRTY_RESERVE`/`DIRTY_FREEZE`, whose entry
   sources are current only while the view is `QUIESCENT`/`STOPPED`; a
   wait for `LIVE` would time out there, so those keep their own rule.
3. **Tests:** a guest test that delivers a GRANTED reply at fence N+1
   before the REOPENED ACK reply (and one with a revision captured before
   the reopen) and expects a re-query, not a block; a host
   `internal/viewcontrol` test that `VIEW_QUERY` keeps answering the same
   GRANTED lease after the recall completes.
4. **Fewer release recalls** (lease linger or a recall-free clean DOMAIN
   release, already a follow-up from zq3-r15/r16) would cut the 10-12 s per
   open and the number of grants that land in this window.

## Host events and facts

- f3 boot `1794574308` unchanged. N1: Rocky clock `Europe/Sofia`, stamp
  `2026-11-20`, newest dnf transaction #65 (expected); nightly gate GO at
  15:31Z (Phase 1) and 15:33Z (Phase 5 step 0).
- Rocky before: PID 78540 (UTC lstart `Fri Nov 20 05:05:29 2026`), boot
  `302a5df1-…`, stock `741.22.1`, pod runtime unchanged. Stopped
  (`stop-rocky` rc 0) at 15:34:11Z, started normally at 15:37:52Z: PID
  46196 (UTC lstart `Fri Nov 20 15:37:52 2026`), boot
  `1d9d3252-5c16-4275-af2c-45328f733c93`. **Rocky was down about 4
  minutes** (15:34:11Z to 15:37:52Z).
- Phase 4: window plan WP=`0ad65223…`, descriptor execution through
  fdescfs supported; observer key enrolled (EA `e62d9c0c…`).
- Phase 7: volume `5e11d915-…`, K04 baseline recorded.
- After READY: B=`93cc1d22…` (cases credentials, sync, metadata,
  namespace; T5 deferred), U=`b02dac1a…`, L=2. The guest requests were
  written (lock-m0 `38bf44ff…`, lock-m1-t8 `7f0a96e5…`, pod `179b09da…`)
  but never used.
- Guard peaks: disk 23.7 MB, exporter RSS 307 MB, runtime RSS 38 MB.
- Final f3 state: Rocky running (PID 46196), `/dev/vmm` holds only
  `rocky`, no fuse/fdesc mount, `fusefs.ko` loaded by the reviewed window
  plan. Rocky has no `hfs-xz3` kernel (U2).

## Follow-up tasks

- **R17-1 → guest grant race:** distinguish a verified-but-not-current
  SOURCE (`-ESTALE`) from an unverified one, and re-query after the view
  settles instead of blocking the session, with the guest and host tests
  above. Then a kernel rebuild and **zq3-r18** with a fresh fill to run
  T1-T11.
- Fewer release recalls (lease linger or a recall-free clean DOMAIN
  release), to bring each guest operation back under a second.
- Still open: ce4 (recall history), e84 (target-mismatch recalls are
  all-domain), s64 (fd-based GETATTR/SETATTR carry no description), qc4
  (zq3-r12), 0a4 (zq3-r11).
