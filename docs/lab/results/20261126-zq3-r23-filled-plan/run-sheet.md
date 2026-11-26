# zq3-r23 run sheet (filled 2026-11-26)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](facts/) · [zq3-r22 record](../20261126-zq3-r22/README.md) ·
[zq3-r22 sheet](../20261126-zq3-r22-filled-plan/run-sheet.md)

Ordered commands for run `zq3-r23`, the twenty-third attempt of zq3, with
the values filled on 2026-11-26 from `main` `f584de4` (the r23 candidate
merge and its review round 1). The candidate is the **r23 rebuild
`21c49a55…`** (source checkout and repository baseline `21d8478`, the km4
merge; built 2026-11-26 for this run). It follows zq3-r22's R22-1: T1's
`create(group-tail)` returned `ENOSPC` after 33 s, m0 blocked on an
uncertain `VIEW_RETIRE` (`-110`) and m1 on `PROCESS_EXIT (-28)`. The host
had refused access child 1's `VIEW_RETIRE` with a bare `WouldBlock`
(another process of the domain held an observation lease), which the
transport retried without ever answering the cell; the guest's cleanup
worker waited out its 30 s bound while holding the session's retirement,
so the CREATE's 6m4 retire-and-retry could not free anything. **The guest
kernel and the CGO daemon (with the window artifacts) were rebuilt:**
- **the CGO daemon, new bytes** `43238fe9…` (go1.27.4, from `21d8478`):
  **km4**. `Facts.checkRetire` returns `viewcontrol.ErrRetirementHeld`, and
  `Controller.retire` answers it `BLOCKED` at the unchanged floor, so the
  guest records a blocked `VIEW_RETIRE` and offers it again later (re4)
  instead of waiting out its bound. The domain-wide rule is unchanged.
  vj4, dh4, s64 host, hh4 host, sj4, 2h4, 0a4, fh4, a84, l64 and nj4 are
  kept. **Phase 4 step 1a is required for these new bytes** (the earlier
  qualifications of `c7b89648…` do not carry over);
- **the guest kernel, new bytes** (`6.1.190-hfs-xz3`, bzImage
  `049b1367…`, GNU build ID `2435216f…`, System.map `1a80593f…`;
  Module.symvers `b68cf94a…` and config `692ed3fa…` unchanged, SELinux
  still in `CONFIG_LSM`): km4's guest part is a log line only.
  `hfs_retire_shared` now logs each 6m4 retire-and-retry round
  (rate-limited): `9p-hfs: session <uuid> retired under charge pressure:
  own retire <errno> freed <n> requests, <k> of <m> other sessions freed
  <n>; retrying the charge once`, or `… nothing freed, the charge fails`.
  6m4, s64, hh4, dh4, 5f4, kn1, wj4, tj4, 1h4, 4h4, vf4, 2f4, re4 and ie4
  are kept. The five modules and `modules.*` are byte-identical;
- **the window artifacts, rebuilt with the daemon** from `21d8478`:
  `controlledruntime.test` `d58fedc4…` and `fdescfs-namespace` `6d54858c…`
  (the committed window `pins.json` moved to them and to the daemon);
- **reused byte for byte from the r22 rebuild `2c85b230…`** (now refused
  as superseded): the native exporter `8cadae40…`, which ran cleanly in
  zq3-r21 and zq3-r22 (empty `exporter.stderr`). The runtime config's
  `Exporter.SHA256`, the argv record and `argv.py` pin it;
- **`hfs_fdattr` stays enabled (bl4/nl4).** In zq3-r21 and zq3-r22 both
  mountinfo rows showed it once, right after `cache=mmap`, and T1's
  `fchown`/`fchmod` of `group` through the open description returned 0.
  `21c49a55…` is now the one entry of `controller.FD_ATTR` (s64 in kernel,
  exporter and daemon). So the batch plan and every request carry
  `"fd_attr": true`, and the bootstrap plan names
  `fd_attr_candidate_sha256` `21c49a55…` with `fd_attr_candidate_manifest`
  (this package's `manifest.json`) and `fd_attr_kernel_plan` (this fill's
  workstation kernel plan copy). Both mounts use `cache=mmap,hfs_fdattr`,
  and T3 adds the s64 peer-rename guard (`stat` of the old name through m0
  must fail `ENOENT`). It has not run for real yet;
- **lab tools re-pinned at `f584de4`:** `controller.py` and
  `rocky_kernel.py` (admit only `21c49a55…`). Every other tool is
  unchanged since the zq3-r22 fill, and every staged copy is re-hashed
  against main;
- **be4 (kept):** guest.py's admission alarm 150 s, a request's whole
  deadline 240 s (this batch), lifecycle steps 180 s, the controller's SSH
  dispatch bound min(request + 15, plan seconds) = 255 s (plan `seconds`
  255), the T6 lock probe case 150 s / overall 240 s, and T7's workload
  240 s with `pod_fixture.py`'s `RUN_SECONDS` 315 s. The guard stays
  3600 s, so a run with several slow steps STOPs early by design;
- `hfs-runtime-prep` and the signer rebuilt at `f584de4` with go1.27.4
  (both link the km4-changed `controlledruntime` and `viewcontrol`
  packages, the candidate daemon's own source; `git diff 21d8478 f584de4`
  touches no Go source);
- the workstation `/usr/bin/ssh` is unchanged (`d4c70b09…`), and so is f3's
  (`6a9e5573…`, F1);
