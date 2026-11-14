# zq3-r8 run sheet (filled 2026-11-14)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](facts/) · [zq3-r7 record](../20261110-zq3-r7/README.md) ·
[zq3-r7 sheet](../20261110-zq3-r7-filled-plan/run-sheet.md)

Ordered commands for run `zq3-r8`, the eighth attempt of zq3, with the values
filled on 2026-11-14 from `main` `5a35150` (after y64 and z64). The
candidate is the y64 rebuild `867ede36…`, which rebuilt all three
components:
- the g64+m64 guest kernel: bzImage `40afad22…`, build ID `870bb9d1…`, hfs
  `.show_options`, an exact `cache=mmap`, a Sources floor and a log line for
  every fault block;
- the i64..x64 daemon `34fb05b2…`;
- the 564/e64/p64/w64 native exporter `ef8f17de…`.

**T5 `mapped-pressure` is deferred** (q64, z64 mechanism). The batch plan
declares the deferral and runs the other four cases. zq3 acceptance
therefore stays incomplete by design.
**This sheet grants no authority.**
Class **A** steps are critical and run only after Paul's explicit final GO on
this filled plan (A1-A8 of the run plan). Class **B** steps are routine
bounded verification under the 2026-11-02 standing approval, but only inside
an approved A window. `PAUL` marks a point where Paul himself must act.
Durations are estimates.

