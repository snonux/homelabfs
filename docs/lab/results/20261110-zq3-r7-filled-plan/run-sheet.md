# zq3-r7 run sheet (filled 2026-11-10)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](facts/) · [zq3-r6 record](../20261110-zq3-r6/README.md) ·
[zq3-r6 sheet](../20261110-zq3-r6-filled-plan/run-sheet.md)

Ordered commands for run `zq3-r7`, the seventh attempt of zq3, with the values
filled on 2026-11-10 from `main` `ba25486` (after c64, d64 and f64). The
candidate is the c64+d64 rebuild `66b5bb2b…`: the c64 guest kernel bzImage
`6f59b593…` (build ID `2ce818d9…`, SELinux in `CONFIG_LSM`) and the d64
daemon `9b7eed8d…` (per-domain view fence, bounded guest reorder window).
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
- **SSH banner timeouts (zq3-r2 R2-5).** A single ProxyCommand SSH to the
  test guest may fail with "Connection timed out during banner exchange".
  Never retry blindly: first prove that nothing ran, with a read-only check
  of the step's own effect (for example `sha256sum /root/.ssh/authorized_keys`
  still `$EA` before retrying the observer removal, `findmnt -t 9p` before
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

## Execution notes (carried over from zq3-r3 to zq3-r7)

