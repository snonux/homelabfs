# zq3-r3 run sheet (filled 2026-11-09)

[Summary](README.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Phase 1 facts](facts/) · [zq3-r2 record](../20261109-zq3-r2/README.md) ·
[zq3-r2 sheet](../20261109-zq3-r2-filled-plan/run-sheet.md)

Ordered commands for run `zq3-r3`, the third attempt of zq3, with the values
filled on 2026-11-09 from `main` `1eba12d` (after o54, p54, q54, r54 and t54).
**This sheet grants no authority.** Class **A** steps are critical and run
only after Paul's explicit final GO on this filled plan (A1-A8 of the run
plan). Class **B** steps are routine bounded verification under the
2026-11-02 standing approval, but only inside an approved A window. `PAUL`
marks a point where Paul himself must act. Durations are estimates.

Generic failure rule (every phase): stop dependent work, collect evidence, never
force anything (no `vm poweroff`, no `kill -9`, no forced unmount, no recursive
delete through a mount), preserve failed resources, report. Values written
`<…>` are run-time fills (see [README](README.md#run-time-fills)); everything
else is exact.

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
  "only if …" is a decision: write the check's result into the run log before
  running or skipping it.
- **SSH banner timeouts (zq3-r2 R2-5).** A single ProxyCommand SSH to the
  test guest may fail with "Connection timed out during banner exchange".
  Never retry blindly: first prove that nothing ran, with a read-only check
  of the step's own effect (for example `sha256sum /root/.ssh/authorized_keys`
  still `<ENROL_AFTER_SHA256>` before retrying the observer removal, `findmnt
  -t 9p` before retrying an unmount, the `rocky_kernel.py` state journal
  before retrying a kernel step). Record the timeout and the check in the run
  log, then retry once. A second failure, or a check that shows the step
  partly ran, stops: inspect. A native guard retry still takes a new stage
  name.
- **A failed 9p mount needs a guest reboot (r54 analysis).** A mount that
  fails after a successful enroll leaves its controlled identity claimed
  ("consumed") for the rest of that guest boot, so a second mount attempt in
  the same boot returns `EACCES`. Never retry a mount, never hand-run `mount
  -t 9p` after a failure: preserve evidence and go to Phase 11. Another
  attempt needs a new guest boot, which means a new run (new run ID, new
  sessions and a fresh fill).

## Execution notes after review round 1 (2026-11-09)

- **D1-a runs right after Phase 1**, before Phase 2 staging and the Phase 3
  kernel install. If a `kldunload` is refused (busy), stop with nothing staged
  or installed. If only `fusefs` unloaded, that state is harmless: stop and
  report. This changes plan item A5 ("no unload"). Paul's standing
  2026-11-09 auto-approval of all project changes covers it; it is named here
  so the change is explicit.
- **Every abort after Phase 4 step 5 also removes the observer key**: on
  Rocky, with `observer_key.py remove --expect-sha256 <ENROL_AFTER_SHA256>`
  while Rocky (or the test guest on Rocky's disk) answers, and on f3 with
  `doas -n rm` of `$R/observer/id_ed25519{,.pub}`. Phase 12's `e08542dc…`
  check must hold on every path.
- The guard's exit status is read from `$R/guard/receipt.json` and
  `stop-uncertain.json`, not from the operator shell.

## Session setup (workstation, bash)

```bash
source /home/paul/tmp/claude/hfs-zq3-r3-fill/session.sh
```

`session.sh` (`85e6bfee…`, [pins](pins.json) `fill_helpers`) defines, adapted
from zq3-r2's:

```bash
D=/home/paul/tmp/claude/hfs-zq3-r3-fill      # private 0700 fill dir
S=$D/snapshot                                # git archive of 1eba12d (docs internal tools)
R=/var/tmp/hfs-validation-xz3/zq3-r3         # f3 staging prefix (parent exists, holds zq3-r1 and zq3-r2)
G=/var/tmp/hfs-zq3-zq3-r3                    # Rocky run directory (m0, m1)
E=$D/evidence; umask 077; mkdir -p "$E"
O='-F /dev/null -T -oBatchMode=yes -oStrictHostKeyChecking=yes -oGlobalKnownHostsFile=/dev/null -oUpdateHostKeys=no -oControlMaster=no -oControlPath=none -oConnectTimeout=5 -oServerAliveInterval=15 -oServerAliveCountMax=8 -oClearAllForwardings=yes -oForwardAgent=no'
F3KH="-oUserKnownHostsFile=$S/docs/lab/f3-host-known_hosts -oHostKeyAlias=f3"
RKKH="-oUserKnownHostsFile=$S/docs/lab/rocky-known_hosts -oHostKeyAlias=rocky"
f3()    { /usr/bin/ssh $O $F3KH -- paul@10.20.0.133 "$@"; }
rocky() { /usr/bin/ssh $O $RKKH -oProxyCommand="/usr/bin/ssh $O $F3KH -W 10.20.0.123:22 -- paul@10.20.0.133" -- root@10.20.0.123 "$@"; }
okpin() { …observer_key.py cd7f38c2… check… }   # before every stdin invocation of observer_key.py
hpin()  { …sha256sum -c of bin/sign_watch.py (03be0b7e…), the signer bin/hfs-retained9p-operator (22e5fb90…) and sh/*… }
K='/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r3-kernel-stage/rocky_kernel.py'
KP='--plan /root/hfs-zq3-zq3-r3-kernel-stage/rocky-kernel-plan.json --plan-sha256 a3f5feea79af45f52d3317b34b1787194962730f7610730b8858dcc1fcbb2e3e --state /root/hfs-zq3-zq3-r3-kernel-state'
NG="doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261109-zq3r3-fdq/native-test-guard.py --root /var/tmp/hfs-20261109-zq3r3-fdq --local-reserved-bytes 4294967296 --profile host --rocky-pid 45407 --rocky-birth 'Mon Nov  9 13:00:32 2026' --rocky-config-sha256 c101adf10d58d8e021faaa1c3dd98ccb7777c2b78b7931f478e71b526814c581 --max-seconds 60"
```

The guard's `--rocky-birth` is the UTC `lstart` (the guard runs `ps` with
`TZ=UTC`). Run every phase in this one shell session: later phases reuse `K`,
`KP`, `NG` and the `W`/`P` variables defined in Phases 4 and 8.

## Open decision D1: f3 module state (PAUL, before the GO)

The 14:11Z facts show `fdescfs.ko` (id 25) and `fusefs.ko` (id 26) still
loaded on f3. zq3-r2 loaded them and, as disclosed, never unloads them; f3
has not rebooted since (same `kern.boottime`). The window plan filler
`fdescfs-window-plan.py` requires both absent: with the committed pins
(`fdescfs_module: absent`, `modules_forbidden: [fdescfs.ko, fusefs.ko]`) it
refuses at Phase 4 step 0 with `fdescfs module present; plan is for absent`.
The fill reproduced this refusal locally with zq3-r2's post-restore window
facts, which match f3's current state; with zq3-r2's Phase 4 facts (modules
absent) the same r3 pins fill. **Without a decision, the run stops at Phase 4
step 0 (F2).** Options:

- **D1-a, unload both modules before Phase 4 step 0** (host kernel module
  change; needs Paul's explicit approval as part of A5). This returns f3 to
  the state zq3-r1 and zq3-r2 started from, so the committed pins, the
  filler and every later step apply unchanged. The exact commands are Phase 4
  step 0a. A refused unload (busy) stops; never `kldunload -f`.
- **D1-b, a reviewed tool change** so the filler and the pins accept an
  already loaded `fdescfs.ko`/`fusefs.ko` as the baseline (the controller's
  `setup` already keeps a loaded `fdescfs`). This is code plus a pins change,
  so it is a plan revision with a new fill.
- **D1-c, reboot f3.** Unloads both, but changes `kern.boottime`, Rocky's PID
  and lstart and (with Rocky's AUTO start) its UEFI vars: new Phase 1 facts,
  a new argv record, `session.sh` and `p5-recheck.py`. A plan revision.

The rest of this sheet assumes **D1-a**; the Phase 1 expectations below
describe the modules as they are now.

## Phase 0: local preparation (B, done; rerun about 5 min)

Done on 2026-11-09 by the fill: bundle, argv record (from the 14:11Z launch
facts, UEFI vars `c71240e9…`), kernel plan (r54 candidate `5eb42c50…`),
templates, the filled bootstrap plan (F1), staging trees and tarballs, r3
window pins, init helper (zq3-r1's reproduced `8c02b98f…`). Local tests
passed at `1eba12d` (see [README](README.md#local-verification)).
Re-verify immediately before Phase 1:

```bash
cd $S/tools/lab/directlaunch && python3 -I -B argv.py verify --record $D/argv/bhyve-argv.json \
  --record-sha256 511179091c5a7fcee6e71323671eac66c52f4e839f9dd118ad4b310df5906c7d --runtime-config $D/bundle/runtime-config.json
$D/bin/hfs-runtime-prep check-argv --bundle $D/bundle --record $D/argv/bhyve-argv.json \
  --record-sha256 511179091c5a7fcee6e71323671eac66c52f4e839f9dd118ad4b310df5906c7d
python3 -I -B $S/tools/lab/retained9p/bootstrap.py check --plan $D/bootstrap/bootstrap-plan.json \
  --plan-sha256 077cfe661678ef280b65f261a86db37db0401c16538881c8ae824293de636a00
hpin && okpin && sha256sum -c <<'EOF'
982c27b3fac24ee0058b1a9ae6ffaf8d2eecf8fd6fd5c99a9f5ec4eff25f633c  /home/paul/tmp/claude/hfs-zq3-r3-fill/stage/f3-stage.tar
87b7c8d67e3ea064676d37d8fe62738a2e20e35b68f713ff5da782737c4d6393  /home/paul/tmp/claude/hfs-zq3-r3-fill/stage/rocky-stage.tar
a3f5feea79af45f52d3317b34b1787194962730f7610730b8858dcc1fcbb2e3e  /home/paul/tmp/claude/hfs-zq3-r3-fill/kernel/rocky-kernel-plan.json
a51e10c10bebe1e1728277f46ad982170067d2fab552977e8e1457a676a65fbd  /home/paul/tmp/claude/hfs-zq3-r3-fill/controller/batch-plan.TEMPLATE.json
1e3305fb6f18205bf345cbe6d3c64eb55cc3de6420da28006cc11262e86b63d3  /home/paul/tmp/claude/hfs-zq3-r3-fill/fdescfs-pins/pins.json
85e6bfeed9da16cdb2c84403da4825578f34630ea649f7bccf915128183c971c  /home/paul/tmp/claude/hfs-zq3-r3-fill/session.sh
EOF
```

The tools snapshot `$S` is `1eba12d`, the main HEAD at fill time; no refresh
is planned. If main moves before the run, `git diff --stat 1eba12d $FINAL --
tools/ internal/controlledruntime/probe docs/lab/f3-host-known_hosts
docs/lab/rocky-known_hosts docs/lab/zq3-fdescfs-window` must be empty (or
touch only tests and docs); otherwise the changed pins make this a plan
revision. A D1-b tool change is such a revision. If a fill step is
interrupted, a leftover `NEW.next` file makes reruns refuse with "File
exists": inspect it, then remove it by hand before retrying.

**Operator key (before Phase 8, ideally now):** on Paul's instruction Claude
copies the operator signing key to
`/home/paul/tmp/claude/hfs-zq3-r3-fill/operator/operator-signing-key` (the
empty `0700` directory exists): a copy (one link), owned by `paul`, mode
`0600`, unencrypted Ed25519 PKCS#8 PEM or a 64-hex seed without newline,
matching public key `2f6d909c…`. The bundle's request template records
exactly this path; nothing in the fill reads it. A different path means
regenerating the bundle (new sessions, new argv record).

**PAUL: final GO** on this sheet and [pins](pins.json) (A1-A8), **including
the D1 choice**. Paul delegated both signatures (A7) to Claude through the
watcher in Phase 8.

## Phase 1: re-verify the Phase 1 facts (B, about 5 min)

The 2026-11-09 14:11Z facts ([facts/](facts/)) are the baseline. Immediately
before Phase 2:

```bash
hpin
f3 doas -n /usr/local/bin/python3.12 -I -B - facts < $S/tools/lab/directlaunch/host.py > $E/p1-launch-facts.json
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); f=d["files"]; print([f[p]["sha256"] for p in ("/zroot/bhyve/rocky/rocky.conf", "/usr/local/lib/vm-bhyve/vm-run", "/usr/local/share/uefi-firmware/BHYVE_UEFI.fd", "/zroot/bhyve/rocky/uefi-vars.fd")], f["/usr/local/share/uefi-firmware/BHYVE_UEFI.fd"]["link"], d["bridge_members"], d["vmm"], d["interfaces"])' $E/p1-launch-facts.json
f3 sh -s < $D/sh/p1-f3.sh > $E/p1-f3.txt
rocky 'cat /proc/sys/kernel/random/boot_id; uname -r; grubby --default-kernel; grub2-editenv - list; getenforce; sha256sum /root/.ssh/authorized_keys /usr/bin/python3.9; df -B1 /boot /; ls /boot | grep -c hfs-xz3; ls -d /etc/homelabfs /root/hfs-zq3-zq3-r3-* /var/tmp/hfs-zq3-zq3-r3 2>&1' > $E/p1-rocky.txt
```

Continue only if: kern.boottime `{ sec = 1794212992, usec = 931825 } Mon Nov  9
10:29:52 2026`; Rocky bhyve PID `45407` with UTC lstart `Mon Nov  9 13:00:32
2026`; freebsd/hfs-test0..2 Stopped; `/dev/vmm` = `rocky`; swap unused;
`fdescfs.ko` and `fusefs.ko` loaded (as at 14:11Z, see D1) with no fusefs or
fdescfs mount and `/dev/fd` on devfs; `/var/tmp/hfs-validation-xz3` and
`/var/tmp/hfs-fdescfs-windows` both `drwx------ root wheel`; all three r3
paths `absent`; PoC share present; f3 `/usr/bin/ssh` `6a9e5573…`; the launch
facts give `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…`
(link `../edk2-bhyve/BHYVE_UEFI.fd`), vars `c71240e9…`, bridge members `re0
tap0` (no `tap100`); Rocky boot ID `4568a037-56db-4838-8f46-c4ac5bc37bf4`,
`5.14.0-687.66.1.el9_8.x86_64` running and default, `next_entry=` empty,
`saved_entry=b26c0ac534534d7fb81b3e0698558bc3-5.14.0-687.66.1.el9_8.x86_64`,
Enforcing, `authorized_keys` `e08542dc…`, python3.9 `1143733d…`, no
`hfs-xz3` file in `/boot`, `/etc/homelabfs` and every r3 path absent (the r1
and r2 leftovers `/root/hfs-zq3-zq3-r{1,2}-*`, `/var/tmp/hfs-zq3-zq3-r{1,2}`
stay as evidence). `boot_indeterminate=3` is expected (2 at zq3-r2; zq3-r1
R7). Any other difference stops: revise the plan (a changed vars hash means a
new argv record).

## Phase 2: stage on f3 (A2, about 3 min)

The parent `/var/tmp/hfs-validation-xz3` already exists (it holds zq3-r1 and
zq3-r2), so this checks it instead of requiring it absent. `paul` cannot look
inside a root `0700` directory, so absence is tested through `doas`. The
tarball has no entry for the parent, so extraction never rewrites its mode or
owner.

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-validation-xz3 /var/tmp/hfs-fdescfs-windows"   # both drwx------ root wheel
f3 "doas -n test ! -e $R && doas -n test ! -e /var/tmp/hfs-20261109-zq3r3-fdq && echo absent"   # must print absent
f3 doas -n tar -x -p --same-owner -f - -C /var/tmp < $D/stage/f3-stage.tar
f3 doas -n sha256 -r $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.sha256r
f3 doas -n stat -f "'%Sp %Su %Sg %l %N'" $(tr '\n' ' ' < $D/stage/f3-paths.txt) | diff - $D/stage/f3-expected.stat
f3 doas -n stat -f "'%Sp %Su %Sg %N'" /var/tmp/hfs-validation-xz3 $R $R/bin $R/records $R/runtime $R/runtime/handoff $R/observer /var/tmp/hfs-20261109-zq3r3-fdq > $E/p2-dirs.txt
```

Expect empty diffs and every directory `drwx------ root wheel`. Stages the
daemon, bhyve, `controlledruntime.test`, `fdescfs-namespace`, the G3
`homelabfs` helper (`0500`), the controllers and `host.py`/`argv.py`/
`host_snapshot.py`/`runtime-volume-init.py` and the main `runtime-guard.py`
(`fece96b3…`, p54 + q54) (`0400`), the argv record, the volume-init plan, the
private runtime config (holds host receipt keys) and the Rocky pin file; the
main native guard (`576d802a…`, o54) and `runner_process.py` go to
`/var/tmp/hfs-20261109-zq3r3-fdq`. `bhyve-slirp-helper` is not staged (the
argv has no slirp device). Failure: preserve, stop.

## Phase 3: Rocky staging, kernel install, observer stage, pod rootfs (A3, A8; about 12 min)

One command per line:

```bash
rocky 'for d in /root/hfs-zq3-zq3-r3-kernel-stage /root/hfs-zq3-zq3-r3-kernel-state /root/hfs-zq3-zq3-r3-incoming /root/hfs-zq3-zq3-r3-pod /root/hfs-zq3-zq3-r3-tools /var/tmp/hfs-zq3-zq3-r3 /etc/homelabfs; do test ! -e $d || echo EXISTS $d; done'
rocky 'tar -x -p --same-owner -f - -C /root' < $D/stage/rocky-stage.tar
rocky "sha256sum $(tr '\n' ' ' < $D/stage/rocky-paths.txt)" | diff - $D/stage/rocky-expected.sha256sum
rocky "$K install $KP --stage /root/hfs-zq3-zq3-r3-kernel-stage" | tee $E/p3-kernel-install.json
rocky "$K check --expect stock $KP; grub2-editenv - list; grubby --default-kernel; ls -Z /boot/*hfs-xz3*" > $E/p3-kernel-check.txt
okpin && rocky '/usr/bin/python3.9 -I -B - stage --run-id zq3-r3 --observer /root/hfs-zq3-zq3-r3-incoming/observer.py --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --probe /root/hfs-zq3-zq3-r3-incoming/installed.py --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278' < $S/tools/lab/retained9p/observer_key.py | tee $E/p3-observer-stage.json
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r3-pod/pod_fixture.py manifest' > $E/p3-pod-manifest.json
# <POD_MANIFEST_SHA256> = the digest the manifest command prints; review, then:
rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r3-pod/pod_fixture.py build --run-id zq3-r3 --expect-manifest-sha256 <POD_MANIFEST_SHA256>' | tee $E/p3-pod-build.json
```

(Rocky's shell is bash, so `VAR=value` prefixes are fine there.) Expect:
install reports one new BLS entry (`<BLS_ENTRY_ID>`) for the r54 kernel
(bzImage `98cc02d5…`, build ID `1f7808b8…`), default still
`5.14.0-687.66.1.el9_8.x86_64`, `saved_entry` unchanged, `next_entry` empty;
observer stage writes `$G/observer.py` and `$G/installed-probe.py` (`0400`)
and creates `$G/m0`, `$G/m1`. Write `<POD_MANIFEST_SHA256>` into a copy of
`$D/pod/pod-request.TEMPLATE.json` (`rootfs_manifest_sha256`). zq3-r1 and
zq3-r2 both got `f7f69339…` (82 entries); a different digest is not a failure
but should be explained before `build`. If Paul declines A8, skip both pod
commands (T7 unrun). Failure: stop before Phase 5 and run U2 (`$K remove
$KP`) while Rocky still runs.

## Phase 4: f3 window, fusefs, observer key (A5, A6; B for facts/tests; about 10 min)

Step 0a (A5, **D1-a only**, Paul's explicit approval): unload the two
modules zq3-r2 left loaded, so that step 0 sees the state the pins expect.
Each line on its own:

```bash
f3 'kldstat -v -n fusefs; kldstat -v -n fdescfs; mount -p | grep -E "fusefs|fdescfs" || echo no-fuse-fdesc-mounts' > $E/p4-modules-before-unload.txt
f3 "doas -n kldunload fusefs"      # normal unload; "Device busy" or any error: stop, never -f
f3 "doas -n kldunload fdescfs"     # same
f3 'kldstat' > $E/p4-modules-after-unload.txt   # neither fusefs.ko nor fdescfs.ko listed
```

Expect `Refs 1` for both files and `no-fuse-fdesc-mounts` before the unload.
After it, `kldstat` matches the zq3-r2 Phase 1 list (`kernel`, `vmm.ko`,
`nmdm.ko`, `if_bridge.ko`, …). Then Phase 4 loads `fdescfs` in `setup` and
`fusefs` in step 4, exactly as in zq3-r2.

Step 0, read-only facts (B). Library pins are by `realpath` (g54, zq3-r1
R4): `/usr/local/lib/libintl.so.8` is a symlink to `libintl.so.8.4.6` and the
controller hashes with `O_NOFOLLOW`. `sh/p4-libs.sh` skips the four base
libraries already in the first list (the filler refuses duplicate paths):

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
f3 'sha256 -r /usr/bin/ssh' | diff - $D/facts/f3-usr-bin-ssh-20261109T141151Z.sha256 && echo F1-unchanged
f3 'env TZ=UTC ps -p 45407 -o lstart=; doas -n ls /dev/vmm; kldstat' > $E/p4-rocky-pin.txt
```

Expect `p4-python-libs.sha256` to list exactly the real paths
`/usr/local/lib/libpython3.12.so.1.0`, `/usr/local/lib/libintl.so.8.4.6`,
`/usr/lib/libdl.so.1`, `/lib/libutil.so.10`, `/lib/libm.so.5` (zq3-r1's and
zq3-r2's closure; any other library is a fact change to review),
`F1-unchanged` (otherwise stop: the filled bootstrap plan pins `6a9e5573…`,
and refilling F1 is a plan revision), and `Mon Nov  9 13:00:32 2026` with
`/dev/vmm` = `rocky`.

Split the facts and fill the window plan on the workstation (B) with the r3
window pins (`1e3305fb…`: the committed pins with `zq3-r1` → `zq3-r3`), then
review the printed SHA256. Without D1-a (or D1-b) the filler refuses here
with `fdescfs module present; plan is for absent`: stop, nothing on f3 has
changed yet. The plan expires 55 minutes after `<F3_NOW>`; setup and qualify
must run before that (restore ignores expiry):

```bash
head -n 1 $E/p4-facts-and-now.txt > $E/p4-facts.json
F3_NOW=$(tail -n 1 $E/p4-facts-and-now.txt)
python3 -I -B $S/tools/lab/fdescfs-window-plan.py --pins $D/fdescfs-pins/pins.json \
  --facts $E/p4-facts.json --host-sha256 $E/p4-host.sha256 --expires $(( F3_NOW + 3300 )) --host-now $F3_NOW \
  --out $D/fdescfs-plan          # prints <WINDOW_PLAN_SHA256>
f3 "doas -n sh -c 'umask 277; test ! -e $R/records/fdescfs-plan.json && cat > $R/records/fdescfs-plan.json'" < $D/fdescfs-plan/plan.json
f3 doas -n sha256 -r $R/records/fdescfs-plan.json      # must equal <WINDOW_PLAN_SHA256>
```

F1 (bootstrap `observer.ssh_sha256`) was filled at fill time from the 14:11Z
fact (`fill_plan.py bootstrap`, plan `077cfe66…`, `bootstrap.py check`
passed); the `F1-unchanged` line above is its recheck. Nothing to fill here.

Step 1, negative before setup (B; a skip counts as unrun). No `--` before the
command; a retry takes a new stage name and keeps the refused one. The o54
host profile needs free ≥ 1 GiB and free + inactive ≥ 3.5 GiB (14:11Z: 4.73 GB
free, 2.93 GB inactive):

```bash
f3 "$NG --stage unsupported-before $R/bin/controlledruntime.test '-test.run=^TestDescriptorExecNativeUnsupported\$' -test.count=1 -test.timeout=60s -test.v" | tee $E/p4-unsupported-before.txt
```

Steps 2-4 (A5): setup, qualify (under the guard), fusefs. The root-private
parent `/var/tmp/hfs-fdescfs-windows` exists (`root wheel` `0700`, with
`zq3-r1`, `zq3-r1.refused-setup-1` and `zq3-r2`); it is checked here, never
created. `setup` refuses an existing run directory:

```bash
f3 "doas -n stat -f '%Sp %Su %Sg %N' /var/tmp/hfs-fdescfs-windows"   # drwx------ root wheel /var/tmp/hfs-fdescfs-windows
f3 "doas -n test ! -e /var/tmp/hfs-fdescfs-windows/zq3-r3 && echo absent"   # must print absent
W="$R/bin/fdescfs-window.py --inspector $R/bin/hfs-controlled-runtime --plan $R/records/fdescfs-plan.json --plan-sha256 <WINDOW_PLAN_SHA256> --directory /var/tmp/hfs-fdescfs-windows/zq3-r3"
f3 "doas -n /usr/local/bin/python3.12 -I -B $W setup" | tee $E/p4-window-setup.txt
f3 "$NG --stage qualify /usr/local/bin/python3.12 -I -B $W qualify" | tee $E/p4-window-qualify.txt
f3 "doas -n $R/bin/hfs-controlled-runtime --check-descriptor-exec" | tee $E/p4-check-descriptor-exec.txt   # must report fdescfs, supported
f3 'kldstat; kldstat -q -n fusefs || doas -n kldload /boot/kernel/fusefs.ko; kldstat' | tee $E/p4-fusefs.txt
```

Disclosed: fdescfs and fusefs stay loaded until f3 reboots (or a later
approved unload). If setup or qualify refuses and a reviewed fix follows,
resume as zq3-r1 did: keep the refused window directory under a new name
(`zq3-r3.refused-setup-1`, via `printf … | f3 doas -n sh -s`), and rerun
step 0 and the window plan fill with `-resume` names (`p4-*-resume.*`, `--out
$D/fdescfs-plan-resume`, remote `$R/records/fdescfs-plan-resume.json`, `W`
pointing at it). The plan's expiry also forces this refill after a pause of
more than 55 minutes.

Step 5 (A6), run-only observer key:

```bash
f3 "doas -n test ! -e $R/observer/id_ed25519 && doas -n ssh-keygen -q -t ed25519 -N '' -C hfs-zq3-zq3-r3-observer -f $R/observer/id_ed25519"
f3 doas -n cat $R/observer/id_ed25519.pub > $E/observer-id_ed25519.pub
rocky '(umask 077; cat > /root/hfs-zq3-zq3-r3-incoming/id_ed25519.pub)' < $E/observer-id_ed25519.pub
okpin && rocky '/usr/bin/python3.9 -I -B - enrol --run-id zq3-r3 --public-key /root/hfs-zq3-zq3-r3-incoming/id_ed25519.pub --observer-sha256 2e1449dd153b83d5a110ce439e0d0754125412a20f8d15d07f7ad4122a3098aa --python /usr/bin/python3.9 --python-sha256 1143733d3cbc738039e938d5875bb694641cf898f4f39f22c956e5b4d974c278 --expect-sha256 e08542dccf25c758af2a9dce35428c9680101cd015295d92499f4d6859866855' < $S/tools/lab/retained9p/observer_key.py | tee $E/p4-observer-enrol.json
```

Record `<ENROL_AFTER_SHA256>` from the enrol output (needed in Phase 11).
The private key never leaves f3. Failure: nothing unloaded; U2 the unselected
kernel; restore the window (Phase 11 step 10).

## Phase 5: pre-stop rechecks, memory, one-shot select and stop Rocky (A1, A3; about 6 min)

Step 1, pre-stop rechecks (B, g54, zq3-r1 R5). In zq3-r1 an external f3
power-off during a pause restarted Rocky and dropped the window, and select
and stop then ran on the new instance. These compare with the Phase 1 facts:

```bash
hpin
f3 'sysctl -n kern.boottime' > $E/p5-boottime.txt              # expect: { sec = 1794212992, usec = 931825 } Mon Nov  9 10:29:52 2026
f3 'env TZ=UTC ps -p 45407 -o lstart=' > $E/p5-rocky-lstart.txt  # expect: Mon Nov  9 13:00:32 2026
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/fdescfs-window.py facts --inspector $R/bin/hfs-controlled-runtime" > $E/p5-facts.json
f3 doas -n cat /var/tmp/hfs-fdescfs-windows/zq3-r3/owned.json > $E/p5-owned.json
python3 -I -B $D/sh/p5-recheck.py $E/p5-boottime.txt $E/p5-rocky-lstart.txt $E/p5-facts.json $E/p5-owned.json | tee $E/p5-recheck.json
```

`p5-recheck.py` passes (exit 0, `"pass": true`) only if the boot time (also
the `boot` field of `facts`) and the UTC lstart equal the values above, and
the parsed `descriptor` of `facts` equals the parsed `owned.json` (a
supported `fdescfs` report with the owned FSID).

Step 1a, memory pre-estimate (B) and the disk-level reference:

```bash
f3 'doas -n vm list; swapinfo; zfs list -t snapshot -o name,guid -s creation zroot/bhyve/rocky | tail -1' > $E/p5-prestop.txt
f3 'sysctl -n hw.pagesize vm.stats.vm.v_free_count vm.stats.vm.v_inactive_count vm.stats.vm.v_active_count vm.stats.vm.v_laundry_count vm.stats.vm.v_wire_count kstat.zfs.misc.arcstats.size kstat.zfs.misc.arcstats.c_min; ps -p 45407 -o rss=' > $E/p5-mem.txt
python3 -I -B $D/sh/p5-estimate.py $E/p5-mem.txt | tee $E/p5-estimate.json
```

`p5-estimate.py` prints two estimates of what Phase 6 will measure. The
zq3-r2 sheet's `rss_based` one, free + inactive + (ARC − `c_min`) + Rocky's
bhyve RSS, is **pessimistic**: on f3 Rocky's guest memory shows as Active
pages, not in the bhyve RSS. In zq3-r2 the RSS was 0.84 GB, while the stop
returned about 7.2 GB that had been Active, so that formula gave 6.8 GB
against a real 13.2 GB before the drain. The better estimate is
`active_based` = free + inactive + (ARC − `c_min`) + Active. It is an upper
bound, because v_active_count also holds the host's own Active pages.

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
rose from 711,398 to 1,256,624 pages (2.2 GB) and wired fell from 831,056 to
288,765 pages. The drain is the same reclaim the pagedaemon runs under
pressure, done now so that free pages are on hand for Phase 6. Without it,
zq3-r2's Phase 6 would likely have failed. **Never cap
`vfs.zfs.arc_max`:** in zq3-r2 the freed ARC stayed in UMA caches, so free
pages did not rise, and the cap was reverted at once.

**Failure in step 1** (any recheck differs): stop before step 2. Nothing is
selected, so `next_entry` is still empty (confirm with `grub2-editenv - list`
once Rocky answers; the `rocky_kernel.py` state journal shows no select) and
U1 is not needed. Two cases (run plan, Phase 5):

- f3 booted since Phase 1 (boot time differs): the window mount and modules
  are gone. Record the retained window directory by hand (`fdescfs-window.py`
  has no action for a vanished mount): `f3 "doas -n ls -la
  /var/tmp/hfs-fdescfs-windows/zq3-r3"` and
  `f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r3" >
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
14,763,950,080 (`p6-bar.json` `"pass": true`); swap unused; all VMs stopped and `no-vmm`; `/dev/fd` still
the owned fdescfs mount (`descriptor-equals-owned: True`). For reference,
zq3-r2 measured 15,457,831,824 bytes here after the Phase 5 UMA drain
(margin 0.69 GB). `c_min` is the effective ARC floor that `runtime-guard.py`
also subtracts (about 527 MB on f3); `vfs.zfs.arc_min` 0 only means "default
floor".

**Only if** the bar fails and step 1b did not run in Phase 5: run the three
step 1b lines once with `p6-uma-*` names, then repeat the `p6-mem` and
`p6-bar` lines as `p6-mem-2.txt`/`p6-bar-2.json`. Do this at most once. If
the bar still fails, restore the window (Phase 11 step 10, `$W restore`), then
U3.

## Phase 7: host setup (A4, about 3 min)

```bash
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py setup --record $R/records/bhyve-argv.json --record-sha256 511179091c5a7fcee6e71323671eac66c52f4e839f9dd118ad4b310df5906c7d" | tee $E/p7-host-setup.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/runtime-volume-init.py --plan $R/records/volume-init.json --plan-sha256 a2dc2c8c28b534bf0ddfe0dec637849677b255a7e8d45f4946747dea5608c876 --record $R/records/volume-init-result.json" | tee $E/p7-volume-init.json
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r3 --path $R/runtime/mnt --label baseline" > $E/k04-baseline.json
```

If `host.py setup` refuses only because the UEFI vars hash drifted from
`c71240e9…` (any Rocky boot since 14:11Z changes it; it was `cab0ac2c…` at
zq3-r2): stop; regenerating the argv record from fresh facts (same sessions,
same args) is a plan revision for review, never an on-the-spot edit. Failure
with no child started: `host.py cleanup --record … --record-sha256 …
--no-child`, restore the window, then U3.

## Phase 8: runtime launch and bootstrap (A3, A4, A7; at most 120 s, about 5 min with prep)

Signing is delegated to Claude (A7, Paul's decision): the driver's stdout goes
through the watcher `bin/sign_watch.py` (`03be0b7e…`, zq3-r2's watcher with
only `D` changed). It copies every line through and, for an
`operator_action` line, runs the signer only if the printed command is
exactly one of the two commands below, the request file's SHA256 equals the
printed `request_sha256` and the signer still hashes to `22e5fb90…`; each
action at most once. Anything else is logged as `"watcher": "refused"` and
left unsigned, so the driver times out (fails closed). The signer pin lives
in two places, `hpin` (`session.sh`) and the watcher's `SIGNER_SHA256`: if
`hfs-retained9p-operator` is ever rebuilt and its hash changes, both pins
change (and with them the watcher's own hash in `hpin` and [pins](pins.json)),
which is a plan revision. Prepare:

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
  --plan-sha256 077cfe661678ef280b65f261a86db37db0401c16538881c8ae824293de636a00 --output $D/bootstrap-run \
  | python3 -I -B $D/bin/sign_watch.py | tee $E/p8-bootstrap.jsonl
```

The plan is re-derived from the zq3-r2 template that ran (q54): it pins the
main `runtime-guard.py` `fece96b3…`, the r3 kernel plan `a3f5feea…` and the
main `rocky_kernel.py` `35ef1f1c…`. The driver launches, under the outer
guard, exactly:

```
/usr/local/bin/doas -n /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r3/bin/runtime-guard.py --run-dir /var/tmp/hfs-validation-xz3/zq3-r3/guard --runtime-sha256 174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7 --vm-name hfs-zq3-r3 --disk-root /var/tmp/hfs-validation-xz3/zq3-r3 --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r3 --skip-path /var/tmp/hfs-validation-xz3/zq3-r3/runtime/mnt -- /var/tmp/hfs-validation-xz3/zq3-r3/bin/hfs-controlled-runtime --config /var/tmp/hfs-validation-xz3/zq3-r3/runtime/runtime-config.json --disposable-validation
```

(guard limits = its defaults = plan ceilings: 3600 s, runtime 4 GiB + 256 MiB
RSS, bhyve 6 GiB + 512 MiB, disk 4 GiB, 8 GiB free, no swap, 512 MiB
available memory, one SIGTERM on breach. `runtime-guard.py` drops its own
leading `--`, unlike the native guard. Since p54 it excludes its own lineage
from `foreign-bhyve` (zq3-r2 R2-2), and since q54 it accepts exactly
`RUNTIME --config CONFIG --disposable-validation` (R2-3).)

The two signer commands the watcher accepts (the driver prints them as
`"operator_action": "sign-enroll"` and `"sign-bind"`):

```
/home/paul/tmp/claude/hfs-zq3-r3-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r3-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/signer-binding
```

Expected order: host-preflight, launch, proposal, guest-ready (test kernel;
GRUB consumed `next_entry`), admission (manifest SHA256, boot ID, provider
nonce, `virtioN` names), enroll-request, **sign-enroll** (`"watcher":
"signed"`, exit 0), provision, enrollment-handoff, awaiting-bind,
bind-request, **sign-bind** (signed, exit 0), bind, mount-0, mount-1, READY.
zq3-r2 reached bind and then failed mount-0 with EPROTO (R2-4); the r54
kernel builds the mount HELLO after enrollment, so mount-0 is the first new
ground. The driver never reads the key.

Failure, a watcher refusal or `uncertain`: nothing is replayed; inspect
`$D/bootstrap-run/journal.jsonl` and `$E/p8-bootstrap.jsonl`, collect the
failure evidence read-only (as in zq3-r2: `p8-fail-guest.txt` with `uname
-r`, boot ID, `dmesg | grep -E "9p|virtio"` and the virtio driver list;
`p8-fail-host.txt`, `p8-fail-exporter.txt`), then go to Phase 11. **A mount
failure is never retried in the same guest boot** (rules above: the failed
mount keeps its identity claimed and a retry returns `EACCES`); the runtime
then stays in `AWAITING_GUEST_BIND` until it exits at its 120 s bootstrap
deadline. If no guest kernel ran (no GRUB output), `next_entry` is
unconsumed: U3 after the child is positively gone.

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
  --template-sha256 a51e10c10bebe1e1728277f46ad982170067d2fab552977e8e1457a676a65fbd \
  --bootstrap-run $D/bootstrap-run --bootstrap-plan-sha256 077cfe661678ef280b65f261a86db37db0401c16538881c8ae824293de636a00 \
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
`rocky '/usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r3-tools/guest-lock-probe.py' < $E/lock-m0.json > $E/t6-lock.json`.
T6b (across m0/m1): recorded **unsupported**, not run.

T7 (A8): fill `<POD_MANIFEST_SHA256>` and `<M0_ID>` into the pod request, copy
it to Rocky and run
`rocky '(umask 077; cat > /root/hfs-zq3-zq3-r3-pod/request.json)' < $E/pod-request.json` then
`rocky 'TZ=UTC LC_ALL=C /usr/bin/python3.9 -I -B /root/hfs-zq3-zq3-r3-pod/pod_fixture.py run --request /root/hfs-zq3-zq3-r3-pod/request.json'`;
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
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r3 --path $R/runtime/mnt --label before" > $E/k04-before.json
$CTL lifecycle --output $OUT --sha256 $B --step after-loss
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host_snapshot.py --run-id zq3-r3 --path $R/runtime/mnt --label after" > $E/k04-after.json
python3 -I -B $CR k04 --output $OUT --sha256 $B --baseline $E/k04-baseline.json --baseline-sha256 <h> --before $E/k04-before.json --before-sha256 <h> --after $E/k04-after.json --after-sha256 <h> --record $E/t11.json
```

T8 (K04 guest side, last): `rocky "umount $G/m1"` (normal; busy or hang → T8
unrun); fill `<M1_ID>` into `lock-probe-m1-T8-refusal.TEMPLATE.json` and run the
lock probe with it: it must refuse at admission (mountpoint not present), and
`rocky "ls -A $G/m1"` must print nothing.

Not run: PTE/TLB/GUP (G14), distributed writers (ov3/pv3).

## Phase 11: cleanup (within A1/A4/A5/A6; about 5-10 min)

**The order below is mandatory. Run one command per line and never batch
them** (zq3-r2 R2-6: a batched cleanup skipped the native unmount, and the
window restore then refused). Write each result into the run log before
running the next line.

Before step 1, settle the runtime's state from the guard and journal evidence
(`f3 'pgrep -lf runtime-guard.py'`, `$D/bootstrap-run/journal.jsonl`):

- **Runtime exited at its bootstrap deadline** (as in zq3-r2: a bootstrap
  failure, the runtime exits 1 with `context deadline exceeded` after 120 s,
  and the guard ends with it). The guest still runs under the exporter bhyve
  child, and the runtime has left its `fusefs.homelabfs` mount at
  `$R/runtime/mnt` (R2-6). Take `<EXPORTER_PID>` from the guard evidence
  (`guard/observations.jsonl`, the runtime stderr), then follow steps 1-10
  unchanged. Step 6 waits for the bhyve child, which ends when the guest
  powers off, and step 8 will find the native mount.
- **T10 ended RESTARTED** (r1 finding F5): the runtime still serves under
  the guard. The step 6 `child-gone` check also matches the guard's
  `--vm-name hfs-zq3-r3`, and `vms.sh start-rocky` refuses while the guard
  lives. Before step 1, end it with one SIGTERM to the guard. The guard then
  makes its own bounded stop: one SIGTERM to the runtime, 30 s grace, up to
  300 s observation. Wait for the guard to exit:

  ```bash
  f3 'pgrep -lf runtime-guard.py'                 # exactly one guard; note <GUARD_PID>
  f3 "doas -n kill -TERM <GUARD_PID>"
  ```

  Exit status 0 with the runtime gone: continue. Exit status 3
  (`stop-uncertain.json`, runtime still running): stop and preserve; no
  further signal without Paul's decision. Otherwise Phase 12 waits up to
  the 3600 s bound.
- **FENCED** (expected) or READY ended by the guard: the runtime has exited;
  follow steps 1-10.

Steps, in this order:

```bash
# 1. evidence tars (private; runtime-config.json is not included)
f3 "doas -n tar -c -f - --exclude control.sock -C $R guard records runtime/owner launch" > $E/f3-evidence.tar
f3 "doas -n tar -c -f - -C /var/tmp/hfs-fdescfs-windows zq3-r3" > $E/f3-window-evidence.tar
# 2. guest 9p mounts: list, then unmount each listed one on its own line
rocky "findmnt -t 9p || echo no-9p"
rocky "umount $G/m0"                             # only if m0 is listed
rocky "umount $G/m1"                             # only if m1 is listed
rocky "findmnt -t 9p || echo no-9p"              # must print no-9p
# 3. observer removal (banner timeout: check authorized_keys is still <ENROL_AFTER_SHA256> before the one retry)
okpin && rocky '/usr/bin/python3.9 -I -B - remove --run-id zq3-r3 --expect-sha256 <ENROL_AFTER_SHA256>' < $S/tools/lab/retained9p/observer_key.py | tee $E/p11-observer-remove.json   # restored_pre_run: true
# 4. grub check
rocky "grub2-editenv - list"                     # next_entry empty; if set: rocky "$K cancel $KP" (U1) on its own line
# 5. guest poweroff
rocky 'systemctl poweroff'
# 6. wait for the child: repeat this line every 10 s, at most 12 times, until child-gone and no-vmm
f3 'ps -ax -ww | grep "[h]fs-zq3-r3" || echo child-gone; test -e /dev/vmm && doas -n ls /dev/vmm || echo no-vmm'
# 7. host cleanup (tap, VM context, vars copy)
f3 "doas -n /usr/local/bin/python3.12 -I -B $R/bin/host.py cleanup --record $R/records/bhyve-argv.json --record-sha256 511179091c5a7fcee6e71323671eac66c52f4e839f9dd118ad4b310df5906c7d --child-pid <EXPORTER_PID>" | tee $E/p11-host-cleanup.json
# 8. native FUSE mount: check, then unmount if listed
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount" | tee $E/p11-native-check.txt
f3 "doas -n umount $R/runtime/mnt"               # only if step 8's check listed it; busy or error: stop and preserve
f3 "mount -p | grep $R/runtime/mnt || echo no-native-mount"   # must print no-native-mount before step 10
# 9. observer key on f3
f3 "doas -n rm -- $R/observer/id_ed25519 $R/observer/id_ed25519.pub"
# 10. window restore, always last
f3 "doas -n /usr/local/bin/python3.12 -I -B $W restore" | tee $E/p11-window-restore.txt
```

`<EXPORTER_PID>` comes from the runtime evidence (guard `observations.jsonl`
/ runtime stdout or stderr). If step 6 still shows the child after 12 tries,
stop: no forced power-off without Paul's decision (risk R2). Release tokens
in case `stdout.log` files are redacted or marked spent before anything is
copied into `docs/lab/results`. Expected restore outcome (j44): the unmount
happens, and the module comparison accepts only the plan-declared `fusefs.ko`
beyond the post-load baseline; `restoration-summary.json` records it in
`window_module_loaded`. If the restore refuses with `normal mount restore
incomplete`, check `mount -p` for a leftover mount first (zq3-r2's cause was
the skipped step 8).

## Phase 12: normal restoration (A1, A3; about 5-10 min)

```bash
f3 'ls -ld /var/tmp/homelabfs-9p-poc/share'           # R6: vm-bhyve's normal start needs it
$S/tools/lab/vms.sh --approved-action start-rocky    # only after Phase 11 verified next_entry empty and the child gone
rocky "$K check --expect stock $KP; getenforce; sha256sum /root/.ssh/authorized_keys; free -m"   # authorized_keys must be e08542dc…
rocky "$K remove $KP" | tee $E/p12-kernel-remove.json   # U2 (Paul chose removal)
f3 'doas -n vm list; doas -n ls /dev/vmm; ifconfig -l; mount -p | grep -E "fdescfs|fusefs" || echo none; kldstat; zpool list zroot' > $E/p12-final.txt
```

Staging prefix deletion is a separate, later step after the evidence is safe
and `mount -p` shows nothing below `/var/tmp/hfs-validation-xz3`. Left on
f3 as evidence: `$R`, `/var/tmp/hfs-20261109-zq3r3-fdq`,
`/var/tmp/hfs-fdescfs-windows/zq3-r3` (and zq3-r1's and zq3-r2's). Left on
Rocky unless Paul decides otherwise: `/root/hfs-zq3-zq3-r3-*`, `$G`
(retention records, pod rootfs and evidence), kernel state journal, and the
zq3-r1 and zq3-r2 leftovers. fdescfs and fusefs stay loaded on f3 again; a
later run meets D1 again.

## Recovery (A3)

- **U1** (Rocky running, `next_entry` set): `rocky "$K cancel $KP"`; manual
  fallback `grub2-editenv - unset next_entry; grub2-editenv - list; grubby --default-kernel`.
- **U2** (stock kernel running, `next_entry` empty): `rocky "$K remove $KP"`.
- **U3** (Rocky stopped with `next_entry` set, no candidate child or
  `/dev/vmm/hfs-zq3-r3`): `$S/tools/lab/vms.sh --approved-action start-rocky`
  (test kernel boots once under the normal config); wait up to 10 min for
  pinned SSH; `rocky "$K check --expect test $KP"`; `rocky "$K
  reboot-to-stock $KP"`; then `rocky "$K check --expect stock $KP"`, U2 and
  the Phase 12 checks. zq3-r1 exercised U3 end to end (16 s to SSH). A hung
  test kernel stops here: a forced power-off needs Paul's explicit decision.
