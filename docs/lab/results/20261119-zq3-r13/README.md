# zq3-r13: READY with both mounts (R12-1 fixed); the matrix went unrun after an operator pause, and m0's unmount hung once the host was gone (2026-11-19)

Run sheet: [filled plan](../20261119-zq3-r13-filled-plan/run-sheet.md)
(no run-time revision).
Candidate xc4 `e9c77e5b…` ([xz3-20261118-r13](../../../../tools/integration/artifacts/evidence/xz3-20261118-r13/README.md)):
the nc4 daemon `c959b708…`, with the 1a4 guest kernel (build ID
`8c1de1ed…`) and the exporter `ef8f17de…` reused byte for byte from 5a4.
T5 stayed deferred. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r13-fill/evidence` (run log `RUN-LOG.md`),
bootstrap journal `/home/paul/tmp/claude/hfs-zq3-r13-fill/bootstrap-run/`.

## Outcome

Phases 0 to 9 passed. Phase 8 reached READY with **both** mounts and the
`usable` gate, so nc4 fixed R12-1 on the lab. The test matrix (T1-T11) still
did not run. The executing session paused for about 55 minutes after READY,
and by the time Phase 9 finished the guard's 3600 s bound had no room left
for it. Phase 11 took the guard-SIGTERM branch. While cleaning up, the
unmount of the session that was **not** blocked (m0) hung in D state after
the host runtime had stopped (new finding R13-1). Restoration was otherwise
clean.

**What held:**
- Phase 1a unloaded `fusefs` and `fdescfs` (both `rc 0`, neither listed
  afterwards). The Phase 4 step 1a process observer qualified the new
  daemon `c959b708…` (uid/gid 0, groups `[0,0,5]`, pid 23653).
- Phase 6 passed with a 0.49 GB margin (reclaimable 15,251,768,616 bytes
  against a bar of 14,763,950,080).
- Phase 8: the watcher made both operator signatures (enroll, then bind
  3.4 s later). mount-0 (mountinfo 391) and mount-1 (408) both came up with
  the exact g64 tuple (`rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`).
  READY came 37.0 s after launch. The `usable` gate passed: baseline and
  claims TERMINAL, `Open 0`, claims 1.7 s after the final call,
  `observe_sequence_last` L=2. **R12-1 did not recur.**
- Phase 9: three SSH no-ops, OBSERVE at L+1=3 passed (both domains
  `REOPENED`, claims `Open 0`), mountinfo had exactly the two tuples,
  `Enforcing`, `6.1.190-hfs-xz3`, the SELinux `Initializing` line, no hfs
  block line.
- 984 timing (final): SSH no-op median 0.21 s (3 samples), OBSERVE net
  0.651 s (1 sample), below the 2.4 s bound.
- No transport fence: `p11-fence.json` `fenced: false` (no `transport.db`
  failure row), `runtime.stderr` empty, `exporter.stderr` empty.
- No AVC records, no relabel. U2 removed the test kernel.

**Operator incident (not a product failure):** the session paused for
about 55 minutes between READY (07:15Z) and Phase 9 (08:10Z) because of an
agent login timeout. The guard's 3600 s bound runs from the Phase 8 launch
(07:14:35Z) and would end at 08:14:35Z, which left no room for T1-T11. They
are recorded as unrun (operator pause), and Phase 11 started at once.

**New finding:**
- **R13-1:** after the runtime stopped, m1 logged that it was blocked and
  unmounted in 1 s. m0 was not blocked, and its `umount` stayed in D state
  (wchan `hfs_control`) beyond `rockyt`'s 270 s bound (`rc=124`). The
  guest poweroff then took about 7.5 minutes. See the analysis below.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (process observer `c959b708…`, nightly gate GO 07:13Z, step 1b UMA drain run, Phase 6 margin 0.49 GB) |
| 8 | **passed** (mount-0 and mount-1 ok, READY, `usable` L=2; R12-1 fixed) |
| 9 | passed (OBSERVE L+1=3; 984 timing sample) |
| T1-T11 | unrun (operator pause; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch; R13-1 in step 2) |

Phase 11 ran the guard-SIGTERM branch. One SIGTERM at 08:11Z; the guard was
gone at the first check (08:11:05Z). Receipt: `runtime_exit 0`, only the
`guard-interrupted SIGTERM` breach (3384.4 s), runtime not running at guard
exit, exporter (pid 25320) still alive. Then: evidence tarballs (4.9 MB),
the step 1a fence record, the unmounts (R13-1), observer removed
(`authorized_keys` back to `e08542dc…`, `restored_pre_run: true`),
`next_entry` consumed, no AVC, full dmesg (444 lines, no new hfs lines
after the umounts), poweroff, host cleanup (tap and vmm destroyed, UEFI
vars original `530b541e…` unchanged), native FUSE unmount (`rc 0`), plain
literal observer key deletion on f3, and the window restore
(`mount_restored: true`, `fusefs.ko` kept loaded by the reviewed plan).
Phase 12: `start-rocky` at 08:24:51Z, `rockywait` saw SSH at the second
try (08:25:07Z), `check --expect stock` ok, `Enforcing`, no
`/.autorelabel`, vm-bhyve run 1 only.

## R13-1: m0's unmount hung after the host was gone

What the evidence shows:

1. The guard stopped the runtime at about 08:10:59Z (elapsed 3384.4 s). The
   exporter (bhyve, pid 25320) kept running.
2. At guest uptime 3377.7 s, which is the same moment, m1 (session
   `a28f9517-…`) logged
   `blocked by uncertain control result on VIEW_WAIT (-71)` and
   `view control worker stopped, error -71, at VIEW_WAIT (event -, ack -, host status -, view phase LIVE, recall -)`.
   This was expected: its host was gone. m0 (session `791ef945-…`) logged
   nothing, then or later.
3. `umount m0` (started about 08:11:35Z) did not return within 270 s
   (`rc=124`). `umount m1` then returned `rc 0` in 1 s. Afterwards
   mountinfo still listed m0, and guest PID 27195 (`umount m0`) was in D
   state with wchan `hfs_control`. No further block or worker-stopped line
   appeared.
4. The f3 `transport.db` copy at guard exit: m0's last VIEW_WAIT native
   cell (40645) was answered and fully read by the exporter (`Unread 0`).
   m1's last one (40646) was answered by the runtime but never read by the
   exporter (`Unread 1304`, nothing seen). No m0 cell after 40645 was
   recorded.
5. The guest poweroff began at 08:16:31Z, and the bhyve child was gone at
   08:24:07Z (try 45), about 7.5 minutes later. zq3-r12's poweroff took
   about 11 s.

**What m0's unmount was waiting on (inference from the code):**

- Every `Tbridge` exchange in the exporter runs `_verify` first
  (`tools/integration/lib9p/src/hfs9p.c` `hfs9p_exchange`). The supervisor's
  check (`hfs9p_supervisor.c` `_verify_locked`) sends a request to the
  runtime. With the runtime gone that request fails, the supervisor marks
  itself fenced, and `_verify` returns `ESTALE`. `hfs9p_exchange` returns
  that error **without encoding a reply** (`goto done`). The bhyve backend
  (`lib9p/patches/0001-dedicated-backend.patch`) then hands the chain back
  with `response_len 0`.
- Linux 6.1 `net/9p/trans_virtio.c` `req_done` completes a request only
  `if (len)`, so it drops a zero-length reply. For hfs clients the 0001
  patch waits for the reply with an uninterruptible `wait_event` (no
  Tflush), so the sending task never wakes up and cannot be killed.
- m1's outstanding VIEW_WAIT fetch was handled while the runtime was going
  away and came back as an ERROR packet (`-71`). That blocked m1, and a
  blocked session unmounts without host traffic, as in zq3-r12.
- m0's view worker had its last answer (cell 40645), so it would send the
  next VIEW_WAIT after the runtime had stopped. On the reasoning above,
  that send got no reply. In `hfs_transport`'s send phase the sender holds
  `session->writer` across `p9_client_hfs_exchange`. The unmount's control
  call (`hfs_control`) takes `session->writer` before sending anything, so
  it would wait on that mutex for ever. That matches wchan `hfs_control`:
  the mutex slow path is a scheduler function, so wchan names its caller.
  It also matches the missing m0 block line: no m0 control call ever failed,
  and the bounded 30 s transport deadline is only checked between replies,
  never during the wait inside the 9p client. Tasks stuck like this cannot
  be killed, which explains the slow poweroff.

**Confidence:** high that m0's unmount waited on host traffic that never
completed, and that the trigger was the host runtime being gone while the
exporter stayed up. Medium-high that the cause is an exporter exchange left with no reply (a
fenced `_verify` returns no bytes, and virtio drops a zero-length reply).
This is read from the source and fits every observation, but the run
captured no stack, and the exporter's state was not dumped. Medium on the
exact holder (m0's view worker stuck in its VIEW_WAIT send while holding
`session->writer`), because wchan alone cannot tell the writer mutex from
`session->lock`. The guest kernel has no hung-task detector
(`# CONFIG_DETECT_HUNG_TASK is not set`), so the missing "blocked for more
than 120 seconds" report says nothing.

