# zq3-r18: READY, Phase 9 and a 7 s admission; T1 gets past admission for the first time, then the guest kernel refuses fchown of a regular file (EOPNOTSUPP) (2026-11-21)

Run sheet: [filled plan](../20261121-zq3-r18-filled-plan/run-sheet.md),
review round 1 applied before the run (merge `557c5b1`).
Candidate r18 `b2a4f69e…` ([xz3-20261121-r18](../../../../tools/integration/artifacts/evidence/xz3-20261121-r18/README.md)):
the 2f4 guest kernel (build ID `020acb2e…`) and the je4/ce4/e84 daemon
`086d7cde…`, both running for the first time. The exporter `2fd7e32f…`
was reused, and `ControlTickets` stayed at 24. T5 stayed deferred. Times
are UTC.

Private evidence:
- `/home/paul/tmp/claude/hfs-zq3-r18-fill/evidence`, with the run log in
  `RUN-LOG.md`;
- controller output in
  `/home/paul/tmp/claude/hfs-zq3-r18-fill/ctrl-run/credentials/`;
- the bootstrap journal in `/home/paul/tmp/claude/hfs-zq3-r18-fill/bootstrap-run/`.

## Before the run (Paul's decisions, 2026-11-21)

- **iom1 and iom2** are two 2 GiB f3 VMs that are not part of HomelabFS.
  - Claude stopped them at 08:54Z on Paul's instruction.
  - Paul's iormesh session started them again 25 s later, unaware of the
    run.
  - Paul then paused iormesh, and Claude stopped them again at 11:43Z.
  - They were restarted after Phase 12 (12:00Z). The iormesh session was
    told at each step.
  - Running them next to the run is impossible: the Phase 6 bar of
    13.75 GiB had only about 0.3–0.4 GB to spare even with both stopped.
- **f3 swap:** 883,332 KiB (42 %) was in use, most likely pushed out while
  iom1/iom2 ran next to Rocky. `runtime-guard.py` allows no swap.
  - With Paul's approval Claude ran `swapoff -a` and then `swapon -a`
    (11:43:39Z), with 7.5 GiB free; swap was then 0.
  - Side effect: Rocky's paged-in memory showed as 1.09 GB of laundry
    pages in Phase 5's estimate (see below).

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) admitted both mounts in 7.3 s,
then failed on its first directory (new finding R18-1). T2-T11 did not run;
the sheet allows no retry. Phase 11 took the guard-SIGTERM branch, and
restoration was clean.

**What held:**
- **Phase 1a:** both modules unloaded normally (fusefs id 35, fdescfs
  id 34, Refs 1, `rc 0` each).
- **Phase 3:** kernel BLS entry `b26c0ac5…-6.1.190-hfs-xz3`; the pod rootfs
  manifest is the predicted `34a71569…` (82 entries).
- **Phase 4 step 1a** qualified the **new daemon `086d7cde…`** (uid/gid 0,
  groups `[0,0,5]`, pid 54156). Window plan WP is `1a8f4519…`;
  `check-descriptor-exec` reports fdescfs as supported.
- **Phase 5:**
  - Nightly gate GO at 11:45Z and 11:49Z.
  - The active-based estimate was **1.25 GB short** of the bar. Active
    pages were only 0.67 GB and Rocky's RSS 0.71 GB, because the swap
    clearing had put Rocky's pages into laundry (1.09 GB), which the
    estimate does not count.
  - The UMA drain freed 129,947 pages (0.53 GB; `uma_free_bytes` 661 MB
    → 141 MB). Rocky stopped cleanly at about 11:50Z.
- **Phase 6** passed with a **0.26 GB margin**: reclaimable 15,028,086,192
  bytes against a bar of 14,763,950,080, swap 0, `descriptor-equals-owned`
  True.
- **Phase 8:**
  - The watcher made both signatures.
  - mount-0 (mountinfo 391) and mount-1 (410) came up with the exact g64
    tuple.
  - READY came **38.3 s after launch** (11:50:58Z → 11:51:36Z).
  - `usable` passed (L=2, both providers Open 0).
- **Phase 9** (budget GO, 3492 s left): OBSERVE at L+1=3 passed (both
  domains `REOPENED`), with two mounts, `Enforcing`/1, the SELinux
  `Initializing` line and no hfs block line. The `LSM:` line was missing
  from the ring buffer (recorded only).
- **984 timing** (final): SSH no-op median 0.407 s; OBSERVE net median
  0.652 s and max 0.832 s over 3 samples, below the 2.4 s bound.
