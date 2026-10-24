# Reproducing the Rocky 9 / bhyve / FUSE PoC

[Findings](PoC-f3-rocky.md) · [Package distribution plan](Packaging-and-rollout.md)

This procedure uses disposable data on f3 and its standalone Rocky VM. It does not deploy HomelabFS replication. Commands are grouped by machine; run guest administration commands as root and host administration commands with doas. Schedule the VM restart before beginning. Do not apply this to all Kubernetes nodes at once.

## 1. Check prerequisites and preserve rollback

On Rocky:

```sh
uname -r
cat /etc/rocky-release
grep -E 'CONFIG_(NET_9P|9P_FS)' /boot/config-$(uname -r)
mokutil --sb-state
grubby --default-kernel
grub2-editenv list
df -h /boot /var/tmp
```

Record the stock kernel path and saved GRUB entry. The tested stock kernel was `5.14.0-687.59.1.el9_8.x86_64`; Secure Boot was disabled. Do not disable Secure Boot on other machines as an implicit part of this procedure.

On f3:

```sh
doas vm list
doas mkdir -p /var/tmp/homelabfs-9p-poc
```

Preserve `/zroot/bhyve/rocky/rocky.conf` as `/var/tmp/homelabfs-9p-poc/rocky.conf.original` once, before modifying it. Do not overwrite an existing backup on reruns. Inspect the current PCI device slots in `vm-bhyve.log`; slot `0:7:0` was unused on the tested VM.

## 2. Install the validated kernel alongside the stock kernel

