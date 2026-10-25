#!/usr/bin/env bash
# Separate Rocky/host transport check; does not use the three FreeBSD guests.
set -euo pipefail
if [[ $# != 3 || $1 != --approved-run ]]; then
    echo "Usage: $0 --approved-run RUN_ID FREEBSD_BINARY" >&2
    exit 2
fi
run=$2
[[ $run =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
binary=$(realpath "$3")
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
results="$repo/docs/lab/results/$run"
[[ ! -e $results ]] || exit 1
mkdir -p "$results"
work="/var/tmp/hfs-poc-$run"
# Check the kernel before creating mounts or test data.
ssh -o BatchMode=yes root@rocky 'modinfo 9pnet_virtio >/dev/null; modinfo 9p >/dev/null'
# shellcheck disable=SC2029 # run ID has a restricted character set.
ssh root@rocky "mkdir '$work'"
scp "$repo/tools/poc/check-filesystem.py" "$repo/tools/poc/check-io.py" "root@rocky:$work/"
scp "$binary" "f3:/var/tmp/hfs-fuse-$run"
# shellcheck disable=SC2029 # run ID has a restricted character set.
ssh f3 "doas -n sh -s -- '$run'" <<'HOST'
set -eu
run=$1
work="/var/tmp/hfs-poc-$run"
backing="/var/tmp/homelabfs-fuse-poc/$run"
mnt="/var/tmp/homelabfs-9p-poc/share/$run"
mkdir "$work" "$backing" "$mnt"
if ! kldstat -q -m fusefs; then kldload fusefs; fi
install -m 755 "/var/tmp/hfs-fuse-$run" "$work/fuse-passthrough"
daemon -f -p "$work/fuse.pid" -o "$work/fuse.log" \
    "$work/fuse-passthrough" "$mnt" "$backing"
sleep 1
mount -p | awk -v p="$mnt" '$2==p && $3 ~ /^fusefs([.]|$)/ {found=1} END {exit !found}'
mount -p > "$work/mounts.txt"
HOST
# shellcheck disable=SC2329 # Called by EXIT/INT/TERM traps below.
cleanup() {
    local status=$?
    trap - EXIT
    # Guest runner unmounts its 9p mount before host cleanup.
    # shellcheck disable=SC2029
    ssh f3 "doas -n sh -s -- '$run'" <<'HOST' || status=1
set -eu
run=$1
mnt="/var/tmp/homelabfs-9p-poc/share/$run"
umount "$mnt"
rmdir "$mnt" "/var/tmp/homelabfs-fuse-poc/$run"
HOST
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
status=0
# shellcheck disable=SC2029
ssh root@rocky "bash -s -- '$run'" > "$results/rocky-run.log" 2>&1 <<'GUEST' || status=$?
set -euo pipefail
run=$1
work="/var/tmp/hfs-poc-$run"
mnt="/mnt/hfs-poc-$run"
cd "$work"
mkdir "$mnt"
cleanup() {
    status=$?
    trap - EXIT
    if mountpoint -q "$mnt"; then umount "$mnt" || exit 1; fi
    rmdir "$mnt" || status=1
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
modprobe 9pnet_virtio
modprobe 9p
mount -t 9p -o trans=virtio,version=9p2000.L,cache=mmap,msize=262144 homelabfs_poc "$mnt"
findmnt -t 9p > mounts.txt
uname -a > environment.txt
cat /etc/rocky-release >> environment.txt
getenforce >> environment.txt
# Separate fresh temporary subdirectories are created/removed by each probe.
timeout 60 python3 check-filesystem.py "$mnt" > raw9p-functional.json
timeout 120 python3 check-io.py "$mnt" > raw9p-io.json
timeout 60 python3 check-filesystem.py "$mnt/$run" > fuse9p-functional.json
timeout 120 python3 check-io.py "$mnt/$run" > fuse9p-io.json
GUEST
scp "root@rocky:$work/*.json" "root@rocky:$work/*.txt" "$results/" || status=1
# Host logs are root-readable via doas, with no private key material.
# shellcheck disable=SC2029
ssh f3 "doas -n cat '$work/fuse.log'" > "$results/host-fuse.log"
# shellcheck disable=SC2029
ssh f3 "doas -n cat '$work/mounts.txt'" > "$results/host-mounts.txt"
printf '%s\n' "$status" > "$results/exit-status.txt"
exit "$status"
