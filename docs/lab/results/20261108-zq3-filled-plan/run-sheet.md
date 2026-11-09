# zq3-r1 run sheet (filled 2026-11-08)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](../20261108-zq3-phase1/README.md)

Ordered commands for run `zq3-r1`, with the values filled on 2026-11-08 from
`main` `08831e7`. **This sheet grants no authority.** Class **A** steps are
critical and run only after Paul's explicit final GO on this filled plan
(A1-A8 of the run plan). Class **B** steps are routine bounded verification
under the 2026-11-02 standing approval, but only inside an approved A window.
`PAUL` marks a point where Paul himself must act. Durations are estimates.

Generic failure rule (every phase): stop dependent work, collect evidence, never
force anything (no `vm poweroff`, no `kill -9`, no forced unmount, no recursive
delete through a mount), preserve failed resources, report. Values written
`<…>` are run-time fills (see [README](README.md#run-time-fills)); everything
else is exact.

## Session setup (workstation, bash)

```bash
D=/home/paul/tmp/claude/hfs-zq3-r1-fill      # private 0700 fill dir
S=$D/snapshot                                # git archive of 08831e7, refreshed to the k44 commit (Phase 0)
R=/var/tmp/hfs-validation-xz3/zq3-r1         # f3 staging prefix
G=/var/tmp/hfs-zq3-zq3-r1                    # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
# Before every stdin invocation of observer_key.py:
okpin() { echo "cd7f38c2660701129f1133194cfea881471480623d7dac843ee443e9688d920e  $S/tools/lab/retained9p/observer_key.py" | sha256sum -c --quiet; }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r1-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r1-kernel-stage/rocky-kernel-plan.json --plan-sha256 ac3a0e754ac59a165398bad44bc73185fd18eb5cf0cc842112f7883f766bc46a --state /root/hfs-zq3-zq3-r1-kernel-state'
```

Run every phase in this one shell session: later phases reuse `K`, `KP`, and
the `NG`/`W` variables defined in Phase 4.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-08 by the fill: bundle, argv record, kernel plan, templates,
staging trees and tarballs, init helper rebuild (reproduced `8c02b98f…`).
Local tests passed: retained9p 259 at the fill (280 after k44), directlaunch 26, fdescfs window, window
plan, runtime guard, volume init (1 skip), vms.sh, `go test
./internal/runtimeprep`. Re-verify immediately before Phase 1:

```bash
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 ba493bf67f9dbd6570074c9f4ec29e4f60ddb3ef7a75e6a0ffeb5546fe243589 --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 ba493bf67f9dbd6570074c9f4ec29e4f60ddb3ef7a75e6a0ffeb5546fe243589
sha256sum -c <<'EOF'
2b4370bacdc330077d1ef52568a6a9107bfc0b330650becb6d8d600bd5a1c8ea  /home/paul/tmp/claude/hfs-zq3-r1-fill/stage/f3-stage.tar
2d7fd06c9d5456f550d8a7cabee243b007b424432e6dddc757fa2c23ccc86d6d  /home/paul/tmp/claude/hfs-zq3-r1-fill/stage/rocky-stage.tar
ac3a0e754ac59a165398bad44bc73185fd18eb5cf0cc842112f7883f766bc46a  /home/paul/tmp/claude/hfs-zq3-r1-fill/kernel/rocky-kernel-plan.json
EOF
```

Refresh the tools snapshot to the final main commit `$FINAL` (which holds
`fill_plan.py` from k44 and the j44 window-restore fix), for example
`rm -rf $S.new && mkdir -m 700 $S.new && git -C /home/paul/git/homelabfs
archive $FINAL tools docs | tar -x -C $S.new && mv $S $S.old && mv $S.new $S`.
Between `08831e7` and `$FINAL`, below `tools/` only `fill_plan.py`,
`test_fill_plan.py`, `tools/lab/retained9p/README.md` (k44) and the window
controller, filler and tests (j44) change; check with
`git diff --stat 08831e7 $FINAL -- tools/`. The j44 files change the pins for
`fdescfs-window.py`, `fdescfs-window-plan.py`, the window `pins.json` and the
f3 staging tarball; use the refreshed values from the pins-refresh record (see
the README), never the original fill values. Then rerun
`python3 -B -m unittest discover -s tools/lab/retained9p` (280 tests at k44)
in the checkout at `$FINAL` (not in `$S`; see
[finding F6](README.md#findings)). If a fill is interrupted, a leftover
`NEW.next` file makes reruns refuse with "File exists": inspect it, then
remove it by hand before retrying.

**PAUL (before Phase 8, ideally now):** place the operator signing key at
`/home/paul/tmp/claude/hfs-zq3-r1-fill/operator/operator-signing-key`: a copy
(one link), owned by `paul`, mode `0600`, unencrypted Ed25519 PKCS#8 PEM or a
64-hex seed without newline, matching public key `2f6d909c…`. The bundle's
request template records exactly this path; no tool here reads it. A
different path means regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8).

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 21:03/21:07Z facts are the baseline. Immediately before Phase 2:

```bash
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
f3 'doas -n vm list; ps -ax -o pid,lstart,command | grep "[b]hyve: rocky"; doas -n ls /dev/vmm; swapinfo; kldstat; mount -p | grep -E "fusefs|fdescfs" || echo none; test ! -e /var/tmp/hfs-validation-xz3 && echo staging-absent; ls -ld /var/tmp/homelabfs-9p-poc/share' > $E/p1-f3.txt
rocky 'uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /' > $E/p1-rocky.txt
```

Continue only if: Rocky PID `80203` with lstart `Sun Nov  8 16:05:53 2026`;
freebsd/hfs-test0..2 Stopped; `/dev/vmm` = `rocky`; swap unused; fusefs and
fdescfs not loaded, no such mounts; staging absent; PoC share present; the new
launch facts give the same `rocky.conf` `c101adf1…`, vm-run `43753bc8…`,
firmware `6b48e967…`, vars `3c3d8783…`, bridge members `re0 tap0` (tap100
free); Rocky `5.14.0-687.66.1.el9_8.x86_64` running and default, `next_entry=`
empty, `saved_entry` unchanged, Enforcing, `authorized_keys` `e08542dc…`,
python3.9 `1143733d…`. Any difference stops: revise the plan.

## Phase 2: stage on f3 (A2, about 3 min)

```bash
f3 'test ! -e /var/tmp/hfs-validation-xz3 && test ! -e /var/tmp/hfs-20261108-zq3r1-fdq'
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261108-zq3r1-fdq > $E/p2-dirs.txt
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
daemon, bhyve, `controlledruntime.test`, `fdescfs-namespace`, the G3
`homelabfs` helper (`0500`), the controllers and `host.py`/`argv.py`/
`host_snapshot.py`/`runtime-volume-init.py` (`0400`), the argv record, the
volume-init plan, the private runtime config (holds host receipt keys) and
the Rocky pin file; the native guard and `runner_process.py` go to
`/var/tmp/hfs-20261108-zq3r1-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

```bash
rocky 'for d in /root/hfs-zq3-zq3-r1-kernel-stage /root/hfs-zq3-zq3-r1-kernel-state /root/hfs-zq3-zq3-r1-incoming /root/hfs-zq3-zq3-r1-pod /root/hfs-zq3-zq3-r1-tools /var/tmp/hfs-zq3-zq3-r1 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r1-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r1 --observer /root/hfs-zq3-zq3-r1-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r1-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r1-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
# <POD_MANIFEST_SHA256> = the digest the manifest command prints; review, then:
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r1-pod/pod_fixture.py build --run-id zq3-r1 --expect-manifest-sha256 <POD_MANIFEST_SHA256>' | tee $E/p3-pod-build.json
```

Expect: install reports one new BLS entry (`<BLS_ENTRY_ID>`), default still
`5.14.0-687.66.1.el9_8.x86_64`, `saved_entry` unchanged, `next_entry` empty;
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. Write `<POD_MANIFEST_SHA256>` into a copy of
`$D/pod/pod-request.TEMPLATE.json` (`rootfs_manifest_sha256`). If Paul
declines A8, skip both pod commands (T7 unrun). Failure: stop before Phase 5
and run U2 (`$K remove $KP`) while Rocky still runs.

## Phase 4: f3 window, fusefs, observer key (A5, A6; B for facts/tests; about 10 min)

Step 0, read-only facts (B):

```bash
f3 doas -n sha256 -r /usr/local/bin/python3.12 /boot/kernel/kernel /sbin/mount /sbin/kldload /sbin/kldstat /sbin/sysctl /bin/freebsd-version /bin/hostname \
  $R/bin/hfs-controlled-runtime $R/bin/fdescfs-namespace $R/bin/controlledruntime.test $R/bin/fdescfs-window.py \
  /boot/kernel/fdescfs.ko /boot/kernel/fusefs.ko /libexec/ld-elf.so.1 /lib/libc.so.7 /lib/libthr.so.3 /lib/libsys.so.7 > $E/p4-host.sha256
f3 'ldd /usr/local/bin/python3.12' > $E/p4-python-ldd.txt     # add `sha256 -r` lines of the listed libs to p4-host.sha256
f3 "stat -f '%Sp %Su %Sg %N' /usr/local/bin/python3.12 /usr/local/bin /usr/local /usr /" > $E/p4-python-stat.txt
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime; date -u +%s" > $E/p4-facts-and-now.txt
f3 'sha256 -r /usr/bin/ssh' > $E/p4-f3-ssh.sha256           # fill F1: bootstrap observer.ssh_sha256
f3 'ps -p 80203 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
```

Split `p4-facts-and-now.txt` into `p4-facts.json` (first line) and `<F3_NOW>`
(last line), then fill the window plan on the workstation (B) and review the
printed SHA256:

```bash
python3 -I -B $S/tools/lab/fdescfs-window-plan.py --pins $S/docs/lab/zq3-fdescfs-window/pins.json \
  --facts $E/p4-facts.json --host-sha256 $E/p4-host.sha256 --expires $(( <F3_NOW> + 3300 )) --host-now <F3_NOW> \
  --out $D/fdescfs-plan          # prints <WINDOW_PLAN_SHA256>
f3 "doas -n sh -c 'umask 277; test ! -e $R/records/fdescfs-plan.json && cat > $R/records/fdescfs-plan.json'" < $D/fdescfs-plan/plan.json
f3 doas -n sha256 -r $R/records/fdescfs-plan.json      # must equal <WINDOW_PLAN_SHA256>
```

Fill the bootstrap plan (F1) from the fact file and check it (B). The filler
refuses unless `p4-f3-ssh.sha256` is exactly one `HASH /usr/bin/ssh` line and
the result passes `bootstrap.py check`'s own validation; it prints the plan's
`sha256`:

```bash
python3 -I -B $S/tools/lab/retained9p/fill_plan.py bootstrap --template $D/bootstrap/bootstrap-plan.TEMPLATE.json \
  --template-sha256 8996f535df95202ed6890b602b9dd8e8d93b31dc2d051b1e76def40b9a773bf9 \
  --ssh-fact $E/p4-f3-ssh.sha256 --output $D/bootstrap/bootstrap-plan.json | tee $E/p4-bootstrap-fill.json
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json --plan-sha256 $(sha256sum $D/bootstrap/bootstrap-plan.json | cut -c1-64)
```

Step 1, negative before setup (B; a skip counts as unrun). No `--` before
the command: the guard's `argparse.REMAINDER` under Python 3.12 keeps a
leading `--` as the program name (zq3-r1 attempt 1 tried to exec `--`; p44).
Each `--stage` name is single use; a refused stage directory is kept and the
retry takes a new name (zq3-r1 used `unsupported-before-r2`):

```bash
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261108-zq3r1-fdq/native-test-guard.py --root /var/tmp/hfs-20261108-zq3r1-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 80203 --rocky-birth 'Sun Nov  8 16:05:53 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v"
```

Resume of zq3-r1 after p44 (only for that run). The first setup mounted
`/dev/fd` and refused the masked `rw` row; it was undone with the pinned
helper's `unmount EXACT_FSID` and a normal `kldunload fdescfs`, after which
`facts` equalled `p4-facts.json`. Before rerunning setup:

1. Refresh the tools snapshot `$S` to the main commit that merged p44 (same
   recipe as the j44 refresh above), so the plan filler and the staged file
   both use the p44 controller `b00b19d1…`.
2. Replace the staged controller, keep the refused window directory under a
   new name (setup refuses an existing run directory, and the directory name
   must equal the plan's `run_id`), and skip the parent creation below (it
   exists).
3. Rerun step 0 and the window plan fill with `-resume` names so the refused
   attempt's evidence and plan stay untouched: `p4-*-resume.*` evidence,
   `--out $D/fdescfs-plan-resume`, remote plan
   `$R/records/fdescfs-plan-resume.json`, and `W`'s `--plan` pointing at it.
   The bootstrap plan fill (F1) and step 1 already passed and are not rerun.

```bash
f3 "doas -n sh -c 'umask 277; test ! -e $R/bin/fdescfs-window.py.p44 && cat > $R/bin/fdescfs-window.py.p44'" < $S/tools/lab/fdescfs-window.py
f3 doas -n sha256 -r $R/bin/fdescfs-window.py.p44     # must equal b00b19d1… (README, pin refresh after p44); stop otherwise
f3 doas -n mv $R/bin/fdescfs-window.py.p44 $R/bin/fdescfs-window.py
f3 "stat -f '%Sp %Su %Sg %N' $R/bin/fdescfs-window.py"   # -r-------- root wheel
printf '%s\n' 'set -e' 'mv /var/tmp/hfs-fdescfs-windows/zq3-r1 /var/tmp/hfs-fdescfs-windows/zq3-r1.refused-setup-1' | f3 doas -n sh -s
```

Steps 2-4 (A5): setup, qualify (under the guard), fusefs. `setup` requires
the root-private parent `/var/tmp/hfs-fdescfs-windows` to exist
([Descriptor-execution-window](../../Descriptor-execution-window.md)); the
first line creates it (missing from Phase 2 before p44). The stdin script
avoids nested quoting through f3's csh login shell:

```bash
printf '%s\n' 'set -e' 'test ! -e /var/tmp/hfs-fdescfs-windows' 'install -d -m 0700 -o root -g wheel /var/tmp/hfs-fdescfs-windows' | f3 doas -n sh -s
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 <WINDOW_PLAN_SHA256> --directory /var/tmp/hfs-fdescfs-windows/zq3-r1"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots.

Step 5 (A6), run-only observer key:

```bash
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r1-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r1-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r1 --public-key /root/hfs-zq3-zq3-r1-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
```

Record `<ENROL_AFTER_SHA256>` from the enrol output (needed in Phase 11).
The private key never leaves f3. Failure: nothing unloaded; U2 the unselected
kernel; restore the window (Phase 11 step 5).

## Phase 5: one-shot select and stop Rocky (A1, A3; about 3 min)

```bash
f3 'doas -n vm list; swapinfo; sysctl -n vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count kstat.zfs.misc.arcstats.size vfs.zfs.arc_min; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
rocky "$K select $KP" | tee $E/p5-select.json        # next_entry = <BLS_ENTRY_ID>; on any failure it runs U1 itself
$S/tools/lab/vms.sh --approved-action stop-rocky      # clean vm stop, waits up to 120 s, never forces
f3 'doas -n vm list; ps -ax | grep "[b]hyve: rocky" || echo no-rocky-process; doas -n ls /dev/vmm 2>/dev/null || echo no-vmm; ifconfig -l' > $E/p5-stopped.txt
```

Failure before the stop: `rocky "$K cancel $KP"` (U1), then U2. Failure after
the stop: `next_entry` is still set, recovery is U3 (below).

## Phase 6: post-stop admission (B, 1 min)

```bash
f3 'swapinfo; sysctl -n hw.physmem vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count kstat.zfs.misc.arcstats.size vfs.zfs.arc_min; doas -n vm list; doas -n ls /dev/vmm 2>/dev/null || echo no-vmm; mount -p | grep /dev/fd' > $E/p6-admission.txt
```

Required: (free + inactive) pages × 4096 + (ARC − arc_min) ≥ 15 GiB
(6 GiB guest + 6 GiB runtime envelope + 3 GiB reserve); swap unused; all VMs
stopped; `/dev/fd` still the owned fdescfs mount. Else: restore the window,
then U3.

## Phase 7: host setup (A4, about 3 min)

```bash
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 ba493bf67f9dbd6570074c9f4ec29e4f60ddb3ef7a75e6a0ffeb5546fe243589" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 747b12b3f8a6a54070d42bcd5e623b44f041066ef368097540ec6e37c48d4c8b --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r1 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`3c3d8783…`: stop; regenerating the argv record from fresh facts (same
sessions, same args) is a plan revision for review, never an on-the-spot edit.
Failure with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3.

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s, about 5 min with prep)

```bash
P=$D/bootstrap/bootstrap-plan.json
python3 -I -B $S/tools/lab/retained9p/bootstrap.py run --plan $P --plan-sha256 $(sha256sum $P | cut -c1-64) --output $D/bootstrap-run
```

The driver launches, under the outer guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r1/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r1/guard --runtime-sha256 174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7 --vm-name hfs-zq3-r1 --disk-root /var/tmp/hfs-validation-xz3/zq3-r1 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r1 --skip-path /var/tmp/hfs-validation-xz3/zq3-r1/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r1/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r1/runtime/runtime-config.json
```

(guard limits = its defaults = plan ceilings: 3600 s, runtime 4 GiB + 256 MiB
RSS, bhyve 6 GiB + 512 MiB, disk 4 GiB, 8 GiB free, no swap, 512 MiB
available memory, one SIGTERM on breach.)

**PAUL, inside the same 120 s:** keep a second terminal open on the
workstation (same user `paul`). When the driver prints
`"operator_action": "sign-enroll"`, run exactly:

```
/home/paul/tmp/claude/hfs-zq3-r1-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/signer-enrollment
```

When it prints `"operator_action": "sign-bind"`, run exactly:

```
/home/paul/tmp/claude/hfs-zq3-r1-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/signer-binding
```

Both print `seconds_left`; sign only the request whose `request_sha256` the
driver printed. Expected order: host-preflight, launch, proposal, guest-ready
(test kernel; GRUB consumed `next_entry`), admission (manifest SHA256, boot
ID, provider nonce, `virtioN` names), enroll-request, **sign-enroll**,
provision, enrollment-handoff, awaiting-bind, bind-request, **sign-bind**,
bind, mount-0, mount-1, READY. The driver never reads the key. Failure or
`uncertain`: nothing is replayed; inspect `$D/bootstrap-run/journal.jsonl`,
go to Phase 11 (inspection first). If no guest kernel ran (no GRUB output),
`next_entry` is unconsumed: U3 after the child is positively gone.

After READY (B): fill the controller batch plan (F6/F7) from the run's
recorded evidence, then check it independently. The filler takes `<BOOT_ID>`,
`<PROVIDER_NONCE>` and `<MANIFEST_SHA256>` from the journal's `guest-ready`
and `admission` results (cross-checked with `work/enroll-request.json`) and
`<M0_ID>`/`<M1_ID>` from the first field of each mount step's `mountinfo`;
it refuses unless the run is READY with every step `ok`, every value agrees,
and the plan passes the controller's own validation, and it never overwrites
an existing plan. It prints the values and `<BATCH_PLAN_SHA256>`:

```bash
python3 -I -B $S/tools/lab/retained9p/fill_plan.py batch --template $D/controller/batch-plan.TEMPLATE.json \
  --template-sha256 24f7116e11d80dcaf889e0b9282110e85abc60d49bb740c21c6b1c063f7fec8b \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 $(sha256sum $P | cut -c1-64) \
  --output $D/controller/batch-plan.json | tee $E/p8-batch-fill.json
$S/tools/lab/check-retained9p.sh check --plan $D/controller/batch-plan.json   # must print the same <BATCH_PLAN_SHA256>
```

Read `<RUNTIME_UUID>` from the READY record (`control.runtime`) in
`$D/bootstrap-run/journal.jsonl`. On a filler refusal nothing was written and
the runtime keeps serving under the guard's bounds. A wrong argument (path,
pin) may be corrected and the filler rerun; a mismatch in the evidence itself
is a run finding: never hand-edit the plan around it, go to Phase 11
(inspection first).

## Phase 9: READY admission (B, 1 min)

```bash
C="$R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock"
f3 "doas -n $C --operation observe --runtime <RUNTIME_UUID> --sequence 1" > $E/observe-001.json
rocky 'findmnt -t 9p; getenforce; uname -r'
```

Every section `complete`, `publication` null, both domains admitted, exactly
the two 9p mounts.

## Phase 10: gate matrix (B inside the window, about 25-35 min)

Order T1-T7, T9, T10, T11, T8; stop dependent steps on the first failure.
`CTL=$S/tools/lab/check-retained9p.sh`, `CR=$S/tools/lab/retained9p/correlate.py`,
`B=<BATCH_PLAN_SHA256>`, `OUT=$D/ctrl-run`. Each next OBSERVE argv comes from
`python3 -I -B $CR command --operation observe --output $OUT --sha256 $B --observe LAST --observe-sha256 H --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock` (prints, never runs).

T1-T5, per case `credentials`, `sync`, `metadata`, `namespace`, `mapped-pressure`:

```bash
f3 "doas -n $C --operation observe --runtime <RUNTIME_UUID> --sequence <N>" > $E/obs-<case>-baseline.json
$CTL probe --plan $D/controller/batch-plan.json --sha256 $B --output $OUT   # first case only; later cases: $CTL next --output $OUT --sha256 $B
f3 "doas -n $C --operation observe --runtime <RUNTIME_UUID> --sequence <N+1>" > $E/obs-<case>-retained.json
python3 -I -B $CR case --output $OUT --sha256 $B --case <case> --baseline $E/obs-<case>-baseline.json --baseline-sha256 <h> \
  --retained $E/obs-<case>-retained.json --retained-sha256 <h> --record $E/corr-<case>.json
$CTL release --output $OUT --sha256 $B --correlation $E/corr-<case>.json --correlation-sha256 <h>
```

T6 (same-mount locks): fill `<M0_ID>` into
`$D/run-inputs/lock-probe-m0.TEMPLATE.json`, then
`rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r1-tools/guest-lock-probe.py' < $E/lock-m0.json > $E/t6-lock.json`.
T6b (across m0/m1): recorded **unsupported**, not run.

T7 (A8): fill `<POD_MANIFEST_SHA256>` and `<M0_ID>` into the pod request, copy
it to Rocky and run
`rocky '(umask 077; cat > /root/hfs-zq3-zq3-r1-pod/request.json)' < $E/pod-request.json` then
`rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r1-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r1-pod/request.json'`;
pass only with `t7_pass: true`.

T9 (lost reply, MKDIR filter): OBSERVE; then
`python3 -I -B $CR command --operation arm-lost-reply --lost-intent MKDIR --output $OUT --sha256 $B --observe <obs> --observe-sha256 <h> --runtime-binary $R/bin/hfs-controlled-runtime --control $R/runtime/owner/control.sock`;
root runs the printed argv on f3 (`doas -n …`); `$CTL lifecycle --output $OUT --sha256 $B --step lost-reply-call`;
OBSERVE; `$CTL lifecycle … --step lost-reply-reread`;
`python3 -I -B $CR lost-reply --output $OUT --sha256 $B --before … --armed … --after … --record $E/t9.json`.
Expected `dropped-reply-effect-committed-and-visible`; never a pass.

T10 (restart): `$CTL lifecycle … --step quiesce`; OBSERVE (after the quiesce);
`$CR command --operation restart …` (refuses unless C9 cut precondition);
root runs the printed argv. RESTARTED or FENCED recorded (FENCED expected,
Paul's accepted behaviour). Last step that needs the live runtime.

T11 (K04 host side, only if FENCED and the runtime exited):

```bash
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r1 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r1 --path $R/runtime/mnt --label after" > $E/k04-after.json
python3 -I -B $CR k04 --output $OUT --sha256 $B --baseline $E/k04-baseline.json --baseline-sha256 <h> --before $E/k04-before.json --before-sha256 <h> --after $E/k04-after.json --after-sha256 <h> --record $E/t11.json
```

T8 (K04 guest side, last): `rocky "umount $G/m1"` (normal; busy or hang → T8
unrun); fill `<M1_ID>` into `lock-probe-m1-T8-refusal.TEMPLATE.json` and run the
lock probe with it: it must refuse at admission (mountpoint not present), and
`rocky "ls -A $G/m1"` must print nothing.

Not run: PTE/TLB/GUP (G14), distributed writers (ov3/pv3).

## Phase 11: cleanup (within A1/A4/A5/A6; about 5-10 min)

```bash
f3 "doas -n tar -c -f - --exclude control.sock -C $R guard records runtime/owner launch" > $E/f3-evidence.tar   # private; runtime-config.json is not included
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r1" > $E/f3-window-evidence.tar
rocky "findmnt -t 9p; umount $G/m0; umount $G/m1 2>/dev/null; findmnt -t 9p || echo no-9p"
okpin && rocky '/usr/bin/python3.9 -I -B - remove --run-id zq3-r1 --expect-sha256 <ENROL_AFTER_SHA256>' < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
rocky "grub2-editenv - list"                     # next_entry empty; if set: $K cancel $KP (U1)
rocky 'systemctl poweroff'
f3 'ps -ax -ww | grep "[h]fs-zq3-r1" || echo child-gone; doas -n ls /dev/vmm 2>/dev/null'
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 ba493bf67f9dbd6570074c9f4ec29e4f60ddb3ef7a75e6a0ffeb5546fe243589 --child-pid <EXPORTER_PID>" | tee $E/p11-host-cleanup.json
f3 "mount -p | grep $R/runtime/mnt && doas -n umount $R/runtime/mnt || echo no-native-mount"
f3 "doas -n rm -- $R/observer/id_ed25519 $R/observer/id_ed25519.pub"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
```

`<EXPORTER_PID>` comes from the runtime evidence (guard `observations.jsonl`
/ runtime stdout). Release tokens in case `stdout.log` files are redacted or
marked spent before anything is copied into `docs/lab/results`. Expected
restore outcome (after task j44): the unmount happens, and the module
comparison accepts only the plan-declared `fusefs.ko` beyond the post-load
baseline; `restoration-summary.json` records it in `window_module_loaded`.
With the controller bytes pinned by this fill (before j44) it would instead
report `fusefs.ko` as a difference; see [finding F1](README.md#findings).

## Phase 12: normal restoration (A1, A3; about 5-10 min)

```bash
$S/tools/lab/vms.sh --approved-action start-rocky    # only after Phase 11 verified next_entry empty and the child gone
rocky "$K check --expect stock $KP; getenforce; sha256sum /root/.ssh/authorized_keys; free -m"   # authorized_keys must be e08542dc…
rocky "$K remove $KP" | tee $E/p12-kernel-remove.json   # U2 (Paul chose removal)
f3 'doas -n vm list; doas -n ls /dev/vmm; ifconfig -l; mount -p | grep -E "fdescfs|fusefs" || echo none; kldstat; zpool list zroot' > $E/p12-final.txt
```

Staging prefix deletion is a separate, later step after the evidence is safe
and `mount -p` shows nothing below `/var/tmp/hfs-validation-xz3`. Left on
Rocky as evidence unless Paul decides otherwise: `/root/hfs-zq3-zq3-r1-*`,
`$G` (retention records, pod rootfs and evidence), kernel state journal.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r1`): `vms.sh --approved-action start-rocky` (test kernel
  boots once under the normal config); wait up to 10 min for pinned SSH;
  `rocky "$K check --expect test $KP"`; `rocky "$K reboot-to-stock $KP"`; then
  `rocky "$K check --expect stock $KP"`, U2 and the Phase 12 checks. A hung
  test kernel stops here: a forced power-off needs Paul's explicit decision.
