# zq3 direct bhyve launch (gap G4)

[Run plan](../../../docs/lab/zq3-run-plan.md) ·
[Base gate](../../../docs/lab/Real-daemon-9p-base-gate.md)

Preparation only. These tools pin the exact bhyve argv with which the
controlled runtime launches Rocky under the candidate exporter, and do the
host setup and post-exit cleanup around that launch. Nothing here has run on
f3. Running `setup` or `cleanup` needs Paul's explicit approval of the filled
plan (A4); a flag or a pinned hash grants no authority.

| File | Where | Effects |
|---|---|---|
| `argv.py` | workstation | none: `generate` pins the argv record from facts, `verify` checks a runtime config |
| `host.py facts` | f3, root | read-only (Phase 1) |
| `host.py setup` | f3, root | UEFI vars copy, one tap in `vm-public` (Phase 7) |
| `host.py cleanup` | f3, root | destroys the run's own vmm context and tap (Phase 11) |
| `test_directlaunch.py` | workstation | local tests with simulated OS boundaries |

## Reconstructed argv

The source is vm-bhyve's own log of the normal Rocky launch
(`/zroot/bhyve/rocky/vm-bhyve.log`, `[bhyve options|devices|console: …]`) plus
`rocky.conf`. vm-run passes `devices, options, console, name` in that order;
`procstat -c` only shows the rewritten title, so the log is the only argv
record. Baseline (2026-11-02 launch, vm-run `43753bc8…`, `rocky.conf`
`c101adf1…`, firmware target `6b48e967…`):

```
-s 0,hostbridge -s 31,lpc -s 0:4:0,nvme,/zroot/bhyve/rocky/disk0.img
-s 0:5:0,virtio-net,tap0,mac=58:9c:fc:4c:38:84 -s 0:6:0,fbuf,tcp=127.0.0.1:5900,vga=io
-c 4 -m 12G -AHPw -l bootrom,/usr/local/share/uefi-firmware/BHYVE_UEFI.fd,/zroot/bhyve/rocky/uefi-vars.fd
-s 0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share
-U e10b6225-44d0-11f1-882a-e8ff1ed7f3d7 -u -l com1,/dev/nmdm-rocky.1A rocky
```

Controlled argv for run `zq3-r1` (each change applies exactly once or the
generator refuses):

```
-s 0,hostbridge -s 31,lpc -s 0:4:0,nvme,/zroot/bhyve/rocky/disk0.img
-s 0:5:0,virtio-net,tap100,mac=58:9c:fc:4c:38:84
-c 4 -m 6G -AHPw -l bootrom,/usr/local/share/uefi-firmware/BHYVE_UEFI.fd,/var/tmp/hfs-validation-xz3/zq3-r1/launch/uefi-vars.fd
-s 0:7:0,virtio-9p -o pci.0.7.0.sharename=hfs0 -o pci.0.7.0.hfs-controller=<session hfs0>
-s 0:8:0,virtio-9p -o pci.0.8.0.sharename=hfs1 -o pci.0.8.0.hfs-controller=<session hfs1>
-U e10b6225-44d0-11f1-882a-e8ff1ed7f3d7 -u -l com1,/dev/nmdm-hfs-zq3-r1.1A hfs-zq3-r1
```

1. Memory 12G becomes 6G (plan decision).
2. The tap is a pinned unit (`tap100`..`tap999`), created by `setup`, not
   vm-bhyve's dynamic `tap0`. NIC slot 0:5:0 and the MAC stay, so the guest
   interface name and address do not change.
3. The fbuf/VNC device is dropped; com1 on a run-owned nmdm pair is the only
   console. GRUB/firmware output on com1 is expected from BHYVE_UEFI's serial
   console but is not yet observed for Rocky without a framebuffer.
4. The UEFI vars are a run-owned copy; the original file is only read.
5. The PoC share is replaced by two dedicated devices. **The `-o` form is
   required:** bhyve's virtio-9p legacy `-s` parser treats `name=value` as
   `sharename=name,path=value` and refuses a second `=`, so the earlier plan
   shape `-s 0:7:0,virtio-9p,sharename=hfs0,hfs-controller=…` would set a
   share named `sharename` with path `hfs0` and then fail. `-o` must follow
   the device's `-s` (pci_emul.c refuses `-s` on an already configured slot).
   The exporter requires canonical lowercase session UUIDs
   (`hfs9p_supervisor_prepare`).
6. The vmm context is `hfs-<run id>`, never `rocky`; run IDs must match
   `zq3-…` so the name cannot collide with `hfs-test0..2`.

Kept on purpose: `-w` and `-u` (the earlier plan sketch omitted both), `-A -H
-P`, CPU count, UUID, slot layout. Not added: `-D` (destroy on poweroff) and
`-S`, which vm-bhyve does not use either.

Without vm-run's loop, a guest **reboot** (bhyve exit 0) ends the child; it is
not restarted.

## Drift refusal

`generate` refuses unless the facts match the reviewed constants in
`argv.py`: `rocky.conf` text and hash, vm-run hash, the firmware symlink (see
below), and every token of the last logged launch block (only the dynamic tap
name may differ).
It also refuses a later `initialising` without its own block, an install ISO
line, a missing bridge, invalid run ID/tap/sessions. A changed profile needs a
reviewed code change, never a re-derivation on the spot.