- **D1-a is Phase 1a**, right after Phase 1 and before Phase 2 staging and the
  Phase 3 kernel install (zq3-r3 R3-3; fdescfs and fusefs are loaded again,
  left by zq3-r6). If a `kldunload` is refused (busy), stop with nothing staged
  or installed. If only `fusefs` unloaded, that state is harmless: stop and
  report. This replaces plan item A5's "no unload". The change was named in
  the zq3-r3 sheet under Paul's standing 2026-11-09 auto-approval of project
  changes; it is part of this sheet's GO.
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
  f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519.pub"
  ```

- **The window restore's outcome is `restoration-summary.json`, not an exit
  code.** `fdescfs-window.py restore` prints nothing on success (zq3-r5's
  `p11-window-restore.txt` is empty). An `rc` reported through `| tee` or any
  other output filter is the filter's status, not the restore's. Phase 11
  step 10 therefore reads
  `/var/tmp/hfs-fdescfs-windows/zq3-r7/restoration-summary.json`: the restore
  succeeded only if it exists with `"mount_restored": true`.
- **Rocky's first boot after `start-rocky` rebooted itself after about
  5 minutes** in zq3-r1 to zq3-r6: the vm-bhyve log shows a bhyve run that
  exits with status 0 (a guest reboot) and the next run, which stays. The
  bhyve PID in the facts is that later run's, and `boot_indeterminate` keeps
  rising (7 now). The zq3-r6 record explains it (R6-3, tasks c64 and b64):
  every earlier test kernel booted without SELinux, so Rocky's stock boot
  found `/.autorelabel`, relabelled the filesystem and rebooted once. See
  the zq3-r6 notes below and Phase 12.

## Execution notes from the zq3-r6 run (2026-11-10)

zq3-r6 ran as option (b) because its guest kernel had no SELinux; zq3-r7 runs
the full plan. These notes from that run are written into the lines they name:

- **`rockyt … | tee`:** the line ends with `; echo rc=${PIPESTATUS[0]}`, which
  prints rockyt's own exit status (124 = bound expired), not tee's (Phase 10
  T8, Phase 11 step 2).
- **Window restore:** judged only by
  `/var/tmp/hfs-fdescfs-windows/zq3-r7/restoration-summary.json` (Phase 11
  step 10), as in the zq3-r5 notes. zq3-r6's restore exited 0 with
  `"mount_restored": true`.
- **f3 observer key deletion:** the plain literal-path line of Phase 11
  step 9, which the agent safety check accepted in zq3-r6.
- **Rocky's 5-minute reboot was a relabel (R6-3).** A test boot without
  SELinux marks `/.autorelabel`; the next stock boot relabels and reboots once
  (vm-bhyve "run 1 exit 0" about 5 minutes after `start-rocky`). zq3-r6's
  Phase 12 showed it again (run 1 from 00:37:44 to 00:42:41 UTC, then run 2,
  PID 41316). `/.autorelabel` is absent in the 02:05Z facts.

## New for zq3-r7

- **Candidate:** the c64+d64 rebuild `66b5bb2b…` (source `060dea0`).
  - **Guest kernel (c64):** `CONFIG_LSM="lockdown,yama,integrity,selinux,bpf,landlock"`,
    so SELinux is an active LSM. bzImage `6f59b593…`, config `28fe4a8e…`,
    System.map `3d11cf22…`, GNU build ID `2ce818d9…`. The five modules and
    `modules.*` are byte-identical to `e01fab21`, and the 964 patch series
    is unchanged.
  - **Daemon (d64):** `9b7eed8d…`. It keeps one view fence per domain, which
    fixes R6-1, the mount-1 `EKEYREJECTED`. It also admits an unissued guest
    cell once inside a 16-cell reorder window, which fixes R6-2, the
    view-control `ETIMEDOUT`.
  - **Window artifacts:** rebuilt from `060dea0`: `controlledruntime.test`
    `56e87332…` and `fdescfs-namespace` `45f2d59a…`.
  - The exporter `36423b6d…` and the init helper `8c02b98f…` are unchanged.
- **Phase 4 step 1a is required again:** the daemon bytes are new, so the
  zq3-r5 qualification of the z54 daemon does not carry over (xz3-20261110g).
- **Tools rebuilt** at `ba25486`: `hfs-runtime-prep` `dbebb5a8…` and the
  signer `9ca59cee…`. d64 changed non-test sources in their Go closure
  (`internal/controlledruntime`, `internal/guestbridge` and
  `internal/viewcontrol`). Two builds of each were byte-identical.
- **Re-pinned:** the kernel plan, the bundle, the argv record, the batch and
  bootstrap templates, the window pins and plan inputs, `rocky_kernel.py`
  and `controller.py` (f64 admits only `66b5bb2b…`) and `argv.py` (a comment
  only).
- **Rocky facts:**
  - bhyve PID `41316`, UTC lstart `Tue Nov 10 00:42:41 2026`: run 2 after the
    relabel reboot that followed zq3-r6's Phase 12 `start-rocky`;
  - boot ID `2ca051f4-…` and `boot_indeterminate=7`;
  - UEFI vars `00f82a83…`, hence a new argv record;
  - `fdescfs.ko` (id 29) and `fusefs.ko` (id 30) are loaded again, left by
    zq3-r6, so Phase 1a runs again.
- **SELinux is a real gate now:**
  - Phase 1 stops if `/.autorelabel` exists on Rocky. The test boot under the
    exporter would then relabel and reboot.
  - Phase 9 requires `getenforce` to print `Enforcing` in the test guest;
    anything else goes to Phase 11 with the runtime still serving.
  - Phase 12 records whether a relabel happened after the stock boot. With the
    SELinux test kernel none is expected; one is a finding, not a failure.
- **Phase 8 to 10:** zq3-r6 reached mount-0 and READY, then mount-1 failed on
  the fence. With d64, expect both mounts, READY with every step `ok`, the
  batch fill and T1-T11.
  - Every Phase 9-10 line was rechecked against the committed
    `controller.py`, `correlate.py`, `guest.py` and `guest-lock-probe.py`,
    then dry-run end to end.
  - `p9-ready.py` and `p9-observe.py` were also run on zq3-r6's real READY and
    OBSERVE records ([README](README.md#phases-9-10-recheck)).

## Session setup (workstation, bash)

```bash
source /home/paul/tmp/claude/hfs-zq3-r7-fill/session.sh
```

`session.sh` (`2bd3f894…`, [pins](pins.json) `fill_helpers`) defines, adapted
from zq3-r6's:

```bash
D=/home/paul/tmp/claude/hfs-zq3-r7-fill      # private 0700 fill dir
S=$D/snapshot                                # git archive of ba25486 (docs internal tools)
R=/var/tmp/hfs-validation-xz3/zq3-r7         # f3 staging prefix (parent exists, holds zq3-r1 to zq3-r6)
G=/var/tmp/hfs-zq3-zq3-r7                    # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
okpin() { …observer_key.py cd7f38c2… check… }   # before every stdin invocation of observer_key.py
rockyt(){ …rocky with a local `timeout 150` around the real ssh, </dev/null; exit 124 = bound expired (read it with `; echo rc=${PIPESTATUS[0]}`)… }
hpin()  { …sha256sum -c of bin/sign_watch.py (0baecfb7…), the signer bin/hfs-retained9p-operator (9ca59cee…, rebuilt at ba25486) and the eleven sh/* helpers… }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r7-kernel-stage/rocky-kernel-plan.json --plan-sha256 4b2e2212622ddac153abf4aa508feceeca8a0f95737672ce6a0777449c0dbcf3 --state /root/hfs-zq3-zq3-r7-kernel-state'
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261110-zq3r7-fdq/native-test-guard.py --root /var/tmp/hfs-20261110-zq3r7-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 41316 --rocky-birth 'Tue Nov 10 00:42:41 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
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

## D1: f3 module state (decided: D1-a, Phase 1a)

The 02:05Z facts show `fdescfs.ko` (id 29) and `fusefs.ko` (id 30) loaded on
f3, left by zq3-r6 (its Phase 1a unloaded zq3-r5's, its Phase 4 loaded them
again; same `kern.boottime` since zq3-r2). The window plan filler
`fdescfs-window-plan.py` requires both absent. With the committed pins
(`fdescfs_module: absent`, `modules_forbidden: [fdescfs.ko, fusefs.ko]`) it
refuses with `fdescfs module present; plan is for absent`. The fill
reproduced this locally again with the r7 pins, using post-restore window
facts with both modules loaded, as on f3 now. The same pins fill with
zq3-r6's real Phase 4 facts (modules absent after its D1-a), with the three
artifact hashes in its host hash list swapped for the new ones. zq3-r3 to
zq3-r6 all ran D1-a (a normal `kldunload` of both, never refused), so this
sheet runs it as Phase 1a again. D1-b (a reviewed tool change) and D1-c (an
f3 reboot) remain plan revisions, not options at run time.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-10 by the fill: `hfs-runtime-prep` and the signer rebuilt at
`ba25486` (d64 changed non-test Go sources in their closure; proof in the
[README](README.md)), bundle (same w54 guest budget), argv record (from the
02:05Z launch facts, UEFI vars `00f82a83…`), kernel plan (c64+d64 candidate
`66b5bb2b…`, new kernel and config, same modules), templates, the filled
bootstrap plan (F1), staging trees and tarballs, r7 window pins (new window
artifacts), init helper (zq3-r1's reproduced `8c02b98f…`). Local tests passed
at `ba25486` (see [README](README.md#local-verification)).
Re-verify immediately before Phase 1:

```bash
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 53e384b9160e3c620b693ab28a2314ec410580bf17635c59edc32793b3d887d7 --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 53e384b9160e3c620b693ab28a2314ec410580bf17635c59edc32793b3d887d7
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json \
  --plan-sha256 edf849a8823fa3f1990750668c142c28e2b880becfeac76c27dfc06f64843681
hpin && okpin && sha256sum -c <<'EOF'
16ff1ee36968dcf79fafbc9dcd97079240671e4bd0cfd9c63f4c29463be0e7e3  /home/paul/tmp/claude/hfs-zq3-r7-fill/stage/f3-stage.tar
84960ed5f52f7d03d4d2ca92d061efbe44eddde8661226c24292b3d65ca0180f  /home/paul/tmp/claude/hfs-zq3-r7-fill/stage/rocky-stage.tar
4b2e2212622ddac153abf4aa508feceeca8a0f95737672ce6a0777449c0dbcf3  /home/paul/tmp/claude/hfs-zq3-r7-fill/kernel/rocky-kernel-plan.json
9fbe2c3ac344dd7ab93333a82c9e07ecadeb01d1652518ea663e7f6c17da04aa  /home/paul/tmp/claude/hfs-zq3-r7-fill/controller/batch-plan.TEMPLATE.json
1947e81c5252203c83dcd280812ea21e28ca48078173db2993617f9293233339  /home/paul/tmp/claude/hfs-zq3-r7-fill/fdescfs-pins/pins.json
2bd3f894115b9768bf9e905557246281bdf5f88fadfb38062202c970283d3e27  /home/paul/tmp/claude/hfs-zq3-r7-fill/session.sh
EOF
cd $D
```

The tools snapshot `$S` is `ba25486`, the main HEAD at fill time; no refresh
is planned. If main moves before the run, `git diff --stat ba25486 $FINAL --
tools/ internal/controlledruntime/probe docs/lab/f3-host-known_hosts
docs/lab/rocky-known_hosts docs/lab/zq3-fdescfs-window` must be empty (or
touch only tests and docs); otherwise the changed pins make this a plan
revision. If a fill step is interrupted, a leftover `NEW.next` file makes
reruns refuse with "File exists": inspect it, then remove it by hand before
retrying.

**Operator key (before Phase 8, ideally now):** on Paul's instruction Claude
copies the operator signing key to
`/home/paul/tmp/claude/hfs-zq3-r7-fill/operator/operator-signing-key` (the
empty `0700` directory exists): a copy (one link), owned by `paul`, mode
`0600`, unencrypted Ed25519 PKCS#8 PEM or a 64-hex seed without newline,
matching public key `2f6d909c…`. The bundle's request template records
exactly this path; nothing in the fill reads it. A different path means
regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8, including the
Phase 1a unload). Paul delegated both signatures (A7) to Claude through the
watcher in Phase 8.

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 2026-11-10 02:05Z facts ([facts/](facts/)) are the baseline. Immediately
before Phase 1a:

```bash
hpin
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); f=d["files"]; print([f[p]["sha256"] for p in ("/zroot/bhyve/rocky/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run", "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", "/zroot/bhyve/rocky/uefi-vars.fd")], f["/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"]["link"], d["bridge_members"], d["vmm"], d["interfaces"])' $E/p1-launch-facts.json
f3 sh -s < $D/sh/p1-f3.sh > $E/p1-f3.txt
rocky 'cat /proc/sys/kernel/random/boot_id; uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /; ls /boot | grep -c hfs-xz3; ls -d /etc/homelabfs /root/hfs-zq3-zq3-r7-* /var/tmp/hfs-zq3-zq3-r7 2>&1; ls -l /.autorelabel 2>&1; grep -E "^SELINUX(TYPE)?=" /etc/selinux/config' > $E/p1-rocky.txt
```

Continue only if all of these hold:

- **f3:**
  - kern.boottime is `{ sec = 1794212992, usec = 931825 } Mon Nov  9
    10:29:52 2026`;
  - Rocky's bhyve PID is `41316` with UTC lstart `Tue Nov 10 00:42:41 2026`;
  - freebsd/hfs-test0..2 are Stopped, `/dev/vmm` = `rocky` and swap is
    unused;
  - `fdescfs.ko` and `fusefs.ko` are loaded (as at 02:05Z), with no fusefs or
    fdescfs mount and `/dev/fd` on devfs;
  - `/var/tmp/hfs-validation-xz3` and `/var/tmp/hfs-fdescfs-windows` are both
    `drwx------ root wheel`, and all three r7 paths are `absent`;
  - the PoC share is present and f3 `/usr/bin/ssh` is `6a9e5573…`.
- **Launch facts:** `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware
  `6b48e967…` (link `../edk2-bhyve/BHYVE_UEFI.fd`), **vars `00f82a83…`**,
  bridge members `re0 tap0` (no `tap100`).
- **Rocky:**
  - boot ID `2ca051f4-1293-4909-90fa-c450ced6e925`;
  - `5.14.0-687.66.1.el9_8.x86_64` running and default, `next_entry=` empty,
    `saved_entry=b26c0ac534534d7fb81b3e0698558bc3-5.14.0-687.66.1.el9_8.x86_64`;
  - `Enforcing`, and `/etc/selinux/config` has `SELINUX=enforcing`;
  - **`/.autorelabel` absent** (`ls` reports "No such file or directory");
  - `authorized_keys` `e08542dc…` and python3.9 `1143733d…`;
  - no `hfs-xz3` file in `/boot`, and `/etc/homelabfs` and every r7 path are
    absent. The r1 to r6 leftovers (`/root/hfs-zq3-zq3-r{1..6}-*` and
    `/var/tmp/hfs-zq3-zq3-r{1..6}`) stay as evidence.

`boot_indeterminate=7` is expected (6 at zq3-r6; zq3-r1 R7; it rises by one
per Rocky start). The interface order in the launch facts may differ
(02:05Z: `wg0` before `tap0`); only the set and the bridge members count. The
f3 run directories now include zq3-r6's (`/var/tmp/hfs-20261110-zq3r6-fdq`
and the zq3-r6 window and staging directories).

**`/.autorelabel` present: stop** before Phase 1a, with nothing changed. The
test kernel's boot under the exporter would find it, relabel and reboot the
guest in the middle of Phase 8. Report it: it means a boot without SELinux
happened since 02:05Z. Any other difference also stops and revises the plan.
A changed vars hash means a new argv record. Another Rocky restart means a
new PID and lstart for `NG` and `p5-recheck.py`.

## Phase 1a: unload the modules zq3-r6 left loaded (A5, D1-a, about 2 min)

Right after Phase 1, before anything is staged. Each line on its own:

```bash
f3 'kldstat -v -n fusefs; kldstat -v -n fdescfs; mount -p | grep -E "fusefs|fdescfs" || echo no-fuse-fdesc-mounts' > $E/p1a-modules-before-unload.txt
f3 "doas -n kldunload fusefs"      # normal unload; "Device busy" or any error: stop, never -f
f3 "doas -n kldunload fdescfs"     # same
f3 'kldstat' > $E/p1a-modules-after-unload.txt   # neither fusefs.ko nor fdescfs.ko listed
```

Expect `Refs 1` for both files (`fusefs.ko` id 30, `fdescfs.ko` id 29) and
`no-fuse-fdesc-mounts` before the unload. After it, `kldstat` matches the
zq3-r2 Phase 1 list (`kernel`, `vmm.ko`, `nmdm.ko`, `if_bridge.ko`, …), as
after the zq3-r3 to zq3-r6 D1-a. Then Phase 4 loads `fdescfs` in `setup` and
`fusefs` in step 4.

## Phase 2: stage on f3 (A2, about 3 min)

The parent `/var/tmp/hfs-validation-xz3` already exists (it holds zq3-r1 to
zq3-r6), so this checks it instead of requiring it absent. `paul` cannot look
inside a root `0700` directory, so absence is tested through `doas`. The
tarball has no entry for the parent, so extraction never rewrites its mode or
owner.

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-validation-xz3 /var/tmp/hfs-fdescfs-windows"   # both drwx------ root wheel
f3 "doas -n test ! -e $R && doas -n test ! -e /var/tmp/hfs-20261110-zq3r7-fdq && echo absent"   # must print absent
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261110-zq3r7-fdq > $E/p2-dirs.txt
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
d64 daemon `9b7eed8d…` (new bytes), bhyve `36423b6d…`,
`controlledruntime.test` (`56e87332…`) and `fdescfs-namespace` (`45f2d59a…`,
both rebuilt from `060dea0`), the G3 `homelabfs` helper (`0500`), the
controllers and `host.py`/`argv.py` (`5147a871…`, comment-only change)/`host_snapshot.py`/
`runtime-volume-init.py` and `runtime-guard.py` (`fece96b3…`, unchanged)
(`0400`), the argv record, the volume-init plan, the private runtime config
(holds host receipt keys) and the Rocky pin file; the native guard
(`576d802a…`, unchanged) and `runner_process.py` go to
`/var/tmp/hfs-20261110-zq3r7-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

One command per line:

```bash
rocky 'for d in /root/hfs-zq3-zq3-r7-kernel-stage /root/hfs-zq3-zq3-r7-kernel-state /root/hfs-zq3-zq3-r7-incoming /root/hfs-zq3-zq3-r7-pod /root/hfs-zq3-zq3-r7-tools /var/tmp/hfs-zq3-zq3-r7 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r7-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r7 --observer /root/hfs-zq3-zq3-r7-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r7-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
PM=$(jf $E/p3-pod-manifest.json manifest_sha256); echo "$PM"   # review (expected f7f69339…), then:
rocky "TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-pod/pod_fixture.py build --run-id zq3-r7 --expect-manifest-sha256 $PM" | tee $E/p3-pod-build.json
```

(Rocky's shell is bash, so `VAR=value` prefixes are fine there.) Expect:
install reports one new BLS entry (`<BLS_ENTRY_ID>`) for the c64 kernel
(bzImage `6f59b593…`, build ID `2ce818d9…`, config `28fe4a8e…` with SELinux
in `CONFIG_LSM`; same release `6.1.190-hfs-xz3` and the same five modules as
964), default still
`5.14.0-687.66.1.el9_8.x86_64`, `saved_entry` unchanged, `next_entry` empty;
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. zq3-r1 to zq3-r6 all got `f7f69339…` (82
entries); a different digest is not a failure but should be explained before
`build`. `PM` feeds the pod request in Phase 8. If Paul declines A8, skip both
pod lines and use `PM=skip` (T7 unrun). Failure: stop before Phase 5 and run
U2 (`$K remove $KP`) while Rocky still runs.

## Phase 4: f3 window, fusefs, observer key (A5, A6; B for facts/tests; about 10 min)

Step 0, read-only facts (B). Phase 1a already unloaded both modules. Library
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
f3 'sha256 -r /usr/bin/ssh' | diff - $D/facts/f3-usr-bin-ssh-20261110T020504Z.sha256 && echo F1-unchanged
f3 'env TZ=UTC ps -p 41316 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
```

Expect `p4-python-libs.sha256` to list exactly the real paths
`/usr/local/lib/libpython3.12.so.1.0`, `/usr/local/lib/libintl.so.8.4.6`,
`/usr/lib/libdl.so.1`, `/lib/libutil.so.10`, `/lib/libm.so.5` (the closure of
zq3-r1 to zq3-r6; any other library is a fact change to review),
`F1-unchanged` (otherwise stop: the filled bootstrap plan pins `6a9e5573…`,
and refilling F1 is a plan revision), and `Tue Nov 10 00:42:41 2026` with
`/dev/vmm` = `rocky` and neither module in `kldstat`.

Split the facts and fill the window plan on the workstation (B) with the r7
window pins (`1947e81c…`: the committed pins as f64 left them, with
`zq3-r1` → `zq3-r7`; daemon `9b7eed8d…`, helper `45f2d59a…`, test
`56e87332…`), then
review the printed SHA256. A refusal here (for example `fdescfs module
present`) means Phase 1a did not hold: stop, nothing on f3 has changed since.
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

F1 (bootstrap `observer.ssh_sha256`) was filled at fill time from the 02:05Z
fact (`fill_plan.py bootstrap`, plan `edf849a8…`, `bootstrap.py check`
passed); the `F1-unchanged` line above is its recheck. Nothing to fill here.

Step 1, negative before setup (B; a skip counts as unrun). No `--` before the
command; a retry takes a new stage name and keeps the refused one. The o54
host profile needs free ≥ 1 GiB and free + inactive ≥ 3.5 GiB (02:05Z: 5.02 GB
free, 3.25 GB inactive):

```bash
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v" | tee $E/p4-unsupported-before.txt
```

Step 1a (B, **required**): process observer qualification of the exact
daemon bytes. The c64+d64 package carries the new d64 daemon `9b7eed8d…`, so
the zq3-r5 qualification of the z54 daemon `39c92d01…` (reused in zq3-r6)
does not carry over (xz3-20261110g, "What the next run must do again").

The staged daemon runs its read-only qualification once:
- under the native guard's host profile (Rocky pinned, 60 s, 512 MiB);
- as root through `doas`, the credentials `runtime-guard.py` later starts it
  with;
- before setup, while `/dev/fd` is still devfs, which this check does not
  need (the f44 run passed on devfs too).

It reads no config, starts no child and mounts nothing. Each line on its own:

```bash
f3 "$NG --stage process-observer $R/bin/hfs-controlled-runtime --check-process-observer" | tee $E/p4-process-observer-guard.json
f3 "doas -n cat /var/tmp/hfs-20261110-zq3r7-fdq/checks/process-observer/stdout" > $E/p4-process-observer.json
f3 "doas -n cat /var/tmp/hfs-20261110-zq3r7-fdq/checks/process-observer/stderr" > $E/p4-process-observer.stderr
python3 -I -B $D/sh/p4-observer.py $E/p4-process-observer-guard.json $E/p4-process-observer.json $E/p4-process-observer.stderr | tee $E/p4-process-observer-check.json
```

Pass criteria (`p4-observer.py`, `"pass": true`, exit 0):
- the guard ran exactly `$R/bin/hfs-controlled-runtime
  --check-process-observer`, with `error` null and child exit 0;
- stderr is empty;
- stdout is one `homelabfs-process-observer/1` record with `platform`
  `freebsd/amd64`, `executable` the staged path, **`sha256` `9b7eed8d…`** and
  `publication` null;
- `uid` 0 and `gid` 0 (the gid among `groups`), a positive `pid`, an
  `incarnation` `freebsd:<pid>:…` and a positive `text_vnode`.

zq3-r5 and zq3-r6 passed exactly these criteria as root for the z54 daemon
(uid/gid 0, groups `[0, 0, 5]`). zq3-r6's real records, renamed to zq3-r7,
fail this check only on the `sha256`.

**Failure: stop before Phase 5**, so Rocky is never stopped. Nothing is
selected yet, the window is not set up and no observer key exists. Keep
`checks/process-observer` on f3 as evidence, run U2 (`rocky "$K remove
$KP"`) while Rocky runs, record and report: the d64 daemon is not
qualified, and no runtime may start with it. The modules Phase 1a unloaded
stay unloaded, which is harmless. Only a guard refusal before the child
started (receipt `exit` null, `error` naming a guard bound such as the
free-page floor) may be retried, once, as `--stage process-observer-2` after
recording the cause. A child exit other than 0 or a record mismatch is never
retried.

Steps 2-4 (A5): setup, qualify (under the guard), fusefs. The root-private
parent `/var/tmp/hfs-fdescfs-windows` exists (`root wheel` `0700`, with
`zq3-r1`, `zq3-r1.refused-setup-1`, `zq3-r2` to `zq3-r6`); it is checked
here, never created. `setup` refuses an existing run directory:

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-fdescfs-windows"   # drwx------ root wheel /var/tmp/hfs-fdescfs-windows
f3 "doas -n test ! -e /var/tmp/hfs-fdescfs-windows/zq3-r7 && echo absent"   # must print absent
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 $WP --directory /var/tmp/hfs-fdescfs-windows/zq3-r7"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots (or a later
approved unload). If setup or qualify refuses and a reviewed fix follows,
resume as zq3-r1 did: keep the refused window directory under a new name
(`zq3-r7.refused-setup-1`, via `printf … | f3 doas -n sh -s`), and rerun
step 0 and the window plan fill with `-resume` names (`p4-*-resume.*`, `--out
$D/fdescfs-plan-resume`, remote `$R/records/fdescfs-plan-resume.json`, `WP`
and `W` pointing at it). The plan's expiry also forces this refill after a
pause of more than 55 minutes.

Step 5 (A6), run-only observer key:

```bash
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r7-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r7-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r7 --public-key /root/hfs-zq3-zq3-r7-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
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
f3 'sysctl -n kern.boottime' > $E/p5-boottime.txt              # expect: { sec = 1794212992, usec = 931825 } Mon Nov  9 10:29:52 2026
f3 'env TZ=UTC ps -p 41316 -o lstart=' > $E/p5-rocky-lstart.txt  # expect: Tue Nov 10 00:42:41 2026
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime" > $E/p5-facts.json
f3 doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r7/owned.json > $E/p5-owned.json
python3 -I -B $D/sh/p5-recheck.py $E/p5-boottime.txt $E/p5-rocky-lstart.txt $E/p5-facts.json $E/p5-owned.json | tee $E/p5-recheck.json
```

`p5-recheck.py` passes (exit 0, `"pass": true`) only if the boot time (also
the `boot` field of `facts`) and the UTC lstart equal the values above, and
the parsed `descriptor` of `facts` equals the parsed `owned.json` (a
supported `fdescfs` report with the owned FSID).

Step 1a, memory pre-estimate (B) and the disk-level reference:

```bash
f3 'doas -n vm list; swapinfo; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_active_count vm.stats.vm.v_laundry_count vm.stats.vm.v_wire_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min; ps -p 41316 -o rss=' > $E/p5-mem.txt
python3 -I -B $D/sh/p5-estimate.py $E/p5-mem.txt | tee $E/p5-estimate.json
```

`p5-estimate.py` prints two estimates of what Phase 6 will measure. The
zq3-r2 sheet's `rss_based` one, free + inactive + (ARC − `c_min`) + Rocky's
bhyve RSS, is **pessimistic**: on f3 Rocky's guest memory shows as Active
pages, not in the bhyve RSS. The better estimate is `active_based` = free +
inactive + (ARC − `c_min`) + Active. It is an upper bound, because
v_active_count also holds the host's own Active pages. zq3-r3 measured
`active_based` 15.14 GB here (margin 0.38 GB), zq3-r4 15.19 GB (margin
0.43 GB), zq3-r5 15.23 GB (margin 0.46 GB) and zq3-r6 15.22 GB (margin
0.45 GB); all four drained. At 02:05Z inactive was 3.25 GB, about 4.6 GB
less than at zq3-r6's facts (7.89 GB), most likely because Rocky's fresh
boot turned it Active; `active_based` counts both, so the estimate should be
similar, and Phase 6 decides.

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
0.18 GB, in zq3-r5 about 0.15 GB and in zq3-r6 about 0.17 GB. The drain is the same
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
  /var/tmp/hfs-fdescfs-windows/zq3-r7"` and
  `f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r7" >
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
(margin 0.54 GB each), zq3-r5 15,294,364,056 (margin 0.53 GB) and zq3-r6
15,329,786,840 (margin 0.57 GB).
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
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 53e384b9160e3c620b693ab28a2314ec410580bf17635c59edc32793b3d887d7" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 22c6c8ef3eb5df368a4ea5b5f20c5d3883439ca2a20269491943934935b6f478 --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r7 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`00f82a83…` (any Rocky boot since 02:05Z changes it; it was `bcc505d5…` at
zq3-r6, and Rocky started twice since, at 00:37:44 and 00:42:41 UTC): stop;
regenerating the argv record from fresh facts (same sessions,
same args) is a plan revision for review, never an on-the-spot edit. Failure
with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3. `k04-baseline.json` is T11's
required baseline (the empty mount point before the runtime starts).

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s, about 5 min with prep)

Signing is delegated to Claude (A7, Paul's decision): the driver's stdout goes
through the watcher `bin/sign_watch.py` (`0baecfb7…`, zq3-r6's watcher with
`D`, comments and the signer pin changed). It copies every line through and, for an
`operator_action` line, runs the signer only if the printed command is
exactly one of the two commands below, the request file's SHA256 equals the
printed `request_sha256` and the signer still hashes to `9ca59cee…`; each
action at most once. Anything else is logged as `"watcher": "refused"` and
left unsigned, so the driver times out (fails closed). The signer pin lives
in two places, `hpin` (`session.sh`) and the watcher's `SIGNER_SHA256`. The
signer is rebuilt at `ba25486`: it links `internal/controlledruntime`,
`internal/guestbridge` and `internal/viewcontrol`, whose non-test sources d64
changed, so zq3-r5's `89905914…` (reused in zq3-r6) is retired.
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
  --plan-sha256 edf849a8823fa3f1990750668c142c28e2b880becfeac76c27dfc06f64843681 --output $D/bootstrap-run \
  | python3 -I -B $D/bin/sign_watch.py | tee $E/p8-bootstrap.jsonl
```

The plan is re-derived from zq3-r6's (the q54 template that ran): it pins
`runtime-guard.py` `fece96b3…` (unchanged), the d64 daemon `9b7eed8d…`
(`runtime.sha256`, also the guard's `--runtime-sha256` below; new), the
r7 kernel plan `4b2e2212…`, `rocky_kernel.py` `792b7aef…` (f64) and
`hfs-runtime-prep` `dbebb5a8…` (rebuilt at `ba25486`), which fills the
enrollment and bind requests with the r7 bundle's guest budget (unchanged:
reservations 1024, received 256, 512/512 MiB).
The driver launches, under the outer guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r7/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r7/guard --runtime-sha256 9b7eed8dc9b98f4a61af000bb17b5008672a7083075e206951fc96b53577088b --vm-name hfs-zq3-r7 --disk-root /var/tmp/hfs-validation-xz3/zq3-r7 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r7 --skip-path /var/tmp/hfs-validation-xz3/zq3-r7/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r7/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r7/runtime/runtime-config.json --disposable-validation
```

(guard limits = its defaults = plan ceilings: 3600 s, runtime 4 GiB + 256 MiB
RSS, bhyve 6 GiB + 512 MiB, disk 4 GiB, 8 GiB free, no swap, 512 MiB
available memory, one SIGTERM on breach. `runtime-guard.py` drops its own
leading `--`, unlike the native guard. It excludes its own lineage from
`foreign-bhyve` (p54, zq3-r2 R2-2) and accepts exactly `RUNTIME --config
CONFIG --disposable-validation` (q54, R2-3).)

The two signer commands the watcher accepts (the driver prints them as
`"operator_action": "sign-enroll"` and `"sign-bind"`):

```
/home/paul/tmp/claude/hfs-zq3-r7-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r7-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/signer-binding
```

Expected order: host-preflight, launch, proposal, guest-ready (test kernel;
GRUB consumed `next_entry`), admission (manifest SHA256, boot ID, provider
nonce, `virtioN` names), enroll-request, **sign-enroll** (`"watcher":
"signed"`, exit 0), provision, enrollment-handoff, awaiting-bind,
bind-request, **sign-bind** (signed, exit 0), bind, mount-0, mount-1, READY.

What zq3-r6 got and what d64 changes:
- zq3-r6 got through bind with both signatures and mount-0 (the first real
  HomelabFS 9p mount, 964 confirmed). The runtime reached READY (domains 2).
- Mount-1 then failed with `mount(2) system call failed: Key was rejected by
  service`. This was R6-1: the runtime kept one view fence for all domains,
  so mount-1's first lease carried fence 2 while the guest expected 1.
- The guest also logged `view control worker stopped, error -110` twice.
  This was R6-2: an out-of-order guest cell was refused as stale, and both
  VIEW_WAITs stalled until the guest's 30 s timeout.
- d64 keeps the fence per domain (ledger schema 2) and admits an unissued
  guest cell once inside a 16-cell reorder window.

**Both mounts should now complete and READY arrive with every step `ok`**,
well inside the 120 s bootstrap deadline. Phases 9 and 10 would then run in
full for the first time. The driver never reads the key.

Failure, a watcher refusal or `uncertain`: nothing is replayed; inspect
`$D/bootstrap-run/journal.jsonl` and `$E/p8-bootstrap.jsonl`, collect the
failure evidence read-only (as in zq3-r3: `p8-fail-guest.txt` with `uname
-r`, boot ID, `dmesg | grep -E "9p|virtio|hfs"` and the virtio driver list;
the full guest `dmesg` as `p8-guest-dmesg.txt` if anything oopsed;
`p8-fail-host.txt`, `p8-fail-exporter.txt`), then go to Phase 11. **A mount
failure is never retried in the same guest boot** (rules above). The runtime
then either stays in `AWAITING_GUEST_BIND` until it exits at its 120 s
bootstrap deadline, or, as in zq3-r6, reaches READY on the mounts that did
complete and keeps serving: then Phase 11 goes through its guard-SIGTERM
branch. A failed mount after w54 should return an error instead of
oopsing: an `ENOSPC` in the guest log would point at the guest budget, an
oops at a new kernel defect. If no guest kernel ran (no GRUB output),
`next_entry` is unconsumed: U3 after the child is positively gone.

**If a mount fails or blocks** (guest-mount's 30 s bound gives `uncertain`,
or a mount step fails while the runtime still runs, as mount-1 did in
zq3-r6): from a second shell (`source
/home/paul/tmp/claude/hfs-zq3-r7-fill/session.sh`), take the live owner
snapshot as zq3-r4 to zq3-r6 did, read-only and while the runtime runs:
`f3 "doas -n tar -c -f - --exclude control.sock -C $R runtime/owner" >
$E/p8-owner-live.tar`. A mount-1 `EKEYREJECTED` or a guest `view control
worker stopped, error -110` after d64 would be a new finding against the
d64 fixes: record the guest's mount process state (`ps -o
pid,stat,wchan:32,cmd -C mount`) and the guest `dmesg | grep -E
"9p|hfs|SELinux"` with the read-only failure evidence above.

**After READY (B, at once; the guard's 3600 s bound runs from the launch).**
Fill the controller batch plan (F6/F7) from the run's recorded evidence,
check it independently, and fill the READY binding and the T6/T7/T8 guest
requests. The filler takes the boot ID, provider nonce and manifest SHA256
from the journal's `guest-ready` and `admission` results (cross-checked with
`work/enroll-request.json`) and the m0/m1 mount IDs from the first field of
each mount step's `mountinfo`; it refuses unless the run is READY with every
step `ok`, every value agrees, and the plan passes the controller's own
validation, and it never overwrites an existing plan. One line each:

```bash
python3 -I -B $S/tools/lab/retained9p/fill_plan.py batch --template $D/controller/batch-plan.TEMPLATE.json \
  --template-sha256 9fbe2c3ac344dd7ab93333a82c9e07ecadeb01d1652518ea663e7f6c17da04aa \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 edf849a8823fa3f1990750668c142c28e2b880becfeac76c27dfc06f64843681 \
  --output $D/controller/batch-plan.json | tee $E/p8-batch-fill.json
B=$(jf $E/p8-batch-fill.json sha256); echo "$B"
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.json | tee $E/p8-batch-check.json   # plan_sha256 must equal $B
python3 -I -B $D/sh/p9-ready.py $D/bootstrap-run | tee $E/p9-ready.json
U=$(python3 -I -B $D/sh/p9-ready.py --runtime $D/bootstrap-run); echo "$U"
python3 -I -B $D/sh/p10-requests.py $D/controller/batch-plan.json $B $PM $E | tee $E/p8-requests.json
```

`p9-ready.py` checks `summary.json` (`ready`) and the journal's READY record
(`CONTROLLED_LOCAL`, two domains, publication null, control socket
`$R/runtime/owner/control.sock`) and prints the runtime UUID, cut and epoch.
`p10-requests.py` writes `$E/lock-m0.json` (T6), `$E/lock-m1-t8.json` (T8)
and, unless `PM=skip`, `$E/pod-request.json` (T7), with the mount IDs taken
from the checked batch plan. On a refusal of any of these nothing was
written and the runtime keeps serving under the guard's bounds. A wrong
argument (path, pin) may be corrected and the line rerun; a mismatch in the
evidence itself is a run finding: never hand-edit a plan or request around
it, go to Phase 11 (inspection first, runtime still serving).

## Phase 9: READY admission and the SELinux gate (B, 1 min)

```bash
C="$R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock"
f3 "doas -n $C --operation observe --runtime $U --sequence 1" > $E/observe-001.json
python3 -I -B $D/sh/p9-observe.py $E/observe-001.json $E/p9-ready.json $D/controller/batch-plan.json $B | tee $E/p9-observe-check.json
rocky 'findmnt -t 9p; getenforce; cat /sys/fs/selinux/enforce; uname -r; dmesg | grep -E "LSM: initializing|SELinux: +Initializing"' | tee $E/p9-guest.txt
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
- exactly two 9p mounts (`hfs0` on `$G/m0`, `hfs1` on `$G/m1`);
- **`getenforce` printing `Enforcing`** and `/sys/fs/selinux/enforce` `1`.
  `guest.py` admits every case and lifecycle step only with enforce = 1, and
  `pod_fixture.py` needs SELinux for T7;
- `6.1.190-hfs-xz3`;
- an `LSM: initializing lsm=…selinux…` line and a `SELinux:  Initializing.`
  line from the c64 kernel.

**Any miss stops before T1.** That includes `Permissive`, `Disabled`, a
missing `/sys/fs/selinux` and a failed OBSERVE check. Record it as a finding
and go to Phase 11 through its guard-SIGTERM branch, with the runtime still
serving. Nothing is relabelled, set or retried around it; zq3-r6's option
(b) does not apply to this run. A missing `LSM`/`SELinux:` dmesg line with
`Enforcing` and enforce `1` is recorded, not a stop: the ring buffer may
have wrapped.

## Phase 10: gate matrix (B inside the window, about 25-35 min)

Order **T1-T7, T9, T10, T11, T8**; stop dependent steps on the first failure
(dependencies are named per step). Results are pass, fail, unsupported, unrun
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
directly for ARM and RESTART. Expected sequences if nothing is lost: case
baselines/retained 2/3, 4/5, 6/7, 8/9, 10/11; T9 pre-arm 12, ARM 13,
post-CALL 14; T10 post-quiesce 15, RESTART 16. A refused or lost command is
never resent with the same sequence (rules above).

**Per case (T1-T5).** Baseline OBSERVE right before the dispatch, the case
(`probe` for the first, `next` afterwards), the retained OBSERVE while its
payload is in the guest, `correlate.py case`, then `release`. The correlator
refuses unless both OBSERVEs are complete and SERVING on the same runtime,
cut and epoch, the applied position advanced, and (from the second case on)
the baseline follows the previous case's release in the same incarnation.
Expect `probe`/`next` to end with the case `payload-retained-awaiting-correlation`
and `release` with it `released` (each prints the controller state). A
correlator refusal leaves the payload retained: stop (T1-T5 incomplete),
Phase 11 with the runtime serving.

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
```

T5, `mapped-pressure`:

```bash
obs obs-namespace-retained obs-mapped-pressure-baseline
$CTL next --output $OUT --sha256 $B | tee $E/t5-next.json
obs obs-mapped-pressure-baseline obs-mapped-pressure-retained
python3 -I -B $CR case --output $OUT --sha256 $B --case mapped-pressure --baseline $E/obs-mapped-pressure-baseline.json --baseline-sha256 $(sha $E/obs-mapped-pressure-baseline.json) --retained $E/obs-mapped-pressure-retained.json --retained-sha256 $(sha $E/obs-mapped-pressure-retained.json) --record $E/corr-mapped-pressure.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-mapped-pressure.json --correlation-sha256 $(sha $E/corr-mapped-pressure.json) | tee $E/t5-release.json
$CTL status --output $OUT > $E/t5-status.json
jf $E/t5-status.json syscall_batch_success      # must print True
```

Mid-release failure (run plan): the guest consumes the retention record
before its first unlink; if an unlink or the deadline then fails, the
controller stays at `release-dispatching-uncertain` with `error`. Never
re-release or replay: root inspects the leaf against `released-CASE.json`,
keeps leftovers and evidence, T1-T5 incomplete, Phase 11.

T6, same-mount locks on m0 (needs T5's released batch only for the order):

```bash
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-tools/guest-lock-probe.py' < $E/lock-m0.json | tee $E/t6-lock.json
```

Pass only with `"passed": true` (exit 0; exit 10 is a fail, recorded with its
`error`; a fail does not block T7). T6b (across m0/m1): recorded
**unsupported**, not run.

T7 (A8, skip if `PM=skip`): two workloads in the ordinary pod profile
bind-sharing m0. The request file must not exist on Rocky yet:

```bash
rocky 'test ! -e /root/hfs-zq3-zq3-r7-pod/request.json && (umask 077; cat > /root/hfs-zq3-zq3-r7-pod/request.json)' < $E/pod-request.json
rocky 'sha256sum /root/hfs-zq3-zq3-r7-pod/request.json'   # must equal the next line
sha $E/pod-request.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r7-pod/request.json' | tee $E/t7-pod.json
```

Pass only with `t7_pass: true` (which also removes the two exited containers
and the pod payload normally); a failure or `uncertain` preserves both. An
SELinux denial of `container_t` on the 9p label is a T7 failure, never
relabelled or disabled.

T9, v24 lost reply with the d44 MKDIR filter (the d64 daemon `9b7eed8d…`
keeps it; d64 changed the view fence and the guest-cell admission, not the
lifecycle client). Run the ARM and the CALL step back to back:

```bash
obs obs-mapped-pressure-retained t9-pre-arm
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
response). RESTARTED leaves the runtime serving: T11 unrun.

T11, K04 host side (**only if** T10 is FENCED). Wait for the runtime to exit
and the guard to write its receipt (repeat the first line every 10 s, at most
30 times, until it prints the receipt):

```bash
f3 "doas -n test -e $R/guard/receipt.json && doas -n cat $R/guard/receipt.json || echo no-receipt"
f3 "doas -n cat $R/guard/receipt.json" > $E/p10-guard-receipt.json
jf $E/p10-guard-receipt.json runtime_running_at_guard_exit    # must print False
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r7 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss | tee $E/t11-after-loss.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r7 --path $R/runtime/mnt --label after" > $E/k04-after.json
python3 -I -B $CR k04 --output $OUT --sha256 $B --baseline $E/k04-baseline.json --baseline-sha256 $(sha $E/k04-baseline.json) --before $E/k04-before.json --before-sha256 $(sha $E/k04-before.json) --after $E/k04-after.json --after-sha256 $(sha $E/k04-after.json) --record $E/t11.json
```

No receipt after 30 tries (5 minutes): T11 unrun; Phase 11 (inspect the guard
first, no signal without Paul's decision). The after-loss step probes the
unarmed m1 (lstat, list, exclusive create, never retried). The record's
`k04_refusal_observed` needs every guest attempt to fail, the marker absent
on the host and no raw-directory change; if m1 already showed the guest's own
`EHOSTDOWN` (T9 re-read, T10 quiesce or the first after-loss attempt) the
probe is flagged vacuous. The recorded answer for remount is "VM restart
required".

T8, K04 guest side, last (needs no live runtime; m1 must still be mounted):

```bash
rocky 'findmnt -t 9p' | tee $E/t8-before.txt
rockyt "umount $G/m1" | tee $E/t8-umount.txt; echo rc=${PIPESTATUS[0]}   # normal only; refused or timed out (rc=124): T8 unrun, Phase 11
rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r7-tools/guest-lock-probe.py' < $E/lock-m1-t8.json | tee $E/t8-lock-refusal.json
rocky "ls -A $G/m1" | tee $E/t8-ls.txt          # must print nothing
```

T8 holds only if the unmount succeeded, the probe refused at admission
before any open (`"passed": false`, `error` `Refused: exact mountpoint, 9p
source and mount ID required`, exit 10) and the directory is empty.

Not run: PTE/TLB/busy-pin/GUP exclusion and funded drains (G14), distributed
writers (ov3/pv3).

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
  T10 unrun, or T10 RESTARTED, r1 finding F5; zq3-r6 took this branch after
  its mount-1 failure). The step 6 `child-gone` check also matches the guard's
  `--vm-name hfs-zq3-r7`, and `vms.sh start-rocky` refuses while the guard
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
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r7" > $E/f3-window-evidence.tar
# 2. guest 9p mounts: list, then unmount each listed one on its own line
rocky "findmnt -t 9p || echo no-9p"
rockyt "umount $G/m0" | tee $E/p11-umount-m0.txt; echo rc=${PIPESTATUS[0]}   # only if m0 is listed; rc=124 (timeout): record, continue
rockyt "umount $G/m1" | tee $E/p11-umount-m1.txt; echo rc=${PIPESTATUS[0]}   # only if m1 is listed; same
rocky "findmnt -t 9p || echo no-9p"              # no-9p expected; see below if not
# 3. observer removal (banner timeout: check authorized_keys is still $EA before the one retry)
okpin && rocky "/usr/bin/python3.9 -I -B - remove --run-id zq3-r7 --expect-sha256 $EA" < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
# 4. grub check
rocky "grub2-editenv - list"                     # next_entry empty; if set: rocky "$K cancel $KP" (U1) on its own line
# 5. guest poweroff
rocky 'systemctl poweroff'
# 6. wait for the child: repeat this line every 10 s, at most 60 times (10 min), until child-gone and no-vmm
f3 'ps -ax -ww | grep "[h]fs-zq3-r7" || echo child-gone; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm'
# 7. host cleanup (tap, VM context, vars copy)
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 53e384b9160e3c620b693ab28a2314ec410580bf17635c59edc32793b3d887d7 --child-pid $XP" | tee $E/p11-host-cleanup.json   # only if XP is a PID
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 53e384b9160e3c620b693ab28a2314ec410580bf17635c59edc32793b3d887d7 --no-child" | tee $E/p11-host-cleanup.json   # only if the exporter line above printed None
# 8. native FUSE mount: check, then unmount if listed
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount" | tee $E/p11-native-check.txt
f3 "doas -n umount $R/runtime/mnt"               # only if step 8's check listed it; busy or error: stop and preserve
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount"   # must print no-native-mount before step 10
# 9. observer key on f3: this exact plain line, literal paths, never inside a script (zq3-r5)
f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519.pub"
f3 "doas -n ls -la $R/observer"                  # only rocky-known_hosts left
# 10. window restore, always last; it prints nothing on success
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
f3 "doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r7/restoration-summary.json" | tee $E/p11-restoration-summary.json   # the restore's real outcome
```

Step 2 after a FENCED T10: m0's session is blocked since T9 and the host is
gone, so its unmount may refuse or not return. A normal `umount` only, never
`-f` or `-l`: if it refuses, or `timeout 150` ends it (`rc=124`; SSH
keepalives alone do not, because sshd keeps answering), record that and
continue with step 3;
the guest poweroff in step 5 takes the mount down with the guest. A
`findmnt` that still lists a mount after that is recorded, not fixed.

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
by "starting bhyve (run 2)". The c64 test kernel boots with SELinux
enforcing, so **no relabel reboot is expected**. Write the result into the
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
- `$R`, `/var/tmp/hfs-20261110-zq3r7-fdq` and
  `/var/tmp/hfs-fdescfs-windows/zq3-r7`;
- the zq3-r1 to zq3-r6 directories.

Left on Rocky unless Paul decides otherwise:
- `/root/hfs-zq3-zq3-r7-*` and `$G` (retention records, pod rootfs and
  evidence);
- the kernel state journal;
- the zq3-r1 to zq3-r6 leftovers.

fdescfs and fusefs stay loaded on f3 again, so a later run meets D1 again.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r7`): `$S/tools/lab/vms.sh --approved-action start-rocky`
  (test kernel boots once under the normal config); wait up to 10 min for
  pinned SSH; `rocky "$K check --expect test $KP"`; `rocky "$K
  reboot-to-stock $KP"`; then `rocky "$K check --expect stock $KP"`, U2 and
  the Phase 12 checks. zq3-r1 exercised U3 end to end (16 s to SSH). If the
  first boot reboots itself before the `check --expect test` line (a relabel
  reboot, zq3-r6 R6-3; not expected with the c64 kernel), `next_entry` is
  already consumed: record it as a finding and continue with `check --expect
  stock`, then U2. A hung test kernel stops here: a forced power-off needs
  Paul's explicit decision.
