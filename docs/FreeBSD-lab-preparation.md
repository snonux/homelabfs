# Prepared FreeBSD lab on f3

On 2026-10-25, f3 was powered on using the f3s skill's `f3sctl power f3 on` workflow. Three FreeBSD 15.1-RELEASE guests were prepared offline at the operator's request. **They are stopped and have never been booted. No integration or smoke tests were run.** Rocky remained running, with its configuration and `vm_list=rocky` unchanged.

| VM | Private address | CPU | RAM | OS disk | ZFS data disk |
|---|---|---|---|---|---|
| hfs-test0 | 172.31.253.11 | 2 | 2 GiB | 20 GiB | 16 GiB |
| hfs-test1 | 172.31.253.12 | 2 | 2 GiB | 20 GiB | 16 GiB |
| hfs-test2 | 172.31.253.13 | 2 | 2 GiB | 20 GiB | 16 GiB |

These are preparation assets, not a completed integration environment. HomelabFS and etcd are not installed. Boot, SSH connectivity, guest pool import, and FUSE operation still need validation during an approved window. The full [test runner](Test-automation.md) remains future work.

## Storage and networking

The vm-bhyve datastore `hfslab` uses `zroot/bhyve/homelabfs-lab`, inheriting encryption from `zroot/bhyve`. Its quota is 120 GiB; each VM dataset has a 40 GiB quota. Disk files are accessible only to root. Guests use UFS root filesystems and separate ZFS pools `hfsdata0`, `hfsdata1`, and `hfsdata2`. Each pool contains `data`, `journal`, and `control` datasets under `/var/db/homelabfs`. Pools were created and cleanly exported offline; a guest rc script imports the preformatted pool without force at boot. Guest ARC is capped at 256 MiB and guest swap is disabled for this initial small profile.

The `hfslab` switch (`vm-hfslab`) has host address `172.31.253.1/24`, no physical uplink, and no NAT. Host IPv4 forwarding was disabled at preparation. Guests have static addresses, no default route, and no DNS configuration. Packages must be staged through f3; no Internet package access has been configured. Recheck network isolation before each run, especially if host forwarding changes.

Each guest has a unique UUID, MAC address, host ID, and SSH host keys. Only Paul's existing SSH public key was installed. Root SSH allows public-key authentication; password and keyboard-interactive authentication are disabled. The saved [public host keys](lab/f3-known_hosts) allow pinning on first connection. Once an approved boot succeeds, access from this repository is:

```sh
ssh -o UserKnownHostsFile=docs/lab/f3-known_hosts \
    -o StrictHostKeyChecking=yes -J paul@f3 root@172.31.253.11
```

Use `.12` and `.13` for the other guests. Rebuilding a guest changes its host key; update the pinned file from trusted offline provisioning output.

Existing local zrepl snapshots include the new lab subtree. Existing remote replication selections do not include it. Retained snapshots consume quota even after test files or disks are deleted; account for them during future cleanup and admission checks. No zrepl configuration was changed.

## Reproducing preparation

The executable [preparation script](../tools/lab/prepare-freebsd-vms.sh) creates stopped VMs only. It requires root on f3, vm-bhyve, Bash, and the existing encrypted bhyve dataset to be available. It verifies an immutable image checksum, checks names, disk capacity, routing and forwarding, and refuses to overwrite or implicitly resume an existing lab.

Download `FreeBSD-15.1-RELEASE-amd64-ufs.raw.xz` from the [official FreeBSD VM image directory](https://download.freebsd.org/releases/VM-IMAGES/15.1-RELEASE/amd64/Latest/). The SHA256 checked against the published checksum at preparation was:

```text
a8e2c0f8331be615ef008c167069076833a417e968ff6b7d8e83b4ac762e9409
```

Stage the compressed image and an administrator's **public** SSH key on f3, then invoke the reviewed script for an explicitly authorized preparation:

```sh
doas /usr/local/bin/bash /var/tmp/prepare-homelabfs-vms.sh \
    --prepare-stopped \
    /var/tmp/homelabfs-vm-prep/FreeBSD-15.1-RELEASE-amd64-ufs.raw.xz \
    /var/tmp/homelabfs-admin.pub
```

The current lab already exists: do not rerun this command to start or repair it. On failure, inspect retained resources and the manifest; the script deliberately does not destroy partial resources. The verified compressed image and decompressed public base remain in `/var/tmp/homelabfs-vm-prep` as preparation cache, outside the lab quota.

## Verification and next approved window

Preparation completed for all three entries in the [manifest](lab/f3-manifest.tsv). Host inspection confirmed stopped VMs, unique VM identities, the isolated bridge, inherited encryption, quotas, disk sizes, and unchanged Rocky configuration/autostart. No temporary memory disks or guest data pools remained attached/imported on the host. The script passed `bash -n` and ShellCheck. This verifies offline preparation, not guest runtime behavior.

f3 has approximately 16 GiB physical RAM and Rocky reserves 12 GiB. **Do not start all three guests alongside Rocky.** A subsequent approved plan must include resource admission and, if necessary, a clean temporary Rocky shutdown and restoration; pausing Rocky does not release its RAM. See [lab lifecycle](FreeBSD-test-lab.md) and [per-run approval](Test-automation.md). None of the new guests auto-starts.