Generic failure rule (every phase): stop dependent work, collect evidence, never
force anything (no `vm poweroff`, no `kill -9`, no forced unmount, no recursive
delete through a mount), preserve failed resources, report. Values written
`<…>` or held in a shell variable set by an earlier line are run-time fills
(see [README](README.md#run-time-fills)); everything else is exact.

## Rules that apply throughout

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
  prepares locally and makes exactly one f3 call.
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

## Execution notes (carried over from zq3-r3 to zq3-r8)

- **D1-a (Phase 1a) is not needed in zq3-r8.** Paul rebooted f3 on
  2026-11-13, so `fdescfs.ko` and `fusefs.ko` are absent (07:57Z facts).
  Phase 1 checks that both are still absent, and Phase 1a is skipped with
  that recorded reason. If Phase 1 finds either module loaded, something
  loaded it after 07:57Z: that is a fact change, so stop and report. The r7
  Phase 1a lines (a normal `kldunload`, never `-f`) would then need a plan
  revision.
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
  $G/m0"` and `rockyt "umount $G/m1"` (session.sh `rockyt`: `timeout 150`
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
  f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519.pub"
  ```

- **The window restore's outcome is `restoration-summary.json`, not an exit
  code.** `fdescfs-window.py restore` prints nothing on success (zq3-r5's
  `p11-window-restore.txt` is empty). An `rc` reported through `| tee` or any
  other output filter is the filter's status, not the restore's. Phase 11
  step 10 therefore reads
  `/var/tmp/hfs-fdescfs-windows/zq3-r8/restoration-summary.json`: the restore
  succeeded only if it exists with `"mount_restored": true`.
- **Rocky's first boot after `start-rocky` rebooted itself after about
  5 minutes** in zq3-r1 to zq3-r6. The zq3-r6 record explains it (R6-3): a
  test boot without SELinux leaves `/.autorelabel`, so the next stock boot
  relabels and reboots once. zq3-r7, the first run whose test kernel had
  SELinux, needed no relabel reboot (one vm-bhyve run, SSH after 26 s).

## Execution notes from the zq3-r6 run (2026-11-10)

These notes are written into the lines they name:

- **`rockyt … | tee`:** the line ends with `; echo rc=${PIPESTATUS[0]}`, which
  prints rockyt's own exit status (124 = bound expired), not tee's (Phase 10
  T8, Phase 11 step 2).
- **Window restore:** judged only by
  `/var/tmp/hfs-fdescfs-windows/zq3-r8/restoration-summary.json` (Phase 11
  step 10), as in the zq3-r5 notes. zq3-r6 and zq3-r7 both restored with
  `"mount_restored": true`.
- **f3 observer key deletion:** the plain literal-path line of Phase 11
  step 9, which the agent safety check accepted in zq3-r6 and zq3-r7.

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

## New for zq3-r8

- **Candidate:** the y64 rebuild `867ede36…` (source `b869710`, all 194
  manifest entries checked).
  - **Guest kernel (g64+m64):** bzImage `40afad22…`, System.map `1ffea83f…`,
    GNU build ID `870bb9d1…`. The config `28fe4a8e…` (SELinux in
    `CONFIG_LSM`), the five modules and `modules.*` are byte-identical to
    `66b5bb2b`.
  - **Daemon (i64, o64, r64, n64, j64, u64, v64, x64):** `34fb05b2…`.
  - **Native exporter (564, e64, p64, w64):** `ef8f17de…`, replacing
    `36423b6d…`. The exporter now carries 564, so zq3-r7's "known 564 race"
    note is dropped. A "busy" recovery reply in T10 would now be a finding.
  - **Window artifacts:** rebuilt from `b869710`: `controlledruntime.test`
    `f71f0bb8…` and `fdescfs-namespace` `3d992cdf…`.
  - The init helper `8c02b98f…` is unchanged.
- **Phase 4 step 1a is required again:** the daemon bytes are new.
- **Tools rebuilt** at `5a35150`: `hfs-runtime-prep` `29ce5bdb…` and the
  signer `b6612534…`. Their Go closure changed since `ba25486`. Two builds
  of each were byte-identical; the second one was forced with `-a`.
- **Bundle:** regenerated from the committed zq3 spec, which now carries the
  o64/i64 sizing: Views Sources 10, WorkBytes 736 MiB, LedgerBytes 188 MiB,
  Claims 128, Capabilities 640, CapabilityIdentities 1024,
  CredentialRevisions 128, ServiceBytes 192 MiB. The guest budget is
  unchanged.
- **T5 deferred (q64):** the batch template declares `deferred:
  [{case: "mapped-pressure", reason: …}]` and holds the other four cases in
  canonical order.
  - `controller.py check`, `fill_plan.py batch`, the controller state and the
    T9/T11 records all repeat the deferral.
  - `syscall_batch_success` means that all four planned cases were released.
  - The sheet records T5 as "unrun (deferred, q64)". T6 needs T5 only for
    order, so it follows T4. T9-T11 run after the four released cases.
  - Expected sequences shift down by two (below).
- **k64 `usable` step** (bootstrap journal `/2`): after READY, the driver
  stats both roots once, waits until 40 s after mount-1, checks that READY
  is still the runtime's last record, then stats both roots again. The
  Phase 8 budget grows by about 70 s.
- **Re-pinned:** the kernel plan, the bundle, the argv record, the batch and
  bootstrap templates (`bootstrap.py` `de2a0054…` and `bootstrap_remote.py`
  `93563b17…` for k64; `guest.py` `c50bb849…`), the window pins and plan
  inputs, `rocky_kernel.py` and `controller.py` (they admit only
  `867ede36…`), and `argv.py` (exporter pin).
- **Facts (2026-11-14 07:57Z):**
  - f3 booted at 2026-11-13 12:51:48 UTC. **Paul rebooted it deliberately.**
    The earlier f3 reboot (2026-11-10 about 09:33 UTC) and Rocky's
    self-reboot (2026-11-10 19:32 UTC) remain "cause not established".
  - Rocky's bhyve PID is `6388`, UTC lstart `Fri Nov 13 12:52:05 2026`
    (vm-bhyve started it right after the boot), boot ID `6f6f61b0-…`.
  - UEFI vars `49bdc1ef…`, hence a new argv record.
  - `fdescfs.ko` and `fusefs.ko` are not loaded, so Phase 1a is skipped.
  - **f3 network change (not ours):** `10.20.0.133` is no longer configured
    on `re0`. `route -n get 10.20.0.123` reports interface `vm-public`, so
    the address appears to sit on the bridge. The bridge members are still
    `re0` and `tap0`.
    - No line or tool in this sheet assumes that the host address is on
      `re0`: `host.py` checks the `vm-public` members, and the session
      addresses f3 by IP.
    - The observer key's `from="10.20.0.133"` needs f3's connection to the
      guest to leave from that address. Phase 1 records `ifconfig vm-public`
      to confirm it.
- **Phases 8-10 expectations:**
  - both mounts complete, with mountinfo showing the exact options;
  - `usable` ok, then READY with every step `ok`;
  - the four cases T1-T4, then T6-T11, all get past admission.
  - Any guest `hfs … blocked` dmesg line is a finding: m64 logs every fault
    block, and a clean unmount is silent.

## Decisions kept from the zq3-r7 run-sheet review round 1 (2026-11-10)

- **`rc` capture after `rockyt`:** see the zq3-r6 notes.
- **SELinux denials:** Phase 11 step 4a captures the guest's AVC records
  before the poweroff (`p11-guest-avc.txt`). Denials are a finding; on their
  own they are not a failure. zq3-r7 logged none.
- **Relabel detection:** Phase 12's `/.autorelabel` check runs at the first
  SSH answer, after any relabel reboot has already happened. The vm-bhyve log
  (a second "starting bhyve" run) is therefore the evidence that decides
  whether a relabel happened.
- **SELinux gates** in Phases 1, 9 and 12 (unchanged).

## Session setup (workstation, bash)

```bash
source /home/paul/tmp/claude/hfs-zq3-r8-fill/session.sh
```

`session.sh` (`3de08957…`, [pins](pins.json) `fill_helpers`) defines, adapted
from zq3-r7's:

```bash
D=/home/paul/tmp/claude/hfs-zq3-r8-fill      # private 0700 fill dir
S=$D/snapshot                                # git archive of 5a35150 (docs internal tools)
R=/var/tmp/hfs-validation-xz3/zq3-r8         # f3 staging prefix (parent exists, holds zq3-r1 to zq3-r7)
G=/var/tmp/hfs-zq3-zq3-r8                    # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
okpin() { …observer_key.py cd7f38c2… check… }   # before every stdin invocation of observer_key.py
rockyt(){ …rocky with a local `timeout 150` around the real ssh, </dev/null; exit 124 = bound expired (read it with `; echo rc=${PIPESTATUS[0]}`)… }
hpin()  { …sha256sum -c of bin/sign_watch.py (4e8e75cf…), the signer bin/hfs-retained9p-operator (b6612534…, rebuilt at 5a35150) and the eleven sh/* helpers… }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r8-kernel-stage/rocky-kernel-plan.json --plan-sha256 0c761d9d5246e2b30a1c867404bd6f0005f64acc4c744a22ce5a9cdf8ff13099 --state /root/hfs-zq3-zq3-r8-kernel-state'
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261110-zq3r8-fdq/native-test-guard.py --root /var/tmp/hfs-20261110-zq3r8-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 6388 --rocky-birth 'Fri Nov 13 12:52:05 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
sha()   { …SHA256 of one local file… }
jf()    { …one field of a recorded JSON file: jf FILE KEY…… }
obs()   { …Phase 10: obs PREV NEW [LAST], the next OBSERVE through correlate.py command, p10-argv.py and one f3 call… }
```

The guard's `--rocky-birth` is the UTC `lstart` (the guard runs `ps` with
`TZ=UTC`). Run every phase in this one shell session: later phases reuse `K`,
`KP`, `NG` and the variables set by earlier lines (`PM`, `F3_NOW`, `WP`, `W`,
`EA`, `P`, `B`, `U`, `C`, `CTL`, `CR`, `OUT`). If the shell is lost, source
`session.sh` again and recompute each variable from its line: every value
comes from a file in `$E` or `$D`.

## D1: f3 module state (not needed: both modules absent)

f3 rebooted on 2026-11-13 (Paul's deliberate reboot). The 07:57Z facts show
neither `fdescfs.ko` nor `fusefs.ko` loaded (`kldstat`: `kernel`, `if_bridge.ko`,
`vmm.ko`, `nmdm.ko`, …; no fuse or fdesc mount; `/dev/fd` on devfs). The window
plan filler `fdescfs-window-plan.py` requires both absent (`fdescfs_module:
absent`, `modules_forbidden: [fdescfs.ko, fusefs.ko]`), so the state already
matches the committed pins.

The fill checked this locally with the r8 pins:
- zq3-r7's real Phase 4 facts (taken after its D1-a, modules absent, paths
  renamed to zq3-r8, the three artifact hashes swapped for the y64 ones)
  **fill**;
- the same facts with zq3-r7's unswapped host hashes **refuse** (`pinned
  artifact differs or missing`);
- post-restore facts with both modules loaded **refuse** (`fdescfs module
  present; plan is for absent`).

Phase 1a (D1-a) is therefore skipped with a recorded reason. Phase 4 loads
`fdescfs` in `setup` and `fusefs` in step 4, and both stay loaded after this
run.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-14 by the fill:
- `hfs-runtime-prep` and the signer rebuilt at `5a35150` (proof in the
  [README](README.md));
- the bundle (o64/i64 spec sizing, same guest budget);
- the argv record (from the 07:57Z launch facts, UEFI vars `49bdc1ef…`);
- the kernel plan (y64 candidate `867ede36…`, new bzImage and System.map,
  same config and modules);
- the templates (T5 deferred in the batch template) and the filled
  bootstrap plan (F1);
- the staging trees and tarballs;
- the r8 window pins (new window artifacts);
- the init helper (zq3-r1's reproduced `8c02b98f…`).

Local tests passed at `5a35150` (see [README](README.md#local-verification)).
Re-verify immediately before Phase 1:

```bash
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 ea34b1818cbac9c1be61af1ccb46afc7d62198200e6e0270c738e192d11ff242 --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 ea34b1818cbac9c1be61af1ccb46afc7d62198200e6e0270c738e192d11ff242
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json \
  --plan-sha256 006b50ed4e0e1d511744fa815cc33a6deb56833a2208654cd6b217eaef7187d6
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.SYNTHETIC-shapecheck.json   # cases_planned: the four, deferred: mapped-pressure
sha256sum /usr/bin/ssh                                   # 16cb2c6a… (the batch template pins it)
hpin && okpin && sha256sum -c <<'EOF'
dce5429ab54fafdbf9a09a6898e750e79aaa99b66e9b81d90449e1cef151493c  /home/paul/tmp/claude/hfs-zq3-r8-fill/stage/f3-stage.tar
75bf705a4f792c7c83e7cd060d588e49dc048c62c18b00a22e55ed13349be4d1  /home/paul/tmp/claude/hfs-zq3-r8-fill/stage/rocky-stage.tar
0c761d9d5246e2b30a1c867404bd6f0005f64acc4c744a22ce5a9cdf8ff13099  /home/paul/tmp/claude/hfs-zq3-r8-fill/kernel/rocky-kernel-plan.json
e3982d13506157291966232f92d8c8877d995a529ae239cbce011839f6457855  /home/paul/tmp/claude/hfs-zq3-r8-fill/controller/batch-plan.TEMPLATE.json
bb028d1f0cee2e054b05a8bace5176811848a7f203ee1e0c863243238c5a6d15  /home/paul/tmp/claude/hfs-zq3-r8-fill/fdescfs-pins/pins.json
3de08957529162a7dbb854992135a51048cde4dcbe1e309c1b16a9458d988f3f  /home/paul/tmp/claude/hfs-zq3-r8-fill/session.sh
EOF
cd $D
```

The tools snapshot `$S` is `5a35150`, the main HEAD at fill time; no refresh
is planned. If main moves before the run, `git diff --stat 5a35150 $FINAL --
tools/ internal/controlledruntime/probe docs/lab/f3-host-known_hosts
docs/lab/rocky-known_hosts docs/lab/zq3-fdescfs-window` must be empty (or
touch only tests and docs); otherwise the changed pins make this a plan
revision. If a fill step is interrupted, a leftover `NEW.next` file makes
reruns refuse with "File exists": inspect it, then remove it by hand before
retrying.

**Operator key (before Phase 8, ideally now):** on Paul's instruction Claude
copies the operator signing key to
`/home/paul/tmp/claude/hfs-zq3-r8-fill/operator/operator-signing-key` (the
empty `0700` directory exists): a copy (one link), owned by `paul`, mode
`0600`, unencrypted Ed25519 PKCS#8 PEM or a 64-hex seed without newline,
matching public key `2f6d909c…`. The bundle's request template records
exactly this path; nothing in the fill reads it. A different path means
regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8; Phase 1a is
not needed, and T5 is deferred). Paul delegated both signatures (A7) to
Claude through the watcher in Phase 8.

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 2026-11-14 07:57Z facts ([facts/](facts/)) are the baseline. Immediately
before Phase 2:

```bash
hpin
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); f=d["files"]; print([f[p]["sha256"] for p in ("/zroot/bhyve/rocky/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run", "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", "/zroot/bhyve/rocky/uefi-vars.fd")], f["/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"]["link"], d["bridge_members"], d["vmm"], d["interfaces"])' $E/p1-launch-facts.json
f3 sh -s < $D/sh/p1-f3.sh > $E/p1-f3.txt
rocky 'cat /proc/sys/kernel/random/boot_id; uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /; ls /boot | grep -c hfs-xz3; ls -d /etc/homelabfs /root/hfs-zq3-zq3-r8-* /var/tmp/hfs-zq3-zq3-r8 2>&1; ls -l /.autorelabel 2>&1; grep -E "^SELINUX(TYPE)?=" /etc/selinux/config' > $E/p1-rocky.txt
```

Continue only if all of these hold:

- **f3:**
  - kern.boottime is `{ sec = 1794574308, usec = 469376 } Fri Nov 13
    14:51:48 2026` (f3 prints local time, UTC+2: 12:51:48 UTC);
  - Rocky's bhyve PID is `6388` with UTC lstart `Fri Nov 13 12:52:05 2026`;
  - freebsd/hfs-test0..2 are Stopped, `/dev/vmm` = `rocky` and swap is
    unused;
  - **neither `fdescfs.ko` nor `fusefs.ko` is in `kldstat`**,
    `no-fuse-fdesc-mounts`, and `/dev/fd` is on devfs;
  - `/var/tmp/hfs-validation-xz3` and `/var/tmp/hfs-fdescfs-windows` are both
    `drwx------ root wheel`, and all three r8 paths are `absent`;
  - the route section shows `interface: vm-public`, and `ifconfig vm-public`
    lists `inet 10.20.0.133` with members `tap0` and `re0`. Record it. If
    `10.20.0.133` is not on `vm-public`, stop: the observer key's
    `from="10.20.0.133"` would no longer match f3's source address;
  - the PoC share is present and f3 `/usr/bin/ssh` is `6a9e5573…`.
- **Launch facts:** `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware
  `6b48e967…` (link `../edk2-bhyve/BHYVE_UEFI.fd`), **vars `49bdc1ef…`**,
  bridge members `re0 tap0` (no `tap100`).
- **Rocky:**
  - boot ID `6f6f61b0-b1d8-446a-8172-487463ac7993`;
  - `5.14.0-687.66.1.el9_8.x86_64` running and default, `next_entry=` empty,
    `saved_entry=b26c0ac534534d7fb81b3e0698558bc3-5.14.0-687.66.1.el9_8.x86_64`;
  - `Enforcing`, and `/etc/selinux/config` has `SELINUX=enforcing`;
  - **`/.autorelabel` absent** (`ls` reports "No such file or directory");
  - `authorized_keys` `e08542dc…` and python3.9 `1143733d…`;
  - no `hfs-xz3` file in `/boot`, and `/etc/homelabfs` and every r8 path are
    absent. The r1 to r7 leftovers (`/root/hfs-zq3-zq3-r{1..7}-*` and
    `/var/tmp/hfs-zq3-zq3-r{1..7}`) stay as evidence.

`boot_indeterminate=7` is expected. It stayed at 7 through every Rocky start
since zq3-r7, so it is not a start counter. The interface order in the
launch facts may differ (07:57Z: `re0 lo0 vm-public pflog0 vm-hfslab tap0
wg0`); only the set and the bridge members count. The f3 run directories now
include zq3-r7's (`/var/tmp/hfs-20261110-zq3r7-fdq` and the zq3-r7 window
and staging directories).

**`/.autorelabel` present: stop** with nothing changed. The test kernel's
boot under the exporter would find it, relabel and reboot the guest in the
middle of Phase 8. Report it: it means a boot without SELinux happened since
07:57Z. **A loaded fusefs or fdescfs: stop** (Execution notes; a plan
revision decides). Any other difference also stops and revises the plan. A
changed vars hash means a new argv record. Another f3 boot or Rocky restart
means a new boot time, PID and lstart for `NG`, `p5-recheck.py` and the
sheet.

## Phase 1a: skipped (D1-a not needed)

Record in the run log: "Phase 1a skipped: fusefs.ko and fdescfs.ko absent
(Phase 1 `kldstat`), f3 booted 2026-11-13 12:51:48 UTC". No line runs here.
The zq3-r3 to zq3-r7 D1-a lines (a normal `kldunload fusefs`, then
`kldunload fdescfs`, never `-f`) are not part of this sheet's GO.

## Phase 2: stage on f3 (A2, about 3 min)

The parent `/var/tmp/hfs-validation-xz3` already exists (it holds zq3-r1 to
zq3-r7), so this checks it instead of requiring it absent. `paul` cannot look
inside a root `0700` directory, so absence is tested through `doas`. The
tarball has no entry for the parent, so extraction never rewrites its mode or
owner.

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-validation-xz3 /var/tmp/hfs-fdescfs-windows"   # both drwx------ root wheel
f3 "doas -n test ! -e $R && doas -n test ! -e /var/tmp/hfs-20261110-zq3r8-fdq && echo absent"   # must print absent
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261110-zq3r8-fdq > $E/p2-dirs.txt
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
y64 daemon `34fb05b2…` (new bytes), the y64 native exporter bhyve
`ef8f17de…` (new bytes: 564/e64/p64/w64), `controlledruntime.test`
(`f71f0bb8…`) and `fdescfs-namespace` (`3d992cdf…`, both rebuilt from
`b869710`), the G3 `homelabfs` helper (`0500`), the
controllers and `host.py`/`argv.py` (`ed63ff97…`, exporter pin)/`host_snapshot.py`/
`runtime-volume-init.py` and `runtime-guard.py` (`fece96b3…`, unchanged)
(`0400`), the argv record, the volume-init plan, the private runtime config
(holds host receipt keys) and the Rocky pin file; the native guard
(`576d802a…`, unchanged) and `runner_process.py` go to
`/var/tmp/hfs-20261110-zq3r8-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

One command per line:

```bash
rocky 'for d in /root/hfs-zq3-zq3-r8-kernel-stage /root/hfs-zq3-zq3-r8-kernel-state /root/hfs-zq3-zq3-r8-incoming /root/hfs-zq3-zq3-r8-pod /root/hfs-zq3-zq3-r8-tools /var/tmp/hfs-zq3-zq3-r8 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r8-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r8 --observer /root/hfs-zq3-zq3-r8-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r8-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
PM=$(jf $E/p3-pod-manifest.json manifest_sha256); echo "$PM"   # review (expected f7f69339…), then:
rocky "TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-pod/pod_fixture.py build --run-id zq3-r8 --expect-manifest-sha256 $PM" | tee $E/p3-pod-build.json
```

(Rocky's shell is bash, so `VAR=value` prefixes are fine there.) Expect:
install reports one new BLS entry (`<BLS_ENTRY_ID>`) for the y64 kernel
(bzImage `40afad22…`, build ID `870bb9d1…`, System.map `1ffea83f…`, config
`28fe4a8e…` with SELinux in `CONFIG_LSM`; same release `6.1.190-hfs-xz3` and
the same five modules as zq3-r8), default still
`5.14.0-687.66.1.el9_8.x86_64`, `saved_entry` unchanged, `next_entry` empty;
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. zq3-r1 to zq3-r7 all got `f7f69339…` (82
entries); a different digest is not a failure but should be explained before
`build`. `PM` feeds the pod request in Phase 8. If Paul declines A8, skip both
pod lines and use `PM=skip` (T7 unrun). Failure: stop before Phase 5 and run
U2 (`$K remove $KP`) while Rocky still runs.

## Phase 4: f3 window, fusefs, observer key (A5, A6; B for facts/tests; about 10 min)

Step 0, read-only facts (B). Both modules are absent since the 2026-11-13 f3
boot (Phase 1 checked them; Phase 1a was skipped). Library
pins are by `realpath` (g54, zq3-r1 R4): `/usr/local/lib/libintl.so.8` is a
symlink to `libintl.so.8.4.6` and the controller hashes with `O_NOFOLLOW`.
`sh/p4-libs.sh` skips the four base libraries already in the first list (the
filler refuses duplicate paths):

```bash
hpin
f3 doas -n sha256 -r /usr/local/bin/python3.12 /boot/kernel/kernel /sbin/mount /sbin/kldload /sbin/kldstat /sbin/sysctl /bin/freebsd-version /bin/hostname \
  $R/bin/hfs-controlled-runtime $R/bin/fdescfs-namespace $R/bin/controlledruntime.test $R/bin/fdescfs-window.py \
  /boot/kernel/fdescfs.ko /boot/kernel/fusefs.ko /libexec/ld-elf.so.1 /lib/libc.so.7 /lib/libthr.so.3 /lib/libsys.so.7 > $E/p4-host.sha256
f3 'ldd /usr/local/bin/python3.12' > $E/p4-python-ldd.txt
f3 sh -s < $D/sh/p4-libs.sh > $E/p4-python-libs.sha256
cat $E/p4-python-libs.sha256 >> $E/p4-host.sha256
f3 "stat -f '%Sp %Su %Sg %N' /usr/local/bin/python3.12 /usr/local/bin /usr/local /usr /" > $E/p4-python-stat.txt
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime; date -u +%s" > $E/p4-facts-and-now.txt
f3 'sha256 -r /usr/bin/ssh' | diff - $D/facts/f3-usr-bin-ssh-20261114T075709Z.sha256 && echo F1-unchanged
f3 'env TZ=UTC ps -p 6388 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
```

Expect `p4-python-libs.sha256` to list exactly the real paths
`/usr/local/lib/libpython3.12.so.1.0`, `/usr/local/lib/libintl.so.8.4.6`,
`/usr/lib/libdl.so.1`, `/lib/libutil.so.10`, `/lib/libm.so.5` (the closure of
zq3-r1 to zq3-r7; f3 rebooted since, so any other library or a changed base
library hash is a fact change to review),
`F1-unchanged` (otherwise stop: the filled bootstrap plan pins `6a9e5573…`,
and refilling F1 is a plan revision), and `Fri Nov 13 12:52:05 2026` with
`/dev/vmm` = `rocky` and neither module in `kldstat`.

Split the facts and fill the window plan on the workstation (B) with the r8
window pins (`bb028d1f…`: the committed pins as y64 left them, with
`zq3-r1` → `zq3-r8`; daemon `34fb05b2…`, helper `3d992cdf…`, test
`f71f0bb8…`), then
review the printed SHA256. A refusal here (for example `fdescfs module
present`) means a module was loaded after Phase 1: stop, nothing on f3 has
changed since.
The plan expires 55 minutes after `F3_NOW`; setup and qualify must run before
that (restore ignores expiry):

```bash
head -n 1 $E/p4-facts-and-now.txt > $E/p4-facts.json
F3_NOW=$(tail -n 1 $E/p4-facts-and-now.txt)
python3 -I -B $S/tools/lab/fdescfs-window-plan.py --pins $D/fdescfs-pins/pins.json \
  --facts $E/p4-facts.json --host-sha256 $E/p4-host.sha256 --expires $(( F3_NOW + 3300 )) --host-now $F3_NOW \
  --out $D/fdescfs-plan          # prints the window plan SHA256
WP=$(sha $D/fdescfs-plan/plan.json); echo "$WP"     # must equal the printed SHA256
f3 "doas -n sh -c 'umask 277; test ! -e $R/records/fdescfs-plan.json && cat > $R/records/fdescfs-plan.json'" < $D/fdescfs-plan/plan.json
f3 doas -n sha256 -r $R/records/fdescfs-plan.json      # must equal $WP
```

F1 (bootstrap `observer.ssh_sha256`) was filled at fill time from the 07:57Z
fact (`fill_plan.py bootstrap`, plan `006b50ed…`, `bootstrap.py check`
passed); the `F1-unchanged` line above is its recheck. Nothing to fill here.

Step 1, negative before setup (B; a skip counts as unrun). No `--` before the
command; a retry takes a new stage name and keeps the refused one. The o54
host profile needs free ≥ 1 GiB and free + inactive ≥ 3.5 GiB (07:57Z: 12.11 GB
free, 1.36 GB inactive):

```bash
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v" | tee $E/p4-unsupported-before.txt
```

Step 1a (B, **required**): process observer qualification of the exact
daemon bytes. The y64 package carries the new i64..x64 daemon `34fb05b2…`,
so zq3-r7's qualification of the d64 daemon `9b7eed8d…` does not carry over
(xz3-20261110h).

The staged daemon runs its read-only qualification once:
- under the native guard's host profile (Rocky pinned, 60 s, 512 MiB);
- as root through `doas`, the credentials `runtime-guard.py` later starts it
  with;
- before setup, while `/dev/fd` is still devfs, which this check does not
  need (the f44 run passed on devfs too).

It reads no config, starts no child and mounts nothing. Each line on its own:

```bash
f3 "$NG --stage process-observer $R/bin/hfs-controlled-runtime --check-process-observer" | tee $E/p4-process-observer-guard.json
f3 "doas -n cat /var/tmp/hfs-20261110-zq3r8-fdq/checks/process-observer/stdout" > $E/p4-process-observer.json
f3 "doas -n cat /var/tmp/hfs-20261110-zq3r8-fdq/checks/process-observer/stderr" > $E/p4-process-observer.stderr
python3 -I -B $D/sh/p4-observer.py $E/p4-process-observer-guard.json $E/p4-process-observer.json $E/p4-process-observer.stderr | tee $E/p4-process-observer-check.json
```

Pass criteria (`p4-observer.py`, `"pass": true`, exit 0):
- the guard ran exactly `$R/bin/hfs-controlled-runtime
  --check-process-observer`, with `error` null and child exit 0;
- stderr is empty;
- stdout is one `homelabfs-process-observer/1` record with `platform`
  `freebsd/amd64`, `executable` the staged path, **`sha256` `34fb05b2…`** and
  `publication` null;
- `uid` 0 and `gid` 0 (the gid among `groups`), a positive `pid`, an
  `incarnation` `freebsd:<pid>:…` and a positive `text_vnode`.

zq3-r5 to zq3-r7 passed exactly these criteria as root (uid/gid 0, groups
`[0, 0, 5]`). zq3-r7's real records, renamed to zq3-r8, fail this check
only on the `sha256` and pass with `34fb05b2…` substituted.

**Failure: stop before Phase 5**, so Rocky is never stopped. Nothing is
selected yet, the window is not set up and no observer key exists. Keep
`checks/process-observer` on f3 as evidence, run U2 (`rocky "$K remove
$KP"`) while Rocky runs, record and report: the y64 daemon is not
qualified, and no runtime may start with it. Both modules are still
absent, which is harmless. Only a guard refusal before the child
started (receipt `exit` null, `error` naming a guard bound such as the
free-page floor) may be retried, once, as `--stage process-observer-2` after
recording the cause. A child exit other than 0 or a record mismatch is never
retried.

Steps 2-4 (A5): setup, qualify (under the guard), fusefs. The root-private
parent `/var/tmp/hfs-fdescfs-windows` exists (`root wheel` `0700`, with
`zq3-r1`, `zq3-r1.refused-setup-1`, `zq3-r2` to `zq3-r7`); it is checked
here, never created. `setup` refuses an existing run directory:

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-fdescfs-windows"   # drwx------ root wheel /var/tmp/hfs-fdescfs-windows
f3 "doas -n test ! -e /var/tmp/hfs-fdescfs-windows/zq3-r8 && echo absent"   # must print absent
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 $WP --directory /var/tmp/hfs-fdescfs-windows/zq3-r8"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots (or a later
approved unload). If setup or qualify refuses and a reviewed fix follows,
resume as zq3-r1 did: keep the refused window directory under a new name
(`zq3-r8.refused-setup-1`, via `printf … | f3 doas -n sh -s`), and rerun
step 0 and the window plan fill with `-resume` names (`p4-*-resume.*`, `--out
$D/fdescfs-plan-resume`, remote `$R/records/fdescfs-plan-resume.json`, `WP`
and `W` pointing at it). The plan's expiry also forces this refill after a
pause of more than 55 minutes.

Step 5 (A6), run-only observer key:

```bash
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r8-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r8-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r8 --public-key /root/hfs-zq3-zq3-r8-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
EA=$(jf $E/p4-observer-enrol.json after_sha256); echo "$EA"   # authorized_keys after enrolment; needed in Phase 11
```

The private key never leaves f3. Failure: nothing unloaded; U2 the unselected
kernel; restore the window (Phase 11 step 10); remove the observer key if it
was enrolled (execution notes).

## Phase 5: pre-stop rechecks, memory, one-shot select and stop Rocky (A1, A3; about 6 min)

Step 1, pre-stop rechecks (B, g54, zq3-r1 R5). In zq3-r1 an external f3
power-off during a pause restarted Rocky and dropped the window, and select
and stop then ran on the new instance. These compare with the Phase 1 facts:

```bash
hpin
f3 'sysctl -n kern.boottime' > $E/p5-boottime.txt              # expect: { sec = 1794574308, usec = 469376 } Fri Nov 13 14:51:48 2026
f3 'env TZ=UTC ps -p 6388 -o lstart=' > $E/p5-rocky-lstart.txt  # expect: Fri Nov 13 12:52:05 2026
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime" > $E/p5-facts.json
f3 doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r8/owned.json > $E/p5-owned.json
python3 -I -B $D/sh/p5-recheck.py $E/p5-boottime.txt $E/p5-rocky-lstart.txt $E/p5-facts.json $E/p5-owned.json | tee $E/p5-recheck.json
```

`p5-recheck.py` passes (exit 0, `"pass": true`) only if the boot time (also
the `boot` field of `facts`) and the UTC lstart equal the values above, and
the parsed `descriptor` of `facts` equals the parsed `owned.json` (a
supported `fdescfs` report with the owned FSID).

Step 1a, memory pre-estimate (B) and the disk-level reference:

```bash
f3 'doas -n vm list; swapinfo; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_active_count vm.stats.vm.v_laundry_count vm.stats.vm.v_wire_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min; ps -p 6388 -o rss=' > $E/p5-mem.txt
python3 -I -B $D/sh/p5-estimate.py $E/p5-mem.txt | tee $E/p5-estimate.json
```

`p5-estimate.py` prints two estimates of what Phase 6 will measure. The
zq3-r2 sheet's `rss_based` one, free + inactive + (ARC − `c_min`) + Rocky's
bhyve RSS, is **pessimistic**: on f3 Rocky's guest memory shows as Active
pages, not in the bhyve RSS. The better estimate is `active_based` = free +
inactive + (ARC − `c_min`) + Active. It is an upper bound, because
v_active_count also holds the host's own Active pages. zq3-r3 measured
`active_based` 15.14 GB here (margin 0.38 GB), zq3-r4 15.19 GB (margin
0.43 GB), zq3-r5 15.23 GB (margin 0.46 GB), zq3-r6 15.22 GB (margin
0.45 GB) and zq3-r7 15.23 GB (margin 0.47 GB); all five drained. At 07:57Z
free was 12.11 GB and inactive 1.36 GB, with ARC 1.17 GB, a day after Rocky's
start. bhyve allocates guest memory lazily, so Rocky's lightly used guest
holds little host memory now. `active_based` counts Active as well, so the
estimate should be similar, and Phase 6 decides.

Step 1b, drain the UMA caches (A1, from zq3-r2 R2-1), **only if**
`p5-estimate.json` shows `"drain_recommended": true`, which means
`active_based` is short of the Phase 6 bar or within 1 GiB of it. Record the
decision in the run log either way. Each line on its own:

```bash
f3 sh -s < $D/sh/p5-uma-stat.sh > $E/p5-uma-before.txt
printf 'sysctl debug.uma_reclaim=2\n' | f3 doas -n sh -s | tee $E/p5-uma-reclaim.txt
f3 sh -s < $D/sh/p5-uma-stat.sh > $E/p5-uma-after.txt
```

`p5-uma-stat.sh` records pagesize, free, wired, inactive and active pages
and `uma_free_bytes` (item size × free items over `vmstat -z`). Expect free
pages to rise and wired pages and `uma_free_bytes` to fall. In zq3-r2 free
rose by 2.2 GB; in zq3-r3 the drain freed about 0.23 GB, in zq3-r4 about
0.18 GB, in zq3-r5 about 0.15 GB, in zq3-r6 about 0.17 GB and in zq3-r7 about
0.15 GB. The drain is the same
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
  /var/tmp/hfs-fdescfs-windows/zq3-r8"` and
  `f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r8" >
  $E/f3-window-evidence-vanished.tar`. Rocky has AUTO yes, so it normally runs
  again: then U2. If it is down, check the PoC share (`f3 'ls -ld
  /var/tmp/homelabfs-9p-poc/share'`, risk R6), start it normally
  (`$S/tools/lab/vms.sh --approved-action start-rocky`), then U2.
- Otherwise (same boot, Rocky runs, a PID/lstart or mount mismatch): inspect
  and stop; U2, then restore the window (Phase 11 step 10, the `$W restore`
  line) only if `facts` still shows the owned FSID.

Steps 2-3, select and stop:

```bash
rocky "$K select $KP" | tee $E/p5-select.json        # next_entry = <BLS_ENTRY_ID>; on any failure it runs U1 itself
$S/tools/lab/vms.sh --approved-action stop-rocky      # clean vm stop, waits up to 120 s, never forces
f3 'doas -n vm list; ps -ax | grep "[b]hyve: rocky" || echo no-rocky-process; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm; ifconfig -l' > $E/p5-stopped.txt
```

Failure in step 2, or if step 3 refuses before stopping anything:
`rocky "$K cancel $KP"` (U1), then U2. Failure after the stop: `next_entry`
is still set, recovery is U3 (below).

## Phase 6: post-stop admission (B, 1 min)

```bash
hpin
f3 'swapinfo; doas -n vm list; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm; mount -p | grep /dev/fd' > $E/p6-admission.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min' > $E/p6-mem.txt
python3 -I -B $D/sh/p6-admission.py $E/p6-mem.txt | tee $E/p6-bar.json
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" > $E/p6-descriptor.json
python3 -I -c 'import json,sys; a, b = (json.load(open(p)) for p in sys.argv[1:]); print("descriptor-equals-owned:", a == b); sys.exit(a != b)' $E/p6-descriptor.json $E/p5-owned.json
```

The bar (g54) is the enforced guard ceilings plus a 3 GiB host reserve:

| Part | Bytes |
|---|---|
| bhyve RSS ceiling, 6 GiB + 512 MiB | 6,979,321,856 |
| runtime RSS ceiling, 4 GiB + 256 MiB | 4,563,402,752 |
| host reserve, 3 GiB | 3,221,225,472 |
| **bar, 13.75 GiB** | **14,763,950,080** (= 3,604,480 pages of 4096) |

Required: reclaimable = (free + inactive) × 4096 + (ARC − `arcstats.c_min`) ≥
14,763,950,080 (`p6-bar.json` `"pass": true`); swap unused; all VMs stopped
and `no-vmm`; `/dev/fd` still the owned fdescfs mount
(`descriptor-equals-owned: True`). For reference, zq3-r3 measured
15,303,398,240 bytes here after its Phase 5 drain, zq3-r4 15,305,552,288
(margin 0.54 GB each), zq3-r5 15,294,364,056 (margin 0.53 GB), zq3-r6
15,329,786,840 (margin 0.57 GB) and zq3-r7 about 15.33 GB (margin 0.57 GB).
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
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 ea34b1818cbac9c1be61af1ccb46afc7d62198200e6e0270c738e192d11ff242" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 33766fc303708c1dcb7e26652effdf044757488c5cc1a9b2e2dc58c7505422f8 --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r8 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`49bdc1ef…` (any Rocky boot since 07:57Z changes it; it was `00f82a83…` at
zq3-r7, and Rocky started many times since, last at 2026-11-13 12:52:05 UTC): stop;
regenerating the argv record from fresh facts (same sessions,
same args) is a plan revision for review, never an on-the-spot edit. Failure
with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3. `k04-baseline.json` is T11's
required baseline (the empty mount point before the runtime starts).

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s to READY plus about 70 s for `usable`, about 6 min with prep)

Signing is delegated to Claude (A7, Paul's decision). The driver's stdout goes
through the watcher `bin/sign_watch.py` (`4e8e75cf…`), which is zq3-r7's
watcher with `D`, the comments and the signer pin changed. It copies every
line through. For an `operator_action` line it runs the signer only if:
- the printed command is exactly one of the two commands below;
- the request file's SHA256 equals the printed `request_sha256`;
- the signer still hashes to `b6612534…`;
- that action has not run before (each runs at most once).

Anything else is logged as `"watcher": "refused"` and left unsigned, so the
driver times out (fails closed). The signer pin lives in two places, `hpin`
(`session.sh`) and the watcher's `SIGNER_SHA256`. The signer is rebuilt at
`5a35150`, because the y64 daemon fixes changed non-test sources it links;
zq3-r7's `9ca59cee…` is retired.
Prepare:

```bash
hpin
stat -c '%a %U %h %s' $D/operator/operator-signing-key   # 600 paul 1 <size>; placed by Claude on Paul's instruction
test ! -e $D/bootstrap-run && echo output-absent
P=$D/bootstrap/bootstrap-plan.json
set -o pipefail
```

Run:

```bash
python3 -I -B $S/tools/lab/retained9p/bootstrap.py run --plan $P \
  --plan-sha256 006b50ed4e0e1d511744fa815cc33a6deb56833a2208654cd6b217eaef7187d6 --output $D/bootstrap-run \
  | python3 -I -B $D/bin/sign_watch.py | tee $E/p8-bootstrap.jsonl
```

The plan is re-derived from zq3-r7's (the q54 template that ran). It pins:
- `runtime-guard.py` `fece96b3…` (unchanged);
- the y64 daemon `34fb05b2…` (`runtime.sha256`, also the guard's
  `--runtime-sha256` below; new);
- the r8 kernel plan `0c761d9d…` and `rocky_kernel.py` `1e9321fd…`;
- the k64 `bootstrap_remote.py` `93563b17…` (`remote_sha256`, with the
  `guest-stat` action of the `usable` step);
- `hfs-runtime-prep` `29ce5bdb…` (rebuilt at `5a35150`). It fills the
  enrollment and bind requests with the r8 bundle's guest budget, which is
  unchanged (reservations 1024, received 256, 512/512 MiB).

The driver itself is `bootstrap.py` `de2a0054…` (journal `/2`).
The driver launches, under the outer guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r8/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r8/guard --runtime-sha256 34fb05b28357b42e37816370f9bb6c0ec046ff2e98763dafff87c3680021694c --vm-name hfs-zq3-r8 --disk-root /var/tmp/hfs-validation-xz3/zq3-r8 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r8 --skip-path /var/tmp/hfs-validation-xz3/zq3-r8/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r8/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r8/runtime/runtime-config.json --disposable-validation
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
/home/paul/tmp/claude/hfs-zq3-r8-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r8-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/signer-binding
```

Expected order:
1. host-preflight, launch, proposal;
2. guest-ready (test kernel; GRUB consumed `next_entry`);
3. admission (manifest SHA256, boot ID, provider nonce, `virtioN` names);
4. enroll-request, then **sign-enroll** (`"watcher": "signed"`, exit 0);
5. provision, enrollment-handoff, awaiting-bind, bind-request;
6. **sign-bind** (signed, exit 0), then bind;
7. mount-0, mount-1, READY;
8. **usable** (k64, new).

`usable` takes about 40-70 s. It runs a first `lstat` of both roots, waits
until 40 s after mount-1 ended (the guest's 30 s QUERY window plus 10 s), checks that
READY is still the runtime's last record, and runs a final `lstat` of both
roots. Its own bound is the settle point plus 30 s. The `lstat`s write
nothing into the mounts, but each adds GETATTR and PROCESS_EXIT claims to the
provider ledgers before T1. `summary.json` says `ready` only if `usable`
passed too.

What zq3-r7 got and what y64 changes:
- zq3-r7 reached READY with every step `ok` and both mounts (35.3 s).
- Its mount roots then refused root (`EACCES`, R7-2): the mount(8)
  PROCESS_EXIT claims stayed `RESERVED/UNPREPARED`, because `Views.Sources: 2`
  gave a source capacity below 3. After the 30 s QUERY window the guest
  blocked both sessions silently.
- Its mountinfo showed only `rw` (R7-1), so `guest.py` refused T1 at
  admission.
- y64 changes all three: Sources 10 with a validation floor (i64, m64),
  definite refusals resolved (j64), every guest fault block logged (m64),
  and hfs `.show_options` with an exact `cache=mmap` (g64). k64's `usable`
  step would have failed zq3-r7 here, before the batch fill.

**Expected:**
- both mounts complete, and each mount step's `mountinfo` shows
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`;
- READY arrives well inside the 120 s bootstrap deadline;
- `usable` is `ok` for both roots in both probes, so the summary says
  `ready` with every step `ok`.

The driver never reads the key.

Failure, a watcher refusal or `uncertain`: nothing is replayed. Inspect
`$D/bootstrap-run/journal.jsonl` and `$E/p8-bootstrap.jsonl`, and collect
the failure evidence read-only, as in zq3-r3:
- `p8-fail-guest.txt`: `uname -r`, the boot ID, `dmesg | grep -E
  "9p|virtio|hfs"` and the virtio driver list;
- `p8-guest-dmesg.txt`: the full guest `dmesg`, if anything oopsed;
- `p8-fail-host.txt` and `p8-fail-exporter.txt`.

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
`usable_note`). The runtime reached READY, but a root failed an `lstat`; the
journal's `usable` result names the probe, each root's outcome and the
errno. The runtime, both mounts and any stuck `lstat` child were left as
they are. Nothing is retried or remounted, and the batch fill is not
attempted: `fill_plan.py` refuses such a run. Collect read-only, while the
runtime runs:
- the live owner snapshot (below);
- the guest `dmesg | grep -E "9p|hfs|SELinux"`, where m64 logs each fault
  block as an `hfs … blocked` line;
- the mountinfo lines.

Then go to Phase 11 through its guard-SIGTERM branch. This is the R7-2
failure again and a finding against i64, j64 and m64.

**If a mount fails or blocks** (guest-mount's 30 s bound gives `uncertain`,
or a mount step fails while the runtime still runs), or `usable` fails, take
the live owner snapshot from a second shell (`source
/home/paul/tmp/claude/hfs-zq3-r8-fill/session.sh`), as zq3-r4 to zq3-r7
did, read-only and while the runtime runs:
`f3 "doas -n tar -c -f - --exclude control.sock -C $R runtime/owner" >
$E/p8-owner-live.tar`. A mount-1 `EKEYREJECTED` or a guest `view control
worker stopped, error -110` would be a regression of d64: record the
guest's mount process state (`ps -o pid,stat,wchan:32,cmd -C mount`) with
the read-only failure evidence above.

**After READY and `usable` (B, at once; the guard's 3600 s bound runs from
the launch).** Fill the controller batch plan (F6/F7) from the run's
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
python3 -I -B $S/tools/lab/retained9p/fill_plan.py batch --template $D/controller/batch-plan.TEMPLATE.json \
  --template-sha256 e3982d13506157291966232f92d8c8877d995a529ae239cbce011839f6457855 \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 006b50ed4e0e1d511744fa815cc33a6deb56833a2208654cd6b217eaef7187d6 \
  --output $D/controller/batch-plan.json | tee $E/p8-batch-fill.json
B=$(jf $E/p8-batch-fill.json sha256); echo "$B"
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.json | tee $E/p8-batch-check.json   # plan_sha256 must equal $B
python3 -I -B $D/sh/p9-ready.py $D/bootstrap-run | tee $E/p9-ready.json
U=$(python3 -I -B $D/sh/p9-ready.py --runtime $D/bootstrap-run); echo "$U"
python3 -I -B $D/sh/p10-requests.py $D/controller/batch-plan.json $B $PM $E | tee $E/p8-requests.json
```

Both `p8-batch-fill.json` and `p8-batch-check.json` must show
`"cases_planned": ["credentials", "sync", "metadata", "namespace"]` and
`deferred` with exactly the `mapped-pressure` entry and its q64 reason.
Anything else is a refusal-level mismatch: stop, runtime serving.

`p9-ready.py` checks:
- `summary.json` (`ready`);
- the journal's one `ok` `usable` result;
- the journal's READY record (`CONTROLLED_LOCAL`, two domains, publication
  null, control socket `$R/runtime/owner/control.sock`).

It prints the runtime UUID, cut and epoch. `p10-requests.py` writes:
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

```bash
C="$R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock"
f3 "doas -n $C --operation observe --runtime $U --sequence 1" > $E/observe-001.json
python3 -I -B $D/sh/p9-observe.py $E/observe-001.json $E/p9-ready.json $D/controller/batch-plan.json $B | tee $E/p9-observe-check.json
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo" | tee $E/p9-mountinfo.txt
rocky 'getenforce; cat /sys/fs/selinux/enforce; uname -r; dmesg | grep -E "LSM: initializing|SELinux: +Initializing"; dmesg | grep -E "hfs.*blocked" || echo no-hfs-block' | tee $E/p9-guest.txt
```

`p9-observe.py` applies the committed correlator's own checks to this first
OBSERVE:
- `OBSERVED`, sequence 1, `complete`, stage `SERVING` and publication null;
- no RESTART or lost-reply section;
- runtime, cut and epoch equal to READY's;
- exactly one unfenced admission with the batch plan's boot ID, provider
  nonce and both sessions;
- a SERVING service identity with an applied position;
- provider ledgers for both sessions, the view controller ledger and the
  transport high-water.

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
  nothing.

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

## Phase 10: gate matrix (B inside the window, about 20-30 min)

Order **T1-T4, T6, T7, T9, T10, T11, T8**; T5 is **unrun (deferred, q64)**.
Stop dependent steps on the first failure (dependencies are named per step). Results are pass, fail, unsupported, unrun
or uncertain; none of the correlator records is acceptance. Two clocks run:
the guard's 3600 s from the Phase 8 launch, and the controller's 3600 s from
`probe` (later commands refuse and preserve). Set once:

```bash
CTL=$S/tools/lab/check-retained9p.sh
CR=$S/tools/lab/retained9p/correlate.py
OUT=$D/ctrl-run
```

**Lifecycle sequences.** Every OBSERVE, ARM and RESTART needs a sequence
strictly above the last accepted one. Phase 9 used 1 and the T1 baseline
uses 2 (both literal: `correlate.py command` needs the controller run, which
`probe` creates). Every later command comes from `correlate.py command`,
whose sequence is the latest reply's plus one: through `obs` for OBSERVE,
directly for ARM and RESTART. Expected sequences if nothing is lost (four cases, T5 deferred): case
baselines/retained 2/3, 4/5, 6/7, 8/9; T9 pre-arm 10, ARM 11, post-CALL 12;
T10 post-quiesce 13, RESTART 14. A refused or lost command is
never resent with the same sequence (rules above).

**T5 `mapped-pressure`: unrun (deferred, q64).** The batch plan declares it
in `deferred` with its reason, so `next` after T4's release refuses ("a
remaining case") and no T5 line exists. Its full 4 MiB canary needs more
view lifetimes than the 4 GiB host budget funds; Paul's decision on q64 is
pending (default option 4: defer). Record it in the run log and the result
as "T5 unrun (deferred, q64)", never as passed. zq3 acceptance stays
incomplete.

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

T1, `credentials`:

```bash
f3 "doas -n $C --operation observe --runtime $U --sequence 2" > $E/obs-credentials-baseline.json
$CTL probe --plan $D/controller/batch-plan.json --sha256 $B --output $OUT | tee $E/t1-probe.json
obs obs-credentials-baseline obs-credentials-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case credentials --baseline $E/obs-credentials-baseline.json --baseline-sha256 $(sha $E/obs-credentials-baseline.json) --retained $E/obs-credentials-retained.json --retained-sha256 $(sha $E/obs-credentials-retained.json) --record $E/corr-credentials.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-credentials.json --correlation-sha256 $(sha $E/corr-credentials.json) | tee $E/t1-release.json
```

T2, `sync`:

```bash
obs obs-credentials-retained obs-sync-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t2-next.json
obs obs-sync-baseline obs-sync-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case sync --baseline $E/obs-sync-baseline.json --baseline-sha256 $(sha $E/obs-sync-baseline.json) --retained $E/obs-sync-retained.json --retained-sha256 $(sha $E/obs-sync-retained.json) --record $E/corr-sync.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-sync.json --correlation-sha256 $(sha $E/corr-sync.json) | tee $E/t2-release.json
```

T3, `metadata`:

```bash
obs obs-sync-retained obs-metadata-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t3-next.json
obs obs-metadata-baseline obs-metadata-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case metadata --baseline $E/obs-metadata-baseline.json --baseline-sha256 $(sha $E/obs-metadata-baseline.json) --retained $E/obs-metadata-retained.json --retained-sha256 $(sha $E/obs-metadata-retained.json) --record $E/corr-metadata.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-metadata.json --correlation-sha256 $(sha $E/corr-metadata.json) | tee $E/t3-release.json
```

T4, `namespace`:

```bash
obs obs-metadata-retained obs-namespace-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t4-next.json
obs obs-namespace-baseline obs-namespace-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case namespace --baseline $E/obs-namespace-baseline.json --baseline-sha256 $(sha $E/obs-namespace-baseline.json) --retained $E/obs-namespace-retained.json --retained-sha256 $(sha $E/obs-namespace-retained.json) --record $E/corr-namespace.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-namespace.json --correlation-sha256 $(sha $E/corr-namespace.json) | tee $E/t4-release.json
$CTL status --output $OUT > $E/t4-status.json
jf $E/t4-status.json syscall_batch_success      # must print True (every PLANNED case released)
jf $E/t4-status.json cases_planned              # ['credentials', 'sync', 'metadata', 'namespace']
jf $E/t4-status.json deferred                   # the mapped-pressure entry with its q64 reason
```

Mid-release failure (run plan): the guest consumes the retention record
before its first unlink; if an unlink or the deadline then fails, the
controller stays at `release-dispatching-uncertain` with `error`. Never
re-release or replay: root inspects the leaf against `released-CASE.json`,
keeps leftovers and evidence, T1-T4 incomplete, Phase 11.

T6, same-mount locks on m0 (needs T5 only for the order; with T5 deferred it follows T4's release):

```bash
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-tools/guest-lock-probe.py' < $E/lock-m0.json | tee $E/t6-lock.json
```

Pass only with `"passed": true` (exit 0; exit 10 is a fail, recorded with its
`error`; a fail does not block T7). T6b (across m0/m1): recorded
**unsupported**, not run.

T7 (A8, skip if `PM=skip`): two workloads in the ordinary pod profile
bind-sharing m0. The request file must not exist on Rocky yet:

```bash
rocky 'test ! -e /root/hfs-zq3-zq3-r8-pod/request.json && (umask 077; cat > /root/hfs-zq3-zq3-r8-pod/request.json)' < $E/pod-request.json
rocky 'sha256sum /root/hfs-zq3-zq3-r8-pod/request.json'   # must equal the next line
sha $E/pod-request.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r8-pod/request.json' | tee $E/t7-pod.json
```

Pass only with `t7_pass: true` (which also removes the two exited containers
and the pod payload normally); a failure or `uncertain` preserves both. An
SELinux denial of `container_t` on the 9p label is a T7 failure, never
relabelled or disabled.

T9, v24 lost reply with the d44 MKDIR filter (the y64 daemon `34fb05b2…`
keeps it). It runs after the four released cases: the controller's lifecycle
steps need `syscall_batch_success`, which with the deferral means every
planned case released. Run the ARM and the CALL step back to back:

```bash
obs obs-namespace-retained t9-pre-arm
python3 -I -B $CR command --operation arm-lost-reply --lost-intent MKDIR --output $OUT --sha256 $B --observe $E/t9-pre-arm.json --observe-sha256 $(sha $E/t9-pre-arm.json) --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock > $E/cmd-t9-arm.json
ARM=$(python3 -I -B $D/sh/p10-argv.py --operation arm-lost-reply $E/cmd-t9-arm.json); echo "$ARM"
f3 "doas -n $ARM" > $E/t9-armed.json
$CTL lifecycle --output $OUT --sha256 $B --step lost-reply-call | tee $E/t9-call.json
obs t9-pre-arm t9-post-call t9-armed
$CTL lifecycle --output $OUT --sha256 $B --step lost-reply-reread | tee $E/t9-reread.json
python3 -I -B $CR lost-reply --output $OUT --sha256 $B --before $E/t9-pre-arm.json --before-sha256 $(sha $E/t9-pre-arm.json) --armed $E/t9-armed.json --armed-sha256 $(sha $E/t9-armed.json) --after $E/t9-post-call.json --after-sha256 $(sha $E/t9-post-call.json) --record $E/t9.json
```

`t9-armed.json` must say `"status":"ARMED"` before the CALL step runs. A
`REFUSED` ARM took no effect but may have consumed its sequence: record T9
failed and T10/T11 unrun (their expected outcomes depend on T9), run T8, then
Phase 11 with the runtime serving. The record's `classification` is expected to be
`dropped-reply-effect-committed-and-visible` (`armed_intent` and
`dropped_opcode` MKDIR, the mkdir fails in m0, m1 sees the directory, m0
blocked with `EHOSTDOWN`); never a pass. A controller step that fails or is
uncertain records `error`, after which every controller command refuses:
then go to Phase 11 (runtime serving).

T10, daemon restart via u24 (last step that needs the live runtime):

```bash
$CTL lifecycle --output $OUT --sha256 $B --step quiesce | tee $E/t10-quiesce.json
obs t9-post-call t10-post-quiesce
python3 -I -B $CR command --operation restart --output $OUT --sha256 $B --observe $E/t10-post-quiesce.json --observe-sha256 $(sha $E/t10-post-quiesce.json) --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock > $E/cmd-t10-restart.json
RST=$(python3 -I -B $D/sh/p10-argv.py --operation restart $E/cmd-t10-restart.json); echo "$RST"
f3 "doas -n $RST" > $E/t10-restart.json
jf $E/t10-restart.json status    # RESTARTED, FENCED or EFFECT_UNREPORTABLE (then: jf $E/t10-restart.json outcome)
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
`ef8f17de…` now carries 564 (and e64, p64, w64), so a "busy" recovery reply
or a lost REFUSED in T10 is a new finding, not zq3-r7's known race.

T11, K04 host side (**only if** T10 is FENCED). Wait for the runtime to exit
and the guard to write its receipt (repeat the first line every 10 s, at most
30 times, until it prints the receipt):

```bash
f3 "doas -n test -e $R/guard/receipt.json && doas -n cat $R/guard/receipt.json || echo no-receipt"
f3 "doas -n cat $R/guard/receipt.json" > $E/p10-guard-receipt.json
jf $E/p10-guard-receipt.json runtime_running_at_guard_exit    # must print False
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r8 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss | tee $E/t11-after-loss.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r8 --path $R/runtime/mnt --label after" > $E/k04-after.json
python3 -I -B $CR k04 --output $OUT --sha256 $B --baseline $E/k04-baseline.json --baseline-sha256 $(sha $E/k04-baseline.json) --before $E/k04-before.json --before-sha256 $(sha $E/k04-before.json) --after $E/k04-after.json --after-sha256 $(sha $E/k04-after.json) --record $E/t11.json
```

No receipt after 30 tries (5 minutes): T11 unrun; Phase 11 (inspect the guard
first, no signal without Paul's decision). The after-loss step probes the
unarmed m1 (lstat, list, exclusive create, never retried). The record's
`k04_refusal_observed` needs every guest attempt to fail, the marker absent
on the host and no raw-directory change; if m1 already showed the guest's own
`EHOSTDOWN` (T9 re-read, T10 quiesce or the first after-loss attempt) the
probe is flagged vacuous. The record also repeats the plan's `deferred` list
(T5). The recorded answer for remount is "VM restart required".

T8, K04 guest side, last (needs no live runtime; m1 must still be mounted):

```bash
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo" | tee $E/t8-before.txt   # m1 must be listed
rockyt "umount $G/m1" | tee $E/t8-umount.txt; echo rc=${PIPESTATUS[0]}   # normal only; refused or timed out (rc=124): T8 unrun, Phase 11
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r8-tools/guest-lock-probe.py' < $E/lock-m1-t8.json | tee $E/t8-lock-refusal.json
rocky "ls -A $G/m1" | tee $E/t8-ls.txt          # must print nothing
```

T8 holds only if m1 was listed before, the unmount succeeded, the probe refused at admission
before any open (`"passed": false`, `error` `Refused: exact mountpoint, 9p
source and mount ID required`, exit 10) and the directory is empty.

Not run: PTE/TLB/busy-pin/GUP exclusion and funded drains (G14), distributed
writers (ov3/pv3).

## Phase 11: cleanup (within A1/A4/A5/A6; about 5-15 min)
## Phase 11: cleanup (within A1/A4/A5/A6; about 5-15 min)

**The order below is mandatory. Run one command per line and never batch
them** (zq3-r2 R2-6: a batched cleanup skipped the native unmount, and the
window restore then refused). Write each result into the run log before
running the next line.

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
  T10 unrun, or T10 RESTARTED, r1 finding F5; a failed `usable` step or a
  Phase 9 mount-tuple miss also lands here; zq3-r6 and zq3-r7 took this
  branch). The step 6 `child-gone` check also matches the guard's
  `--vm-name hfs-zq3-r8`, and `vms.sh start-rocky` refuses while the guard
  lives. Before step 0, end it with one SIGTERM to the guard. The guard then
  makes its own bounded stop: one SIGTERM to the runtime, 30 s grace, up to
  300 s observation. Wait for the guard to exit:

  ```bash
  f3 'pgrep -U root -lf runtime-guard.py'         # exactly one root python guard; note <GUARD_PID> (the csh wrapper is paul's)
  f3 "doas -n kill -TERM <GUARD_PID>"
  f3 'pgrep -U root -lf runtime-guard.py || echo guard-gone'   # repeat every 10 s until guard-gone (at most the 330 s bound)
  ```

  Then `receipt.json` with the runtime gone: continue. `stop-uncertain.json`
  (runtime still running): stop and preserve; no further signal without
  Paul's decision.
- **FENCED** (T10, expected) or READY ended by the guard at its bound: the
  runtime has exited; follow steps 0-10.

Steps, in this order:

```bash
# 0. guard receipt: the exporter (bhyve child) PID for step 7
f3 "doas -n cat $R/guard/receipt.json" > $E/p11-guard-receipt.json
jf $E/p11-guard-receipt.json runtime_running_at_guard_exit   # must print False
jf $E/p11-guard-receipt.json exporter    # prints None when no child was ever seen: then step 7 uses --no-child and XP stays unset
XP=$(jf $E/p11-guard-receipt.json exporter pid); echo "$XP"     # only when the line above did not print None
# 1. evidence tars (private; runtime-config.json is not included)
f3 "doas -n tar -c -f - --exclude control.sock -C $R guard records runtime/owner launch" > $E/f3-evidence.tar
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r8" > $E/f3-window-evidence.tar
# 2. guest 9p mounts: list, then unmount each listed one on its own line
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"
rockyt "umount $G/m0" | tee $E/p11-umount-m0.txt; echo rc=${PIPESTATUS[0]}   # only if m0 is listed; rc=124 (timeout): record, continue
rockyt "umount $G/m1" | tee $E/p11-umount-m1.txt; echo rc=${PIPESTATUS[0]}   # only if m1 is listed; same
rocky "grep ' 9p ' /proc/self/mountinfo; grep -c ' 9p ' /proc/self/mountinfo"   # count 0 expected; see below if not
# 3. observer removal (banner timeout: check authorized_keys is still $EA before the one retry)
okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r8 --expect-sha256 $EA" < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
# 4. grub check
rocky "grub2-editenv - list"                     # next_entry empty; if set: rocky "$K cancel $KP" (U1) on its own line
# 4a. SELinux denials in the guest (evidence only; "<no matches>" with rc=1 is the good case)
rockyt "ausearch -m AVC,USER_AVC,SELINUX_ERR -ts boot -i; dmesg | grep -i -e avc: -e selinux" > $E/p11-guest-avc.txt 2>&1; echo rc=$?
# 5. guest poweroff
rocky 'systemctl poweroff'
# 6. wait for the child: repeat this line every 10 s, at most 60 times (10 min), until child-gone (h64: the vmm context stays until step 7)
f3 'ps -ax -ww | grep "[h]fs-zq3-r8" || echo child-gone'
# 7. host cleanup (tap, VM context, vars copy)
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 ea34b1818cbac9c1be61af1ccb46afc7d62198200e6e0270c738e192d11ff242 --child-pid $XP" | tee $E/p11-host-cleanup.json   # only if XP is a PID
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 ea34b1818cbac9c1be61af1ccb46afc7d62198200e6e0270c738e192d11ff242 --no-child" | tee $E/p11-host-cleanup.json   # only if the exporter line above printed None
# 8. native FUSE mount: check, then unmount if listed
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount" | tee $E/p11-native-check.txt
f3 "doas -n umount $R/runtime/mnt"               # only if step 8's check listed it; busy or error: stop and preserve
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount"   # must print no-native-mount before step 10
# 9. observer key on f3: this exact plain line, literal paths, never inside a script (zq3-r5)
f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519.pub"
f3 "doas -n ls -la $R/observer"                  # only rocky-known_hosts left
# 10. window restore, always last; it prints nothing on success
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
f3 "doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r8/restoration-summary.json" | tee $E/p11-restoration-summary.json   # the restore's real outcome
```

Step 2 after a FENCED T10: m0's session is blocked since T9 and the host is
gone, so its unmount may refuse or not return. A normal `umount` only, never
`-f` or `-l`: if it refuses, or `timeout 150` ends it (`rc=124`; SSH
keepalives alone do not, because sshd keeps answering), record that and
continue with step 3;
the guest poweroff in step 5 takes the mount down with the guest. A
mountinfo count above 0 after that is recorded, not fixed.

Step 6 waits for `child-gone` only (h64). The run's vmm context
`/dev/vmm/hfs-zq3-r8` is still there after the child exits; step 7's
`host.py cleanup` destroys it with the tap. zq3-r7's step 6 waited for
`no-vmm` as well, which could never appear before step 7.

Step 6: zq3-r3's guest needed about 3.5 minutes to power off after its oops
(R3-4), hence 60 tries. If the child is still there after them, stop: no
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
f3 'ls -ld /var/tmp/homelabfs-9p-poc/share'           # R6: vm-bhyve's normal start needs it
$S/tools/lab/vms.sh --approved-action start-rocky    # only after Phase 11 verified next_entry empty and the child gone
rocky "$K check --expect stock $KP; getenforce; sha256sum /root/.ssh/authorized_keys; free -m"   # authorized_keys must be e08542dc…
rocky 'ls -l /.autorelabel 2>&1; cat /proc/sys/kernel/random/boot_id; journalctl --list-boots --no-pager | tail -n 4' | tee $E/p12-relabel.txt
f3 'doas -n tail -n 30 /zroot/bhyve/rocky/vm-bhyve.log' > $E/p12-vm-bhyve-log.txt
f3 'env TZ=UTC ps -ax -o pid= -o lstart= -o command= | grep "[b]hyve: rocky"' | tee $E/p12-rocky-pid.txt
rocky "$K remove $KP" | tee $E/p12-kernel-remove.json   # U2 (Paul chose removal)
f3 'doas -n vm list; doas -n ls /dev/vmm; ifconfig -l; mount -p | grep -E "fdescfs|fusefs" || echo none; kldstat; zpool list zroot' > $E/p12-final.txt
```

**Relabel record (SELinux gate, R6-3).** Earlier runs rebooted Rocky about
5 minutes after `start-rocky`. Each test kernel had booted without SELinux
and left `/.autorelabel` behind, so the stock boot relabelled and rebooted
once. That reboot shows as a vm-bhyve "bhyve exited with status 0" followed
by "starting bhyve (run 2)". That vm-bhyve line is the deciding evidence,
because the `/.autorelabel` check below only runs after any relabel reboot. The y64 test kernel (c64 config) boots with
SELinux enforcing, so **no relabel reboot is expected** (zq3-r7: none). Write the result into the
run log from three lines:
- `p12-vm-bhyve-log.txt`: the runs since `start-rocky`;
- `p12-relabel.txt`: whether `/.autorelabel` exists at the first SSH answer,
  and the boots `journalctl` lists;
- `p12-rocky-pid.txt`: the bhyve start time.

A relabel reboot, or an `/.autorelabel` still present, is a **finding, not
a failure**. It would mean the test boot ran without SELinux after all, or
something else marked a relabel. Record it and continue.

Wait up to 10 minutes for pinned SSH. Without a relabel, a stock boot answers
in about a minute or two (zq3-r1's U3: 16 s). With one, it answers after
about 5 more minutes. Run the `check` line only once SSH answers. Record the
new PID, lstart and boot ID for the next fill.

Staging prefix deletion is a separate, later step after the evidence is safe
and `mount -p` shows nothing below `/var/tmp/hfs-validation-xz3`. Left on f3
as evidence:
- `$R`, `/var/tmp/hfs-20261110-zq3r8-fdq` and
  `/var/tmp/hfs-fdescfs-windows/zq3-r8`;
- the zq3-r1 to zq3-r7 directories.

Left on Rocky unless Paul decides otherwise:
- `/root/hfs-zq3-zq3-r8-*` and `$G` (retention records, pod rootfs and
  evidence);
- the kernel state journal;
- the zq3-r1 to zq3-r7 leftovers.

fdescfs and fusefs stay loaded on f3 after this run (setup and step 4 load
them), so a later run in the same f3 boot meets D1 again.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r8`): `$S/tools/lab/vms.sh --approved-action start-rocky`
  (test kernel boots once under the normal config); wait up to 10 min for
  pinned SSH; `rocky "$K check --expect test $KP"`; `rocky "$K
  reboot-to-stock $KP"`; then `rocky "$K check --expect stock $KP"`, U2 and
  the Phase 12 checks. zq3-r1 exercised U3 end to end (16 s to SSH). If the
  first boot reboots itself before the `check --expect test` line (a relabel
  reboot, zq3-r6 R6-3; not expected with the SELinux kernel), `next_entry` is
  already consumed: record it as a finding and continue with `check --expect
  stock`, then U2. A hung test kernel stops here: a forced power-off needs
  Paul's explicit decision.
