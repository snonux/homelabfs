#!/usr/bin/env bash
# Operator invokes this only after approving this bounded run; no VM lifecycle.
set -euo pipefail
if [[ ${1:-} != --approved-run || $# != 4 ]]; then
    echo "Usage: $0 --approved-run RUN_ID FREEBSD_BINARY PACKAGE_TAR" >&2
    exit 2
fi
run=$2
[[ $run =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
binary=$(realpath "$3")
packages=$(realpath "$4")
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
results="$repo/docs/lab/results/$run"
[[ ! -e $results ]] || { echo 'Run ID already exists.' >&2; exit 1; }
mkdir -p "$results"
opts=(-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes
    -o "UserKnownHostsFile=$repo/docs/lab/f3-known_hosts" -J paul@f3)
for i in 0 1 2; do
    guest="root@172.31.253.$((11+i))"
    remote="/var/tmp/hfs-poc-$run"
    # shellcheck disable=SC2029 # run ID is restricted to safe characters above.
    ssh "${opts[@]}" "$guest" "mkdir -m 755 '$remote'"
    scp "${opts[@]}" "$binary" "$guest:$remote/fuse-passthrough"
    scp "${opts[@]}" "$packages" "$guest:$remote/packages.tar"
    scp "${opts[@]}" "$repo/tools/poc/check-filesystem.py" \
        "$repo/tools/poc/check-io.py" "$guest:$remote/"
    status=0
    # shellcheck disable=SC2029 # validated run ID and numeric index.
    ssh "${opts[@]}" "$guest" "sh -s -- '$remote' '$i'" \
        > "$results/hfs-test$i-run.log" 2>&1 <<'GUEST' || status=$?
set -eu
work=$1
index=$2
cd "$work"
mkdir packages
tar -xf packages.tar -C packages
# FreeBSD VM images include pkg; dependencies are staged, no network needed.
ASSUME_ALWAYS_YES=yes
export ASSUME_ALWAYS_YES
# Upgrade pkg in its own process before installing packages/migrating its DB.
find packages -name 'pkg-*.pkg' -exec /usr/local/sbin/pkg add {} +
find packages -name '*.pkg' ! -name 'pkg-*.pkg' -exec /usr/local/sbin/pkg add {} +
uname -a > environment.txt
zpool status >> environment.txt
zfs list >> environment.txt
kldstat >> environment.txt
sysctl vfs.zfs.arc.max >> environment.txt
pkg info >> environment.txt
mount -p >> environment.txt
for peer in 11 12 13; do ping -c 2 "172.31.253.$peer" >> environment.txt; done
zpool status -x "hfsdata$index"
test "$(df -T /var/db/homelabfs/data | awk 'NR==2 {print $2}')" = zfs
backing="/var/db/homelabfs/data/$(basename "$work")"
mkdir "$backing" "$work/mount"
chmod 755 fuse-passthrough
cleanup() {
    status=$?
    trap - EXIT
    if mount -p | awk -v p="$work/mount" '$2==p {found=1} END {exit !found}'; then
        umount "$work/mount" || exit 1
    fi
    # No recursive deletion of unknown files; probes clean their own payloads.
    rmdir "$work/mount" "$backing" || status=1
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
daemon -f -p "$work/fuse.pid" -o "$work/fuse.log" \
    "$work/fuse-passthrough" "$work/mount" "$backing"
ready=no
for attempt in 1 2 3 4 5; do
    if mount -p | awk -v p="$work/mount" '$2==p && $3 ~ /^fusefs([.]|$)/ {found=1} END {exit !found}'; then
        ready=yes; break
    fi
    sleep 1
done
test "$ready" = yes
mount -p > mounts-during-test.txt
# Baseline distinguishes guest/ZFS problems from FUSE problems.
timeout 60 python3.12 check-filesystem.py "$backing" > zfs-functional.json
timeout 120 python3.12 check-io.py "$backing" > zfs-io.json
timeout 60 python3.12 check-filesystem.py "$work/mount" > fuse-functional.json
timeout 120 python3.12 check-io.py "$work/mount" > fuse-io.json
zpool status > pool-after.txt
GUEST
    mkdir "$results/hfs-test$i"
    scp "${opts[@]}" "$guest:$remote/*.json" \
        "$guest:$remote/*.txt" "$guest:$remote/fuse.log" \
        "$results/hfs-test$i/" || status=1
    printf '%s\n' "$status" > "$results/hfs-test$i-exit-status.txt"
    (( status == 0 )) || { echo "hfs-test$i failed; inspect $results" >&2; exit 1; }
done
echo "Native PoC passed on all three guests: $results"