The matching-stock-source 9p module build failed; [the findings](PoC-f3-rocky.md#module-build-and-working-alternative) and saved log explain why. Rebuilding those sources with a configuration change alone is not a working recipe.

For this PoC, the guest already had `elrepo-release` installed. On a new test guest, follow [ELRepo's official repository and public-key setup](https://elrepo.org/wiki/doku.php?id=start) first. Keep signature verification enabled. The exact tested packages were:

```sh
dnf -y --disablerepo='*' --enablerepo=elrepo-kernel install \
  kernel-lt-6.1.190-1.el9.elrepo \
  kernel-lt-modules-extra-6.1.190-1.el9.elrepo
```

DNF also installs matching `kernel-lt-core` and `kernel-lt-modules`. Upstream availability of this exact version is not permanent; the [package plan](Packaging-and-rollout.md) retains validated versions internally. A newer version must pass the same gates before replacing this version.

Verify the installed kernel configuration contains `CONFIG_NET_9P=m`, `CONFIG_NET_9P_VIRTIO=m`, and `CONFIG_9P_FS=m`. Check the boot image exists and `modinfo -k 6.1.190-1.el9.elrepo.x86_64 9pnet_virtio` resolves.

Restore the recorded stock default using `grubby --set-default STOCK_KERNEL_PATH`. Use `grub2-reboot ENTRY_ID` for a one-time test boot. Obtain the ELRepo entry ID from its filename under `/boot/loader/entries/`, without `.conf`; do not copy another machine's machine ID. Verify both `saved_entry` and `next_entry` using `grub2-editenv list`.

## 3. Attach the isolated 9p share

On f3:

```sh
doas mkdir -p /var/tmp/homelabfs-9p-poc/share
doas chmod 777 /var/tmp/homelabfs-9p-poc/share
```

The permissive mode is only for a disposable test directory. Add this to the VM configuration, adapting the unused PCI slot if necessary:

```text
bhyve_options="-s 0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share"
```

If `bhyve_options` already exists, merge the device option rather than defining it twice or discarding existing options.

On Rocky, run `sync` and `systemctl poweroff`. Wait until `doas vm list` on f3 reports the VM stopped, then run `doas vm start rocky`. Verify SSH returns and `uname -r` reports the selected ELRepo kernel. If boot fails, use the host console to select the retained stock kernel; the one-time entry should already have been consumed.

On Rocky:

```sh
modprobe 9pnet_virtio
modprobe 9p
mkdir -p /mnt/homelabfs-9p-poc
mount -t 9p -o trans=virtio,version=9p2000.L,cache=mmap,msize=262144 \
  homelabfs_poc /mnt/homelabfs-9p-poc
findmnt /mnt/homelabfs-9p-poc
```

Do not add an fstab entry for the experiment. `cache=none` was tested and failed writable mmap and SQLite WAL; `cache=mmap` passed the initial checks but introduces cache/writeback behavior that still needs failure testing.

## 4. Build and mount the Go FUSE probe

From the HomelabFS checkout on a Go build machine:

```sh
cd tools/poc/fuse-passthrough
go mod download
go vet ./...
GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/homelabfs-fuse-poc .
scp /tmp/homelabfs-fuse-poc f3:/var/tmp/homelabfs-fuse-poc-bin
```

The module pins go-fuse v2.9.0. The implementation is a loopback compatibility probe, with no replication, journal, ownership, or fencing. Its Go module is separate from any future HomelabFS implementation.

On f3:

```sh
doas install -m 755 /var/tmp/homelabfs-fuse-poc-bin /var/tmp/homelabfs-9p-poc/fuse-passthrough
doas mkdir -p /var/tmp/homelabfs-fuse-poc/backing /var/tmp/homelabfs-9p-poc/share/fuse
doas chmod 777 /var/tmp/homelabfs-fuse-poc/backing
```

Load `fusefs` with `doas kldload fusefs` if it is not already loaded. Start the daemon, redirecting all standard streams so the SSH session can close:

```sh
doas daemon -f -p /var/tmp/homelabfs-9p-poc/fuse.pid \
  -o /var/tmp/homelabfs-9p-poc/fuse.log \
  /var/tmp/homelabfs-9p-poc/fuse-passthrough \
  /var/tmp/homelabfs-9p-poc/share/fuse /var/tmp/homelabfs-fuse-poc/backing
```

Inspect the READY log and `mount -p`. Do not start another instance on an existing mount. The backing directory is outside the export and disjoint from the mountpoint. The guest accesses FUSE at `/mnt/homelabfs-9p-poc/fuse`; the existing 9p device traverses the nested host mount, so adding FUSE requires no additional VM restart.

## 5. Functional and I/O checks

Copy `tools/poc/check-filesystem.py` and `tools/poc/check-io.py` to Rocky, for example under `/var/tmp/homelabfs-9p-build/`. Check `findmnt` and the host FUSE mount first: an ordinary directory with the same name is not evidence of the correct data path.

Run as guest root; the permissions check deliberately changes a child process's UID:

```sh
timeout 60 python3 /var/tmp/homelabfs-9p-build/check-filesystem.py /mnt/homelabfs-9p-poc/fuse
timeout 120 python3 /var/tmp/homelabfs-9p-build/check-io.py /mnt/homelabfs-9p-poc/fuse
```

Each script creates and removes its own temporary subdirectory. Run the same scripts on the raw 9p root for comparison. Record exit codes and JSON; do not treat a timeout or missing output as success. These checks do not simulate power loss or prove production compatibility.

For fio, install `fio` with `doas pkg install fio` on FreeBSD and `dnf install fio` on Rocky. Copy `tools/poc/compare.fio` to both. Use the same job file with `fio --directory=TEST_DIRECTORY --output-format=json --output=RESULT.json compare.fio` on four distinct directories: direct f3/ZFS, f3 through the host FUSE mount, guest raw 9p, and guest 9p/FUSE. For the host-only FUSE run, use a fresh directory under `/var/tmp/homelabfs-9p-poc/share/fuse/`, not the raw backing directory. Run paths sequentially, preserve version information, check every job's `error`, and remove only `sequential.bin` and `random.bin` created by the jobs. See the results document for timings and limits.

## 6. Cleanup and rollback

Keep test paths free of workload data. Close only your own test processes and leave other users' shells alone. Unmount the host FUSE mount before removing its mountpoint or backing tree; use a normal unmount and investigate busy mounts instead of forcing them. Unmount the guest 9p mount only after its users exit. Do not recursively delete through a mounted export.

The original configuration backup and retained stock kernel provide rollback. Remove only the added PoC device option from the current VM configuration, preserving any later edits. A clean VM stop/start applies that change. A normal subsequent boot selects the stock kernel unless the operator explicitly changes its default; 9p will then be unavailable. Installed ELRepo packages can remain as an optional test boot. No persistent FUSE service or automatic guest mount was configured.
