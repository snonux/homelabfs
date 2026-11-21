# zq3-r19 run sheet (filled 2026-11-21)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](facts/) · [zq3-r18 record](../20261121-zq3-r18/README.md) ·
[zq3-r18 sheet](../20261121-zq3-r18-filled-plan/run-sheet.md)

Ordered commands for run `zq3-r19`, the nineteenth attempt of zq3, with the
values filled on 2026-11-21 from `main` `f2f6c44` (the r19 candidate
merge, after vf4, wf4, yf4 and yg4). The candidate is the r19 rebuild
`09acd09d…`, made after zq3-r18's R18-1 (T1 admitted both mounts in 7.3 s
for the first time, then the guest kernel's `hfs_setattr` refused the
`ATTR_KILL_*` bits that every chown/fchown of a regular file carries, so
`fchown` returned `EOPNOTSUPP` before any RPC):
- **the guest kernel, new bytes** (vf4; `6.1.190-hfs-xz3`, bzImage
  `e5add01e…`, GNU build ID `e3d474df…`, System.map `91e970f0…`):
  `hfs_setattr` accepts the `ATTR_KILL_*` bits (R18-1); dentry ops are set
  once, in `hfs_dentry_attach` (zq3-r18's `d_set_d_op` WARNINGs); link(2)
  and the create `EEXIST` path `d_drop` before the re-lookup, so
  `d_splice_alias` never sees a hashed dentry (a guest `BUG_ON` that T4
  `namespace` would have hit on every link). The 2f4/ie4/re4 behaviour is
  kept;
- **the CGO daemon, new bytes** (wf4; `bb4ad73c…`): an expected-target
  recall is scoped to the requesting domain when no peer holds a lease or
  an open reservation (otherwise it stays all-domain). je4/ce4/e84 are
  kept. **Phase 4 step 1a is required for these new bytes** (zq3-r18's
  check of `086d7cde…` does not carry over);
- **the window artifacts, new bytes:** `controlledruntime.test`
  `408cdafd…` and `fdescfs-namespace` `d2ca3ab6…` (rebuilt with the daemon
  from `e160370`; the committed window `pins.json` names them);
- **guest.py** (yf4, `005d9592…`): every case syscall is a named `p.call`,
  so a failure names its syscall; the controller pins it (`probe_sha256`)
  and sends it from the workstation;
- **the native exporter** `2fd7e32f…` is reused byte for byte; the runtime
  spec (`ControlTickets` 24), the kernel config `28fe4a8e…`, the five
  modules, `Module.symvers` and `modules.*` are unchanged;
- **be4 (kept):** guest.py's admission alarm 150 s, a request's whole
  deadline 240 s (this batch), lifecycle steps 180 s, the controller's SSH
  dispatch bound min(request + 15, plan seconds) = 255 s (plan `seconds`
  255), the T6 lock probe case 150 s / overall 240 s, and T7's workload
  240 s with `pod_fixture.py`'s `RUN_SECONDS` 315 s. The guard stays
  3600 s, so a run with several slow steps STOPs early by design;
- `hfs-runtime-prep` and the signer rebuilt at `f2f6c44` (both link
  `internal/viewcontrol`, which wf4 changed);
- **three f3 VMs that are not HomelabFS, `iom1`, `iom2` and `iom3`** (2 GiB
  each, uefi). On Paul's decision their default state is now **Stopped**:
  the iormesh session stopped them at about 15:00Z on 2026-11-21 and will
  not start them without telling Claude. Phases 1 and 5 require all three
  Stopped; **the run never starts them** (Phase 12 only checks they are
  still Stopped, and Claude messages the iormesh session after the run);
- **f3 swap is in use** (2026-11-21 15:14Z: 891,068 KiB of 2 GiB, 42 %,
  left from an iormesh OOM incident at 14:20Z). Phase 6 requires swap
  unused (`runtime-guard.py` admits no swap use), so **Phase 0b clears it
  before Phase 1, only with Paul's explicit approval for this run**;
  without it the run stops before Phase 1a.

**Rocky upgrades itself once a day** ([N1](#n1-rockys-nightly-upgrade)): a
`dnf -y upgrade` runs at 00:55 Rocky-local = 22:55 UTC (Rocky's clock is
Europe/Sofia, EET, UTC+2; zq3-r12's run-time revision R1). The run must
start after that day's run has stamped and finish before the next one
(**22:55 UTC on the run day: 2026-11-21 22:55 UTC for a run on
2026-11-21**); the gate before Phase 5 enforces it (latest Phase 5 start
21:25 UTC; the gate computes the dates from its own clock). Phase 1
rechecks the stamp, the dnf history (newest transaction still **#65**), the
pod-runtime NEVRAs and SHA256s and the kernel facts, and any drift means a
refill.

**The guard's 3600 s bound runs from the Phase 8 launch** (zq3-r13's
lesson: an operator pause of about 55 minutes after READY left no time for
the matrix). Phase 9 starts within 5 minutes of READY, READY to Phase 11
runs in one continuous session, and `gbudget` checks before Phase 9 and
before every step that needs the live runtime that it still fits before the
bound ([Phase 10](#phase-10-gate-matrix-b-inside-the-window-about-20-30-min)).

**Every session line runs in bash** (zq3-r13's zsh slip): the operator's
login shell is zsh, which does not split `session.sh`'s `$O` into words, so
each line runs through a bash wrapper that sources `session.sh` and
`vars.env` first; `session.sh` refuses to load in any other shell.

**How to read the code blocks (zq3-r18's operator slips).** Nothing in this
sheet is referenced by sheet line number: never run a line "by number".
Every code block starts with a `# ==== BEGIN <block> ====` comment line and
ends with a `# ==== END <block> ====` comment line; only the lines between
them are commands (the markers themselves are harmless comments). Text
outside a code block is never a command. A ```` ```text ```` block is a
summary or a template, never run. When a wrapper runs a range of lines,
check that the range starts right after a BEGIN marker and stops at or
before the END marker of the same block.

**T5 `mapped-pressure` stays deferred** (z64 mechanism). q64/f84 fixed the
view-lifetime wall, but T5 is not yet re-planned or sized, so the batch plan
declares the deferral and runs the other four cases. zq3 acceptance
therefore stays incomplete by design.
**This sheet grants no authority.**
Class **A** steps are critical and run only after Paul's explicit final GO on
this filled plan (A1-A8 of the run plan, including the Phase 0b swap
clearing and the Phase 1a unload).
Class **B** steps are routine bounded verification under the 2026-11-02
standing approval, but only inside an approved A window. `PAUL` marks a point
where Paul himself must act. Durations are estimates.

Generic failure rule (every phase): stop dependent work, collect evidence, never
force anything (no `vm poweroff`, no `kill -9`, no forced unmount, no recursive
delete through a mount), preserve failed resources, report. Values written
`<…>` or held in a shell variable set by an earlier line are run-time fills
(see [README](README.md#run-time-fills)); everything else is exact.

## Rules that apply throughout

- **Tool timeouts for long lines (review round 1, M1).** The executing agent's
  shell tool kills a command after 120 s by default. Run Phase 8's
  `bootstrap.py run | sign_watch.py | tee` line in the background (or with a
  tool timeout of at least 600 s) and watch `p8-bootstrap.jsonl` until the
  driver exits: up to 120 s to READY plus at most about 100 s for `usable`.
  A killed driver leaves an open journal intent (`uncertain`, never replayed)
  while the guard and runtime keep running, so the window is lost.
  `gbudget` lines are local and fast. Every
  `rockyt` line (its own bound is 270 s) and the `vms.sh stop-rocky` /
  `start-rocky` lines in Phases 5 and 12 (and U3's `start-rocky`) run with a
  tool timeout of at least 300 s, so their own bounds (and `rc=124`) are what
  end them. **be4 raised the guest deadlines, so the rule is now:**
  - every `$CTL` controller line (`probe`, `next`, `release`,
    `lifecycle`): **at least 300 s** (each dispatch is bounded by
    min(240 + 15, 255) = 255 s, a lifecycle step by 195 s);
  - the T6 and T8 guest lock-probe lines: **at least 300 s** (the probe's
    overall budget is 240 s, its entry timer 238 s);
  - T7's `pod_fixture.py run` line: **at least 420 s** (its own
    `RUN_SECONDS = 315` plus setup and teardown; the old 300 s would kill
    the ssh mid-run and leave containers and an uncertain T7);
  - Phase 3's kernel `install` and pod `build`, and any other line that can
    take more than a minute: at least 300 s (zq3-r10 review round 1).
- **Bash only (zq3-r13's zsh slip).** The operator's login shell is zsh.
  `session.sh`'s functions need bash: zsh does not split the unquoted `$O`,
  so ssh receives every option as one word and fails at option parsing.
  Run every line below in bash, through a wrapper that sources the session
  and the persisted variables first, for example `bash -c 'source
  /home/paul/tmp/claude/hfs-zq3-r19-fill/session.sh && source
  /home/paul/tmp/claude/hfs-zq3-r19-fill/vars.env && <line>'` (or one
  interactive bash session). `session.sh` refuses to load in another shell.
- **No pause between READY and the matrix (zq3-r13).** The guard stops the
  runtime 3600 s after the Phase 8 launch, whatever the operator does.
  Phase 9 starts within 5 minutes of READY, READY to Phase 11 runs in one
  continuous session that cannot time out at login, and `gbudget STEP`
  decides before Phase 9 and before each budgeted step (Phase 10 table).
- **f3's login shell is csh.** Every `f3 '…'`/`f3 "…"` string below avoids
  `2>/dev/null` (csh reads `2` as an argument), `VAR=value cmd` prefixes (use
  `env VAR=value cmd`) and quotes nested more than one level. Anything more
  complex goes to `sh` on stdin: `f3 sh -s < FILE`, `printf … | f3 doas -n sh -s`,
  or the proven `f3 "doas -n sh -c 'umask 277; test ! -e X && cat > X'" < FILE`.
- **Native guard stage names are single use.** `native-test-guard.py` creates
  `checks/<stage>` exclusively and keeps it on refusal; a retry takes a new
  name (for example `unsupported-before-2`), and the refused directory stays as
  evidence. No `--` follows `--stage NAME`: the guard's `argparse.REMAINDER`
  keeps a leading `--` under Python 3.12 and would exec it (zq3-r1 R1).
- **One command per line, never batched** (zq3-r2 R2-6). Run each line of a
  block on its own, read its output, then run the next. A line marked
  "only if …" is a decision: write the check's result into the run log
  (`$E/RUN-LOG.md`) before running or skipping it. A local `VAR=$(…); echo
  "$VAR"` line counts as one line, and so does the `obs` helper (Phase 10): it
  prepares locally and makes exactly one f3 call. **Never chain a
  follow-up after a bounded command** (the zq3-r11 slip): a `rockyt`
  unmount and the probe after it are separate lines, and the next line runs
  only after the previous line's `rc` was read and recorded.
- **Guest 9p mount checks count mountinfo lines (zq3-r7 R7-3, h64).** The
  guest's `findmnt -t 9p` exits 0 even when nothing matches, so `findmnt -t
  9p || echo no-9p` never prints `no-9p`. Every guest mount check in this
  sheet therefore uses `grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p '
  /proc/self/mountinfo`: the matching lines, then their count (`0` means no
  9p mount).
- **SSH banner timeouts (zq3-r2 R2-5).** A single ProxyCommand SSH to the
  test guest may fail with "Connection timed out during banner exchange".
  Never retry blindly: first prove that nothing ran, with a read-only check
  of the step's own effect (for example `sha256sum /root/.ssh/authorized_keys`
  still `$EA` before retrying the observer removal, the mountinfo count before
  retrying an unmount, the `rocky_kernel.py` state journal before retrying a
  kernel step). Record the timeout and the check in the run log, then retry
  once. A second failure, or a check that shows the step partly ran, stops:
  inspect. A native guard retry still takes a new stage name. A lifecycle
  command (OBSERVE, ARM, RESTART) is never retried with the same sequence:
  a lost reply may have consumed it.
- **f3 unreachable (zq3-r3 R3-2): pause.** If f3 stops answering (connect or
  banner timeouts, or ping RTT far above the usual ~2 ms), pause the run at
  the current line. Record the time and `ping -c 10 10.20.0.133` in the run
  log, and resume only after the RTT stays under 100 ms for a minute. Then
  retry the interrupted SSH call at most once, and only after a read-only check
  proved that nothing ran (rule above). Nothing on the hosts is changed to
  "help" while paused. The Phase 4 window plan expires 55 minutes after
  `F3_NOW`: a longer pause before qualify means the `-resume` refill (Phase 4).
- **A failed 9p mount needs a guest reboot (r54 analysis).** A mount that
  fails after a successful enroll leaves its controlled identity claimed
  ("consumed") for the rest of that guest boot, so a second mount attempt in
  the same boot returns `EACCES`. Never retry a mount, never hand-run `mount
  -t 9p` after a failure: preserve evidence and go to Phase 11. Another
  attempt needs a new guest boot, which means a new run (new run ID, new
  sessions and a fresh fill).

## Execution notes (carried over from zq3-r3 to zq3-r19)

- **D1-a (Phase 1a) is needed again in zq3-r16**, as in zq3-r3 to zq3-r7
  and zq3-r9 to zq3-r15. zq3-r13's window loaded `fdescfs.ko` and
  `fusefs.ko` in the current f3 boot, and its restore kept them loaded (the
  reviewed plan). The 11:31Z facts list both (ids 30 and 31) with no fuse
  or fdesc mount. The window plan filler
  requires both absent, so Phase 1a unloads them with a normal `kldunload`
  (fusefs first, then fdescfs, **never `-f`**). A refused unload ("Device
  busy" or any error) stops the run before anything is staged: a plan
  revision decides (D1-b, a reviewed tool change, or D1-c, an f3 reboot, are
  not run-time options).
- **Every abort after Phase 4 step 5 also removes the observer key**: on
  Rocky, with `observer_key.py remove --expect-sha256 $EA` while Rocky (or the
  test guest on Rocky's disk) answers, and on f3 with the plain single command
  of Phase 11 step 9 (literal paths, its own line; see the zq3-r5 notes
  below). Phase 12's `e08542dc…` check must hold on every path.
- The guard's exit status is read from `$R/guard/receipt.json` and
  `stop-uncertain.json`, not from the operator shell.
- **Phase 8 retry after a host-preflight failure (zq3-r3 attempt 1).** If the
  driver stops at `host-preflight` and its journal holds only that step's
  intent and failure (no `launch` intent), nothing ran on f3: after the pause
  rule above, keep the failed output as evidence with
  `mv $D/bootstrap-run $D/bootstrap-run.preflight-timeout-1` and
  `mv $E/p8-bootstrap.jsonl $E/p8-bootstrap.preflight-timeout-1.jsonl`, then
  rerun the Phase 8 command once (the watcher only signs below
  `$D/bootstrap-run`). Any later stop is never rerun.

## Execution notes from the zq3-r4 review round 1 (2026-11-09)

These overrode the lines they name in the zq3-r4 sheet. This sheet also writes
them into those lines (Phase 10 T8 and T10, Phase 11 before step 0 and steps
2 and 7), so the notes and the lines agree:

- **Guard SIGTERM (Phase 11 RESTARTED/serving branch):** find the guard with
  `f3 'pgrep -U root -lf runtime-guard.py'`. The remote csh wrapper of the
  guard's SSH session also carries the guard argv (zq3-r2 R2-2), so a plain
  `pgrep -lf` lists two processes. Signal only the root python process.
- **Guest unmounts are bounded locally:** use `rockyt "umount
  $G/m0"` and `rockyt "umount $G/m1"` (session.sh `rockyt`: `timeout 270`
  around the real ssh; `timeout` cannot run the `rocky` function), with T8's
  unmount bounded the same way. SSH keepalives do not end a blocked `umount`,
  because sshd keeps answering. A timeout means a blocked 9p session (the
  expected FENCED state): record it, never use `-f`/`-l`, and continue to the
  poweroff. Every `rockyt … | tee` line ends with `; echo
  rc=${PIPESTATUS[0]}`, which prints rockyt's own exit status (124 = bound
  expired), not tee's (zq3-r6 execution note).
- **T10 abort paths:** if `correlate.py command --operation restart`
  refuses, or `t10-restart.json` reports `REFUSED`, treat the runtime as
  still serving. Run T8 first, as the ARM-refused path does, then Phase 11
  through its guard-SIGTERM branch.
- **Missing exporter:** if the guard receipt's `exporter` is null, step 7 is
  `host.py cleanup --record … --record-sha256 … --no-child`, not
  `--child-pid`.
- **Phase 0 recheck:** also `sha256sum /usr/bin/ssh` on the workstation. The
  batch template pins it (`16cb2c6a…`); an openssh update since the fill
  would make the batch fill refuse after READY, so stop and refill if it
  differs.

## Execution notes from the zq3-r5 run (2026-11-09)

- **f3 observer key deletion is one plain command.** In zq3-r5 the agent's
  safety check refused the f3 key deletion when it was wrapped in a `bash -c`
  script, and the deletion was left to Paul. Phase 11 step 9 (and every
  abort path that removes the key) therefore runs exactly this line on its own,
  with literal paths and no variables, no script and no loop:

  ```bash
  # ==== BEGIN Execution-notes-from-the-zq3-r5-run-2026-11-09 block 1 (run each line on its own) ====
  f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r19/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r19/observer/id_ed25519.pub"
  # ==== END Execution-notes-from-the-zq3-r5-run-2026-11-09 block 1 ====
  ```

- **The window restore's outcome is `restoration-summary.json`, not an exit
  code.** `fdescfs-window.py restore` prints nothing on success (zq3-r5's
  `p11-window-restore.txt` is empty). An `rc` reported through `| tee` or any
  other output filter is the filter's status, not the restore's. Phase 11
  step 10 therefore reads
  `/var/tmp/hfs-fdescfs-windows/zq3-r19/restoration-summary.json`: the restore
  succeeded only if it exists with `"mount_restored": true`.
- **Rocky's first boot after `start-rocky` rebooted itself after about
  5 minutes** in zq3-r1 to zq3-r6. The zq3-r6 record explains it (R6-3): a
  test boot without SELinux leaves `/.autorelabel`, so the next stock boot
  relabels and reboots once. zq3-r7, the first run whose test kernel had
  SELinux, needed no relabel reboot (one vm-bhyve run, SSH after 26 s), and
  neither did zq3-r8 to zq3-r13 (SSH after about 10-20 s each).

## Execution notes from the zq3-r6 run (2026-11-10)

These notes are written into the lines they name:

- **`rockyt … | tee`:** the line ends with `; echo rc=${PIPESTATUS[0]}`, which
  prints rockyt's own exit status (124 = bound expired), not tee's (Phase 10
  T8, Phase 11 step 2).
- **Window restore:** judged only by
  `/var/tmp/hfs-fdescfs-windows/zq3-r19/restoration-summary.json` (Phase 11
  step 10), as in the zq3-r5 notes. zq3-r6 to zq3-r13 all restored with
  `"mount_restored": true`.
- **f3 observer key deletion:** the plain literal-path line of Phase 11
  step 9, which the agent safety check accepted in zq3-r6 to zq3-r13.

## Execution notes from the zq3-r7 run (2026-11-10, task h64)

zq3-r7 ran end to end. Both mounts came up, READY arrived with every step
`ok` and SELinux was enforcing. T1 then failed at admission (R7-1) and the
mount roots refused root (R7-2). Its errata are written into the lines they
name:

- **Guest mount checks count ` 9p ` mountinfo lines** (R7-3; rules above):
  in Phase 9, T8 and Phase 11 step 2.
- **Phase 11 step 6 waits for `child-gone` only.** The run's vmm context
  stays until step 7's `host.py cleanup` destroys it, so step 6 no longer
  waits for `no-vmm` (zq3-r6 effectively did the same).
- **R7-1 (no mount options in mountinfo)** is fixed by g64 in this kernel
  (hfs `.show_options`, exact `cache=mmap`). Phase 9 now requires the exact
  tuple `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`,
  which `guest.py` `check_tuple` and `pod_fixture.py` require.
- **R7-2 (the source-capacity bound)** is fixed by i64 (Sources 10 in the zq3
  spec, with a derived floor in validation) and by m64 (the guest-side
  floor). j64 resolves a definite admission refusal instead of leaving it
  unresolved, and m64 logs every guest fault block. k64 adds the post-READY
  `usable` bootstrap step: both roots must still answer an `lstat` after the
  guest's 30 s QUERY window has passed.

## Execution notes from the zq3-r8 run (2026-11-14)

zq3-r8 ([record](../20261114-zq3-r8/README.md)) reached READY with both
mounts and the exact g64 tuple, then the k64 `usable` gate failed at its first
probe (R8-1): m1's root `lstat` did not return, and the guest blocked both
sessions on unanswered QUERYs (a PROCESS_EXIT on m0, a GETATTR on m1). The
cause was the y64 daemon's recalls, which parked every CALL and ran a
synchronous SSH observation per VIEW_ACK on the shared control worker. Phase
11 and Phase 12 ran cleanly through the guard-SIGTERM branch. Kept from the
zq3-r8 sheet, and written into the lines they name:
- **h64 errata:** guest 9p checks count ` 9p ` lines in
  `/proc/self/mountinfo` (Phase 9, T8, Phase 11 step 2); Phase 11 step 6
  waits for `child-gone` only.
- **T5 deferral (z64):** the batch template carries `deferred:
  [{case: "mapped-pressure", reason: …}]` and the other four cases in
  canonical order. The reason is updated: q64's lifetime wall is fixed
  (q64/f84 merged), but T5 is not yet re-planned or sized.
- **Phase 1 checks that `10.20.0.133` is on `vm-public`** (the observer key's
  `from="10.20.0.133"`); zq3-r8 to zq3-r13 confirmed it, and so do the
  11:31Z facts.
- **Persisted variables:** append every `VAR=` result to `vars.env` (see
  Session setup; `L` is new).
- **Phase 11 step 4b** saves the full guest `dmesg`.
- **m64 block-line rule:** before T9, any guest `hfs … blocked` line is a
  finding. T9 (m0) and a FENCED T10/T11 (m1) block sessions on purpose, so
  from T9 on only block lines for an unexpected session or operation are
  findings. **Since zq3-r11 (z84):** a `failed provider stop` block line
  logged by a normal unmount (T8, or Phase 11 step 2 of a session that was
  not already blocked) is always a finding. **Since zq3-r12 (1a4):** a
  `view control worker stopped` or `initial attributes timed out behind a
  recall` line is a finding at any point; every block-line capture greps
  for both.
- **Phase 9 gates** on the exact mount tuple
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`, exactly
  two 9p mountinfo lines, SELinux enforcing and no `hfs … blocked` line.

## Execution notes from the zq3-r9 run (2026-11-14)

zq3-r9 ([record](../20261114-zq3-r9/README.md)) fixed R8-1: Phase 1a
unloaded both modules (rc 0 each), Phase 4 step 1a qualified the l84 daemon,
READY arrived with every step `ok` and both signatures by the watcher, and the
b84 `usable` gate's first probe and its `baseline` OBSERVE (sequence 1) were
clean. The gate's **final probe** then hung on both roots (R9-1): each
root's GETATTR was answered ADMITTED but its claim stayed
`RESERVED/UNPREPARED`, because the first admission was refused behind a
recall and every guest QUERY resume was refused (only DESCRIPTION_RELEASE and
FORGET could resume). The guest blocked both sessions on QUERY timeouts
(`blocked by QUERY timeout on GETATTR (-110)`). The trigger was most likely
the guest's memory pressure pass releasing the in-flight observation's lease.
Phase 11 (guard-SIGTERM branch) and Phase 12 ran cleanly; Phase 9 and the
984 timing never ran, so no timing sample exists yet. Kept, and written into
the lines they name:
- **Phase 1a (D1-a)** again: zq3-r9's window left both modules loaded.
- **Phase 4 step 1a** again: new daemon bytes (zq3-r9: `868dc045…`).
- **The tool-timeout rule** and **`printf %q` for `vars.env`** (zq3-r9
  review round 1), the Phase 9-10 helpers (`p9-ready.py`, `p9-observe.py`,
  `p9-timing.py`, `p10-*.py`), the L sequence shift and OBSERVE evidence
  `/2`.
- **T5 deferred** with the r9 reason (unchanged).
- The guard-SIGTERM branch of Phase 11 in its mandated order, which zq3-r9
  ran cleanly: live owner snapshots before the SIGTERM, one SIGTERM, evidence
  tars, `rockyt` unmounts with `rc` capture, observer removal, AVC and full
  `dmesg` capture, poweroff, `child-gone`, host cleanup, native unmount, the
  plain literal f3 key deletion and the restore judged by
  `restoration-summary.json`.

## Execution notes from the zq3-r10 run (2026-11-15)

zq3-r10 ([record](../20261115-zq3-r10/README.md)) was the first run past
the b84 `usable` gate: Phase 1a unloaded both modules (rc 0 each), Phase 4
step 1a qualified the r84 daemon, READY and `usable` passed (L = 2), and
Phase 9 passed with both providers `Open 0` and the exact g64 tuple. The 984
timing gave its first samples: SSH no-op median 0.198 s, OBSERVE net
0.605 s and 0.434 s, far below the 2.4 s model bound. Two failures followed:
- **R10-1:** T1 `credentials` hit guest.py's 10 s admission alarm. The
  alarm stayed pending during the uninterruptible hfs wait and raised
  `Deadline` right after `os.open` returned, before the fd was registered,
  so the fd leaked to process exit. In `do_exit` the PROCESS_EXIT work was
  queued first, so the final release met a pending allocation, failed
  locally with `-EAGAIN` and blocked m0 (`blocked by DESCRIPTION_RELEASE
  failure on DESCRIPTION_RELEASE (-11)`). The DESCRIPTION_RELEASE never
  reached the host. Case `dispatching-uncertain`; T2-T4, T6, T7, T9-T11
  unrun.
- **R10-2:** T8's normal unmount of m1 (rc 0) logged `blocked by failed
  provider stop on STOP (-16)`: the unmount stop check ran before
  `kill_anon_super` and refused a once-reopened LIVE view. Functionally
  harmless, but every clean unmount of a once-reopened mount logged it.

T8 held. Phase 11 (guard-SIGTERM branch, in the mandated order) and Phase 12
ran cleanly; the test kernel was removed (U2). Kept, and written into the
lines they name:
- **Phase 1a (D1-a)** again: zq3-r10's window left both modules loaded.
- **Phase 4 step 1a** again: new daemon bytes (`bf86d805…`).
- **The extended tool-timeout rule** of the zq3-r10 review round 1
  (controller lines, T6/T7/T8, Phase 3, U3 at least 300 s; Phase 8 in the
  background), **`printf %q` for `vars.env`**, the `observe-p9-2.json`
  rerun name, the Phase 9-10 helpers, the L shift, OBSERVE evidence `/2`,
  the `tf3` timing and the `obs-timing.tsv` capture check.
- **T5 deferred** with the r9 reason (unchanged).
- The live owner snapshots before and after any guest block line, as
  zq3-r10 took them for T1 (`t1-owner-live.tar`, `t1-owner-after.tar`).

## Execution notes from the zq3-r11 run (2026-11-18)

zq3-r11 ([record](../20261118-zq3-r11/README.md)) passed the b84 `usable`
gate and Phase 9 again (L = 2; Phase 1a unloaded both modules, Phase 4
step 1a qualified the b94 daemon, Phase 6 margin 0.50 GB after the drain).
The 984 timing gave OBSERVE net 0.466 s and 0.483 s. zq3-r10's R10-1 and
R10-2 did not recur. Two new failures followed:
- **R11-1:** T1 `credentials`: the m0 root `open-root` at guest 103.6 s,
  then nothing until guest.py's 10 s alarm fired at 147.2 s (the OPEN took
  43.6 s); the guest blocked m0 (`blocked by QUERY timeout on OPEN (-110)`)
  and the probe ended `dispatching-uncertain` after 44.7 s. Cause: the
  read-only root OPEN under the caller's own observation lease started an
  all-domain recall (z94), the host answered m0's early DRAINED with STALE,
  the guest stopped its view worker (`error -116`) and the recall waited in
  WAIT_DRAINED forever. Fixed by y94 (host) in this candidate; z94 is not
  fixed.
- **R11-2:** T8's normal `umount` of m1 stayed in D in `hfs_view_enter` (the
  stat of the mount point) until `rockyt`'s 150 s bound (`rc=124`); T8
  unrun, and the guest poweroff later took about 530 s. Fixed on the guest
  side by 1a4 (the view wait is bounded at 30 s); 0a4 (the host never drops
  a participant that stops acknowledging) is not fixed.
- **Operator slip:** the T8 command was chained, so the lock probe started
  after the unmount had timed out. It only stats and changed nothing, but
  from now on **never chain a follow-up command after a bounded unmount
  that timed out**: each unmount or probe runs on its own line, and its
  `rc` is checked before the next line (rules above, T8, Phase 11 step 2).

Phase 11 (guard-SIGTERM branch, in the mandated order) and Phase 12 ran
cleanly: Rocky started at 09:01:00 UTC (PID `3146`), no relabel reboot, the
test kernel removed (U2). Kept, and written into the lines they name:
- **Phase 1a (D1-a)** again: zq3-r11's window left both modules loaded.
- **Phase 4 step 1a** again: new daemon bytes (`e0981a0a…`).
- The extended tool-timeout rule, `printf %q` for `vars.env`, the Phase 9-10
  helpers, the L shift, OBSERVE evidence `/2`, the `tf3` timing with the
  `obs-timing.tsv` capture check, the T5 reason and the Phase 11
  guard-SIGTERM order.
- `p10-admission.py` (it showed exactly where T1 stalled) and the case
  failure capture with live owner snapshots (`t1-owner-live.tar`,
  `t1-owner-after.tar`).
- The block-line captures before and after every normal unmount (T8, Phase 11
  step 2).

## Execution notes from the zq3-r12 run (2026-11-18)

zq3-r12 ([record](../20261118-zq3-r12/README.md)) stopped in Phase 8,
before READY. Phase 1 found that Rocky runs on Europe/Sofia (run-time
revision R1, now in [N1](#n1-rockys-nightly-upgrade)), Phase 1a unloaded
both modules, Phase 4 step 1a qualified the 5a4 daemon `e0981a0a…`, the
Phase 5 gate printed GO, step 1b drained and Phase 6 passed (margin
0.50 GB). Admission, both signatures by the watcher and mount-0 with the
exact g64 tuple worked; then:
- **R12-1:** mount-1 `uncertain` ("mount did not return before its
  deadline; left running"). At guest uptime 23.5 s both sessions were
  `blocked by uncertain control result on VIEW_WAIT (-71)`, and both 1a4
  worker lines read `(event -, ack -, host status -, …)`. Offline analysis:
  not a 1a4 regression. The host runtime's guest transport fenced itself (a
  retried mapped cell fenced the whole transport), the exporter turned the
  failed fetches into ERROR packets and the guest mapped them to `-EPROTO`.
  `runtime.stderr` and `exporter.stderr` were empty: the reason was never
  logged. Fixed by nc4 in this candidate (the retry reuses its recorded
  base; every fence records its reason). Mount-1 hung for 30 s instead of
  failing at once because the guest's `hfs_transport` retry loops ignore
  `session->blocked` (oc4, not fixed).
- **Operator slip:** the Phase 12 SSH wait loop ran `bash -c "rocky true"`.
  Session functions are not exported, so it found no `rocky` and waited
  10 minutes for nothing (Rocky was up). From now on **never call a session
  function (`f3`, `rocky`, `rockyt`, `tf3`, `obs`, `rockywait`) inside `bash
  -c`**: wait loops run in the session shell itself (`rockywait`, Phase 12),
  or a wrapper sources `session.sh` first.

Phase 11 (guard-SIGTERM branch, in the mandated order) and Phase 12 ran
cleanly: m0 unmounted in about 1 s, the guest poweroff took about 11 s, no
AVC, no relabel reboot; Rocky restarted at 17:24:06 UTC (PID `53482`), U2
done. Kept, and written into the lines they name:
- **Phase 1a (D1-a)** again: zq3-r12's window left both modules loaded.
- **Phase 4 step 1a** again: new daemon bytes (`c959b708…`).
- **R1:** N1, the Phase 1 time-zone expectation and the Phase 5 gate
  compute in `Europe/Sofia`; the gate is now `sh/n1-gate.py`, with a stamp
  rule added.
- Everything kept from zq3-r11 (above), the e94 pod `/2` request and the
  R11 checks.

## Execution notes from the zq3-r13 run (2026-11-19)

zq3-r13 ([record](../20261119-zq3-r13/README.md)) passed Phases 0 to 9:
Phase 1a unloaded both modules (rc 0 each), Phase 4 step 1a qualified the
nc4 daemon `c959b708…` (uid/gid 0, groups `[0,0,5]`, pid 23653), the Phase 5
gate printed GO, step 1b drained about 0.12 GB and Phase 6 passed (margin
0.49 GB). Phase 8 reached READY 37.0 s after launch with **both** mounts and
the exact g64 tuple, both signatures by the watcher and the `usable` gate
(L = 2): **R12-1 did not recur**. Phase 9 passed (OBSERVE L+1 = 3, both
providers `Open 0`; 984 timing: SSH no-op median 0.21 s, OBSERVE net
0.651 s). Then:
- **Operator pause:** the executing session paused about 55 minutes
  between READY (07:15Z) and Phase 9 (08:10Z), an agent login timeout. The
  guard's 3600 s bound runs from the Phase 8 launch (07:14:35Z) and ended at
  08:14:35Z, so T1-T11 were recorded unrun and Phase 11 took the
  guard-SIGTERM branch at once. From now on **Phase 9 starts within
  5 minutes of READY**, READY to Phase 11 runs in **one continuous session
  that cannot time out at login**, and **`gbudget`** (session.sh, over
  `sh/guard-budget.py`) prints the remaining guard seconds and decides GO or
  STOP before Phase 9 and before each budgeted step (Phase 10 table). A STOP
  goes to Phase 11 through its guard-SIGTERM branch.
- **R13-1:** after the runtime stopped, m1 (blocked) unmounted in 1 s, but
  m0's `umount` (its session not blocked) stayed in D (wchan
  `hfs_control`) beyond `rockyt`'s 270 s bound (`rc=124`), and the guest
  poweroff took about 7.5 minutes. The exporter's endpoint check failed and
  sent no reply, virtio dropped bhyve's empty completion, and the guest's
  9p wait was unbounded. Fixed by **oc4** (bounded 30 s reply wait,
  `transport_failed` stops later retries, teardown included) and **pc4**
  (the exporter always replies; ERROR 5 UNAVAILABLE) in this candidate.
  **R13-1 check:** Phase 11 step 2 (and T8) record each unmount's seconds
  and `rc`; after a host stop an unmount should return within about 30-60 s,
  and `rc=124` is a finding.
- **Operator slip (zsh):** Phase 11 step 9's first attempt ran from a zsh
  shell and failed at ssh option parsing (zsh passes the unquoted `$O` as
  one word), so nothing ran on f3; the files were checked present and the
  line was retried through the bash wrapper (`rc 0`). From now on **every
  session line runs in bash**: a wrapper sources `session.sh` and `vars.env`
  first (Session setup), and `session.sh` refuses to load in another shell.

Phase 11 (guard-SIGTERM branch, in the mandated order) and Phase 12 ran
cleanly otherwise: no transport fence (`p11-fence.json` `fenced: false`),
no AVC, the window restored (`mount_restored: true`), Rocky restarted at
08:24:51 UTC (PID `46923`), no relabel reboot, U2 done. Kept, and written
into the lines they name:
- **Phase 1a (D1-a)** again: zq3-r13's window left both modules loaded.
- **Phase 4 step 1a** stays required, although the daemon `c959b708…` is
  unchanged and passed it in zq3-r13: it is cheap and qualifies the bytes
  actually staged for this run.
- Everything kept from zq3-r12 (above): N1 and `sh/n1-gate.py`, the early
  Phase 1 gate, the in-run cutoff, `p11-fence.py`, `rockywait`, the e94 pod
  `/2` request and every finding rule.

## Execution notes from the zq3-r14 run (2026-11-19)

zq3-r14 ([record](../20261119-zq3-r14/README.md)) passed Phases 0 to 9
again: Phase 1a unloaded both modules (Refs 1/1, `rc 0` each), Phase 4
step 1a qualified the xc4 daemon `c959b708…` (uid/gid 0, groups `[0,0,5]`,
pid 80757), the Phase 5 gate printed GO (12:33Z and 12:36Z), step 1b
drained about 0.28 GB and Phase 6 passed (margin 0.41 GB). Phase 8 reached
READY 32 s after launch with both mounts and the exact g64 tuple, both
signatures by the watcher and `usable` (L = 2). Phase 9 started about 80 s
after READY, inside the new 5-minute budget, and passed (OBSERVE L+1 = 3;
984: SSH no-op median 0.329 s, OBSERVE net median 0.493 s). Then:
- **R14-1:** T1 (`credentials`) failed during guest admission. The m0 root
  OPEN (`open-root[0]`, 1.6 s after `identity-checked`) returned only about
  14.1 s later, behind three recalls (most likely one m0-only scoped recall
  from the root lstat's lease release and two all-domain recalls, at least
  one from the read-only OPEN conflicting with its own READ observation
  lease, z94). guest.py's 10 s admission alarm had fired, so the probe
  ended `Deadline` (`admission_ms` null, `deadline_hit` true, `etimedout`
  false) and the controller recorded `dispatching-uncertain` (18.2 s).
  T2-T11 were unrun (no retry). The host was not stuck: all three recalls
  and claims 7-12 completed, no fence, no block line, no `ETIMEDOUT`.
  Fixed by **z94** (this candidate's daemon) and **be4** (the guest
  deadlines above). Each recall cost about 4-5 s on the lab, mostly the
  per-ACK SSH observation (about 0.5 s per ACK, five ACK phases).
- **R13-1 is fixed:** in the Phase 11 guard-SIGTERM branch both sessions
  logged `blocked by uncertain control result on VIEW_WAIT (-112), exporter
  ERROR 5` and `view control worker stopped, error -112, exporter ERROR 5,
  at VIEW_WAIT` when the guard stopped the runtime (the designed oc4/pc4
  path), both `umount`s returned `rc 0` in 1 s, and the guest poweroff took
  seconds (zq3-r13: about 7.5 minutes).
- **Operator slip (child wait):** after the poweroff, the operator's own
  wait helper for the bhyve child first used the previous run's stale
  `hfs-zq3-r13` pattern; the child was then checked directly with
  `[h]fs-zq3-r14` and was already gone. From now on the step 6 line is run
  exactly as written in this sheet, with this run's pattern
  `[h]fs-zq3-r16` (zq3-r15 followed this); a hand-written wait helper must name the current run ID (`zq3-r19`) and be
  checked against the sheet before use, and a `child-gone` from a pattern
  that names another run counts for nothing.

Phase 11 and Phase 12 were otherwise clean: `fenced: false`, no AVC, the
window restored (`mount_restored: true`), Rocky restarted at 12:41:44 UTC
(PID 84753, boot `b4d9ee31-…`), no relabel reboot, U2 done. Kept, and
written into the lines they name: Phase 1a (zq3-r14's window left both
modules loaded again), Phase 4 step 1a (**required**, new daemon bytes),
the guard budget (now with the be4 budgets), bash only, the R13-1 unmount
timing in Phase 11 step 2 and T8, and every finding rule.

## Execution notes from the zq3-r15 run (2026-11-19)

zq3-r15 ([record](../20261119-zq3-r15/README.md)) passed Phases 0 to 9:
Phase 1a unloaded both modules (Refs 1/1, `rc 0` each), Phase 4 step 1a
qualified the **z94 daemon `8668e5f0…`** (uid/gid 0, groups `[0,0,5]`,
pid 7065; the bytes r16 reuses), Phase 3's pod rootfs manifest was the
predicted `34a71569…`, the Phase 5 gate printed GO (16:27Z and 16:29Z),
step 1b drained about 0.14 GB and Phase 6 passed (margin 0.42 GB). Phase 8
reached READY 33 s after launch with both mounts and the exact g64 tuple,
both signatures by the watcher and `usable` (L = 2). Phase 9 (budget GO,
3511 s left) passed (OBSERVE L+1 = 3; 984: SSH no-op median 0.219 s,
OBSERVE net median 0.483 s). Then:
- **R15-1:** T1 (`credentials`) failed during guest admission. z94 worked:
  the m0 root OPEN succeeded in 7.4 s (zq3-r14: 14.1 s) and the be4 alarm
  did not fire (`deadline_hit` false). But the following `os.fstat` of the
  root fd (`stat-root[0]`) failed after 7.9 s with `ESTALE` (errno 116).
  The provider ledger's freed pages show the fstat's GETATTR was never
  numbered or sent: `hfs_mmap_begin` (patch 0004) took its DOMAIN READ
  lease with VIEW_ACQUIRE, which stayed PENDING while a recall ran; the
  recall reinstalled the same cut and the host granted the lease, but the
  view revision had moved, so the post-acquire stamp re-check returned
  `-ESTALE` without a log line. The controller recorded
  `dispatching-uncertain` (24.1 s); T2-T11 were unrun (no retry). No
  fence, no block line during T1, `runtime.stderr` empty. Fixed by **ie4**
  (the r16 guest kernel): the observation acquires again.
- **R13-1 stays fixed:** after the guard stop both sessions blocked
  (m0 `VIEW_WAIT (-116), exporter ERROR 4`, m1 `VIEW_WAIT (-112), exporter
  ERROR 5`) and both `umount`s returned `rc 0` in 1 s.
- The child-wait line was used exactly as written (pattern
  `[h]fs-zq3-r15`); the bhyve child was gone at the second check.

Phase 11 and Phase 12 were otherwise clean: `fenced: false`, no AVC, the
window restored (`mount_restored: true`, `fusefs.ko` kept loaded by the
reviewed plan), Rocky restarted at 16:33:55 UTC (PID 11069, boot
`53cec809-…`), no relabel reboot, U2 done. Kept, and written into the lines
they name: Phase 1a (zq3-r15's window left both modules loaded again),
Phase 4 step 1a (**required**, although the daemon bytes are unchanged),
the be4 guard budget, bash only, the R13-1 unmount timing in Phase 11
step 2 and T8, and every finding rule.

## Execution notes from the zq3-r16 run (2026-11-20)

zq3-r16 ([record](../20261120-zq3-r16/README.md)) passed Phases 0 to 9:
Phase 1a unloaded both modules (Refs 1/1, `rc 0` each), Phase 4 step 1a
qualified the z94 daemon `8668e5f0…` again (uid/gid 0, groups `[0,0,5]`,
pid 74605), Phase 3's pod rootfs manifest was the predicted `34a71569…`,
the Phase 5 gate printed GO (04:58Z and 05:01Z), step 1b drained about
0.12 GB and Phase 6 passed (margin 0.41 GB). Phase 8 reached READY 32 s
after launch with both mounts (mountinfo 436 and 454) and the exact g64
tuple, both signatures by the watcher and `usable` (L = 2). Phase 9 (budget
GO, 3510 s left) passed (OBSERVE L+1 = 3; 984: SSH no-op median 0.232 s,
OBSERVE net median 0.675 s). Then:
- **ie4 worked:** m0's root OPEN and the following `fstat` succeeded and
  m0 was admitted (`root-admitted[0]` 4.6 s after `identity-checked`);
  m1's re-check logged the ie4 line (`observation re-check failed (view
  moved past the granted lease); acquiring again before releasing it`).
- **R16-1:** m1's root OPEN (`open-root[1]`) failed 8.3 s later with
  `EAGAIN` (errno 11, `BlockingIOError`). Its ie4 re-acquire (ticket #3)
  was answered BLOCKED on its first exchange because the control capacity
  was full: 1 (the request) + 3 unretired release tickets + 4 holders of
  m0's shared T1 lease + 1 reserved credit of the held lease = 9 against
  `ControlTickets` 8, shared by both mounts. The OPEN never reached the
  host. The controller recorded `dispatching-uncertain` (16.7 s); T2-T11
  were unrun (no retry). No fence, no block line during T1,
  `runtime.stderr` empty. Fixed by **qe4** (`ControlTickets` 24, this
  bundle) and **re4** (this candidate's guest kernel: release the held
  lease and retry after retirement progress; re-send refused retirements).
- **R13-1 stays fixed:** after the guard stop both sessions blocked
  (`VIEW_WAIT (-112), exporter ERROR 5`) and both `umount`s returned `rc 0`
  at once (0 s).
- The child-wait line was used exactly as written (pattern
  `[h]fs-zq3-r16`).

Phase 11 and Phase 12 were otherwise clean: `fenced: false`, no AVC, the
window restored (`mount_restored: true`, `fusefs.ko` kept loaded by the
reviewed plan), Rocky restarted at 05:05:29 UTC (PID 78540, boot
`302a5df1-…`), no relabel reboot, U2 done. Kept, and written into the lines
they name: Phase 1a (zq3-r16's window left both modules loaded again),
Phase 4 step 1a (**required**, now for new daemon bytes), the be4 guard
budget, bash only, the R13-1 unmount timing in Phase 11 step 2 and T8, and
every finding rule.

## Execution notes from the zq3-r17 run (2026-11-20)

zq3-r17 ([record](../20261120-zq3-r17/README.md)) passed Phases 0 to 9:
Phase 1a unloaded both modules (Refs 1/1, `rc 0` each), Phase 4 step 1a
qualified the qe4/te4 daemon `85900893…` (uid/gid 0, groups `[0,0,5]`, pid
42431), Phase 3's pod rootfs manifest was the predicted `34a71569…`, the
Phase 5 gate printed GO (15:31Z and 15:33Z), the active-based estimate left
only 17 MB of margin, step 1b drained about 0.43 GB and Phase 6 passed
(margin 0.38 GB). Phase 8 reached READY 31 s after launch with both mounts
(mountinfo 391 and 408) and the exact g64 tuple, both signatures by the
watcher and `usable` (L = 2). Phase 9 (budget GO, 3512 s left) passed
(OBSERVE L+1 = 3; 984: SSH no-op median 0.215 s, OBSERVE net median
0.832 s). Then:
- **R16-1 is gone:** no `VIEW_ACQUIRE` was answered BLOCKED, so re4's
  back-off never ran; m0 was admitted (`root-admitted[0]` 11.8 s after
  `identity-checked`) and m1's root OPEN succeeded (its re-check logged the
  ie4 line and acquired again).
- **R17-1:** m1's root `fstat` (`stat-root[1]`, 25.2 s after
  `identity-checked`) failed 5.9 s later with `OSError` errno 129
  (`EKEYREJECTED`). The guest blocked m1's session: `blocked by uncertain
  view ticket on VIEW_QUERY (-129), exporter ERROR -`. The host's verified
  GRANTED VIEW_QUERY reply for acquisition #5 (lease fence 6) reached the
  guest while its own REOPEN/REOPENED ACK of the recall was unsettled, so
  the guest refused it at receipt and blocked the session. The controller
  recorded `dispatching-uncertain`; T2-T11 were unrun (no retry). No fence,
  `runtime.stderr` empty. Fixed by **2f4** (this candidate's guest kernel:
  wait for the fence raise and query again; passed grants are only
  released). Admission was slow (about 10-12 s per root, mostly recalls);
  **je4** and **e84** (this candidate's daemon) cut those recalls.
- **R13-1 stays fixed:** after the guard stop m0 blocked (`VIEW_WAIT
  (-112), exporter ERROR 5`) and both `umount`s returned `rc 0`.
- The child-wait line was used exactly as written (pattern
  `[h]fs-zq3-r17`).

Phase 11 and Phase 12 were otherwise clean: `fenced: false`, no AVC, the
window restored, Rocky restarted at 15:37:52 UTC (PID 46196, boot
`1d9d3252-…`), no relabel reboot, U2 done. Kept, and written into the lines
they name: Phase 1a (zq3-r17's window left both modules loaded again),
Phase 4 step 1a (**required**, now for new daemon bytes), the be4 guard
budget, bash only, the R13-1 unmount timing in Phase 11 step 2 and T8, and
every finding rule; new: an `EKEYREJECTED` from a probe syscall is a
finding too.

## Execution notes from the zq3-r18 run (2026-11-21)

zq3-r18 ([record](../20261121-zq3-r18/README.md)) passed Phases 0 to 9:
the pre-run swap clearing (`swapoff -a`, `swapon -a`, 0 used), Phase 1a
(Refs 1/1, `rc 0` each), Phase 4 step 1a for the je4/ce4/e84 daemon
`086d7cde…` (uid/gid 0, groups `[0,0,5]`, pid 54156), the predicted pod
manifest `34a71569…`, the Phase 5 gate GO (11:45Z, 11:49Z), a drain
(`active_based` margin −1.25 GB, because Rocky's pages sat in laundry after
the swap clearing; the drain freed about 0.53 GB) and Phase 6 (margin
0.26 GB). Phase 8 reached READY 38.3 s after launch with both mounts
(mountinfo 391 and 410), both signatures by the watcher and `usable`
(L = 2). Phase 9 passed (budget GO, 3492 s left). Then:
- **T1 admitted both mounts in 7.27 s** (zq3-r17: 10-12 s per root;
  `deadline_hit` false, `etimedout` false).
- **R18-1:** the case leaf's `mkdir` took 18.0 s, then the case's
  `fchown` of a regular file failed with `OSError` errno 95
  (`EOPNOTSUPP`) at +73.2 s and the probe ended `dispatching-uncertain`
  after 76.0 s. Every chown/fchown of a regular file carries
  `ATTR_KILL_*` bits, and the 2f4 kernel's `hfs_setattr` refused them
  before any RPC. The guest also logged two `d_set_d_op` WARNINGs
  (`fs/dcache.c:1924/1925` from `hfs_namespace` ← `vfs_mkdir`) and four
  `observation VIEW_ACQUIRE BLOCKED by the host … retiring, then retrying`
  lines for m0. No block line, no fence, `runtime.stderr` empty. Fixed by
  **vf4** (this candidate's kernel). T2-T11 unrun.
- **Recalls (ce4):** T1's history held 7 recalls: two bootstrap DOMAIN
  `VIEW_RELEASE` recalls, then in T1 one `CALL_CONFLICT/MKDIR` scope ALL
  (13.1 s), and four `EXPECTED_TARGET/VIEW_ACQUIRE` recalls (ALL 9.7 s,
  DOMAIN 6.3 s, ALL 9.5 s, ALL 7.5 s), all COMPLETED. **wf4** (this
  candidate's daemon) scopes such expected-target recalls to the requesting
  domain when no peer holds a lease or reservation, so expect most of them
  `DOMAIN` (about 6 s) instead of `ALL` (7.5-13 s).
- **The review-round-1 failure OBSERVE could not run as written:** the
  case-failure block's `obs obs-$CASE-baseline obs-$CASE-fail` line was
  refused, because `correlate.py command` will not build a command for a
  controller run that recorded an error ("run state differs or recorded an
  error; inspect, never correlate"). As a recorded, read-only deviation the
  operator took one direct OBSERVE at sequence 5 (L+3) in the Phase 9 form,
  which captured T1's recall history. **This sheet uses that direct form**
  (yf4 rule, run plan "Case failure OBSERVE"; Phase 10's case-failure
  block).
- **The largest-total selection worked:** Phase 11 step 1b chose the
  seven-recall history.
- **Typo:** the zq3-r18 sheet's Phase 1 boot-ID line had a wrong suffix;
  the facts and `pins.json` were right. This sheet copies the boot ID
  exactly from the facts file (`rocky-phase1-20261121T151444Z.txt`).
- **Operator slips, none of which changed anything on a host** (this sheet
  marks every code block's first and last line and refers to no sheet line
  number):
  - a wrapper line-range bug left one Phase 4 step 0 local file with only
    the library hashes, so it was rebuilt in order;
  - a code-fence line and a prose line were run "by number" (bash syntax
    errors, nothing ran);
  - the `--no-child` cleanup line ran first and was refused by design (this
    sheet labels it "ONLY if the exporter is None" and puts the
    `--child-pid` line first);
  - a wait loop matched its own echoed command text (this sheet's step 6
    line matches the exact line `child-gone` with `grep -x`).

Phase 11 and Phase 12 were otherwise clean: guard SIGTERM, `fenced:
false`, both unmounts `rc 0` in 1 s (R13-1 stays fixed), no AVC, the window
restored, Rocky restarted at 11:58:43 UTC (PID 59685, boot
`c1dc3905-234b-4f83-91a9-4cb5e87b729f`), no relabel reboot, U2 done; iom1
and iom2 were restarted after Phase 12 (that restart is gone from this
sheet: iom1-3 now stay Stopped). Kept, and written into the lines they
name: Phase 1a (zq3-r18's window left both modules loaded again), Phase 4
step 1a (**required**, now for new daemon bytes), the be4 guard budget,
bash only, the R13-1 unmount timing in Phase 11 step 2 and T8, and every
finding rule; new: an `EOPNOTSUPP` from a probe syscall is a finding too,
and so is any guest `WARNING`/`BUG` line.

## New for zq3-r19

- **Candidate:** the r19 rebuild `09acd09d…` (194 files; source checkout
  and repository baseline `e160370`, no `inputs/repository-changes.patch`;
  package `/home/paul/tmp/claude/hfs-xz3-20261121-r19/package`).
  - **Guest kernel (vf4), new bytes:** bzImage `e5add01e…`, GNU build ID
    `e3d474df…`, System.map `91e970f0…`; config `28fe4a8e…`, the five
    modules, `Module.symvers` and `modules.*` unchanged. The kernel plan was
    regenerated (`792eadae`); against zq3-r18's it differs in `run_id`,
    `manifest_sha256`, the bzImage and System.map entries and the build ID.
  - **CGO daemon (wf4), new bytes:** `bb4ad73c…` (GNU build ID
    `841fe14d…`) in bootstrap `runtime.sha256`, the guard
    `--runtime-sha256`, `sh/p4-observer.py` and the staged binary; **Phase 4
    step 1a is required** for it.
  - **Window artifacts, new bytes:** `408cdafd…`/`d2ca3ab6…` (the r19
    window pins are the committed `pins.json` renamed, `4e73f869`).
  - **Same bytes:** the exporter `2fd7e32f…` (reused with the r17 native
    records) and the G3 init helper `8c02b98f…`.
- **Runtime spec:** unchanged since r17 (`ControlTickets` 24). The bundle
  was regenerated from it (`make-spec.py`: only the run ID and the operator
  key path change).
- **Tools rebuilt** at `f2f6c44`: `hfs-runtime-prep` `6220ecae` and the
  signer `6b723f18` (two identical builds each, the second forced with
  `-a`); both link `internal/viewcontrol` (wf4).
- **Re-pinned** (`git diff c9c11f4 f2f6c44 -- tools
  docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`):
  `rocky_kernel.py` `220fc897` and `controller.py` `d70620d3` (both admit
  only `09acd09d…`), **`guest.py` `005d9592`** (yf4: every case syscall is
  a named `p.call`, so a failure names its syscall; the batch template's
  `probe_sha256`; the synthetic batch passes `controller.py check` and the
  dry run ran it), the window `pins.json` and its build script default.
  Unchanged: `guest-lock-probe.py` `fd439897`, `pod_fixture.py`
  `07314543`, `pod_workload.py` `c714370f`, `bootstrap.py`, `correlate.py`,
  `bootstrap_remote.py` `0dcb0fe0…`, `fill_plan.py` `893350cf…`, `argv.py`
  `78462c2e…`, `runtime-guard.py` `fece96b3…` and the native test guard
  `576d802a…`.
- **Case-failure OBSERVE (yf4 rule):** the case-failure block's fresh
  OBSERVE is now the direct `tf3 observe <next> "doas -n $C --operation
  observe --runtime $U --sequence <next>" > $E/obs-$CASE-fail.json` line
  (zq3-r18 showed that the `obs` helper refuses after a failed case). The
  line's comment gives the sequence rule.
- **iom1, iom2 and iom3** (not HomelabFS, 2 GiB each): Stopped by default
  on Paul's decision (the iormesh session stopped them at about 15:00Z and
  will not start them without telling Claude). Phase 1 and Phase 5 step 1
  require all three `Stopped` (if one runs: `f3 "doas -n vm stop iomN"` on
  its own line, wait until `Stopped`, record it; never force). **The run
  never starts them:** Phase 12 and every abort path only check that they
  are still `Stopped`, and Claude messages the iormesh session after the
  run. The memory estimate and the Phase 6 bar assume all three stopped.
- **f3 swap and Phase 0b:** 891,068 KiB in use at the facts (42 %, left
  from an iormesh OOM incident at 14:20Z). **Phase 0b clears it before
  Phase 1, only with Paul's explicit approval for this run** and only with
  iom1-3 Stopped: free memory must exceed the swap in use, then
  `swapoff -a` and `swapon -a` on f3, each on its own line, then
  `swapinfo -k` shows 0 used. Without the approval the run stops before
  Phase 1a. Phase 6 still requires **swap unused**. zq3-r18's side effect:
  the cleared pages land in laundry, so Phase 5's `active_based` estimate
  reads short (zq3-r18: margin −1.25 GB); the UMA drain and the Phase 6
  bar decide (zq3-r18's Phase 6 margin was 0.26 GB).
- **be4 values kept:** every batch request `seconds` 240 and the plan
  `seconds` 255 (zq3-r14's 60 and 120 are still refused: the fill checked
  both refusals); lock-probe budgets 150/240; the guard budget table in
  Phase 10 and `sh/guard-budget.py`; the raised tool-timeout rule (Rules).
- **T7 pod rootfs manifest:** `pod_workload.py` and `pod_fixture.py` are
  unchanged and the pod runtime is byte-identical, so expect
  **`34a71569…`** again; any other digest must be explained before `build`.
- **Run ID `zq3-r19`** (same length as zq3-r18), native guard root
  `/var/tmp/hfs-20261121-zq3r19-fdq` (fill date 2026-11-21).
- **Expectations:**
  - through Phase 9 as in zq3-r18 (both mounts, READY, `usable`, the
    Phase 9 OBSERVE); Phase 4 step 1a passes for `bb4ad73c…`;
  - **T1 should now pass:** vf4 fixes R18-1, and with wf4 the three of
    four post-mutation expected-target recalls that ran `ALL` in zq3-r18
    should run `DOMAIN` (about 6 s instead of 7.5-13 s). The remaining
    risk is the recall cost per guest operation after a mutation against
    T1's 255 s dispatch bound (**xf4**, open). Then **T2-T4, T6, T7, T9,
    T10, T11 and T8** within the guard budgets (none of T2-T11 has run for
    real); **T4 `namespace` exercises link(2) on the fixed kernel**; T5
    stays deferred;
  - **record per-case admission timings** (`p10-admission.py`), the
    **recalls** from `controller.recalls` (`recalls.py`: trigger, scope,
    phase durations, ACK waits), **the named failing syscall** on any
    failure (yf4: guest.py's failure record names it), and **every
    2f4/ie4/re4/vf4 guest log line** (a not-yet-current grant waiting for
    the fence and re-queried, a passed grant released, `view moved` /
    `acquiring again`, a host `BLOCKED`, release-and-retry, retirement
    re-send, `retry exhausted`, any `WARNING`/`BUG`) as findings;
  - **any `EAGAIN`, `ESTALE`, `EKEYREJECTED`, `ETIMEDOUT` or `EOPNOTSUPP`
    from a probe syscall is a finding** (with the guest log lines around
    it), never a retry;
  - Phase 11's unmounts should again return within seconds (R13-1 check).
- **Remaining known risks, not fixed:**
  - **0a4:** the host never drops a participant that stops acknowledging;
  - **ue4:** release tickets still cannot retire during a recall; a host
    `BLOCKED` on a full ticket budget is a finding;
  - **xf4:** each guest operation after a mutation can still cost a recall
    (now mostly DOMAIN-scoped); a case whose recalls add up past the 255 s
    dispatch bound ends `dispatching-uncertain` (safe, a finding);
  - **tg4:** `ATTR_TOUCH` handling; zq3 does not use it (record if seen);
  - a re-acquire, re4 retry or 2f4 re-query loop that runs out of its
    budget returns an errno to the probe: a finding (with the guest log
    line), never a retry;
  - the memory margin: zq3-r18's Phase 6 margin was 0.26 GB after a drain;
    Phase 0b's swap clearing moves pages into laundry again. Phase 5's
    estimate, the drain and the Phase 6 bar decide, as before.
- **Kept:** every finding rule (`ETIMEDOUT`, `view control worker stopped`,
  `initial attributes timed out behind a recall`, block lines, an `ESTALE`,
  `EAGAIN`, `EKEYREJECTED` or `EOPNOTSUPP` from a probe syscall), the 984
  timing capture, the per-case admission-step timings and the oc4/pc4 errno
  records (`-112`/`-116`, `exporter ERROR <n>`).
- **Facts (2026-11-21 15:14:44Z, read-only, the baseline):**
  - f3 boot unchanged (`1794574308`, Paul's deliberate 2026-11-13 reboot);
  - Rocky's bhyve PID `59685`, UTC lstart `Sat Nov 21 11:58:43 2026`
    (started by zq3-r18's Phase 12), boot ID
    `c1dc3905-234b-4f83-91a9-4cb5e87b729f` (copied from
    `rocky-phase1-20261121T151444Z.txt`), running the stock
    `741.22.1.el9_9` = grub default, `next_entry` empty, Enforcing, no
    `/.autorelabel`, `authorized_keys` `e08542dc…`, python3.9
    `729ac37f…`; no `hfs-xz3` file in `/boot` (zq3-r18's U2 removed its
    test kernel; the kdump initramfs list has none either);
  - Rocky's clock and upgrade: `Europe/Sofia (EET, +0200)`, synchronized;
    stamp `2026-11-21` (00:55:06 EET); the newest dnf transaction is still
    **#65** (2026-11-20 00:55, fd-find only); the hourly ticks since only
    logged the deferred reboot. The next run is 2026-11-22 00:55 EET =
    2026-11-21 22:55 UTC, so **the latest Phase 5 start is 21:25 UTC**;
  - **UEFI vars `88bbe93f…`** (zq3-r18: `5ea958b3…`; its Phase 12 boot
    changed them), hence a new argv record; `rocky.conf` `c101adf1…`,
    vm-run `43753bc8…`, firmware `6b48e967…`, f3 `/usr/bin/ssh`
    `6a9e5573…` and python3.12 unchanged;
  - `10.20.0.133` on `vm-public` (members `tap0`, `re0`); `/dev/vmm` holds
    only `rocky`;
  - **`fdescfs.ko` (id 35) and `fusefs.ko` (id 36) loaded** (left by
    zq3-r18's window, no fuse/fdesc mount): Phase 1a runs;
  - zq3-r18's f3 directories exist (evidence; leave them);
  - iom1, iom2 and iom3 `Stopped`;
  - pod runtime byte-identical to zq3-r18's (podman `249e918c…`
    `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…`
    `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…`, seccomp `7dd44119…`);
  - f3 memory: free 10.19 GB (2,488,465 pages), inactive 0.12 GB, active
    0.32 GB, ARC 3.14 GB, swap 891,068 KiB used (zq3-r18's facts: 4.92,
    0.22 GB, ARC 4.95 GB, 883,332 KiB swap); Phase 0b, Phase 5's estimate
    and the Phase 6 bar decide.

## Decisions kept from the zq3-r7 to zq3-r18 run-sheet reviews

- **`rc` capture after `rockyt`:** see the zq3-r6 notes.
- **SELinux denials:** Phase 11 step 4a captures the guest's AVC records
  before the poweroff (`p11-guest-avc.txt`). Denials are a finding; on their
  own they are not a failure. zq3-r7 to zq3-r13 logged none.
- **Relabel detection:** Phase 12's `/.autorelabel` check runs at the first
  SSH answer, after any relabel reboot has already happened. The vm-bhyve log
  (a second "starting bhyve" run) is therefore the evidence that decides
  whether a relabel happened.
- **SELinux gates** in Phases 1, 9 and 12 (unchanged).
- **zq3-r8 review round 1:** the persisted-variable list (with `L`), the
  block-line rule and Phase 11 step 4b (above).
- **zq3-r9 review round 1:** the tool-timeout rule (rules above), `printf
  %q` quoting for `vars.env` (Session setup), the Phase 11 timing summary
  shown as reference text in Phase 9, the `usable` timing stated as a
  deadline, and no `set -o pipefail` in the Phase 8 Prepare block.
- **zq3-r10 review round 1:** the tool-timeout rule also covers every `$CTL`
  controller line, the T6/T8 lock-probe lines, T7's `pod_fixture.py run`,
  Phase 3's kernel install and pod build, and U3's `start-rocky` (at least
  300 s); a Phase 9 OBSERVE rerun writes `observe-p9-2.json`.

- **zq3-r11 review round 1:** T7 was skipped (`pod_fixture.py` pinned the
  pre-EL9.9 podman/crun NEVRAs); e94 moved those pins into the request, so
  T7 runs again here. A slow admission can still end `dispatching-uncertain`
  instead of a clean `Deadline` (the controller's 75 s dispatch bound);
  both are findings. Phase 11 step 2 captures the guest block lines right
  before and after the unmounts, and the failure block uses `$CASE`.
- **zq3-r11 execution:** never chain a follow-up after a bounded unmount
  that timed out (rules above).
- **zq3-r12 review round 1:** `p10-admission.py` flags `etimedout` in
  replies and observations, and Phase 11 scans the releases and T6-T11
  (kept); the nightly gate (now `sh/n1-gate.py`); `rockyt` 270 s with
  `rc=124` a finding; Phase 11 step 6 waits 90 × 10 s.
- **zq3-r12 execution:** run-time revision R1 (Rocky's clock is
  Europe/Sofia; N1) and never call a session function inside `bash -c`
  (`rockywait`, Phase 12).
- **zq3-r13 review round 1:** `p11-fence.py` decodes JSON-string fields
  (the `stdout` of `bootstrap-run/calls/*.json`), the in-run cutoff (no new
  case or step after 22:15 UTC on the run day, Phase 11 step 5 before
  22:45 UTC, the 22:35 UTC fallback), the case-failure block takes its live
  owner snapshot first, `rockywait` lines get a tool timeout of at least
  900 s, and the early Phase 1 gate.
- **zq3-r13 execution:** no pause between READY and the matrix (Phase 9
  within 5 minutes of READY, `gbudget` before each budgeted step), every
  session line in bash (the zsh slip), and the R13-1 unmount timing in
  Phase 11 step 2 and T8.
- **zq3-r14 execution:** the child-wait line is used exactly as
  written, with this run's pattern (the stale-pattern slip); the be4 bounds
  replace the 10 s / 60 s / 75 s alarms, and the guard budget and the
  tool-timeout rule follow them.
- **zq3-r15 review round 1:** tool-timeout comments on every `$CTL`,
  lock-probe (>= 300 s) and T7 pod run (>= 420 s) line (kept).
- **zq3-r15 execution:** R15-1 (an `ESTALE` from a probe syscall is a
  finding; ie4 was the r16 fix) and the ie4 log lines are recorded
  as findings; nothing in the procedure changed.
- **zq3-r16 execution:** R16-1 (an `EAGAIN` from a probe syscall is a
  finding; qe4's `ControlTickets` 24 and re4's guest retry are this
  candidate's fix) and the ie4/re4 log lines are recorded as findings;
  nothing in the procedure changed.
- **zq3-r17 execution:** R17-1 (an `EKEYREJECTED` from a probe syscall is a
  finding; 2f4 was the r18 fix); nothing in the procedure changed.
- **zq3-r18 execution:** R18-1 (an `EOPNOTSUPP` from a probe syscall is a
  finding; vf4 is this candidate's fix); the case-failure OBSERVE is the
  yf4 direct form; code blocks carry BEGIN/END marker lines and nothing is
  referenced by sheet line number; the `--no-child` cleanup line is labelled
  "ONLY if the exporter is None" and the child wait matches `child-gone`
  with `grep -x`; the boot ID is copied from the facts file.

## N1: Rocky's nightly upgrade

Rocky runs a daily `dnf -y upgrade` (zq3-r12's run-time revision R1).
Its time zone is `Europe/Sofia (EET, +0200)`.
`unattended-upgrade-rocky.timer` fires hourly at :55 local, but
`/usr/local/sbin/unattended-upgrade-rocky daily` runs `dnf upgrade` only
once per local calendar day, at the first tick after local midnight (00:55
EET = **22:55 UTC**). It stamps `/var/lib/unattended-upgrade/last-daily`
with the local date and never reboots Rocky ("reboot deferred: standalone
rocky requires explicit maintenance approval"). The test guest that boots
Rocky's disk also starts the timer (`OnBootSec=10min`), but the stamp
already holds today's local date, so it only logs the deferred reboot.

The fill's facts (2026-11-21 15:14:44Z = 17:14:44 EET,
`rocky-upgrade-20261121T151444Z.txt`): stamp `2026-11-21`, written
00:55:06 EET; the newest dnf transaction is still **#65** (2026-11-20 00:55,
1 package): it upgraded only `fd-find` (10.4.2-6.el9 → 10.5.2-1.el9,
`@epel`; `rocky-dnf65.txt` in the zq3-r16 facts), so the kernel, python3.9
and the pod runtime are unchanged (re-verified in the same facts). Rocky runs
`5.14.0-741.22.1.el9_9` = grub default = the kernel plan's stock default,
python3.9 is `729ac37f…`, and podman, crun, conmon and seccomp.json hash as
in zq3-r12 to zq3-r18. Their NEVRAs are pins too (e94). The journal also
shows one run at 10:35 EET, 11 minutes after zq3-r13's Phase 12 started
Rocky (10:24:51 EET): the timer's `OnBootSec` tick, which found today's
stamp and only logged the deferred reboot. Expect the same after this
run's Phase 12.

**The run must start after that day's run has stamped and finish before
the next one**, 00:55 EET on the next local day = **22:55 UTC on the run
day** (2026-11-21 22:55 UTC for a run on 2026-11-21). If it does not, the
upgrade can replace the kernel, python3.9, podman or crun:
- Phase 1 records the stamp and the dnf history (`p1-upgrade.txt`,
  `n1-gate.py phase1`). A newest transaction other than #65 means a dnf run
  since the fill's facts: record `dnf history info <ID>`, and Phase 1's
  pod-runtime NEVRA/SHA256 check (`p1-pod-runtime.txt`) and kernel facts
  (boot ID, running and default kernel, `next_entry`, python3.9) decide, as
  they do anyway;
- any drift stops before Phase 1a and needs a refill (new facts, a new pod
  template and possibly a new kernel plan), never an edit of a request.

**Go/no-go gate before Phase 5.** Phase 5 step 0 runs `sh/n1-gate.py gate`
locally, before anything stops Rocky. It computes in `Europe/Sofia`. R1's
three rules are unchanged and zq3-r13 added the stamp rule. It stops the run if:
- Phase 1 (`p1-rocky.txt`) ran before the last 00:55 Rocky-local upgrade;
- it is less than 35 minutes after 00:55 Rocky-local (the upgrade may still
  run);
- it is later than 23:25 Rocky-local = **21:25 UTC** (00:55 minus the 90
  minutes Phases 5-12 may take), so the **latest Phase 5 start is 21:25 UTC
  on the run day** (2026-11-21 for a run that day);
- the Phase 1 stamp is not the local date of the last 00:55 (that day's
  run had not stamped at Phase 1, so it may still be pending; added in
  zq3-r13).

The fill tested the exact helper at fixed times (`--now`, which the run
never passes): 13:30Z GO (also with the real 15:14Z upgrade record), 21:20Z
GO, 21:25Z GO, 21:30Z STOP, 22:56Z STOP, 23:10Z STOP, the next day with the
old Phase 1 STOP, the next day with a fresh Phase 1 and stamp GO, a stale
stamp STOP and 01:00Z (2 h after the upgrade) GO; `phase1` reports
`dnf_unchanged` true for the #65 facts and false for #64 and a synthetic #66. If the run slips to a later day, nothing here is retyped:
the gate computes the last and next 00:55 local and 23:25 local from its
own clock and the in-run Phase 1 files, and the in-run cutoff times below
(22:15, 22:35 and 22:45 UTC) apply on that run day. On `STOP` nothing is
selected and Rocky still runs: the
run aborts cleanly (observer key removed on Rocky and f3, U2, window
restored) and is rescheduled with fresh Phase 1 facts.

`pod_fixture.py` itself refuses at `check_runtime` if podman or crun changed
between Phase 1 and T7 (SHA256 or `runtime_nevra` differs): then T7 is
unrun, never edited around.

## Session setup (workstation, bash)

```bash
# ==== BEGIN Session-setup-workstation-bash block 1 (run each line on its own) ====
source /home/paul/tmp/claude/hfs-zq3-r19-fill/session.sh
# ==== END Session-setup-workstation-bash block 1 ====
```

`session.sh` (`0fa43208…`, [pins](pins.json) `fill_helpers`) defines, adapted
from zq3-r18's (a summary, not runnable as written; bash only):

```text
# ==== BEGIN Session-setup-workstation-bash block 2 (text: not a command, never run) ====
D=/home/paul/tmp/claude/hfs-zq3-r19-fill     # private 0700 fill dir
S=$D/snapshot                                # git archive of f2f6c44 (docs internal tools)
R=/var/tmp/hfs-validation-xz3/zq3-r19        # f3 staging prefix (parent exists, holds zq3-r1 to zq3-r18)
G=/var/tmp/hfs-zq3-zq3-r19                   # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
okpin() { …observer_key.py cd7f38c2… check… }   # before every stdin invocation of observer_key.py
rockyt(){ …rocky with a local `timeout 270` around the real ssh, </dev/null; exit 124 = bound expired (read it with `; echo rc=${PIPESTATUS[0]}`)… }
hpin()  { …sha256sum -c of bin/sign_watch.py (b278db65…), the signer bin/hfs-retained9p-operator (6b723f18…, rebuilt at f2f6c44) and the seventeen sh/* helpers… }
rockywait() { …Phase 12/U3: `rocky true` every 10 s until it answers, at most TRIES (default 36) tries; run it in bash that sourced session.sh (the bash-only wrapper is fine), never in a bare bash -c that did not source it… }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r19-kernel-stage/rocky-kernel-plan.json --plan-sha256 792eadaebfff25bb3aa5600ff395f915bba218e8d2014b1dc0607fcbac112abf --state /root/hfs-zq3-zq3-r19-kernel-state'
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261121-zq3r19-fdq/native-test-guard.py --root /var/tmp/hfs-20261121-zq3r19-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 59685 --rocky-birth 'Sat Nov 21 11:58:43 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
gbudget() { …Phases 9-10: sh/guard-budget.py over $D/bootstrap-run/journal.jsonl; `gbudget` prints the remaining guard seconds, `gbudget STEP` one JSON line with "decision" GO|STOP (exit 1 on STOP)… }
sha()   { …SHA256 of one local file… }
jf()    { …one field of a recorded JSON file: jf FILE KEY…… }
tf3()   { …tf3 LABEL SEQUENCE F3-ARGS: one f3 call, its wall time appended to $E/obs-timing.tsv (984 measurement)… }
obs()   { …Phase 10: obs PREV NEW [LAST], the next OBSERVE through correlate.py command, p10-argv.py and one timed f3 call… }
# ==== END Session-setup-workstation-bash block 2 ====
```

The guard's `--rocky-birth` is the UTC `lstart` (the guard runs `ps` with
`TZ=UTC`). Run every phase in this one shell session: later phases reuse `K`,
`KP`, `NG` and the variables set by earlier lines (`PM`, `F3_NOW`, `WP`, `W`,
`EA`, `P`, `B`, `U`, **`L`**, `C`, `CTL`, `CR`, `OUT`, `XP`, `ARM`, `RST`,
`T0` and the guard PID `<GUARD_PID>`). When lines run one at a time through a
wrapper (as in zq3-r7 to zq3-r13), append every `VAR=value` line's result to
the run's `vars.env` immediately with shell quoting, because `C` and `W`
contain spaces and `$`-references: `printf '%s=%q\n' L "$L" >> $D/vars.env`
(and so on for each variable),
and never rely on `set -o pipefail` persisting between lines: read exit codes
with `PIPESTATUS` as written. If the shell is lost, source `session.sh` again
and recompute each variable from its line, or reload it from `vars.env`:
every value comes from a file in `$E` or `$D`.

**Session functions are not exported** (zq3-r12's Phase 12 slip): `f3`,
`rocky`, `rockyt`, `tf3`, `obs`, `rockywait` and `gbudget` exist only in a
bash shell that sourced `session.sh`. Never call them inside `bash -c`,
`timeout … bash -c` or `sh -c` without sourcing first: there they fail with
"command not found", and a loop around them silently waits for nothing.

**Bash only (zq3-r13's zsh slip).** The operator's login shell is zsh. In
zsh the unquoted `$O` stays one word, so every ssh call fails at option
parsing (zq3-r13's first Phase 11 step 9 attempt; nothing ran). `session.sh`
now refuses to load outside bash (`session.sh: bash only …`, return 1).
Every line of this sheet therefore runs in bash: either one interactive
bash session that sourced the file, or, when each line runs through a tool
wrapper, a wrapper that sources both files first:

```text
# ==== BEGIN Session-setup-workstation-bash block 3 (text: not a command, never run) ====
bash -c 'source /home/paul/tmp/claude/hfs-zq3-r19-fill/session.sh && { test ! -e /home/paul/tmp/claude/hfs-zq3-r19-fill/vars.env || source /home/paul/tmp/claude/hfs-zq3-r19-fill/vars.env; } && <the sheet line>'
# ==== END Session-setup-workstation-bash block 3 ====
```

(`vars.env` holds the `printf %q` lines, so sourcing it restores `PM`,
`F3_NOW`, `WP`, `W`, `EA`, `P`, `B`, `U`, `L`, `C`, `CTL`, `CR`, `OUT`,
`XP`, `ARM`, `RST`, `T0` and the guard PID.) A wait loop such as
`rockywait 36` runs the same way.

## D1: f3 module state (decided: D1-a, Phase 1a)

The 15:14Z facts show `fdescfs.ko` (id 35) and `fusefs.ko` (id 36) loaded on
f3, left by zq3-r18 (its Phase 4 loaded them, its window restore kept them,
and f3 has not rebooted since Paul's deliberate 2026-11-13 reboot), with no
fuse or fdesc mount and `/dev/fd` on devfs. The window plan filler
`fdescfs-window-plan.py` requires both absent (`fdescfs_module: absent`,
`modules_forbidden: [fdescfs.ko, fusefs.ko]`).

The fill checked this locally with the r19 pins:
- zq3-r8's real Phase 4 facts (modules absent after the reboot, paths renamed
  to zq3-r19, the three artifact hashes the r19 ones) **fill**;
- the same facts with the r18 artifact hashes zq3-r18 staged, or
  with the xc4 ones, **refuse** (`pinned artifact differs or missing`);
- post-restore facts with both modules loaded, the state on f3 now,
  **refuse** (`fdescfs module present; plan is for absent`).

zq3-r3 to zq3-r7 and zq3-r9 to zq3-r18 all ran D1-a (a normal `kldunload` of
both, never refused), so this sheet runs it again as Phase 1a. D1-b (a
reviewed tool change) and D1-c (an f3 reboot) remain plan revisions, not
options at run time. Phase 4 loads `fdescfs` in `setup` and `fusefs` in
step 4, and both stay loaded after this run.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-21 by the fill:
- `hfs-runtime-prep` and the signer rebuilt at `f2f6c44` (they link the
  wf4 packages (internal/viewcontrol); two identical builds each, see the
  [README](README.md));
- the bundle (the committed spec, unchanged: `ControlTickets` 24, pinning
  the exporter `2fd7e32f…`; new sessions);
- the argv record (from the 15:14Z launch facts, UEFI vars `88bbe93f…`,
  exporter `2fd7e32f…`);
- the kernel plan (r19 candidate `09acd09d…`: the new vf4 kernel
  `e3d474df…`; stock default `741.22.1.el9_9`);
- the templates (T5 still deferred, r9 reason; be4 request `seconds` 240,
  plan `seconds` 255, lock-probe budgets 150/240; the e94 pod template `/2`
  with the unchanged podman/crun NEVRAs from the facts and the be4
  `pod_fixture.py` pin) and the filled bootstrap plan (F1);
- the staging trees and tarballs (the new vf4 kernel, the new wf4
  daemon and window artifacts; the same exporter bytes and be4 guest
  tools);
- the r19 window pins (the committed pins, renamed);
- the init helper (zq3-r1's reproduced `8c02b98f…`).

Local tests passed at `f2f6c44` (see [README](README.md#local-verification)).
Re-verify immediately before Phase 1:

```bash
# ==== BEGIN Phase-0 block 1 (run each line on its own) ====
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 093378b73019ad83a9dc2465e12c3138d538c03f586385008fedd3ab5c1eb83f --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 093378b73019ad83a9dc2465e12c3138d538c03f586385008fedd3ab5c1eb83f
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json \
  --plan-sha256 331136994f310b46c4d8a7d29a5840255778074b19b659969835b69226ff2c20
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.SYNTHETIC-shapecheck.json   # cases_planned: the four, deferred: mapped-pressure
sha256sum /usr/bin/ssh                                   # 16cb2c6a… (the batch template pins it)
hpin && okpin && sha256sum -c <<'EOF'
2693cd8201086c0bf3198f62da840d5043f5dbd638386729882b90adc55e5cfd  /home/paul/tmp/claude/hfs-zq3-r19-fill/stage/f3-stage.tar
dd29128355be9a12aeec7de82e3040d1c6ad39721067f292cec873881b513b65  /home/paul/tmp/claude/hfs-zq3-r19-fill/stage/rocky-stage.tar
792eadaebfff25bb3aa5600ff395f915bba218e8d2014b1dc0607fcbac112abf  /home/paul/tmp/claude/hfs-zq3-r19-fill/kernel/rocky-kernel-plan.json
c92a58f698f639e38a3f33eb5cb5aaa26a037cf4a61bee11aea6f3ea780e5fb3  /home/paul/tmp/claude/hfs-zq3-r19-fill/controller/batch-plan.TEMPLATE.json
4e73f8697b0e51190bdb363f83faba3187b3bd26bf64b634840f7b69b246b466  /home/paul/tmp/claude/hfs-zq3-r19-fill/fdescfs-pins/pins.json
0fa43208ebf1c95cfab36d2b6b1d4d5dbfafd7e25f630491a99099ed029b4226  /home/paul/tmp/claude/hfs-zq3-r19-fill/session.sh
EOF
cd $D
# ==== END Phase-0 block 1 ====
```

The tools snapshot `$S` is `f2f6c44`, the main HEAD at fill time; no refresh
is planned. If main moves before the run, `git diff --stat f2f6c44 $FINAL --
tools/ internal/controlledruntime/probe docs/lab/f3-host-known_hosts
docs/lab/rocky-known_hosts docs/lab/zq3-fdescfs-window` must be empty (or
touch only tests and docs); otherwise the changed pins make this a plan
revision. If a fill step is interrupted, a leftover `NEW.next` file makes
reruns refuse with "File exists": inspect it, then remove it by hand before
retrying.

**Operator key (before Phase 8, ideally now):** on Paul's instruction Claude
copies the operator signing key to
`/home/paul/tmp/claude/hfs-zq3-r19-fill/operator/operator-signing-key` (the
empty `0700` directory exists): a copy (one link), owned by `paul`, mode
`0600`, unencrypted Ed25519 PKCS#8 PEM or a 64-hex seed without newline,
matching public key `2f6d909c…`. The bundle's request template records
exactly this path; nothing in the fill reads it. A different path means
regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8, including
the Phase 1a unload and T7 (A8); T5 is deferred; N1: the run starts and ends before
Rocky's next nightly upgrade). Paul delegated both signatures (A7) to
Claude through the watcher in Phase 8.

## Phase 0b: pre-run swap clearing (A, needs Paul's explicit approval for this run; about 2 min)

**Run only after Paul approved it for this run** (his zq3-r18 approval does
not carry over). **Without that approval the run stops here, before
Phase 1a**: nothing has changed on any host. Run it only while iom1, iom2
and iom3 are all `Stopped`. Each line on its own:

```bash
# ==== BEGIN Phase-0b block 1 (run each line on its own) ====
f3 'doas -n vm list; swapinfo -k; sysctl -n hw.pagesize vm.stats.vm.v_free_count' | tee $E/p0b-swap-before.txt   # iom1, iom2, iom3 Stopped (any one running, or vm list failing: stop here and ask Paul); the last two lines are pagesize and free pages
python3 -I -c 'import sys; l=[x for x in open(sys.argv[1]).read().splitlines() if x.strip()]; page, free = int(l[-2]), int(l[-1]); used = sum(int(x.split()[2]) for x in l if x.startswith("/dev/")); print("swap-used-KiB:", used, "free-bytes:", page * free, "fits:", page * free > used * 1024); sys.exit(not page * free > used * 1024)' $E/p0b-swap-before.txt; echo rc=$?   # fits: True and rc=0 required, else stop and ask Paul
f3 "doas -n swapoff -a"     # tool timeout >= 300 s; an error: stop (swap stays as it was) and ask Paul
f3 "doas -n swapon -a"      # always right after swapoff, even if swapoff reported an error
f3 'swapinfo -k' | tee $E/p0b-swap-after.txt     # Used must be 0
# ==== END Phase-0b block 1 ====
```

The free-memory check uses free pages × page size against the swap in use
(15:14Z: 2,488,465 pages × 4096 = 10.19 GB free against 891,068 KiB =
0.91 GB used). `swapoff -a` pages everything back in; `swapon -a` re-enables
the device from `/etc/fstab`. Record both outputs and the time in the run
log. **Side effect (zq3-r18):** the paged-in memory lands in laundry, so
Phase 5's `active_based` estimate reads short (zq3-r18: margin −1.25 GB);
the UMA drain (step 1b) and the Phase 6 bar decide, as they did in zq3-r18
(Phase 6 margin 0.26 GB). Phase 1 then records swap 0 used; Phase 6 still
requires it.

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 2026-11-21 15:14Z facts ([facts/](facts/)) are the baseline. Immediately
before Phase 1a:

```bash
# ==== BEGIN Phase-1 block 1 (run each line on its own) ====
hpin
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); f=d["files"]; print([f[p]["sha256"] for p in ("/zroot/bhyve/rocky/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run", "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", "/zroot/bhyve/rocky/uefi-vars.fd")], f["/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"]["link"], d["bridge_members"], d["vmm"], d["interfaces"])' $E/p1-launch-facts.json
f3 sh -s < $D/sh/p1-f3.sh > $E/p1-f3.txt
rocky 'date -u; timedatectl; cat /proc/sys/kernel/random/boot_id; uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /; ls /boot | grep -c hfs-xz3; ls -d /etc/homelabfs /root/hfs-zq3-zq3-r19-* /var/tmp/hfs-zq3-zq3-r19 2>&1; ls -l /.autorelabel 2>&1; grep -E "^SELINUX(TYPE)?=" /etc/selinux/config' > $E/p1-rocky.txt
rocky 'echo "== stamp"; cat /var/lib/unattended-upgrade/last-daily; echo "== dnf-history"; dnf history list | head -n 3; echo "== timer"; systemctl list-timers --all --no-pager unattended-upgrade-rocky.timer | sed -n 2p' > $E/p1-upgrade.txt
python3 -I -B $D/sh/n1-gate.py phase1 $E/p1-upgrade.txt | tee $E/p1-upgrade.json   # stamp = today (Europe/Sofia), dnf_unchanged: true (newest #65)
python3 -I -B $D/sh/n1-gate.py gate $E/p1-rocky.txt $E/p1-upgrade.txt | tee $E/p1-nightly-gate.txt; echo rc=${PIPESTATUS[0]}   # early go/no-go (review round 1): STOP stops here, before Phase 1a
rocky "sha256sum /usr/bin/podman /usr/bin/crun /usr/bin/conmon /usr/share/containers/seccomp.json; rpm -qf --qf '%{NEVRA}\n' -- /usr/bin/podman /usr/bin/crun" > $E/p1-pod-runtime.txt
python3 -I -c 'import json,sys; t=json.load(open(sys.argv[1])); w=open(sys.argv[2]).read().split(); h=dict(zip(w[1:8:2], w[0:8:2])); now=[h[p] for p in ("/usr/bin/podman", "/usr/bin/crun", "/usr/bin/conmon", "/usr/share/containers/seccomp.json")] + w[8:]; pin=[t["runtime_sha256"][n] for n in ("podman", "crun", "conmon", "seccomp")] + [t["runtime_nevra"][n] for n in ("podman", "crun")]; print("pod-runtime-unchanged:", now == pin)' $D/pod/pod-request.TEMPLATE.json $E/p1-pod-runtime.txt
# ==== END Phase-1 block 1 ====
```

Continue only if all of these hold:

- **f3:**
  - kern.boottime is `{ sec = 1794574308, usec = 469376 } Fri Nov 13
    14:51:48 2026` (f3 prints local time, UTC+2: 12:51:48 UTC; unchanged
    since zq3-r8);
  - Rocky's bhyve PID is `59685` with UTC lstart `Sat Nov 21 11:58:43 2026`;
  - freebsd/hfs-test0..2 **and iom1, iom2 and iom3** are Stopped and
    `/dev/vmm` = `rocky`. If one of iom1-3 runs, stop it on its own line
    (`f3 "doas -n vm stop iom1"`, likewise iom2 or iom3; never force),
    repeat `f3 'doas -n vm list'` until it shows `Stopped` (at most about 2
    minutes; still running: stop and ask Paul), and record the stop in the
    run log. The run never starts them (their default state is Stopped);
  - swap: the `swapinfo` line shows **0 used** after Phase 0b (15:14Z
    before it: 891,068 KiB used, left from an iormesh OOM incident). Swap in
    use here means Phase 0b did not run or did not clear it: stop before
    Phase 1a and ask Paul (Phase 6 requires swap unused, because
    `runtime-guard.py` admits no swap use);
  - **`fdescfs.ko` and `fusefs.ko` are both in `kldstat`** (15:14Z: ids 35
    and 36, left by zq3-r18), `no-fuse-fdesc-mounts`, and `/dev/fd` is on
    devfs. Phase 1a unloads them;
  - `/var/tmp/hfs-validation-xz3` and `/var/tmp/hfs-fdescfs-windows` are both
    `drwx------ root wheel`, and all three r19 paths are `absent`;
  - the route section shows `interface: vm-public`, and `ifconfig vm-public`
    lists `inet 10.20.0.133` with members `tap0` and `re0`. Record it. If
    `10.20.0.133` is not on `vm-public`, stop: the observer key's
    `from="10.20.0.133"` would no longer match f3's source address;
  - the PoC share is present and f3 `/usr/bin/ssh` is `6a9e5573…`.
- **Launch facts:** `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware
  `6b48e967…` (link `../edk2-bhyve/BHYVE_UEFI.fd`), **vars `88bbe93f…`**,
  bridge members `re0 tap0` (no `tap100`).
- **Rocky:**
  - boot ID `c1dc3905-234b-4f83-91a9-4cb5e87b729f`;
  - **the running kernel equals the grub default**, and both equal the
    kernel plan's stock default `5.14.0-741.22.1.el9_9.x86_64`
    (`grubby --default-kernel` prints
    `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64`, `saved_entry` names the
    same kernel), `next_entry=` empty (N1; zq3-r11's S1 showed how a
    nightly upgrade without a reboot splits them). A mismatch, or a boot ID
    other than the one above (for example after the nightly 00:55 `dnf -y
    upgrade` and a reboot), stops here;
  - `timedatectl` shows `Time zone: Europe/Sofia (EET, +0200)` and
    `System clock synchronized: yes`, so its daily `dnf -y upgrade` runs
    at 00:55 EET = 22:55 UTC (N1, R1). Another time zone stops here: the
    Phase 5 nightly gate computes in `Europe/Sofia`;
  - **`p1-upgrade.json`**: `stamp` is today's local date (Europe/Sofia;
    `2026-11-21` for a run on that day: today's run has stamped) and
    `dnf_unchanged: true` (newest transaction #65 of 2026-11-20 00:55, as in
    the fill's facts). `dnf_unchanged: false` means a dnf run since the
    facts: record `rocky 'dnf history info <ID>'` in the run log; the
    pod-runtime and kernel checks below then decide, and any drift stops
    (refill). An older stamp means today's run has not happened yet: wait
    for it (the Phase 5 gate refuses that Phase 1 anyway) and redo Phase 1;
  - **`p1-nightly-gate.txt`: `nightly-gate: GO` and `rc=0`** (review round
    1: the Phase 5 gate run early, so a run that would span the upgrade
    stops before Phase 1a changes anything). `STOP` stops here with nothing
    changed on any host; Phase 5 step 0 still runs the gate again;
  - `Enforcing`, and `/etc/selinux/config` has `SELINUX=enforcing`;
  - **`/.autorelabel` absent** (`ls` reports "No such file or directory");
  - `authorized_keys` `e08542dc…` and python3.9 `729ac37f…`;
  - **`pod-runtime-unchanged: True`**: podman `249e918c…`
    (`podman-6:5.8.5-3.el9_9.x86_64`), crun `659867c7…`
    (`crun-1.29.1-1.el9.x86_64`), conmon `1ac2a8db…` and seccomp.json
    `7dd44119…` equal the pod template's pins (N1). `False` stops here: a
    nightly upgrade replaced them, and T7 would refuse; refill;
  - no `hfs-xz3` file in `/boot` (zq3-r18's U2 removed its test kernel), and
    `/etc/homelabfs` and every r19 path are absent. The r1 to r15 leftovers
    (`/root/hfs-zq3-zq3-r{1..15}-*` and `/var/tmp/hfs-zq3-zq3-r{1..15}`) stay
    as evidence.

`boot_indeterminate=7` is expected. It stayed at 7 through every Rocky start
since zq3-r7, so it is not a start counter. The interface order in the
launch facts may differ (15:14Z: `re0 lo0 vm-public pflog0 vm-hfslab tap0
wg0`); only the set and the bridge members count. The f3 run directories now
include zq3-r18's (`/var/tmp/hfs-20261121-zq3r18-fdq` and the zq3-r18
window and staging directories) and the xd4 native build root
`/var/tmp/hfs-xz3-20261119-r14-r1` (evidence; leave it).

**`/.autorelabel` present: stop** before Phase 1a, with nothing changed. The
test kernel's boot under the exporter would find it, relabel and reboot the
guest in the middle of Phase 8. Report it: it means a boot without SELinux
happened since 15:14Z. **A fusefs or fdescfs mount, or a module state other
than both loaded: stop** (a plan revision decides). Any other difference also
stops and revises the plan. A changed vars hash means a new argv record.
Another f3 boot or Rocky restart means a new boot time, PID and lstart for
`NG`, `p5-recheck.py` and the sheet (an f3 boot would also unload both
modules, so Phase 1a would then be skipped with that reason).

## Phase 1a: unload the modules zq3-r18 left loaded (A5, D1-a, about 2 min)

Right after Phase 1, before anything is staged. Each line on its own:

```bash
# ==== BEGIN Phase-1a block 1 (run each line on its own) ====
f3 'kldstat -v -n fusefs; kldstat -v -n fdescfs; mount -p | grep -E "fusefs|fdescfs" || echo no-fuse-fdesc-mounts' > $E/p1a-modules-before-unload.txt
f3 "doas -n kldunload fusefs"      # normal unload; "Device busy" or any error: stop, never -f
f3 "doas -n kldunload fdescfs"     # same
f3 'kldstat' > $E/p1a-modules-after-unload.txt   # neither fusefs.ko nor fdescfs.ko listed
# ==== END Phase-1a block 1 ====
```

Expect `Refs 1` for both files (`fusefs.ko` id 36, `fdescfs.ko` id 35) and
`no-fuse-fdesc-mounts` before the unload. After it, `kldstat` matches
zq3-r18's `p1a-modules-after-unload.txt` (`kernel`, `if_bridge.ko`, `vmm.ko`,
`nmdm.ko`, …), as after the zq3-r3 to zq3-r7 and zq3-r9 to zq3-r18 D1-a. Record both outputs in the run log. A
refused unload stops here: nothing is staged, Rocky runs, nothing else
changed (report; a plan revision decides). Then Phase 4 loads `fdescfs` in
`setup` and `fusefs` in step 4.

## Phase 2: stage on f3 (A2, about 3 min)

The parent `/var/tmp/hfs-validation-xz3` already exists (it holds zq3-r1 to
zq3-r18), so this checks it instead of requiring it absent. `paul` cannot look
inside a root `0700` directory, so absence is tested through `doas`. The
tarball has no entry for the parent, so extraction never rewrites its mode or
owner.

```bash
# ==== BEGIN Phase-2 block 1 (run each line on its own) ====
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-validation-xz3 /var/tmp/hfs-fdescfs-windows"   # both drwx------ root wheel
f3 "doas -n test ! -e $R && doas -n test ! -e /var/tmp/hfs-20261121-zq3r19-fdq && echo absent"   # must print absent
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261121-zq3r19-fdq > $E/p2-dirs.txt
# ==== END Phase-2 block 1 ====
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
NEW wf4 daemon `bb4ad73c…` (Phase 4 step 1a qualifies it), the native
exporter bhyve `2fd7e32f…` (same bytes; it ran in zq3-r14 to zq3-r18),
`controlledruntime.test` (`408cdafd…`) and `fdescfs-namespace` (`d2ca3ab6…`,
both the r19 builds from `e160370`, new), the G3 `homelabfs` helper (`0500`), the
controllers and `host.py`/`argv.py` (`78462c2e…`, exporter pin `2fd7e32f…`)/`host_snapshot.py`/
`runtime-volume-init.py` and `runtime-guard.py` (`fece96b3…`, unchanged)
(`0400`), the argv record, the volume-init plan, the private runtime config
(holds host receipt keys) and the Rocky pin file; the native guard
(`576d802a…`, unchanged) and `runner_process.py` go to
`/var/tmp/hfs-20261121-zq3r19-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

One command per line:

```bash
# ==== BEGIN Phase-3 block 1 (run each line on its own) ====
rocky 'for d in /root/hfs-zq3-zq3-r19-kernel-stage /root/hfs-zq3-zq3-r19-kernel-state /root/hfs-zq3-zq3-r19-incoming /root/hfs-zq3-zq3-r19-pod /root/hfs-zq3-zq3-r19-tools /var/tmp/hfs-zq3-zq3-r19 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r19-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r19 --observer /root/hfs-zq3-zq3-r19-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r19-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 729ac37ffbf7f0ded4b08a9d49e95145c8e46717bf485f76a3675b27255054a8' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
PM=$(jf $E/p3-pod-manifest.json manifest_sha256); echo "$PM"   # review: expect 34a71569… (zq3-r15's to zq3-r18's real digest; nothing in the rootfs changed since), then:
rocky "TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-pod/pod_fixture.py build --run-id zq3-r19 --expect-manifest-sha256 $PM" | tee $E/p3-pod-build.json
# ==== END Phase-3 block 1 ====
```

(Rocky's shell is bash, so `VAR=value` prefixes are fine there.) Expect:
install reports one new BLS entry (`<BLS_ENTRY_ID>`) for the vf4 kernel
(bzImage `e5add01e…`, build ID `e3d474df…`, System.map `91e970f0…`, new
bytes; config `28fe4a8e…` with SELinux in `CONFIG_LSM`, release
`6.1.190-hfs-xz3` and the five modules as in zq3-r13 to zq3-r18, whose BLS entry was
`b26c0ac534534d7fb81b3e0698558bc3-6.1.190-hfs-xz3`; the same release gives
the same entry ID), default still the
plan's stock kernel `5.14.0-741.22.1.el9_9.x86_64` (N1), `saved_entry`
unchanged, `next_entry` empty;
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. zq3-r12 to zq3-r14 got `523485cc…` (82
entries); be4's `pod_workload.py` (`c714370f`, 23,325 bytes) changed it,
and zq3-r15 to zq3-r18 got the predicted
**`34a71569f126ebeaa838a26788e75e1144b4d927228a4c9461940eb1fb2f46c4`**.
`pod_workload.py`, `pod_fixture.py` and the pod runtime are unchanged, so
expect that digest again. The rootfs is otherwise built from Rocky's own
installed files; the only dnf transaction since (#65) upgraded `fd-find`,
which the rootfs does not contain. If `PM` equals the prediction, record it and
run `build`; any other digest is not a failure but must be explained
(compare the entries with zq3-r18's `p3-pod-manifest.json`) before
`build`; record it. `PM` feeds the pod request in Phase 8. If Paul declines A8, skip
both pod lines and use `PM=skip` (T7 unrun).

**T7 runs (e94).** It has never run for real: zq3-r11 skipped it because
`pod_fixture.py` hard-coded the pre-EL9.9 podman/crun NEVRAs, zq3-r12
stopped in Phase 8, zq3-r13's matrix went unrun (operator pause) and
zq3-r14 to zq3-r18 stopped at T1 (R14-1, R15-1, R16-1, R17-1, R18-1). e94
moved those pins into the request: the pod template
(`homelabfs-retained9p-pod/2`)
pins `runtime_nevra` `podman-6:5.8.5-3.el9_9.x86_64` and
`crun-1.29.1-1.el9.x86_64` and the four `runtime_sha256` values, all from
the 15:14:44Z facts, and `check_runtime` requires `rpm -qf` and the hashes
to match them exactly. So both pod lines above run (A8), and `PM` is the
reviewed manifest digest, persisted in `vars.env`. `PM=skip` only if Paul
declines A8 (T7 then unrun). Failure: stop before Phase 5 and run U2
(`$K remove $KP`) while Rocky still runs.

## Phase 4: f3 window, fusefs, observer key (A5, A6; B for facts/tests; about 10 min)

Step 0, read-only facts (B). Both modules are absent since Phase 1a unloaded
them (its `p1a-modules-after-unload.txt`). Library
pins are by `realpath` (g54, zq3-r1 R4): `/usr/local/lib/libintl.so.8` is a
symlink to `libintl.so.8.4.6` and the controller hashes with `O_NOFOLLOW`.
`sh/p4-libs.sh` skips the four base libraries already in the first list (the
filler refuses duplicate paths):

```bash
# ==== BEGIN Phase-4 block 1 (run each line on its own) ====
hpin
f3 doas -n sha256 -r /usr/local/bin/python3.12 /boot/kernel/kernel /sbin/mount /sbin/kldload /sbin/kldstat /sbin/sysctl /bin/freebsd-version /bin/hostname \
  $R/bin/hfs-controlled-runtime $R/bin/fdescfs-namespace $R/bin/controlledruntime.test $R/bin/fdescfs-window.py \
  /boot/kernel/fdescfs.ko /boot/kernel/fusefs.ko /libexec/ld-elf.so.1 /lib/libc.so.7 /lib/libthr.so.3 /lib/libsys.so.7 > $E/p4-host.sha256
f3 'ldd /usr/local/bin/python3.12' > $E/p4-python-ldd.txt
f3 sh -s < $D/sh/p4-libs.sh > $E/p4-python-libs.sha256
cat $E/p4-python-libs.sha256 >> $E/p4-host.sha256
f3 "stat -f '%Sp %Su %Sg %N' /usr/local/bin/python3.12 /usr/local/bin /usr/local /usr /" > $E/p4-python-stat.txt
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime; date -u +%s" > $E/p4-facts-and-now.txt
f3 'sha256 -r /usr/bin/ssh' | diff - $D/facts/f3-usr-bin-ssh-20261121T151444Z.sha256 && echo F1-unchanged
f3 'env TZ=UTC ps -p 59685 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
# ==== END Phase-4 block 1 ====
```

Expect `p4-python-libs.sha256` to list exactly the real paths
`/usr/local/lib/libpython3.12.so.1.0`, `/usr/local/lib/libintl.so.8.4.6`,
`/usr/lib/libdl.so.1`, `/lib/libutil.so.10`, `/lib/libm.so.5` (the closure of
zq3-r1 to zq3-r13; zq3-r8 to zq3-r13 recorded `libpython3.12.so.1.0`
`6fc7b9b0…` after the 2026-11-10 package update, and the window plan takes live hashes, so any
other library or a changed hash is a fact change to review),
`F1-unchanged` (otherwise stop: the filled bootstrap plan pins `6a9e5573…`,
and refilling F1 is a plan revision), and `Sat Nov 21 11:58:43 2026` with
`/dev/vmm` = `rocky` and neither module in `kldstat`.

Split the facts and fill the window plan on the workstation (B) with the r19
window pins (`4e73f869…`: the committed pins at `f2f6c44`, with `zq3-r1` →
`zq3-r19`; daemon `bb4ad73c…`, helper `d2ca3ab6…`, test `408cdafd…`), then
review the printed SHA256. A refusal here (for example `fdescfs module
present`) means a module was loaded after Phase 1a: stop, nothing on f3 has
changed since except the Phase 1a unload.
The plan expires 55 minutes after `F3_NOW`; setup and qualify must run before
that (restore ignores expiry):

```bash
# ==== BEGIN Phase-4 block 2 (run each line on its own) ====
head -n 1 $E/p4-facts-and-now.txt > $E/p4-facts.json
F3_NOW=$(tail -n 1 $E/p4-facts-and-now.txt)
python3 -I -B $S/tools/lab/fdescfs-window-plan.py --pins $D/fdescfs-pins/pins.json \
  --facts $E/p4-facts.json --host-sha256 $E/p4-host.sha256 --expires $(( F3_NOW + 3300 )) --host-now $F3_NOW \
  --out $D/fdescfs-plan          # prints the window plan SHA256
WP=$(sha $D/fdescfs-plan/plan.json); echo "$WP"     # must equal the printed SHA256
f3 "doas -n sh -c 'umask 277; test ! -e $R/records/fdescfs-plan.json && cat > $R/records/fdescfs-plan.json'" < $D/fdescfs-plan/plan.json
f3 doas -n sha256 -r $R/records/fdescfs-plan.json      # must equal $WP
# ==== END Phase-4 block 2 ====
```

F1 (bootstrap `observer.ssh_sha256`) was filled at fill time from the 15:14Z
fact (`fill_plan.py bootstrap`, plan `33113699…`, `bootstrap.py check`
passed); the `F1-unchanged` line above is its recheck. Nothing to fill here.

Step 1, negative before setup (B; a skip counts as unrun). No `--` before the
command; a retry takes a new stage name and keeps the refused one. The o54
host profile needs free ≥ 1 GiB and free + inactive ≥ 3.5 GiB (15:14Z: 10.19 GB
free, 0.12 GB inactive, before Phase 0b):

```bash
# ==== BEGIN Phase-4 block 3 (run each line on its own) ====
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v" | tee $E/p4-unsupported-before.txt
# ==== END Phase-4 block 3 ====
```

Step 1a (B, **required**): process observer qualification of the exact
daemon bytes. The r19 package carries the NEW wf4 daemon `bb4ad73c…`,
so zq3-r18's check of the je4/ce4/e84 `086d7cde…` (pid 54156) does **not**
carry over: `bb4ad73c…` needs `--check-process-observer`
under the bounded host guard (xz3-20261121-r19 README). These lines must
pass before anything else changes on the host.

The staged daemon runs its read-only qualification once:
- under the native guard's host profile (Rocky pinned, 60 s, 512 MiB);
- as root through `doas`, the credentials `runtime-guard.py` later starts it
  with;
- before setup, while `/dev/fd` is still devfs, which this check does not
  need (the f44 run passed on devfs too).

It reads no config, starts no child and mounts nothing. Each line on its own:

```bash
# ==== BEGIN Phase-4 block 4 (run each line on its own) ====
f3 "$NG --stage process-observer $R/bin/hfs-controlled-runtime --check-process-observer" | tee $E/p4-process-observer-guard.json
f3 "doas -n cat /var/tmp/hfs-20261121-zq3r19-fdq/checks/process-observer/stdout" > $E/p4-process-observer.json
f3 "doas -n cat /var/tmp/hfs-20261121-zq3r19-fdq/checks/process-observer/stderr" > $E/p4-process-observer.stderr
python3 -I -B $D/sh/p4-observer.py $E/p4-process-observer-guard.json $E/p4-process-observer.json $E/p4-process-observer.stderr | tee $E/p4-process-observer-check.json
# ==== END Phase-4 block 4 ====
```

Pass criteria (`p4-observer.py`, `"pass": true`, exit 0):
- the guard ran exactly `$R/bin/hfs-controlled-runtime
  --check-process-observer`, with `error` null and child exit 0;
- stderr is empty;
- stdout is one `homelabfs-process-observer/1` record with `platform`
  `freebsd/amd64`, `executable` the staged path, **`sha256` `bb4ad73c…`** and
  `publication` null;
- `uid` 0 and `gid` 0 (the gid among `groups`), a positive `pid`, an
  `incarnation` `freebsd:<pid>:…` and a positive `text_vnode`.

zq3-r5 to zq3-r18 passed exactly these criteria as root (uid/gid 0, groups
`[0, 0, 5]`; zq3-r18 for the je4/ce4/e84 `086d7cde…`, pid 54156). zq3-r18's real
records, renamed to zq3-r19 and with `bb4ad73c…` substituted, **pass** this
check; unchanged (the je4/ce4/e84 `sha256`) they fail on the `sha256` only.

**Failure: stop before Phase 5**, so Rocky is never stopped. Nothing is
selected yet, the window is not set up and no observer key exists. Keep
`checks/process-observer` on f3 as evidence, run U2 (`rocky "$K remove
$KP"`) while Rocky runs, record and report: the daemon is not
qualified on this f3 boot, and no runtime may start with it. Both modules are absent
since Phase 1a, which is harmless. Only a guard refusal before the child
started (receipt `exit` null, `error` naming a guard bound such as the
free-page floor) may be retried, once, as `--stage process-observer-2` after
recording the cause. A child exit other than 0 or a record mismatch is never
retried.

Steps 2-4 (A5): setup, qualify (under the guard), fusefs. The root-private
parent `/var/tmp/hfs-fdescfs-windows` exists (`root wheel` `0700`, with
`zq3-r1`, `zq3-r1.refused-setup-1`, `zq3-r2` to `zq3-r13`); it is checked
here, never created. `setup` refuses an existing run directory:

```bash
# ==== BEGIN Phase-4 block 5 (run each line on its own) ====
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-fdescfs-windows"   # drwx------ root wheel /var/tmp/hfs-fdescfs-windows
f3 "doas -n test ! -e /var/tmp/hfs-fdescfs-windows/zq3-r19 && echo absent"   # must print absent
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 $WP --directory /var/tmp/hfs-fdescfs-windows/zq3-r19"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
# ==== END Phase-4 block 5 ====
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots (or a later
approved unload). If setup or qualify refuses and a reviewed fix follows,
resume as zq3-r1 did: keep the refused window directory under a new name
(`zq3-r19.refused-setup-1`, via `printf … | f3 doas -n sh -s`), and rerun
step 0 and the window plan fill with `-resume` names (`p4-*-resume.*`, `--out
$D/fdescfs-plan-resume`, remote `$R/records/fdescfs-plan-resume.json`, `WP`
and `W` pointing at it). The plan's expiry also forces this refill after a
pause of more than 55 minutes.

Step 5 (A6), run-only observer key:

```bash
# ==== BEGIN Phase-4 block 6 (run each line on its own) ====
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r19-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r19-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r19 --public-key /root/hfs-zq3-zq3-r19-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 729ac37ffbf7f0ded4b08a9d49e95145c8e46717bf485f76a3675b27255054a8 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
EA=$(jf $E/p4-observer-enrol.json after_sha256); echo "$EA"   # authorized_keys after enrolment; needed in Phase 11
# ==== END Phase-4 block 6 ====
```

The private key never leaves f3. Failure: nothing unloaded; U2 the unselected
kernel; restore the window (Phase 11 step 10); remove the observer key if it
was enrolled (execution notes).

## Phase 5: pre-stop rechecks, memory, one-shot select and stop Rocky (A1, A3; about 6 min)

Step 0, **nightly-upgrade gate** (B, N1; R1's rules in `sh/n1-gate.py` plus
the stamp rule), before anything else in Phase 5 and so before Rocky is
stopped:

```bash
# ==== BEGIN Phase-5 block 1 (run each line on its own) ====
python3 -I -B $D/sh/n1-gate.py gate $E/p1-rocky.txt $E/p1-upgrade.txt | tee $E/p5-nightly-gate.txt; echo rc=${PIPESTATUS[0]}
# ==== END Phase-5 block 1 ====
```

It prints `nightly-gate: GO` and `rc=0` only if Phase 1 (`p1-rocky.txt`)
ran after the last 00:55 Rocky-local upgrade, it is at least 35 minutes after
that upgrade, it is not later than 23:25 Rocky-local = 21:25 UTC (00:55
minus 90 minutes, the budget from Phase 5 to the end of Phase 12; the
latest GO is 21:25 UTC on the run day), and the Phase 1 stamp
(`p1-upgrade.txt`) is the local date of the last 00:55 (that day's run had
stamped). The gate only admits the start: from Phase 8 on, the in-run
cutoff applies (no new case or step after 22:15 UTC on the run day; Phase
11 step 5's guest poweroff before 22:45 UTC; see the top of Phase 8), and
so does the guard budget (Phase 10).
**`STOP` (`rc=1`; the `rc`
is the check's own, not `tee`'s): do not proceed.** Abort cleanly while Rocky still runs its stock kernel: nothing
is selected yet. Each on its own line: remove the observer key on Rocky
(`okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r19
--expect-sha256 $EA" < $S/tools/lab/retained9p/observer_key.py | tee
$E/p5-abort-observer-remove.json`), then on f3 with Phase 11 step 9's plain
literal line; U2 (`rocky "$K remove $KP"`); restore the window (Phase 11
step 10, the `$W restore` line and the `restoration-summary.json` line).
Record the gate output and the abort in the run log; a new attempt needs
fresh Phase 1 facts after the upgrade (and a refill on any drift).

Step 1, pre-stop rechecks (B, g54, zq3-r1 R5). In zq3-r1 an external f3
power-off during a pause restarted Rocky and dropped the window, and select
and stop then ran on the new instance. These compare with the Phase 1 facts:

```bash
# ==== BEGIN Phase-5 block 2 (run each line on its own) ====
hpin
f3 'sysctl -n kern.boottime' > $E/p5-boottime.txt              # expect: { sec = 1794574308, usec = 469376 } Fri Nov 13 14:51:48 2026
f3 'env TZ=UTC ps -p 59685 -o lstart=' > $E/p5-rocky-lstart.txt  # expect: Sat Nov 21 11:58:43 2026
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime" > $E/p5-facts.json
f3 doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r19/owned.json > $E/p5-owned.json
python3 -I -B $D/sh/p5-recheck.py $E/p5-boottime.txt $E/p5-rocky-lstart.txt $E/p5-facts.json $E/p5-owned.json | tee $E/p5-recheck.json
f3 'doas -n vm list; swapinfo -k' | tee $E/p5-iom-swap.txt     # r19: iom1, iom2 and iom3 must show Stopped; swap 0 used after Phase 0b (Phase 6 decides)
# ==== END Phase-5 block 2 ====
```

r19: if iom1, iom2 or iom3 is not `Stopped`, this is a failed recheck: stop
it on its own line as in Phase 1 (`f3 "doas -n vm stop iomN"`, never force),
wait until `vm list` shows `Stopped`, record it, and repeat step 1a's
estimate before select (the memory bar assumes all three stopped).

`p5-recheck.py` passes (exit 0, `"pass": true`) only if the boot time (also
the `boot` field of `facts`) and the UTC lstart equal the values above, and
the parsed `descriptor` of `facts` equals the parsed `owned.json` (a
supported `fdescfs` report with the owned FSID).

Step 1a, memory pre-estimate (B) and the disk-level reference:

```bash
# ==== BEGIN Phase-5 block 3 (run each line on its own) ====
f3 'doas -n vm list; swapinfo; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_active_count vm.stats.vm.v_laundry_count vm.stats.vm.v_wire_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min; ps -p 59685 -o rss=' > $E/p5-mem.txt
python3 -I -B $D/sh/p5-estimate.py $E/p5-mem.txt | tee $E/p5-estimate.json
# ==== END Phase-5 block 3 ====
```

`p5-estimate.py` prints two estimates of what Phase 6 will measure. The
zq3-r2 sheet's `rss_based` one, free + inactive + (ARC − `c_min`) + Rocky's
bhyve RSS, is **pessimistic**: on f3 Rocky's guest memory shows as Active
pages, not in the bhyve RSS. The better estimate is `active_based` = free +
inactive + (ARC − `c_min`) + Active. It is an upper bound, because
v_active_count also holds the host's own Active pages. zq3-r3 measured
`active_based` 15.14 GB here (margin 0.38 GB), zq3-r4 15.19 GB (margin
0.43 GB), zq3-r5 15.23 GB (margin 0.46 GB), zq3-r6 15.22 GB (margin
0.45 GB), zq3-r7 15.23 GB (margin 0.47 GB), zq3-r8 15.22 GB (margin
0.45 GB), zq3-r9 15.24 GB (margin 0.48 GB), zq3-r10 15.27 GB (margin
0.51 GB), zq3-r11 15.14 GB (margin 0.37 GB), zq3-r12 15.22 GB
(margin 0.45 GB) and zq3-r13 15.21 GB (margin 0.45 GB); all eleven
drained (zq3-r14: 0.22 GB margin, step 1b freed about 0.28 GB,
Phase 6 margin 0.41 GB; zq3-r15: 0.35 GB margin, step 1b freed about
0.14 GB, Phase 6 margin 0.42 GB; zq3-r16: 0.37 GB margin, step 1b freed
about 0.12 GB, Phase 6 margin 0.41 GB; zq3-r17: only 0.017 GB margin, step
1b freed about 0.43 GB, Phase 6 margin 0.38 GB; zq3-r18: margin −1.25 GB
after its swap clearing put Rocky's pages in laundry (1.09 GB), step 1b
freed about 0.53 GB, Phase 6 margin 0.26 GB). At 15:14Z, about 3 hours
after Rocky's start and with iom1-3 stopped, free was 10.19 GB and
inactive 0.12 GB, with ARC 3.14 GB and 891,068 KiB of swap in use (before
Phase 0b; zq3-r18's facts: 4.92, 0.22 and 4.95 GB). Phase 0b moves the
swapped pages back into memory (laundry), as in zq3-r18. `active_based` counts ARC above `c_min` and Active as well, so the
estimate should be similar to the earlier runs, and Phase 6 decides.

Step 1b, drain the UMA caches (A1, from zq3-r2 R2-1), **only if**
`p5-estimate.json` shows `"drain_recommended": true`, which means
`active_based` is short of the Phase 6 bar or within 1 GiB of it. Record the
decision in the run log either way. Each line on its own:

```bash
# ==== BEGIN Phase-5 block 4 (run each line on its own) ====
f3 sh -s < $D/sh/p5-uma-stat.sh > $E/p5-uma-before.txt
printf 'sysctl debug.uma_reclaim=2\n' | f3 doas -n sh -s | tee $E/p5-uma-reclaim.txt
f3 sh -s < $D/sh/p5-uma-stat.sh > $E/p5-uma-after.txt
# ==== END Phase-5 block 4 ====
```

`p5-uma-stat.sh` records pagesize, free, wired, inactive and active pages
and `uma_free_bytes` (item size × free items over `vmstat -z`). Expect free
pages to rise and wired pages and `uma_free_bytes` to fall. In zq3-r2 free
rose by 2.2 GB; in zq3-r3 the drain freed about 0.23 GB, in zq3-r4 about
0.18 GB, in zq3-r5 about 0.15 GB, in zq3-r6 about 0.17 GB, in zq3-r7 about
0.15 GB, in zq3-r8 about 0.19 GB, in zq3-r9 about 0.16 GB, in zq3-r10
about 0.13 GB, in zq3-r11 about 0.20 GB, in zq3-r12 about 0.12 GB and in zq3-r13 about 0.12 GB. The drain is the same
reclaim the pagedaemon runs under pressure, done now so that free pages are on
hand for Phase 6. **Never cap `vfs.zfs.arc_max`:** in zq3-r2 the freed ARC
stayed in UMA caches, so free pages did not rise, and the cap was reverted at
once.

**Failure in step 1** (any recheck differs): stop before step 2. Nothing is
selected, so `next_entry` is still empty (confirm with `grub2-editenv - list`
once Rocky answers; the `rocky_kernel.py` state journal shows no select) and
U1 is not needed. Remove the observer key (execution notes). Two cases (run
plan, Phase 5):

- f3 booted since Phase 1 (boot time differs): the window mount and modules
  are gone. Record the retained window directory by hand (`fdescfs-window.py`
  has no action for a vanished mount): `f3 "doas -n ls -la
  /var/tmp/hfs-fdescfs-windows/zq3-r19"` and
  `f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r19" >
  $E/f3-window-evidence-vanished.tar`. Rocky has AUTO yes, so it normally runs
  again: then U2. If it is down, check the PoC share (`f3 'ls -ld
  /var/tmp/homelabfs-9p-poc/share'`, risk R6), start it normally
  (`$S/tools/lab/vms.sh --approved-action start-rocky`), then U2.
- Otherwise (same boot, Rocky runs, a PID/lstart or mount mismatch): inspect
  and stop; U2, then restore the window (Phase 11 step 10, the `$W restore`
  line) only if `facts` still shows the owned FSID.

Steps 2-3, select and stop:

```bash
# ==== BEGIN Phase-5 block 5 (run each line on its own) ====
rocky "$K select $KP" | tee $E/p5-select.json        # next_entry = <BLS_ENTRY_ID>; on any failure it runs U1 itself
$S/tools/lab/vms.sh --approved-action stop-rocky      # clean vm stop, waits up to 120 s, never forces
f3 'doas -n vm list; ps -ax | grep "[b]hyve: rocky" || echo no-rocky-process; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm; ifconfig -l' > $E/p5-stopped.txt
# ==== END Phase-5 block 5 ====
```

Failure in step 2, or if step 3 refuses before stopping anything:
`rocky "$K cancel $KP"` (U1), then U2. Failure after the stop: `next_entry`
is still set, recovery is U3 (below).

## Phase 6: post-stop admission (B, 1 min)

```bash
# ==== BEGIN Phase-6 block 1 (run each line on its own) ====
hpin
f3 'swapinfo; doas -n vm list; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm; mount -p | grep /dev/fd' > $E/p6-admission.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min' > $E/p6-mem.txt
python3 -I -B $D/sh/p6-admission.py $E/p6-mem.txt | tee $E/p6-bar.json
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" > $E/p6-descriptor.json
python3 -I -c 'import json,sys; a, b = (json.load(open(p)) for p in sys.argv[1:]); print("descriptor-equals-owned:", a == b); sys.exit(a != b)' $E/p6-descriptor.json $E/p5-owned.json
# ==== END Phase-6 block 1 ====
```

The bar (g54) is the enforced guard ceilings plus a 3 GiB host reserve:

| Part | Bytes |
|---|---|
| bhyve RSS ceiling, 6 GiB + 512 MiB | 6,979,321,856 |
| runtime RSS ceiling, 4 GiB + 256 MiB | 4,563,402,752 |
| host reserve, 3 GiB | 3,221,225,472 |
| **bar, 13.75 GiB** | **14,763,950,080** (= 3,604,480 pages of 4096) |

Required: reclaimable = (free + inactive) × 4096 + (ARC − `arcstats.c_min`) ≥
14,763,950,080 (`p6-bar.json` `"pass": true`); **swap unused** (the
`swapinfo` line in `p6-admission.txt` shows 0 used; r19: swap was in use at
the facts and Phase 0b cleared it, see below); all VMs stopped (iom1,
iom2 and iom3 included)
and `no-vmm`; `/dev/fd` still the owned fdescfs mount
(`descriptor-equals-owned: True`). For reference, zq3-r3 measured
15,303,398,240 bytes here after its Phase 5 drain, zq3-r4 15,305,552,288
(margin 0.54 GB each), zq3-r5 15,294,364,056 (margin 0.53 GB), zq3-r6
15,329,786,840 (margin 0.57 GB), zq3-r7 about 15.33 GB (margin 0.57 GB) and
zq3-r8 15,283,237,008 (margin 0.52 GB), zq3-r9 15,342,496,000 (margin
0.58 GB), zq3-r10 15,338,803,152 (margin 0.57 GB), zq3-r11
15,264,448,296 (margin 0.50 GB), zq3-r12 15,267,994,272 (margin
0.50 GB) and zq3-r13 15,251,768,616 (margin 0.49 GB).
**r19 swap rule.** `runtime-guard.py` samples `swapinfo -k` and treats any
swap use above `--swap-limit-bytes` 0 as a breach (one SIGTERM), so a launch
with swap in use would end at the first sample. If the swapped pages were
Rocky's bhyve, stopping Rocky freed them and the line shows 0. If swap is
still in use here: do not launch. Restore the window (Phase 11 step 10),
remove the observer key (Phase 11 step 9's literal line on f3; on Rocky after
U3), then U3, and ask Paul: Phase 0b is the only swap clearing this sheet
covers, and it runs before Phase 1, never again here.

`c_min` is the effective ARC floor that `runtime-guard.py` also subtracts
(about 527 MB on f3); `vfs.zfs.arc_min` 0 only means "default floor".

**Only if** the bar fails and step 1b did not run in Phase 5: run the three
step 1b lines once with `p6-uma-*` names, then repeat the `p6-mem` and
`p6-bar` lines as `p6-mem-2.txt`/`p6-bar-2.json`. Do this at most once. If
the bar still fails, restore the window (Phase 11 step 10, `$W restore` and
the summary line), remove the observer key (Phase 11 step 9's literal line on
f3; on Rocky after U3), then U3.

## Phase 7: host setup (A4, about 3 min)

```bash
# ==== BEGIN Phase-7 block 1 (run each line on its own) ====
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 093378b73019ad83a9dc2465e12c3138d538c03f586385008fedd3ab5c1eb83f" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 a363ef1c96873893cf0edcdd9c2dc08ef299efa5194663e5639b60f0b35d5f58 --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r19 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
# ==== END Phase-7 block 1 ====
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`88bbe93f…` (any Rocky boot since 15:14Z changes it; it was `5ea958b3…` at
zq3-r18, and Rocky's last start was zq3-r18's Phase 12 at 2026-11-21
11:58:43 UTC): stop;
regenerating the argv record from fresh facts (same sessions,
same args) is a plan revision for review, never an on-the-spot edit. Failure
with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3. `k04-baseline.json` is T11's
required baseline (the empty mount point before the runtime starts).

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s to READY plus about 100 s for `usable`, about 7 min with prep)

**In-run cutoff (zq3-r13 review round 1).** No new case or lifecycle step
(T1-T11, T8 included) starts after **22:15 UTC on the run day** (2026-11-21
for a run that day; 00:55 EET minus 40 minutes). At 22:15 UTC go straight to Phase 11 (the
guard-SIGTERM branch while the runtime serves), so that Phase 11 step 5's
guest poweroff is done **before 22:45 UTC**: Rocky's daily `dnf upgrade` at
22:55 UTC must never run against Rocky's disk while the test guest boots it
(the guest starts the same timer, and its stamp is the previous local day
after midnight). If step 5 has not run by 22:35 UTC, run step 3 (observer
removal) and step 5 at once, record the skipped evidence steps (4a, 4b) in
the run log, and continue with step 6.

Signing is delegated to Claude (A7, Paul's decision). The driver's stdout goes
through the watcher `bin/sign_watch.py` (`b278db65…`), which is zq3-r18's
watcher (it signed both requests in zq3-r12 to zq3-r18) with `D`, the
comments and the signer pin changed. It copies every
line through. For an `operator_action` line it runs the signer only if:
- the printed command is exactly one of the two commands below;
- the request file's SHA256 equals the printed `request_sha256`;
- the signer still hashes to `6b723f18…`;
- that action has not run before (each runs at most once).

Anything else is logged as `"watcher": "refused"` and left unsigned, so the
driver times out (fails closed). The signer pin lives in two places, `hpin`
(`session.sh`) and the watcher's `SIGNER_SHA256`. The signer is rebuilt at
`f2f6c44`: it links the wf4 packages (internal/viewcontrol), so zq3-r18's `f6d0b8f7…` no
longer matches the source.
Prepare:

```bash
# ==== BEGIN Phase-8 block 1 (run each line on its own) ====
hpin
stat -c '%a %U %h %s' $D/operator/operator-signing-key   # 600 paul 1 <size>; placed by Claude on Paul's instruction
test ! -e $D/bootstrap-run && echo output-absent
P=$D/bootstrap/bootstrap-plan.json
# ==== END Phase-8 block 1 ====
```

(`set -o pipefail` is not listed: it does not persist between wrapper lines.
The driver's outcome is read from its journal and `summary.json`, not from
the pipeline's exit status; in zq3-r8 to zq3-r12 the wrapper ran the Phase 8 line with
`set -o pipefail;` prefixed and recorded `${PIPESTATUS[*]}` as evidence.)

Run:

```bash
# ==== BEGIN Phase-8 block 2 (run each line on its own) ====
python3 -I -B $S/tools/lab/retained9p/bootstrap.py run --plan $P \
  --plan-sha256 331136994f310b46c4d8a7d29a5840255778074b19b659969835b69226ff2c20 --output $D/bootstrap-run \
  | python3 -I -B $D/bin/sign_watch.py | tee $E/p8-bootstrap.jsonl
# ==== END Phase-8 block 2 ====
```

The plan is re-derived from zq3-r18's (the template that ran through
READY, `usable` and T1's dispatch). It pins:
- `runtime-guard.py` `fece96b3…` (unchanged);
- the NEW wf4 daemon `bb4ad73c…` (`runtime.sha256`, also the guard's
  `--runtime-sha256` below; Phase 4 step 1a qualifies it; the `usable` gate's
  `host-observe` runs it in client mode);
- the r19 kernel plan `792eadae…` (the new vf4 kernel `e3d474df…`) and
  `rocky_kernel.py` `220fc897…`;
- the b84 `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`, unchanged
  since zq3-r9, with the `guest-stat` and `host-observe` actions of the
  `usable` step);
- `hfs-runtime-prep` `6220ecae…` (rebuilt at `f2f6c44`). It fills the
  enrollment and bind requests with the r19 bundle's guest budget, which is
  unchanged (reservations 1024, received 256, 512/512 MiB).

The driver itself is `bootstrap.py` `2c9982b4…` (journal `/3`, unchanged since zq3-r9).
The driver launches, under the outer guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r19/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r19/guard --runtime-sha256 bb4ad73ca4cac3906065933dfb39ff98f0e75376618f7c200168679101efa579 --vm-name hfs-zq3-r19 --disk-root /var/tmp/hfs-validation-xz3/zq3-r19 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r19 --skip-path /var/tmp/hfs-validation-xz3/zq3-r19/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r19/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r19/runtime/runtime-config.json --disposable-validation
```

The guard runs with its defaults, which are the plan ceilings:
- 3600 s;
- runtime RSS 4 GiB + 256 MiB, bhyve RSS 6 GiB + 512 MiB;
- disk 4 GiB, 8 GiB free disk, no swap, 512 MiB available memory;
- one SIGTERM on breach.

Unlike the native guard, `runtime-guard.py` drops its own leading `--`. It
excludes its own lineage from `foreign-bhyve` (p54, zq3-r2 R2-2) and accepts
exactly `RUNTIME --config CONFIG --disposable-validation` (q54, R2-3).

The two signer commands the watcher accepts (the driver prints them as
`"operator_action": "sign-enroll"` and `"sign-bind"`):

```
/home/paul/tmp/claude/hfs-zq3-r19-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r19-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/signer-binding
```

Expected order:
1. host-preflight, launch, proposal;
2. guest-ready (test kernel; GRUB consumed `next_entry`);
3. admission (manifest SHA256, boot ID, provider nonce, `virtioN` names);
4. enroll-request, then **sign-enroll** (`"watcher": "signed"`, exit 0);
5. provision, enrollment-handoff, awaiting-bind, bind-request;
6. **sign-bind** (signed, exit 0), then bind;
7. mount-0, mount-1, READY;
8. **usable** (k64 + b84).

`usable` ends at most about 100 s after mount-1 (its deadline: 40 + 30 + 30 s;
a healthy gate ends after about 45-55 s; zq3-r10's, zq3-r11's and
zq3-r13's gates passed all five phases with L = 2; zq3-r12 never reached
it). Its phases (journal `/3`):
1. `first`: an `lstat` of both roots right away (a root already refused
   fails fast);
2. `settle`: wait until 40 s after mount-1 ended (the guest's 30 s QUERY
   window plus 10 s), then check that READY is still the runtime's last
   record;
3. `baseline`: OBSERVE until neither provider ledger holds an open claim;
4. `final`: a second `lstat` of both roots;
5. `claims`: OBSERVE every 5 s, for at most 30 s after the final probe, until
   each provider shows a TERMINAL GETATTR and a TERMINAL PROCESS_EXIT
   numbered after the baseline and nothing open.

Each OBSERVE runs the pinned runtime once on f3 in client mode
(`host-observe`), takes a fresh guest SSH observation and consumes one
lifecycle sequence. `summary.json` records the last one as
`observe_sequence_last` (L, at least 2), on success and on failure; Phase 9
starts at L+1. The `lstat`s write nothing into the mounts, but each adds
GETATTR and PROCESS_EXIT claims to the provider ledgers before T1.
`summary.json` says `ready` only if `usable` passed too.

What zq3-r18 got and what r19 changes:
- zq3-r18 reached READY 38.3 s after launch with both mounts (mountinfo
  391 and 410, the exact g64 tuple), both signatures by the watcher and the
  `usable` gate (L = 2).
- r19 changes the guest kernel (vf4: `hfs_setattr` accepts the
  `ATTR_KILL_*` bits; dentry ops set once; link and create-`EEXIST`
  `d_drop` before the re-lookup; 2f4's re-query, re4's back-off, ie4's
  re-acquire and the GETATTR/LOOKUP re-issue stay) and the daemon (wf4: an
  expected-target recall is DOMAIN-scoped when no peer holds a lease or an
  open reservation; je4, ce4 and e84 stay). The `usable` gate's recalls
  may now be DOMAIN-scoped more often; a guest `WARNING`/`BUG` line there
  is a finding to record.
  The exporter has zq3-r18's bytes (pc4); the oc4
  behaviour stays in the new kernel: an exporter
  ERROR now reaches the guest as its own errno (ERROR 5 `-EHOSTDOWN`,
  ERROR 4 `-ESTALE`, others `-EPROTO`), block and worker-stop lines print
  `exporter ERROR <n>`, retries and polls stop once a session is blocked,
  every hfs 9p reply wait ends after 30 s (`ETIMEDOUT`), and the exporter
  answers ERROR 5 UNAVAILABLE when its host transport or endpoint check
  fails instead of sending no reply.
- **The exporter `2fd7e32f…` ran cleanly in zq3-r14 to zq3-r18** (launch, admission,
  both mounts, the guard stop). A launch or admission refusal, an exporter
  exit, or any line in `guard/exporter.stderr` is still a finding against
  pc4 (collect it with the failure evidence below).
- **R12-1 check (kept):** a guest `blocked by uncertain control result`
  line, a worker stop, or a `guestbridge: transport failed at` line in
  `runtime.stderr` is a transport-fence finding; collect the fence record
  (below). With oc4 the guest line names the exporter's reason: `(-112),
  exporter ERROR 5` means the exporter saw the host transport down (the
  fence), `(-71), exporter ERROR -` a guest-side protocol error.
- oc4 side effect to watch: a mount in flight on a blocked session now
  stops at once (`-EHOSTDOWN`) instead of retrying for about 30 s, so a
  failed mount should fail fast rather than end `uncertain` at
  guest-mount's 30 s bound.
- 1a4 side effect to watch here: a probe that meets a view stuck in a recall
  fails with `ETIMEDOUT` after 30 s instead of hanging. In the `usable`
  gate that fails the gate (a finding, never retried); a `view control
  worker stopped` line in the guest log names the session, phase, host
  status and (since oc4) the exporter's ERROR reason.
- x84 side effect, as before: a guest request that meets a pending
  allocation of its session waits (FIFO, at most about 30 s) for the fence
  instead of failing at once; the gate's own deadline still bounds it.
- o84 side effect still to watch: each parked claim re-runs `Admit`
  (`c.mu.TryLock`) on every guest QUERY, about 100 Hz. A claim that parks
  for long can therefore contend with other admissions; it shows as slow
  OBSERVEs (the 984 timing, Phase 9 on) or as a claim still open at the end
  of the `claims` window. Record either (zq3-r10, zq3-r11, zq3-r13 to zq3-r18: none;
  zq3-r12: not reached).

**Expected:**
- both mounts complete, and each mount step's `mountinfo` shows
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`;
- READY arrives well inside the 120 s bootstrap deadline;
- `usable` is `ok`: both roots answer both probes, the `baseline` OBSERVE
  shows no open claim, and the `claims` phase sees a TERMINAL GETATTR and
  PROCESS_EXIT on each provider after the baseline, and the `final` probe
  returns on both roots (zq3-r10, zq3-r11 and zq3-r13 to zq3-r18: passed, L = 2; zq3-r12: not reached). The summary says
  `ready` with every step `ok` and an integer `observe_sequence_last`;
- the guest logs no `hfs … blocked` line and no `QUERY timeout` during the
  gate (zq3-r9's R9-1 lines were `blocked by QUERY timeout on GETATTR
  (-110)`; zq3-r10, zq3-r11 and zq3-r13 to zq3-r18 logged none here), no `blocked by
  uncertain control result` line (R12-1), no `view control worker stopped`
  line and no `ETIMEDOUT`; `runtime.stderr` stays empty (no fence line). A request that meets a pending
  allocation waits for the fence (x84, at most about 30 s) and one that
  meets a view stuck in a recall fails `ETIMEDOUT` after 30 s (1a4), so a
  slower or failed gate is possible: the deadline above still bounds it,
  and a gate near its deadline is a finding to record;
- `guard/exporter.stderr` stays empty and the exporter keeps running (as in
  zq3-r14 to zq3-r18).

The driver never reads the key.

Failure, a watcher refusal or `uncertain`: nothing is replayed. Inspect
`$D/bootstrap-run/journal.jsonl` and `$E/p8-bootstrap.jsonl`, and collect
the failure evidence read-only, as in zq3-r3:
- `p8-fail-guest.txt`: `uname -r`, the boot ID, `dmesg | grep -E
  "9p|virtio|hfs"` and the virtio driver list;
- `p8-guest-dmesg.txt`: the full guest `dmesg`, if anything oopsed;
- `p8-fail-host.txt` and `p8-fail-exporter.txt`;
- the transport fence record (nc4, block below): `p8-runtime-stderr.txt`
  and `p8-fence.json`.

Then go to Phase 11. **A mount failure is never retried in the same guest
boot** (rules above). The runtime then either stays in
`AWAITING_GUEST_BIND` until it exits at its 120 s bootstrap deadline, or
reaches READY on the mounts that did complete and keeps serving; then
Phase 11 goes through its guard-SIGTERM branch. A failed mount should return
an error instead of oopsing: an `ENOSPC` in the guest log would point at
the guest budget, an oops at a new kernel defect. If no guest kernel ran
(no GRUB output), `next_entry` is unconsumed: U3 after the child is
positively gone.

**`usable` failed** (summary `failed`, `stopped_at: usable`, with a
`usable_note`). The runtime reached READY, but the gate failed. The note says
which kind of failure it was:
- **a mount root failed an `lstat`** (the k64 note: "a mount root failed the
  usable gate"): the journal's `usable` result names the probe (`first` or
  `final`), each root's outcome and the errno;
- **a claim did not reach TERMINAL** (the b84 `USABLE_CLAIMS_FAILED` note: "a
  provider claim did not reach TERMINAL in time"): the result's `claims` names
  each open claim per mount (number, opcode, status, stage); probe `baseline`
  or `claims`. This is the R8-1 pattern: a stranded GETATTR or PROCESS_EXIT
  blocks its session once the guest's 30 s QUERY window ends;
- **OBSERVE could not be read** (the `USABLE_OBSERVE_FAILED` note: "the usable
  gate could not read the provider claims"): refused, incomplete (a failed
  refresh fences the Admission), not SERVING, without the b84 claim summary,
  or never answered; the journal's `usable` error names the cause.

In every case the runtime, both mounts and any stuck `lstat` child were left
as they are. Nothing is retried or remounted, no further OBSERVE is sent
by hand, and the batch fill is not attempted: `fill_plan.py` refuses such a
run. Record `observe_sequence_last` from `summary.json` (any later OBSERVE,
for example a Phase 11 inspection Paul approves, needs a larger sequence).
Collect read-only, while the runtime runs:
- the live owner snapshot (below), the inspection source for the claim
  ledgers, as in zq3-r8 to zq3-r12 (take one before and one after any guest
  block line, as zq3-r9 to zq3-r12 did: `p8-owner-live.tar`,
  `p8-owner-after.tar`);
- the guest `dmesg | grep -E "9p|hfs|SELinux"`, where m64 logs each fault
  block as an `hfs … blocked` line;
- the mountinfo lines.

Then go to Phase 11 through its guard-SIGTERM branch (one SIGTERM to the
guard; never a retry). A claims failure is a finding against 784, 884 and
d84 (R8-1 again). A `final` probe that hangs with an open GETATTR claim
`RESERVED/UNPREPARED` and guest `QUERY timeout` block lines is R9-1 again, a
finding against o84 and p84. An lstat failure with no open claim is a new
finding.

**If a mount fails or blocks** (guest-mount's 30 s bound gives `uncertain`,
or a mount step fails while the runtime still runs), or `usable` fails, take
the live owner snapshot from a second shell (`source
/home/paul/tmp/claude/hfs-zq3-r19-fill/session.sh`), as zq3-r4 to zq3-r12
did, read-only and while the runtime runs. A mount-1 `EKEYREJECTED` or a
guest `view control worker stopped, error -110` would be a regression of
d64: record the guest's mount process state (`ps -o
pid,stat,wchan:32,cmd -C mount`) with the read-only failure evidence above.
Then the nc4 fence record, each on its own line:

```bash
# ==== BEGIN Phase-8 block 3 (run each line on its own) ====
f3 "doas -n tar -c -f - --exclude control.sock -C $R runtime/owner" > $E/p8-owner-live.tar
f3 "doas -n cat $R/guard/runtime.stderr" > $E/p8-runtime-stderr.txt   # nc4: a fence logs `guestbridge: transport failed at …` here
python3 -I -B $D/sh/p11-fence.py $E/p8-runtime-stderr.txt $E/p8-owner-live.tar $D/bootstrap-run/journal.jsonl $(ls $D/bootstrap-run/calls/*.json) | tee $E/p8-fence.json
# ==== END Phase-8 block 3 ====
```

`p8-fence.json` `"fenced": true` names the fence: `Site` (the code location
that fenced the transport; for a failed protection, the step that called
it, such as `commitWrite`), `Cells`, `Class`, `Error` and `Time`, from
`runtime.stderr`, the `failure` row in `runtime/owner/transport/transport.db`
and any OBSERVE the `usable` gate took after it (`transport.failure` in its
`host-observe` call). The three should agree; write them into the run log.
`"fenced": false` with a guest `-71`/`uncertain control result` block line
would mean nc4 missed a fence path: a finding. Either way nothing is
retried; go to Phase 11 (guard-SIGTERM branch while the runtime serves).

**After READY and `usable` (B, at once, with no pause; the guard's 3600 s
bound runs from the launch).** zq3-r13 paused about 55 minutes here and
lost the whole matrix; zq3-r14 started Phase 9 about 80 s after READY and zq3-r15 about 55 s: run
these lines and Phase 9 straight on, so that Phase 9 starts within 5
minutes of READY. Fill the controller batch plan (F6/F7) from the run's
recorded evidence, check it independently, and fill the READY binding and
the T6/T7/T8 guest requests. The filler takes:
- the boot ID, provider nonce and manifest SHA256 from the journal's
  `guest-ready` and `admission` results, cross-checked with
  `work/enroll-request.json`;
- the m0/m1 mount IDs from the first field of each mount step's
  `mountinfo`.

It refuses unless the run is READY with every step `ok` (including
`usable`), every value agrees and the plan passes the controller's own
validation. It never overwrites an existing plan. The template's T5
deferral passes through unchanged. One line each:

```bash
# ==== BEGIN Phase-8 block 4 (run each line on its own) ====
python3 -I -B $S/tools/lab/retained9p/fill_plan.py batch --template $D/controller/batch-plan.TEMPLATE.json \
  --template-sha256 c92a58f698f639e38a3f33eb5cb5aaa26a037cf4a61bee11aea6f3ea780e5fb3 \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 331136994f310b46c4d8a7d29a5840255778074b19b659969835b69226ff2c20 \
  --output $D/controller/batch-plan.json | tee $E/p8-batch-fill.json
B=$(jf $E/p8-batch-fill.json sha256); echo "$B"
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.json | tee $E/p8-batch-check.json   # plan_sha256 must equal $B
python3 -I -B $D/sh/p9-ready.py $D/bootstrap-run | tee $E/p9-ready.json
U=$(python3 -I -B $D/sh/p9-ready.py --runtime $D/bootstrap-run); echo "$U"
L=$(python3 -I -B $D/sh/p9-ready.py --observe-last $D/bootstrap-run); echo "$L"   # observe_sequence_last: Phase 9 uses L+1
python3 -I -B $D/sh/p10-requests.py $D/controller/batch-plan.json $B $PM $E | tee $E/p8-requests.json
# ==== END Phase-8 block 4 ====
```

Both `p8-batch-fill.json` and `p8-batch-check.json` must show
`"cases_planned": ["credentials", "sync", "metadata", "namespace"]` and
`deferred` with exactly the `mapped-pressure` entry and its q64/f84 re-plan
reason.
Anything else is a refusal-level mismatch: stop, runtime serving.

`p9-ready.py` checks:
- `summary.json` (`ready`, and an integer `observe_sequence_last` of at
  least 2);
- the journal header `homelabfs-zq3-bootstrap-journal/3`;
- the journal's one `ok` `usable` result, whose own `observe_sequence_last`
  equals the summary's;
- the journal's READY record (`CONTROLLED_LOCAL`, two domains, publication
  null, control socket `$R/runtime/owner/control.sock`).

It prints the runtime UUID, cut, epoch, `observe_sequence_last` (L) and
`phase9_sequence` (L+1). Persist `U` and `L` in `vars.env`. `p10-requests.py` writes:
- `$E/lock-m0.json` (T6);
- `$E/lock-m1-t8.json` (T8);
- unless `PM=skip`, `$E/pod-request.json` (T7).

The mount IDs come from the checked batch plan. If any of these lines
refuses, nothing was written and the runtime keeps serving under the
guard's bounds. A wrong argument (path, pin) may be corrected and the line
rerun. A mismatch in the evidence itself is a run finding: never hand-edit
a plan or request around it; go to Phase 11 (inspection first, runtime
still serving).

## Phase 9: READY admission, mount tuple and the SELinux gate (B, 1 min)

**In-run cutoff:** after 22:15 UTC on the run day do not start Phase 9; go
to Phase 11 (top of Phase 8).

**Guard budget (zq3-r13):** Phase 9 starts within 5 minutes of READY, and
its first line is the budget check (Phase 10 explains `gbudget` and its
table). `"decision": "GO"` and `rc=0` are required. `"late_after_ready":
true` (more than 5 minutes after READY) is an operator finding to record;
the budget still decides. Write `launch_utc` and `guard_end_utc` into the
run log. `STOP` (`rc=1`): Phase 9 and T1-T11 unrun (guard budget), Phase 11
through its guard-SIGTERM branch at once.

```bash
# ==== BEGIN Phase-9 block 1 (run each line on its own) ====
gbudget phase9 | tee $E/p9-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: Phase 9 and T1-T11 unrun, Phase 11 (guard-SIGTERM branch)
C="$R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock"
tf3 ssh-noop 0 'doas -n true'      # 984 baseline 1 of 3: SSH + doas wall time (each on its own line)
tf3 ssh-noop 0 'doas -n true'      # 2 of 3
tf3 ssh-noop 0 'doas -n true'      # 3 of 3
tf3 observe $((L+1)) "doas -n $C --operation observe --runtime $U --sequence $((L+1))" > $E/observe-p9.json
cat $E/obs-timing.tsv              # 984 capture check: three ssh-noop lines and one observe line (sequence L+1), each ending in rc 0
python3 -I -B $D/sh/p9-observe.py $E/observe-p9.json $E/p9-ready.json $D/controller/batch-plan.json $B | tee $E/p9-observe-check.json
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo" | tee $E/p9-mountinfo.txt
rocky 'getenforce; cat /sys/fs/selinux/enforce; uname -r; dmesg | grep -E "LSM: initializing|SELinux: +Initializing"; dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/p9-guest.txt
# ==== END Phase-9 block 1 ====
```

The three `ssh-noop` lines and the OBSERVE line each make exactly one f3
call; `tf3` only adds a line to `$E/obs-timing.tsv` (984 measurement, below).
The `cat` line is local: if the file lacks any of the four lines, record it
(the timing summary would miss them), but it never stops the run.

`p9-observe.py` applies the committed correlator's own checks to this first
post-bootstrap OBSERVE:
- `OBSERVED`, sequence **L+1** (`observe_sequence_last` + 1 from
  `p9-ready.json`), evidence `homelabfs-controlled-evidence/2`, `complete`,
  stage `SERVING` and publication null;
- no RESTART or lost-reply section;
- runtime, cut and epoch equal to READY's;
- exactly one unfenced admission with the batch plan's boot ID, provider
  nonce and both sessions;
- a SERVING service identity with an applied position;
- provider ledgers for both sessions, the view controller ledger and the
  transport high-water.

It also prints each provider's claim counts (`Allocated`, `Retired`,
`Open`) without gating on them. An `Open` count above 0 right after the
`usable` gate's `claims` phase is a finding to record in the run log.

A refused OBSERVE consumes nothing (the runtime refuses a sequence that is
not above the last accepted one): if it says the sequence is stale, check
`L` against `summary.json` and rerun the line once with the right value,
writing to `$E/observe-p9-2.json` (not over `observe-p9.json`) and recording
that `obs-timing.tsv` then has one extra row. A
lost reply may have consumed `L+1`: then the T1 baseline below uses `L+3`
instead of `L+2` (record it), never `L+1` again.

Expect `"pass": true`. The guest must show:
- **exactly two 9p mountinfo lines** (count `2`): `hfs0` on `$G/m0` and
  `hfs1` on `$G/m1`, each with exactly
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4` (g64;
  `guest.py` `check_tuple` and `pod_fixture.py` require this tuple);
- **`getenforce` printing `Enforcing`** and `/sys/fs/selinux/enforce` `1`.
  `guest.py` admits every case and lifecycle step only with enforce = 1, and
  `pod_fixture.py` needs SELinux for T7;
- `6.1.190-hfs-xz3`;
- an `LSM: initializing lsm=…selinux…` line and a `SELinux:  Initializing.`
  line from the kernel;
- `no-hfs-block`. m64 logs every guest fault block, and a clean session logs
  nothing. The same grep catches 1a4's `view control worker stopped` and
  `initial attributes timed out behind a recall` lines.

**Any miss stops before T1:**
- a mount count other than 2, or a different option tuple: a finding
  against g64; T1-T8 would refuse at admission, as in zq3-r7;
- `Permissive`, `Disabled` or a missing `/sys/fs/selinux`;
- an `hfs … blocked` line: a session the guest already blocked, as in R7-2;
- a failed OBSERVE check.

Record it as a finding and go to Phase 11 through its guard-SIGTERM branch,
with the runtime still serving. Nothing is relabelled, set, remounted or
retried around it. A missing `LSM`/`SELinux:` dmesg line with `Enforcing`
and enforce `1` is recorded, not a stop: the ring buffer may have wrapped.

**984 measurement (B, read-only, evidence only).** The offline 984 workload
model says the lab passes only if one fresh guest SSH observation costs less
than about 2.4 s. zq3-r10 gave the first samples on f3 (SSH no-op median
0.198 s; OBSERVE net 0.605 s and 0.434 s), zq3-r11 two more (0.466 s and
0.483 s; zq3-r12 stopped before Phase 9), zq3-r13 one (0.651 s; SSH
no-op median 0.21 s) and zq3-r14 two (median 0.493 s, max 0.521 s; zq3-r15: median 0.483 s, max 0.491 s; SSH
no-op median 0.329 s); this run adds more, and the line
after the Phase 9 OBSERVE checks that they were captured. Each OBSERVE takes a
fresh guest observation (the runtime refreshes the guest Admission for it),
so its wall time is an upper bound for that cost. `tf3` records the wall
time of the Phase 9 OBSERVE, the T1 baseline and every `obs` OBSERVE in
Phase 10 in `$E/obs-timing.tsv`, next to the three `ssh-noop` lines above
(the SSH + doas overhead each line also pays). It is summarised twice: the
T4 block ends with `p9-timing.py … | tee $E/p10-timing-after-t4.json`, and
Phase 11 starts with the final summary, whichever step the matrix stopped
at:

```text
# ==== BEGIN Phase-9 block 2 (text: not a command, never run) ====
python3 -I -B $D/sh/p9-timing.py $E/obs-timing.tsv | tee $E/p10-timing-final.json   # runs in Phase 11, shown here for reference
# ==== END Phase-9 block 2 ====
```

`p9-timing.py` prints the ssh-noop median, each OBSERVE's wall and net
seconds (wall minus the ssh-noop median), their median and maximum, and
`net_max_below_984_bound` against 2.4 s. Write the median and the maximum
into the run log (see the [run log template](#run-log-template)). The number
never stops the run, but a net time near or above 2.4 s is a finding for
984. The `usable` gate's own OBSERVEs are not timed individually (its
journal records only the step); `summary.json` gives their count
(`observe_sequence_last`).

## Phase 10: gate matrix (B inside the window, about 20-30 min)

**In-run cutoff:** no new case or lifecycle step after 22:15 UTC on
the run day; the remaining steps are recorded unrun and Phase 11 starts at
once, with its step 5 poweroff before 22:45 UTC (top of Phase 8).

Order **T1-T4, T6, T7, T9, T10, T11, T8**; T5 is **unrun (deferred, re-plan pending)**.
Stop dependent steps on the first failure (dependencies are named per step). Results are pass, fail, unsupported, unrun
or uncertain; none of the correlator records is acceptance. Two clocks run:
the guard's 3600 s from the Phase 8 launch (the guard budget below), and
the controller's 3600 s from `probe` (later commands refuse and preserve).
Set once:

```bash
# ==== BEGIN Phase-10 block 1 (run each line on its own) ====
CTL=$S/tools/lab/check-retained9p.sh
CR=$S/tools/lab/retained9p/correlate.py
OUT=$D/ctrl-run
# ==== END Phase-10 block 1 ====
```

**Guard budget (zq3-r13 lesson; B, local).** The guard stops the runtime
3600 s after the Phase 8 launch, whatever the operator does. Before each
step below that needs the live runtime, its first line is `gbudget STEP`
(`session.sh` over `sh/guard-budget.py`; it takes the guard start from the
journal's `launch` intent, a little before the real start, so it errs on the
safe side). Each step starts only if `"decision": "GO"` (`rc=0`): now is no
later than launch + 3600 s − (the step's worst-case seconds + a 300 s
Phase 11 reserve). **be4:** the step seconds are the run plan's be4 worst
cases (each controller dispatch up to 255 s, each lifecycle step 195 s,
T7's run 315 s), not zq3-r14's 300 s per case. The reserve keeps time for what still needs the serving runtime
after a step: the case-failure capture block (live owner snapshot,
`runtime.stderr`, fence record) and Phase 11's `pgrep -U root` and one
SIGTERM. `gbudget` alone prints the remaining guard seconds at any time.

| Step | Worst-case s (be4) | Latest start (launch +) | zq3-r14 budget |
|---|---|---|---|
| Phase 9 | 180 (no guest.py dispatch) | 3120 s (52:00) | 180 |
| T1, T2, T3, T4 (each: probe 255 + release 255 + two OBSERVEs 60 + slack 90) | 660 | 2640 s (44:00) | 300 |
| T6 (lock probe overall 240 + SSH and operator 60) | 300 | 3000 s (50:00) | 180 |
| T7 (`pod_fixture.py run`, `RUN_SECONDS` 315, plus setup) | 615 | 2685 s (44:45) | 420 |
| T9 (with T10, so the pair stays together: three steps of 195 + ARM, OBSERVEs, RESTART, slack 405) | 990 | 2310 s (38:30) | 540 |
| T10 (one step 195 + OBSERVE, RESTART, slack 195) | 390 | 2910 s (48:30) | 240 |
| T8 while the runtime still serves (T10 not FENCED; `rockyt` 270 + one dispatch 255 + 15) | 540 | 2760 s (46:00) | 360 |
| T11, and T8 after a FENCED T10 | none: the runtime and guard have already ended | in-run cutoff only |

The launch time is in the journal (`gbudget phase9` prints it as
`launch_utc`, with `guard_end_utc` and each step's `latest_start_utc`);
write the launch time and the guard end into the run log at Phase 9.
**The worst cases do not all fit** (Phase 9 180 + T1-T4 2640 + T6 300 + T7
615 + T9 with T10 990 + reserve 300 = 5025 s > 3600 s); the be4 review
kept the 3600 s guard and accepts early stops on slow runs. A healthy matrix
fits easily (zq3-r14's T1 admission took about 17 s, zq3-r15's ran 23 s, zq3-r16's 16 s, zq3-r17's 31 s to its failure and zq3-r18's 7.3 s; each case is
expected well under a minute of guest time): Phase 9 at about launch +
2 min, T1-T4 by about + 20 min, T6, T7, T9 and T10 by about + 35 min, with
about 1000 s to spare. **`STOP` (`rc=1`): do not start the step.** Record it and every
remaining step as unrun (guard budget), then go to Phase 11 through its
guard-SIGTERM branch while the runtime still serves (a T9 that would start
after 38:30 is unrun with T10 and T11; T8 then also runs only if its own
budget line says GO). A `gbudget` refusal (`rc=2`: no `/3` journal or not
exactly one `launch` intent) means a wrong path or a broken run: record
it, and judge the time from the journal's `launch` line by hand.

**Lifecycle sequences (shifted by L).** Every OBSERVE, ARM and RESTART needs
a sequence strictly above the last accepted one. The b84 `usable` gate used
1 to L (`observe_sequence_last`), Phase 9 used L+1 and the T1 baseline uses
L+2 (both computed from `L`: `correlate.py command` needs the controller run,
which `probe` creates). Every later command comes from `correlate.py
command`, whose sequence is the latest reply's plus one: through `obs` for
OBSERVE, directly for ARM and RESTART. Expected sequences if nothing is lost
(four cases, T5 deferred):

| Step | Sequence | zq3-r8 sheet (L = 0) |
|---|---|---|
| `usable` gate OBSERVEs | 1 … L | none |
| Phase 9 OBSERVE | L+1 | 1 |
| T1-T4 baseline/retained | (L+2, L+3), (L+4, L+5), (L+6, L+7), (L+8, L+9) | 2/3 … 8/9 |
| T9 pre-arm, ARM, post-CALL | L+10, L+11, L+12 | 10, 11, 12 |
| T10 post-quiesce, RESTART | L+13, L+14 | 13, 14 |

`obs` prints each reply's sequence; compare it with the table and record a
difference. A refused or lost command is never resent with the same
sequence (rules above).

**T5 `mapped-pressure`: unrun (deferred).** The batch plan declares it in
`deferred` with its reason, so `next` after T4's release refuses ("a
remaining case") and no T5 line exists. q64/f84 fixed the view-lifetime wall
that first deferred it (retired view tickets recycled, ACK archive and recall
UUIDs pruned), but T5 is not yet re-planned or sized for the full 4 MiB
canary. Record it in the run log and the result as "T5 unrun (deferred,
re-plan pending)", never as passed. zq3 acceptance stays incomplete.

**Per case (T1-T4).** Baseline OBSERVE right before the dispatch, the case
(`probe` for the first, `next` afterwards), the retained OBSERVE while its
payload is in the guest, `correlate.py case`, then `release`. The correlator
refuses unless both OBSERVEs are complete and SERVING on the same runtime,
cut and epoch, the applied position advanced, and (from the second case on)
the baseline follows the previous case's release in the same incarnation.
Expect `probe`/`next` to end with the case `payload-retained-awaiting-correlation`
and `release` with it `released` (each prints the controller state, which
repeats `cases_planned` and `deferred`). A correlator refusal leaves the
payload retained: stop (T1-T4 incomplete), Phase 11 with the runtime
serving.

**R11-1 check (y94; z94 merged since zq3-r15; 0a4 still open).** zq3-r11's T1 failed in
admission: the m0 root OPEN parked for 43.6 s behind an all-domain recall
that its own observation lease started (z94), the host answered m0's early
DRAINED STALE, the guest stopped its view worker (`error -116`) and blocked
m0 (`blocked by QUERY timeout on OPEN (-110)`), and the probe ended
`dispatching-uncertain` after 44.7 s. With y94 the early DRAINED is answered
BLOCKED and accepted once m1's INSTALLED_DRAIN arrives, so the recall
completes and each case should be admitted and pass. z94 removed the
recalls a read-only OPEN started, but lease releases still start them (je4),
so expect recalls during admission: they show as a gap at
`open-root` → `stat-root` in `p10-admission.py` (evidence, not a stop).
Each case's `ctrl-run/$CASE/stdout.log` carries the y84 `admission-step`
events; `p10-admission.py` summarises them after T4 (below) and flags
`deadline_hit` and `etimedout` (errno 110 in the failure, in any `reply` event
or in any `errno`/`*_errno` observation field). **If a case fails or is
uncertain**, before
Phase 11 run, each on its own line (the second shows whether a block line, a
`view control worker stopped` line or an `initial attributes timed out
behind a recall` line appeared):

```bash
# ==== BEGIN Phase-10 block 2 (run each line on its own) ====
CASE=credentials; printf '%s=%q\n' CASE "$CASE" >> $D/vars.env   # the failing case's name: credentials (T1), sync (T2), metadata (T3) or namespace (T4)
python3 -I -B $D/sh/p10-admission.py $OUT/$CASE/stdout.log | tee $E/p10-admission-$CASE.json
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/t-fail-guest-block.txt
f3 "doas -n tar -c -f - --exclude control.sock -C $R runtime/owner" > $E/$CASE-owner-live.tar   # live owner snapshot first (holds transport.db)
f3 "doas -n cat $R/guard/runtime.stderr" > $E/t-fail-runtime-stderr.txt
N=$((L+3)); echo "$N"; printf '%s=%q\n' N "$N" >> $D/vars.env   # review round 1: the failure OBSERVE sequence, one above the last accepted one. EDIT the expression before running when the failure is not "T1 failed after its L+2 baseline": $((L+4)) if T1's retained OBSERVE (L+3) was also taken; for T2-T4 one above the last accepted sequence in the Lifecycle-sequences table (e.g. T2 failing after its baseline L+4: $((L+5)))
tf3 observe $N "doas -n $C --operation observe --runtime $U --sequence $N" > $E/obs-$CASE-fail.json   # yf4 direct form, never the obs helper (correlate.py refuses after a failed case). A refused sequence is not consumed (set N one higher and retry once); a failing OBSERVE (runtime gone) is recorded, never retried
python3 -I -B $D/sh/p11-fence.py $E/t-fail-runtime-stderr.txt $E/$CASE-owner-live.tar $(ls $E/observe-p9*.json $E/obs-*.json) | tee $E/t-fail-fence.json   # nc4 fence record: "fenced" false expected
python3 -I -B $D/sh/recalls.py $E/recalls-t1.json $(ls -tr $E/observe-p9.json $E/obs-credentials-*.json 2>/dev/null) | tee $E/recalls-t1.txt   # ce4: recall timing after T1 (pass or fail); findings, never a gate   # only when CASE=credentials (T1); other cases: Phase 11 step 1b covers them
# ==== END Phase-10 block 2 ====
```

(the block takes the live owner snapshot `$E/$CASE-owner-live.tar` first and
feeds it to `p11-fence.py`, review round 1; its fresh OBSERVE is the yf4
direct form: zq3-r18 found that the `obs` helper refuses after a failed
case, so the `tf3 observe <next>` line fills `<next>` by its comment's rule
and its own run-log line records the value used), and take a second snapshot
after the block lines, as zq3-r10 and zq3-r11 did for T1 (`f3 "doas -n tar
-c -f - --exclude control.sock -C $R runtime/owner" >
$E/$CASE-owner-after.tar`). Record as findings, never retry:
- `deadline_hit` with `admission_ms` null: admission took over guest.py's
  150 s admission alarm (be4; one hfs syscall can take about 90-120 s; the
  Deadline ends the request only when the syscall returns);
- a long admission that still passed (`admission_ms` well above zq3-r14's
  roughly 17 s, or one gap of several seconds): a finding to record with
  the recall it lines up with (fence or domain-sequence steps in the
  owner snapshots/OBSERVEs); with z94 the root OPEN should start none, and
  with ie4 a recall during an observation should cost a re-acquire, not an
  `ESTALE`, with re4 and qe4 a host BLOCKED should cost a release and a
  retry after retirement progress, not an `EAGAIN`, and with 2f4 a grant
  that beats the guest's fence raise should cost a re-query, not an
  `EKEYREJECTED`; `recalls.py` gives each recall's trigger, scope and
  phase durations;
- the failing syscall: yf4's `guest.py` names it in the failure record
  (`p10-admission.py`'s `failure`, the case's `stdout.log`); write it into
  the run log with its errno (an `EOPNOTSUPP` is R18-1 again, a finding
  against vf4);
- `etimedout` true (`etimedout_at` names the event and field): a syscall
  waited 30 s for a view stuck in a recall (1a4) and failed `ETIMEDOUT`,
  whether the step failed on it or recorded it as an observation;
- a `view control worker stopped` line: the guest stopped a view worker;
  the line names the session, the worker step, the host event and ACK
  phase and the host status (an `ack DRAINED, host status STALE` pair would
  be R11-1 again, a finding against y94);
- a case that ends `dispatching-uncertain` or with a dispatch error: the
  controller's dispatch bound (request + 15 = 255 s) ended before the guest
  did (be4: safe and accepted, payload and evidence kept, never replayed);
- `t-fail-fence.json` `"fenced": true`: a host transport fence (R12-1
  check above); write its `Site`, `Cells`, `Class`, `Error` and `Time`
  into the run log.

A `DESCRIPTION_RELEASE … (-11)` block line again is a finding against x84.

**R12-1 check (nc4; with oc4 and pc4 the guest names the reason).** A case
(or any later step) that meets a host transport fence shows a guest
`blocked by …` line and a `guestbridge: transport failed at` line in
`runtime.stderr`. Since oc4 the guest line carries the exporter's reason:
`(-112), exporter ERROR 5` (UNAVAILABLE: the exporter found the host
transport down), where zq3-r12 saw `uncertain control result … (-71)`. The case-failure block above captures the
fence record (`t-fail-fence.json`): a fence is a finding against nc4 (its
`Site` names where); the runtime cannot recover from it (the transport
answers Unavailable from then on), so the matrix stops there and Phase 11
runs through its guard-SIGTERM branch.

T1, `credentials`:

```bash
# ==== BEGIN Phase-10 block 3 (run each line on its own) ====
gbudget T1 | tee $E/t1-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T1 and the rest unrun, Phase 11 (guard-SIGTERM branch)
tf3 observe $((L+2)) "doas -n $C --operation observe --runtime $U --sequence $((L+2))" > $E/obs-credentials-baseline.json
$CTL probe --plan $D/controller/batch-plan.json --sha256 $B --output $OUT | tee $E/t1-probe.json  # tool timeout >= 300 s
obs obs-credentials-baseline obs-credentials-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case credentials --baseline $E/obs-credentials-baseline.json --baseline-sha256 $(sha $E/obs-credentials-baseline.json) --retained $E/obs-credentials-retained.json --retained-sha256 $(sha $E/obs-credentials-retained.json) --record $E/corr-credentials.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-credentials.json --correlation-sha256 $(sha $E/corr-credentials.json) | tee $E/t1-release.json  # tool timeout >= 300 s
python3 -I -B $D/sh/recalls.py $E/recalls-t1.json $(ls -tr $E/observe-p9.json $E/obs-credentials-*.json 2>/dev/null) | tee $E/recalls-t1.txt   # ce4: recall timing, ONLY if T1 passed (after a failure the case-failure block runs this line, after its fresh OBSERVE); findings, never a gate
# ==== END Phase-10 block 3 ====
```

r19 (ce4): the `recalls.py` line runs once after T1 whether T1 passed or
failed (after a failure, as the last line of the case-failure block). It
reads the one of those OBSERVEs whose `controller.recalls` has the largest
`total` (after a failure that is the block's fresh `obs-$CASE-fail`) and
prints one line per recall (trigger kind and opcode, requesting domain,
scope `ALL`/`DOMAIN`, outcome, phase durations, ACK waits). Record the
recall count, scopes and the longest phase and ACK wait in the run log next
to T1's admission timing. `history_found: false` is a finding (pre-ce4 bytes
or no OBSERVE), never a stop.

T2, `sync`:

```bash
# ==== BEGIN Phase-10 block 4 (run each line on its own) ====
gbudget T2 | tee $E/t2-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T2 and the rest unrun, Phase 11 (guard-SIGTERM branch)
obs obs-credentials-retained obs-sync-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t2-next.json  # tool timeout >= 300 s
obs obs-sync-baseline obs-sync-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case sync --baseline $E/obs-sync-baseline.json --baseline-sha256 $(sha $E/obs-sync-baseline.json) --retained $E/obs-sync-retained.json --retained-sha256 $(sha $E/obs-sync-retained.json) --record $E/corr-sync.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-sync.json --correlation-sha256 $(sha $E/corr-sync.json) | tee $E/t2-release.json  # tool timeout >= 300 s
# ==== END Phase-10 block 4 ====
```

T3, `metadata`:

```bash
# ==== BEGIN Phase-10 block 5 (run each line on its own) ====
gbudget T3 | tee $E/t3-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T3 and the rest unrun, Phase 11 (guard-SIGTERM branch)
obs obs-sync-retained obs-metadata-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t3-next.json  # tool timeout >= 300 s
obs obs-metadata-baseline obs-metadata-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case metadata --baseline $E/obs-metadata-baseline.json --baseline-sha256 $(sha $E/obs-metadata-baseline.json) --retained $E/obs-metadata-retained.json --retained-sha256 $(sha $E/obs-metadata-retained.json) --record $E/corr-metadata.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-metadata.json --correlation-sha256 $(sha $E/corr-metadata.json) | tee $E/t3-release.json  # tool timeout >= 300 s
# ==== END Phase-10 block 5 ====
```

T4, `namespace`:

```bash
# ==== BEGIN Phase-10 block 6 (run each line on its own) ====
gbudget T4 | tee $E/t4-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T4 and the rest unrun, Phase 11 (guard-SIGTERM branch)
obs obs-metadata-retained obs-namespace-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t4-next.json  # tool timeout >= 300 s
obs obs-namespace-baseline obs-namespace-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case namespace --baseline $E/obs-namespace-baseline.json --baseline-sha256 $(sha $E/obs-namespace-baseline.json) --retained $E/obs-namespace-retained.json --retained-sha256 $(sha $E/obs-namespace-retained.json) --record $E/corr-namespace.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-namespace.json --correlation-sha256 $(sha $E/corr-namespace.json) | tee $E/t4-release.json  # tool timeout >= 300 s
$CTL status --output $OUT > $E/t4-status.json  # tool timeout >= 300 s
jf $E/t4-status.json syscall_batch_success      # must print True (every PLANNED case released)
jf $E/t4-status.json cases_planned              # ['credentials', 'sync', 'metadata', 'namespace']
jf $E/t4-status.json deferred                   # the mapped-pressure entry with its re-plan reason
python3 -I -B $D/sh/p9-timing.py $E/obs-timing.tsv | tee $E/p10-timing-after-t4.json   # 984 measurement, evidence only
python3 -I -B $D/sh/p10-admission.py $OUT/credentials/stdout.log $OUT/sync/stdout.log $OUT/metadata/stdout.log $OUT/namespace/stdout.log | tee $E/p10-admission-steps.jsonl   # y84 admission-step timings, evidence only
# ==== END Phase-10 block 6 ====
```

Write each case's `admission_ms` and its largest gap into the run log
(`p10-admission-steps.jsonl`: one line per case, `deadline_hit` and
`etimedout` false expected). A gap of several seconds at `open-root` →
`stat-root` is a root open parked behind recalls (z94 still starts them); it
is evidence, not a stop.

Mid-release failure (run plan): the guest consumes the retention record
before its first unlink; if an unlink or the deadline then fails, the
controller stays at `release-dispatching-uncertain` with `error`. Never
re-release or replay: root inspects the leaf against `released-CASE.json`,
keeps leftovers and evidence, T1-T4 incomplete, Phase 11.

T6, same-mount locks on m0 (needs T5 only for the order; with T5 deferred it follows T4's release):

```bash
# ==== BEGIN Phase-10 block 7 (run each line on its own) ====
gbudget T6 | tee $E/t6-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T6 and the rest unrun, Phase 11 (guard-SIGTERM branch)
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-tools/guest-lock-probe.py' < $E/lock-m0.json | tee $E/t6-lock.json  # tool timeout >= 300 s
# ==== END Phase-10 block 7 ====
```

Pass only with `"passed": true` (exit 0; exit 10 is a fail, recorded with its
`error`; a fail does not block T7). T6b (across m0/m1): recorded
**unsupported**, not run.

T7 (A8, skip if `PM=skip`): two workloads in the ordinary pod profile
bind-sharing m0. The request file must not exist on Rocky yet:

```bash
# ==== BEGIN Phase-10 block 8 (run each line on its own) ====
gbudget T7 | tee $E/t7-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T7 and the rest unrun, Phase 11 (guard-SIGTERM branch)
rocky 'test ! -e /root/hfs-zq3-zq3-r19-pod/request.json && (umask 077; cat > /root/hfs-zq3-zq3-r19-pod/request.json)' < $E/pod-request.json
rocky 'sha256sum /root/hfs-zq3-zq3-r19-pod/request.json'   # must equal the next line
sha $E/pod-request.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r19-pod/request.json' | tee $E/t7-pod.json  # tool timeout >= 420 s
# ==== END Phase-10 block 8 ====
```

Pass only with `t7_pass: true` (which also removes the two exited containers
and the pod payload normally); a failure or `uncertain` preserves both. An
SELinux denial of `container_t` on the 9p label is a T7 failure, never
relabelled or disabled. The request is `homelabfs-retained9p-pod/2` with the
e94 `runtime_nevra` pins (`p10-requests.py` only fills `PM` and m0's mount
ID). A refusal at `check_runtime` (`SHA256 differs from its pin` or
`package identity differs from its runtime_nevra pin`) means Rocky's
packages changed since the facts (N1): T7 unrun, never edit the request.
T7 has never run for real: zq3-r1 to zq3-r10 stopped before it, zq3-r11
skipped it, zq3-r12 stopped in Phase 8, zq3-r13's matrix went unrun and
zq3-r14 to zq3-r18 stopped at T1. **Tool timeout for the `pod_fixture.py run` line:
at least 420 s** (be4: `RUN_SECONDS` 315).

T9, v24 lost reply with the d44 MKDIR filter (the daemon `bb4ad73c…`
keeps it). It runs after the four released cases: the controller's lifecycle
steps need `syscall_batch_success`, which with the deferral means every
planned case released. Run the ARM and the CALL step back to back:

```bash
# ==== BEGIN Phase-10 block 9 (run each line on its own) ====
gbudget T9 | tee $E/t9-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T9, T10 and T11 unrun (T8 by its own budget line), Phase 11 (guard-SIGTERM branch)
obs obs-namespace-retained t9-pre-arm
python3 -I -B $CR command --operation arm-lost-reply --lost-intent MKDIR --output $OUT --sha256 $B --observe $E/t9-pre-arm.json --observe-sha256 $(sha $E/t9-pre-arm.json) --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock > $E/cmd-t9-arm.json
ARM=$(python3 -I -B $D/sh/p10-argv.py --operation arm-lost-reply $E/cmd-t9-arm.json); echo "$ARM"
f3 "doas -n $ARM" > $E/t9-armed.json
$CTL lifecycle --output $OUT --sha256 $B --step lost-reply-call | tee $E/t9-call.json  # tool timeout >= 300 s
obs t9-pre-arm t9-post-call t9-armed
$CTL lifecycle --output $OUT --sha256 $B --step lost-reply-reread | tee $E/t9-reread.json  # tool timeout >= 300 s
python3 -I -B $CR lost-reply --output $OUT --sha256 $B --before $E/t9-pre-arm.json --before-sha256 $(sha $E/t9-pre-arm.json) --armed $E/t9-armed.json --armed-sha256 $(sha $E/t9-armed.json) --after $E/t9-post-call.json --after-sha256 $(sha $E/t9-post-call.json) --record $E/t9.json
# ==== END Phase-10 block 9 ====
```

`t9-armed.json` must say `"status":"ARMED"` before the CALL step runs. A
`REFUSED` ARM took no effect but may have consumed its sequence: record T9
failed and T10/T11 unrun (their expected outcomes depend on T9), run T8, then
Phase 11 with the runtime serving. The record's `classification` is expected to be
`dropped-reply-effect-committed-and-visible` (`armed_intent` and
`dropped_opcode` MKDIR, the mkdir fails in m0, m1 sees the directory, m0
blocked with `EHOSTDOWN`; since oc4 its block line also carries `exporter
ERROR <n>`: record it); never a pass. A controller step that fails or is
uncertain records `error`, after which every controller command refuses:
then go to Phase 11 (runtime serving).

T10, daemon restart via u24 (last step that needs the live runtime):

```bash
# ==== BEGIN Phase-10 block 10 (run each line on its own) ====
gbudget T10 | tee $E/t10-budget.json; echo rc=${PIPESTATUS[0]}   # guard budget: GO and rc=0 required; STOP: T10 and T11 unrun, T8 by its own budget line, Phase 11 (guard-SIGTERM branch)
$CTL lifecycle --output $OUT --sha256 $B --step quiesce | tee $E/t10-quiesce.json  # tool timeout >= 300 s
obs t9-post-call t10-post-quiesce
python3 -I -B $CR command --operation restart --output $OUT --sha256 $B --observe $E/t10-post-quiesce.json --observe-sha256 $(sha $E/t10-post-quiesce.json) --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock > $E/cmd-t10-restart.json
RST=$(python3 -I -B $D/sh/p10-argv.py --operation restart $E/cmd-t10-restart.json); echo "$RST"
f3 "doas -n $RST" > $E/t10-restart.json
jf $E/t10-restart.json status    # RESTARTED, FENCED or EFFECT_UNREPORTABLE (then: jf $E/t10-restart.json outcome)
# ==== END Phase-10 block 10 ====
```

`correlate.py command --operation restart` refuses (writes
nothing, prints the counts) unless the last controller step was the quiesce
and the OBSERVE is later than every case-bound OBSERVE and shows
view-controller `Leases` and `Reservations` 0, no `Recall` and `Matches`
true: then T10 is **unrun**, the runtime keeps serving and T11 is unrun. The
same holds if `t10-restart.json` reports `REFUSED`. In both cases run T8
first, as the ARM-refused path does, then Phase 11 through its guard-SIGTERM
branch (execution notes). RESTARTED or FENCED are both recorded;
FENCED (fencing, then runtime exit) is Paul's accepted behaviour and the
expected outcome after T9 (the blocked guest never re-reads the dropped
response). RESTARTED leaves the runtime serving: T11 unrun. The exporter
`2fd7e32f…` carries 564 (and e64, p64, w64) and pc4, so a "busy" recovery
reply or a lost REFUSED in T10 is a new finding, not zq3-r7's known race.
(zq3-r8 to zq3-r18 never reached T10, so this exporter path is still
unexercised on f3.) After a FENCED T10 the runtime exits and the exporter
answers every later guest exchange with ERROR 5 UNAVAILABLE (pc4): guest
lines with `(-112), exporter ERROR 5` after the RESTART are the expected
consequence, not a new failure; record them.
**After a transport fence (nc4):** a RESTART rebuilds the guest transport
from its ledger, and the guestbridge restore refuses a ledger that carries
a `failure` row (`fsapi.Unavailable`, by design: a fenced cut is never
restored). If a fence happened before T10 (the matrix would normally have
stopped already), a refused restart or an `EFFECT_UNREPORTABLE` outcome is
the expected consequence: record it next to the fence record, never retry
or work around it.

T11, K04 host side (**only if** T10 is FENCED). Wait for the runtime to exit
and the guard to write its receipt (repeat the first line every 10 s, at most
30 times, until it prints the receipt):

```bash
# ==== BEGIN Phase-10 block 11 (run each line on its own) ====
f3 "doas -n test -e $R/guard/receipt.json && doas -n cat $R/guard/receipt.json || echo no-receipt"
f3 "doas -n cat $R/guard/receipt.json" > $E/p10-guard-receipt.json
jf $E/p10-guard-receipt.json runtime_running_at_guard_exit    # must print False
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r19 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss | tee $E/t11-after-loss.json  # tool timeout >= 300 s
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r19 --path $R/runtime/mnt --label after" > $E/k04-after.json
python3 -I -B $CR k04 --output $OUT --sha256 $B --baseline $E/k04-baseline.json --baseline-sha256 $(sha $E/k04-baseline.json) --before $E/k04-before.json --before-sha256 $(sha $E/k04-before.json) --after $E/k04-after.json --after-sha256 $(sha $E/k04-after.json) --record $E/t11.json
# ==== END Phase-10 block 11 ====
```

No receipt after 30 tries (5 minutes): T11 unrun; Phase 11 (inspect the guard
first, no signal without Paul's decision). The after-loss step probes the
unarmed m1 (lstat, list, exclusive create, never retried). The record's
`k04_refusal_observed` needs every guest attempt to fail, the marker absent
on the host and no raw-directory change; if m1 already showed the guest's own
`EHOSTDOWN` (T9 re-read, T10 quiesce or the first after-loss attempt) the
probe is flagged vacuous. The record also repeats the plan's `deferred` list
(T5). The recorded answer for remount is "VM restart required".

T8, K04 guest side, last (needs no live runtime; m1 must still be mounted).
**One line at a time, and read each line's result before the next** (the
zq3-r11 slip): the lock probe and the `ls` run **only if** the unmount line
printed `rc=0`. While the runtime still serves (T10 not FENCED), T8 starts
only after its own `gbudget T8` line printed GO; after a FENCED T10 skip
that line (no guard budget applies). On `rc=124` (the 270 s bound) or any other `rc`, record the
`rc`, capture the guest state with the two lines after the block, mark T8
unrun and go to Phase 11; never start the probe behind a stuck unmount.

```bash
# ==== BEGIN Phase-10 block 12 (run each line on its own) ====
gbudget T8 | tee $E/t8-budget.json; echo rc=${PIPESTATUS[0]}   # only while the runtime still serves (T10 not FENCED): GO and rc=0 required; STOP: T8 unrun, Phase 11 (guard-SIGTERM branch)
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo" | tee $E/t8-before.txt   # m1 must be listed
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/t8-block-before.txt
T0=$(date +%s); rockyt "umount $G/m1" | tee $E/t8-umount.txt; echo rc=${PIPESTATUS[0]} seconds=$(( $(date +%s) - T0 ))   # normal only; read rc before the next line: anything but rc=0 means T8 unrun
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/t8-block-after.txt   # R10-2/1a4 check: no new line
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r19-tools/guest-lock-probe.py' < $E/lock-m1-t8.json | tee $E/t8-lock-refusal.json   # only if the unmount printed rc=0; tool timeout >= 300 s
rocky "ls -A $G/m1" | tee $E/t8-ls.txt          # only if the unmount printed rc=0; must print nothing
# ==== END Phase-10 block 12 ====
```

T8 holds only if m1 was listed before, the unmount succeeded, the probe refused at admission
before any open (`"passed": false`, `error` `Refused: exact mountpoint, 9p
source and mount ID required`, exit 10) and the directory is empty.

**If the unmount did not print `rc=0`** (R11-2 again?), capture, each on its
own line, then go to Phase 11:

```text
# ==== BEGIN Phase-10 block 13 (text: not a command, never run) ====
rocky 'ps -eo pid,stat,wchan:32,cmd | grep -E "[u]mount|[g]uest-lock|[p]ython3"' | tee $E/t8-hang-guest.txt
f3 "doas -n tar -c -f - --exclude control.sock -C $R runtime/owner" > $E/t8-owner-hang.tar
# ==== END Phase-10 block 13 ====
```

With 1a4 the stat in front of the unmount fails with `ETIMEDOUT` after its
30 s view wait instead of hanging, and one worst-case getattr (about
90-120 s) plus the unmount's own 30 s view wait fits the 270 s bound, so
`rc=124` should not happen. Since oc4 every hfs 9p reply wait also ends
after 30 s (`ETIMEDOUT`) and marks the session `transport_failed`, so even
a host that stopped answering cannot hold the unmount beyond about one
more deadline (R13-1). If `rc=124` happens anyway, record it as a finding
(the capture above shows where the unmount waits: a `D`-state `umount`
still in `hfs_view_enter` after 270 s would point at 1a4, one in
`hfs_control` or the 9p client at oc4, anything else at another wait).
Record the printed `seconds` either way.

**R10-2 check (z84):** compare `t8-block-after.txt` with
`t8-block-before.txt`. In zq3-r10 T8's unmount added `session … blocked by
failed provider stop on STOP (-16)` for m1. With z84 the unmount should add
**no** line (it may take up to about 2 s longer for the settle). A new
`failed provider stop` line for m1's session (`df8e1015-…`) is a finding
against z84, and a new `view control worker stopped` or `timed out behind a
recall` line one against 1a4/y94; neither changes T8's own result. If T10
was FENCED, m1's session may already carry a block line from T10/T11 (on
purpose); only a new line counts.

Not run: PTE/TLB/busy-pin/GUP exclusion and funded drains (G14), distributed
writers (ov3/pv3).

## Phase 11: cleanup (within A1/A4/A5/A6; about 5-15 min)

**The order below is mandatory. Run one command per line and never batch
them** (zq3-r2 R2-6: a batched cleanup skipped the native unmount, and the
window restore then refused). Write each result into the run log before
running the next line.

If Phase 9 ran (at least one timed OBSERVE), first write the final 984
summary (local, read-only; it never changes the cleanup):

```bash
# ==== BEGIN Phase-11 block 1 (run each line on its own) ====
python3 -I -B $D/sh/p9-timing.py $E/obs-timing.tsv | tee $E/p10-timing-final.json
python3 -I -B $D/sh/p10-admission.py $(ls $OUT/*/release/stdout.log $OUT/lifecycle-*/stdout.log $E/t6-lock.json $E/t7-pod.json $E/t8-lock-refusal.json 2>/dev/null) | tee $E/p11-etimedout-scan.jsonl   # releases, T6-T11: etimedout per file
# ==== END Phase-11 block 1 ====
```

The second line (review round 1) scans every release log, the T9-T11
lifecycle step logs and the T6, T7 and T8 probe reports that exist for
errno 110: lifecycle steps record an `OSError` errno as an observation and
continue, and the probes report errnos in their JSON. Any `"etimedout":
true` line is a finding (write `etimedout_at` into the run log); it changes
nothing in the cleanup.

Before step 1, settle the runtime's state from the guard and journal evidence
(`f3 'pgrep -U root -lf runtime-guard.py'`, `$D/bootstrap-run/journal.jsonl`):

- **Runtime exited at its bootstrap deadline** (no READY; as in zq3-r2 to
  zq3-r5: the runtime exits 1 with `context deadline exceeded` after 120 s,
  and the guard ends with it). The guest still runs under the exporter bhyve
  child, and the runtime has left its `fusefs.homelabfs` mount at
  `$R/runtime/mnt` (R2-6). Follow steps 0-10. Step 6 waits for the bhyve
  child, which ends when the guest powers off, and step 8 will find the native
  mount.
- **Runtime still serving**: READY was reached and the matrix stopped before
  a FENCED T10 (a failed or refused step, a Phase 9 SELinux or OBSERVE miss,
  T10 unrun, or T10 RESTARTED, r1 finding F5; a failed `usable` step (any
  of its three failure kinds), a Phase 9 mount-tuple miss or a guard budget
  STOP (Phases 9-10) also lands here; zq3-r6 to zq3-r18 took this branch). The step 6 `child-gone` check also matches the guard's
  `--vm-name hfs-zq3-r19`, and `vms.sh start-rocky` refuses while the guard
  lives. Before step 0, end it with one SIGTERM to the guard. The guard then
  makes its own bounded stop: one SIGTERM to the runtime, 30 s grace, up to
  300 s observation. Wait for the guard to exit:

  ```bash
  # ==== BEGIN Phase-11 block 2 (run each line on its own) ====
  f3 'pgrep -U root -lf runtime-guard.py'         # exactly one root python guard; note <GUARD_PID> (the csh wrapper is paul's)
  f3 "doas -n kill -TERM <GUARD_PID>"
  f3 'pgrep -U root -lf runtime-guard.py || echo guard-gone'   # repeat every 10 s until guard-gone (at most the 330 s bound)
  # ==== END Phase-11 block 2 ====
  ```

  Then `receipt.json` with the runtime gone: continue. `stop-uncertain.json`
  (runtime still running): stop and preserve; no further signal without
  Paul's decision.
- **FENCED** (T10, expected) or READY ended by the guard at its bound: the
  runtime has exited; follow steps 0-10.

Steps, in this order:

```bash
# ==== BEGIN Phase-11 block 3 (run each line on its own) ====
# 0. guard receipt: the exporter (bhyve child) PID for step 7
f3 "doas -n cat $R/guard/receipt.json" > $E/p11-guard-receipt.json
jf $E/p11-guard-receipt.json runtime_running_at_guard_exit   # must print False
jf $E/p11-guard-receipt.json exporter    # prints None when no child was ever seen: then step 7 uses --no-child and XP stays unset
XP=$(jf $E/p11-guard-receipt.json exporter pid); echo "$XP"     # only when the line above did not print None
# 1. evidence tars (private; runtime-config.json is not included)
f3 "doas -n tar -c -f - --exclude control.sock -C $R guard records runtime/owner launch" > $E/f3-evidence.tar
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r19" > $E/f3-window-evidence.tar
# 1a. transport fence record (nc4; read-only): runtime stderr, the transport.db failure row (in f3-evidence.tar and the owner snapshots), Transport.Failure in every OBSERVE taken
f3 "doas -n cat $R/guard/runtime.stderr" > $E/p11-runtime-stderr.txt
python3 -I -B $D/sh/p11-fence.py $E/p11-runtime-stderr.txt $E/f3-evidence.tar $(ls $E/*owner*.tar $E/observe-p9*.json $E/obs-*.json $E/t9-*.json $E/t10-*.json $D/bootstrap-run/calls/*.json 2>/dev/null) | tee $E/p11-fence.json   # "fenced": false expected
# 1b. ce4 recall history (r19; read-only, findings only): the OBSERVE whose controller.recalls has the largest total (review round 1: a RESTARTED T10 starts an empty history)
python3 -I -B $D/sh/recalls.py $E/recalls-p11.json $(ls -tr $E/observe-p9*.json $E/obs-*.json $E/t9-*.json $E/t10-*.json 2>/dev/null) | tee $E/recalls-p11.txt
# 2. guest 9p mounts: list, then unmount each listed one on its own line, reading its rc before the next line (block lines captured before and after)
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/p11-block-before-umount.txt
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"
T0=$(date +%s); rockyt "umount $G/m0" | tee $E/p11-umount-m0.txt; echo rc=${PIPESTATUS[0]} seconds=$(( $(date +%s) - T0 ))   # only if m0 is listed; R13-1 check: expect about 30-60 s at most; rc=124 (timeout): record, continue
T0=$(date +%s); rockyt "umount $G/m1" | tee $E/p11-umount-m1.txt; echo rc=${PIPESTATUS[0]} seconds=$(( $(date +%s) - T0 ))   # only if m1 is listed; same
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"   # count 0 expected; see below if not
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/p11-block-after-umount.txt   # a new line here after a NORMAL unmount is an R10-2 (z84) or 1a4 finding
# 3. observer removal (banner timeout: check authorized_keys is still $EA before the one retry)
okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r19 --expect-sha256 $EA" < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
# 4. grub check
rocky "grub2-editenv - list"                     # next_entry empty; if set: rocky "$K cancel $KP" (U1) on its own line
# 4a. SELinux denials in the guest (evidence only; "<no matches>" with rc=1 is the good case)
rockyt "ausearch -m AVC,USER_AVC,SELINUX_ERR -ts boot -i; dmesg | grep -i -e avc: -e selinux" > $E/p11-guest-avc.txt 2>&1; echo rc=$?
# 4b. full guest kernel log (evidence only: keeps the m64 `9p-hfs: session … blocked` lines)
rockyt "dmesg" > $E/p11-guest-dmesg.txt 2>&1; echo rc=$?
# 5. guest poweroff
rocky 'systemctl poweroff'
# 6. wait for the child: this one bounded loop (pattern [h]fs-zq3-r19, never another run's; zq3-r14 slip) checks every 10 s, at most 90 times (15 min), matching the EXACT line child-gone with grep -x (zq3-r18 slip); tool timeout >= 1200 s; h64: the vmm context stays until step 7
gone=no; for i in $(seq 90); do if f3 'ps -ax -ww | grep "[h]fs-zq3-r19" || echo child-gone' | grep -qx child-gone; then gone=yes; break; fi; sleep 10; done; echo "child-wait gone=$gone tries=$i $(date -u +%H:%M:%SZ)"   # gone=yes required; gone=no: stop, no forced power-off
# 7. host cleanup (tap, VM context, vars copy): run exactly ONE of the next two lines; the --child-pid line is the normal case
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 093378b73019ad83a9dc2465e12c3138d538c03f586385008fedd3ab5c1eb83f --child-pid $XP" | tee $E/p11-host-cleanup.json   # NORMAL: when step 0 set XP to a PID
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 093378b73019ad83a9dc2465e12c3138d538c03f586385008fedd3ab5c1eb83f --no-child" | tee $E/p11-host-cleanup.json   # ONLY if the exporter is None (step 0 printed None and XP is unset); never as a first try (zq3-r18 slip)
# 8. native FUSE mount: check, then unmount if listed
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount" | tee $E/p11-native-check.txt
f3 "doas -n umount $R/runtime/mnt"               # only if step 8's check listed it; busy or error: stop and preserve
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount"   # must print no-native-mount before step 10
# 9. observer key on f3: this exact plain line, literal paths, never inside a script (zq3-r5); in bash, never zsh (zq3-r13)
f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r19/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r19/observer/id_ed25519.pub"
f3 "doas -n ls -la $R/observer"                  # only rocky-known_hosts left
# 10. window restore, always last; it prints nothing on success
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
f3 "doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r19/restoration-summary.json" | tee $E/p11-restoration-summary.json   # the restore's real outcome
# ==== END Phase-11 block 3 ====
```

Step 1a (nc4): `p11-fence.json` `"fenced": false` is expected. `true` is a
finding: write each record's `Site`, `Cells`, `Class`, `Error` and `Time`
into the run log (`runtime.stderr`, the `transport.db` row and any
`transport.failure` in the OBSERVEs should agree, and the first fence
wins). The cleanup continues unchanged. A runtime restart or restore after
a fence refuses that ledger by design (T10 above): record the refusal,
never work around it. After a FENCED T10 the runtime's own lifecycle fence
may also stop the guest transport: a record whose `Time` follows the T10
RESTART is a consequence of T10, not an R12-1-type failure; record it with
that note. A record before T10 (or without T10) is the finding.

Step 2 after a FENCED T10: m0's session is blocked since T9 and the host is
gone, so its unmount may refuse or not return. A normal `umount` only, never
`-f` or `-l`: if it refuses, or `timeout 270` ends it (`rc=124`; SSH
keepalives alone do not, because sshd keeps answering), record that and
continue with step 3;
the guest poweroff in step 5 takes the mount down with the guest. A
mountinfo count above 0 after that is recorded, not fixed.

Step 2 after a host stop (zq3-r13's **R13-1 check**; oc4 and pc4; zq3-r14 to zq3-r18:
both unmounts `rc 0` within 1 s after both sessions blocked `(-112), exporter
ERROR 5`): in zq3-r13 the runtime was gone, m1 (blocked) unmounted in 1 s, but m0's
`umount` (its session **not** blocked) stayed in D in `hfs_control` past
the 270 s bound (`rc=124`) and delayed the poweroff by about 7.5 minutes.
Now the exporter answers ERROR 5 UNAVAILABLE instead of no reply, and every
hfs 9p reply wait ends after 30 s, after which the session is
`transport_failed` and its teardown stops retrying. So **each unmount here
should return within about 30-60 s** (a blocked session in about 1 s).
Each unmount line prints its `rc` and `seconds`; write both into the run
log. `rc=124`, or `seconds` well above 60, is a finding (R13-1 again):
record it, never force, and continue with step 3. The block lines after the
unmounts may show the expected `(-112), exporter ERROR 5` for a session
whose host was gone (record them).

Step 2 of a session that was **not** blocked (for example m1 when the
matrix stopped before T9, or m0 when it stopped before T9 without a block
line): the host is gone by then, so this is the R13-1 case above; the
unmount should also, with z84, log no `failed provider stop` line. Step 4b's
full `dmesg` shows it; a new such line is a finding against z84 (R10-2
again). With 1a4 an unmount behind a stuck recall ends after its 30 s view
wait instead of staying in D (zq3-r11's R11-2: m1's unmount hit the 150 s
bound and the guest poweroff took about 530 s; zq3-r12's blocked m0
unmounted in about 1 s); `rc=124` here is recorded as a finding.

Step 6 waits for the exact line `child-gone` only (h64; `grep -qx`, so
the loop never matches its own echoed command text, zq3-r18's slip; its one
output line `child-wait gone=… tries=…` does not contain that line). The
run's vmm context
`/dev/vmm/hfs-zq3-r19` is still there after the child exits; step 7's
`host.py cleanup` destroys it with the tap. zq3-r7's step 6 waited for
`no-vmm` as well, which could never appear before step 7.

Step 6: zq3-r3's guest needed about 3.5 minutes to power off after its oops
(R3-4), zq3-r11's about 530 s behind its stuck m1 unmount and zq3-r13's
about 7.5 minutes behind its stuck m0 unmount (R13-1; zq3-r12's about
11 s), hence 90
tries (review round 1; still no forced power-off). If the child is still there after them, stop: no
forced power-off without Paul's decision (risk R2). Release tokens in case
`stdout.log` files are redacted or marked spent before anything is copied
into `docs/lab/results`. Expected restore outcome (j44, first run in
zq3-r3): the unmount happens, and the module comparison accepts only the
plan-declared `fusefs.ko` beyond the post-load baseline;
`restoration-summary.json` records it in `window_module_loaded`. **Judge
step 10 only by that file** (zq3-r5): `"mount_restored": true` is success; a
missing file means the restore refused (its traceback is in
`p11-window-restore.txt`), whatever `rc` a `| tee` reported. If the restore
refuses with `normal mount restore incomplete`, check `mount -p` for a
leftover mount first (zq3-r2's cause was the skipped step 8).

## Phase 12: normal restoration (A1, A3; about 5-10 min)

```bash
# ==== BEGIN Phase-12 block 1 (run each line on its own) ====
f3 'ls -ld /var/tmp/homelabfs-9p-poc/share'           # R6: vm-bhyve's normal start needs it
$S/tools/lab/vms.sh --approved-action start-rocky    # only after Phase 11 verified next_entry empty and the child gone
rockywait 36                                         # in bash that sourced session.sh (the wrapper is fine), never a bare bash -c; tool timeout >= 900 s (up to about 720 s); prints rocky-ssh-up try N HH:MM:SSZ
rocky "$K check --expect stock $KP; getenforce; sha256sum /root/.ssh/authorized_keys; free -m"   # authorized_keys must be e08542dc…
rocky 'ls -l /.autorelabel 2>&1; cat /proc/sys/kernel/random/boot_id; journalctl --list-boots --no-pager | tail -n 4' | tee $E/p12-relabel.txt
f3 'doas -n tail -n 30 /zroot/bhyve/rocky/vm-bhyve.log' > $E/p12-vm-bhyve-log.txt
f3 'env TZ=UTC ps -ax -o pid= -o lstart= -o command= | grep "[b]hyve: rocky"' | tee $E/p12-rocky-pid.txt
rocky "$K remove $KP" | tee $E/p12-kernel-remove.json   # U2 (Paul chose removal)
f3 'doas -n vm list; doas -n ls /dev/vmm; ifconfig -l; mount -p | grep -E "fdescfs|fusefs" || echo none; kldstat; zpool list zroot' > $E/p12-final.txt
# ==== END Phase-12 block 1 ====
```

r19: after Rocky is back and checked (the lines above, U2 included),
confirm that the three non-HomelabFS VMs are still in their default state.
**The run never starts them** (Paul's decision: Stopped by default; the
iormesh session starts them only after telling Claude):

```bash
# ==== BEGIN Phase-12 block 2 (run each line on its own) ====
f3 'doas -n vm list' | tee $E/p12-iom.txt     # iom1, iom2 and iom3 must still show Stopped; running: record it and tell Paul, never stop or start it here
# ==== END Phase-12 block 2 ====
```

After the run, Claude messages the iormesh session that zq3-r19 is over and
that iom1-3 are still Stopped (the run log records the message time).

**Relabel record (SELinux gate, R6-3).** Earlier runs rebooted Rocky about
5 minutes after `start-rocky`. Each test kernel had booted without SELinux
and left `/.autorelabel` behind, so the stock boot relabelled and rebooted
once. That reboot shows as a vm-bhyve "bhyve exited with status 0" followed
by "starting bhyve (run 2)". That vm-bhyve line is the deciding evidence,
because the `/.autorelabel` check below only runs after any relabel reboot. The oc4 test kernel (the unchanged c64 config) boots with
SELinux enforcing, so **no relabel reboot is expected** (zq3-r7 to zq3-r18: none). Write the result into the
run log from three lines:
- `p12-vm-bhyve-log.txt`: the runs since `start-rocky`;
- `p12-relabel.txt`: whether `/.autorelabel` exists at the first SSH answer,
  and the boots `journalctl` lists;
- `p12-rocky-pid.txt`: the bhyve start time.

A relabel reboot, or an `/.autorelabel` still present, is a **finding, not
a failure**. It would mean the test boot ran without SELinux after all, or
something else marked a relabel. Record it and continue.

`rockywait 36` waits up to about 12 minutes for pinned SSH (each failed
try is a bounded SSH attempt through f3 plus 10 s, up to about 20 s; give
the line a tool timeout of at least 900 s, review round 1) (zq3-r12's
slip: its loop ran `bash -c "rocky true"`, where the function does not
exist, and waited 10 minutes for nothing; Rocky's journal began 8 s after
the start). Without a relabel, a stock boot answers in about a minute or
two (zq3-r1's U3: 16 s). With one, it answers after about 5 more minutes:
on `rocky-ssh-timeout` run `rockywait 36` once more; if Rocky still does not
answer, stop and inspect (vm-bhyve log, `vm list`), never force. Run the
`check` line only once SSH answers. Record the new PID, lstart and boot ID
for the next fill. The stock boot is `741.22.1.el9_9` (zq3-r18's Phase 12
booted it last). Expect Rocky's upgrade timer to tick about 10 minutes
after the start (N1); with today's stamp it only logs the deferred
reboot.

Staging prefix deletion is a separate, later step after the evidence is safe
and `mount -p` shows nothing below `/var/tmp/hfs-validation-xz3`. Left on f3
as evidence:
- `$R`, `/var/tmp/hfs-20261121-zq3r19-fdq` and
  `/var/tmp/hfs-fdescfs-windows/zq3-r19`;
- the zq3-r1 to zq3-r18 directories.

Left on Rocky unless Paul decides otherwise:
- `/root/hfs-zq3-zq3-r19-*` and `$G` (retention records, pod rootfs and
  evidence);
- the kernel state journal;
- the zq3-r1 to zq3-r18 leftovers.

fdescfs and fusefs stay loaded on f3 after this run (setup and step 4 load
them), so a later run in the same f3 boot meets D1 again.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r19`): `$S/tools/lab/vms.sh --approved-action start-rocky`
  (test kernel boots once under the normal config); wait for pinned SSH
  with `rockywait 36` in bash that sourced `session.sh` (the wrapper is fine; never a bare `bash -c`; tool timeout
  at least 900 s; once more on a timeout); `rocky "$K check --expect test $KP"`; `rocky "$K
  reboot-to-stock $KP"`; then `rocky "$K check --expect stock $KP"`, U2 and
  the Phase 12 checks. zq3-r1 exercised U3 end to end (16 s to SSH). If the
  first boot reboots itself before the `check --expect test` line (a relabel
  reboot, zq3-r6 R6-3; not expected with the SELinux kernel), `next_entry` is
  already consumed: record it as a finding and continue with `check --expect
  stock`, then U2. A hung test kernel stops here: a forced power-off needs
  Paul's explicit decision.
- **iom1-3 on every stop path.** The run never starts iom1, iom2 or iom3:
  their default state is Stopped (Paul's decision). Any abort (Phases 0b-4,
  a Phase 6 refusal through U3, a hung test kernel) ends with the Phase 12
  check `f3 'doas -n vm list'` showing all three `Stopped` (record it; a
  running one is recorded and reported, never touched), and Claude then
  messages the iormesh session that the run is over.

## Run log template

`$E/RUN-LOG.md` (private) gets one line per phase or decision, with the UTC
time, as in zq3-r18. The lines new or changed for zq3-r19:

```text
# ==== BEGIN Run-log-template block 1 (text: not a command, never run) ====
- <UTC> Session: every line in bash through the wrapper (session.sh + vars.env); zsh never used: yes|no.
- <UTC> N1: Rocky clock Europe/Sofia (timedatectl, R1): yes|no; stamp .. and dnf newest #.. (p1-upgrade.json, dnf_unchanged True|False); Phase 5 nightly-gate GO|STOP at ..Z; Phase 1 pod runtime (p1-pod-runtime.txt) unchanged: True|False; kernel facts unchanged: yes|no.
- <UTC> Phase 1 passed: ... fusefs.ko (id ..) and fdescfs.ko (id ..) loaded, no fuse/fdesc mount, inet 10.20.0.133 on vm-public, vars .., Rocky PID 59685 running .. = default ..
- <UTC> Phase 1a: before-unload Refs 1/1, no-fuse-fdesc-mounts; kldunload fusefs rc .., kldunload fdescfs rc ..; after-unload kldstat lists neither.
- <UTC> Phase 3: kernel install BLS .. for build ID e3d474df (bzImage e5add01e); pod manifest PM=.. (expected 34a71569 as in zq3-r15 to zq3-r18); pod build ..
- <UTC> Phase 4 step 1a process observer (NEW wf4 daemon): PASS|FAIL for bb4ad73c (uid/gid .., groups .., pid ..).
- <UTC> Phase 8: launch ..Z (journal launch intent); exporter 2fd7e32f: exporter.stderr empty|..; mount-0 ok|.., mount-1 ok|.. ; transport fence (p8-fence.json / runtime.stderr): none|Site .. Cells .. Class .. Error ..; READY .. s after launch; usable ok|failed: first .., baseline .., final .., claims .., observe_sequence_last L=..; usable_note: none|..; guest block / worker-stopped / ETIMEDOUT lines (with errno and exporter ERROR n): none|..
- <UTC> Phase 8 after READY: B=.., U=.., L=.., PM=.. (appended to vars.env); minutes since READY ..
- <UTC> Guard budget: launch ..Z, guard end ..Z; Phase 9 gbudget GO|STOP (late_after_ready ..); per step: T1 .., T2 .., T3 .., T4 .., T6 .., T7 .., T9 .., T10 .., T8 ..|n/a (FENCED); any STOP: steps unrun (guard budget) ..
- <UTC> Phase 9: OBSERVE sequence L+1=.. pass|fail; claims Open per provider ..; mountinfo count ..; Enforcing ..; no-hfs-block ..; obs-timing.tsv lines .. (3 ssh-noop + 1 observe).
- <UTC> 984 timing: ssh-noop median .. s; OBSERVE net median .. s, max .. s over .. samples (after T4: ..); net_max_below_984_bound true|false.
- <UTC> o84 contention: slow OBSERVEs or long-open claims none|..
- <UTC> T1..T4: sequences (L+2, L+3) .. (L+8, L+9) as expected|differences ..; syscall_batch_success ..
- <UTC> admission steps (p10-admission.py): credentials .. ms (largest gap .. at ..), sync .., metadata .., namespace ..; deadline_hit none|..; etimedout none|..; dispatch error/uncertain none|..
- <UTC> z94 check: recalls during each case's admission (fence/domain-sequence steps between baseline and retained OBSERVE, owner snapshots): credentials ..|none, sync .., metadata .., namespace ..; open-root gap ..
- <UTC> vf4/2f4/ie4/re4 check: guest log lines (not-yet-current grant waited for the fence and re-queried / passed grant released / view moved / acquiring again / host BLOCKED / released-and-retried acquire / retirement re-send / retry exhausted / re-issued GETATTR or LOOKUP / any WARNING or BUG) none|..; EAGAIN, ESTALE, EKEYREJECTED, ETIMEDOUT or EOPNOTSUPP from a probe syscall none|.. (failing syscall named by guest.py: ..); T1 m0 and m1 root-admitted yes|no; T1 fchown passed yes|no (R18-1 check); T4 link(2) passed yes|no (vf4 d_drop check)
- <UTC> ce4 recalls (recalls.py): after T1 total .. (scopes ALL ../DOMAIN .., triggers ..; longest phase .. us, longest ACK wait .. us); Phase 11 total ..; history_found true|false.
- <UTC> iom1/iom2/iom3: Stopped at Phase 0b yes|no, Phase 1 yes|no (stopped by the run: ..), Phase 5 step 1 yes|no, Phase 12 yes|no (never started by the run); iormesh session messaged at ..Z.
- <UTC> Phase 0b (Paul's approval for this run at ..Z): free .. bytes > swap used .. KiB; swapoff -a rc .., swapon -a rc ..; swapinfo after: .. used. swap: Phase 1 .. KiB used, Phase 5 .. KiB, Phase 6 .. KiB (must be 0 to launch).
- <UTC> Case failure (if any): CASE=.., failing syscall (yf4 name) .. errno ..; fresh OBSERVE sequence <next>=.. (rule: L+3 after T1's baseline, ..) status ..; EOPNOTSUPP/EAGAIN/ESTALE/EKEYREJECTED/ETIMEDOUT none|..; guest WARNING/BUG lines none|..
- <UTC> Tool timeouts used: $CTL lines >= 300 s, T6/T8 lock probe >= 300 s, T7 pod_fixture run >= 420 s: yes|no.
- <UTC> oc4 guest lines: errnos seen (-112 EHOSTDOWN / -116 ESTALE / -71 EPROTO) ..; exporter ERROR n in block/worker lines ..; transport_failed signs ((-112), exporter ERROR 5 or ETIMEDOUT after 30 s) none|..
- <UTC> ETIMEDOUT scan (p11-etimedout-scan.jsonl, releases and T6-T11): none|..; R11-1 check: no early-DRAINED STALE / worker-stopped line|..; z94 recalls visible as open-root gaps: ..; R10-2/1a4 check: T8 unmount rc .. seconds .., new block/worker-stopped/timed-out lines none|..
- <UTC> Transport fence record (p11-fence.json, Phase 11 step 1a): fenced false|true: Site .., Cells .., Class .., Error .., Time ..; runtime.stderr line ..; transport.db row ..; restore refusal (if a restart followed) ..
- <UTC> T5 unrun (deferred, re-plan pending).
- <UTC> T7: pod_fixture run t7_pass ..|refused at check_runtime (N1 drift)|..
- <UTC> T9 pre-arm L+10, ARM L+11, post-CALL L+12; classification ..
- <UTC> T10 post-quiesce L+13, RESTART L+14; status ..
- <UTC> Phase 11 step 2 (R13-1 check): umount m0 rc .. seconds .., umount m1 rc .. seconds ..; mountinfo after ..; poweroff to child-gone .. s (step 6 line as written, pattern [h]fs-zq3-r19).
- <UTC> Phase 12: start-rocky ..; rockywait: rocky-ssh-up try .. at ..Z (in the session shell); vm-bhyve runs ..; relabel none|..
# ==== END Run-log-template block 1 ====
```

**zq3-r15 review round 1 (2026-11-19), kept:** tool-timeout comments on every $CTL, lock-probe (>= 300 s) and T7 pod run (>= 420 s) line; the D1 module ids (now 34/35, from the 15:14Z facts); the facts time in EET.

**zq3-r16 review round 1 (2026-11-20), kept:** the zq3-r14 note's boot ID (`b4d9ee31`), the git-diff scope wording, a tool-timeout comment on `$CTL status`.

**zq3-r17 review round 1 (2026-11-20), kept:** the zq3-r5 execution notes' literal key-deletion line and restore-summary paths name the current run ID (zq3-r19), and so does the wait-helper note.

**zq3-r18 review round 1 (2026-11-21), kept:** the case-failure block takes one fresh OBSERVE while the runtime still serves (now in the yf4 direct form, see the zq3-r18 run notes); `recalls.py` picks the history with the largest `total`; iom handling on every stop path (now: check that iom1-3 are still Stopped, never start them).

**zq3-r19 review:** pending.