The record (`homelabfs-zq3-bhyve-argv/1`, canonical JSON) is a pure function
of run ID, sessions, tap and the UEFI vars hash; `validate_record` rebuilds it
and refuses any edited field. `verify` refuses a runtime config whose
`Exporter.Args` differ in any element or order, whose `Exporter.Path` is not
`<staging>/bin/bhyve`, or whose `Exporter.SHA256` (32 numbers in Go's JSON) is
not the candidate bhyve `ef8f17de…` (y64 rebuild; `36423b6d…` before it). **Contract for the G2 config generator:**
copy `args` verbatim and run `verify` before staging.

`setup` re-collects the facts and refuses if anything drifted since the record
was pinned (including the UEFI vars hash, which a normal Rocky boot can change,
and the firmware link text and target hash).

### Firmware symlink

`/usr/local/share/uefi-firmware/BHYVE_UEFI.fd` is a symlink installed by pkg
`edk2-bhyve` (`g202508_3`) to `../edk2-bhyve/BHYVE_UEFI.fd`; bhyve follows it,
and the argv keeps the link path vm-bhyve logged. The first Phase 1 `facts`
run (2026-11-08) refused it with `EMLINK` (FreeBSD's errno for `O_NOFOLLOW`
on a symlink). It is now the only symlink these tools follow, one hop:

- `host.py` requires a root-owned symlink, resolves its text lexically and
  refuses a target outside `/usr/local/share` before opening anything. The
  target is digested with `O_NOFOLLOW` and must be a root-owned regular file,
  not writable by group/other, below root-controlled real directories.
- The fact records `link`, `target`, `sha256` and `bytes`. An unexpected but
  confined link text is recorded as Phase 1 evidence; `argv.py` then refuses
  unless the text is exactly `../edk2-bhyve/BHYVE_UEFI.fd`, the target is
  `/usr/local/share/edk2-bhyve/BHYVE_UEFI.fd` and its hash is `6b48e967…`
  (the 3,653,632 target bytes; the earlier `sha256(1)` pin followed the link).
  The record carries the link text and target too.
- Every other symlink, including a second hop from the firmware target, is
  still refused (`symlink refused`).

## Host safety properties

- `setup` refuses before any change unless every vm-bhyve guest is Stopped,
  `/dev/vmm` is empty, the pinned tap and nmdm names do not exist, `vm-public`
  exists, no process names the run VM and the launch directory is new.
- `cleanup` needs positive evidence that the child is gone: `--child-pid` (from
  runtime evidence) must not exist and no process may carry the VM name; or
  `--no-child`, which refuses if the run's vmm context exists. It never sends a
  signal, including to a hung short command on a deadline.
- Only the record's own vmm context is destroyed, only if `/dev/vmm` was empty
  before setup and the name is not a vm-bhyve guest. Foreign contexts are left.
- Only the pinned tap is destroyed, only if setup's `tap-create` succeeded, the
  tap did not exist before, and its description is the run label. `tap0` and
  any relabelled tap are left.
- nmdm pairs have no destroy command and stay as inert nodes; `cleanup`
  reports them. The vars copy is preserved with its final hash; the original
  must still match the pin.
- `/dev/nmdm*` and `/dev/tap*` are never looked up by name (devfs cloning
  would create them); `/dev` is listed instead.
- Every mutating command writes `<step>.intent.json` first. A failed or
  uncertain step leaves no result and blocks every later `setup`/`cleanup`
  until root reconciles it. A clean refusal (for example the child still
  running) dispatches nothing, so `cleanup` can simply be rerun later.

## Usage (all inside the approved window)

```
# Phase 1, read-only, before anything is staged:
ssh f3 doas -n /usr/local/bin/python3.12 -I -B - facts < tools/lab/directlaunch/host.py > launch-facts.json
# Workstation, after Phase 1 and before Phase 2 staging:
python3 -I -B tools/lab/directlaunch/argv.py generate --facts launch-facts.json \
  --run-id zq3-r1 --session <hfs0 uuid> --session <hfs1 uuid> --tap tap100 --output bhyve-argv.json
python3 -I -B tools/lab/directlaunch/argv.py verify --record bhyve-argv.json \
  --record-sha256 <sha> --runtime-config <private runtime config>
# Phase 7 / Phase 11 on f3, with host.py, argv.py and the record staged:
/usr/local/bin/python3.12 -I -B host.py setup --record <path> --record-sha256 <sha>
/usr/local/bin/python3.12 -I -B host.py cleanup --record <path> --record-sha256 <sha> --child-pid <pid>
```

Tests: `python3 -B -m unittest test_directlaunch` in this directory. They use
a temporary root, a command simulator and a model of bhyve's option parser;
the hashes of vm-run and the firmware are patched to fake files. The firmware
is laid out as the package installs it (relative symlink plus target).
`testdata/` holds the retained 2026-11-05 `rocky.conf` and vm-bhyve log tail.

## Open facts and limits

- The baseline log block is the 2026-11-02 launch (PID 6444). On 2026-11-08
  Rocky ran as PID 80203, so it was restarted since; that launch's log block,
  the current UEFI vars hash and the vm-run/firmware hashes must come from a
  fresh `host.py facts` run before a record can be generated. The 2026-11-08
  run failed on the firmware symlink; after the fix above a read-only rerun
  on f3 (2026-11-08 21:07Z) recorded the pinned link, target and hash.
- The tap unit must be chosen free from those facts.
- Tested locally only (Python 3.14; f3 runs 3.12, no newer syntax is used).
  `ifconfig`, `bhyvectl`, `ps` and `vm list` outputs are simulated from their
  documented formats; the first native use is the approved run itself.