**Relation to oc4.** oc4 (guest transport retries) is in progress, and its
change exempts unmount and retirement traffic from stop-on-block. R13-1
shows that this traffic must also be **bounded when the host is
unavailable**. Specifically:
- the exporter must answer every exchange, including a fenced `_verify`,
  with an encoded ERROR (pc4's distinct host-transport-failure code fits),
  never with a zero-length reply;
- the guest's unmount path must not wait without bound for
  `session->writer` or a fixed tag held by a sender that may never be
  answered; at the least, a bounded or killable 9p wait for hfs requests;
- once the host is known to be gone, unmount should finish locally
  (retaining the session's evidence) instead of waiting for host replies.

A replay test would stop the runtime with two live mounts, one of them
between VIEW_WAIT polls, and require both unmounts and a guest poweroff
within a fixed bound.

## Host events and facts

- f3 boot `1794574308` unchanged. N1: Rocky clock `Europe/Sofia`, stamp
  `2026-11-19`, newest dnf transaction still #64 (unchanged); nightly gate
  GO at 07:10Z (Phase 1) and 07:13Z (Phase 5 step 0).
- Rocky before: PID 53482 (UTC lstart `Wed Nov 18 17:24:06 2026`), boot
  `6fe8702d-…`, stock `741.22.1`, pod runtime unchanged, pod manifest
  `523485cc…` (same as zq3-r12). Stopped by ACPI at 07:13:50Z, started
  normally at 08:24:51Z: PID 46923 (UTC lstart `Thu Nov 19 08:24:51 2026`),
  boot `5d5c32fe-e4f4-469d-8be0-2c1a3270d70b`. **Rocky was down about 71
  minutes** (07:13:50Z to 08:24:51Z), most of it the operator pause and
  R13-1's slow poweroff.
- Phase 5 step 1a recommended the UMA drain (active-based margin 0.45 GB).
  Step 1b freed about 0.12 GB (28,867 pages; `uma_free_bytes` 155.0 MB →
  40.3 MB).
- After READY: B=`f58b8dea…` (cases credentials, sync, metadata,
  namespace; T5 deferred), U=`010d0c7d…`, L=2. The guest requests were
  written (lock-m0 `bcb6344d…`, lock-m1-t8 `e370ca5d…`, pod `0191aa77…`)
  but never used.
- Guard peaks: disk 23.8 MB, exporter RSS 350 MB, runtime RSS 39 MB.
- **Operator slip (f3 observer key deletion):** the first attempt, from a
  zsh shell, failed while ssh parsed its options, so nothing ran on f3. The
  files were checked to be still present, and the plain literal line was
  retried through the bash wrapper (`rc 0`). Afterwards only
  `rocky-known_hosts` was left.
- Final f3 state: Rocky running, `/dev/vmm` holds only `rocky`, `tap100`
  gone, no fuse/fdesc mount, `fdescfs.ko` (30) and `fusefs.ko` (31) loaded,
  zroot ONLINE. Rocky has no `hfs-xz3` kernel (U2).

## Follow-up tasks

- **zq3-r14:** rerun with a fresh fill (new Phase 1 facts, window plan and
  pins) to run T1-T11. The candidate can stay `e9c77e5b…` unless the R13-1
  fixes land first.
- **R13-1 → oc4** (guest 0003 and the 9p client): bound unmount and
  retirement traffic when the host is unavailable (above). **pc4**
  (exporter): always encode a reply, including a fenced `_verify`.
- **Run sheet:** the executing session must not pause between READY and
  the matrix. The next fill should state a latest Phase 9 start (the
  guard launch plus 3600 s, minus the matrix estimate and the Phase 11
  margin), with a rule to go straight to Phase 11 once it has passed. Run
  READY through Phase 11 in one continuous session that cannot time out at
  login.
- Still open from zq3-r12: qc4; from zq3-r11: z94, 0a4, 2a4.