- **Admission is fast now (je4/e84):** T1 went from `identity-checked` to
  `admitted` in 7.27 s. r17 took 10-12 s per root, and r14 about 17 s.
  `deadline_hit` and `etimedout` were false.
- **R17-1 is gone (2f4):** no `EKEYREJECTED`, no uncertain-view-ticket
  block, and both roots passed OPEN and `fstat`.
- **ce4 works:** OBSERVE carries `controller.recalls`. `recalls.py`
  decoded seven recalls (below).
- **R13-1 stays fixed:**
  - After the guard stop, both sessions logged `view control worker
    stopped, error -112, exporter ERROR 5` at uptime 344.7 s.
  - m0 also logged `blocked by uncertain control result on VIEW_WAIT`.
  - Both `umount`s returned `rc 0` within 1 s.
- **Cleanup:** no transport fence (`fenced: false`), `runtime.stderr`
  empty, no AVC records, no relabel. U2 removed the test kernel.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (new daemon `086d7cde…` qualified, nightly gate GO, UMA drain run, Phase 6 margin 0.26 GB) |
| 8 | passed (both mounts, READY 38 s after launch, `usable` L=2) |
| 9 | passed (OBSERVE L+1=3; 984 timing) |
| T1 | **fail** (R18-1) |
| T2-T11 | unrun (no retry after T1; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

## R18-1: the guest's hfs_setattr refuses the kill bits that every chown of a regular file carries (EOPNOTSUPP)

### What the evidence shows

**T1:**
- Budget GO at 11:53:31Z; the baseline OBSERVE at L+2=4 passed.
- `probe` started at 11:53:37Z and ended after 76.0 s with
  `dispatching-uncertain` ("required actual syscall case failed:
  credentials").

**The probe's own log** (`ctrl-run/credentials/stdout.log`):
- `admitted` at +7.3 s;
- `invoke mkdir-exclusive zq3-zq3-r18-credentials` (sequence 1), whose
  reply came 18.0 s later with errno 0;
- 48 s later, with no further `invoke`, a `failure`: `OSError` errno 95
  (`EOPNOTSUPP`, "Operation not supported"), then `terminal`.

Without an `invoke`, the failing call was one of `guest.py`'s plain
syscalls after `p.directory()`'s mkdir. The ledger shows which one (see
the analysis below).

**Guest kernel log** (`t-fail-guest-dmesg-full.txt`):
- At uptime 178.40 s, during the mkdir, two `WARNING`s from the probe's
  python3 (PID 2601):
  - `fs/dcache.c:1924 d_set_d_op+0x67`;
  - `fs/dcache.c:1925 d_set_d_op+0x72`.
  - Both have the call trace `hfs_namespace+0x365` ← `vfs_mkdir` ←
    `do_mkdirat`. Line 1924 is the warning for a dentry that already has
    `d_op`; line 1925 is the one for a dentry that already has
    `DCACHE_OP_*` flags.
- At 189, 200, 212 and 224 s, `9p-hfs: session 558f85f5… (m0) observation
  VIEW_ACQUIRE BLOCKED by the host (attempt 1); retiring, then retrying
  after 52 ms if that frees anything` (re4's back-off).
- No block line or worker stop before the guard stop.

**Host recall history** (ce4, `recalls-t1-fail.json`). All seven recalls
completed, and the longest single ACK block was 0.46 s:

| Start (Z) | Trigger | Scope | Duration |
|---|---|---|---|
| 11:51:20.9 | RELEASE/VIEW_RELEASE (bootstrap, m0) | DOMAIN | 5.4 s |
| 11:51:28.6 | RELEASE/VIEW_RELEASE (bootstrap, m1) | DOMAIN | 7.0 s |
| 11:53:50.2 | CALL_CONFLICT/MKDIR (m0) | ALL | 13.1 s (INSTALL_FINAL 6.0 s) |
| 11:54:03.7 | EXPECTED_TARGET/VIEW_ACQUIRE (m0) | ALL | 9.7 s |
| 11:54:18.7 | EXPECTED_TARGET/VIEW_ACQUIRE (m0) | DOMAIN | 6.3 s |
| 11:54:28.2 | EXPECTED_TARGET/VIEW_ACQUIRE (m0) | ALL | 9.5 s |
| 11:54:41.4 | EXPECTED_TARGET/VIEW_ACQUIRE (m0) | ALL | 7.5 s |

How this lines up:
- The MKDIR recall matches the 18 s mkdir.
- The four VIEW_ACQUIRE recalls match the guest's four BLOCKED lines, one
  for one, about 11 s apart.
- The failure came at about 11:54:50Z, just after the last recall ended.

### Analysis

The analysis was done offline against the r18 kernel source (patch 0003 at
HEAD) and the decoded controller ledger from `credentials-owner-after.tar`.
Kernel source: `/home/paul/tmp/claude/hfs-xz3-20261121-r18/linux-source`.

**1. The failing syscall is `fchown` on the new file `group`, and the
guest refused it locally.**
- The newest retained cut (catalog position 7) holds:
  - the leaf at mode **0o777**, so `fchmod(directory, 0o777)` reached the
    host;
  - the entry `group`, a regular file with mode 0o640, so its CREATE
    committed;
  - but `group` still has uid:gid **0:0**.
- So `create_owned_file`'s `os.fchown(fd, 41002, 42002)`
  (`tools/lab/retained9p/guest.py:703`, called from `credentials_groups`)
  never reached the host. No host ledger has an `Unsupported` code.
- Where the error comes from in Linux:
  - `chown_common` (`fs/open.c:727-729`) adds `ATTR_KILL_SUID |
    ATTR_KILL_PRIV` (and `ATTR_KILL_SGID` when needed) for every
    non-directory;
  - `notify_change` (`fs/attr.c:431-466`) passes the kill bits down;
  - `hfs_setattr` (`fs/9p/hfs_inode.c:962-973`, patch 0003 ~5174) allows
    only `MODE|UID|GID|SIZE|times|FILE|OPEN|FORCE` and otherwise returns
    `-EOPNOTSUPP` before `hfs_begin`.
- `fchmod` of the directory passed, because chmod sends only
  `ATTR_MODE|ATTR_CTIME`.
- The check has existed since the tz3 import (`35c3e44`). No earlier run
  got past admission to this point:
  - r10, r11 and r14 hit deadlines;
  - r15 failed with ESTALE;
  - r16 failed with EAGAIN;
  - r17 failed with EKEYREJECTED.
- It is not re4 giving up, which would return `-EAGAIN` with an "after N
  attempts" line. All four BLOCKED lines say "attempt 1", and each retry
  was granted.

**2. The `d_set_d_op` WARNINGs are a harmless double initialisation, also
from tz3.**
- `hfs_lookup` turns a negative lookup into success and always sets
  `hfs_dops` (`:589-591`).
- `hfs_namespace` (`:779`, after MKDIR) and `hfs_atomic_open` (`:703`,
  after CREATE) set it a second time.
- The second call stores the same ops and flags, and the old `d_fsdata`
  is NULL. The only effect is a `W` taint from 178.40 s on.
- The CREATE call is silent only because the WARN fires once per boot.

**3. Each BLOCKED VIEW_ACQUIRE is the expected-target protocol working,
slowly.**
- On the guest, every non-mutating `hfs_begin` asks for an observation
  lease on the view's installed target (`hfs_view.c:237-256`,
  `hfs_mmap.c:2230`). That target only advances at a recall's
  INSTALL_FINAL.
- On the host, the cut includes the journal position and the state hash
  (`controller.go:552-553`). Every MKDIR, LOOKUP or OPEN pin, SETATTR and
  CREATE therefore moves the cut.
- So the next GETATTR (`may_lookup`/`may_open`) mismatches. `acquire`
  (`tickets.go:367`, `:385-401`) starts a recall and answers BLOCKED, and
  re4 retries 52 ms later and gets the grant.
- The predicted sequence matches the history exactly:
  - root GETATTR after the MKDIR: ALL;
  - leaf GETATTR after the LOOKUP pin (pin-only): DOMAIN, so e84 scoping
    worked;
  - leaf GETATTR after the SETATTR: ALL;
  - `group` GETATTR after the CREATE: ALL.
- e84 scopes a recall to DOMAIN only for pin-only moves, but m1 held no
  lease. By e84's own reasoning (`mismatch_scope.go:24-29`), three of the
  four recalls could have been DOMAIN.
- This is not a correctness failure, but it costs 6-13 s per guest
  operation that follows a mutation. T1's remaining steps alternate
  mutation and observation many times, so after the fchown fix the next
  limit is likely T1's deadline, unless this cost drops.

### Fix

- **vf4 (required, guest kernel):**
  - `hfs_setattr` accepts `ATTR_KILL_SUID|ATTR_KILL_SGID|ATTR_KILL_PRIV`;
    `notify_change` has already turned them into `ATTR_MODE`, and the host
    clears 06000 on any uid/gid change (`mutation.go:476-487`). The
    refusal of MODE with suid/sgid together with UID/GID stays.
  - Handle the non-root truncate of a suid file and `file_remove_privs`
    before a non-root write the same way.
  - In the same rebuild, set dentry ops once (removes the taint).
  - Add a guest harness setattr/chown test.
- **wf4 (daemon, recommended):** scope an expected-target recall to the
  requesting domain whenever no peer holds a lease, not only for pin-only
  moves.
- **xf4 (design):** stop paying a full recall for a guest's own cut moves.
  Either the host refreshes install-only when only the requester's own
  calls changed and no peer holds a lease, or `hfs_d_revalidate` keeps
  valid dentries so cached walks stop re-pinning. Estimate T1's recall
  count against its 76 s budget before zq3-r19.
- **yf4 (sheet and probe):**
  - the direct failure OBSERVE (above);
  - `p.call` wrappers for the plain syscalls in `guest.py`, so the next
    failure names its syscall;
  - the boot-ID typo source.

zq3-r19 needs a kernel rebuild (vf4). Daemon `086d7cde…` can be reused
unless wf4 lands first.

## Run notes and sheet findings

- **The review-round-1 failure OBSERVE cannot run as written.** The
  case-failure block's `obs obs-$CASE-baseline obs-$CASE-fail` line was
  refused, because `correlate.py command` will not build commands for a
  controller run that recorded an error ("run state differs or recorded an
  error; inspect, never correlate").
  - As a recorded, read-only deviation, Claude took one direct OBSERVE at
    sequence 5, in the same form as the Phase 9 and T1-baseline lines
    (`tf3 observe 5 …`). That captured the T1 recall history above.
  - Without it, `recalls-t1.json` held only the two bootstrap recalls.
  - Next sheet: use that direct form, `L+3`, after a failed T1.
- **The largest-total selection worked:** Phase 11 step 1b chose the
  seven-recall history.
- **Typo:** the sheet's Phase 1 boot-ID line had a wrong suffix
  (`b5be-…`). The facts and `pins.json` held the right one, and it matched.
- **Operator slips, none of which changed anything on a host:**
  - a wrapper range bug left one Phase 4 step 0 local file with only the
    library hashes, so it was rebuilt in order;
  - a code-fence line and a prose line were run by number (bash syntax
    errors);
  - the `--no-child` cleanup line ran first and was refused by design;
  - a wait loop matched its own echoed command text and was rerun with an
    exact match.

## Host events and facts

- **f3:** boot `1794574308` unchanged.
- **N1:** Rocky clock `Europe/Sofia`, stamp `2026-11-21`, newest dnf
  transaction #65.
- **Rocky before the run:**
  - PID 46196 (UTC lstart `Fri Nov 20 15:37:52 2026`), boot
    `1d9d3252-5c16-…`, stock `741.22.1`, pod runtime unchanged;
  - stopped cleanly at about 11:50Z.
- **Rocky after the run:**
  - started normally at 11:58:43Z, SSH answered at 11:59:03Z;
  - PID 59685 (UTC lstart `Sat Nov 21 11:58:43 2026`), boot
    `c1dc3905-234b-4f83-91a9-4cb5e87b729f`;
  - **down about 9 minutes**, no relabel (vm-bhyve run 1 only);
  - its UEFI vars changed with this boot, so the next fill needs fresh
    facts.
- **Run values:**
  - Phase 7: volume `52c8c108-…`.
  - After READY: B is `c68b9e45…`, U is `2666e474…`, L=2.
  - Observer EA `53a16a3d…`, removed (`restored_pre_run: true`).
- **Guard:**
  - one SIGTERM at 11:56:49Z; the guard was gone by 11:56:55Z;
  - `runtime_exit 0`; exporter pid 56006;
  - peaks: disk 23.9 MB, exporter RSS 307 MB, runtime RSS 39 MB.
- **Final f3 state:**
  - Rocky running, `/dev/vmm` holds only `rocky`;
  - no fuse or fdesc mount; `fdescfs.ko` and `fusefs.ko` loaded, as the
    reviewed plan allows;
  - iom1 and iom2 running again (PIDs 60266 and 60597).

## Follow-up tasks

- **vf4:** the R18-1 guest kernel fix (kill bits in `hfs_setattr`, dentry
  ops set once, harness test). Then a kernel rebuild and zq3-r19.
- **wf4:** DOMAIN-scoped expected-target recalls when no peer holds a
  lease.
- **xf4:** the design task for recall-free own cut moves (host refresh or
  guest revalidate).
- **yf4:** run-sheet and `guest.py` diagnostics fixes.
- Still open: 5f4 (2f4 nits), ue4, 0a4, 2a4, 094, a84, l64, s54, s64, k84,
  qc4.