- **Rocky runs the stock `5.14.0-741.27.1.el9_9`** = grub default =
  `saved_entry`, with `next_entry` empty, since zq3-r22's Phase 12 started
  it (14:51:24Z, PID `50982`, boot `99de7fb2-…`). That start changed the
  UEFI vars (`4f63d71b…`), so the argv record, the bundle and its sessions
  are new. **The custom kernel never becomes Rocky's default:** Phase 3 adds
  a non-default entry, Phase 5 sets it only as the one-shot `next_entry`,
  the Phase 8 boot consumes it, Phase 11 verifies `next_entry` empty,
  Phase 12's restart boots the stock `741.27.1` and checks it (`check
  --expect stock`), and U2 removes the test kernel;
- **no dnf run since #68** (2026-11-26 00:55 EET) at the fill's facts: the
  pod template's pins equal zq3-r22's (refilled from the facts anyway);
- **iormesh VMs:** another session uses f3 for its own el9 VMs (iom1-3 and
  a k3s test VM), started and stopped one at a time. At the facts
  (20:15Z) iom1-3 were Stopped and no k3s VM was listed; after the facts,
  with the orchestrator's agreement, the iormesh session started iom1 and
  iom2 (2 GiB each, for up to about 5 hours), since this run is not before
  the next day. **The run-day refresh must confirm that every iormesh VM
  is Stopped again.** **The run starts only while every
  iormesh VM is Stopped and `/dev/vmm` holds only `rocky`, and this sheet
  never starts or stops one:** if one runs at Phase 1 or Phase 5 step 1,
  stop the run and coordinate;
- **f3 swap is 0 used** at the facts: **no Phase 0b this run.** Phase 1
  checks that swap shows 0 used; if not, stop and ask Paul (Phase 6 still
  requires swap unused);
- **f3 left `fdescfs.ko` (id 39) and `fusefs.ko` (id 40) loaded** after
  zq3-r22: Phase 1a unloads them again;
- **R22-2 (operator): the Phase 8 driver runs in the background and the
  operator polls its log, never a task notification.** Phase 8 block 2a
  waits for `READY` (or the driver's end) in `p8-bootstrap.jsonl` with a
  bounded loop that cannot match itself, then for `summary.json`, and the
  after-READY lines start at once.

**This run does not happen on the fill day.** The facts were taken at
2026-11-26 20:15:19Z, about an hour before that day's latest Phase 5 start
(21:25 UTC), which is too close for Phases 0 to 4. **On the run day, after
Rocky's 00:55 EET (22:55 UTC) nightly dnf run has stamped, redo Phase 0's
recheck and take fresh read-only Phase 1 facts before anything else** (the
zq3-r21 later-day refresh, commits `9b28254` and `18f268d`):
- the newest dnf transaction still #68 and the pod runtime, kernel facts,
  Rocky PID/boot, UEFI vars and f3 boot unchanged: the sheet runs as filled
  (only the Phase 1 records are new);
- a new transaction (#69 or later) that changed nothing pinned: refill
  `sh/n1-gate.py`'s expected transaction and the sheet's N1 lines (a
  record fix: the gate does not read it), then n1-gate.py's hpin in
  `session.sh`, and `session.sh`'s pin in the Phase 0 `sha256sum` list and
  `pins.json` (hpin refuses a stale pin, so a missed step fails closed);
- **a new stock kernel made the grub default while Rocky still runs
  `741.27.1` (the N1 kernel split): STOP.** Paul must approve a Rocky
  maintenance reboot into the new default first (as on 2026-11-26 06:46Z),
  and the reboot means a refill (new kernel plan stock default, UEFI vars,
  argv record, bundle and sessions, Rocky PID in `NG` and the Phase 5
  helpers);
- a changed pod runtime: refill the pod template (and its pins);
- any Rocky restart (new PID, boot ID or UEFI vars) or f3 reboot: refill;
- **every iormesh VM Stopped and `/dev/vmm` = `rocky` again** (iom1 and
  iom2 were started after the fill's facts, with the orchestrator's
  agreement, for up to about 5 hours): if one still runs, the run waits
  and the orchestrator coordinates; the sheet never touches them.

**Run-day refresh done (2026-11-26 22:56:44Z, stamp `20261126T225644Z`,
run day 2026-11-27 EET): a record refresh.** Rocky's nightly run stamped
`2026-11-27` at 00:55:07 EET and wrote **dnf transaction #69** (2026-11-27
00:55, 2 packages: `rsync-3.2.7-4.el9_9` and `libgs-9.54.0-20.el9_9.1`;
`needs-restarting -r` clean). Nothing pinned changed: Rocky PID `50982`,
lstart, boot ID `99de7fb2…`, running kernel `741.27.1` = grub default =
`saved_entry`, `next_entry` empty, python3.9, `authorized_keys`, the pod
runtime (hashes and NEVRAs), UEFI vars `4f63d71b…`, the launch facts, f3
boot `1794574308`, f3 `/usr/bin/ssh`, swap 0 used and both modules (ids 39
and 40, no mounts) as at 20:15Z; every iormesh VM was Stopped and
`/dev/vmm` held only `rocky`. So `sh/n1-gate.py` now expects **#69 of
2026-11-27 00:55** (`c354f923…`), `session.sh`'s hpin names it, and
`session.sh` is `3e92efb9…` in the Phase 0 list and [pins](pins.json).
Phase 0 passes with the new pins. The refreshed facts are in
[facts/](facts/) next to the 20:15Z ones, which stay the baseline for
everything the refresh found unchanged. The run's own Phase 1 (below) is
still taken on the run day and must show #69.

**Rocky upgrades itself once a day** ([N1](#n1-rockys-nightly-upgrade)): a
`dnf -y upgrade` runs at 00:55 Rocky-local = 22:55 UTC (Rocky's clock is
Europe/Sofia, EET, UTC+2; zq3-r12's run-time revision R1). The run must
start after that day's run has stamped and finish before the next one
(**22:55 UTC on the run day**); the gate before Phase 5 enforces it (latest
Phase 5 start 21:25 UTC on the run day; the gate computes the dates from
its own clock). Phase 1 rechecks the stamp, the dnf history (newest
transaction #68 at the fill, **#69** at the run-day refresh), the pod-runtime NEVRAs and SHA256s and
the kernel facts, and any drift means a refill (above).

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

**How to read the code blocks (zq3-r18's operator slips; zq3-r19 to zq3-r22 ran with them and had none).** Nothing in this
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
this filled plan (A1-A8 of the run plan, including the Phase 1a unload;
there is no Phase 0b swap clearing this run).
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
  `bootstrap.py run | sign_watch.py | tee` line in the background (the
  shell tool's background mode) and **at once** run Phase 8 block 2a, which
  polls `p8-bootstrap.jsonl` for `READY` (or the driver's end) and then
  `summary.json` with bounded loops that read files and so cannot match
  themselves (each wait line with a tool timeout of at least 600 s): up to
  120 s to READY plus at most about 100 s for `usable`. **Never wait for
  the background task's completion notification** (R22-2: it arrived
  about 20 minutes after the driver had exited, and Phase 9 started late).
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
  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/session.sh && source
  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/vars.env && <line>'` (or one
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

## Execution notes (carried over from zq3-r3 to zq3-r23)

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
  batch template pins it (`d4c70b09…` since the Fedora 45 workstation upgrade; `16cb2c6a…`
  through zq3-r20); an openssh update since the fill
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
  f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r23/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r23/observer/id_ed25519.pub"
  # ==== END Execution-notes-from-the-zq3-r5-run-2026-11-09 block 1 ====
  ```

- **The window restore's outcome is `restoration-summary.json`, not an exit
  code.** `fdescfs-window.py restore` prints nothing on success (zq3-r5's
  `p11-window-restore.txt` is empty). An `rc` reported through `| tee` or any
  other output filter is the filter's status, not the restore's. Phase 11
  step 10 therefore reads
  `/var/tmp/hfs-fdescfs-windows/zq3-r23/restoration-summary.json`: the restore
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
  `/var/tmp/hfs-fdescfs-windows/zq3-r23/restoration-summary.json` (Phase 11
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
  `[h]fs-zq3-r16` (zq3-r15 followed this); a hand-written wait helper must name the current run ID (`zq3-r23`) and be
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
  exactly from the facts file (`rocky-phase1-20261122T163703Z.txt`).
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

## Execution notes from the zq3-r19 run (2026-11-21)

zq3-r19 ([record](../20261121-zq3-r19/README.md)) passed Phases 0b to 9:
the Phase 0b swap clearing (Paul's approval; 852,220 KiB → 0), Phase 1a
(fusefs id 36, fdescfs id 35, `rc 0` each), Phase 4 step 1a for the wf4
daemon `bb4ad73c…` (uid/gid 0, groups `[0,0,5]`, pid 88454), the predicted
pod manifest `34a71569…`, the nightly gate GO (16:11Z, 16:14Z), a drain
(`active_based` margin −1.28 GB from 0.99 GB of laundry after the swap
clearing; the drain freed 0.78 GB) and Phase 6 (margin 0.37 GB). Phase 8
reached READY 33 s after launch (mounts 395 and 444, both signatures by the
watcher, `usable`, L = 2); Phase 9 passed (budget GO, 3494 s left). Then:
- **R18-1 is fixed (vf4):** yf4's named events show `fchown(group,
  41002:42002)` errno 0, then `fchmod 0640` and the 12-byte `pwrite`; no
  guest `WARNING`/`BUG` line at all.
- **R19-1:** the `credentials_groups` access child (uid 41001, groups
  `[42001, 42002, 42003]`) opened `group` read-only and got
  `BlockingIOError` errno 11 (`EAGAIN`); the parent refused the case
  ("supplementary group access") and the probe ended
  `dispatching-uncertain` after 96.0 s (admission 8.14 s). The guest log
  showed bursts of `observation VIEW_ACQUIRE BLOCKED by the host (attempt
  N); retiring, then retrying after 52/100/200 ms` for m0 and, at 207.45 s,
  `VIEW_ACQUIRE BLOCKED, retry limit reached after 4 attempts (-EAGAIN)`
  (four attempts in about 0.43 s): the re4 back-off counted a busy local
  retirement round against its BLOCKED cap. Fixed by **1h4** (this
  candidate's kernel). T2-T11 unrun.
- **Recalls (ce4, wf4):** the history held 10 recalls: two bootstrap DOMAIN
  `VIEW_RELEASE`, then in T1 `CALL_CONFLICT/MKDIR` ALL 13.1 s, five
  `EXPECTED_TARGET/VIEW_ACQUIRE` recalls, **all DOMAIN** (5.2-6.7 s), one
  `RELEASE` DOMAIN 7.0 s and `CALL_CONFLICT/CREATE` ALL 14.7 s. **xf4**
  (this candidate's 3h4 daemon and 4h4 kernel) replaces the expected-target
  recalls after the guest's own changes with view advances: the model
  replay (094/2a4, `TestZQ3T1CredentialsReplay`) has only the 7
  CALL_CONFLICT recalls and about 37 advance grants, about 1 min 26 s
  modelled (about 2.5 min measured).
- **The case-failure block ran exactly as written** (`N=5`, the direct
  OBSERVE at sequence 5 was OBSERVED, `fenced` false, `runtime.stderr`
  empty, owner live/after tars, full guest dmesg); `recalls.py` picked the
  ten-recall history.
- **Operator flow:** no slip recorded; the BEGIN/END markers, the step 6
  `grep -qx child-gone` loop and the step 7 order worked.

Phase 11 and Phase 12 were clean: guard SIGTERM, `fenced: false`, block
lines only after the guard stop (the R13-1 pattern), both unmounts `rc 0`
in 0 s, no AVC, dmesg without `WARNING`/`BUG`, the window restored, Rocky
restarted (PID 93547, no relabel), U2 done, iom1-3 still Stopped. Since
then Rocky was rebooted once more for the N1 kernel split (above: PID
30974, 16:36:05Z on 2026-11-22). Kept, and written into the lines they
name: Phase 1a (zq3-r19's window left both modules loaded again), Phase 4
step 1a (**required**, then for `c04c519f…`), the be4 guard budget, bash
only, the R13-1 unmount timing, the yf4 case-failure OBSERVE and every
finding rule.

## Execution notes from the zq3-r20 run (2026-11-22)

zq3-r20 ([record](../20261122-zq3-r20/README.md)) passed Phases 0 to 9
after Rocky's approved maintenance reboot into `741.24.1` (16:36Z; the N1
kernel split): Phase 1a (fdescfs id 36, fusefs id 37, `rc 0` each), the
predicted pod manifest `34a71569…` (dnf #66 did not change the rootfs),
Phase 4 step 1a for the 3h4 daemon `c04c519f…` (uid/gid 0, groups
`[0,0,5]`, pid 35139), the nightly gate GO (17:10Z, 17:13Z), the Phase 5
estimate margin +0.26 GB with a drain (free +0.18 GB), Phase 6 margin
0.37 GB, READY (launch 17:13:51Z, mounts 391 and 408, both signatures by
the watcher, `usable` with L = 2) and Phase 9 (OBSERVE net median 0.56 s).
Then:
- **1h4 worked:** five times in T1 the guest logged `observation re-check
  failed (VIEW_ACQUIRE BLOCKED for the target before a reinstall);
  acquiring again at once`; no `EAGAIN`, no `retry limit reached`.
- **R20-1:** T1 (`credentials`) ended `dispatching-uncertain` after
  103.1 s (admission 7.6 s). mkdir, open-leaf, fchmod 0777, create `group`,
  fchown 41002:42002, fchmod 0640 and the 12-byte pwrite all returned 0;
  then the access child (uid 41001, groups `[42001,42002,42003]`) failed
  `EACCES` and the case was refused ("supplementary group access"). The
  host's `revalidatePath` required search permission on every ancestor of
  the leaf, and the 0700 root (uid 0) refused it. Fixed by **sj4** (this
  candidate's daemon).
- **xf4's advance never fired:** `advances` 0; the guest never sent
  `"advance"` because its cacheless test counted every registered inode
  (the mount root lives as long as the mount). Fixed by **tj4** and **wj4
  item 1** (this candidate's kernel). T1 still paid five EXPECTED_TARGET
  recalls (DOMAIN, 4.4-6.0 s) and two CALL_CONFLICT recalls (MKDIR ALL
  26.6 s, SETATTR ALL 11.6 s); **vj4** (this candidate's daemon) removes
  the CALL_CONFLICT ones against the requester's own leases.
- No guest `WARNING`/`BUG`, no hfs block line during the case, no fence,
  `runtime.stderr` empty, no AVC. T2-T11 unrun.

Phase 11 and Phase 12 were clean: guard SIGTERM 17:18:25Z (gone
17:18:35Z), `fenced: false`, both unmounts `rc 0` within 1 s, the window
restored, Rocky started on stock `741.24.1` at 17:19:43Z (PID 40425, boot
`9a3774b8-…`), no relabel, U2 done, iom1-3 still Stopped. Kept, and written
into the lines they name: Phase 1a (zq3-r20's window left both modules
loaded again), Phase 4 step 1a (**required**, now for `c7b89648…`), the be4
guard budget, bash only, the R13-1 unmount timing, the yf4 case-failure
OBSERVE and every finding rule; new: an `EACCES` from a probe syscall below
the root is a finding (R20-1 again, against sj4).

## Execution notes from the zq3-r21 run (2026-11-26)

zq3-r21 ([record](../20261126-zq3-r21/README.md)) passed Phases 0 to 9
after Rocky's approved maintenance reboot into `741.27.1` (06:46Z; the N1
kernel split again): Phase 1a (fusefs id 38, fdescfs id 37, `rc 0` each),
the predicted pod manifest `34a71569…`, Phase 4 step 1a for the daemon
`c7b89648…` (uid/gid 0, groups `[0,0,5]`, pid 89245), the nightly gate GO
(07:00Z, 07:03Z), the Phase 5 estimate margin +0.14 GB with a drain (about
0.23 GB freed), Phase 6 margin 0.29 GB, READY (launch 07:04:58Z, READY
07:05:32Z, mounts 391 and 408, both signatures by the watcher, `usable`
with L = 2) and Phase 9 (OBSERVE net median 0.474 s). The exporter
`8cadae40…` launched cleanly, and both mountinfo rows showed `hfs_fdattr`
once, right after `cache=mmap`. Then:
- **R20-1 is fixed (sj4):** the access child (uid 41001) read `group`
  below the 0700 root; the second child got `EACCES` as designed.
- **The xf4 advance and vj4 work:** `recalls.py` reported `advances` 8 and
  `self_admissions` 8, only the 2 bootstrap `RELEASE/VIEW_RELEASE` (DOMAIN)
  recalls, and no CALL_CONFLICT or EXPECTED_TARGET recall. T1 admission
  took 8.6 s.
- **R21-1:** T1 (`credentials`) ended `dispatching-uncertain` after 44.1 s.
  `create-exclusive group-tail` and its `fchown` returned 0, then
  `fchmod(group-tail, 0640)` returned `ENOSPC` about 0.2 ms after it was
  invoked, and both sessions blocked with `-28` (a DESCRIPTION_RELEASE on
  m0, a PROCESS_EXIT on m1; the view workers stopped with `-112`). The
  host recorded no refusal, fence or stderr. The guest's shared ordinary
  byte budget (512 MiB) was full of retained but retirable owners, and
  `hfs_call` failed on its completion package without retiring. Fixed by
  **6m4** (this candidate's kernel).
- No guest `WARNING`/`BUG`, no AVC, no fence, `runtime.stderr` empty.
  T2-T11 unrun.

Phase 11 and Phase 12 were clean: guard SIGTERM 07:08:09Z (gone
07:08:12Z), `fenced: false`, both unmounts `rc 0` within 1 s, the window
restored, Rocky started on stock `741.27.1` at 07:09:18Z (PID 93782, boot
`c7fd367a-…`), no relabel, U2 done, iom1-3 still Stopped. Kept, and written
into the lines they name: Phase 1a (zq3-r21's window left both modules
loaded again, now ids 38 and 39), Phase 4 step 1a (runs in every run; the
daemon `c7b89648…` is the same bytes), the be4 guard budget, bash only, the
R13-1 unmount timing, the yf4 case-failure OBSERVE and every finding rule;
new: an `ENOSPC` from a probe syscall is a finding (R21-1 again, against
6m4), with the guest's `blocked by … (-28)` lines.

## Execution notes from the zq3-r22 run (2026-11-26)

zq3-r22 ([record](../20261126-zq3-r22/README.md)) passed Phases 0 to 9
with no dnf run and no Rocky reboot since its fill: Phase 1a (fusefs id
39, fdescfs id 38, `rc 0` each), the predicted pod manifest `34a71569…`
(82 entries), Phase 4 step 1a for the daemon `c7b89648…` again (uid/gid 0,
groups `[0,0,5]`, pid 39889), the nightly gate GO (14:21Z, 14:25Z), the
Phase 5 estimate margin +0.15 GB with a drain (about 0.20 GB freed), Phase
6 margin 0.28 GB, READY (launch 14:26:46Z, READY 14:27:19Z, mounts 391 and
408, both signatures by the watcher, `usable` with L = 2) and Phase 9
(OBSERVE net median 0.447 s). The exporter `8cadae40…` launched cleanly,
and both mountinfo rows showed `hfs_fdattr` once, right after
`cache=mmap`. Then:
- **R20-1 stays fixed (sj4):** access child 1 (uid 41001) read `group`
  below the 0700 root; child 2 got `EACCES` as designed.
- **xf4 and vj4 still work:** `recalls.py` reported `advances` 7 and
  `self_admissions` 6, only the 2 bootstrap `RELEASE/VIEW_RELEASE`
  (DOMAIN) recalls. T1 admission took 10.0 s.
- **R22-1:** T1 (`credentials`) ended `dispatching-uncertain` after
  73.9 s. `create-exclusive(group-tail)` returned `ENOSPC` 33.26 s after it
  was invoked; m0 blocked `by uncertain control result on VIEW_RETIRE
  (-110)` and m1 `by process-exit failure on PROCESS_EXIT (-28)`, both view
  workers stopped with `-112`. The host had no CREATE claim; m0 kept 7
  retirable TERMINAL claims. Root cause (km4): the host refused access
  child 1's `VIEW_RETIRE` with a bare `WouldBlock` while child 2's and the
  T1 process's observation leases were active, and the transport retried
  that cell without ever answering it; m0's single cleanup worker waited
  out its 30 s bound holding the retirement mutex, so the CREATE's 6m4
  retry could not retire. Fixed by **km4** (this candidate's daemon: such a
  refusal is answered `BLOCKED`); the guest now logs each 6m4 round.
- No guest `WARNING`/`BUG`, no AVC, no fence, `runtime.stderr` and
  `exporter.stderr` empty. T2-T11 unrun.

**R22-2 (operator):** the driver ran in the background as required, reached
READY at 14:27:19Z and exited at 14:28:04Z, but the operator waited for the
background task's completion notification instead of watching
`p8-bootstrap.jsonl`. It arrived at 14:47:11Z, so Phase 9 started at
14:47:23Z (`late_after_ready: true`; the guard budget still gave GO) and
Rocky's downtime grew by about 20 minutes to 25 min 42 s. Fixed in this
sheet: Phase 8 block 2a polls the driver log with a bounded loop and the
after-READY lines start at once.

Phase 11 and Phase 12 were clean: guard SIGTERM 14:49:53Z (gone
14:49:56Z), `fenced: false`, both unmounts `rc 0` in 0 s, the window
restored, Rocky started on stock `741.27.1` at 14:51:23Z (PID 50982, boot
`99de7fb2-…`), no relabel, U2 done, iom1-3 still Stopped. Kept, and
written into the lines they name: Phase 1a (zq3-r22's window left both
modules loaded again, now ids 39 and 40), Phase 4 step 1a (runs in every
run, and is required for the new daemon `43238fe9…`), the be4 guard
budget, bash only, the R13-1 unmount timing, the yf4 case-failure OBSERVE
and every finding rule; new: a guest `blocked by uncertain control result
on VIEW_RETIRE` line is R22-1 again (a finding against km4), and the km4
`retired under charge pressure` lines are recorded.

## New for zq3-r23

- **Candidate:** the r23 rebuild `21c49a55…` (source checkout and
  repository baseline `21d8478`, the km4 merge, so there is no
  `inputs/repository-changes.patch`; package
  `/home/paul/tmp/claude/hfs-xz3-20261126-r23/package`; built and admitted
  offline on 2026-11-26, [evidence](../../../../tools/integration/artifacts/evidence/xz3-20261126-r23/README.md)).
  - **CGO daemon, new bytes:** `43238fe9…` (go1.27.4; km4). Bootstrap
    `runtime.sha256`, the guard `--runtime-sha256`, `sh/p4-observer.py`,
    the window pins and the staged binary pin it. **Phase 4 step 1a is
    required for it** (never run on f3 yet).
  - **Guest kernel, new bytes:** bzImage `049b1367…`, GNU build ID
    `2435216f…`, System.map `1a80593f…`; Module.symvers `b68cf94a…`,
    config `692ed3fa…`, the five modules and `modules.*` unchanged. The
    kernel plan was regenerated (`dda355cf`); against zq3-r22's it differs
    in `run_id`, `manifest_sha256`, the bzImage and System.map entries and
    the build ID; the stock default stays `741.27.1`. The r22 package
    `2c85b230…` is refused as superseded ("use the r23 rebuild
    (21c49a55)").
  - **Window artifacts, new bytes:** `controlledruntime.test` `d58fedc4…`
    and `fdescfs-namespace` `6d54858c…` (built with the daemon). The r23
    window pins are the committed `pins.json` at `f584de4` renamed
    (`22b2c45b`); zq3-r22's artifact hashes refuse there.
  - **Same bytes as zq3-r22:** the exporter `8cadae40…` and the G3 init
    helper `8c02b98f…`.
- **`hfs_fdattr` enabled (bl4/nl4/ol4)**, as in zq3-r21 and zq3-r22:
  - batch template: `"fd_attr": true` at the top and in each of the four
    requests (release and step requests copy it); `check` on a zero-ID copy
    reports it, and a plan with `fd_attr` false, a request without it or
    requests with it under a plan without it refuse;
  - bootstrap plan: `fd_attr_candidate_sha256` `21c49a55…`,
    `fd_attr_candidate_manifest`
    `/home/paul/tmp/claude/hfs-xz3-20261126-r23/package/manifest.json`
    (read under that digest; its daemon must be `runtime.sha256` and its
    exporter the runtime config's `Exporter.SHA256`) and
    `fd_attr_kernel_plan` (this fill's `kernel/rocky-kernel-plan.json`,
    read under `kernel.plan_sha256`; its `manifest_sha256` is the same
    candidate). `bootstrap.py check` passes; a missing field, the r22
    manifest, zq3-r22's kernel plan, a missing manifest file or zq3-r22's
    daemon as `runtime.sha256` refuse;
  - the mounts: `cache=mmap,hfs_fdattr` (mountinfo must show `hfs_fdattr`
    once, right after `cache=mmap`), and `fill_plan.py batch` requires the
    batch's `candidate_manifest_sha256` to equal
    `fd_attr_candidate_sha256`;
  - T3 adds the VM-only s64 guard (Phase 10); zq3-r21 and zq3-r22 never
    reached it.
- **Runtime spec:** unchanged since 95f6d84 (it pins the exporter
  `8cadae40…`; `ControlTickets` 24). The bundle was regenerated from it
  (`make-spec.py`: only the run ID and the operator key path change; the
  output equals zq3-r22's spec renamed). The generated files have
  zq3-r22's key structure (`runtime-config.json` differs only in its
  session-UUID map keys).
- **Tools rebuilt** at `f584de4` with go1.27.4: `hfs-runtime-prep`
  `63fa89c6` and the signer `d606a1cd` (two identical builds each, the
  second forced with `-a`; `vcs.modified=false`).
- **Re-pinned** (`git diff 1ed87bf f584de4 -- tools/lab
  docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`):
  `controller.py` `167f8363` and `rocky_kernel.py` `e99d91b8` (admit only
  `21c49a55…`; `FD_ATTR`), and the window `pins.json` (the three artifact
  pins). Unchanged: `bootstrap.py` `b8714120`, `bootstrap_remote.py`
  `5155ff08`, `fill_plan.py` `1a32774d`, `guest.py` `746a67bc` (the batch
  `probe_sha256`), `correlate.py` `d20f9753`, `pod_fixture.py` `7c1e545f`,
  `argv.py` `940d4787`, `guest-lock-probe.py` `fd439897`, `pod_workload.py`
  `c714370f`, `observer.py` `2e1449dd`, `observer_key.py` `cd7f38c2`,
  `host.py` `738e1ca5`, `host_snapshot.py` `b29ffed2`, `fdescfs-window.py`
  `b00b19d1`, `fdescfs-window-plan.py` `56ef5bb4`, `runtime-guard.py`
  `fece96b3…` and the native test guard `576d802a…`.
- **Workstation `/usr/bin/ssh`:** `d4c70b09…` (unchanged) in the batch and
  bootstrap `ssh.sha256` and the Phase 0 recheck. f3's `/usr/bin/ssh`
  (`6a9e5573…`, the bootstrap `observer.ssh_sha256`, F1) is unchanged.
- **Pod template:** `runtime_sha256` and `runtime_nevra` refilled from the
  2026-11-26 20:15:19Z `== pod-runtime` facts, equal to zq3-r22's
  (podman `8d94a8fb…` `podman-6:5.8.5-5.el9_9.x86_64`; crun, conmon,
  seccomp.json unchanged); `fixture_sha256` is pl4's `7c1e545f…`. The
  committed `pod_fixture.py` accepts it and refuses the malformed variants,
  zq3-r20's fixture pin included. **Expected rootfs manifest `34a71569…`**
  again (zq3-r21 and zq3-r22 measured it; no dnf run since).
- **No Phase 0b:** f3 swap is 0 used at the facts. Phase 1 requires 0
  used; anything else stops before Phase 1a and Paul decides.
- **iormesh VMs:** Phase 1 and Phase 5 step 1 require every one of them
  `Stopped` and `/dev/vmm` to hold exactly `rocky` (the window is scheduled
  while they are stopped); **if one runs, stop the run and coordinate; this
  sheet never stops or starts one.** Phase 12 and every abort path only
  record their state.
- **R22-2: Phase 8 block 2a.** The driver line runs in the background (the
  shell tool's background mode). The operator then runs block 2a at once:
  a bounded wait for `READY` or the driver's end in `p8-bootstrap.jsonl`
  (`timeout 540 sh -c 'until grep -q READY "$1" || test -s "$2"; do sleep
  5; done'`, which greps a file and so can never match itself), then a
  bounded wait for `summary.json`, and goes straight on to the after-READY
  lines. A task notification is never what the operator waits for.
- **Run day and refresh:** the facts are of 2026-11-26 20:15:19Z, too
  late for that day's window. The run day starts with Phase 0's recheck
  and fresh Phase 1 facts after Rocky's nightly dnf has stamped (header,
  N1); a kernel split stops for Paul's maintenance-reboot approval, and
  the refresh must find every iormesh VM Stopped again (iom1 and iom2 were
  started after the facts, with the orchestrator's agreement).
- **Run ID `zq3-r23`** (same length as zq3-r22), native guard root
  `/var/tmp/hfs-20261126-zq3r23-fdq` (fill date 2026-11-26), private fill
  directory `/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill` (under
  the candidate's run root).
- **Expectations:**
  - through Phase 9 as in zq3-r22 (both mounts with `hfs_fdattr`, READY,
    `usable`, the Phase 9 OBSERVE), with Phase 9 starting within 5 minutes
    of READY (R22-2); Phase 4 step 1a passes for the new daemon
    `43238fe9…`;
  - **T1 should pass:** with km4 a child's `VIEW_RETIRE` refused while
    another process of the domain holds a lease is answered `BLOCKED`, so
    m0's cleanup worker moves on and releases the leases, the retirement is
    offered again (re4), and the CREATE's 6m4 retire-and-retry no longer
    waits behind a 30 s bound. sj4, tj4/wj4 and vj4 held in zq3-r21 and
    zq3-r22. Then **T2-T4, T6, T7, T9, T10, T11 and T8** within the guard
    budgets (none of T2-T11 has run for real); T3 adds the s64
    peer-rename guard; T4 `namespace` exercises link(2); T5 stays deferred;
  - **record per case:** the admission timings (`p10-admission.py`), the
    **recalls by trigger**, the **advance** and **self-admission** counts
    (`recalls.py`), **the named failing syscall** on any failure (yf4), and
    **every guest re4/1h4/4h4/tj4/s64/hh4/dh4/6m4/km4 log line**: `local
    retirement mutex busy`, the BLOCKED `re-check` lines, an advance
    adopted or released unadopted (`-EXDEV`), a not-yet-current grant
    re-queried, `view moved` / `acquiring again`, a host `BLOCKED`,
    release-and-retry, `retry limit reached`, an ABANDON, **every `retired
    under charge pressure` line (what the own retire and the other
    sessions freed, and whether the charge was retried or `nothing freed,
    the charge fails`)**, any `blocked by … failure` line (with its errno),
    any `WARNING`/`BUG`;
  - **any `ENOSPC` (R21-1/R22-1), `EACCES` (R20-1), `EAGAIN`,
    `EOPNOTSUPP`, `EKEYREJECTED`, `ESTALE` or `ETIMEDOUT` from a probe
    syscall is a finding, with its named syscall** (and the guest log lines
    around it), never a retry; a guest `blocked by uncertain control
    result on VIEW_RETIRE (-110)` line is R22-1 again, a finding against
    km4;
  - T9-T11 block sessions on purpose: hh4 (ABANDON of stranded
    never-prepared claims) and dh4 (DRAIN_WRITE/GAP_RESOLVE exempt from the
    poll halt) run for the first time there; record what the guest and
    host log;
  - Phase 11's unmounts should again return within seconds (R13-1 check).
- **Remaining known risks, not fixed:**
  - **km4's limits:** `hfs_retire` still retires only the oldest run of
    claims with one set of credentials per round; `hfs_retire_shared` does
    not offer view tickets; a family stays charged while another process of
    the mount holds a lease (the guest offers it again only through re4's
    kicks and the next cleanup packet). When nothing is retirable, a call
    still fails cleanly with `ENOSPC`: a finding;
  - **ue4:** release tickets still cannot retire during a recall; a host
    `BLOCKED` on a full ticket budget is a finding;
  - **the s64 description path** (`fstat`/`fchmod`/`fchown` through the
    open description, the peer-rename fallback): zq3-r21 and zq3-r22 ran
    T1's `fchown` and `fchmod` of `group` through it (0), and the rest runs
    for the first time; an errno from it is a finding against s64;
  - **a peer's observation in flight** across a creating CALL still starts
    an all-domain CALL_CONFLICT recall (vj4 exempts only the requester's
    own leases; `TestZQ3T1PeerConflictReplay`);
  - a re-acquire, re4/1h4 retry or 4h4 advance re-check that runs out of
    its budget returns an errno to the probe: a finding (with the guest log
    line), never a retry.
- **Kept:** every finding rule (`ETIMEDOUT`, `view control worker stopped`,
  `initial attributes timed out behind a recall`, block lines, an `ESTALE`,
  `EAGAIN`, `EKEYREJECTED` or `EOPNOTSUPP` from a probe syscall), the 984
  timing capture, the per-case admission-step timings and the oc4/pc4 errno
  records (`-112`/`-116`, `exporter ERROR <n>`).
- **Facts (2026-11-26 20:15:19Z, read-only, the baseline):**
  - f3 boot unchanged (`1794574308`, Paul's deliberate 2026-11-13 reboot);
    `vm list`: `rocky` Running (50982), `freebsd`, `hfs-test0..2` and
    **iom1, iom2, iom3 Stopped**; no iormesh k3s VM listed; `/dev/vmm`
    holds only `rocky`;
  - Rocky's bhyve PID `50982`, UTC lstart `Thu Nov 26 14:51:24 2026`
    (zq3-r22's Phase 12 start), boot ID
    `99de7fb2-c8e3-4b60-afef-7b11e77b4142` (copied from
    `rocky-phase1-20261126T201519Z.txt`), up 5:23, running the stock
    `5.14.0-741.27.1.el9_9.x86_64` = grub default = `saved_entry`,
    `next_entry` empty, Enforcing, no `/.autorelabel`, systemd `running`,
    `authorized_keys` `e08542dc…` (one line), python3.9 `729ac37f…`; no
    `hfs-xz3` file in `/boot`; `/etc/homelabfs` absent;
  - Rocky's clock and upgrade: `Europe/Sofia (EET, +0200)`, synchronized;
    stamp `2026-11-26` (00:59:03 EET); the newest dnf transaction is still
    **#68** (2026-11-26 00:55, 29 packages: the stock kernel `741.27.1` as
    the grub default, kernel-tools/headers, ruby, webkit2gtk3-jsc; the
    687.66.1 kernel removed; no pod runtime change); `needs-restarting -r`
    clean. The next run is 2026-11-27 00:55 EET = 2026-11-26 22:55 UTC;
    the latest Phase 5 start on 2026-11-26 was 21:25 UTC, so **the run is
    on a later day** (the gate recomputes the times; fresh Phase 1 facts
    first);
  - UEFI vars `4f63d71b…` (new: zq3-r22's Phase 12 start changed them),
    `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…`, f3
    `/usr/bin/ssh` `6a9e5573…` unchanged;
  - `10.20.0.133` on `vm-public` (members `tap0`, `re0`);
  - **`fdescfs.ko` (id 39) and `fusefs.ko` (id 40) loaded** (left by
    zq3-r22's window, no fuse/fdesc mount): Phase 1a runs;
  - the r23 staging, window and guard-root paths are absent; zq3-r22's
    f3 directories exist (evidence; leave them);
  - pod runtime (unchanged since dnf #67): podman `8d94a8fb…`
    `podman-6:5.8.5-5.el9_9.x86_64`, crun `659867c7…`
    `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…`, seccomp `7dd44119…`;
  - f3 memory (a record only, not a headroom judgement: the iormesh
    session runs its VMs at times): physmem 16,879,837,184, free 5.42 GB
    (1,324,113 pages), inactive 1.55 GB, active 0.24 GB, wired 9.24 GB,
    ARC 8.34 GB (`c_min` 0.53 GB), Rocky up 5.4 hours, **swap 0 used**;
    Phase 5's estimate and the Phase 6 bar decide at run time;
  - zroot ONLINE, 343G free; `/var/tmp/homelabfs-9p-poc/share` present.

## Decisions kept from the zq3-r7 to zq3-r22 run-sheet reviews

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
- **zq3-r19 execution:** R19-1 (an `EAGAIN` from the guest's BLOCKED retry
  limit; 1h4 is this candidate's fix); nothing in the procedure changed.
  New for zq3-r20: no Phase 0b (swap 0 at the facts), the advance count and
  recalls by trigger from `recalls.py`, and the 1h4/4h4 guest log lines as
  findings.
- **zq3-r20 execution:** R20-1 (an `EACCES` from a non-owner's walk below
  the 0700 root; sj4 is this candidate's fix) and the never-sent xf4
  advance (tj4/wj4); nothing in the procedure changed. New for zq3-r21:
  `hfs_fdattr` mounts and the T3 peer-rename guard, the `self_admissions`
  count from `recalls.py`, the refilled podman pins, the new workstation
  ssh pin, and iormesh VMs are never stopped or started by the sheet.
- **zq3-r21 execution:** R21-1 (an `ENOSPC` from a probe syscall after the
  guest's shared byte budget filled with retirable owners; 6m4 is this
  candidate's fix); nothing in the procedure changed. New for zq3-r22: no
  dnf run and no Rocky reboot between the run and the fill (Rocky runs
  zq3-r21's Phase 12 incarnation), the 6m4 log lines and `ENOSPC` as
  findings.
- **zq3-r22 execution:** R22-1 (an `ENOSPC` from T1's `create` after
  m0's cleanup worker blocked on an unanswered `VIEW_RETIRE`; km4 is this
  candidate's fix) and R22-2 (the operator waited for a task notification
  instead of the driver log). New for zq3-r23: Phase 8 block 2a (a bounded
  poll of `p8-bootstrap.jsonl` for READY, then `summary.json`; never a
  notification), Phase 4 step 1a required for the new daemon, the km4 log
  lines and a `VIEW_RETIRE (-110)` block as findings, and the run-day
  refresh of Phase 0/1 (the fill day's window had passed).

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

The fill's facts (2026-11-26 20:15:19Z = 22:15:19 EET,
`rocky-upgrade-20261126T201519Z.txt`): stamp `2026-11-26`, written
00:59:03 EET; the newest dnf transaction is still **#68** (2026-11-26 00:55,
29 packages, C/E/I/U): **the stock kernel `5.14.0-741.27.1.el9_9`**
(kernel, -core, -modules, -modules-core, -devel installed and made the
grub default; kernel-tools/-libs/-headers upgraded; the 687.66.1 kernel
removed), ruby 3.0.7-171 with its gems and webkit2gtk3-jsc
(`rocky-dnf68-20261126T201519Z.txt`). Rocky runs `5.14.0-741.27.1.el9_9` =
grub default = `saved_entry` = the kernel plan's stock default since
zq3-r22's Phase 12 start (14:51Z), and `needs-restarting -r` is clean.
The hourly ticks log "reboot deferred: standalone rocky requires explicit
maintenance approval" whenever a reboot is pending, as every tick does.
python3.9 is `729ac37f…`; podman (`8d94a8fb…`,
`podman-6:5.8.5-5.el9_9.x86_64`, from dnf #67 of 2026-11-25), crun, conmon
and seccomp.json hash as at the zq3-r21 and zq3-r22 fills, so the pod
template's pins equal zq3-r22's. The NEVRAs are pins too (e94). The journal also
shows one run at 10:35 EET, 11 minutes after zq3-r13's Phase 12 started
Rocky (10:24:51 EET): the timer's `OnBootSec` tick, which found today's
stamp and only logged the deferred reboot. Expect the same after this
run's Phase 12.

**Run-day refresh (2026-11-26 22:56:44Z = 2026-11-27 00:56:44 EET,
`rocky-upgrade-20261126T225644Z.txt`):** stamp `2026-11-27`, written
00:55:07 EET; the newest dnf transaction is **#69** (2026-11-27 00:55, 2
packages, Upgrade: `rsync-3.2.7-4.el9_9` and `libgs-9.54.0-20.el9_9.1`,
`rocky-dnf69-20261126T225644Z.txt`); `needs-restarting -r` clean. No
kernel, python3.9 or pod-runtime change (`rocky-grub-…`,
`rocky-pod-runtime-…` byte-identical to 20:15Z), so `n1-gate.py`'s
`FACTS_TOP` is now #69 (a record fix) and nothing else is refilled.

**The run must start after that day's run has stamped and finish before
the next one**, 00:55 EET on the next local day = **22:55 UTC on the run
day** (2026-11-27 22:55 UTC for a run on 2026-11-27; the fill day's window
had passed, so the run day first refreshes Phase 0 and Phase 1, header). If it does not, the
upgrade can replace the kernel, python3.9, podman or crun:
- Phase 1 records the stamp and the dnf history (`p1-upgrade.txt`,
  `n1-gate.py phase1`). A newest transaction other than #69 means a dnf run
  since the run-day refresh: record `dnf history info <ID>`, and Phase 1's
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
  on the run day** (2026-11-26 for a run that day);
- the Phase 1 stamp is not the local date of the last 00:55 (that day's
  run had not stamped at Phase 1, so it may still be pending; added in
  zq3-r13).

The fill tested the exact helper at fixed times (`--now`, which the run
never passes): 12:30Z GO, the real 20:15Z Phase 1 and upgrade records
GO at 20:30Z and 21:25Z and STOP at 21:26Z, 21:20Z GO, 21:25Z GO,
21:30Z STOP, 22:56Z STOP, 23:10Z STOP, the next day with the old Phase 1
(synthetic or real) STOP, the next day with a fresh Phase 1 and stamp GO, a
stale stamp STOP and 01:00Z (2 h after the upgrade) GO; `phase1` reports
`dnf_unchanged` true for the #68 facts and false for #67 and a synthetic
#69 (17 cases). The run-day refresh moved `FACTS_TOP` to #69 (`c354f923…`):
`phase1` reports `dnf_unchanged` true for the 22:56:44Z record (#69) and
false for the 20:15Z one (#68); `gate` on the 22:56:44Z records gives STOP
at 22:57Z (less than 35 min after 00:55, as expected that soon), GO at
2026-11-27 12:00Z and STOP at 21:26Z. If the run slips to a later day, nothing here is retyped:
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
source /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/session.sh
# ==== END Session-setup-workstation-bash block 1 ====
```

`session.sh` (`3e92efb9…` since the run-day refresh, [pins](pins.json) `fill_helpers`) defines, adapted
from zq3-r22's (a summary, not runnable as written; bash only):

```text
# ==== BEGIN Session-setup-workstation-bash block 2 (text: not a command, never run) ====
D=/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill   # private 0700 fill dir
S=$D/snapshot                                # git archive of f584de4 (docs internal tools)
R=/var/tmp/hfs-validation-xz3/zq3-r23        # f3 staging prefix (parent exists, holds zq3-r1 to zq3-r22)
G=/var/tmp/hfs-zq3-zq3-r23                   # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
okpin() { …observer_key.py cd7f38c2… check… }   # before every stdin invocation of observer_key.py
rockyt(){ …rocky with a local `timeout 270` around the real ssh, </dev/null; exit 124 = bound expired (read it with `; echo rc=${PIPESTATUS[0]}`)… }
hpin()  { …sha256sum -c of bin/sign_watch.py (11182a80…), the signer bin/hfs-retained9p-operator (d606a1cd…, rebuilt at f584de4 with go1.27.4) and the seventeen sh/* helpers… }
rockywait() { …Phase 12/U3: `rocky true` every 10 s until it answers, at most TRIES (default 36) tries; run it in bash that sourced session.sh (the bash-only wrapper is fine), never in a bare bash -c that did not source it… }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r23-kernel-stage/rocky-kernel-plan.json --plan-sha256 dda355cfba97e8a0ca11d89d2ab01a61e6d3802be8ca5d537a4c9a16c487fb3f --state /root/hfs-zq3-zq3-r23-kernel-state'
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261126-zq3r23-fdq/native-test-guard.py --root /var/tmp/hfs-20261126-zq3r23-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 50982 --rocky-birth 'Thu Nov 26 14:51:24 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
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
bash -c 'source /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/session.sh && { test ! -e /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/vars.env || source /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/vars.env; } && <the sheet line>'
# ==== END Session-setup-workstation-bash block 3 ====
```

(`vars.env` holds the `printf %q` lines, so sourcing it restores `PM`,
`F3_NOW`, `WP`, `W`, `EA`, `P`, `B`, `U`, `L`, `C`, `CTL`, `CR`, `OUT`,
`XP`, `ARM`, `RST`, `T0` and the guard PID.) A wait loop such as
`rockywait 36` runs the same way.

## D1: f3 module state (decided: D1-a, Phase 1a)

The 20:15Z facts show `fdescfs.ko` (id 39) and `fusefs.ko` (id 40) loaded on
f3, left by zq3-r22 (its Phase 4 loaded them, its window restore kept them,
and f3 has not rebooted since Paul's deliberate 2026-11-13 reboot), with no
fuse or fdesc mount and `/dev/fd` on devfs. The window plan filler
`fdescfs-window-plan.py` requires both absent (`fdescfs_module: absent`,
`modules_forbidden: [fdescfs.ko, fusefs.ko]`).

The fill checked this locally with the r23 pins:
- zq3-r8's real Phase 4 facts (modules absent after the reboot, paths renamed
  to zq3-r23, the three artifact hashes the r23 ones: the new daemon,
  helper and test binary) **fill**;
- the same facts with zq3-r22's artifact hashes (= zq3-r21's)
  **refuse** (`pinned artifact differs or missing`);
- the same facts with the r20 artifact hashes zq3-r20 staged, or
  with the xc4 ones, **refuse** (`pinned artifact differs or missing`);
- post-restore facts with both modules loaded, the state on f3 now,
  **refuse** (`fdescfs module present; plan is for absent`).

zq3-r3 to zq3-r7 and zq3-r9 to zq3-r22 all ran D1-a (a normal `kldunload` of
both, never refused), so this sheet runs it again as Phase 1a. D1-b (a
reviewed tool change) and D1-c (an f3 reboot) remain plan revisions, not
options at run time. Phase 4 loads `fdescfs` in `setup` and `fusefs` in
step 4, and both stay loaded after this run.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-26 by the fill:
- `hfs-runtime-prep` and the signer rebuilt at `f584de4` with go1.27.4
  (two identical builds each, see the [README](README.md));
- the bundle (the committed spec, unchanged: `ControlTickets` 24, pinning
  the exporter `8cadae40…`; new sessions);
- the argv record (from the 20:15Z launch facts, UEFI vars `4f63d71b…`,
  exporter `8cadae40…`);
- the kernel plan (r23 candidate `21c49a55…`: kernel `2435216f…`;
  stock default `741.27.1.el9_9`);
- the templates (T5 still deferred, r9 reason; be4 request `seconds` 240,
  plan `seconds` 255, lock-probe budgets 150/240; **`fd_attr` on in the
  batch template and the bootstrap plan**; the workstation ssh
  `d4c70b09…`; the e94 pod template `/2` with the runtime pins refilled
  from the facts (equal to zq3-r22's) and pl4's `pod_fixture.py` pin) and the filled bootstrap
  plan (F1);
- the staging trees and tarballs (the new kernel, daemon and window
  artifacts, `rocky_kernel.py`, kernel plan, argv record, volume-init and
  runtime config; the reused exporter; the be4 guest tools; pl4's
  `pod_fixture.py`);
- the r23 window pins (the committed pins at `f584de4`, which moved to the
  new daemon and window artifacts, renamed);
- the init helper (zq3-r1's reproduced `8c02b98f…`).

Local tests passed at `f584de4` (see [README](README.md#local-verification)).
**Run-day refresh:** the fill's facts are of 2026-11-26 20:15Z, after that
day's window had effectively passed. On the run day, after Rocky's nightly
dnf run has stamped (00:55 EET), run this block and then Phase 1 (fresh
facts) before anything else; the header lists what a difference means (a
kernel split stops for Paul's maintenance-reboot approval and a refill;
any iormesh VM still running waits). Re-verify immediately before Phase 1:

```bash
# ==== BEGIN Phase-0 block 1 (run each line on its own) ====
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 d19bc90202140d9c8b1fa84d89a79fad759b71e537137008752e93ab762d8422 --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 d19bc90202140d9c8b1fa84d89a79fad759b71e537137008752e93ab762d8422
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json \
  --plan-sha256 67c551f3194225b3c65e53310b85ae3d2419d7c24632e79a2e8a2ab1cba27ea0
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.SYNTHETIC-shapecheck.json   # cases_planned: the four, deferred: mapped-pressure
sha256sum /usr/bin/ssh                                   # d4c70b09… (Fedora 45; the batch template and the bootstrap plan pin it)
hpin && okpin && sha256sum -c <<'EOF'
be6585e729dd32a2bbbe1d6d598dfb58ec2cb9442e075260d3cf8fede8d990e7  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/stage/f3-stage.tar
29588b14ea9fb634a38c42a0bde55990f622134adf396186414a8e92c0797699  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/stage/rocky-stage.tar
dda355cfba97e8a0ca11d89d2ab01a61e6d3802be8ca5d537a4c9a16c487fb3f  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/kernel/rocky-kernel-plan.json
aa1ab40f3198a559758ca2b33e4c08a537b34d4087a8f023feef7061dcc126d1  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/controller/batch-plan.TEMPLATE.json
22b2c45b094550810b5ddf1521212810ae99c9a63f8b4ebe6d6642e4e746043a  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/fdescfs-pins/pins.json
3e92efb96e05bc29959b8a2021bda161001af407b5b127e6faa3ef8889d6d56b  /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/session.sh
EOF
cd $D
# ==== END Phase-0 block 1 ====
```

The tools snapshot `$S` is `f584de4`, the main HEAD at fill time; no refresh
is planned. If main moves before the run, `git diff --stat f584de4 $FINAL --
tools/ internal/controlledruntime/probe docs/lab/f3-host-known_hosts
docs/lab/rocky-known_hosts docs/lab/zq3-fdescfs-window` must be empty (or
touch only tests and docs); otherwise the changed pins make this a plan
revision. If a fill step is interrupted, a leftover `NEW.next` file makes
reruns refuse with "File exists": inspect it, then remove it by hand before
retrying.

**Operator key (before Phase 8, ideally now):** on Paul's instruction Claude
copies the operator signing key to
`/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/operator/operator-signing-key` (the
empty `0700` directory exists): a copy (one link), owned by `paul`, mode
`0600`, unencrypted Ed25519 PKCS#8 PEM or a 64-hex seed without newline,
matching public key `2f6d909c…`. The bundle's request template records
exactly this path; nothing in the fill reads it. A different path means
regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8, including
the Phase 1a unload and T7 (A8); T5 is deferred; N1: the run starts and ends before
Rocky's next nightly upgrade). Paul delegated both signatures (A7) to
Claude through the watcher in Phase 8.

## Phase 0b: no swap clearing this run

f3 swap is 0 used at the 2026-11-26 20:15Z facts, so zq3-r19's Phase 0b
(`swapoff -a` / `swapon -a`) does not run. Phase 1 checks the `swapinfo`
line: **0 used** continues; any swap in use stops before Phase 1a, with
nothing changed, and Paul decides (Phase 6 requires swap unused, because
`runtime-guard.py` admits no swap use).

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 2026-11-26 20:15Z facts ([facts/](facts/)) are the baseline. On the run
day this block is the fresh-facts refresh (header): it runs after that
day's nightly dnf run has stamped, and every check below compares against
the fill's facts. Immediately before Phase 1a:

```bash
# ==== BEGIN Phase-1 block 1 (run each line on its own) ====
hpin
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); f=d["files"]; print([f[p]["sha256"] for p in ("/zroot/bhyve/rocky/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run", "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", "/zroot/bhyve/rocky/uefi-vars.fd")], f["/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"]["link"], d["bridge_members"], d["vmm"], d["interfaces"])' $E/p1-launch-facts.json
f3 sh -s < $D/sh/p1-f3.sh > $E/p1-f3.txt
rocky 'date -u; timedatectl; cat /proc/sys/kernel/random/boot_id; uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /; ls /boot | grep -c hfs-xz3; ls -d /etc/homelabfs /root/hfs-zq3-zq3-r23-* /var/tmp/hfs-zq3-zq3-r23 2>&1; ls -l /.autorelabel 2>&1; grep -E "^SELINUX(TYPE)?=" /etc/selinux/config' > $E/p1-rocky.txt
rocky 'echo "== stamp"; cat /var/lib/unattended-upgrade/last-daily; echo "== dnf-history"; dnf history list | head -n 3; echo "== timer"; systemctl list-timers --all --no-pager unattended-upgrade-rocky.timer | sed -n 2p' > $E/p1-upgrade.txt
python3 -I -B $D/sh/n1-gate.py phase1 $E/p1-upgrade.txt | tee $E/p1-upgrade.json   # stamp = today (Europe/Sofia), dnf_unchanged: true (newest #69, run-day refresh)
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
  - Rocky's bhyve PID is `50982` with UTC lstart `Thu Nov 26 14:51:24 2026`;
  - freebsd/hfs-test0..2 **and every iormesh VM** (iom1, iom2, iom3 and
    the iormesh k3s test VM if `vm list` shows one) are Stopped and
    `/dev/vmm` = `rocky` (iom1 and iom2 were started after the fill's
    facts, with the orchestrator's agreement, for up to about 5 hours; they
    must be Stopped again). **If any iormesh VM runs, stop the run here**
    (nothing has changed yet) and coordinate with the iormesh session
    through Paul or the orchestrator: the window is scheduled while they are
    stopped. This sheet never stops or starts one;
  - swap: the `swapinfo` line shows **0 used** (20:15Z facts: 0 used; no
    Phase 0b this run). Swap in use here: stop before Phase 1a, with
    nothing changed, and ask Paul (Phase 6 requires swap unused, because
    `runtime-guard.py` admits no swap use);
  - **`fdescfs.ko` and `fusefs.ko` are both in `kldstat`** (20:15Z: ids 39
    and 40, left by zq3-r22), `no-fuse-fdesc-mounts`, and `/dev/fd` is on
    devfs. Phase 1a unloads them;
  - `/var/tmp/hfs-validation-xz3` and `/var/tmp/hfs-fdescfs-windows` are both
    `drwx------ root wheel`, and all three r23 paths are `absent`;
  - the route section shows `interface: vm-public`, and `ifconfig vm-public`
    lists `inet 10.20.0.133` with members `tap0` and `re0`. Record it. If
    `10.20.0.133` is not on `vm-public`, stop: the observer key's
    `from="10.20.0.133"` would no longer match f3's source address;
  - the PoC share is present and f3 `/usr/bin/ssh` is `6a9e5573…`.
- **Launch facts:** `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware
  `6b48e967…` (link `../edk2-bhyve/BHYVE_UEFI.fd`), **vars `4f63d71b…`**,
  bridge members `re0 tap0` (no `tap100`).
- **Rocky:**
  - boot ID `99de7fb2-c8e3-4b60-afef-7b11e77b4142`;
  - **the running kernel equals the grub default**, and both equal the
    kernel plan's stock default `5.14.0-741.27.1.el9_9.x86_64`
    (`grubby --default-kernel` prints
    `/boot/vmlinuz-5.14.0-741.27.1.el9_9.x86_64`, `saved_entry` names the
    same kernel), `next_entry=` empty (N1; zq3-r11's S1 showed how a
    nightly upgrade without a reboot splits them). A mismatch, or a boot ID
    other than the one above (for example after the nightly 00:55 `dnf -y
    upgrade` and a reboot), stops here;
  - `timedatectl` shows `Time zone: Europe/Sofia (EET, +0200)` and
    `System clock synchronized: yes`, so its daily `dnf -y upgrade` runs
    at 00:55 EET = 22:55 UTC (N1, R1). Another time zone stops here: the
    Phase 5 nightly gate computes in `Europe/Sofia`;
  - **`p1-upgrade.json`**: `stamp` is today's local date (Europe/Sofia;
    `2026-11-27` for a run on that day: today's run has stamped) and
    `dnf_unchanged: true` (newest transaction #69 of 2026-11-27 00:55, as in
    the run-day refresh). `dnf_unchanged: false` means a dnf run since the
    facts (likely on a later run day): record `rocky 'dnf history info
    <ID>'` in the run log; the pod-runtime and kernel checks below then
    decide. Any drift stops (refill; a kernel split needs Paul's
    maintenance-reboot approval first); no drift still means refreshing
    `n1-gate.py`'s expected transaction before Phase 5 (header). An older stamp means today's run has not happened yet: wait
    for it (the Phase 5 gate refuses that Phase 1 anyway) and redo Phase 1;
  - **`p1-nightly-gate.txt`: `nightly-gate: GO` and `rc=0`** (review round
    1: the Phase 5 gate run early, so a run that would span the upgrade
    stops before Phase 1a changes anything). `STOP` stops here with nothing
    changed on any host; Phase 5 step 0 still runs the gate again;
  - `Enforcing`, and `/etc/selinux/config` has `SELINUX=enforcing`;
  - **`/.autorelabel` absent** (`ls` reports "No such file or directory");
  - `authorized_keys` `e08542dc…` and python3.9 `729ac37f…`;
  - **`pod-runtime-unchanged: True`**: podman `8d94a8fb…`
    (`podman-6:5.8.5-5.el9_9.x86_64`), crun `659867c7…`
    (`crun-1.29.1-1.el9.x86_64`), conmon `1ac2a8db…` and seccomp.json
    `7dd44119…` equal the pod template's pins (N1). `False` stops here: a
    nightly upgrade replaced them, and T7 would refuse; refill;
  - no `hfs-xz3` file in `/boot` (zq3-r22's U2 removed its test kernel), and
    `/etc/homelabfs` and every r23 path are absent. The r1 to r22 leftovers
    (`/root/hfs-zq3-zq3-r{1..22}-*` and `/var/tmp/hfs-zq3-zq3-r{1..22}`) stay
    as evidence.

`boot_indeterminate=7` is expected. It stayed at 7 through every Rocky start
since zq3-r7, so it is not a start counter. The interface order in the
launch facts may differ (20:15Z: `re0 lo0 vm-public pflog0 vm-hfslab wg0
tap0`); only the set and the bridge members count. The f3 run directories now
include zq3-r22's (`/var/tmp/hfs-20261126-zq3r22-fdq` and the
zq3-r22 window and staging directories), the xd4 native build root
`/var/tmp/hfs-xz3-20261119-r14-r1` and the r21 native build root
`/var/tmp/hfs-xz3-20261125-r21-r1` (the r23 exporter is r21's; evidence;
leave them).

**`/.autorelabel` present: stop** before Phase 1a, with nothing changed. The
test kernel's boot under the exporter would find it, relabel and reboot the
guest in the middle of Phase 8. Report it: it means a boot without SELinux
happened since 20:15Z. **A fusefs or fdescfs mount, or a module state other
than both loaded: stop** (a plan revision decides). Any other difference also
stops and revises the plan. A changed vars hash means a new argv record.
Another f3 boot or Rocky restart means a new boot time, PID and lstart for
`NG`, `p5-recheck.py` and the sheet: **stop and refill** (fresh Phase 1
facts, new pins), never continue with this sheet. An f3 boot would also
unload both modules, which changes D1 and Phase 1a; that is part of the
refill, not a reason to skip Phase 1a here.

## Phase 1a: unload the modules zq3-r22 left loaded (A5, D1-a, about 2 min)

Right after Phase 1, before anything is staged. Each line on its own:

```bash
# ==== BEGIN Phase-1a block 1 (run each line on its own) ====
f3 'kldstat -v -n fusefs; kldstat -v -n fdescfs; mount -p | grep -E "fusefs|fdescfs" || echo no-fuse-fdesc-mounts' > $E/p1a-modules-before-unload.txt
f3 "doas -n kldunload fusefs"      # normal unload; "Device busy" or any error: stop, never -f
f3 "doas -n kldunload fdescfs"     # same
f3 'kldstat' > $E/p1a-modules-after-unload.txt   # neither fusefs.ko nor fdescfs.ko listed
# ==== END Phase-1a block 1 ====
```

Expect `Refs 1` for both files (`fusefs.ko` id 40, `fdescfs.ko` id 39) and
`no-fuse-fdesc-mounts` before the unload. After it, `kldstat` matches
zq3-r22's `p1a-modules-after-unload.txt` (`kernel`, `if_bridge.ko`, `vmm.ko`,
`nmdm.ko`, …), as after the zq3-r3 to zq3-r7 and zq3-r9 to zq3-r22 D1-a. Record both outputs in the run log. A
refused unload stops here: nothing is staged, Rocky runs, nothing else
changed (report; a plan revision decides). Then Phase 4 loads `fdescfs` in
`setup` and `fusefs` in step 4.

## Phase 2: stage on f3 (A2, about 3 min)

The parent `/var/tmp/hfs-validation-xz3` already exists (it holds zq3-r1 to
zq3-r22), so this checks it instead of requiring it absent. `paul` cannot look
inside a root `0700` directory, so absence is tested through `doas`. The
tarball has no entry for the parent, so extraction never rewrites its mode or
owner.

```bash
# ==== BEGIN Phase-2 block 1 (run each line on its own) ====
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-validation-xz3 /var/tmp/hfs-fdescfs-windows"   # both drwx------ root wheel
f3 "doas -n test ! -e $R && doas -n test ! -e /var/tmp/hfs-20261126-zq3r23-fdq && echo absent"   # must print absent
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261126-zq3r23-fdq > $E/p2-dirs.txt
# ==== END Phase-2 block 1 ====
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
NEW daemon `43238fe9…` (go1.27.4, km4, from `21d8478`; Phase 4 step 1a
qualifies it), the native exporter bhyve `8cadae40…` (s64 `_args` and the
hh4 ABANDON forwarder on top of 9h4; ran in zq3-r21 and zq3-r22),
`controlledruntime.test` (`d58fedc4…`) and `fdescfs-namespace`
(`6d54858c…`, both rebuilt with the daemon from `21d8478`), the G3 `homelabfs` helper (`0500`), the controllers and
`host.py`/`argv.py` (`940d4787…`, exporter pin `8cadae40…`)/`host_snapshot.py`/
`runtime-volume-init.py` and `runtime-guard.py` (`fece96b3…`, unchanged)
(`0400`), the argv record, the volume-init plan, the private runtime config
(holds host receipt keys; its `Exporter.SHA256` is `8cadae40…`, and Phase
8's `host-preflight` re-hashes this staged copy against the bootstrap
plan's `prep.config_sha256`, ol4) and the Rocky pin file; the native guard
(`576d802a…`, unchanged) and `runner_process.py` go to
`/var/tmp/hfs-20261126-zq3r23-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

One command per line:

```bash
# ==== BEGIN Phase-3 block 1 (run each line on its own) ====
rocky 'for d in /root/hfs-zq3-zq3-r23-kernel-stage /root/hfs-zq3-zq3-r23-kernel-state /root/hfs-zq3-zq3-r23-incoming /root/hfs-zq3-zq3-r23-pod /root/hfs-zq3-zq3-r23-tools /var/tmp/hfs-zq3-zq3-r23 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r23-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r23 --observer /root/hfs-zq3-zq3-r23-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r23-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 729ac37ffbf7f0ded4b08a9d49e95145c8e46717bf485f76a3675b27255054a8' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
PM=$(jf $E/p3-pod-manifest.json manifest_sha256); echo "$PM"   # review: expect 34a71569… (zq3-r15's to zq3-r22's real digest; no dnf run since zq3-r21; explain any other digest before build), then:
rocky "TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-pod/pod_fixture.py build --run-id zq3-r23 --expect-manifest-sha256 $PM" | tee $E/p3-pod-build.json
# ==== END Phase-3 block 1 ====
```

(Rocky's shell is bash, so `VAR=value` prefixes are fine there.) Expect:
install reports one new BLS entry (`<BLS_ENTRY_ID>`) for the r23
kernel (bzImage `049b1367…`, build ID `2435216f…`, System.map `1a80593f…`,
new bytes; config `692ed3fa…` unchanged, SELinux still in `CONFIG_LSM`;
release `6.1.190-hfs-xz3` and the five modules as in zq3-r13 to zq3-r22,
whose BLS entry was
`b26c0ac534534d7fb81b3e0698558bc3-6.1.190-hfs-xz3`; the same release gives
the same entry ID), default still the
plan's stock kernel `5.14.0-741.27.1.el9_9.x86_64` (N1), `saved_entry`
unchanged, `next_entry` empty (`install` never uses `--make-default`: the
custom kernel never becomes Rocky's default);
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. zq3-r12 to zq3-r14 got `523485cc…` (82
entries); be4's `pod_workload.py` (`c714370f`, 23,325 bytes) changed it,
and zq3-r15 to zq3-r22 got the predicted
**`34a71569f126ebeaa838a26788e75e1144b4d927228a4c9461940eb1fb2f46c4`**.
pl4 changed `pod_fixture.py` (`7c1e545f…`: the closure skips the exact
`/etc/ld.so.cache` mapping, and a merged-usr `/X` alias row attests a
`/usr/X` path only where no `/usr` row exists); its commits pin that
Rocky's closure gives the same table and manifest. `pod_workload.py` and
`pod_fixture.py` are unchanged and no dnf run happened since zq3-r21 (at the
fill's facts; the run-day Phase 1 rechecks it),
so expect that digest again. The rootfs is otherwise built from
Rocky's own installed files. If `PM` equals the prediction, record it and
run `build`; any other digest is not a failure but must be explained
(compare the entries with zq3-r22's `p3-pod-manifest.json`) before
`build`; record it. `PM` feeds the pod request in Phase 8. If Paul declines A8, skip
both pod lines and use `PM=skip` (T7 unrun).

**T7 runs (e94).** It has never run for real: zq3-r11 skipped it because
`pod_fixture.py` hard-coded the pre-EL9.9 podman/crun NEVRAs, zq3-r12
stopped in Phase 8, zq3-r13's matrix went unrun (operator pause) and
zq3-r14 to zq3-r22 stopped at T1 (R14-1 to R22-1). e94
moved those pins into the request: the pod template
(`homelabfs-retained9p-pod/2`)
pins `runtime_nevra` `podman-6:5.8.5-5.el9_9.x86_64` (dnf #67) and
`crun-1.29.1-1.el9.x86_64` and the four `runtime_sha256` values, all from
the 20:15:19Z facts, and `check_runtime` requires `rpm -qf` and the hashes
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
f3 'sha256 -r /usr/bin/ssh' | diff - $D/facts/f3-usr-bin-ssh-20261126T201519Z.sha256 && echo F1-unchanged
f3 'env TZ=UTC ps -p 50982 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
# ==== END Phase-4 block 1 ====
```

Expect `p4-python-libs.sha256` to list exactly the real paths
`/usr/local/lib/libpython3.12.so.1.0`, `/usr/local/lib/libintl.so.8.4.6`,
`/usr/lib/libdl.so.1`, `/lib/libutil.so.10`, `/lib/libm.so.5` (the closure of
zq3-r1 to zq3-r13; zq3-r8 to zq3-r13 recorded `libpython3.12.so.1.0`
`6fc7b9b0…` after the 2026-11-10 package update, and the window plan takes live hashes, so any
other library or a changed hash is a fact change to review),
`F1-unchanged` (otherwise stop: the filled bootstrap plan pins `6a9e5573…`,
and refilling F1 is a plan revision), and `Thu Nov 26 14:51:24 2026` with
`/dev/vmm` = `rocky` and neither module in `kldstat`.

Split the facts and fill the window plan on the workstation (B) with the r23
window pins (`22b2c45b…`: the committed pins at `f584de4`, with `zq3-r1` →
`zq3-r23`; daemon `43238fe9…`, helper `6d54858c…`, test `d58fedc4…`), then
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

F1 (bootstrap `observer.ssh_sha256`) was filled at fill time from the 20:15Z
fact (`fill_plan.py bootstrap`, plan `67c551f3…`, `bootstrap.py check`
passed); the `F1-unchanged` line above is its recheck. Nothing to fill here.

Step 1, negative before setup (B; a skip counts as unrun). No `--` before the
command; a retry takes a new stage name and keeps the refused one. The o54
host profile needs free ≥ 1 GiB and free + inactive ≥ 3.5 GiB (20:15Z, Rocky up 5.4 hours: 5.42 GB
free, 1.55 GB inactive):

```bash
# ==== BEGIN Phase-4 block 3 (run each line on its own) ====
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v" | tee $E/p4-unsupported-before.txt
# ==== END Phase-4 block 3 ====
```

Step 1a (B, **required**): process observer qualification of the exact
daemon bytes. The r23 package carries the NEW daemon `43238fe9…`
(go1.27.4, km4), so zq3-r21's and zq3-r22's checks of `c7b89648…`
(pids 89245 and 39889) do **not** carry over: the new bytes need
`--check-process-observer` on this run's staged copy under the bounded
host guard (the run plan's daemon row; xz3-20261126-r23 README). These
lines must pass before anything else changes on the host.

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
f3 "doas -n cat /var/tmp/hfs-20261126-zq3r23-fdq/checks/process-observer/stdout" > $E/p4-process-observer.json
f3 "doas -n cat /var/tmp/hfs-20261126-zq3r23-fdq/checks/process-observer/stderr" > $E/p4-process-observer.stderr
python3 -I -B $D/sh/p4-observer.py $E/p4-process-observer-guard.json $E/p4-process-observer.json $E/p4-process-observer.stderr | tee $E/p4-process-observer-check.json
# ==== END Phase-4 block 4 ====
```

Pass criteria (`p4-observer.py`, `"pass": true`, exit 0):
- the guard ran exactly `$R/bin/hfs-controlled-runtime
  --check-process-observer`, with `error` null and child exit 0;
- stderr is empty;
- stdout is one `homelabfs-process-observer/1` record with `platform`
  `freebsd/amd64`, `executable` the staged path, **`sha256` `43238fe9…`** and
  `publication` null;
- `uid` 0 and `gid` 0 (the gid among `groups`), a positive `pid`, an
  `incarnation` `freebsd:<pid>:…` and a positive `text_vnode`.

zq3-r5 to zq3-r22 passed exactly these criteria as root (uid/gid 0,
groups `[0, 0, 5]`; zq3-r22 for `c7b89648…`, pid 39889). zq3-r22's
real records, renamed to zq3-r23 and with `43238fe9…` substituted,
**pass** this check; renamed with their own daemon `c7b89648…` they fail
on the `sha256` only, and unrenamed on the staged path too.

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
f3 "doas -n test ! -e /var/tmp/hfs-fdescfs-windows/zq3-r23 && echo absent"   # must print absent
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 $WP --directory /var/tmp/hfs-fdescfs-windows/zq3-r23"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
# ==== END Phase-4 block 5 ====
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots (or a later
approved unload). If setup or qualify refuses and a reviewed fix follows,
resume as zq3-r1 did: keep the refused window directory under a new name
(`zq3-r23.refused-setup-1`, via `printf … | f3 doas -n sh -s`), and rerun
step 0 and the window plan fill with `-resume` names (`p4-*-resume.*`, `--out
$D/fdescfs-plan-resume`, remote `$R/records/fdescfs-plan-resume.json`, `WP`
and `W` pointing at it). The plan's expiry also forces this refill after a
pause of more than 55 minutes.

Step 5 (A6), run-only observer key:

```bash
# ==== BEGIN Phase-4 block 6 (run each line on its own) ====
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r23-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r23-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r23 --public-key /root/hfs-zq3-zq3-r23-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 729ac37ffbf7f0ded4b08a9d49e95145c8e46717bf485f76a3675b27255054a8 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
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
(`okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r23
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
f3 'env TZ=UTC ps -p 50982 -o lstart=' > $E/p5-rocky-lstart.txt  # expect: Thu Nov 26 14:51:24 2026
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime" > $E/p5-facts.json
f3 doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r23/owned.json > $E/p5-owned.json
python3 -I -B $D/sh/p5-recheck.py $E/p5-boottime.txt $E/p5-rocky-lstart.txt $E/p5-facts.json $E/p5-owned.json | tee $E/p5-recheck.json
f3 'doas -n vm list; swapinfo -k' | tee $E/p5-iom-swap.txt     # r23: every VM but rocky (iom1-3, any iormesh k3s VM) must show Stopped; swap 0 used (no Phase 0b this run; Phase 6 decides)
f3 'doas -n ls /dev/vmm' | tee $E/p5-vmm.txt     # r23: must list exactly rocky (catches a VM vm-bhyve does not manage)
# ==== END Phase-5 block 2 ====
```

r23: if any iormesh VM (iom1-3 or a k3s test VM) is not `Stopped`, or
`/dev/vmm` lists anything besides `rocky` (a bhyve VM that vm-bhyve does
not manage, such as an iormesh one, would not show in `vm list`), this is a
failed recheck: stop before step 2 (nothing is selected yet; abort as for a
failed step 1 below) and coordinate with the iormesh session; never stop or
start it from this sheet (the memory bar assumes them all stopped).

`p5-recheck.py` passes (exit 0, `"pass": true`) only if the boot time (also
the `boot` field of `facts`) and the UTC lstart equal the values above, and
the parsed `descriptor` of `facts` equals the parsed `owned.json` (a
supported `fdescfs` report with the owned FSID).

Step 1a, memory pre-estimate (B) and the disk-level reference:

```bash
# ==== BEGIN Phase-5 block 3 (run each line on its own) ====
f3 'doas -n vm list; swapinfo; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_active_count vm.stats.vm.v_laundry_count vm.stats.vm.v_wire_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min; ps -p 50982 -o rss=' > $E/p5-mem.txt
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
freed about 0.53 GB, Phase 6 margin 0.26 GB; zq3-r19: margin −1.28 GB
after its swap clearing (0.99 GB laundry), step 1b freed about 0.78 GB,
Phase 6 margin 0.37 GB; zq3-r20, with no swap clearing: margin
+0.26 GB, step 1b freed about 0.18 GB, Phase 6 margin 0.37 GB; zq3-r21:
margin +0.14 GB, step 1b freed about 0.23 GB, Phase 6 margin 0.29 GB;
zq3-r22: margin +0.15 GB, step 1b freed about 0.20 GB, Phase 6 margin
0.28 GB). At 20:15Z (Rocky up 5.4 hours, iom1-3 stopped) free was 5.42 GB,
inactive 1.55 GB, active 0.24 GB, wired 9.24 GB, ARC 8.34 GB (`c_min`
0.53 GB) and **no swap in use**. These are recorded facts only: the iormesh session runs
its VMs on f3 at times, so Phase 5 measures afresh and decides. `active_based` counts ARC above `c_min` and Active as well, so the
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
  /var/tmp/hfs-fdescfs-windows/zq3-r23"` and
  `f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r23" >
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
`swapinfo` line in `p6-admission.txt` shows 0 used; r23: swap was 0 at the
facts, no Phase 0b, see below); all VMs stopped (iom1,
iom2 and iom3 included)
and `no-vmm`; `/dev/fd` still the owned fdescfs mount
(`descriptor-equals-owned: True`). For reference, zq3-r3 measured
15,303,398,240 bytes here after its Phase 5 drain, zq3-r4 15,305,552,288
(margin 0.54 GB each), zq3-r5 15,294,364,056 (margin 0.53 GB), zq3-r6
15,329,786,840 (margin 0.57 GB), zq3-r7 about 15.33 GB (margin 0.57 GB) and
zq3-r8 15,283,237,008 (margin 0.52 GB), zq3-r9 15,342,496,000 (margin
0.58 GB), zq3-r10 15,338,803,152 (margin 0.57 GB), zq3-r11
15,264,448,296 (margin 0.50 GB), zq3-r12 15,267,994,272 (margin
0.50 GB), zq3-r13 15,251,768,616 (margin 0.49 GB), zq3-r20
15,135,932,016 (margin 0.37 GB), zq3-r21 15,056,814,296 (margin
0.29 GB) and zq3-r22 15,043,770,824 (margin 0.28 GB).
**r23 swap rule.** `runtime-guard.py` samples `swapinfo -k` and treats any
swap use above `--swap-limit-bytes` 0 as a breach (one SIGTERM), so a launch
with swap in use would end at the first sample. If the swapped pages were
Rocky's bhyve, stopping Rocky freed them and the line shows 0. If swap is
still in use here: do not launch. Restore the window (Phase 11 step 10),
remove the observer key (Phase 11 step 9's literal line on f3; on Rocky after
U3), then U3, and ask Paul: this sheet clears no swap (no Phase 0b this
run, and never here).

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
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 d19bc90202140d9c8b1fa84d89a79fad759b71e537137008752e93ab762d8422" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 b30ac43a03c98caf6aac8ee83ba763e1f9e684b7d683126668f62db2d3ba4c23 --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r23 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
# ==== END Phase-7 block 1 ====
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`4f63d71b…` (any Rocky boot since 20:15Z changes it; it was `0be7dec3…`
at the zq3-r22 fill, and Rocky's last start was zq3-r22's Phase 12
at 2026-11-26 14:51:24 UTC): stop;
regenerating the argv record from fresh facts (same sessions,
same args) is a plan revision for review, never an on-the-spot edit. Failure
with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3. `k04-baseline.json` is T11's
required baseline (the empty mount point before the runtime starts).

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s to READY plus about 100 s for `usable`, about 7 min with prep)

**In-run cutoff (zq3-r13 review round 1).** No new case or lifecycle step
(T1-T11, T8 included) starts after **22:15 UTC on the run day** (2026-11-26
for a run that day; 00:55 EET minus 40 minutes). At 22:15 UTC go straight to Phase 11 (the
guard-SIGTERM branch while the runtime serves), so that Phase 11 step 5's
guest poweroff is done **before 22:45 UTC**: Rocky's daily `dnf upgrade` at
22:55 UTC must never run against Rocky's disk while the test guest boots it
(the guest starts the same timer, and its stamp is the previous local day
after midnight). If step 5 has not run by 22:35 UTC, run step 3 (observer
removal) and step 5 at once, record the skipped evidence steps (4a, 4b) in
the run log, and continue with step 6.

Signing is delegated to Claude (A7, Paul's decision). The driver's stdout goes
through the watcher `bin/sign_watch.py` (`11182a80…`), which is zq3-r22's
watcher (it signed both requests in zq3-r12 to zq3-r22) with `D`, the
comments and the signer pin changed. It copies every
line through. For an `operator_action` line it runs the signer only if:
- the printed command is exactly one of the two commands below;
- the request file's SHA256 equals the printed `request_sha256`;
- the signer still hashes to `d606a1cd…`;
- that action has not run before (each runs at most once).

Anything else is logged as `"watcher": "refused"` and left unsigned, so the
driver times out (fails closed). The signer pin lives in two places, `hpin`
(`session.sh`) and the watcher's `SIGNER_SHA256`. The signer is rebuilt at
`f584de4` with go1.27.4; it links the km4-changed packages and its VCS
stamp changed, so zq3-r22's `4b8dd6d6…` no longer matches.
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

Run, **in the background** (the shell tool's background mode; never in
the foreground and never waiting for its completion notification, R22-2),
then go straight to block 2a:

```bash
# ==== BEGIN Phase-8 block 2 (run each line on its own) ====
python3 -I -B $S/tools/lab/retained9p/bootstrap.py run --plan $P \
  --plan-sha256 67c551f3194225b3c65e53310b85ae3d2419d7c24632e79a2e8a2ab1cba27ea0 --output $D/bootstrap-run \
  | python3 -I -B $D/bin/sign_watch.py | tee $E/p8-bootstrap.jsonl
# ==== END Phase-8 block 2 ====
```

**Block 2a, right after starting block 2 (R22-2): poll the driver's log,
never a notification.** Each wait line greps or tests a file, so it can
never match its own command line (no `pgrep -f`), and each has its own
bound; give each line a tool timeout of at least 600 s. The first line
returns as soon as `READY` is in `p8-bootstrap.jsonl` (the `ready` step's
record) or the driver has ended without it (`summary.json` written); the
second waits for the driver's end (`usable` takes at most about 100 s
after READY). Then run the after-READY lines below at once:

```bash
# ==== BEGIN Phase-8 block 2a (run each line on its own) ====
timeout 540 sh -c 'until grep -q READY "$1" || test -s "$2"; do sleep 5; done' sh $E/p8-bootstrap.jsonl $D/bootstrap-run/summary.json; echo "ready-wait rc=$?"   # rc 0: READY or the driver ended; rc 124: neither in 540 s, inspect the journal (never relaunch)
date -u +%Y-%m-%dT%H:%M:%SZ | tee $E/p8-ready-seen.txt
timeout 300 sh -c 'until test -s "$1"; do sleep 5; done' sh $D/bootstrap-run/summary.json; echo "summary-wait rc=$?"   # rc 0: the driver exited; rc 124: usable still running after 300 s, inspect
cat $D/bootstrap-run/summary.json   # outcome ready: the after-READY lines at once; anything else: the failure paths below
# ==== END Phase-8 block 2a ====
```

The plan is re-derived from zq3-r22's (the template that ran through
READY, `usable` and T1's dispatch). It pins:
- `runtime-guard.py` `fece96b3…` (unchanged);
- the NEW daemon `43238fe9…` (go1.27.4, km4; `runtime.sha256`, also the guard's
  `--runtime-sha256` below; Phase 4 step 1a qualifies it; the `usable` gate's
  `host-observe` runs it in client mode);
- the r23 kernel plan `dda355cf…` (kernel `2435216f…`, stock default
  `741.27.1`) and `rocky_kernel.py` `e99d91b8…`;
- `bootstrap_remote.py` `5155ff08…` (`remote_sha256`; the b84 `guest-stat`
  and `host-observe` actions, and since bl4 the `--fd-attr` mount option
  with its mountinfo rule);
- `hfs-runtime-prep` `63fa89c6…` (rebuilt at `f584de4` with go1.27.4). It
  fills the enrollment and bind requests with the r23 bundle's guest
  budget, which is unchanged (reservations 1024, received 256, 512/512 MiB);
- **`hfs_fdattr` (bl4/nl4, as in zq3-r21 and zq3-r22):** `fd_attr_candidate_sha256` `21c49a55…`
  (the `controller.FD_ATTR` entry), `fd_attr_candidate_manifest` (the
  package's `manifest.json`, read under that digest: its daemon must be
  `runtime.sha256` and its `freebsd/bhyve` the workstation runtime config's
  `Exporter.SHA256` `8cadae40…`) and `fd_attr_kernel_plan` (this fill's
  workstation copy of the kernel plan, read under `kernel.plan_sha256`, its
  `manifest_sha256` that same candidate). `load_plan` checks all three
  before any contact; `host-preflight` re-hashes the staged f3
  `runtime-config.json` against `prep.config_sha256` on every run (ol4).
  Both mounts then pass `--fd-attr on` and mount with
  `trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,hfs_fdattr,msize=65536`.

The driver itself is `bootstrap.py` `b8714120…` (journal `/3`, unchanged
since zq3-r9; unchanged since the zq3-r21 fill, which added the bl4/nl4
fd_attr checks and the ol4 config re-hash).
The driver launches, under the outer guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r23/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r23/guard --runtime-sha256 43238fe9953dbb3659c18b05b18f04d514d06ed600a47f9247f5e91f24bab255 --vm-name hfs-zq3-r23 --disk-root /var/tmp/hfs-validation-xz3/zq3-r23 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r23 --skip-path /var/tmp/hfs-validation-xz3/zq3-r23/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r23/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r23/runtime/runtime-config.json --disposable-validation
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
/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/signer-binding
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

What zq3-r22 got and what r23 changes:
- zq3-r22 reached READY with both mounts (mountinfo 391 and 408, the g64
  tuple with `hfs_fdattr` once, right after `cache=mmap`), both signatures
  by the watcher and the `usable` gate (L = 2); the exporter `8cadae40…`
  launched cleanly and `exporter.stderr` stayed empty.
- r23 changes the daemon (km4: a domain-wide `VIEW_RETIRE` refusal is
  answered `BLOCKED` at the unchanged floor instead of a never-answered
  `WouldBlock`) and the guest kernel (km4's log line for each 6m4
  retire-and-retry round; 6m4, s64, hh4, dh4, 5f4, kn1, wj4, tj4, 1h4,
  4h4, vf4, 2f4, re4 and ie4 stay). The daemon is `43238fe9…`; the
  exporter `8cadae40…` is zq3-r22's bytes. Both mounts carry
  `hfs_fdattr`. The `usable` gate's observations may be
  served by advances (the OBSERVE's
  `controller.recalls.advances` counts them); a guest `WARNING`/`BUG` line
  there is a finding to record.
  The oc4 behaviour stays in the new kernel: an exporter
  ERROR now reaches the guest as its own errno (ERROR 5 `-EHOSTDOWN`,
  ERROR 4 `-ESTALE`, others `-EPROTO`), block and worker-stop lines print
  `exporter ERROR <n>`, retries and polls stop once a session is blocked,
  every hfs 9p reply wait ends after 30 s (`ETIMEDOUT`), and the exporter
  answers ERROR 5 UNAVAILABLE when its host transport or endpoint check
  fails instead of sending no reply.
- **The exporter `8cadae40…` runs for the third time** (zq3-r21 and
  zq3-r22 launched it cleanly with an empty `exporter.stderr`; its
  predecessor `2fd7e32f…` ran cleanly in zq3-r14 to zq3-r20). A launch or admission refusal, an exporter exit,
  or any line in `guard/exporter.stderr` is a finding against s64, hh4 or
  9h4 (collect it with the failure evidence below); 9h4's controlled C
  bootstrap now waits out backend-mutex contention (`EAGAIN`, up to 10 s
  per call) instead of fencing itself.
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
  of the `claims` window. Record either (zq3-r10, zq3-r11, zq3-r13 to zq3-r22: none;
  zq3-r12: not reached).

**Expected:**
- both mounts complete, and each mount step's `mountinfo` shows
  `hfs_fdattr` exactly once, right after `cache=mmap` (bl4; expected super
  options `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`;
  otherwise `bootstrap_remote.py guest-mount` exits 2 `changed` with the
  mount kept, and the batch fill refuses);
- READY arrives well inside the 120 s bootstrap deadline;
- `usable` is `ok`: both roots answer both probes, the `baseline` OBSERVE
  shows no open claim, and the `claims` phase sees a TERMINAL GETATTR and
  PROCESS_EXIT on each provider after the baseline, and the `final` probe
  returns on both roots (zq3-r10, zq3-r11 and zq3-r13 to zq3-r22: passed, L = 2; zq3-r12: not reached). The summary says
  `ready` with every step `ok` and an integer `observe_sequence_last`;
- the guest logs no `hfs … blocked` line and no `QUERY timeout` during the
  gate (zq3-r9's R9-1 lines were `blocked by QUERY timeout on GETATTR
  (-110)`; zq3-r10, zq3-r11 and zq3-r13 to zq3-r22 logged none here), no `blocked by
  uncertain control result` line (R12-1), no `view control worker stopped`
  line and no `ETIMEDOUT`; `runtime.stderr` stays empty (no fence line). A request that meets a pending
  allocation waits for the fence (x84, at most about 30 s) and one that
  meets a view stuck in a recall fails `ETIMEDOUT` after 30 s (1a4), so a
  slower or failed gate is possible: the deadline above still bounds it,
  and a gate near its deadline is a finding to record;
- `guard/exporter.stderr` stays empty and the exporter keeps running (as
  in zq3-r21 and zq3-r22, and its predecessor in zq3-r14 to zq3-r20).

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
the guest budget (R21-1 or R22-1 again, a finding against 6m4/km4), an oops at a new kernel defect. If no guest kernel ran
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
/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/session.sh`), as zq3-r4 to zq3-r12
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
lost the whole matrix, and zq3-r22 started Phase 9 about 20 minutes after
READY because the operator waited for a task notification (R22-2); zq3-r14
started Phase 9 about 80 s after READY and zq3-r15 about 55 s: run
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
  --template-sha256 aa1ab40f3198a559758ca2b33e4c08a537b34d4087a8f023feef7061dcc126d1 \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 67c551f3194225b3c65e53310b85ae3d2419d7c24632e79a2e8a2ab1cba27ea0 \
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
  `hfs1` on `$G/m1`, each with
  `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`
  (g64 + bl4: `guest.py` `check_tuple` requires the base options and
  `hfs_fdattr` exactly once right after `cache=mmap`, because every batch
  request carries `"fd_attr": true`; `pod_fixture.py` checks the base
  options as a subset and accepts it);
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
  against g64 (a tuple without `hfs_fdattr` right after `cache=mmap`: one
  against bl4/s64); T1-T8 would refuse at admission, as in zq3-r7;
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
no-op median 0.329 s; zq3-r20: net median 0.56 s; zq3-r21: SSH no-op
median 0.223 s, net median 0.474 s, max 0.546 s; zq3-r22: SSH no-op
median 0.222 s, net median 0.447 s, max 0.596 s); this run adds more, and the line
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
fits easily (zq3-r14's T1 admission took about 17 s, zq3-r15's ran 23 s, zq3-r16's 16 s, zq3-r17's 31 s to its failure, zq3-r18's 7.3 s, zq3-r19's 8.1 s, zq3-r20's 7.6 s, zq3-r21's 8.6 s and zq3-r22's 10.0 s; each case is
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

**R11-1 check (y94; z94 merged since zq3-r15; 0a4 is in this candidate's daemon).** zq3-r11's T1 failed in
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
direct form (zq3-r19 ran it as written): zq3-r18 found that the `obs` helper refuses after a failed
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
  `EKEYREJECTED`, with 1h4 a busy retirement round should be waited for
  (`local retirement mutex busy`) and an expected-target BLOCKED
  re-checked, not an `EAGAIN`, and with 4h4/3h4 (and now tj4/wj4) an
  observation after the guest's own change should get an advance, not a
  recall, and with vj4 a creating CALL should be admitted past its own
  observation leases (`self_admissions`), not start a CALL_CONFLICT recall,
  and with 6m4 a call or view ticket whose charge fails should retire every
  session of the provider and retry once, not fail `ENOSPC`, and with km4 a
  child's `VIEW_RETIRE` refused while another process holds a lease should
  come back `BLOCKED` (offered again later), not block m0 on `VIEW_RETIRE
  (-110)`; record every `retired under charge pressure` line;
  `recalls.py` gives each recall's trigger, scope and phase durations;
- the failing syscall: yf4's `guest.py` names it in the failure record
  (`p10-admission.py`'s `failure`, the case's `stdout.log`); write it into
  the run log with its errno (an `EOPNOTSUPP` is R18-1 again, a finding
  against vf4; an `EAGAIN` with a guest `retry limit reached` line is
  R19-1 again, a finding against 1h4; an `EACCES` for a non-owner below
  the 0700 root is R20-1 again, a finding against sj4; an `ENOSPC` (with
  guest `blocked by … (-28)` lines) is R21-1 or R22-1 again, a finding
  against 6m4 and km4, and a guest `blocked by uncertain control result on
  VIEW_RETIRE (-110)` line is R22-1 again, a finding against km4;
  an errno from an fd-based call (`fstat`, `fchmod`, `fchown`) is a finding against s64);
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

r23 (ce4): the `recalls.py` line runs once after T1 whether T1 passed or
failed (after a failure, as the last line of the case-failure block). It
reads the one of those OBSERVEs whose `controller.recalls` has the largest
`total` (after a failure that is the block's fresh `obs-$CASE-fail`) and
prints one line per recall (trigger kind and opcode, requesting domain,
scope `ALL`/`DOMAIN`, outcome, phase durations, ACK waits); its first line
carries `advances`, vj4's `self_admissions` (new in r21; `null` while 0)
and `by_trigger`. Record the recall count, the recalls by trigger, the
advance and self-admission counts, scopes and the longest phase and ACK
wait in the run log next to T1's admission timing. The model replay
(`TestZQ3T1CredentialsReplay`, vj4 with advance) has 0 CALL_CONFLICT, 0
EXPECTED_TARGET and at most 2 RELEASE (DOMAIN) recalls, 7-10 self
admissions and 35-37 advances (T1 modelled 22 s, measured about 1.5 min).
A CALL_CONFLICT recall is a finding against vj4; an EXPECTED_TARGET recall,
or `advances` 0 or null (zq3-r20 read 0; zq3-r21 read 8 advances, 8 self
admissions and only the 2 RELEASE recalls, zq3-r22 7 and 6), one against
xf4 (tj4/wj4).
`history_found: false` is a finding (pre-ce4 bytes or no OBSERVE), never a
stop.

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

T3 with `hfs_fdattr` (bl4, s64; every batch request carries `"fd_attr":
true`): the `metadata` case adds the VM-only guard of the s64 rename
fallback. It keeps `peer-original` open on m0, renames it to
`peer-renamed` through m1 (the other session), then requires `stat` of the
old name through m0 to fail `ENOENT` (never the renamed file's
attributes), `fstat` of the fd to describe the same file, and the new name
to be it; `peer-renamed` stays as payload and is released with the rest.
This is the first VM run of the fallback (`test_fd_attr.py` checks only
the case logic on one local directory). A `stat(old name)` that succeeds,
or an `fstat` that fails (`ESTALE`, `EOPNOTSUPP`, `EACCES`) or describes
another file, is a finding against s64: record the named syscall and the
guest log lines around it.

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
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-tools/guest-lock-probe.py' < $E/lock-m0.json | tee $E/t6-lock.json  # tool timeout >= 300 s
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
rocky 'test ! -e /root/hfs-zq3-zq3-r23-pod/request.json && (umask 077; cat > /root/hfs-zq3-zq3-r23-pod/request.json)' < $E/pod-request.json
rocky 'sha256sum /root/hfs-zq3-zq3-r23-pod/request.json'   # must equal the next line
sha $E/pod-request.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r23-pod/request.json' | tee $E/t7-pod.json  # tool timeout >= 420 s
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
zq3-r14 to zq3-r22 stopped at T1. **Tool timeout for the `pod_fixture.py run` line:
at least 420 s** (be4: `RUN_SECONDS` 315).

T9, v24 lost reply with the d44 MKDIR filter (the daemon `43238fe9…`
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
ERROR <n>`: record it); never a pass. With hh4 the blocked m0 session
ABANDONs its stranded never-prepared claims (the host returns their credit
as gaps), and with dh4 its DRAIN_WRITE and GAP_RESOLVE are exempt from the
poll halt: both run for the first time here, so record what the guest and
host log (block lines, `runtime.stderr`, the next OBSERVE's claims). A controller step that fails or is
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
`8cadae40…` carries 564 (and e64, p64, w64), pc4, 9h4, s64 `_args` and the
hh4 ABANDON forwarder, so a "busy" recovery reply or a lost REFUSED in T10
is a new finding, not zq3-r7's known race. (zq3-r8 to zq3-r22 never
reached T10, so this exporter path is still unexercised on f3.) After a FENCED T10 the runtime exits and the exporter
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
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r23 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss | tee $E/t11-after-loss.json  # tool timeout >= 300 s
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r23 --path $R/runtime/mnt --label after" > $E/k04-after.json
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
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r23-tools/guest-lock-probe.py' < $E/lock-m1-t8.json | tee $E/t8-lock-refusal.json   # only if the unmount printed rc=0; tool timeout >= 300 s
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
  STOP (Phases 9-10) also lands here; zq3-r6 to zq3-r22 took this branch). The step 6 `child-gone` check also matches the guard's
  `--vm-name hfs-zq3-r23`, and `vms.sh start-rocky` refuses while the guard
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
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r23" > $E/f3-window-evidence.tar
# 1a. transport fence record (nc4; read-only): runtime stderr, the transport.db failure row (in f3-evidence.tar and the owner snapshots), Transport.Failure in every OBSERVE taken
f3 "doas -n cat $R/guard/runtime.stderr" > $E/p11-runtime-stderr.txt
python3 -I -B $D/sh/p11-fence.py $E/p11-runtime-stderr.txt $E/f3-evidence.tar $(ls $E/*owner*.tar $E/observe-p9*.json $E/obs-*.json $E/t9-*.json $E/t10-*.json $D/bootstrap-run/calls/*.json 2>/dev/null) | tee $E/p11-fence.json   # "fenced": false expected
# 1b. ce4 recall history (r23; read-only, findings only): the OBSERVE whose controller.recalls has the largest total (review round 1: a RESTARTED T10 starts an empty history)
python3 -I -B $D/sh/recalls.py $E/recalls-p11.json $(ls -tr $E/observe-p9*.json $E/obs-*.json $E/t9-*.json $E/t10-*.json 2>/dev/null) | tee $E/recalls-p11.txt
# 2. guest 9p mounts: list, then unmount each listed one on its own line, reading its rc before the next line (block lines captured before and after)
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/p11-block-before-umount.txt
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"
T0=$(date +%s); rockyt "umount $G/m0" | tee $E/p11-umount-m0.txt; echo rc=${PIPESTATUS[0]} seconds=$(( $(date +%s) - T0 ))   # only if m0 is listed; R13-1 check: expect about 30-60 s at most; rc=124 (timeout): record, continue
T0=$(date +%s); rockyt "umount $G/m1" | tee $E/p11-umount-m1.txt; echo rc=${PIPESTATUS[0]} seconds=$(( $(date +%s) - T0 ))   # only if m1 is listed; same
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"   # count 0 expected; see below if not
rocky 'dmesg | grep -E "hfs.*blocked|view control worker stopped|timed out behind a recall" || echo no-hfs-block' | tee $E/p11-block-after-umount.txt   # a new line here after a NORMAL unmount is an R10-2 (z84) or 1a4 finding
# 3. observer removal (banner timeout: check authorized_keys is still $EA before the one retry)
okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r23 --expect-sha256 $EA" < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
# 4. grub check
rocky "grub2-editenv - list"                     # next_entry empty; if set: rocky "$K cancel $KP" (U1) on its own line
# 4a. SELinux denials in the guest (evidence only; "<no matches>" with rc=1 is the good case)
rockyt "ausearch -m AVC,USER_AVC,SELINUX_ERR -ts boot -i; dmesg | grep -i -e avc: -e selinux" > $E/p11-guest-avc.txt 2>&1; echo rc=$?
# 4b. full guest kernel log (evidence only: keeps the m64 `9p-hfs: session … blocked` lines)
rockyt "dmesg" > $E/p11-guest-dmesg.txt 2>&1; echo rc=$?
# 5. guest poweroff
rocky 'systemctl poweroff'
# 6. wait for the child: this one bounded loop (pattern [h]fs-zq3-r23, never another run's; zq3-r14 slip) checks every 10 s, at most 90 times (15 min), matching the EXACT line child-gone with grep -x (zq3-r18 slip); tool timeout >= 1200 s; h64: the vmm context stays until step 7
gone=no; for i in $(seq 90); do if f3 'ps -ax -ww | grep "[h]fs-zq3-r23" || echo child-gone' | grep -qx child-gone; then gone=yes; break; fi; sleep 10; done; echo "child-wait gone=$gone tries=$i $(date -u +%H:%M:%SZ)"   # gone=yes required; gone=no: stop, no forced power-off
# 7. host cleanup (tap, VM context, vars copy): run exactly ONE of the next two lines; the --child-pid line is the normal case
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 d19bc90202140d9c8b1fa84d89a79fad759b71e537137008752e93ab762d8422 --child-pid $XP" | tee $E/p11-host-cleanup.json   # NORMAL: when step 0 set XP to a PID
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 d19bc90202140d9c8b1fa84d89a79fad759b71e537137008752e93ab762d8422 --no-child" | tee $E/p11-host-cleanup.json   # ONLY if the exporter is None (step 0 printed None and XP is unset); never as a first try (zq3-r18 slip)
# 8. native FUSE mount: check, then unmount if listed
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount" | tee $E/p11-native-check.txt
f3 "doas -n umount $R/runtime/mnt"               # only if step 8's check listed it; busy or error: stop and preserve
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount"   # must print no-native-mount before step 10
# 9. observer key on f3: this exact plain line, literal paths, never inside a script (zq3-r5); in bash, never zsh (zq3-r13)
f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r23/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r23/observer/id_ed25519.pub"
f3 "doas -n ls -la $R/observer"                  # only rocky-known_hosts left
# 10. window restore, always last; it prints nothing on success
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
f3 "doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r23/restoration-summary.json" | tee $E/p11-restoration-summary.json   # the restore's real outcome
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

Step 2 after a host stop (zq3-r13's **R13-1 check**; oc4 and pc4; zq3-r14 to zq3-r22:
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
`/dev/vmm/hfs-zq3-r23` is still there after the child exits; step 7's
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
rocky "$K check --expect stock $KP; getenforce; sha256sum /root/.ssh/authorized_keys; free -m"   # check --expect stock: running = grub default = 5.14.0-741.27.1.el9_9 (never 6.1.190-hfs-xz3); authorized_keys must be e08542dc…
rocky 'ls -l /.autorelabel 2>&1; cat /proc/sys/kernel/random/boot_id; journalctl --list-boots --no-pager | tail -n 4' | tee $E/p12-relabel.txt
f3 'doas -n tail -n 30 /zroot/bhyve/rocky/vm-bhyve.log' > $E/p12-vm-bhyve-log.txt
f3 'env TZ=UTC ps -ax -o pid= -o lstart= -o command= | grep "[b]hyve: rocky"' | tee $E/p12-rocky-pid.txt
rocky "$K remove $KP" | tee $E/p12-kernel-remove.json   # U2 (Paul chose removal)
f3 'doas -n vm list; doas -n ls /dev/vmm; ifconfig -l; mount -p | grep -E "fdescfs|fusefs" || echo none; kldstat; zpool list zroot' > $E/p12-final.txt
# ==== END Phase-12 block 1 ====
```

r23: after Rocky is back and checked (the lines above, U2 included),
record the state of the iormesh VMs (iom1-3 and any k3s test VM). **The run
never starts or stops them** (the iormesh session starts and stops them
one at a time; the window ran while they were stopped):

```bash
# ==== BEGIN Phase-12 block 2 (run each line on its own) ====
f3 'doas -n vm list' | tee $E/p12-iom.txt     # every iormesh VM should still show Stopped; one running: record it and tell Paul, never stop or start it here
# ==== END Phase-12 block 2 ====
```

After the run, the orchestrator (or Claude) tells the iormesh session that
zq3-r23 is over and f3 is free (the run log records the time).

**Relabel record (SELinux gate, R6-3).** Earlier runs rebooted Rocky about
5 minutes after `start-rocky`. Each test kernel had booted without SELinux
and left `/.autorelabel` behind, so the stock boot relabelled and rebooted
once. That reboot shows as a vm-bhyve "bhyve exited with status 0" followed
by "starting bhyve (run 2)". That vm-bhyve line is the deciding evidence,
because the `/.autorelabel` check below only runs after any relabel reboot. The oc4 test kernel (the unchanged c64 config) boots with
SELinux enforcing, so **no relabel reboot is expected** (zq3-r7 to zq3-r22: none). Write the result into the
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
for the next fill. The stock boot is `741.27.1.el9_9`, Rocky's grub
default and `saved_entry` (an ordinary stock boot, as zq3-r22's Phase 12
start was): **the restored kernel must be the stock `5.14.0-741.27.1.el9_9`, and
the custom `6.1.190-hfs-xz3` never becomes Rocky's default** (it is only
ever the one-shot `next_entry`, consumed by the Phase 8 boot and verified
empty in Phase 11; U2 then removes it). Expect Rocky's upgrade timer to tick about 10 minutes
after the start (N1); with today's stamp it only logs the deferred
reboot.

Staging prefix deletion is a separate, later step after the evidence is safe
and `mount -p` shows nothing below `/var/tmp/hfs-validation-xz3`. Left on f3
as evidence:
- `$R`, `/var/tmp/hfs-20261126-zq3r23-fdq` and
  `/var/tmp/hfs-fdescfs-windows/zq3-r23`;
- the zq3-r1 to zq3-r22 directories.

Left on Rocky unless Paul decides otherwise:
- `/root/hfs-zq3-zq3-r23-*` and `$G` (retention records, pod rootfs and
  evidence);
- the kernel state journal;
- the zq3-r1 to zq3-r22 leftovers.

fdescfs and fusefs stay loaded on f3 after this run (setup and step 4 load
them), so a later run in the same f3 boot meets D1 again.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r23`): `$S/tools/lab/vms.sh --approved-action start-rocky`
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
- **iormesh VMs on every stop path.** The run never starts or stops
  iom1-3 or the iormesh k3s test VM (the iormesh session owns them). Any
  abort (Phases 1-4, a Phase 6 refusal through U3, a hung test kernel)
  ends with the Phase 12 check `f3 'doas -n vm list'` (record their state;
  a running one is recorded and reported, never touched), and the
  orchestrator or Claude then tells the iormesh session that f3 is free.

## Run log template

`$E/RUN-LOG.md` (private) gets one line per phase or decision, with the UTC
time, as in zq3-r22. The lines new or changed for zq3-r23:

```text
# ==== BEGIN Run-log-template block 1 (text: not a command, never run) ====
- <UTC> Session: every line in bash through the wrapper (session.sh + vars.env); zsh never used: yes|no.
- <UTC> N1: Rocky clock Europe/Sofia (timedatectl, R1): yes|no; stamp .. and dnf newest #.. (p1-upgrade.json, dnf_unchanged True|False); Phase 5 nightly-gate GO|STOP at ..Z; Phase 1 pod runtime (p1-pod-runtime.txt) unchanged: True|False; kernel facts unchanged: yes|no.
- <UTC> Phase 1 passed: ... fusefs.ko (id ..) and fdescfs.ko (id ..) loaded, no fuse/fdesc mount, inet 10.20.0.133 on vm-public, vars .., swap 0 used yes|no, Rocky PID 50982 running .. = default .. (741.27.1)
- <UTC> Phase 1a: before-unload Refs 1/1, no-fuse-fdesc-mounts; kldunload fusefs rc .., kldunload fdescfs rc ..; after-unload kldstat lists neither.
- <UTC> Phase 3: kernel install BLS .. for build ID 2435216f (bzImage 049b1367); pod manifest PM=.. (expected 34a71569 as in zq3-r15 to zq3-r22; any other digest explained ..); pod build ..
- <UTC> Phase 4 step 1a process observer (the NEW km4 go1.27.4 daemon): PASS|FAIL for 43238fe9 (uid/gid .., groups .., pid ..).
- <UTC> Phase 8: launch ..Z (journal launch intent); exporter 8cadae40 (third run): exporter.stderr empty|..; mount-0 ok|.., mount-1 ok|.., hfs_fdattr once right after cache=mmap on both rows yes|no; transport fence (p8-fence.json / runtime.stderr): none|Site .. Cells .. Class .. Error ..; READY .. s after launch; usable ok|failed: first .., baseline .., final .., claims .., observe_sequence_last L=..; usable_note: none|..; guest block / worker-stopped / ETIMEDOUT lines (with errno and exporter ERROR n): none|..
- <UTC> Phase 8 block 2a: driver started in the background at ..Z; ready-wait rc .. at ..Z (p8-ready-seen.txt); summary-wait rc ..; outcome ..; no task notification waited for: yes|no (R22-2 check).
- <UTC> Phase 8 after READY: B=.., U=.., L=.., PM=.. (appended to vars.env); minutes since READY ..
- <UTC> Guard budget: launch ..Z, guard end ..Z; Phase 9 gbudget GO|STOP (late_after_ready ..); per step: T1 .., T2 .., T3 .., T4 .., T6 .., T7 .., T9 .., T10 .., T8 ..|n/a (FENCED); any STOP: steps unrun (guard budget) ..
- <UTC> Phase 9: OBSERVE sequence L+1=.. pass|fail; claims Open per provider ..; mountinfo count ..; Enforcing ..; no-hfs-block ..; obs-timing.tsv lines .. (3 ssh-noop + 1 observe).
- <UTC> 984 timing: ssh-noop median .. s; OBSERVE net median .. s, max .. s over .. samples (after T4: ..); net_max_below_984_bound true|false.
- <UTC> o84 contention: slow OBSERVEs or long-open claims none|..
- <UTC> T1..T4: sequences (L+2, L+3) .. (L+8, L+9) as expected|differences ..; syscall_batch_success ..
- <UTC> admission steps (p10-admission.py): credentials .. ms (largest gap .. at ..), sync .., metadata .., namespace ..; deadline_hit none|..; etimedout none|..; dispatch error/uncertain none|..
- <UTC> z94 check: recalls during each case's admission (fence/domain-sequence steps between baseline and retained OBSERVE, owner snapshots): credentials ..|none, sync .., metadata .., namespace ..; open-root gap ..
- <UTC> 1h4/4h4/vf4/2f4/ie4/re4 check: guest log lines (local retirement mutex busy / BLOCKED re-check / advance adopted / advance released unadopted -EXDEV / not-yet-current grant waited for the fence and re-queried / passed grant released / view moved / acquiring again / host BLOCKED / released-and-retried acquire / retirement re-send / retry exhausted / re-issued GETATTR or LOOKUP / any WARNING or BUG) none|..; ENOSPC, EAGAIN, ESTALE, EKEYREJECTED, ETIMEDOUT or EOPNOTSUPP from a probe syscall none|.. (failing syscall named by guest.py: ..); T1 m0 and m1 root-admitted yes|no; T1 fchown passed yes|no (R18-1 check); T4 link(2) passed yes|no (vf4 d_drop check); T1 access child below the 0700 root passed yes|no (R20-1/sj4 check); T1 create/fchmod of group-tail passed yes|no and guest `blocked by … (-28)` lines none|.. (R21-1/6m4 check); guest `blocked by uncertain control result on VIEW_RETIRE` lines none|.. (R22-1/km4 check); `retired under charge pressure` lines none|.. (own retire freed .., others freed .., retried yes|no); T3 hfs_fdattr peer rename: stat(old name) ENOENT and fstat same file yes|no (s64)
- <UTC> ce4 recalls (recalls.py): after T1 total .., advances .. (3h4), self_admissions .. (vj4), by_trigger .. (scopes ALL ../DOMAIN ..; longest phase .. us, longest ACK wait .. us); Phase 11 total .., advances ..; history_found true|false.
- <UTC> iormesh VMs (iom1-3, any k3s test VM): all Stopped at Phase 1 yes|no, Phase 5 step 1 yes|no, Phase 12 yes|no (never started or stopped by the run); iormesh session told at ..Z.
- <UTC> swap (no Phase 0b this run): Phase 1 .. KiB used (must be 0, else stop and ask Paul), Phase 5 .. KiB, Phase 6 .. KiB (must be 0 to launch).
- <UTC> Case failure (if any): CASE=.., failing syscall (yf4 name) .. errno ..; fresh OBSERVE sequence <next>=.. (rule: L+3 after T1's baseline, ..) status ..; ENOSPC/EOPNOTSUPP/EAGAIN/ESTALE/EKEYREJECTED/ETIMEDOUT none|..; guest WARNING/BUG lines none|..
- <UTC> Tool timeouts used: $CTL lines >= 300 s, T6/T8 lock probe >= 300 s, T7 pod_fixture run >= 420 s: yes|no.
- <UTC> oc4 guest lines: errnos seen (-112 EHOSTDOWN / -116 ESTALE / -71 EPROTO) ..; exporter ERROR n in block/worker lines ..; transport_failed signs ((-112), exporter ERROR 5 or ETIMEDOUT after 30 s) none|..
- <UTC> ETIMEDOUT scan (p11-etimedout-scan.jsonl, releases and T6-T11): none|..; R11-1 check: no early-DRAINED STALE / worker-stopped line|..; z94 recalls visible as open-root gaps: ..; R10-2/1a4 check: T8 unmount rc .. seconds .., new block/worker-stopped/timed-out lines none|..
- <UTC> Transport fence record (p11-fence.json, Phase 11 step 1a): fenced false|true: Site .., Cells .., Class .., Error .., Time ..; runtime.stderr line ..; transport.db row ..; restore refusal (if a restart followed) ..
- <UTC> T5 unrun (deferred, re-plan pending).
- <UTC> T7: pod_fixture run t7_pass ..|refused at check_runtime (N1 drift)|..
- <UTC> T9 pre-arm L+10, ARM L+11, post-CALL L+12; classification ..
- <UTC> T10 post-quiesce L+13, RESTART L+14; status ..
- <UTC> Phase 11 step 2 (R13-1 check): umount m0 rc .. seconds .., umount m1 rc .. seconds ..; mountinfo after ..; poweroff to child-gone .. s (step 6 line as written, pattern [h]fs-zq3-r23).
- <UTC> Phase 12: start-rocky ..; rockywait: rocky-ssh-up try .. at ..Z (in the session shell); vm-bhyve runs ..; relabel none|..
# ==== END Run-log-template block 1 ====
```

**zq3-r15 review round 1 (2026-11-19), kept:** tool-timeout comments on every $CTL, lock-probe (>= 300 s) and T7 pod run (>= 420 s) line; the D1 module ids (now 39/40, from the 20:15Z facts); the facts time in EET.

**zq3-r16 review round 1 (2026-11-20), kept:** the zq3-r14 note's boot ID (`b4d9ee31`), the git-diff scope wording, a tool-timeout comment on `$CTL status`.

**zq3-r17 review round 1 (2026-11-20), kept:** the zq3-r5 execution notes' literal key-deletion line and restore-summary paths name the current run ID (zq3-r23), and so does the wait-helper note.

**zq3-r18 review round 1 (2026-11-21), kept:** the case-failure block takes one fresh OBSERVE while the runtime still serves (now in the yf4 direct form, see the zq3-r18 run notes); `recalls.py` picks the history with the largest `total`; iom handling on every stop path (now: check that iom1-3 are still Stopped, never start them).

**zq3-r19 review round 1 (2026-11-21), kept:** the case-failure OBSERVE takes its sequence from a separate persisted `N=$((L+3))` line (edited for other cases); the T1 `recalls.py` line runs only after a passed T1; (Phase 0b's iom stop rule is moot: no Phase 0b this run).

**zq3-r20 review round 1 (2026-11-22), kept:** the Phase 4 F1 recheck diffs against the current facts file, and the Phase 5 memory pre-estimate reads the current Rocky PID.

**zq3-r21 review:** none before its run.

**zq3-r22 review:** none before its run.

**zq3-r23 review:** pending.
